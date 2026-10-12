package tui

import (
	"context"
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"

	agentai "eiksy/internal/ai"
	appservice "eiksy/internal/app"
	domainai "eiksy/internal/domain/ai"
	domainsettings "eiksy/internal/domain/settings"
	securestorage "eiksy/internal/securestorage"
	sftpdomain "eiksy/internal/domain/sftp"
	domainsessions "eiksy/internal/domain/sessions"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type SessionRef struct {
	ID    string
	Title string
}

type Tab struct {
	Title   string
	Session *SessionRef
}

func (t *Tab) BindSession(sessionID, title string) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		t.Session = nil
		return
	}
	t.Session = &SessionRef{ID: sessionID, Title: strings.TrimSpace(title)}
}

func (t *Tab) UnbindSession() {
	t.Session = nil
}

type ChatMessage struct {
	Role    string
	Content string
}

type ToolCallView struct {
	Name   string
	Output string
	Status string
}

type SessionSelector interface {
	SelectChatSession(sessionID string) error
}

type SessionForker interface {
	ForkChatSession(sessionID, title string) (domainai.ChatSession, error)
}

type ApprovalResolver interface {
	ResolveApproval(requestID, mode string) error
}

type AIBackend interface {
	SendChatMessage(message, activeSessionID string) error
	GetShellState() appservice.ShellState
}

type AIChatResetBackend interface {
	ClearChat() error
}

type SessionProfileBackend interface {
	ListSessionProfiles() []domainsessions.Profile
	CreateSessionProfileInput(domainsessions.ProfileInput) error
	DeleteSessionProfile(string) error
}

type RuntimeSessionBackend interface {
	LaunchSession(string) (appservice.RuntimeSessionView, error)
	ConnectSession(string) error
	ReconnectSession(string) error
	DisconnectSession(string) error
	CloseSession(string) error
	SendSSHInput(string, string) error
	AcceptSSHHostKey(string) error
	ListSFTPFiles(string, string) ([]sftpdomain.FileEntry, error)
	NavigateSFTP(string, string) ([]sftpdomain.FileEntry, error)
	ReadSFTPFile(string, string) (string, error)
}

type SFTPWriteBackend interface {
	SaveSFTPFile(string, string, string) error
}

type SFTPUploadBackend interface {
	SelectUploadFiles() ([]string, error)
	UploadSFTPFiles(string, string, []string) error
}

type SFTPDownloadBackend interface {
	SelectDownloadDirectory() (string, error)
	DownloadSFTPFiles(string, string, []string) error
}

type CommandPolicyBackend interface { UpdateCommandPolicy(domainai.CommandPolicy) error }
type CommandApprovalBackend interface { ResolveCommandPolicyRequest(requestID, mode string) error }

type SecureStorageBackend interface { GetSecureStorageStatus() securestorage.Status; EnsureMasterPassword(string) error; LockSecureStorage() }

type portForwardForm struct { field int; editIndex int; localPort, remoteHost, remotePort, hostID string }
func newPortForwardForm() *portForwardForm { return &portForwardForm{editIndex: -1} }
func editPortForwardForm(rule domainsettings.PortForwardRule, index int) *portForwardForm {
	return &portForwardForm{editIndex:index, localPort:rule.LocalPort, remoteHost:rule.RemoteHost, remotePort:rule.RemotePort, hostID:rule.HostID}
}
func (f *portForwardForm) value() string { switch f.field { case 0:return f.localPort; case 1:return f.remoteHost; case 2:return f.remotePort; case 3:return f.hostID }; return "" }
func (f *portForwardForm) setValue(v string) { switch f.field { case 0:f.localPort=v; case 1:f.remoteHost=v; case 2:f.remotePort=v; case 3:f.hostID=v } }

 type sftpUploadDone struct{ count int }
type sftpUploadError struct{ err error }
type sftpDownloadDone struct{ path string }
type sftpDownloadError struct{ err error }

type SSHConfigImportBackend interface { ImportSSHConfig(string) ([]domainsessions.Profile, error) }

type Backend interface {
	ListChatSessions() []domainai.ChatSession
	CreateChatSession(title string) (domainai.ChatSession, error)
	SelectChatSession(sessionID string) error
	ForkChatSession(sessionID, title string) (domainai.ChatSession, error)
	GetSettings() domainsettings.AppSettings
	UpdateSettings(domainsettings.AppSettings) error
}

type Model struct {
	width     int
	height    int
	tabs      []Tab
	activeTab int
	messages  []ChatMessage
	toolCalls []ToolCallView
	input     string
	terminalLines []string
	terminalInput string
	sftpEntries []sftpdomain.FileEntry
	sftpPath string
	sftpSelected int
	fileContent string
	sftpEdit bool
	sftpEditPath string
	sftpEditLines []string
	sftpEditRow int
	sftpEditCol int
	sftpTransferStatus string
	activeView  string
	palette   *CommandPalette
	shortcuts bool
	approval  *agentai.ApprovalRequest

	sessions        []SessionRef
	activeSession   int
	sessionSelector SessionSelector
	sessionForker   SessionForker

	approvalResolver ApprovalResolver
	backend         Backend
	settings        domainsettings.AppSettings
	settingsIndex   int
	secureStorageBackend SecureStorageBackend
	secureStorageStatus securestorage.Status
	masterPasswordInput string
	masterPasswordPrompt bool
	profileBackend   SessionProfileBackend
	profiles         []domainsessions.Profile
	activeProfile    int
	profileForm      *sessionProfileForm
	portForwardForm  *portForwardForm
	sshConfigImport string
	sshConfigForm bool
	aiBackend        AIBackend
	aiProviderIndex  int
	aiToolIndex      int
	aiApprovalIndex  int
	aiProviderForm   *aiProviderConfigForm
	aiCloudModels    []string
	aiProviderStatus string
	aiCloudAuthSessionID string
	aiCloudAuthPending bool
	aiCloudAuthMessage string
	openBrowserURL func(string) error
	runtimeBackend   RuntimeSessionBackend
	runtimeSessions  map[string]appservice.RuntimeSessionView
	pendingHostKey   string
}

type sessionProfileForm struct {
	id string
	field int
	name string
	host string
	port string
	username string
	password string
}

func newSessionProfileForm() *sessionProfileForm { return &sessionProfileForm{port: "22"} }

func newSessionProfileEditForm(profile domainsessions.Profile) *sessionProfileForm {
	return &sessionProfileForm{id: profile.ID, name: profile.Name, host: profile.Host, port: strconv.Itoa(profile.Port), username: profile.Username}
}

func (f *sessionProfileForm) value() string {
	switch f.field {
	case 0: return f.name
	case 1: return f.host
	case 2: return f.port
	case 3: return f.username
	case 4: return f.password
	}
	return ""
}

func (f *sessionProfileForm) setValue(v string) {
	switch f.field {
	case 0: f.name = v
	case 1: f.host = v
	case 2: f.port = v
	case 3: f.username = v
	case 4: f.password = v
	}
}

type PaletteCommand struct {
	ID    string
	Title string
}

type CommandPalette struct {
	Query    string
	Selected int
}

var paletteCommands = []PaletteCommand{
	{ID: "next-tab", Title: "Next tab"},
	{ID: "previous-tab", Title: "Previous tab"},
	{ID: "next-session", Title: "Next session"},
	{ID: "previous-session", Title: "Previous session"},
	{ID: "fork-session", Title: "Fork active session"},
	{ID: "quit", Title: "Quit Eiksy"},
}

func NewModel() Model {
	return Model{
		openBrowserURL: openBrowserURL,
		tabs: []Tab{
			{Title: "Sessions"},
			{Title: "Terminal"},
			{Title: "Files"},
			{Title: "Tools"},
			{Title: "Settings"},
			{Title: "AI"},
		},
	}
}

// WithChatSessions provides application-owned session metadata to the TUI.
// Message bodies are intentionally not part of this presentation model.
func (m Model) WithBackend(backend Backend) Model {
	m.backend = backend
	if backend != nil {
		m.settings = backend.GetSettings()
		m.sessionSelector = backend
		m.sessionForker = backend
		if profileBackend, ok := backend.(SessionProfileBackend); ok { m.profileBackend = profileBackend }
		if aiBackend, ok := backend.(AIBackend); ok { m.aiBackend = aiBackend }
		if secureBackend, ok := backend.(SecureStorageBackend); ok { m.secureStorageBackend = secureBackend; m.secureStorageStatus = secureBackend.GetSecureStorageStatus() }
		if runtimeBackend, ok := backend.(RuntimeSessionBackend); ok {
			m.runtimeBackend = runtimeBackend
			if m.runtimeSessions == nil { m.runtimeSessions = make(map[string]appservice.RuntimeSessionView) }
		}
		m.refreshSessions()
		m.refreshProfiles()
	}
	return m
}

func (m Model) WithChatSessions(sessions []SessionRef) Model {
	m.sessions = append([]SessionRef(nil), sessions...)
	if m.activeSession >= len(m.sessions) {
		m.activeSession = 0
	}
	m.syncActiveSessionTab()
	return m
}

// WithTerminalSession binds a connected runtime session to the Terminal view.
func (m Model) WithTerminalSession(sessionID, title string) Model {
	if len(m.tabs) > 1 {
		m.tabs[1].BindSession(sessionID, title)
	}
	return m
}

func (m Model) hasTerminalSession() bool {
	return len(m.tabs) > 1 && m.tabs[1].Session != nil
}

func (m Model) runtimeSessionsForSession(sessionID string) (appservice.RuntimeSessionView, bool) {
	for _, view := range m.runtimeSessions {
		if view.ID == sessionID {
			return view, true
		}
	}
	return appservice.RuntimeSessionView{}, false
}

// WithSessionSelector connects session selection to the application service.
func (m Model) WithSessionSelector(selector SessionSelector) Model {
	m.sessionSelector = selector
	return m
}

// ActiveSession returns the currently highlighted chat session.
func (m Model) WithSessionForker(forker SessionForker) Model {
	m.sessionForker = forker
	return m
}

func (m Model) ActiveSession() *SessionRef {
	if m.activeSession < 0 || m.activeSession >= len(m.sessions) {
		return nil
	}
	session := m.sessions[m.activeSession]
	return &session
}

func (m Model) Init() tea.Cmd { return nil }

