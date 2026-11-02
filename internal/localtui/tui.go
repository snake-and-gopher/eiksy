package localtui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"

	agentai "eiksy/internal/ai"
	appservice "eiksy/internal/app"
	domainai "eiksy/internal/domain/ai"
	"eiksy/internal/securestorage"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type Backend interface {
	GetSecureStorageStatus() securestorage.Status
	EnsureMasterPassword(string) error
	GetLocalAgentState() appservice.LocalAgentState
	SelectAIProvider(string) error
	SaveCloudProvider(model, endpoint, token string) error
	SaveLocalProvider(downloadURL string) error
	DownloadLocalModel(downloadURL string) error
	StartLocalModel() error
	StopLocalModel() error
	ListCloudModels(endpoint, token string) ([]string, error)
	ResolveCommandPolicyRequest(requestID string, mode domainai.CommandPermissionMode) error
}

type screen int

const (
	screenUnavailable screen = iota
	screenSetupPassword
	screenConfirmPassword
	screenUnlockPassword
	screenChat
	screenProviderMenu
	screenCloudConfig
	screenLocalConfig
)

type chatMessage struct {
	role    string
	content string
}

type transcriptEntry struct {
	role    string
	content string
	tool    string
	status  string
	output  string
	isTool  bool
}

type agentEventMessage struct {
	event  agentai.Event
	events <-chan agentai.Event
}

type agentStreamClosed struct{}
type localOperationEvent struct { payload map[string]any }

type agentApprovalDone struct { mode domainai.CommandPermissionMode }

type agentApprovalError struct { err error; mode domainai.CommandPermissionMode }

type providerActionDone struct { message string; closeConfig bool }
type providerActionError struct{ err error }
type providerModelsDone struct{ models []string }
type providerModelsError struct{ err error }
type providerSelected struct{ provider domainai.ProviderDescriptor }
type secureStorageUnlockError struct { err error; resume screen; creating bool }

type Model struct {
	backend Backend
	agent   agentai.Agent
	ctx     context.Context
	cancel  context.CancelFunc

	width, height int
	cursor        int
	scrollOffset  int
	screen        screen
	resumeScreen  screen
	status        string
	input         string
	currentPrompt string
	retryPrompt   string
	retryPromptNeedsAppend bool
	retryApproval bool
	retryApprovalMode domainai.CommandPermissionMode
	password      string
	confirm       string

	messages      []chatMessage
	transcript    []transcriptEntry
	historyLoaded bool
	approval      *agentai.ApprovalRequest
	resolving bool
	running   bool

	providers     []domainai.ProviderDescriptor
	providerIndex int
	configField   int
	cloudModel    string
	cloudEndpoint string
	cloudToken    string
	localURL      string
	modelChoices  []string
}

func NewModel(service *appservice.Service, agent agentai.Agent) Model {
	return NewModelWithBackend(service, agent)
}

func NewModelWithBackend(service Backend, agent agentai.Agent) Model {
	ctx, cancel := context.WithCancel(context.Background())
	m := Model{backend: service, agent: agent, screen: screenUnavailable, ctx:ctx, cancel:cancel}
	if service == nil {
		m.status = "Local agent backend is unavailable."
		return m
	}
	m.refreshProviders()
	m.refreshSecureStorageScreen()
	if service.GetSecureStorageStatus().Unlocked { m.loadHistory() }
	m.restorePendingApproval()
	return m
}

func Run(service *appservice.Service, agent agentai.Agent) error {
	model := NewModel(service, agent)
	program := tea.NewProgram(model, tea.WithAltScreen())
	defer model.cancel()
	if service != nil {
		service.SetRuntimeContext(model.ctx, func(eventName string, data ...interface{}) {
			if eventName != "agent:operate" || len(data) == 0 { return }
			payload, ok := data[0].(map[string]any)
			if !ok { return }
			program.Send(localOperationEvent{payload:payload})
		})
	}
	_, err := program.Run()
	return err
}

func (m *Model) refreshSecureStorageScreen() {
	if m.backend == nil {
		m.screen = screenUnavailable
		return
	}
	status := m.backend.GetSecureStorageStatus()
	switch {
	case !status.Available:
		m.screen = screenUnavailable
		m.status = "Secure storage is unavailable. Check the operating-system keychain."
	case !status.Configured:
		m.screen = screenSetupPassword
		m.resumeScreen = screenChat
		m.status = "Create a master password to protect AI provider credentials."
	case !status.Unlocked:
		m.screen = screenUnlockPassword
		m.resumeScreen = screenChat
		m.status = "Unlock secure storage to continue."
	default:
		m.screen = screenChat
		m.status = "Ready. Type a message or /help."
	}
}

func (m *Model) refreshProviders() {
	if m.backend == nil { return }
	state := m.backend.GetLocalAgentState().AI
	m.providers = append([]domainai.ProviderDescriptor(nil), state.Providers...)
	if m.providerIndex >= len(m.providers) { m.providerIndex = 0 }
}

