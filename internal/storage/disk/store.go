package disk

import (
	"errors"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"slices"
	"sync"
	"time"

	"eiksy/internal/debuglog"
	"eiksy/internal/domain/ai"
	"eiksy/internal/domain/protocols"
	"eiksy/internal/domain/sessions"
	"eiksy/internal/domain/settings"
	"eiksy/internal/domain/workspace"
	"eiksy/internal/securestorage"
)

const (
	maxLaunchHistoryEntries = 100
	defaultEiksyDir         = ".eiksy"
)

type Store struct {
	mu                  sync.RWMutex
	baseDir             string
	sessionsPath        string
	settingsPath        string
	protocols           []protocols.Descriptor
	sessionProfiles     map[string]sessions.Profile
	sessionOrder        []string
	runtimeTabs         map[string]workspace.Tab
	runtimeOrder        []string
	launchHistory       []sessions.HistoryEntry
	aiState             ai.WorkspaceState
	legacyAIChatHistory []ai.ChatMessage
	settings            settings.AppSettings
	workspaceLayout     workspace.Layout
	events              []workspace.Event
	eventCounter        int64
	secretManager       *securestorage.Manager
}

type persistedSettings struct {
	settings.AppSettings
	AIState *persistedAIWorkspaceState `json:"aiState,omitempty"`
}

type persistedChatHistory struct {
	Sessions      []ai.ChatSession `json:"sessions"`
	Legacy        []ai.ChatMessage `json:"messages,omitempty"`
	AgentMessages []ai.ChatMessage `json:"agentMessages,omitempty"`
}

type persistedAIWorkspaceState struct {
	LegacyMessages []ai.ChatMessage `json:"messages,omitempty"`
	Providers     []persistedAIProviderDescriptor `json:"providers"`
	ContextPolicy ai.ContextPolicy                `json:"contextPolicy"`
	CommandPolicy ai.CommandPolicy                `json:"commandPolicy"`
	ChatSessionID string                          `json:"chatSessionId"`
}

type persistedAIProviderDescriptor struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	Class       ai.ProviderClass `json:"class"`
	Model       string           `json:"model"`
	Endpoint    string           `json:"endpoint,omitempty"`
	DownloadURL string           `json:"downloadUrl,omitempty"`
	LocalPath   string           `json:"localPath,omitempty"`
	Status      string           `json:"status"`
	Selected    bool             `json:"selected"`
	Configured  bool             `json:"configured"`
}

func NewStore() (*Store, error) {
	debuglog.Printf("disk store: resolving base directory")
	baseDir, err := eiksyDir()
	if err != nil {
		debuglog.Printf("disk store: resolve base directory failed: %v", err)
		return nil, err
	}
	debuglog.Printf("disk store: base directory=%q", baseDir)
	return NewStoreAt(baseDir)
}

func eiksyDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home dir: %w", err)
	}
	return filepath.Join(homeDir, defaultEiksyDir), nil
}

func NewStoreAt(baseDir string) (*Store, error) {
	return NewStoreAtWithKeyring(baseDir, nil)
}

