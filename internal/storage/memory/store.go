package memory

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"eiksy/internal/domain/ai"
	"eiksy/internal/domain/protocols"
	"eiksy/internal/domain/sessions"
	"eiksy/internal/domain/settings"
	"eiksy/internal/domain/workspace"
	"eiksy/internal/securestorage"
)

const maxLaunchHistoryEntries = 100

type Store struct {
	mu                  sync.RWMutex
	protocols           []protocols.Descriptor
	sessionProfiles     map[string]sessions.Profile
	sessionOrder        []string
	runtimeTabs         map[string]workspace.Tab
	runtimeOrder        []string
	launchHistory       []sessions.HistoryEntry
	aiState             ai.WorkspaceState
	settings            settings.AppSettings
	workspaceLayout     workspace.Layout
	events              []workspace.Event
	eventCounter        int64
	auditEvents         []ai.CommandAuditEvent
	secrets             map[string]string
	secureStatus        securestorage.Status
}

func NewStore() *Store {
	return &Store{
		protocols:           defaultProtocols(),
		sessionProfiles:     map[string]sessions.Profile{},
		sessionOrder:        []string{},
		runtimeTabs:         map[string]workspace.Tab{},
		runtimeOrder:        []string{},
		launchHistory:       []sessions.HistoryEntry{},
		aiState:             withDefaultChatSession(defaultAIState()),
		settings:            defaultSettings(),
		workspaceLayout:     defaultWorkspaceLayout(),
		events:              []workspace.Event{},
		secrets:             map[string]string{},
		secureStatus:        securestorage.Status{Available: true, Configured: true, Unlocked: true},
	}
}

func (s *Store) Protocols() []protocols.Descriptor {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return append([]protocols.Descriptor(nil), s.protocols...)
}

func (s *Store) SessionProfiles() []sessions.Profile {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]sessions.Profile, 0, len(s.sessionOrder))
	for _, id := range s.sessionOrder {
		result = append(result, cloneProfile(s.sessionProfiles[id]))
	}

	return result
}

func (s *Store) SessionProfile(profileID string) (sessions.Profile, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	profile, ok := s.sessionProfiles[profileID]
	return cloneProfile(profile), ok
}

func (s *Store) UpsertSessionProfile(profile sessions.Profile) error {
	s.mu.Lock()
	defer s.mu.Unlock()

		profile.HasPassword = s.secretExistsLocked(securestorage.SessionPasswordKey(profile.ID))
	profile.HasKeyPassphrase = s.secretExistsLocked(securestorage.SessionKeyPassphraseKey(profile.ID))
	if _, ok := s.sessionProfiles[profile.ID]; !ok {
		s.sessionOrder = append(s.sessionOrder, profile.ID)
	}

	s.sessionProfiles[profile.ID] = profile
	return nil
}

func (s *Store) DeleteSessionProfile(profileID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.sessionProfiles[profileID]; !ok {
		return fmt.Errorf("session profile %q not found", profileID)
	}

	delete(s.sessionProfiles, profileID)
	delete(s.secrets, securestorage.SessionPasswordKey(profileID))
	delete(s.secrets, securestorage.SessionKeyPassphraseKey(profileID))
	filtered := make([]string, 0, len(s.sessionOrder))
	for _, id := range s.sessionOrder {
		if id != profileID {
			filtered = append(filtered, id)
		}
	}
	s.sessionOrder = filtered
	return nil
}

func (s *Store) LaunchHistory() []sessions.HistoryEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := append([]sessions.HistoryEntry(nil), s.launchHistory...)
	for left, right := 0, len(result)-1; left < right; left, right = left+1, right-1 {
		result[left], result[right] = result[right], result[left]
	}

	return result
}

func (s *Store) AIState() ai.WorkspaceState {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return cloneAIState(s.aiState)
}

func (s *Store) UpdateAIState(state ai.WorkspaceState) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.aiState = cloneAIState(state)
	if len(s.aiState.ChatSessions) == 0 && s.aiState.ChatSessionID != "" { s.aiState.ChatSessions = []ai.ChatSession{{ID: s.aiState.ChatSessionID, Title: "Main session"}} }
	for i := range s.aiState.Providers { s.aiState.Providers[i].Token = "" }
	s.syncChatSessionLocked()
	return nil
}

func (s *Store) ListChatSessions() []ai.ChatSession {
	s.mu.RLock(); defer s.mu.RUnlock()
	result := make([]ai.ChatSession, 0, len(s.aiState.ChatSessions))
	for _, session := range s.aiState.ChatSessions { copy := session; copy.Messages = nil; result = append(result, copy) }
	return result
}