func (m *Model) loadHistory() {
	if m.backend == nil { return }
	state := m.backend.GetLocalAgentState().AI
	m.messages = make([]chatMessage, 0, len(state.AgentMessages))
	m.transcript = nil
	for _, message := range state.AgentMessages {
		m.messages = append(m.messages, chatMessage{role:message.Role, content:message.Content})
		m.transcript = append(m.transcript, transcriptEntry{role:message.Role, content:message.Content})
	}
	m.historyLoaded = true
}

func (m *Model) refreshPersistedMessages() {
	if m.backend == nil { return }
	state := m.backend.GetLocalAgentState().AI
	m.messages = make([]chatMessage, 0, len(state.AgentMessages))
	for _, message := range state.AgentMessages {
		m.messages = append(m.messages, chatMessage{role:message.Role, content:message.Content})
	}
}

func (m *Model) appendNewPersistedMessages() {
	if m.backend == nil { return }
	state := m.backend.GetLocalAgentState().AI
	start := len(m.messages)
	if start > len(state.AgentMessages) { start = len(state.AgentMessages) }
	for _, message := range state.AgentMessages[start:] {
		duplicate := false
		for i := len(m.transcript)-1; i >= 0; i-- {
			if m.transcript[i].isTool { continue }
			duplicate = m.transcript[i].role == message.Role && m.transcript[i].content == message.Content
			break
		}
		if duplicate { continue }
		m.transcript = append(m.transcript, transcriptEntry{role:message.Role, content:message.Content})
	}
	m.refreshPersistedMessages()
}

func (m *Model) restorePendingApproval() {
	if m.backend == nil {
		return
	}
	state := m.backend.GetLocalAgentState().AI
	pending := state.PendingNativeToolCall
	if pending == nil || pending.Target != "local" {
		return
	}
	for _, request := range state.CommandPolicy.PendingRequests {
		if request.ID != pending.RequestID || request.Target != "local" {
			continue
		}
		if pending.UserMessage != "" {
			hasUserMessage := false
			for i := len(m.transcript)-1; i >= 0; i-- {
				if m.transcript[i].isTool { continue }
				if m.transcript[i].role == "user" && m.transcript[i].content == pending.UserMessage { hasUserMessage = true }
				break
			}
			if !hasUserMessage { m.transcript = append(m.transcript, transcriptEntry{role:"user", content:pending.UserMessage}) }
		}
		m.approval = &agentai.ApprovalRequest{
			RequestID: request.ID,
			ToolID:    request.ToolID,
			Target:    request.Target,
			Command:   request.Command,
			Reason:    request.Reason,
			Preview:   appservice.LocalApprovalPreview(pending.ToolName, pending.ToolArguments),
		}
		hasTranscriptEntry := false
		for i := len(m.transcript)-1; i >= 0; i-- {
			if m.transcript[i].isTool && strings.Contains(m.transcript[i].output, "approval_required") {
				hasTranscriptEntry = true
				break
			}
		}
		if !hasTranscriptEntry {
			output, _ := json.Marshal(map[string]string{"status":"approval_required","operation":pending.ToolName,"command":request.Command})
			m.transcript = append(m.transcript, transcriptEntry{isTool:true, tool:pending.ToolName, status:"approval_required", output:prettyToolOutput(string(output))})
		}
		m.status = "A local operation is waiting for your approval."
		return
	}
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case agentEventMessage:
		m.handleAgentEvent(msg.event)
		return m, waitAgentEvent(msg.events)
	case agentStreamClosed:
		if m.running {
			m.running = false
			m.currentPrompt = ""
			m.status = "Agent stream closed unexpectedly."
		}
	case localOperationEvent:
		m.handleLocalOperation(msg.payload)
	case secureStorageUnlocked:
		m.refreshProviders()
		if !m.historyLoaded { m.loadHistory() } else { m.refreshPersistedMessages() }
		m.restorePendingApproval()
		m.screen = msg.resume
		if m.screen == screenUnavailable { m.screen = screenChat }
		if msg.created { m.status = "Master password created. Secure storage unlocked." } else { m.status = "Secure storage unlocked." }
		if !m.hasConfiguredSelectedProvider() { m.status += " Configure an AI provider with /provider." }
		if m.retryApproval {
			mode := m.retryApprovalMode
			m.retryApproval = false
			return m, m.resolveApproval(mode)
		}
		if m.retryPrompt != "" {
			prompt, appendUser := m.retryPrompt, m.retryPromptNeedsAppend
			m.retryPrompt, m.retryPromptNeedsAppend = "", false
			m.input = ""
			return m, m.startAgent(prompt, appendUser)
		}
	case secureStorageUnlockError:
		m.screen = screenUnlockPassword
		if msg.creating { m.screen = screenSetupPassword }
		m.resumeScreen = msg.resume
		m.status = fmt.Sprintf("Master password could not be accepted: %v", msg.err)
	case providerSelected:
		m.refreshProviders()
		m.openProviderConfig(msg.provider)
		m.status = "Selected " + msg.provider.Name + "."
	case agentApprovalDone:
		m.resolving = false
		m.retryApproval = false
		m.approval = nil
		if len(m.transcript) > 0 && m.transcript[len(m.transcript)-1].isTool {
			entry := &m.transcript[len(m.transcript)-1]
			if entry.status == "approval_required" || entry.status == "running" {
				if msg.mode == domainai.CommandPermissionModeDeny { entry.status = "denied" } else { entry.status = "finished" }
			}
		}
		m.appendNewPersistedMessages()
		m.restorePendingApproval()
		lastToolStatus := ""
		for i := len(m.transcript)-1; i >= 0; i-- {
			if m.transcript[i].isTool { lastToolStatus = m.transcript[i].status; break }
		}
		switch {
		case m.approval != nil:
			m.status = "A subsequent local operation is waiting for your approval."
		case lastToolStatus == "denied":
			m.status = "The local operation was denied and was not executed."
		case lastToolStatus == "execution_failed":
			m.status = "The approved local operation failed."
		default:
			m.status = fmt.Sprintf("Approval %s completed.", msg.mode)
		}
	case agentApprovalError:
		m.resolving = false
		if errors.Is(msg.err, securestorage.ErrMasterPasswordRequired) {
			m.retryApproval = true
			m.retryApprovalMode = msg.mode
			m.requestUnlock(screenChat, "Secure storage locked. Unlock it to resume the approved operation.")
		} else {
			m.status = fmt.Sprintf("Approval failed: %v", msg.err)
			m.transcript = append(m.transcript, transcriptEntry{role:"system", content:m.status})
		}
	case providerActionDone:
		m.refreshProviders()
		m.status = msg.message
		if msg.closeConfig { m.cloudToken = ""; m.screen = screenChat }
	case providerActionError:
		m.status = fmt.Sprintf("Provider operation failed: %v", msg.err)
		if errors.Is(msg.err, securestorage.ErrMasterPasswordRequired) {
			m.requestUnlock(m.screen, "Secure storage locked. Unlock it to continue provider configuration.")
		}
	case providerModelsDone:
		m.modelChoices = append([]string(nil), msg.models...)
		if len(msg.models) == 0 {
			m.status = "The provider returned no models."
		} else {
			m.status = "Available models: " + strings.Join(msg.models, ", ")
		}
	case providerModelsError:
		m.status = fmt.Sprintf("Unable to load models: %v", msg.err)
		if errors.Is(msg.err, securestorage.ErrMasterPasswordRequired) {
			m.requestUnlock(screenCloudConfig, "Secure storage locked. Unlock it to load models.")
		}
	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC || (msg.Type == tea.KeyRunes && len(msg.Runes) == 1 && msg.Runes[0] == 3) {
			if m.cancel != nil { m.cancel() }
			return m, tea.Quit
		}
		return m.updateKey(msg)
	}
	return m, nil
}