func NewStoreAtWithKeyring(baseDir string, keyring securestorage.Keyring) (*Store, error) {
	debuglog.Printf("disk store: initializing at %q", baseDir)
	if err := os.MkdirAll(filepath.Join(baseDir, "models"), 0o700); err != nil {
		debuglog.Printf("disk store: create directory failed: %v", err)
		return nil, fmt.Errorf("create eiksy directory: %w", err)
	}
	debuglog.Printf("disk store: directory created")
	if err := os.Chmod(baseDir, 0o700); err != nil {
		debuglog.Printf("disk store: chmod failed: %v", err)
		return nil, fmt.Errorf("secure eiksy directory permissions: %w", err)
	}

	var (
		secretManager *securestorage.Manager
		err           error
	)
	if keyring == nil {
		secretManager, err = securestorage.New(filepath.Join(baseDir, "secrets.db"))
	} else {
		secretManager, err = securestorage.NewWithKeyring(filepath.Join(baseDir, "secrets.db"), keyring)
	}
	if err != nil {
		debuglog.Printf("disk store: secure storage initialization failed: %v", err)
		return nil, err
	}
	debuglog.Printf("disk store: secure storage initialized")

	store := &Store{
		baseDir:             baseDir,
		sessionsPath:        filepath.Join(baseDir, "sessions.json"),
		settingsPath:        filepath.Join(baseDir, "settings.json"),
		protocols:           defaultProtocols(),
		sessionProfiles:     map[string]sessions.Profile{},
		sessionOrder:        []string{},
		runtimeTabs:         map[string]workspace.Tab{},
		runtimeOrder:        []string{},
		launchHistory:       []sessions.HistoryEntry{},
		aiState:             defaultAIState(),
		settings:            defaultSettings(),
		workspaceLayout:     defaultWorkspaceLayout(),
		events:              []workspace.Event{},
		secretManager:       secretManager,
	}
	store.aiState.ChatSessions = []ai.ChatSession{{ID: store.aiState.ChatSessionID, Title: "Main session", CreatedAt: time.Now().UTC().Format(time.RFC3339), UpdatedAt: time.Now().UTC().Format(time.RFC3339), Messages: append([]ai.ChatMessage(nil), store.aiState.Messages...)}}

	if err := store.ensureFiles(); err != nil {
		debuglog.Printf("disk store: ensure files failed: %v", err)
		return nil, err
	}
	debuglog.Printf("disk store: files ensured")
	if err := store.loadSessionProfiles(); err != nil {
		debuglog.Printf("disk store: load session profiles failed: %v", err)
		return nil, err
	}
	debuglog.Printf("disk store: session profiles loaded")
	if err := store.loadSettings(); err != nil {
		debuglog.Printf("disk store: load settings failed: %v", err)
		return nil, err
	}
	debuglog.Printf("disk store: settings loaded")

	debuglog.Printf("disk store: initialization complete")
	return store, nil
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

func (s *Store) UpdateAIState(state ai.WorkspaceState) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	previous := s.aiState
	s.aiState = cloneAIState(state)
	for i := range s.aiState.Providers {
		s.aiState.Providers[i].Token = ""
	}
	if !chatMessagesEqual(previous.Messages, s.aiState.Messages) || !chatMessagesEqual(previous.AgentMessages, s.aiState.AgentMessages) || len(s.aiState.ChatSessions) > 0 {
		if err := s.persistAIChatHistoryLocked(s.aiState.Messages); err != nil {
			s.aiState = previous
			return err
		}
	}
	if err := s.saveSettings(); err != nil {
		s.aiState = previous
		return err
	}
	return nil
}

func (s *Store) ListChatSessions() []ai.ChatSession {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]ai.ChatSession, 0, len(s.aiState.ChatSessions))
	for _, session := range s.aiState.ChatSessions {
		copy := session
		copy.Messages = nil
		result = append(result, copy)
	}
	return result
}

func (s *Store) CreateChatSession(title string) (ai.ChatSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	title = strings.TrimSpace(title)
	if title == "" { title = "New session" }
	now := time.Now().UTC().Format(time.RFC3339)
	session := ai.ChatSession{ID: fmt.Sprintf("chat-%d", time.Now().UTC().UnixNano()), Title: title, CreatedAt: now, UpdatedAt: now}
	s.aiState.ChatSessions = append(s.aiState.ChatSessions, session)
	s.aiState.ChatSessionID = session.ID
	s.aiState.Messages = nil
	if err := s.persistAIChatHistoryLocked(nil); err != nil { return ai.ChatSession{}, err }
	if err := s.saveSettings(); err != nil { return ai.ChatSession{}, err }
	return session, nil
}

func (s *Store) SelectChatSession(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, session := range s.aiState.ChatSessions {
		if session.ID != id { continue }
		s.aiState.ChatSessionID = session.ID
		s.aiState.Messages = append([]ai.ChatMessage(nil), session.Messages...)
		return s.saveSettings()
	}
	return fmt.Errorf("chat session %q not found", id)
}