func (s *Store) CreateChatSession(title string) (ai.ChatSession, error) {
	s.mu.Lock(); defer s.mu.Unlock()
	title = strings.TrimSpace(title); if title == "" { title = "New session" }
	now := time.Now().UTC().Format(time.RFC3339)
	session := ai.ChatSession{ID: fmt.Sprintf("chat-%d", time.Now().UTC().UnixNano()), Title: title, CreatedAt: now, UpdatedAt: now}
	s.aiState.ChatSessions = append(s.aiState.ChatSessions, session); s.aiState.ChatSessionID = session.ID; s.aiState.Messages = nil
	return session, nil
}

func (s *Store) SelectChatSession(id string) error {
	s.mu.Lock(); defer s.mu.Unlock()
	for _, session := range s.aiState.ChatSessions { if session.ID == id { s.aiState.ChatSessionID = id; s.aiState.Messages = append([]ai.ChatMessage(nil), session.Messages...); return nil } }
	return fmt.Errorf("chat session %q not found", id)
}

func (s *Store) syncChatSessionLocked() {
	for i := range s.aiState.ChatSessions { if s.aiState.ChatSessions[i].ID == s.aiState.ChatSessionID { s.aiState.ChatSessions[i].Messages = append([]ai.ChatMessage(nil), s.aiState.Messages...); s.aiState.ChatSessions[i].UpdatedAt = time.Now().UTC().Format(time.RFC3339); return } }
}

func (s *Store) ClearAIHistory() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.aiState.Messages = nil
	return nil
}

func (s *Store) UpdateSettings(updated settings.AppSettings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	updated.VaultToken = ""
	updated.VaultPassword = ""
	updated.KeePassPassword = ""
	updated.HasVaultToken = s.secretExistsLocked(securestorage.VaultTokenKey())
	updated.HasKeePassPassword = s.secretExistsLocked(securestorage.KeePassPasswordKey())
	updated.HasVaultPassword = s.secretExistsLocked(securestorage.VaultPasswordKey())
	s.settings = updated
	return nil
}

func (s *Store) Settings() settings.AppSettings {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.settings
}

func (s *Store) WorkspaceLayout() workspace.Layout {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return cloneWorkspaceLayout(s.workspaceLayout)
}

func (s *Store) RuntimeTabs() []workspace.Tab {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]workspace.Tab, 0, len(s.runtimeOrder))
	for _, id := range s.runtimeOrder {
		result = append(result, s.runtimeTabs[id])
	}

	return result
}

func (s *Store) Events() []workspace.Event {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]workspace.Event, 0, len(s.events))
	for i := len(s.events) - 1; i >= 0; i-- {
		result = append(result, s.events[i])
	}

	return result
}