func waitAgentEvent(events <-chan agentai.Event) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-events
		if !ok {
			return agentStreamClosed{}
		}
		return agentEventMessage{event: event, events: events}
	}
}

func (m *Model) handleLocalOperation(payload map[string]any) {
	encoded, err := json.MarshalIndent(payload, "", "  ")
	if err != nil { return }
	command, _ := payload["command"].(string)
	tool := strings.TrimSpace(strings.SplitN(command, " ", 2)[0])
	status, _ := payload["status"].(string)

	// The service emits this event for every local operation, including policy
	// denials and operations that did not need approval. Update the tool call
	// already shown in the transcript instead of silently dropping the event or
	// creating a duplicate entry.
	for i := len(m.transcript)-1; i >= 0; i-- {
		entry := &m.transcript[i]
		if !entry.isTool { continue }
		if tool != "" && entry.tool != tool { continue }
		if entry.status != "running" && entry.status != "approval_required" && !strings.Contains(entry.output, "approval_required") { continue }
		if tool != "" { entry.tool = tool }
		entry.status = status
		entry.output = truncateDisplay(string(encoded), 2400)
		m.scrollOffset = 0
		if statusText := localOperationStatus(status); statusText != "" { m.status = statusText }
		return
	}
	m.transcript = append(m.transcript, transcriptEntry{isTool:true, tool:tool, status:status, output:truncateDisplay(string(encoded),2400)})
	m.scrollOffset = 0
	m.status = localOperationStatus(status)
}

func localOperationStatus(status string) string {
	switch status {
	case "executed": return "Local operation completed."
	case "execution_failed": return "Local operation failed."
	case "denied": return "Local operation was denied and not executed."
	default: return ""
	}
}