// WithApprovalResolver connects the TUI to the existing backend approval
// service without making the TUI responsible for policy or execution.
func (m Model) WithApprovalResolver(resolver ApprovalResolver) Model {
	m.approvalResolver = resolver
	return m
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case agentai.Event:
		m.handleAgentEvent(msg)
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case approvalResolutionDone:
		m.messages = append(m.messages, ChatMessage{Role: "System", Content: fmt.Sprintf("Approval %s applied.", msg.mode)})
	case approvalResolutionError:
		m.messages = append(m.messages, ChatMessage{Role: "System", Content: fmt.Sprintf("Approval failed: %v", msg.err)})
	case sessionSelectionDone:
		m.messages = append(m.messages, ChatMessage{Role: "System", Content: fmt.Sprintf("Session %s selected.", msg.sessionID)})
	case sessionSelectionError:
		m.messages = append(m.messages, ChatMessage{Role: "System", Content: fmt.Sprintf("Session selection failed: %v", msg.err)})
	case sessionForkDone:
		m.sessions = append(m.sessions, SessionRef{ID: msg.session.ID, Title: msg.session.Title})
		m.activeSession = len(m.sessions) - 1
		m.syncActiveSessionTab()
		m.messages = nil
		m.toolCalls = nil
		m.messages = append(m.messages, ChatMessage{Role: "System", Content: fmt.Sprintf("Session %s forked.", msg.session.Title)})
	case sessionForkError:
		m.messages = append(m.messages, ChatMessage{Role: "System", Content: fmt.Sprintf("Session fork failed: %v", msg.err)})
	case sessionCreateDone:
		m.sessions = append(m.sessions, msg.session)
		m.activeSession = len(m.sessions) - 1
		m.syncActiveSessionTab()
		m.messages = append(m.messages, ChatMessage{Role: "System", Content: fmt.Sprintf("Session %s created.", msg.session.Title)})
	case settingsUpdateDone:
		m.settings = msg.settings
		if m.settingsIndex >= m.settingsCount() { m.settingsIndex = m.settingsCount()-1 }
	case aiProviderSelectDone:
		m.messages = append(m.messages, ChatMessage{Role: "System", Content: fmt.Sprintf("AI provider %s selected.", msg.providerID)})
	case aiProviderSelectError:
		m.messages = append(m.messages, ChatMessage{Role: "System", Content: fmt.Sprintf("AI provider selection failed: %v", msg.err)})
	case aiProviderConfigDone:
		m.aiProviderStatus = "AI provider configuration saved."
		m.aiCloudModels = nil
	case aiProviderConfigError:
		m.aiProviderStatus = fmt.Sprintf("AI provider configuration failed: %v", msg.err)
	case aiProviderModelsDone:
		m.aiCloudModels = append([]string(nil), msg.models...)
		if len(msg.models) == 0 { m.aiProviderStatus = "No models returned by API." } else { m.aiProviderStatus = fmt.Sprintf("Loaded %d cloud model(s).", len(msg.models)) }
	case aiProviderModelsError:
		m.aiProviderStatus = fmt.Sprintf("Cloud model loading failed: %v", msg.err)
	case aiProviderLocalActionDone:
		m.aiProviderStatus = fmt.Sprintf("Local model %s completed.", msg.action)
	case aiProviderLocalActionError:
		m.aiProviderStatus = fmt.Sprintf("Local model operation failed: %v", msg.err)
	case aiCloudAuthStarted:
		m.aiCloudAuthSessionID = msg.session.ID
		m.aiCloudAuthPending = msg.session.Status == "pending"
		m.aiCloudAuthMessage = nonEmpty(msg.session.Message, "Waiting for browser authorization.")
		if msg.session.AuthURL != "" {
			if m.openBrowserURL == nil { m.openBrowserURL = openBrowserURL }
			if err := m.openBrowserURL(msg.session.AuthURL); err != nil { m.aiCloudAuthPending = false; m.aiCloudAuthMessage = fmt.Sprintf("Unable to open browser: %v", err); return m, nil }
		}
		if m.aiCloudAuthPending { return m, tea.Tick(time.Second, func(time.Time) tea.Msg { return aiCloudAuthPoll{sessionID: msg.session.ID} }) }
	case aiCloudAuthPoll:
		return m, m.pollCloudProviderAuth(msg.sessionID)
	case aiCloudAuthPending:
		return m, tea.Tick(time.Second, func(time.Time) tea.Msg {
			return aiCloudAuthPoll{sessionID: msg.sessionID}
		})
	case aiCloudAuthDone:
		m.aiCloudAuthPending = false
		m.aiCloudAuthMessage = msg.message
		m.messages = append(m.messages, ChatMessage{Role: "System", Content: msg.message})
	case aiCloudAuthError:
		m.aiCloudAuthPending = false
		m.aiCloudAuthMessage = fmt.Sprintf("%v", msg.err)
		m.messages = append(m.messages, ChatMessage{Role: "System", Content: fmt.Sprintf("Browser authorization failed: %v", msg.err)})
	case aiSendDone:
	case aiSendError:
		m.messages = append(m.messages, ChatMessage{Role: "System", Content: fmt.Sprintf("AI request failed: %v", msg.err)})
	case aiClearDone:
		m.messages = nil
		m.toolCalls = nil
	case sshConfigImportDone:
		m.refreshProfiles(); m.messages=append(m.messages,ChatMessage{Role:"System",Content:fmt.Sprintf("Imported %d SSH profile(s).",msg.count)})
	case sshConfigImportError:
		m.messages=append(m.messages,ChatMessage{Role:"System",Content:fmt.Sprintf("SSH config import failed: %v",msg.err)})
	case aiClearError:
		m.messages = append(m.messages, ChatMessage{Role: "System", Content: fmt.Sprintf("AI clear failed: %v", msg.err)})
	case aiRefreshDone:
		m.messages = append([]ChatMessage(nil), msg.messages...)
	case commandPolicyUpdateDone:
		m.messages = append(m.messages, ChatMessage{Role: "System", Content: "Command policy updated."})
	case commandApprovalDone:
		m.messages=append(m.messages,ChatMessage{Role:"System",Content:fmt.Sprintf("Command request resolved: %s.",msg.mode)}); m.aiApprovalIndex=0
	case commandApprovalError:
		m.messages=append(m.messages,ChatMessage{Role:"System",Content:fmt.Sprintf("Command request resolution failed: %v",msg.err)})
	case commandPolicyUpdateError:
		m.messages = append(m.messages, ChatMessage{Role: "System", Content: fmt.Sprintf("Command policy update failed: %v", msg.err)})
	case secureStorageDone:
		m.masterPasswordInput = ""
		m.masterPasswordPrompt = false
		if m.secureStorageBackend != nil { m.secureStorageStatus = m.secureStorageBackend.GetSecureStorageStatus() }
		m.messages = append(m.messages, ChatMessage{Role: "System", Content: "Secure storage unlocked."})
	case secureStorageError:
		m.masterPasswordInput = ""
		m.messages = append(m.messages, ChatMessage{Role: "System", Content: fmt.Sprintf("Secure storage operation failed: %v", msg.err)})
	case settingsUpdateError:
		m.messages = append(m.messages, ChatMessage{Role: "System", Content: fmt.Sprintf("Settings update failed: %v", msg.err)})
	case sessionProfileCreateDone:
		m.refreshProfiles()
		m.messages = append(m.messages, ChatMessage{Role:"System", Content:"Session profile created."})
	case sessionProfileDeleteDone:
		m.refreshProfiles()
		m.messages = append(m.messages, ChatMessage{Role:"System", Content:"Session profile deleted."})
	case sessionProfileDeleteError:
		m.messages = append(m.messages, ChatMessage{Role:"System", Content:fmt.Sprintf("Session profile deletion failed: %v", msg.err)})
	case sessionProfileCreateError:
		m.messages = append(m.messages, ChatMessage{Role:"System", Content:fmt.Sprintf("Session profile creation failed: %v", msg.err)})
	case sftpListDone:
		m.sftpPath = msg.path
		m.sftpEntries = append([]sftpdomain.FileEntry(nil), msg.entries...)
		m.sftpSelected = 0
		m.fileContent = ""
	case sftpListError:
		m.messages = append(m.messages, ChatMessage{Role:"System", Content:fmt.Sprintf("SFTP listing failed: %v", msg.err)})
	case sftpReadDone:
		m.fileContent = msg.content
		m.sftpPath = msg.path
		m.sftpEditPath = msg.path
	case sftpReadError:
		m.messages = append(m.messages, ChatMessage{Role:"System", Content:fmt.Sprintf("SFTP read failed: %v", msg.err)})
	case sftpSaveDone:
		m.fileContent = msg.content
		m.sftpEdit = false
		m.sftpEditLines = nil
		m.sftpEditPath = ""
		m.messages = append(m.messages, ChatMessage{Role:"System", Content:fmt.Sprintf("SFTP file saved: %s", msg.path)})
	case sftpSaveError:
		m.messages = append(m.messages, ChatMessage{Role:"System", Content:fmt.Sprintf("SFTP save failed: %v", msg.err)})
	case sftpUploadDone:
		m.sftpTransferStatus = fmt.Sprintf("Uploaded %d file(s) to %s.", msg.count, nonEmpty(m.sftpPath, "."))
	case sftpUploadError:
		m.sftpTransferStatus = fmt.Sprintf("SFTP upload failed: %v", msg.err)
	case sftpDownloadDone:
		m.sftpTransferStatus = fmt.Sprintf("Downloaded %s.", msg.path)
	case sftpDownloadError:
		m.sftpTransferStatus = fmt.Sprintf("SFTP download failed: %v", msg.err)
	case runtimeLaunchDone:
		if m.runtimeSessions == nil { m.runtimeSessions = make(map[string]appservice.RuntimeSessionView) }
		m.runtimeSessions[msg.view.ProfileID] = msg.view
		m.bindRuntimeTerminal(msg.view)
		return m, m.connectRuntime(msg.view.ID)
	case runtimeOperationDone:
		if view, ok := m.runtimeSessions[msg.profileID]; ok {
			view.Status = msg.status
			if msg.status == "closed" {
				delete(m.runtimeSessions, msg.profileID)
				m.tabs[1].UnbindSession()
			} else {
				m.runtimeSessions[msg.profileID] = view
				if msg.status == "connected" { m.bindRuntimeTerminal(view) }
			}
		}
		m.messages = append(m.messages, ChatMessage{Role:"System", Content:msg.message})
	case runtimeOperationError:
		if view, ok := m.runtimeSessions[msg.profileID]; ok {
			view.Status = "error"
			m.runtimeSessions[msg.profileID] = view
			if strings.Contains(strings.ToLower(msg.err.Error()), "unknown host key") {
				m.pendingHostKey = view.ID
				m.messages = append(m.messages, ChatMessage{Role:"System", Content:fmt.Sprintf("Unknown SSH host key. Press Ctrl+Y to accept it, then reconnect.\n%v", msg.err)})
				break
			}
		}
		m.messages = append(m.messages, ChatMessage{Role:"System", Content:fmt.Sprintf("%s: %v", msg.operation, msg.err)})
	case runtimeHostKeyAccepted:
		m.pendingHostKey = ""
		m.messages = append(m.messages, ChatMessage{Role:"System", Content:"SSH host key accepted. Reconnecting..."})
		return m, m.reconnectActiveRuntime()
	case runtimeHostKeyAcceptError:
		m.messages = append(m.messages, ChatMessage{Role:"System", Content:fmt.Sprintf("SSH host key acceptance failed: %v", msg.err)})
	case runtimeOutput:
		if m.tabs[1].Session != nil && m.tabs[1].Session.ID == msg.sessionID {
			m.terminalLines = append(m.terminalLines, msg.data)
		}
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyF2:
			m.activeTab = 0
			m.activeView = ""
			return m, nil
		case tea.KeyF3:
			m.activeTab = 1
			m.activeView = ""
			return m, nil
		case tea.KeyF4:
			m.activeTab = 2
			m.activeView = ""
			return m, nil
		case tea.KeyF5:
			m.activeTab = 3
			m.activeView = "tools"
			return m, nil
		case tea.KeyF6:
			m.activeTab = 4
			m.activeView = ""
			return m, nil
		case tea.KeyF7:
			m.activeTab = 5
			m.activeView = ""
			return m, nil
		}
		if msg.Type == tea.KeyCtrlP {
			m.shortcuts = false
			if m.palette == nil { m.palette = &CommandPalette{} } else { m.palette = nil }
			return m, nil
		}
		if m.shortcuts {
			if msg.Type == tea.KeyEsc || (msg.Type == tea.KeyRunes && len(msg.Runes) == 1 && msg.Runes[0] == '?') {
				m.shortcuts = false
			}
			return m, nil
		}
		if m.palette != nil {
			return m.updateCommandPalette(msg)
		}
		if msg.Type == tea.KeyCtrlC || msg.Type == tea.KeyCtrlQ || msg.Type == tea.KeyF10 {
			return m, tea.Quit
		}
		if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 && msg.Runes[0] == '?' && m.approval == nil && strings.TrimSpace(m.input) == "" {
			m.shortcuts = true
			return m, nil
		}
		if m.approval != nil && msg.Type == tea.KeyRunes && len(msg.Runes) == 1 {
			switch msg.Runes[0] {
			case 'y', 'Y':
				return m, m.resolveApproval("now")
			case 's', 'S':
				return m, m.resolveApproval("session")
			case 'a', 'A':
				return m, m.resolveApproval("always")
			case 'n', 'N':
				return m, m.resolveApproval("deny")
			}
		}
		if m.activeTab == 4 && m.sshConfigForm {
			if msg.Type==tea.KeyEsc { m.sshConfigForm=false; m.sshConfigImport=""; return m,nil }
			if msg.Type==tea.KeyCtrlS { if cmd:=m.importSSHConfig(); cmd!=nil{return m,cmd}; return m,nil }
			if msg.Type==tea.KeyBackspace { r:=[]rune(m.sshConfigImport); if len(r)>0{m.sshConfigImport=string(r[:len(r)-1])}; return m,nil }
			if msg.Type==tea.KeyEnter { m.sshConfigImport+="\n"; return m,nil }
			if msg.Type==tea.KeyRunes { m.sshConfigImport+=string(msg.Runes); return m,nil }
		}
		if m.activeTab == 2 && m.sftpEdit {
			return m.updateSFTPEditor(msg)
		}
		switch msg.Type {
		case tea.KeyF8:
			if m.activeTab == 4 && !m.sshConfigForm && m.portForwardForm == nil { m.sshConfigForm=true; m.sshConfigImport=""; return m,nil }
		case tea.KeyCtrlN:
			if m.activeTab == 0 && m.profileForm == nil { m.profileForm = newSessionProfileForm(); return m, nil }
			if m.activeTab == 4 && m.portForwardForm == nil { m.portForwardForm = newPortForwardForm(); return m, nil }
		case tea.KeyCtrlD:
			if m.activeTab == 4 && m.portForwardForm == nil {
				base := m.settingsPortForwardIndex() + 1
				if m.settingsIndex >= base && m.settingsIndex < base+len(m.settings.PortForwardRules) {
					idx := m.settingsIndex - base
					updated := m.settings
					updated.PortForwardRules = append([]domainsettings.PortForwardRule(nil), updated.PortForwardRules...)
					updated.PortForwardRules = append(updated.PortForwardRules[:idx], updated.PortForwardRules[idx+1:]...)
					backend := m.backend
					return m, func() tea.Msg {
						if err := backend.UpdateSettings(updated); err != nil { return settingsUpdateError{err: err} }
						return settingsUpdateDone{settings: updated}
					}
				}
			}
			if m.activeTab == 0 && m.profileForm == nil { if cmd:=m.deleteActiveProfile(); cmd!=nil { return m,cmd } }
			if m.activeTab == 5 && m.aiProviderForm != nil && m.aiProviderForm.isLocal() {
				if cmd := m.downloadAIProviderModel(); cmd != nil { return m, cmd }
				return m, nil
			}
		case tea.KeyCtrlE:
			if m.activeTab == 4 && m.portForwardForm == nil {
				base := m.settingsPortForwardIndex() + 1
				if m.settingsIndex >= base && m.settingsIndex < base+len(m.settings.PortForwardRules) {
					idx := m.settingsIndex - base
					m.portForwardForm = editPortForwardForm(m.settings.PortForwardRules[idx], idx)
					return m, nil
				}
			}
			if m.activeTab == 0 && m.profileForm == nil && len(m.profiles) > 0 && m.activeProfile >= 0 && m.activeProfile < len(m.profiles) {
				m.profileForm = newSessionProfileEditForm(m.profiles[m.activeProfile])
				return m, nil
			}
			if m.activeTab == 2 && !m.sftpEdit && m.fileContent != "" && m.sftpEditPath != "" { m.startSFTPEditor(); return m, nil }
			if m.activeTab == 5 && m.aiProviderForm == nil && strings.TrimSpace(m.input) == "" {
				if m.openAIProviderConfig() { return m, nil }
			}
		case tea.KeyCtrlS:
			if m.activeTab == 5 && m.aiProviderForm != nil && m.aiProviderForm.isLocal() {
				if cmd := m.submitAIProviderConfig(); cmd != nil { return m, cmd }
				return m, nil
			}
		case tea.KeyCtrlK:
			if m.activeTab == 5 {
				if cmd := m.clearAIChat(); cmd != nil { return m, cmd }
				return m, nil
			}
		case tea.KeyCtrlA:
			if m.activeTab == 5 && m.aiProviderForm != nil && !m.aiProviderForm.isLocal() {
				if cmd := m.startCloudProviderAuth(); cmd != nil { return m, cmd }
				return m, nil
			}
		case tea.KeyCtrlL:
			if m.activeTab == 5 && m.aiProviderForm != nil && !m.aiProviderForm.isLocal() {
				if cmd := m.loadAIProviderModels(); cmd != nil { return m, cmd }
				return m, nil
			}
			if m.activeTab == 4 && m.secureStorageBackend != nil { m.secureStorageBackend.LockSecureStorage(); m.secureStorageStatus = m.secureStorageBackend.GetSecureStorageStatus(); m.messages = append(m.messages, ChatMessage{Role: "System", Content: "Secure storage locked."}); return m, nil }
		case tea.KeyCtrlT:
			if m.activeTab == 4 && m.portForwardForm == nil {
				base := m.settingsPortForwardIndex() + 1
				if m.settingsIndex >= base && m.settingsIndex < base+len(m.settings.PortForwardRules) {
					idx := m.settingsIndex - base
					updated := m.settings
					updated.PortForwardRules = append([]domainsettings.PortForwardRule(nil), updated.PortForwardRules...)
					updated.PortForwardRules[idx].Enabled = !updated.PortForwardRules[idx].Enabled
					backend := m.backend
					return m, func() tea.Msg {
						if err := backend.UpdateSettings(updated); err != nil { return settingsUpdateError{err: err} }
						return settingsUpdateDone{settings: updated}
					}
				}
			}
		case tea.KeyCtrlR:
			if m.activeTab == 5 && m.aiProviderForm != nil && m.aiProviderForm.isLocal() {
				if cmd := m.runAIProviderLocalAction(); cmd != nil { return m, cmd }
				return m, nil
			}
			if m.activeTab == 0 && m.profileForm == nil { if cmd := m.reconnectActiveRuntime(); cmd != nil { return m, cmd } }
		case tea.KeyCtrlX:
			if m.profileForm == nil { if cmd := m.disconnectActiveRuntime(); cmd != nil { return m, cmd } }
		case tea.KeyCtrlW:
			if m.profileForm == nil { if cmd := m.closeActiveRuntime(); cmd != nil { return m, cmd } }
		
		case tea.KeyCtrlU:
			if m.activeTab == 2 && !m.sftpEdit { if cmd := m.uploadSFTPFiles(); cmd != nil { return m, cmd } }
		case tea.KeyCtrlO:
			if m.activeTab == 2 && !m.sftpEdit { if cmd := m.downloadSFTPSelection(); cmd != nil { return m, cmd } }
		case tea.KeyCtrlY:
			if m.pendingHostKey != "" && m.runtimeBackend != nil {
				backend := m.runtimeBackend
				sessionID := m.pendingHostKey
				return m, func() tea.Msg {
					if err := backend.AcceptSSHHostKey(sessionID); err != nil { return runtimeHostKeyAcceptError{err: err} }
					return runtimeHostKeyAccepted{}
				}
			}
		case tea.KeyLeft:
			m.activeView = ""
			m.selectPreviousTab()
		case tea.KeyRight, tea.KeyTab:
			m.activeView = ""
			m.selectNextTab()
		case tea.KeyShiftTab:
			m.activeView = ""
			m.selectPreviousTab()
		case tea.KeyUp:
			if m.activeTab == 3 && len(m.aiPendingRequests()) > 0 { m.aiApprovalIndex=(m.aiApprovalIndex+3)%4; break }
			if m.aiProviderForm != nil && !m.aiProviderForm.isLocal() { m.aiProviderForm.field = (m.aiProviderForm.field + 2) % 3; break }
			if m.aiProviderForm != nil && m.aiProviderForm.isLocal() { m.aiProviderForm.field = 0; break }
			if m.portForwardForm != nil { m.portForwardForm.field=(m.portForwardForm.field+3)%4; break }
			if m.profileForm != nil { m.profileForm.field=(m.profileForm.field+4)%5; break }
			if m.activeTab == 0 && len(m.profiles)>0 { m.activeProfile=(m.activeProfile-1+len(m.profiles))%len(m.profiles); break }
			if m.activeTab == 2 && len(m.sftpEntries)>0 { m.sftpSelected=(m.sftpSelected-1+len(m.sftpEntries))%len(m.sftpEntries); break }
			if m.activeTab == 3 && m.aiToolCount() > 0 { m.aiToolIndex=(m.aiToolIndex-1+m.aiToolCount())%m.aiToolCount(); break }
			if m.activeTab == 4 { m.settingsIndex = (m.settingsIndex - 1 + m.settingsCount()) % m.settingsCount() } else if m.activeTab == 5 && strings.TrimSpace(m.input) == "" && m.aiProviderCount() > 0 { m.aiProviderIndex = (m.aiProviderIndex - 1 + m.aiProviderCount()) % m.aiProviderCount() } else { m.selectPreviousSession() }
		case tea.KeyDown:
			if m.activeTab == 3 && len(m.aiPendingRequests()) > 0 { m.aiApprovalIndex=(m.aiApprovalIndex+1)%4; break }
			if m.aiProviderForm != nil && !m.aiProviderForm.isLocal() { m.aiProviderForm.field = (m.aiProviderForm.field + 1) % 3; break }
			if m.aiProviderForm != nil && m.aiProviderForm.isLocal() { m.aiProviderForm.field = 0; break }
			if m.portForwardForm != nil { m.portForwardForm.field=(m.portForwardForm.field+1)%4; break }
			if m.profileForm != nil { m.profileForm.field=(m.profileForm.field+1)%5; break }
			if m.activeTab == 0 && len(m.profiles)>0 { m.activeProfile=(m.activeProfile+1)%len(m.profiles); break }
			if m.activeTab == 2 && len(m.sftpEntries)>0 { m.sftpSelected=(m.sftpSelected+1)%len(m.sftpEntries); break }
			if m.activeTab == 3 && m.aiToolCount() > 0 { m.aiToolIndex=(m.aiToolIndex+1)%m.aiToolCount(); break }
			if m.activeTab == 4 { m.settingsIndex = (m.settingsIndex + 1) % m.settingsCount() } else if m.activeTab == 5 && strings.TrimSpace(m.input) == "" && m.aiProviderCount() > 0 { m.aiProviderIndex = (m.aiProviderIndex + 1) % m.aiProviderCount() } else { m.selectNextSession() }
		case tea.KeyEnter:
			if m.activeTab == 3 && len(m.aiPendingRequests()) > 0 { if cmd:=m.resolveAIApproval(); cmd!=nil { return m,cmd }; return m,nil }
			if m.aiProviderForm != nil {
				if cmd := m.submitAIProviderConfig(); cmd != nil { return m, cmd }
				return m, nil
			}
			if m.portForwardForm != nil {
				f:=m.portForwardForm
				if strings.TrimSpace(f.localPort)=="" || strings.TrimSpace(f.remoteHost)=="" || strings.TrimSpace(f.remotePort)=="" || strings.TrimSpace(f.hostID)=="" { m.messages=append(m.messages,ChatMessage{Role:"System",Content:"Port forwarding rule requires all fields."}); return m,nil }
				updated:=m.settings
				updated.PortForwardRules=append([]domainsettings.PortForwardRule(nil),updated.PortForwardRules...)
				rule:=domainsettings.PortForwardRule{LocalPort:f.localPort,RemoteHost:f.remoteHost,RemotePort:f.remotePort,HostID:f.hostID,Enabled:true}
				if f.editIndex >= 0 && f.editIndex < len(updated.PortForwardRules) {
					rule.Enabled = updated.PortForwardRules[f.editIndex].Enabled
					updated.PortForwardRules[f.editIndex] = rule
				} else {
					updated.PortForwardRules = append(updated.PortForwardRules, rule)
				}
				backend:=m.backend; m.portForwardForm=nil
				return m,func() tea.Msg { if err:=backend.UpdateSettings(updated); err!=nil{return settingsUpdateError{err:err}}; return settingsUpdateDone{settings:updated} }
			}
			if m.masterPasswordPrompt {
				if m.masterPasswordInput == "" || m.secureStorageBackend == nil { return m, nil }
				password := m.masterPasswordInput
				backend := m.secureStorageBackend
				return m, func() tea.Msg {
					if err := backend.EnsureMasterPassword(password); err != nil { return secureStorageError{err: err} }
					return secureStorageDone{}
				}
			}
			if m.activeTab == 3 { if cmd := m.toggleAITool(); cmd != nil { return m, cmd } }
			if m.profileForm != nil { if cmd := m.submitProfileForm(); cmd != nil { return m, cmd }; return m, nil }
			if m.activeTab == 0 {
				if len(m.profiles) == 0 { m.refreshProfiles() }
				if len(m.profiles) > 0 {
					if cmd := m.openActiveProfile(); cmd != nil { return m, cmd }
					return m, nil
				}
			}
			if m.activeTab == 4 {
				if m.secureStorageBackend != nil && m.settingsIndex == 3 { m.masterPasswordPrompt = true; m.masterPasswordInput = ""; return m, nil }
				if m.settingsIndex == m.settingsPortForwardIndex() { if m.portForwardForm == nil { m.portForwardForm=newPortForwardForm() }; return m,nil }
				if cmd := m.toggleSetting(); cmd != nil { return m, cmd }
			} else if m.activeTab == 5 && strings.TrimSpace(m.input) == "" {
				if cmd := m.selectAIProvider(); cmd != nil { return m, cmd }
			} else if m.activeTab == 1 {
				if cmd := m.submitTerminalInput(); cmd != nil { return m, cmd }
			} else if m.activeTab == 2 {
				if cmd := m.openSFTPSelection(); cmd != nil { return m, cmd }
			} else if m.activeTab == 0 && len(m.sessions) > 0 && strings.TrimSpace(m.input) == "" {
				if cmd := m.selectActiveSession(); cmd != nil {
					return m, cmd
				}
			} else {
				if cmd := m.submitChatInput(); cmd != nil { return m, cmd }
			}
		case tea.KeyEsc:
			if m.aiProviderForm != nil { m.aiProviderForm=nil; m.aiCloudModels=nil; m.aiProviderStatus=""; return m,nil }
			if m.portForwardForm != nil { m.portForwardForm=nil; return m,nil }
			if m.masterPasswordPrompt { m.masterPasswordPrompt = false; m.masterPasswordInput = ""; return m, nil }
			if m.profileForm != nil { m.profileForm = nil; return m, nil }
		case tea.KeyBackspace:
			if m.aiProviderForm != nil {
				v := []rune(m.aiProviderForm.value())
				if len(v) > 0 { m.aiProviderForm.setValue(string(v[:len(v)-1])) }
				break
			}
			if m.portForwardForm != nil { v:=m.portForwardForm.value(); if len(v)>0 {m.portForwardForm.setValue(v[:len(v)-1])}; break }
			if m.masterPasswordPrompt {
				if len(m.masterPasswordInput) > 0 { m.masterPasswordInput = m.masterPasswordInput[:len(m.masterPasswordInput)-1] }
				break
			}
			if m.activeTab == 2 && !m.sftpEdit && m.sftpPath != "" && m.sftpPath != "." && m.sftpPath != "/" && m.fileContent == "" {
				parent := path.Dir(m.sftpPath)
				if parent == "" { parent = "." }
				return m, m.loadSFTPFiles(parent)
			}
			if m.profileForm != nil { v:=m.profileForm.value(); if len(v)>0 { m.profileForm.setValue(v[:len(v)-1]) }; break }
			if m.activeTab == 1 {
				if len(m.terminalInput) > 0 {
					m.terminalInput = m.terminalInput[:len(m.terminalInput)-1]
				}
			} else if len(m.input) > 0 {
				m.input = m.input[:len(m.input)-1]
			}
		case tea.KeyCtrlF:
			if m.activeTab == 0 && strings.TrimSpace(m.input) == "" {
				if cmd := m.forkActiveSession(); cmd != nil { return m, cmd }
			}
		case tea.KeyRunes:
			if m.aiProviderForm != nil {
				for _, r := range msg.Runes { if r >= 32 { m.aiProviderForm.setValue(m.aiProviderForm.value()+string(r)) } }
				break
			}
			if m.portForwardForm != nil { for _,r:=range msg.Runes {if r>=32 {m.portForwardForm.setValue(m.portForwardForm.value()+string(r))}}; break }
			if m.masterPasswordPrompt {
				for _, r := range msg.Runes { if r >= 32 { m.masterPasswordInput += string(r) } }
				break
			}
			if m.profileForm != nil { for _, r := range msg.Runes { if r>=32 { m.profileForm.setValue(m.profileForm.value()+string(r)) } }; break }
			if m.activeTab == 1 {
				if !m.hasTerminalSession() {
					break
				}
				for _, r := range msg.Runes {
					if r >= 32 { m.terminalInput += string(r) }
				}
				break
			}
			if len(msg.Runes) == 1 && msg.Runes[0] >= 32 {
				m.input += string(msg.Runes)
			}
		}
	}
	return m, nil
}

