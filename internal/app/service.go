package app

import (
	"bytes"
	"fmt"
	"context"
	"net/http"
	"os/exec"
	"sync"
	"time"

	"eiksy/internal/domain/ai"
	"eiksy/internal/domain/protocols"
	"eiksy/internal/domain/sessions"
	"eiksy/internal/domain/settings"
	sftpdomain "eiksy/internal/domain/sftp"
	"eiksy/internal/domain/workspace"
	"eiksy/internal/securestorage"
	"eiksy/internal/localexec"
	sshmanager "eiksy/internal/ssh"
	sftpmanager "eiksy/internal/sftp"
)

type ShellState struct {
	Protocols           []protocols.Descriptor           `json:"protocols"`
	SessionProfiles     []sessions.Profile               `json:"sessionProfiles"`
	ActiveSessions      []RuntimeSessionView             `json:"activeSessions"`
	SessionHistory      []sessions.HistoryEntry          `json:"sessionHistory"`
	AI                  ai.WorkspaceState                `json:"ai"`
	Workspace           WorkspaceView                    `json:"workspace"`
	Settings            settings.AppSettings             `json:"settings"`
}

type WorkspaceView struct {
	Layout       workspace.Layout  `json:"layout"`
	RecentEvents []workspace.Event `json:"recentEvents"`
}

type RuntimeSessionView struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	ProtocolID  string `json:"protocolId"`
	ProfileID   string `json:"profileId"`
	Status      string `json:"status"`
	Description string `json:"description"`
}

type CloudProviderAuthSession struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	AuthURL  string `json:"authUrl,omitempty"`
	Message  string `json:"message,omitempty"`
	Endpoint string `json:"endpoint,omitempty"`
}

const (
	vaultAuthMethodToken   = "token"
	vaultAuthMethodOIDC    = "oidc"
	vaultAuthMethodOIDCSec = "oidc-sec"
	vaultAuthMethodDomain  = "domain"
	localAIProviderID      = "local-qwen3-4b"
	localAIEndpoint        = "http://127.0.0.1:8012/v1"
	localAIHost            = "127.0.0.1"
	localAIPort            = "8012"
)

type Service struct {
	ctx             context.Context
	store           stateStore
	sshManager      sshManager
	sftpManager     sftpManager
	httpClient      *http.Client
	emitFn          func(eventName string, data ...interface{})
	authMu          sync.Mutex
	cloudAuth       *cloudAuthSession
	localAIMu       sync.Mutex
	localAICmd      *exec.Cmd
	localAIDone     chan error
	localAIErr      *bytes.Buffer
	localAIStopping bool
	localExecutor *localexec.Executor
}

type stateStore interface {
	Protocols() []protocols.Descriptor
	SessionProfiles() []sessions.Profile
	SessionProfile(string) (sessions.Profile, bool)
	LaunchHistory() []sessions.HistoryEntry
	AIState() ai.WorkspaceState
	Settings() settings.AppSettings
	WorkspaceLayout() workspace.Layout
	RuntimeTabs() []workspace.Tab
	Events() []workspace.Event
	UpdateAIState(ai.WorkspaceState) error
	ListChatSessions() []ai.ChatSession
	CreateChatSession(string) (ai.ChatSession, error)
	SelectChatSession(string) error
	ForkChatSession(string, string) (ai.ChatSession, error)
	ClearAIHistory() error
	UpdateSettings(settings.AppSettings) error
	SecureStorageStatus() securestorage.Status
	EnsureMasterPassword(string) error
	LockSecureStorage()
	SecretExists(string) bool
	LoadSecret(string) (string, error)
	StoreSecret(string, string) error
	DeleteSecret(string) error
	OpenRuntimeTab(workspace.Tab)
	CloseRuntimeTab(string) bool
	RecordLaunch(string) error
}

type sessionProfileMutator interface {
	UpsertSessionProfile(sessions.Profile) error
	DeleteSessionProfile(string) error
}

type sshManager interface {
	Connect(context.Context, string, string, int, string, string, map[string]string) error
	SendInput(string, string) error
	ResizeTerminal(string, int, int) error
	Disconnect(string) error
	SetOutputHandler(string, func(data string))
	GetCurrentDir(string) (string, error)
	AcceptHostKey(string) error
}

type sftpManager interface {
	Connect(context.Context, string, string, int, string, string, map[string]string) error
	Connected(string) bool
	ListDir(string, string) ([]sftpdomain.FileEntry, error)
	Stat(string, string) (sftpdomain.FileEntry, error)
	ReadFile(string, string) (string, error)
	WriteFile(string, string, string) error
	UploadFile(string, string, string) error
	DownloadFile(string, string, string) error
	Disconnect(string) error
}

func NewService(store stateStore, sshManager sshManager, sftpManager sftpManager) *Service {
	service := &Service{
		store:       store,
		sshManager:  sshManager,
		sftpManager: sftpManager,
		httpClient:  &http.Client{Timeout: 120 * time.Second},
		emitFn:      func(string, ...interface{}) {},
	}
	return service
}