func (m *Model) handleAgentEvent(event agentai.Event) {
	switch event.Type {
	case agentai.EventToolStarted:
		m.transcript = append(m.transcript, transcriptEntry{isTool:true, tool:event.Tool, status:"running"})
		m.scrollOffset = 0
		if len(m.transcript) > 100 { m.transcript = append([]transcriptEntry(nil), m.transcript[len(m.transcript)-100:]...) }
	case agentai.EventToolOutput:
		if len(m.transcript) > 0 && m.transcript[len(m.transcript)-1].isTool {
			entry := &m.transcript[len(m.transcript)-1]
			entry.output = truncateDisplay(prettyToolOutput(event.Content), 2400)
			if status := nativeToolResultStatus(event.Content); status != "" { entry.status = status }
		}
	case agentai.EventToolFinished:
		if len(m.transcript) > 0 && m.transcript[len(m.transcript)-1].isTool {
			entry := &m.transcript[len(m.transcript)-1]
			if entry.status == "running" { entry.status = "finished" }
			if event.Tool != "" { entry.tool = event.Tool }
		}
	case agentai.EventApprovalRequired:
		if event.Approval != nil {
			request := *event.Approval
			m.approval = &request
			m.scrollOffset = 0
			m.status = "Approval required. Choose [Y] allow once, [A] always allow, or [N] deny."
		}
	case agentai.EventTextDelta:
		if event.Content != "" {
			if len(m.transcript) > 0 && !m.transcript[len(m.transcript)-1].isTool && m.transcript[len(m.transcript)-1].role == "assistant" { m.transcript[len(m.transcript)-1].content += event.Content } else { m.transcript = append(m.transcript, transcriptEntry{role:"assistant", content:event.Content}) }
			m.scrollOffset = 0
		}
	case agentai.EventMessageFinished:
		m.running = false
		m.currentPrompt = ""
		if m.approval == nil {
			m.refreshProviders()
			m.refreshPersistedMessages()
			m.status = "Ready."
		}
	case agentai.EventError:
		m.running = false
		if errors.Is(event.Err, securestorage.ErrMasterPasswordRequired) {
			m.retryPrompt = m.currentPrompt
			m.retryPromptNeedsAppend = false
			m.requestUnlock(screenChat, "Secure storage locked. Enter your master password to continue.")
			return
		}
		if event.Err != nil {
			m.currentPrompt = ""
			m.status = event.Err.Error()
			m.transcript = append(m.transcript, transcriptEntry{role:"system", content:event.Err.Error()})
		} else {
			m.currentPrompt = ""
			m.status = "The agent returned an unknown error."
		}
	case agentai.EventCancellation:
		m.running = false
		m.currentPrompt = ""
		m.status = "Request cancelled."
	}
}

func nativeToolResultStatus(value string) string {
	var result struct { Status string `json:"status"` }
	if json.Unmarshal([]byte(value), &result) != nil { return "" }
	return result.Status
}

func prettyToolOutput(value string) string {
	var compact any
	if json.Unmarshal([]byte(value), &compact) != nil {
		return value
	}
	encoded, err := json.MarshalIndent(compact, "", "  ")
	if err != nil {
		return value
	}
	return string(encoded)
}

func (m *Model) requestUnlock(resume screen, message string) {
	m.resumeScreen = resume
	m.screen = screenUnlockPassword
	m.password = ""
	m.confirm = ""
	m.status = message
}

func (m *Model) updateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.screen {
	case screenUnavailable:
		if msg.Type == tea.KeyEsc || msg.Type == tea.KeyEnter {
			return m, tea.Quit
		}
		return m, nil
	case screenSetupPassword, screenConfirmPassword, screenUnlockPassword:
		return m.updatePasswordKey(msg)
	case screenProviderMenu:
		return m.updateProviderMenuKey(msg)
	case screenCloudConfig:
		return m.updateCloudConfigKey(msg)
	case screenLocalConfig:
		return m.updateLocalConfigKey(msg)
	case screenChat:
		return m.updateChatKey(msg)
	default:
		return m, nil
	}
}

func (m *Model) updatePasswordKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyEsc {
		if m.screen == screenUnlockPassword {
			return m, tea.Quit
		}
		m.password, m.confirm = "", ""
		m.screen = screenSetupPassword
		return m, nil
	}
	if msg.Type == tea.KeyBackspace {
		if m.screen == screenConfirmPassword {
			m.confirm = removeLastRune(m.confirm)
		} else {
			m.password = removeLastRune(m.password)
		}
		return m, nil
	}
	if msg.Type == tea.KeyEnter {
		switch m.screen {
		case screenSetupPassword:
			if strings.TrimSpace(m.password) == "" {
				m.status = "Master password cannot be empty."
				return m, nil
			}
			m.screen = screenConfirmPassword
			m.status = "Confirm the master password."
		case screenConfirmPassword:
			if m.password != m.confirm {
				m.confirm = ""
				m.status = "Passwords do not match. Confirm again."
				return m, nil
			}
			return m, m.unlockOrCreate(m.password)
		case screenUnlockPassword:
			if m.password == "" {
				m.status = "Enter your master password."
				return m, nil
			}
			return m, m.unlockOrCreate(m.password)
		}
		return m, nil
	}
	if msg.Type == tea.KeyRunes {
		if m.screen == screenConfirmPassword {
			m.confirm += string(msg.Runes)
		} else {
			m.password += string(msg.Runes)
		}
	}
	return m, nil
}

func (m *Model) unlockOrCreate(password string) tea.Cmd {
	backend := m.backend
	resume := m.resumeScreen
	creating := m.screen == screenConfirmPassword
	m.password, m.confirm = "", ""
	return func() tea.Msg {
		if err := backend.EnsureMasterPassword(password); err != nil {
			return secureStorageUnlockError{err: err, resume: resume, creating: creating}
		}
		return secureStorageUnlocked{resume: resume, created: creating}
	}
}

type secureStorageUnlocked struct {
	resume  screen
	created bool
}