func (m *Model) updateCommandPalette(msg tea.KeyMsg) (Model, tea.Cmd) {
	filtered := m.filteredPaletteCommands()
	switch msg.Type {
	case tea.KeyEsc:
		m.palette = nil
	case tea.KeyUp:
		if len(filtered) > 0 { m.palette.Selected = (m.palette.Selected - 1 + len(filtered)) % len(filtered) }
	case tea.KeyDown:
		if len(filtered) > 0 { m.palette.Selected = (m.palette.Selected + 1) % len(filtered) }
	case tea.KeyEnter:
		if len(filtered) > 0 {
			command := filtered[m.palette.Selected]
			m.palette = nil
			return *m, m.executePaletteCommand(command.ID)
		}
	case tea.KeyBackspace:
		if len(m.palette.Query) > 0 { m.palette.Query = m.palette.Query[:len(m.palette.Query)-1]; m.palette.Selected = 0 }
	case tea.KeyRunes:
		for _, r := range msg.Runes {
			if r >= 32 { m.palette.Query += string(r) }
		}
		m.palette.Selected = 0
	}
	return *m, nil
}

func (m Model) filteredPaletteCommands() []PaletteCommand {
	if m.palette == nil { return nil }
	query := strings.ToLower(strings.TrimSpace(m.palette.Query))
	if query == "" { return append([]PaletteCommand(nil), paletteCommands...) }
	result := make([]PaletteCommand, 0, len(paletteCommands))
	for _, command := range paletteCommands {
		if strings.Contains(strings.ToLower(command.Title), query) || strings.Contains(command.ID, query) { result = append(result, command) }
	}
	return result
}