// NewLocalAgentService creates an agent-only service with local execution and
// deliberately no SSH or SFTP managers. Commands run as the current OS user.
func NewLocalAgentService(store stateStore) (*Service, error) {
	executor, err := localexec.New()
	if err != nil { return nil, err }
	return &Service{store: store, emitFn: func(string, ...interface{}) {}, localExecutor: executor}, nil
}

// NewTUIService creates the application service with only the dependencies
// required by the non-runtime terminal UI. It does not initialize SSH/SFTP.
func NewTUIService(store stateStore) *Service {
	return &Service{
		store:  store,
		emitFn: func(string, ...interface{}) {},
	}
}

// NewTUIRuntimeService creates the application service used by the interactive
// TUI. It shares the same SSH/SFTP runtime managers as the GUI instead of
// keeping protocol execution in the TUI layer.
func NewTUIRuntimeService(store stateStore) *Service {
	return NewService(store, sshmanager.NewManager(), sftpmanager.NewManager())
}

func (s *Service) SetRuntimeContext(ctx context.Context, emitFn func(eventName string, data ...interface{})) {
	s.ctx = ctx
	if emitFn != nil {
		s.emitFn = emitFn
	}
}

func (s *Service) EmitLog(level, message string) {
	timestamp := time.Now().UTC().Format(time.RFC3339)
	s.emitFn("app:log", map[string]string{
		"level":   level,
		"message": message,
		"time":    timestamp,
	})
}

func (s *Service) LockSecureStorage() {
	// Close all live protocol connections before locking credentials. This
	// prevents already-authenticated sessions from surviving a credential lock.
	for _, tab := range s.store.RuntimeTabs() {
		if s.sshManager != nil {
			if err := s.sshManager.Disconnect(tab.ID); err != nil {
				s.emitFn("app:log", map[string]string{"level": "error", "message": fmt.Sprintf("disconnect SSH session %s during secure-storage lock: %v", tab.ID, err)})
			}
		}
		if s.sftpManager != nil {
			if err := s.sftpManager.Disconnect(tab.ID); err != nil {
				s.emitFn("app:log", map[string]string{"level": "error", "message": fmt.Sprintf("disconnect SFTP session %s during secure-storage lock: %v", tab.ID, err)})
			}
		}
	}
	s.store.LockSecureStorage()
	state := s.store.AIState()
	state.PendingNativeToolCall = nil
	state.CommandPolicy.PendingRequests = nil
	if err := s.store.UpdateAIState(state); err != nil {
		s.emitFn("app:log", map[string]string{"level": "error", "message": fmt.Sprintf("persist AI state after secure-storage lock: %v", err)})
	}
	if err := s.store.ClearAIHistory(); err != nil {
		s.emitFn("app:log", map[string]string{"level": "error", "message": fmt.Sprintf("clear AI history from memory after secure-storage lock: %v", err)})
	}
	s.emitFn("secure-storage:status", map[string]bool{"unlocked": false})
}

func (s *Service) GetShellState() ShellState {
	tabs := s.store.RuntimeTabs()
	activeSessions := make([]RuntimeSessionView, 0, len(tabs))
	for _, tab := range tabs {
		activeSessions = append(activeSessions, RuntimeSessionView(tab))
	}
	profiles := s.scrubProfilesForShell(s.store.SessionProfiles())
	aiState := s.scrubAIStateForShell(s.store.AIState())
	appSettings := s.scrubSettingsForShell(s.store.Settings())
	protocolDescriptors := s.store.Protocols()
	history := s.store.LaunchHistory()
	events := s.store.Events()

	// Keep the Wails API contract stable for empty collections. Go's nil slices
	// are encoded as JSON null, while the frontend expects arrays and calls
	// array methods such as map/filter on these values.
	if protocolDescriptors == nil {
		protocolDescriptors = []protocols.Descriptor{}
	}
	if profiles == nil {
		profiles = []sessions.Profile{}
	}
	if history == nil {
		history = []sessions.HistoryEntry{}
	}
	if events == nil {
		events = []workspace.Event{}
	}

	return ShellState{
		Protocols:       protocolDescriptors,
		SessionProfiles: profiles,
		ActiveSessions:  activeSessions,
		SessionHistory:  history,
		AI:              aiState,
		Workspace: WorkspaceView{
			Layout:       s.store.WorkspaceLayout(),
			RecentEvents: events,
		},
		Settings: appSettings,
	}
}
func (s *Service) resolveContext(ctx context.Context) context.Context {
	if ctx != nil {
		return ctx
	}
	if s.ctx != nil {
		return s.ctx
	}
	return context.Background()
}

func (s *Service) GetSettings() settings.AppSettings {
	return s.store.Settings()
}

func (s *Service) GetSecureStorageStatus() securestorage.Status {
	return s.store.SecureStorageStatus()
}

func (s *Service) EnsureMasterPassword(password string) error {
	return s.store.EnsureMasterPassword(password)
}