func (m *Model) updateProviderMenuKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyEsc {
		m.screen = screenChat
		return m, nil
	}
	if msg.Type == tea.KeyUp {
		if m.providerIndex > 0 { m.providerIndex-- }
		return m, nil
	}
	if msg.Type == tea.KeyDown {
		if m.providerIndex+1 < len(m.providers) { m.providerIndex++ }
		return m, nil
	}
	if msg.Type != tea.KeyEnter || len(m.providers) == 0 {
		return m, nil
	}
	provider := m.providers[m.providerIndex]
	return m, m.selectedProvider(provider)
}

func (m *Model) selectedProvider(provider domainai.ProviderDescriptor) tea.Cmd {
	if m.backend == nil { return nil }
	if !m.backend.GetSecureStorageStatus().Unlocked {
		m.requestUnlock(screenProviderMenu, "Unlock secure storage before changing providers.")
		return nil
	}
	backend := m.backend
	m.status = "Selecting AI provider..."
	return func() tea.Msg {
		if err := backend.SelectAIProvider(provider.ID); err != nil { return providerActionError{err: err} }
		return providerSelected{provider: provider}
	}
}

func providerIndex(providers []domainai.ProviderDescriptor, id string) int {
	for i := range providers { if providers[i].ID == id { return i } }
	return 0
}

func (m *Model) updateCloudConfigKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyEsc {
		m.screen = screenChat
		return m, nil
	}
	if msg.Type == tea.KeyCtrlS || (msg.Type == tea.KeyEnter && m.configField == 2) {
		return m, m.saveCloudConfig()
	}
	if msg.Type == tea.KeyTab || msg.Type == tea.KeyEnter || msg.Type == tea.KeyDown {
		m.configField = (m.configField + 1) % 3
		return m, nil
	}
	if msg.Type == tea.KeyUp {
		m.configField = (m.configField + 2) % 3
		return m, nil
	}
	if msg.Type == tea.KeyCtrlL {
		return m, m.loadModels()
	}
	if msg.Type == tea.KeyBackspace {
		switch m.configField {
		case 0: m.cloudModel = removeLastRune(m.cloudModel)
		case 1: m.cloudEndpoint = removeLastRune(m.cloudEndpoint)
		case 2: m.cloudToken = removeLastRune(m.cloudToken)
		}
		return m, nil
	}
	if msg.Type == tea.KeyRunes {
		switch m.configField {
		case 0: m.cloudModel += string(msg.Runes)
		case 1: m.cloudEndpoint += string(msg.Runes)
		case 2: m.cloudToken += string(msg.Runes)
		}
	}
	return m, nil
}

func (m *Model) updateLocalConfigKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyEsc {
		m.screen = screenChat
		return m, nil
	}
	switch msg.Type {
	case tea.KeyCtrlS, tea.KeyEnter:
		return m, m.saveLocalConfig()
	case tea.KeyCtrlD:
		return m, m.downloadLocalModel()
	case tea.KeyCtrlR:
		return m, m.toggleLocalModel()
	case tea.KeyBackspace:
		m.localURL = removeLastRune(m.localURL)
	case tea.KeyRunes:
		m.localURL += string(msg.Runes)
	}
	return m, nil
}

func (m *Model) updateChatKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyPgUp {
		m.scrollOffset += m.transcriptPageSize()
		return m, nil
	}
	if msg.Type == tea.KeyPgDown {
		m.scrollOffset -= m.transcriptPageSize()
		if m.scrollOffset < 0 { m.scrollOffset = 0 }
		return m, nil
	}
	if msg.Type == tea.KeyHome && msg.Alt {
		m.scrollOffset = m.maxTranscriptScroll()
		return m, nil
	}
	if msg.Type == tea.KeyEnd && msg.Alt {
		m.scrollOffset = 0
		return m, nil
	}
	if m.approval != nil {
		if m.resolving { return m, nil }
		if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 {
			switch msg.Runes[0] {
			case 'y', 'Y': return m, m.resolveApproval(domainai.CommandPermissionModeNow)
			case 'a', 'A': return m, m.resolveApproval(domainai.CommandPermissionModeAlways)
			case 'n', 'N': return m, m.resolveApproval(domainai.CommandPermissionModeDeny)
			}
		}
		return m, nil
	}
	if m.running {
		return m, nil
	}
	if msg.Type == tea.KeyEsc {
		m.input = ""
		m.cursor = 0
		return m, nil
	}
	if msg.Type == tea.KeyCtrlJ {
			m.input = insertAtRune(m.input, m.cursor, "\n")
			m.cursor++
			return m, nil
		}
		if msg.Type == tea.KeyEnter {
			value := strings.TrimSpace(m.input)
			m.input = ""
			m.cursor = 0
		if value == "" { return m, nil }
		if strings.HasPrefix(value, "/") { return m, m.handleSlashCommand(value) }
		if !m.hasConfiguredSelectedProvider() {
			m.status = "No configured AI provider. Use /provider first."
			return m, nil
		}
		if !m.backend.GetSecureStorageStatus().Unlocked {
			m.retryPrompt = value
			m.retryPromptNeedsAppend = true
			m.requestUnlock(screenChat, "Secure storage locked. Unlock it before sending a message.")
			return m, nil
		}
		return m, m.startAgent(value, true)
	}
	switch msg.Type {
	case tea.KeyBackspace:
		m.input, m.cursor = deleteBeforeRune(m.input, m.cursor)
	case tea.KeyDelete:
		m.input = deleteAtRune(m.input, m.cursor)
	case tea.KeyLeft:
		if m.cursor > 0 { m.cursor-- }
	case tea.KeyRight:
		if m.cursor < len([]rune(m.input)) { m.cursor++ }
	case tea.KeyHome:
		m.cursor = 0
	case tea.KeyEnd:
		m.cursor = len([]rune(m.input))
	case tea.KeyRunes:
		value := string(msg.Runes)
		m.input = insertAtRune(m.input, m.cursor, value)
		m.cursor += len([]rune(value))
	}
	return m, nil
}