func (m *Model) executePaletteCommand(id string) tea.Cmd {
	switch id {
	case "next-tab": m.selectNextTab()
	case "previous-tab": m.selectPreviousTab()
	case "next-session": m.selectNextSession()
	case "previous-session": m.selectPreviousSession()
	case "fork-session": return m.forkActiveSession()
	case "quit": return tea.Quit
	}
	return nil
}

func (m *Model) handleAgentEvent(event agentai.Event) {
	switch event.Type {
	case agentai.EventTextDelta:
		if event.Content != "" {
			m.messages = append(m.messages, ChatMessage{Role: "AI", Content: event.Content})
		}
	case agentai.EventToolStarted:
		name := strings.TrimSpace(event.Tool)
		if name == "" {
			name = "unknown"
		}
		m.toolCalls = append(m.toolCalls, ToolCallView{Name: name, Status: "running"})
	case agentai.EventToolOutput:
		if len(m.toolCalls) > 0 {
			m.toolCalls[len(m.toolCalls)-1].Output = event.Content
		}
	case agentai.EventToolFinished:
		if len(m.toolCalls) > 0 {
			call := &m.toolCalls[len(m.toolCalls)-1]
			call.Status = "finished"
			if strings.TrimSpace(event.Tool) != "" {
				call.Name = strings.TrimSpace(event.Tool)
			}
		}
	case agentai.EventApprovalRequired:
		if event.Approval != nil {
			request := *event.Approval
			m.approval = &request
		}
	case agentai.EventError:
		content := "AI error"
		if event.Err != nil {
			content = event.Err.Error()
		}
		m.messages = append(m.messages, ChatMessage{Role: "System", Content: content})
	case agentai.EventCancellation:
		m.messages = append(m.messages, ChatMessage{Role: "System", Content: "AI request cancelled"})
	}
}