func (s *Store) syncChatSessionLocked() {
	for i := range s.aiState.ChatSessions {
		if s.aiState.ChatSessions[i].ID == s.aiState.ChatSessionID {
			s.aiState.ChatSessions[i].Messages = append([]ai.ChatMessage(nil), s.aiState.Messages...)
			s.aiState.ChatSessions[i].UpdatedAt = time.Now().UTC().Format(time.RFC3339)
			return
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	s.aiState.ChatSessions = append(s.aiState.ChatSessions, ai.ChatSession{ID: s.aiState.ChatSessionID, Title: "Main session", CreatedAt: now, UpdatedAt: now, Messages: append([]ai.ChatMessage(nil), s.aiState.Messages...)})
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
	updated.HasVaultToken = s.secretManager.SecretExists(securestorage.VaultTokenKey())
	updated.HasKeePassPassword = s.secretManager.SecretExists(securestorage.KeePassPasswordKey())
	updated.HasVaultPassword = s.secretManager.SecretExists(securestorage.VaultPasswordKey())
	s.settings = updated
	return s.saveSettings()
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
	filtered := make([]string, 0, len(s.runtimeOrder))
	for index, id := range s.runtimeOrder {
		if id != sessionID {
			filtered = append(filtered, id)
		} else {
			removedIndex = index
		}
	}
	s.runtimeOrder = filtered
	s.events = append(s.events, workspace.Event{
		ID:      s.nextEventIDLocked(),
		Type:    "session.closed",
		Subject: sessionID,
		At:      time.Now().UTC().Format(time.RFC3339),
	})
	if len(s.runtimeOrder) == 0 {
		s.workspaceLayout.ActiveTabID = ""
		return true
	}
	nextIndex := removedIndex
	if nextIndex >= len(s.runtimeOrder) {
		nextIndex = len(s.runtimeOrder) - 1
	}
	if nextIndex < 0 {
		nextIndex = 0
	}
	s.workspaceLayout.ActiveTabID = s.runtimeOrder[nextIndex]
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
	s.launchHistory = append(s.launchHistory, sessions.HistoryEntry{ProfileID: profile.ID, ProfileName: profile.Name, LaunchedAt: now})
	if len(s.launchHistory) > maxLaunchHistoryEntries {
		s.launchHistory = append([]sessions.HistoryEntry(nil), s.launchHistory[len(s.launchHistory)-maxLaunchHistoryEntries:]...)
	}
	if err := s.saveSessionProfilesLocked(); err != nil {
		return err
	}
	return nil
}

func (s *Store) UpsertSessionProfile(profile sessions.Profile) error {
	s.mu.Lock()
	defer s.mu.Unlock()
		profile.HasPassword = s.secretManager.SecretExists(securestorage.SessionPasswordKey(profile.ID))
	profile.HasKeyPassphrase = s.secretManager.SecretExists(securestorage.SessionKeyPassphraseKey(profile.ID))
	if _, ok := s.sessionProfiles[profile.ID]; !ok {
		s.sessionOrder = append(s.sessionOrder, profile.ID)
	}
	s.sessionProfiles[profile.ID] = profile
	return s.saveSessionProfilesLocked()
}

func (s *Store) DeleteSessionProfile(profileID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.sessionProfiles[profileID]; !ok {
		return fmt.Errorf("session profile %q not found", profileID)
	}
	if err := s.secretManager.DeleteSecret(securestorage.SessionPasswordKey(profileID)); err != nil {
		return err
	}
	if err := s.secretManager.DeleteSecret(securestorage.SessionKeyPassphraseKey(profileID)); err != nil {
		return err
	}
	delete(s.sessionProfiles, profileID)
	filtered := make([]string, 0, len(s.sessionOrder))
	for _, id := range s.sessionOrder {
		if id != profileID {
			filtered = append(filtered, id)
		}
	}
	s.sessionOrder = filtered
	return s.saveSessionProfilesLocked()
}

func (s *Store) ensureFiles() error {
	if err := ensureJSONFile(s.sessionsPath, []byte("[]\n")); err != nil {
		return fmt.Errorf("create sessions file: %w", err)
	}
	settingsBytes, err := json.MarshalIndent(newPersistedSettings(defaultSettings(), defaultAIState()), "", "  ")
	if err != nil {
		return fmt.Errorf("marshal default settings: %w", err)
	}
	settingsBytes = append(settingsBytes, '\n')
	if err := ensureJSONFile(s.settingsPath, settingsBytes); err != nil {
		return fmt.Errorf("create settings file: %w", err)
	}
	return nil
}

func (s *Store) loadSessionProfiles() error {
	content, err := os.ReadFile(s.sessionsPath)
	if err != nil {
		return fmt.Errorf("read sessions file: %w", err)
	}
	if len(strings.TrimSpace(string(content))) == 0 {
		return nil
	}
	var profiles []sessions.Profile
	if err := json.Unmarshal(content, &profiles); err != nil {
		return fmt.Errorf("decode sessions file: %w", err)
	}
	for _, persisted := range profiles {
		profile := cloneProfile(persisted)
		profile.HasPassword = s.secretManager.SecretExists(securestorage.SessionPasswordKey(profile.ID))
		profile.HasKeyPassphrase = s.secretManager.SecretExists(securestorage.SessionKeyPassphraseKey(profile.ID))
		s.sessionProfiles[profile.ID] = profile
		s.sessionOrder = append(s.sessionOrder, profile.ID)
	}
	return nil
}

func (s *Store) loadSettings() error {
	content, err := os.ReadFile(s.settingsPath)
	if err != nil {
		return fmt.Errorf("read settings file: %w", err)
	}
	if len(strings.TrimSpace(string(content))) == 0 {
		s.settings = defaultSettings()
		return s.saveSettings()
	}
	var loaded settings.AppSettings
	var persisted persistedSettings
	if err := json.Unmarshal(content, &persisted); err != nil {
		return fmt.Errorf("decode settings file: %w", err)
	}
	loaded = persisted.AppSettings
	defaults := defaultSettings()
	if loaded.Theme == "" {
		loaded.Theme = defaults.Theme
	}
	if loaded.DefaultProtocol == "" {
		loaded.DefaultProtocol = defaults.DefaultProtocol
	}
	if loaded.WindowLayout.SidebarWidth == 0 {
		loaded.WindowLayout.SidebarWidth = defaults.WindowLayout.SidebarWidth
	}
	if loaded.WindowLayout.AssistantWidth == 0 {
		loaded.WindowLayout.AssistantWidth = defaults.WindowLayout.AssistantWidth
	}
	if loaded.VaultMountPoint == "" {
		loaded.VaultMountPoint = defaults.VaultMountPoint
	}
	if strings.TrimSpace(loaded.VaultProvider) == "" {
		loaded.VaultProvider = defaults.VaultProvider
	}
	if loaded.VaultProvider != "vault" && loaded.VaultProvider != "keepass" {
		loaded.VaultProvider = defaults.VaultProvider
	}
	switch strings.ToLower(strings.TrimSpace(loaded.VaultAuthMethod)) {
	case "oidc", "oidc-sec", "domain":
		loaded.VaultAuthMethod = strings.ToLower(strings.TrimSpace(loaded.VaultAuthMethod))
	default:
		loaded.VaultAuthMethod = settings.DefaultVaultAuthMethod
	}
	loaded.VaultLogin = strings.TrimSpace(loaded.VaultLogin)
	loaded.KeePassDatabasePath = strings.TrimSpace(loaded.KeePassDatabasePath)
	loaded.HasVaultToken = s.secretManager.SecretExists(securestorage.VaultTokenKey())
	loaded.HasKeePassPassword = s.secretManager.SecretExists(securestorage.KeePassPasswordKey())
	loaded.HasVaultPassword = s.secretManager.SecretExists(securestorage.VaultPasswordKey())
	loaded.PortForwardRules = normalizePortForwardRules(loaded.PortForwardRules)
	s.settings = loaded
	if persisted.AIState != nil {
		s.aiState = aiStateFromPersisted(*persisted.AIState)
		s.legacyAIChatHistory = append([]ai.ChatMessage(nil), persisted.AIState.LegacyMessages...)
	}
	if history, err := s.loadAIChatHistory(); err != nil {
		return err
	} else if history != nil {
		s.aiState.Messages = history
	} else if len(s.aiState.ChatSessions) == 0 {
		// Older settings files do not persist the session list. Recreate the
		// default application-owned session before new sessions are created.
		s.syncChatSessionLocked()
	}
	return nil
}

func (s *Store) persistAIChatHistoryLocked(messages []ai.ChatMessage) error {
	s.syncChatSessionLocked()
	payload, err := json.Marshal(persistedChatHistory{Sessions: s.aiState.ChatSessions, Legacy: messages, AgentMessages: s.aiState.AgentMessages})
	if err != nil {
		return fmt.Errorf("encode AI chat history: %w", err)
	}
	if err := s.secretManager.StoreSecret(securestorage.AIChatHistoryKey(), string(payload)); err != nil {
		return fmt.Errorf("store encrypted AI chat history: %w", err)
	}
	return nil
}

func (s *Store) loadAIChatHistory() ([]ai.ChatMessage, error) {
	if s.secretManager == nil || !s.secretManager.SecretExists(securestorage.AIChatHistoryKey()) {
		return nil, nil
	}
	payload, err := s.secretManager.LoadSecret(securestorage.AIChatHistoryKey())
	if err != nil {
		if errors.Is(err, securestorage.ErrMasterPasswordRequired) {
			return nil, nil
		}
		return nil, fmt.Errorf("load encrypted AI chat history: %w", err)
	}
	var envelope persistedChatHistory
	if err := json.Unmarshal([]byte(payload), &envelope); err == nil && (envelope.Sessions != nil || envelope.AgentMessages != nil) {
		s.aiState.ChatSessions = cloneChatSessions(envelope.Sessions)
		s.aiState.AgentMessages = append([]ai.ChatMessage(nil), envelope.AgentMessages...)
		if s.aiState.ChatSessionID == "" && len(s.aiState.ChatSessions) > 0 { s.aiState.ChatSessionID = s.aiState.ChatSessions[0].ID }
		return activeChatSessionMessages(s.aiState.ChatSessions, s.aiState.ChatSessionID), nil
	}
	var messages []ai.ChatMessage
	if err := json.Unmarshal([]byte(payload), &messages); err != nil {
		return nil, fmt.Errorf("decode encrypted AI chat history: %w", err)
	}
	return messages, nil
}

func cloneChatSessions(sessions []ai.ChatSession) []ai.ChatSession {
	result := make([]ai.ChatSession, len(sessions))
	for i, session := range sessions { result[i] = session; result[i].Messages = append([]ai.ChatMessage(nil), session.Messages...) }
	return result
}

func activeChatSessionMessages(sessions []ai.ChatSession, id string) []ai.ChatMessage {
	for _, session := range sessions {
		if session.ID == id { return append([]ai.ChatMessage(nil), session.Messages...) }
	}
	return nil
}

func chatMessagesEqual(left, right []ai.ChatMessage) bool {
	return slices.Equal(left, right)
}

func (s *Store) saveSessionProfilesLocked() error {
	profiles := make([]sessions.Profile, 0, len(s.sessionOrder))
	for _, id := range s.sessionOrder {
		profiles = append(profiles, s.sessionProfiles[id])
	}
	data, err := json.MarshalIndent(profiles, "", "  ")
	if err != nil {
		return fmt.Errorf("encode sessions file: %w", err)
	}
	data = append(data, '\n')
	if err := writeFileAtomically(s.sessionsPath, data); err != nil {
		return fmt.Errorf("write sessions file: %w", err)
	}
	return nil
}

func (s *Store) saveSettings() error {
	data, err := json.MarshalIndent(newPersistedSettings(s.settings, s.aiState), "", "  ")
	if err != nil {
		return fmt.Errorf("encode settings file: %w", err)
	}
	data = append(data, '\n')
	if err := writeFileAtomically(s.settingsPath, data); err != nil {
		return fmt.Errorf("write settings file: %w", err)
	}
	return nil
}

func writeFileAtomically(target string, data []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(target), ".eiksy-*")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryName, target)
}