func (m *Model) startAgent(value string, appendUser bool) tea.Cmd {
	if !m.hasConfiguredSelectedProvider() {
		m.status = "No configured AI provider. Use /provider first."
		m.input = value
		return nil
	}
	if m.agent == nil {
		m.status = "Local agent is unavailable."
		return nil
	}
	if appendUser { m.transcript = append(m.transcript, transcriptEntry{role:"user", content:value}) }
	m.currentPrompt = value
	m.running = true
	m.status = "Thinking..."
	events, err := m.agent.Run(m.ctx, "", value)
	if err != nil {
		m.running = false
		if errors.Is(err, securestorage.ErrMasterPasswordRequired) {
			m.retryPrompt = value
			m.retryPromptNeedsAppend = false
			m.requestUnlock(screenChat, "Secure storage locked. Enter your master password to continue.")
			return nil
		}
		m.currentPrompt = ""
		m.status = err.Error()
		m.transcript = append(m.transcript, transcriptEntry{role:"system", content:err.Error()})
		return nil
	}
	return waitAgentEvent(events)
}

func (m *Model) handleSlashCommand(value string) tea.Cmd {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "/help":
		m.transcript = append(m.transcript, transcriptEntry{role:"system", content:"Commands: /provider — configure an AI provider; /help — show help; /quit — exit."})
	case "/provider":
		m.refreshProviders()
		m.screen = screenProviderMenu
		m.status = "Choose an AI provider with ↑/↓ and Enter."
	case "/quit", "/exit":
		return tea.Quit
	default:
		m.status = "Unknown command. Type /help."
	}
	return nil
}

func (m *Model) resolveApproval(mode domainai.CommandPermissionMode) tea.Cmd {
	if m.approval == nil || m.resolving { return nil }
	if mode != domainai.CommandPermissionModeDeny && !m.backend.GetSecureStorageStatus().Unlocked {
		m.retryApproval = true
		m.retryApprovalMode = mode
		m.requestUnlock(screenChat, "Secure storage locked. Unlock it before approving a local operation.")
		return nil
	}
	requestID := m.approval.RequestID
	backend := m.backend
	m.resolving = true
	m.status = "Applying approval and resuming the agent..."
	return func() tea.Msg {
		if err := backend.ResolveCommandPolicyRequest(requestID, mode); err != nil {
			return agentApprovalError{err: err, mode:mode}
		}
		return agentApprovalDone{mode: mode}
	}
}

func (m *Model) saveCloudConfig() tea.Cmd {
	if !m.backend.GetSecureStorageStatus().Unlocked {
		m.requestUnlock(screenCloudConfig, "Unlock secure storage before saving provider credentials.")
		return nil
	}
	model, endpoint, token := strings.TrimSpace(m.cloudModel), strings.TrimSpace(m.cloudEndpoint), m.cloudToken
	if model == "" || endpoint == "" {
		m.status = "Model and endpoint are required."
		return nil
	}
	backend := m.backend
	return func() tea.Msg {
		if err := backend.SaveCloudProvider(model, endpoint, token); err != nil { return providerActionError{err: err} }
		return providerActionDone{message: "Cloud AI provider saved and selected.", closeConfig: true}
	}
}

func (m *Model) saveLocalConfig() tea.Cmd {
	downloadURL := strings.TrimSpace(m.localURL)
	if downloadURL == "" { m.status = "Model download URL is required."; return nil }
	backend := m.backend
	return func() tea.Msg {
		if err := backend.SaveLocalProvider(downloadURL); err != nil { return providerActionError{err: err} }
		return providerActionDone{message: "Local AI provider saved and selected."}
	}
}

func (m *Model) downloadLocalModel() tea.Cmd {
	if !m.backend.GetSecureStorageStatus().Unlocked {
		m.requestUnlock(screenLocalConfig, "Unlock secure storage before downloading the local model.")
		return nil
	}
	backend, downloadURL := m.backend, strings.TrimSpace(m.localURL)
	if downloadURL == "" { m.status = "Model download URL is required."; return nil }
	m.status = "Downloading local model..."
	return func() tea.Msg {
		if err := backend.DownloadLocalModel(downloadURL); err != nil { return providerActionError{err: err} }
		return providerActionDone{message: "Local model downloaded."}
	}
}