func (m *Model) resolveApproval(mode string) tea.Cmd {
	if m.approval == nil {
		return nil
	}
	requestID := m.approval.RequestID
	m.approval = nil
	if m.approvalResolver == nil {
		m.messages = append(m.messages, ChatMessage{Role: "System", Content: fmt.Sprintf("Approval unavailable for %s.", requestID)})
		return nil
	}
	resolver := m.approvalResolver
	return func() tea.Msg {
		if err := resolver.ResolveApproval(requestID, mode); err != nil {
			return approvalResolutionError{err: err}
		}
		return approvalResolutionDone{mode: mode}
	}
}

type approvalResolutionDone struct{ mode string }
type approvalResolutionError struct{ err error }

func (m *Model) selectPreviousSession() {
	if len(m.sessions) == 0 {
		return
	}
	m.activeSession = (m.activeSession - 1 + len(m.sessions)) % len(m.sessions)
	m.syncActiveSessionTab()
}

func (m *Model) selectNextSession() {
	if len(m.sessions) == 0 {
		return
	}
	m.activeSession = (m.activeSession + 1) % len(m.sessions)
	m.syncActiveSessionTab()
}

func (m *Model) syncActiveSessionTab() {
	if len(m.tabs) == 0 || len(m.sessions) == 0 {
		return
	}
	session := m.sessions[m.activeSession]
	m.tabs[0].BindSession(session.ID, session.Title)
}

func (m *Model) selectActiveSession() tea.Cmd {
	session := m.ActiveSession()
	if session == nil {
		return nil
	}
	if m.sessionSelector == nil {
		m.messages = append(m.messages, ChatMessage{Role: "System", Content: fmt.Sprintf("Session %s selected.", session.Title)})
		return nil
	}
	sessionID := session.ID
	selector := m.sessionSelector
	return func() tea.Msg {
		if err := selector.SelectChatSession(sessionID); err != nil {
			return sessionSelectionError{err: err}
		}
		return sessionSelectionDone{sessionID: sessionID}
	}
}

type sessionSelectionDone struct{ sessionID string }
type sessionSelectionError struct{ err error }

func (m *Model) forkActiveSession() tea.Cmd {
	session := m.ActiveSession()
	if session == nil { return nil }
	if m.sessionForker == nil {
		m.messages = append(m.messages, ChatMessage{Role: "System", Content: "Session forking unavailable."})
		return nil
	}
	sessionID := session.ID
	title := session.Title + " (fork)"
	forker := m.sessionForker
	return func() tea.Msg {
		created, err := forker.ForkChatSession(sessionID, title)
		if err != nil { return sessionForkError{err: err} }
		return sessionForkDone{session: created}
	}
}

type sessionForkDone struct{ session domainai.ChatSession }
type sessionForkError struct{ err error }

func (m *Model) submitTerminalInput() tea.Cmd {
	if !m.hasTerminalSession() {
		return nil
	}
	content := m.terminalInput
	if content == "" {
		return nil
	}
	sessionID := m.tabs[1].Session.ID
	m.terminalLines = append(m.terminalLines, "$ "+strings.TrimSpace(content))
	m.terminalInput = ""

	if m.runtimeBackend == nil {
		return nil
	}

	backend := m.runtimeBackend
	profileID := ""
	if view, ok := m.runtimeSessionsForSession(sessionID); ok {
		profileID = view.ProfileID
	}
	return func() tea.Msg {
		if err := backend.SendSSHInput(sessionID, content+"\n"); err != nil {
			return runtimeOperationError{operation:"Terminal input failed", profileID:profileID, err:err}
		}
		return nil
	}
}
type sftpListDone struct { sessionID, path string; entries []sftpdomain.FileEntry }
type sftpListError struct { err error }
type sftpReadDone struct { path, content string }
type sftpReadError struct { err error }
type sftpSaveDone struct { path, content string }
type sftpSaveError struct { err error }

type runtimeLaunchDone struct{ view appservice.RuntimeSessionView }
type runtimeOperationDone struct {
	profileID string
	status string
	message string
}
type runtimeOperationError struct {
	operation string
	profileID string
	err error
}
type runtimeOutput struct {
	sessionID string
	data string
}
type runtimeHostKeyAccepted struct{}
type runtimeHostKeyAcceptError struct{ err error }

func (m *Model) activeRuntime() (appservice.RuntimeSessionView, bool) {
	if len(m.profiles) == 0 || m.runtimeSessions == nil { return appservice.RuntimeSessionView{}, false }
	profile := m.profiles[m.activeProfile]
	view, ok := m.runtimeSessions[profile.ID]
	return view, ok
}

func (m *Model) bindRuntimeTerminal(view appservice.RuntimeSessionView) {
	if view.Status == "connected" || view.Status == "connecting" {
		m.tabs[1].BindSession(view.ID, view.Title)
	}
	m.activeTab = 1
}

func (m *Model) openActiveProfile() tea.Cmd {
	if m.runtimeBackend == nil {
		m.messages = append(m.messages, ChatMessage{Role:"System", Content:"Runtime session management unavailable."})
		return nil
	}
	if len(m.profiles) == 0 || m.activeProfile >= len(m.profiles) {
		return nil
	}
	profile := m.profiles[m.activeProfile]
	if view, ok := m.activeRuntime(); ok {
		switch view.Status {
		case "connected":
			m.bindRuntimeTerminal(view)
			return nil
		case "connecting":
			return nil
		case "disconnected", "error":
			return m.connectRuntime(view.ID)
		}
	}
	backend := m.runtimeBackend
	return func() tea.Msg {
		view, err := backend.LaunchSession(profile.ID)
		if err != nil { return runtimeOperationError{operation:"Session launch failed", profileID:profile.ID, err:err} }
		return runtimeLaunchDone{view:view}
	}
}

func (m *Model) connectRuntime(sessionID string) tea.Cmd {
	backend := m.runtimeBackend
	profileID := m.profiles[m.activeProfile].ID
	return func() tea.Msg {
		if err := backend.ConnectSession(sessionID); err != nil {
			return runtimeOperationError{operation:"Session connect failed", profileID:profileID, err:err}
		}
		return runtimeOperationDone{profileID:profileID,status:"connected",message:"Session connected."}
	}
}

func (m *Model) reconnectActiveRuntime() tea.Cmd {
	view, ok := m.activeRuntime()
	if !ok || m.runtimeBackend == nil { return nil }
	backend := m.runtimeBackend
	profileID := view.ProfileID
	return func() tea.Msg {
		if err := backend.ReconnectSession(view.ID); err != nil {
			return runtimeOperationError{operation:"Session reconnect failed", profileID:profileID, err:err}
		}
		return runtimeOperationDone{profileID:profileID,status:"connected",message:"Session reconnected."}
	}
}

func (m *Model) disconnectActiveRuntime() tea.Cmd {
	view, ok := m.activeRuntime()
	if !ok || m.runtimeBackend == nil { return nil }
	backend := m.runtimeBackend
	profileID := view.ProfileID
	return func() tea.Msg {
		if err := backend.DisconnectSession(view.ID); err != nil {
			return runtimeOperationError{operation:"Session disconnect failed", profileID:profileID, err:err}
		}
		return runtimeOperationDone{profileID:profileID,status:"disconnected",message:"Session disconnected."}
	}
}

func (m *Model) closeActiveRuntime() tea.Cmd {
	view, ok := m.activeRuntime()
	if !ok || m.runtimeBackend == nil { return nil }
	backend := m.runtimeBackend
	profileID := view.ProfileID
	return func() tea.Msg {
		if err := backend.CloseSession(view.ID); err != nil {
			return runtimeOperationError{operation:"Session close failed", profileID:profileID, err:err}
		}
		return runtimeOperationDone{profileID:profileID,status:"closed",message:"Session closed."}
	}
}

func (m *Model) submitChatInput() tea.Cmd {
	content := strings.TrimSpace(m.input)
	if content == "" || (m.activeTab != 0 && m.activeTab != 5) {
		return nil
	}
	m.messages = append(m.messages, ChatMessage{Role: "You", Content: content})
	m.input = ""
	if m.aiBackend == nil {
		return nil
	}
	backend := m.aiBackend
	activeSessionID := ""
	if session := m.ActiveSession(); session != nil {
		activeSessionID = session.ID
	}
	return func() tea.Msg {
		if err := backend.SendChatMessage(content, activeSessionID); err != nil {
			return aiSendError{err: err}
		}
		return aiSendDone{}
	}
}

func (m *Model) selectNextTab() {
	if len(m.tabs) == 0 {
		return
	}
	m.activeTab = (m.activeTab + 1) % len(m.tabs)
}

func (m *Model) selectPreviousTab() {
	if len(m.tabs) == 0 {
		return
	}
	m.activeTab = (m.activeTab - 1 + len(m.tabs)) % len(m.tabs)
}

func (m Model) View() string {
	width, height := m.width, m.height
	if width < 80 { width = 80 }
	if height < 24 { height = 24 }

	const headerHeight, tabsHeight, statusHeight, footerHeight = 2, 3, 1, 2
	bodyHeight := height - headerHeight - tabsHeight - statusHeight - footerHeight
	if bodyHeight < 10 { bodyHeight = 10 }
	const sidebarWidth = 27
	mainWidth := width - sidebarWidth - 3
	if mainWidth < 30 { mainWidth = 30 }

	border := lipgloss.Color("240")
	accent := lipgloss.Color("81")
	muted := lipgloss.Color("245")
	activeBg := lipgloss.Color("24")
	panelTitle := lipgloss.NewStyle().Bold(true).Foreground(accent)
	key := lipgloss.NewStyle().Foreground(accent).Bold(true)

	header := lipgloss.NewStyle().Width(width).Height(headerHeight).
		Background(lipgloss.Color("235")).Foreground(lipgloss.Color("255")).Padding(0, 2).
		Render("EIKSY  Think. Connect. Operate.")

	tabs := make([]string, 0, len(m.tabs))
	for i, tab := range m.tabs {
		label := fmt.Sprintf("F%d %s", i+2, tab.Title)
		style := lipgloss.NewStyle().Foreground(muted).Padding(0, 2)
		if i == m.activeTab {
			style = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("255")).Background(activeBg).Padding(0, 2)
		}
		tabs = append(tabs, style.Render(label))
	}
	tabRow := lipgloss.NewStyle().Width(width).Height(tabsHeight).BorderBottom(true).BorderForeground(border).
		Render(lipgloss.JoinHorizontal(lipgloss.Bottom, tabs...))

	body := lipgloss.JoinHorizontal(lipgloss.Top,
		m.renderSidebar(sidebarWidth-2, bodyHeight-2, panelTitle, key),
		" ",
		m.renderMainPanel(mainWidth, bodyHeight-2, panelTitle, key),
	)

	if m.shortcuts { body = m.renderHelp(width-2, bodyHeight-2, panelTitle) }
	if m.profileForm != nil { body = m.renderProfileForm(width-2, bodyHeight-2, panelTitle, key) }
	if m.palette != nil { body = m.renderPalette(width-2, bodyHeight-2, panelTitle, key) }

	active := m.currentViewName()
	statusText := fmt.Sprintf(" %s  |  Session %d/%d", active, m.activeSession+1, len(m.sessions))
	if len(m.sessions) == 0 { statusText = fmt.Sprintf(" %s  |  No session selected", active) }
	statusBar := lipgloss.NewStyle().Foreground(muted).Width(width).Height(statusHeight).Render(statusText)

	footerText := key.Render("F2-F7") + " switch view   " + key.Render("Enter") + " select/send   " +
		key.Render("Ctrl+P") + " menu   " + key.Render("F10") + " exit   " + key.Render("?") + " help"
	footer := lipgloss.NewStyle().Width(width).Height(footerHeight).BorderTop(true).BorderForeground(border).
		Padding(0, 1).Render(footerText)

	return lipgloss.JoinVertical(lipgloss.Left, header, tabRow, body, statusBar, footer)
}