func (s *Store) OpenRuntimeTab(tab workspace.Tab) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.runtimeTabs[tab.ID]; !ok {
		s.runtimeOrder = append(s.runtimeOrder, tab.ID)
	}

	s.runtimeTabs[tab.ID] = tab
	s.workspaceLayout.ActiveTabID = tab.ID
	s.events = append(s.events, workspace.Event{
		ID:      s.nextEventIDLocked(),
		Type:    "session.opened",
		Subject: tab.Title,
		At:      time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Store) CloseRuntimeTab(sessionID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.runtimeTabs[sessionID]; !ok {
		return false
	}

	delete(s.runtimeTabs, sessionID)
	removedIndex := -1
	filteredOrder := make([]string, 0, len(s.runtimeOrder))
	for index, id := range s.runtimeOrder {
		if id != sessionID {
			filteredOrder = append(filteredOrder, id)
		} else {
			removedIndex = index
		}
	}
	if removedIndex >= 0 {
		s.events = append(s.events, workspace.Event{
			ID:      s.nextEventIDLocked(),
			Type:    "session.closed",
			Subject: sessionID,
			At:      time.Now().UTC().Format(time.RFC3339),
		})
	}
	s.runtimeOrder = filteredOrder
	if len(s.runtimeOrder) > 0 {
		nextIndex := removedIndex
		if nextIndex >= len(s.runtimeOrder) {
			nextIndex = len(s.runtimeOrder) - 1
		}
		if nextIndex < 0 {
			nextIndex = 0
		}
		s.workspaceLayout.ActiveTabID = s.runtimeOrder[nextIndex]
	} else {
		s.workspaceLayout.ActiveTabID = ""
	}

	return true
}

func (s *Store) RecordLaunch(profileID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	profile, ok := s.sessionProfiles[profileID]
	if !ok {
		return fmt.Errorf("session profile %q not found", profileID)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	profile.LastLaunchedAt = now
	s.sessionProfiles[profile.ID] = profile

	s.launchHistory = append(s.launchHistory, sessions.HistoryEntry{
		ProfileID:   profile.ID,
		ProfileName: profile.Name,
		LaunchedAt:  now,
	})
	if len(s.launchHistory) > maxLaunchHistoryEntries {
		s.launchHistory = append([]sessions.HistoryEntry(nil), s.launchHistory[len(s.launchHistory)-maxLaunchHistoryEntries:]...)
	}
	return nil
}

func (s *Store) nextEventIDLocked() string {
	s.eventCounter++
	return fmt.Sprintf("event-%d", s.eventCounter)
}

func (s *Store) LockSecureStorage() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.secureStatus.Unlocked = false
}

func (s *Store) SecureStorageStatus() securestorage.Status {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.secureStatus
}

func (s *Store) EnsureMasterPassword(password string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(password) == "" {
		return fmt.Errorf("master password is required")
	}
	s.secureStatus = securestorage.Status{Available: true, Configured: true, Unlocked: true}
	return nil
}

func (s *Store) SecretExists(key string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.secretExistsLocked(key)
}

func (s *Store) LoadSecret(key string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.secureStatus.Unlocked {
		return "", fmt.Errorf("secure storage is locked")
	}
	return s.secrets[key], nil
}

func (s *Store) StoreSecret(key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.secureStatus.Unlocked {
		return fmt.Errorf("secure storage is locked")
	}
	s.secrets[key] = value
	return nil
}

func (s *Store) DeleteSecret(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.secrets, key)
	return nil
}

func (s *Store) secretExistsLocked(key string) bool {
	_, ok := s.secrets[key]
	return ok
}

func withDefaultChatSession(state ai.WorkspaceState) ai.WorkspaceState {
	now := time.Now().UTC().Format(time.RFC3339)
	state.ChatSessions = []ai.ChatSession{{ID: state.ChatSessionID, Title: "Main session", CreatedAt: now, UpdatedAt: now, Messages: append([]ai.ChatMessage(nil), state.Messages...)}}
	return state
}

func defaultProtocols() []protocols.Descriptor {
	return []protocols.Descriptor{
		{ID: "ssh", Name: "Secure Shell", Scheme: "ssh", Capabilities: []protocols.Capability{protocols.CapabilityTerminal, protocols.CapabilityCredentialLink}},
		{ID: "sftp", Name: "SSH File Transfer", Scheme: "sftp", Capabilities: []protocols.Capability{protocols.CapabilityFileBrowser, protocols.CapabilityCredentialLink}},
	}
}

func defaultAIState() ai.WorkspaceState {
	return ai.WorkspaceState{
		Providers: []ai.ProviderDescriptor{
			{ID: "openai-compatible-cloud", Name: "OpenAI-compatible Cloud", Class: ai.ProviderClassOpenAICompatible, Model: "Remote model", Status: "configuration required", Selected: true, Configured: false},
			{ID: "local-qwen3-4b", Name: "Local Qwen3 4B", Class: ai.ProviderClassLocalOpenAI, Model: "Qwen3-4B-Q4_K_M", Endpoint: "http://127.0.0.1:8012/v1", DownloadURL: "https://huggingface.co/unsloth/Qwen3-4B-GGUF/resolve/main/Qwen3-4B-Q4_K_M.gguf?download=true", Status: "download required", Selected: false, Configured: false},
		},
		ContextPolicy: ai.ContextPolicy{
			SendTerminalSelection: true,
			SendRecentOutput:      false,
			RequireConfirmation:   true,
		},
		CommandPolicy: defaultCommandPolicy(),
		Messages:      []ai.ChatMessage{{Role: "assistant", Content: "Ask for command suggestions or paste terminal errors for analysis."}},
		ChatSessionID: fmt.Sprintf("chat-%d", time.Now().UTC().UnixNano()),
		ChatSessions:  []ai.ChatSession{},
	}
}

func defaultCommandPolicy() ai.CommandPolicy {
	return ai.CommandPolicy{
		Tools: []ai.CommandTool{
			{ID: "shell", Name: "Shell command", Description: "Run command in active SSH session", Enabled: true},
			{ID: "sftp", Name: "SFTP operations", Description: "Browse and edit files over SFTP", Enabled: true},
			{ID: "search", Name: "Search", Description: "Run grep/find-like queries on host", Enabled: true},
		},
		PendingRequests:     []ai.CommandRequest{},
	}
}