func (s *Store) nextEventIDLocked() string {
	s.eventCounter++
	return fmt.Sprintf("event-%d", s.eventCounter)
}

func ensureJSONFile(path string, defaultContent []byte) error {
	if _, err := os.Stat(path); err == nil {
		return os.Chmod(path, 0o600)
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.WriteFile(path, defaultContent, 0o600)
}

func (s *Store) LockSecureStorage() {
	if s.secretManager != nil {
		s.secretManager.Lock()
	}
	_ = s.ClearAIHistory()
}

func (s *Store) SecureStorageStatus() securestorage.Status {
	if s.secretManager == nil {
		return securestorage.Status{}
	}
	return s.secretManager.Status()
}

func (s *Store) EnsureMasterPassword(password string) error {
	if s.secretManager == nil {
		return fmt.Errorf("secure storage is unavailable")
	}
	err := s.secretManager.EnsureMasterPassword(password)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.settings.HasVaultToken = s.secretManager.SecretExists(securestorage.VaultTokenKey())
	s.settings.HasKeePassPassword = s.secretManager.SecretExists(securestorage.KeePassPasswordKey())
	s.settings.HasVaultPassword = s.secretManager.SecretExists(securestorage.VaultPasswordKey())
	if history, err := s.loadAIChatHistory(); err != nil {
		return err
	} else if history != nil {
		s.aiState.Messages = history
		s.syncChatSessionLocked()
	} else if s.legacyAIChatHistory != nil {
		if err := s.persistAIChatHistoryLocked(s.legacyAIChatHistory); err != nil {
			return err
		}
		s.aiState.Messages = append([]ai.ChatMessage(nil), s.legacyAIChatHistory...)
		s.syncChatSessionLocked()
		s.legacyAIChatHistory = nil
		if err := s.saveSettings(); err != nil {
			return err
		}
	}
	for id, profile := range s.sessionProfiles {
		profile.HasPassword = s.secretManager.SecretExists(securestorage.SessionPasswordKey(id))
		profile.HasKeyPassphrase = s.secretManager.SecretExists(securestorage.SessionKeyPassphraseKey(id))
		s.sessionProfiles[id] = profile
	}
	defer s.mu.Unlock()
	return nil
}

func (s *Store) SecretExists(key string) bool {
	return s.secretManager != nil && s.secretManager.SecretExists(key)
}

func (s *Store) LoadSecret(key string) (string, error) {
	if s.secretManager == nil {
		return "", fmt.Errorf("secure storage is unavailable")
	}
	return s.secretManager.LoadSecret(key)
}

func (s *Store) StoreSecret(key, value string) error {
	if s.secretManager == nil {
		return fmt.Errorf("secure storage is unavailable")
	}
	return s.secretManager.StoreSecret(key, value)
}

func (s *Store) DeleteSecret(key string) error {
	if s.secretManager == nil {
		return nil
	}
	return s.secretManager.DeleteSecret(key)
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
		ContextPolicy: ai.ContextPolicy{SendTerminalSelection: true, SendRecentOutput: false, RequireConfirmation: true},
		CommandPolicy: defaultCommandPolicy(),
		Messages:      []ai.ChatMessage{{Role: "assistant", Content: "Ask for command suggestions or paste terminal errors for analysis."}},
		ChatSessionID: fmt.Sprintf("chat-%d", time.Now().UTC().UnixNano()),
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
		Theme:               "dark",
		DefaultProtocol:     "ssh",
		WindowLayout:        settings.WindowLayout{SidebarWidth: 300, AssistantWidth: 360},
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
	return workspace.Layout{SidebarSections: []workspace.SidebarSection{{ID: "sessions", Title: "Sessions"}, {ID: "files", Title: "SFTP Browser"}}}
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

func newPersistedSettings(app settings.AppSettings, state ai.WorkspaceState) persistedSettings {
	persisted := persistedSettings{
		AppSettings: app,
	}
	persisted.AIState = aiStateToPersisted(state)
	return persisted
}

func normalizePortForwardRules(rules []settings.PortForwardRule) []settings.PortForwardRule {
	normalized := make([]settings.PortForwardRule, 0, len(rules))
	for _, rule := range rules {
		entry := rule
		entry.HostID = strings.TrimSpace(entry.HostID)
		entry.LocalPort = strings.TrimSpace(entry.LocalPort)
		entry.RemoteHost = strings.TrimSpace(entry.RemoteHost)
		entry.RemotePort = strings.TrimSpace(entry.RemotePort)
		normalized = append(normalized, entry)
	}
	return normalized
}

func aiStateToPersisted(state ai.WorkspaceState) *persistedAIWorkspaceState {
	persisted := &persistedAIWorkspaceState{
		ContextPolicy: state.ContextPolicy,
		CommandPolicy: cloneCommandPolicy(state.CommandPolicy),
		ChatSessionID: state.ChatSessionID,
		Providers:     make([]persistedAIProviderDescriptor, 0, len(state.Providers)),
	}
	for _, provider := range state.Providers {
		persisted.Providers = append(persisted.Providers, persistedAIProviderDescriptor{
			ID:          provider.ID,
			Name:        provider.Name,
			Class:       provider.Class,
			Model:       provider.Model,
			Endpoint:    provider.Endpoint,
			DownloadURL: provider.DownloadURL,
			LocalPath:   provider.LocalPath,
			Status:      provider.Status,
			Selected:    provider.Selected,
			Configured:  provider.Configured,
		})
	}
	return persisted
}

func aiStateFromPersisted(persisted persistedAIWorkspaceState) ai.WorkspaceState {
	state := ai.WorkspaceState{
		ContextPolicy: persisted.ContextPolicy,
		CommandPolicy: cloneCommandPolicy(persisted.CommandPolicy),
		ChatSessionID: persisted.ChatSessionID,
		Providers:     make([]ai.ProviderDescriptor, 0, len(persisted.Providers)),
	}
	for _, provider := range persisted.Providers {
		state.Providers = append(state.Providers, ai.ProviderDescriptor{
			ID:          provider.ID,
			Name:        provider.Name,
			Class:       provider.Class,
			Model:       provider.Model,
			Endpoint:    provider.Endpoint,
			DownloadURL: provider.DownloadURL,
			LocalPath:   provider.LocalPath,
			Status:      provider.Status,
			Selected:    provider.Selected,
			Configured:  provider.Configured,
		})
	}
	defaultProviders := defaultAIState().Providers
	for _, defaultProvider := range defaultProviders {
		if providerIndexByID(state.Providers, defaultProvider.ID) >= 0 {
			continue
		}
		state.Providers = append(state.Providers, defaultProvider)
	}
	defaultPolicy := defaultCommandPolicy()
	if len(state.CommandPolicy.Tools) == 0 {
		state.CommandPolicy.Tools = append([]ai.CommandTool(nil), defaultPolicy.Tools...)
	}
	if state.CommandPolicy.CommandRules == nil {
		state.CommandPolicy.CommandRules = []ai.CommandRule{}
	}
	if state.CommandPolicy.PendingRequests == nil {
		state.CommandPolicy.PendingRequests = []ai.CommandRequest{}
	}
	return state
}

func providerIndexByID(providers []ai.ProviderDescriptor, providerID string) int {
	for index, provider := range providers {
		if provider.ID == providerID {
			return index
		}
	}
	return -1
}


const maxPersistentAuditEvents = 500

func (s *Store) auditPath() string { return filepath.Join(s.baseDir, "audit.json") }

func (s *Store) AppendCommandAudit(event ai.CommandAuditEvent) error {
	s.mu.Lock(); defer s.mu.Unlock()
	events, err := s.loadAuditLocked(); if err != nil { return err }
	events = append(events, event)
	if len(events) > maxPersistentAuditEvents { events = events[len(events)-maxPersistentAuditEvents:] }
	data, err := json.MarshalIndent(events, "", "  "); if err != nil { return fmt.Errorf("encode audit file: %w", err) }
	if err := os.WriteFile(s.auditPath(), append(data, '\n'), 0o600); err != nil { return fmt.Errorf("write audit file: %w", err) }
	return nil
}

func (s *Store) CommandAuditTrail() []ai.CommandAuditEvent {
	s.mu.RLock(); defer s.mu.RUnlock()
	events, err := s.loadAuditLocked(); if err != nil { return []ai.CommandAuditEvent{} }
	return events
}

func (s *Store) loadAuditLocked() ([]ai.CommandAuditEvent, error) {
	raw, err := os.ReadFile(s.auditPath())
	if os.IsNotExist(err) { return []ai.CommandAuditEvent{}, nil }
	if err != nil { return nil, fmt.Errorf("read audit file: %w", err) }
	if len(raw) == 0 { return []ai.CommandAuditEvent{}, nil }
	var events []ai.CommandAuditEvent
	if err := json.Unmarshal(raw, &events); err != nil { return nil, fmt.Errorf("decode audit file: %w", err) }
	if len(events) > maxPersistentAuditEvents { events = events[len(events)-maxPersistentAuditEvents:] }
	return events, nil
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
	if err := s.persistAIChatHistoryLocked(nil); err != nil { return ai.ChatSession{}, err }
	if err := s.saveSettings(); err != nil { return ai.ChatSession{}, err }
	return session, nil
}