func boolLabel(value bool) string { if value { return "ON" }; return "OFF" }
func nonEmpty(value, fallback string) string { if strings.TrimSpace(value) == "" { return fallback }; return value }

func (m *Model) refreshProfiles() {
	if m.profileBackend == nil {
		return
	}
	m.profiles = m.profileBackend.ListSessionProfiles()
	if m.activeProfile >= len(m.profiles) {
		m.activeProfile = len(m.profiles) - 1
	}
	if m.activeProfile < 0 {
		m.activeProfile = 0
	}
}

func (m *Model) deleteActiveProfile() tea.Cmd {
	if m.profileBackend == nil || len(m.profiles) == 0 {
		return nil
	}
	profileID := strings.TrimSpace(m.profiles[m.activeProfile].ID)
	if profileID == "" {
		return nil
	}
	backend := m.profileBackend
	return func() tea.Msg {
		if err := backend.DeleteSessionProfile(profileID); err != nil {
			return sessionProfileDeleteError{err: err}
		}
		return sessionProfileDeleteDone{}
	}
}

type sessionProfileDeleteDone struct{}
type sessionProfileDeleteError struct{ err error }

func (m *Model) activeRuntimeSessionID() string {
	if len(m.tabs) <= 1 || m.tabs[1].Session == nil { return "" }
	return m.tabs[1].Session.ID
}

func (m *Model) loadSFTPFiles(targetPath string) tea.Cmd {
	if m.runtimeBackend == nil || m.activeRuntimeSessionID() == "" { return nil }
	sessionID := m.activeRuntimeSessionID()
	backend := m.runtimeBackend
	return func() tea.Msg {
		entries, err := backend.ListSFTPFiles(sessionID, targetPath)
		if err != nil { return sftpListError{err: err} }
		path := strings.TrimSpace(targetPath)
		if path == "" { path = "." }
		return sftpListDone{sessionID: sessionID, path: path, entries: entries}
	}
}

func (m *Model) openSFTPSelection() tea.Cmd {
	if m.runtimeBackend == nil || m.activeRuntimeSessionID() == "" { return nil }
	if len(m.sftpEntries) == 0 { return m.loadSFTPFiles(m.sftpPath) }
	entry := m.sftpEntries[m.sftpSelected]
	if entry.IsDir { return m.loadSFTPFiles(entry.Path) }
	backend := m.runtimeBackend
	sessionID := m.activeRuntimeSessionID()
	return func() tea.Msg {
		content, err := backend.ReadSFTPFile(sessionID, entry.Path)
		if err != nil { return sftpReadError{err: err} }
		return sftpReadDone{path: entry.Path, content: content}
	}
}

func (m *Model) startSFTPEditor() {
	m.sftpEdit = true
	m.sftpEditLines = strings.Split(m.fileContent, "\n")
	if len(m.sftpEditLines) == 0 { m.sftpEditLines = []string{""} }
	m.sftpEditRow, m.sftpEditCol = 0, 0
}

func (m *Model) updateSFTPEditor(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.sftpEdit = false; m.sftpEditLines = nil; m.sftpEditPath = ""
	case tea.KeyCtrlS:
		return *m, m.saveSFTPEditor()
	case tea.KeyLeft:
		if m.sftpEditCol > 0 { m.sftpEditCol-- } else if m.sftpEditRow > 0 { m.sftpEditRow--; m.sftpEditCol = len([]rune(m.sftpEditLines[m.sftpEditRow])) }
	case tea.KeyRight:
		if m.sftpEditCol < len([]rune(m.sftpEditLines[m.sftpEditRow])) { m.sftpEditCol++ } else if m.sftpEditRow+1 < len(m.sftpEditLines) { m.sftpEditRow++; m.sftpEditCol = 0 }
	case tea.KeyUp:
		if m.sftpEditRow > 0 { m.sftpEditRow--; if n:=len([]rune(m.sftpEditLines[m.sftpEditRow])); m.sftpEditCol > n { m.sftpEditCol=n } }
	case tea.KeyDown:
		if m.sftpEditRow+1 < len(m.sftpEditLines) { m.sftpEditRow++; if n:=len([]rune(m.sftpEditLines[m.sftpEditRow])); m.sftpEditCol > n { m.sftpEditCol=n } }
	case tea.KeyEnter:
		line:=[]rune(m.sftpEditLines[m.sftpEditRow]); left,right:=string(line[:m.sftpEditCol]),string(line[m.sftpEditCol:])
		m.sftpEditLines[m.sftpEditRow]=left; m.sftpEditLines=append(m.sftpEditLines,"")
		copy(m.sftpEditLines[m.sftpEditRow+1:],m.sftpEditLines[m.sftpEditRow:len(m.sftpEditLines)-1]); m.sftpEditLines[m.sftpEditRow+1]=right
		m.sftpEditRow++; m.sftpEditCol=0
	case tea.KeyBackspace:
		if m.sftpEditCol>0 { line:=[]rune(m.sftpEditLines[m.sftpEditRow]); m.sftpEditLines[m.sftpEditRow]=string(line[:m.sftpEditCol-1])+string(line[m.sftpEditCol:]); m.sftpEditCol--
		} else if m.sftpEditRow>0 { m.sftpEditCol=len([]rune(m.sftpEditLines[m.sftpEditRow-1])); m.sftpEditLines[m.sftpEditRow-1]+=m.sftpEditLines[m.sftpEditRow]; m.sftpEditLines=append(m.sftpEditLines[:m.sftpEditRow],m.sftpEditLines[m.sftpEditRow+1:]...); m.sftpEditRow-- }
	case tea.KeyDelete:
		line:=[]rune(m.sftpEditLines[m.sftpEditRow])
		if m.sftpEditCol<len(line) { m.sftpEditLines[m.sftpEditRow]=string(line[:m.sftpEditCol])+string(line[m.sftpEditCol+1:]) } else if m.sftpEditRow+1<len(m.sftpEditLines) { m.sftpEditLines[m.sftpEditRow]+=m.sftpEditLines[m.sftpEditRow+1]; m.sftpEditLines=append(m.sftpEditLines[:m.sftpEditRow+1],m.sftpEditLines[m.sftpEditRow+2:]...) }
	case tea.KeyRunes:
		line:=[]rune(m.sftpEditLines[m.sftpEditRow])
		for _,r:=range msg.Runes { if r>=32 { line=append(line,0); copy(line[m.sftpEditCol+1:],line[m.sftpEditCol:]); line[m.sftpEditCol]=r; m.sftpEditCol++ } }
		m.sftpEditLines[m.sftpEditRow]=string(line)
	}
	return *m,nil
}

func (m *Model) saveSFTPEditor() tea.Cmd {
	backend,ok:=m.backend.(SFTPWriteBackend)
	if !ok || m.runtimeBackend==nil || m.activeRuntimeSessionID()=="" || strings.TrimSpace(m.sftpEditPath)=="" { m.messages=append(m.messages,ChatMessage{Role:"System",Content:"SFTP file saving unavailable."}); return nil }
	content:=strings.Join(m.sftpEditLines,"\n"); path:=m.sftpEditPath; sessionID:=m.activeRuntimeSessionID()
	return func() tea.Msg { if err:=backend.SaveSFTPFile(sessionID,path,content); err!=nil { return sftpSaveError{err:err} }; return sftpSaveDone{path:path,content:content} }
}

func (m *Model) uploadSFTPFiles() tea.Cmd {
	backend, ok := m.backend.(SFTPUploadBackend)
	if !ok || m.runtimeBackend == nil || m.activeRuntimeSessionID() == "" {
		m.sftpTransferStatus = "SFTP upload unavailable."
		return nil
	}
	sessionID := m.activeRuntimeSessionID()
	remoteDir := nonEmpty(m.sftpPath, ".")
	return func() tea.Msg {
		paths, err := backend.SelectUploadFiles()
		if err != nil { return sftpUploadError{err: err} }
		if len(paths) == 0 { return sftpUploadDone{count: 0} }
		if err := backend.UploadSFTPFiles(sessionID, remoteDir, paths); err != nil { return sftpUploadError{err: err} }
		return sftpUploadDone{count: len(paths)}
	}
}

func (m *Model) downloadSFTPSelection() tea.Cmd {
	backend, ok := m.backend.(SFTPDownloadBackend)
	if !ok || m.runtimeBackend == nil || m.activeRuntimeSessionID() == "" {
		m.sftpTransferStatus = "SFTP download unavailable."
		return nil
	}
	if len(m.sftpEntries) == 0 || m.sftpSelected < 0 || m.sftpSelected >= len(m.sftpEntries) {
		m.sftpTransferStatus = "Select a remote file to download."
		return nil
	}
	entry := m.sftpEntries[m.sftpSelected]
	if entry.IsDir {
		m.sftpTransferStatus = "Select a remote file to download."
		return nil
	}
	sessionID := m.activeRuntimeSessionID()
	remotePath := entry.Path
	return func() tea.Msg {
		localDir, err := backend.SelectDownloadDirectory()
		if err != nil { return sftpDownloadError{err: err} }
		if strings.TrimSpace(localDir) == "" { return sftpDownloadError{err: fmt.Errorf("download directory is empty")} }
		if err := backend.DownloadSFTPFiles(sessionID, localDir, []string{remotePath}); err != nil { return sftpDownloadError{err: err} }
		return sftpDownloadDone{path: remotePath}
	}
}

func (m *Model) refreshSessions() {
	if m.backend == nil { return }
	items := m.backend.ListChatSessions()
	m.sessions = make([]SessionRef, 0, len(items))
	for _, item := range items { m.sessions = append(m.sessions, SessionRef{ID: item.ID, Title: item.Title}) }
	if m.activeSession >= len(m.sessions) { m.activeSession = 0 }
	m.syncActiveSessionTab()
}