func defaultSettings() settings.AppSettings {
	return settings.AppSettings{
		Theme:           "dark",
		DefaultProtocol: "ssh",
		WindowLayout: settings.WindowLayout{
			SidebarWidth:   300,
			AssistantWidth: 360,
		},
		PromptBeforeAI:      true,
		AllowCloudModels:    true,
		SSHConfigAutoLoaded: false,
		VaultMountPoint:     settings.DefaultVaultMountPoint,
		VaultAutoRenewToken: false,
		VaultAuthMethod:     settings.DefaultVaultAuthMethod,
		VaultProvider:       settings.DefaultVaultProvider,
	}
}

func defaultWorkspaceLayout() workspace.Layout {
	return workspace.Layout{
		SidebarSections: []workspace.SidebarSection{
			{ID: "sessions", Title: "Sessions"},
			{ID: "files", Title: "SFTP Browser"},
		},
	}
}

func cloneAIState(state ai.WorkspaceState) ai.WorkspaceState {
	cloned := state
	cloned.Providers = append([]ai.ProviderDescriptor(nil), state.Providers...)
	cloned.CommandPolicy = cloneCommandPolicy(state.CommandPolicy)
	cloned.Messages = append([]ai.ChatMessage{}, state.Messages...)
	cloned.AgentMessages = append([]ai.ChatMessage{}, state.AgentMessages...)
	cloned.ChatSessions = make([]ai.ChatSession, len(state.ChatSessions))
	for i, session := range state.ChatSessions { cloned.ChatSessions[i] = session; cloned.ChatSessions[i].Messages = append([]ai.ChatMessage(nil), session.Messages...) }
	return cloned
}

func cloneCommandPolicy(policy ai.CommandPolicy) ai.CommandPolicy {
	cloned := policy
	cloned.Tools = append([]ai.CommandTool(nil), policy.Tools...)
	cloned.PendingRequests = append([]ai.CommandRequest(nil), policy.PendingRequests...)

	return cloned
}

func cloneWorkspaceLayout(layout workspace.Layout) workspace.Layout {
	cloned := layout
	cloned.SidebarSections = append([]workspace.SidebarSection(nil), layout.SidebarSections...)
	return cloned
}

func cloneProfile(profile sessions.Profile) sessions.Profile {
	cloned := profile
	cloned.Tags = append([]string{}, profile.Tags...)
	if profile.Options != nil {
		cloned.Options = make(map[string]string, len(profile.Options))
		for key, value := range profile.Options {
			cloned.Options[key] = value
		}
	}
	return cloned
}

const maxPersistentAuditEvents = 500

func (s *Store) AppendCommandAudit(event ai.CommandAuditEvent) error {
	s.mu.Lock(); defer s.mu.Unlock()
	s.auditEvents = append(s.auditEvents, event)
	if len(s.auditEvents) > maxPersistentAuditEvents { s.auditEvents = append([]ai.CommandAuditEvent(nil), s.auditEvents[len(s.auditEvents)-maxPersistentAuditEvents:]...) }
	return nil
}

func (s *Store) CommandAuditTrail() []ai.CommandAuditEvent {
	s.mu.RLock(); defer s.mu.RUnlock()
	return append([]ai.CommandAuditEvent(nil), s.auditEvents...)
}


func (s *Store) ForkChatSession(id, title string) (ai.ChatSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var source *ai.ChatSession
	for i := range s.aiState.ChatSessions {
		if s.aiState.ChatSessions[i].ID == id {
			copy := s.aiState.ChatSessions[i]
			copy.Messages = append([]ai.ChatMessage(nil), copy.Messages...)
			source = &copy
			break
		}
	}
	if source == nil { return ai.ChatSession{}, fmt.Errorf("chat session %q not found", id) }
	title = strings.TrimSpace(title)
	if title == "" { title = source.Title + " (fork)" }
	now := time.Now().UTC().Format(time.RFC3339)
	session := ai.ChatSession{ID: fmt.Sprintf("chat-%d", time.Now().UTC().UnixNano()), Title: title, CreatedAt: now, UpdatedAt: now, Messages: source.Messages}
	s.aiState.ChatSessions = append(s.aiState.ChatSessions, session)
	s.aiState.ChatSessionID = session.ID
	s.aiState.Messages = append([]ai.ChatMessage(nil), session.Messages...)
	return session, nil
}