func (m *Model) toggleLocalModel() tea.Cmd {
	if !m.backend.GetSecureStorageStatus().Unlocked {
		m.requestUnlock(screenLocalConfig, "Unlock secure storage before starting the local model.")
		return nil
	}
	provider := m.currentProvider()
	if provider == nil { return nil }
	backend := m.backend
	start := provider.Status != "running"
	m.status = "Updating local model process..."
	return func() tea.Msg {
		var err error
		if start { err = backend.StartLocalModel() } else { err = backend.StopLocalModel() }
		if err != nil { return providerActionError{err: err} }
		if start { return providerActionDone{message: "Local model started."} }
		return providerActionDone{message: "Local model stopped."}
	}
}

func (m *Model) loadModels() tea.Cmd {
	if !m.backend.GetSecureStorageStatus().Unlocked {
		m.requestUnlock(screenCloudConfig, "Unlock secure storage before loading model names.")
		return nil
	}
	endpoint, token := strings.TrimSpace(m.cloudEndpoint), m.cloudToken
	backend := m.backend
	return func() tea.Msg {
		models, err := backend.ListCloudModels(endpoint, token)
		if err != nil { return providerModelsError{err: err} }
		return providerModelsDone{models: models}
	}
}

func (m Model) hasConfiguredSelectedProvider() bool {
	for _, provider := range m.providers { if provider.Selected && provider.Configured { return true } }
	return false
}

func (m *Model) currentProvider() *domainai.ProviderDescriptor {
	if m.providerIndex < 0 || m.providerIndex >= len(m.providers) { return nil }
	return &m.providers[m.providerIndex]
}

func (m Model) View() string {
	width := m.width
	if width < 40 { width = 40 }
	if width > 120 { width = 120 }
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81"))
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	panel := lipgloss.NewStyle().Width(width-4).Padding(1, 2).Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("240"))
	var body strings.Builder
	body.WriteString(title.Render(safeTerminalText("EIKSY  LOCAL AGENT")))
	body.WriteString("\n")
	body.WriteString(muted.Render("Local host · current OS user · no SSH or SFTP"))
	body.WriteString("\n\n")

	switch m.screen {
	case screenUnavailable:
		body.WriteString("Secure storage is unavailable. The local agent cannot start safely.")
	case screenSetupPassword:
		body.WriteString("Create a master password for encrypted provider credentials.\n\n")
		body.WriteString("Master password: " + masked(m.password) + "▌\n")
		body.WriteString("\nEnter continue · Esc exit")
	case screenConfirmPassword:
		body.WriteString("Confirm master password: " + masked(m.confirm) + "▌\n")
		body.WriteString("\nEnter confirm · Esc back")
	case screenUnlockPassword:
		body.WriteString("Master password: " + masked(m.password) + "▌\n")
		body.WriteString("\nEnter unlock · Esc exit")
	case screenProviderMenu:
		body.WriteString("AI PROVIDERS\n\n")
		for i, provider := range m.providers {
			marker := "  "
			if i == m.providerIndex { marker = "› " }
			selected := ""
			if provider.Selected { selected = "  *" }
			fmt.Fprintf(&body, "%s%s%s  [%s]  %s\n", marker, safeTerminalLine(provider.Name), selected, safeTerminalLine(provider.Status), safeTerminalLine(provider.Model))
		}
		body.WriteString("\n↑/↓ select · Enter configure · Esc back")
	case screenCloudConfig:
		body.WriteString("OPENAI-COMPATIBLE PROVIDER\n\n")
		fmt.Fprintf(&body, "%sModel:    %s\n", fieldMarker(m.configField, 0), safeTerminalLine(m.cloudModel))
		fmt.Fprintf(&body, "%sEndpoint: %s\n", fieldMarker(m.configField, 1), safeTerminalLine(m.cloudEndpoint))
		fmt.Fprintf(&body, "%sAPI token: %s\n", fieldMarker(m.configField, 2), masked(m.cloudToken))
		body.WriteString("\nTab/↑/↓ change field · Ctrl+L load models · Ctrl+S save · Esc cancel")
		if len(m.modelChoices) > 0 {
			body.WriteString("\n\nModels: " + safeTerminalLine(strings.Join(m.modelChoices, ", ")))
		}
	case screenLocalConfig:
		provider := m.currentProvider()
		status := "not configured"
		if provider != nil { status = provider.Status }
		body.WriteString("LOCAL MODEL\n\n")
		fmt.Fprintf(&body, "Status: %s\nDownload URL: %s\n", safeTerminalLine(status), safeTerminalLine(m.localURL))
		body.WriteString("\nCtrl+S save · Ctrl+D download · Ctrl+R start/stop · Esc cancel")
	case screenChat:
		if len(m.transcript) == 0 {
			body.WriteString("Start a conversation with your local-host agent.\n")
			body.WriteString("Use /provider to configure an OpenAI-compatible or local model.\n")
		} else {
			lines := m.renderTranscriptLines(title)
			visible := m.transcriptPageSize()
			offset := m.scrollOffset
			maxOffset := len(lines) - visible
			if maxOffset < 0 { maxOffset = 0 }
			if offset > maxOffset { offset = maxOffset }
			end := len(lines) - offset
			start := end - visible
			if start < 0 { start = 0 }
			for _, line := range lines[start:end] { body.WriteString(line + "\n") }
			if maxOffset > 0 {
				if offset == 0 { body.WriteString(muted.Render("↓ latest · PgUp/PgDown scroll · Alt+Home/End jump") + "\n") } else { body.WriteString(muted.Render(fmt.Sprintf("↑ older · %d lines from latest · PgUp/PgDown scroll", offset)) + "\n") }
			}
		}
		if m.approval != nil {
			body.WriteString("\n")
			body.WriteString(title.Render("APPROVAL REQUIRED"))
			fmt.Fprintf(&body, "\nTool: %s\nOperation: %s\n", safeTerminalLine(m.approval.ToolID), safeTerminalLine(m.approval.Command))
			if m.approval.Reason != "" { fmt.Fprintf(&body, "Reason: %s\n", safeTerminalLine(m.approval.Reason)) }
			if m.approval.Preview != "" { fmt.Fprintf(&body, "Details:\n%s\n", safeTerminalText(m.approval.Preview)) }
			body.WriteString("\n[Y] allow once   [A] always allow exact operation   [N] deny")
		}
		if m.running { body.WriteString("\n\nAgent is working...") }
		body.WriteString("\n\n> " + safeTerminalText(insertAtRune(m.input, m.cursor, "▌")))
	}
	body.WriteString("\n\n")
	body.WriteString(muted.Render(safeTerminalLine(m.status)))
	footer := muted.Render("\nEnter send/confirm · /provider configure model · /help commands · Ctrl+C exit")
	return panel.Render(body.String()+footer)
}