func (m *Model) submitProfileForm() tea.Cmd {
	f:=m.profileForm
	if f==nil { return nil }
	if m.profileBackend==nil { m.messages=append(m.messages,ChatMessage{Role:"System",Content:"Session profile management unavailable."}); m.profileForm=nil; return nil }
	if strings.TrimSpace(f.name)=="" || strings.TrimSpace(f.host)=="" { m.messages=append(m.messages,ChatMessage{Role:"System",Content:"Session name and host are required."}); return nil }
	port:=22
	if p,err:=strconv.Atoi(strings.TrimSpace(f.port)); err==nil && p>0 { port=p }
	input:=domainsessions.ProfileInput{ID:f.id,Name:f.name,ProtocolID:"ssh",Host:f.host,Port:port,Username:f.username,Password:f.password,Options:map[string]string{"auth_method":"password"}}
	backend:=m.profileBackend
	m.profileForm=nil
	return func() tea.Msg { if err:=backend.CreateSessionProfileInput(input); err!=nil { return sessionProfileCreateError{err} }; return sessionProfileCreateDone{} }
}

type sessionProfileCreateDone struct{}
type sessionProfileCreateError struct{ err error }

func (m Model) renderProfileForm(width,height int,title,key lipgloss.Style) string {
	f:=m.profileForm
	labels:=[]string{"Name","Host","Port","Username","Password"}
	values:=[]string{f.name,f.host,f.port,f.username,strings.Repeat("*",len(f.password))}
	var b strings.Builder
	if f.id != "" { b.WriteString(title.Render("EDIT SSH SESSION")) } else { b.WriteString(title.Render("NEW SSH SESSION")) }; b.WriteString("\n\n")
	for i,label:=range labels { marker:="  "; if i==f.field { marker="› " }; value:=values[i]; if i==f.field { value+="▌" }; fmt.Fprintf(&b,"%s%-10s %s\n",marker,label,value) }
	b.WriteString("\n"); b.WriteString(key.Render("↑/↓")); b.WriteString(" field  "); b.WriteString(key.Render("Enter")); b.WriteString(" save  "); b.WriteString(key.Render("Esc")); b.WriteString(" cancel")
	return panelFixed(b.String(),width,height)
}

func (m *Model) createChatSession() tea.Cmd {
	if m.backend == nil { m.messages = append(m.messages, ChatMessage{Role: "System", Content: "Session creation unavailable."}); return nil }
	backend := m.backend
	return func() tea.Msg {
		created, err := backend.CreateChatSession("New session")
		if err != nil { return sessionForkError{err: err} }
		return sessionCreateDone{session: SessionRef{ID: created.ID, Title: created.Title}}
	}
}

type sessionCreateDone struct{ session SessionRef }

func (m *Model) toggleSetting() tea.Cmd {
	if m.backend == nil { return nil }
	updated := m.settings
	switch m.settingsIndex {
	case 0: updated.PromptBeforeAI = !updated.PromptBeforeAI
	case 1: updated.AllowCloudModels = !updated.AllowCloudModels
	case 2:
		if strings.EqualFold(updated.Theme, "dark") { updated.Theme = "light" } else { updated.Theme = "dark" }
	}
	backend := m.backend
	return func() tea.Msg {
		if err := backend.UpdateSettings(updated); err != nil { return settingsUpdateError{err: err} }
		return settingsUpdateDone{settings: updated}
	}
}

type secureStorageDone struct{}
type secureStorageError struct{ err error }

type aiSendDone struct{}
type aiSendError struct{ err error }
type aiClearDone struct{}
type aiClearError struct{ err error }
type aiRefreshDone struct{ messages []ChatMessage }

type settingsUpdateDone struct{ settings domainsettings.AppSettings }
type settingsUpdateError struct{ err error }

func (m Model) settingsPortForwardIndex() int { if m.secureStorageBackend != nil { return 4 }; return 3 }
func (m Model) settingsCount() int { return m.settingsPortForwardIndex()+1 }

func (m Model) currentViewName() string {
	if m.activeTab < 0 || m.activeTab >= len(m.tabs) { return "Sessions" }
	return m.tabs[m.activeTab].Title
}

func panelFixed(content string, width, height int) string {
	return lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("240")).
		Padding(0, 1).Width(width).Height(height).Render(content)
}

func (m Model) renderSidebar(width, height int, title, key lipgloss.Style) string {
	var b strings.Builder
	b.WriteString(title.Render("SESSIONS")); b.WriteString("\n\n")
	if len(m.profiles) == 0 {
		b.WriteString("No session profiles\n")
	} else {
		for i, profile := range m.profiles {
			marker := "  "; if i == m.activeProfile { marker = "› " }
			name := profile.Name; if name == "" { name = profile.ID }
			line := marker + name
			if i == m.activeProfile { line = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("255")).Background(lipgloss.Color("24")).Width(width).Render(line) }
			b.WriteString(line); b.WriteByte('\n')
		}
	}
	b.WriteString("\n"); b.WriteString(title.Render("NAVIGATION")); b.WriteString("\n\n")
	b.WriteString(key.Render("↑/↓")); b.WriteString(" sessions\n")
	b.WriteString(key.Render("←/→")); b.WriteString(" tabs\n")
	b.WriteString(key.Render("Tab")); b.WriteString(" next tab\n")
	b.WriteString(key.Render("Enter")); b.WriteString(" select/send\n")
	b.WriteString(key.Render("Ctrl+N")); b.WriteString(" new session\n"); b.WriteString(key.Render("Ctrl+E")); b.WriteString(" edit session\n")
	b.WriteString(key.Render("Ctrl+D")); b.WriteString(" delete profile\n")
	b.WriteString(key.Render("Ctrl+R")); b.WriteString(" reconnect  ")
	b.WriteString(key.Render("Ctrl+X")); b.WriteString(" disconnect  ")
	b.WriteString(key.Render("Ctrl+W")); b.WriteString(" close runtime  ")
	b.WriteString(key.Render("Ctrl+Y")); b.WriteString(" accept host key\n\n")
	b.WriteString(title.Render("ACTIVE SESSION")); b.WriteString("\n\n")
	if len(m.profiles)>0 { b.WriteString(m.profiles[m.activeProfile].Name) } else if s := m.ActiveSession(); s != nil { b.WriteString(s.Title) } else { b.WriteString("none") }
	return panelFixed(b.String(), width, height)
}

func (m Model) renderMainPanel(width, height int, title, key lipgloss.Style) string {
	var b strings.Builder
	b.WriteString(title.Render(strings.ToUpper(m.currentViewName()))); b.WriteString("\n\n")
	switch m.activeTab {
	case 0:
		if m.approval != nil {
			b.WriteString(title.Render("ACTION REQUEST")); b.WriteString("\n\n")
			fmt.Fprintf(&b, "Tool:    %s\nSession: %s\nCommand: %s\n", m.approval.ToolID, m.approval.SessionID, m.approval.Command)
			if m.approval.Reason != "" { fmt.Fprintf(&b, "Reason:  %s\n", m.approval.Reason) }
			b.WriteString("\n" + key.Render("[Y]") + " now   " + key.Render("[S]") + " session   " + key.Render("[A]") + " always   " + key.Render("[N]") + " deny")
		} else if len(m.messages) == 0 && len(m.toolCalls) == 0 {
			b.WriteString("No messages yet.\n\nAsk Eiksy something in the input line below.")
		} else {
			for _, message := range m.messages { fmt.Fprintf(&b, "%s: %s\n", message.Role, message.Content) }
			for _, tool := range m.toolCalls {
				fmt.Fprintf(&b, "\n[%s] %s\n", tool.Name, tool.Status)
				if tool.Output != "" { fmt.Fprintf(&b, "  %s\n", tool.Output) }
			}
		}
		b.WriteString("\n\n> "); b.WriteString(m.input)
	case 1:
		if !m.hasTerminalSession() {
			b.WriteString("No active terminal session.\n\nSelect a session and press Enter to launch/connect.")
		} else {
			fmt.Fprintf(&b, "Session: %s\n\n", m.tabs[1].Session.Title)
			if len(m.terminalLines) == 0 { b.WriteString("Connected. Ready for input.") } else {
				for _, line := range m.terminalLines { b.WriteString(line); b.WriteByte('\n') }
			}
			b.WriteString("\n\n> "); b.WriteString(m.terminalInput)
			if view, ok := m.activeRuntime(); ok { fmt.Fprintf(&b, "\n\nStatus: %s", view.Status) }
		}
	case 2:
		path := m.sftpPath; if path == "" { path = "." }
		fmt.Fprintf(&b, "Path: %s\n\n", path)
		if m.sftpTransferStatus != "" { b.WriteString(m.sftpTransferStatus); b.WriteString("\n\n") }
		if m.sftpEdit {
			b.WriteString("EDIT: "); b.WriteString(m.sftpEditPath); b.WriteString("\n\n")
			for i, line := range m.sftpEditLines {
				if i == m.sftpEditRow { r:=[]rune(line); b.WriteString(string(r[:m.sftpEditCol])); b.WriteString("▌"); b.WriteString(string(r[m.sftpEditCol:])) } else { b.WriteString(line) }
				b.WriteByte('\n')
			}
			b.WriteString("\nCtrl+S save   Esc cancel")
		} else if m.fileContent != "" {
			b.WriteString("FILE: "); b.WriteString(path); b.WriteString("\n\n")
			b.WriteString(m.fileContent)
			b.WriteString("\n\nCtrl+E edit")
		} else if len(m.sftpEntries) == 0 {
			if m.activeRuntimeSessionID() == "" { b.WriteString("No active SSH session.\n\nConnect a session from Sessions first.") } else { b.WriteString("No files loaded yet.\n\nPress Enter to load the remote directory.") }
		} else {
			for i, entry := range m.sftpEntries {
				marker := "  "; if i == m.sftpSelected { marker = "> " }
				kind := "file"; if entry.IsDir { kind = "dir" }
				fmt.Fprintf(&b, "%s[%-4s] %s\n", marker, kind, entry.Name)
			}
			b.WriteString("\n↑/↓ select   Enter open/read   Backspace parent")
		}
	case 3:
		b.WriteString("AI command policy\n\n")
		state := appservice.ShellState{}
		if m.aiBackend != nil { state = m.aiBackend.GetShellState() }
		tools := state.AI.CommandPolicy.Tools
		if len(tools) == 0 { b.WriteString("No AI tools configured.\n") } else {
			for i, tool := range tools { marker := "  "; if i == m.aiToolIndex { marker = "› " }; status := "OFF"; if tool.Enabled { status = "ON" }; fmt.Fprintf(&b, "%s[%s] %s\n", marker, status, nonEmpty(tool.Name, tool.ID)) }
		}
		b.WriteString("\nCommand rules\n")
		if len(state.AI.CommandPolicy.CommandRules) == 0 { b.WriteString("  No explicit rules.\n") } else { for _, rule := range state.AI.CommandPolicy.CommandRules { fmt.Fprintf(&b, "  [%s] %s %s\n", rule.Action, nonEmpty(rule.ToolID, "*"), rule.Pattern) } }
		b.WriteString("\nPending approvals\n")
		if len(state.AI.CommandPolicy.PendingRequests) == 0 { b.WriteString("  None.\n") } else { for _, request := range state.AI.CommandPolicy.PendingRequests { fmt.Fprintf(&b, "  %s: %s\n", request.ID, request.Command) } }
		if len(state.AI.CommandPolicy.PendingRequests) > 0 {
			request := state.AI.CommandPolicy.PendingRequests[0]
			actions := []string{"Run now", "Allow for session", "Always allow", "Deny"}
			b.WriteString("\n\nSelected approval\n")
			fmt.Fprintf(&b, "  %s: %s\n  ", request.ID, request.Command)
			for i, action := range actions { if i == m.aiApprovalIndex { b.WriteString("[" + action + "] ") } else { b.WriteString(action + " ") } }
			b.WriteString("\n  ↑/↓ choose action   Enter resolve")
		}
		b.WriteString("\n↑/↓ select tool   Enter toggle")
		for i := len(m.messages) - 1; i >= 0; i-- {
			if m.messages[i].Role == "System" {
				b.WriteString("\n\n")
				b.WriteString(m.messages[i].Content)
				break
			}
		}
	case 4:
		if m.sshConfigForm { b.WriteString("IMPORT SSH CONFIG\n\n"); b.WriteString(m.sshConfigImport); b.WriteString("▌\n\nCtrl+S import   Esc cancel"); break }
		if m.portForwardForm != nil { b.WriteString("ADD PORT FORWARDING RULE\n\n"); fields:=[]string{fmt.Sprintf("Local port:  %s",m.portForwardForm.localPort),fmt.Sprintf("Remote host: %s",m.portForwardForm.remoteHost),fmt.Sprintf("Remote port: %s",m.portForwardForm.remotePort),fmt.Sprintf("SSH host:    %s",m.portForwardForm.hostID)}; for i,line:=range fields {marker:="  "; if i==m.portForwardForm.field {marker="› "}; b.WriteString(marker+line+"\n")}; b.WriteString("\n↑/↓ field   Enter save   Esc cancel"); break }
		b.WriteString("Persistent application settings.\n\n")
		if m.masterPasswordPrompt { b.WriteString("MASTER PASSWORD\n\n> " + strings.Repeat("*", len(m.masterPasswordInput)) + "▌\n\nEnter unlock/create   Esc cancel"); break }
		status := "unavailable"
		if m.secureStorageStatus.Available { if !m.secureStorageStatus.Configured { status = "not configured" } else if m.secureStorageStatus.Unlocked { status = "unlocked" } else { status = "locked" } }
		lines := []string{
			fmt.Sprintf("Prompt before AI actions: %s", boolLabel(m.settings.PromptBeforeAI)),
			fmt.Sprintf("Allow cloud models:       %s", boolLabel(m.settings.AllowCloudModels)),
			fmt.Sprintf("Theme:                    %s", nonEmpty(m.settings.Theme, "default")),
		}
		if m.secureStorageBackend != nil {
			lines = append(lines, fmt.Sprintf("Secure storage:           %s", status))
		}
		for i, line := range lines { marker := "  "; if i == m.settingsIndex { marker = "› " }; b.WriteString(marker + line + "\n") }
		base := m.settingsPortForwardIndex() + 1
		for i, rule := range m.settings.PortForwardRules {
			idx := base + i
			marker := "  "; if idx == m.settingsIndex { marker = "› " }
			enabled := "OFF"; if rule.Enabled { enabled = "ON" }
			fmt.Fprintf(&b, "%s[%-3s] %s:%s → %s:%s via %s\\n", marker, enabled, rule.LocalPort, rule.RemoteHost, rule.RemotePort, rule.HostID, rule.HostID)
		}
		b.WriteString("\n↑/↓ select   Enter change   F8 import SSH config   Ctrl+N add   Ctrl+E edit   Ctrl+D delete   Ctrl+T toggle   Ctrl+L lock")
	case 5:
		if m.aiProviderForm != nil {
			return m.renderAIProviderConfig(width, height, title, key)
		}
		b.WriteString("AI Providers\n\n")
		b.WriteString(m.aiProviderView())
		b.WriteString("\n\n↑/↓ select provider   Enter activate   Ctrl+E configure")
		b.WriteString("\n\nChat\n")
		if len(m.messages) == 0 { b.WriteString("No AI messages yet.") } else { for _, message := range m.messages { fmt.Fprintf(&b, "%s: %s\n", message.Role, message.Content) } }
		b.WriteString("\n> "); b.WriteString(m.input)
		b.WriteString("\n\nCtrl+K clear conversation")
		if providers := m.aiProviders(); len(providers) > 0 && m.aiProviderIndex >= 0 && m.aiProviderIndex < len(providers) && providers[m.aiProviderIndex].Class == domainai.ProviderClassLocalOpenAI {
			b.WriteString("\nCtrl+E configure   Ctrl+R start/stop local model")
		}
	}
	return panelFixed(b.String(), width, height)
}