func (m *Model) openProviderConfig(provider domainai.ProviderDescriptor) {
	m.providerIndex = providerIndex(m.providers, provider.ID)
	m.modelChoices = nil
	if provider.Class == domainai.ProviderClassLocalOpenAI {
		m.localURL = provider.DownloadURL
		m.screen = screenLocalConfig
		return
	}
	m.cloudModel = provider.Model
	m.cloudEndpoint = provider.Endpoint
	m.cloudToken = ""
	m.configField = 0
	m.screen = screenCloudConfig
}


func safeTerminalText(value string) string {
	var builder strings.Builder
	for _, r := range value {
		switch r {
		case '\n', '\t':
			builder.WriteRune(r)
		case '\r':
			builder.WriteRune('\n')
		default:
			if !unicode.IsControl(r) { builder.WriteRune(r) }
		}
	}
	return builder.String()
}

func safeTerminalLine(value string) string {
	value = safeTerminalText(value)
	value = strings.ReplaceAll(value, "\n", "\\n")
	value = strings.ReplaceAll(value, "\t", "\\t")
	return value
}

func truncateDisplay(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit { return value }
	return string(runes[:limit]) + "\n[display truncated]"
}

func (m Model) transcriptPageSize() int {
	if m.height <= 0 { return 18 }
	size := m.height - 13
	if size < 3 { return 3 }
	return size
}

func (m Model) maxTranscriptScroll() int {
	lines := m.renderTranscriptLines(lipgloss.NewStyle().Bold(true))
	max := len(lines) - m.transcriptPageSize()
	if max < 0 { return 0 }
	return max
}

func (m Model) renderTranscriptLines(title lipgloss.Style) []string {
	var rendered strings.Builder
	for _, entry := range m.transcript {
		if entry.isTool {
			fmt.Fprintf(&rendered, "[%s] %s\n", safeTerminalLine(entry.tool), safeTerminalLine(entry.status))
			if entry.output != "" { fmt.Fprintf(&rendered, "%s\n", safeTerminalText(entry.output)) }
			rendered.WriteString("\n")
			continue
		}
		role := strings.ToUpper(entry.role)
		if role == "ASSISTANT" { role = "EIKSY" }
		if role == "USER" { role = "YOU" }
		fmt.Fprintf(&rendered, "%s\n%s\n\n", title.Render(safeTerminalText(role)), safeTerminalText(entry.content))
	}
	return strings.Split(strings.TrimRight(rendered.String(), "\n"), "\n")
}

func insertAtRune(value string, index int, insert string) string {
	runes := []rune(value)
	if index < 0 { index = 0 }
	if index > len(runes) { index = len(runes) }
	return string(runes[:index]) + insert + string(runes[index:])
}

func deleteBeforeRune(value string, index int) (string, int) {
	runes := []rune(value)
	if index <= 0 || len(runes) == 0 { return value, 0 }
	if index > len(runes) { index = len(runes) }
	return string(runes[:index-1]) + string(runes[index:]), index-1
}

func deleteAtRune(value string, index int) string {
	runes := []rune(value)
	if index < 0 || index >= len(runes) { return value }
	return string(runes[:index]) + string(runes[index+1:])
}

func masked(value string) string { return strings.Repeat("•", len([]rune(value))) }
func fieldMarker(active, field int) string { if active == field { return "› " }; return "  " }
func removeLastRune(value string) string {
	runes := []rune(value)
	if len(runes) == 0 { return value }
	return string(runes[:len(runes)-1])
}