func (m Model) renderPalette(width, height int, title, key lipgloss.Style) string {
	var b strings.Builder
	b.WriteString(title.Render("COMMAND PALETTE")); b.WriteString("\n\n> "); b.WriteString(m.palette.Query); b.WriteString("\n\n")
	for i, command := range m.filteredPaletteCommands() {
		marker := "  "; if i == m.palette.Selected { marker = "› " }
		fmt.Fprintf(&b, "%s%s\n", marker, command.Title)
	}
	b.WriteString("\n"); b.WriteString(key.Render("↑/↓")); b.WriteString(" select  "); b.WriteString(key.Render("Enter")); b.WriteString(" run  "); b.WriteString(key.Render("Esc")); b.WriteString(" close")
	return panelFixed(b.String(), width, height)
}

func (m Model) renderHelp(width, height int, title lipgloss.Style) string {
	var b strings.Builder
	b.WriteString(title.Render("KEYBOARD")); b.WriteString("\n\n")
	for _, line := range []string{
		"F2  Sessions       F3  Terminal",
		"F4  Files          F5  Tools",
		"F6  Settings       F7  AI          F10 Exit",
		"",
		"←/→  previous/next tab",
		"Tab  next tab      Shift+Tab previous tab",
		"↑/↓  previous/next session",
		"Enter select/connect  Ctrl+N new session  Ctrl+D delete profile",
		"Ctrl+R reconnect      Ctrl+X disconnect       Ctrl+W close runtime",
		"Ctrl+Y accept unknown SSH host key",
		"Ctrl+U upload files      Ctrl+O download selected file",
		"Ctrl+P command palette",
		"Ctrl+Q quit        Esc close overlay",
	} { b.WriteString(line); b.WriteByte('\n') }
	return panelFixed(b.String(), width, height)
}

// Config contains presentation/runtime options for the terminal UI.

func chatMessagesForEvent(state appservice.ShellState, eventName string) ([]ChatMessage, bool) {
	var source []domainai.ChatMessage
	switch eventName {
	case "ai:message":
		source = state.AI.Messages
	case "agent:message":
		source = state.AI.AgentMessages
	default:
		return nil, false
	}
	messages := make([]ChatMessage, 0, len(source))
	for _, message := range source {
		messages = append(messages, ChatMessage{Role: message.Role, Content: message.Content})
	}
	return messages, true
}

func Run(backend Backend) error {
	model := NewModel().WithBackend(backend)
	program := tea.NewProgram(model, tea.WithAltScreen())
	if service, ok := backend.(*appservice.Service); ok {
		service.SetRuntimeContext(context.Background(), func(eventName string, data ...interface{}) {
			if messages, ok := chatMessagesForEvent(service.GetShellState(), eventName); ok {
				program.Send(aiRefreshDone{messages: messages})
				return
			}
			if eventName != "" && len(data) > 0 {
				if payload, ok := data[0].(map[string]string); ok {
					sessionID := strings.TrimPrefix(eventName, "terminal:output:")
					if sessionID != eventName {
						program.Send(runtimeOutput{sessionID: sessionID, data: payload["data"]})
					}
				}
			}
		})
	}
	_, err := program.Run()
	return err
}

func (m *Model) importSSHConfig() tea.Cmd { backend,ok:=m.backend.(SSHConfigImportBackend); if !ok {m.messages=append(m.messages,ChatMessage{Role:"System",Content:"SSH config import unavailable."});return nil}; raw:=m.sshConfigImport; if strings.TrimSpace(raw)=="" {m.messages=append(m.messages,ChatMessage{Role:"System",Content:"SSH config is empty."});return nil}; m.sshConfigForm=false;m.sshConfigImport=""; return func() tea.Msg {profiles,err:=backend.ImportSSHConfig(raw);if err!=nil{return sshConfigImportError{err}};return sshConfigImportDone{count:len(profiles)}} }

type sshConfigImportDone struct{count int}
type sshConfigImportError struct{err error}

func (m *Model) clearAIChat() tea.Cmd {
	backend, ok := m.backend.(AIChatResetBackend)
	if !ok {
		m.messages = append(m.messages, ChatMessage{Role: "System", Content: "AI conversation reset unavailable."})
		return nil
	}
	return func() tea.Msg {
		if err := backend.ClearChat(); err != nil { return aiClearError{err: err} }
		return aiClearDone{}
	}
}

func (m Model) aiToolCount() int {
	if m.aiBackend == nil { return 0 }
	return len(m.aiBackend.GetShellState().AI.CommandPolicy.Tools)
}
type commandApprovalDone struct{ mode string }
type commandApprovalError struct{ err error }

func (m Model) aiPendingRequests() []domainai.CommandRequest {
	if m.aiBackend == nil { return nil }
	return m.aiBackend.GetShellState().AI.CommandPolicy.PendingRequests
}
func (m Model) aiApprovalMode() string {
	switch m.aiApprovalIndex { case 1: return "session"; case 2: return "always"; case 3: return "deny"; default: return "now" }
}
func (m *Model) resolveAIApproval() tea.Cmd {
	backend, ok := m.backend.(CommandApprovalBackend); if !ok { m.messages=append(m.messages,ChatMessage{Role:"System",Content:"Command approval unavailable."}); return nil }
	requests:=m.aiPendingRequests(); if len(requests)==0 { return nil }; request:=requests[0]; mode:=m.aiApprovalMode()
	return func() tea.Msg { if err:=backend.ResolveCommandPolicyRequest(request.ID,mode); err!=nil { return commandApprovalError{err:err} }; return commandApprovalDone{mode:mode} }
}
type commandPolicyUpdateDone struct{}
type commandPolicyUpdateError struct{ err error }
func (m *Model) toggleAITool() tea.Cmd {
	backend, ok := m.backend.(CommandPolicyBackend)
	if !ok || m.aiBackend == nil { return nil }
	state := m.aiBackend.GetShellState()
	if m.aiToolIndex < 0 || m.aiToolIndex >= len(state.AI.CommandPolicy.Tools) { return nil }
	state.AI.CommandPolicy.Tools[m.aiToolIndex].Enabled = !state.AI.CommandPolicy.Tools[m.aiToolIndex].Enabled
	policy := state.AI.CommandPolicy
	return func() tea.Msg {
		if err := backend.UpdateCommandPolicy(policy); err != nil { return commandPolicyUpdateError{err: err} }
		return commandPolicyUpdateDone{}
	}
}
