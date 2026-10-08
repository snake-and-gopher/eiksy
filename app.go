package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strconv"

	"eiksy/internal/app"
	"eiksy/internal/debuglog"
	"eiksy/internal/domain/ai"
	"eiksy/internal/domain/sessions"
	"eiksy/internal/domain/settings"
	sftpdomain "eiksy/internal/domain/sftp"
	"eiksy/internal/securestorage"
	sftpmanager "eiksy/internal/sftp"
	sshmanager "eiksy/internal/ssh"
	"eiksy/internal/storage/disk"
	"eiksy/internal/storage/memory"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type App struct {
	ctx     context.Context
	service *app.Service
}

func NewApp() *App { return &App{} }

func (a *App) startup(ctx context.Context) {
	debuglog.Printf("startup: begin; debug log=%q", debuglog.Path())
	a.ctx = ctx
	debuglog.Printf("startup: context initialized")

	debuglog.Printf("startup: initializing disk store")
	store, storeErr := disk.NewStore()
	if storeErr != nil {
		debuglog.Printf("startup: disk store initialization failed: %v", storeErr)
		log.Printf("disk store unavailable, falling back to memory store: %v", storeErr)
		store = nil
	} else {
		debuglog.Printf("startup: disk store initialized successfully")
	}

	if store != nil {
		a.service = app.NewService(store, sshmanager.NewManager(), sftpmanager.NewManager())
	} else {
		a.service = app.NewService(memory.NewStore(), sshmanager.NewManager(), sftpmanager.NewManager())
	}
	debuglog.Printf("startup: service initialized; persistent=%t", store != nil)

	a.service.SetRuntimeContext(ctx, func(eventName string, data ...interface{}) {
		runtime.EventsEmit(ctx, eventName, data...)
	})
	debuglog.Printf("startup: runtime context configured")

	if storeErr != nil {
		a.service.EmitLog("warn", "Disk store unavailable; running with in-memory defaults.")
	} else {
		a.service.EmitLog("info", "Application started with disk storage.")
		a.autoImportInitialSSHConfig()
	}
	debuglog.Printf("startup: complete")
}

func (a *App) restoreWindow(ctx context.Context) {
	service := a.currentService()
	state := service.GetShellState().Settings.WindowState
	if !state.Saved {
		// First launch: use the available screen/work area instead of a fixed size.
		runtime.WindowMaximise(ctx)
		return
	}
	if state.Maximized {
		runtime.WindowMaximise(ctx)
		return
	}
	if state.Width > 0 && state.Height > 0 {
		runtime.WindowSetSize(ctx, state.Width, state.Height)
	}
	runtime.WindowSetPosition(ctx, state.X, state.Y)
}

func (a *App) saveWindowState(ctx context.Context) {
	if a.service == nil {
		return
	}
	state := a.service.GetShellState().Settings
	width, height := runtime.WindowGetSize(ctx)
	x, y := runtime.WindowGetPosition(ctx)
	state.WindowState = settings.WindowState{
		Width:     width,
		Height:    height,
		X:         x,
		Y:         y,
		Maximized: runtime.WindowIsMaximised(ctx),
		Saved:     true,
	}
	if err := a.service.UpdateSettings(state); err != nil {
		debuglog.Printf("shutdown: unable to save window state: %v", err)
	}
}

func (a *App) beforeClose(ctx context.Context) bool {
	a.saveWindowState(ctx)
	return false
}

func (a *App) GetShellState() app.ShellState {
	debuglog.Printf("wails: GetShellState called")
	state := a.currentService().GetShellState()
	debuglog.Printf("wails: GetShellState completed; profiles=%d activeSessions=%d history=%d", len(state.SessionProfiles), len(state.ActiveSessions), len(state.SessionHistory))
	return state
}
func (a *App) GetReleaseVersion() string {
	version := resolveReleaseVersion()
	debuglog.Printf("wails: GetReleaseVersion called; version=%q", version)
	return version
}
func (a *App) LaunchSession(profileID string) (app.RuntimeSessionView, error) { return a.currentService().LaunchSession(profileID) }
func (a *App) CloseSession(sessionID string) error { return a.currentService().CloseSession(sessionID) }
func (a *App) ConnectSession(sessionID string) error { return a.currentService().ConnectSession(sessionID) }
func (a *App) DisconnectSession(sessionID string) error { return a.currentService().DisconnectSession(sessionID) }
func (a *App) ReconnectSession(sessionID string) error { return a.currentService().ReconnectSession(sessionID) }
func (a *App) ExecuteCommand(sessionID, command string) (sessions.CommandExecutionResult, error) { return a.currentService().ExecuteCommand(sessionID, command) }
func (a *App) CreateSessionProfile(input sessions.ProfileInput) error { return a.currentService().CreateSessionProfileInput(input) }
func (a *App) DeleteSessionProfile(id string) error { return a.currentService().DeleteSessionProfile(id) }
func (a *App) ConnectSSH(tabID, profileID string) error { return a.currentService().ConnectSSH(a.ctx, tabID, profileID) }
func (a *App) SendSSHInput(tabID, data string) error { return a.currentService().SendSSHInput(tabID, data) }
func (a *App) ResizeTerminal(tabID string, cols, rows int) error { return a.currentService().ResizeTerminal(tabID, cols, rows) }
func (a *App) DisconnectSSH(tabID string) error { return a.currentService().DisconnectSSH(tabID) }
func (a *App) ListSFTPFiles(tabID, path string) ([]sftpdomain.FileEntry, error) { return a.currentService().ListSFTPFiles(tabID, path) }
func (a *App) NavigateSFTP(tabID, path string) ([]sftpdomain.FileEntry, error) { return a.currentService().NavigateSFTP(tabID, path) }
func (a *App) ReadSFTPFile(tabID, path string) (string, error) { return a.currentService().ReadSFTPFile(tabID, path) }
func (a *App) SaveSFTPFile(tabID, path, content string) error { return a.currentService().SaveSFTPFile(tabID, path, content) }
func (a *App) SelectUploadFiles() ([]string, error) { return runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{Title: "Select file(s) to upload"}) }
func (a *App) SelectDownloadDirectory() (string, error) { return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: "Select folder for downloaded files"}) }
func (a *App) UploadSFTPFiles(tabID, remoteDir string, localPaths []string) error { return a.currentService().UploadSFTPFiles(tabID, remoteDir, localPaths) }
func (a *App) DownloadSFTPFiles(tabID, localDir string, remotePaths []string) error { return a.currentService().DownloadSFTPFiles(tabID, localDir, remotePaths) }
func (a *App) ValidateCredentialPath(provider, secretPath string) error { return a.currentService().ValidateCredentialPath(provider, secretPath) }
func (a *App) ImportSSHConfig(raw string) ([]sessions.Profile, error) { return a.currentService().ImportSSHConfig(raw) }
func (a *App) GetSecureStorageStatus() securestorage.Status { return a.currentService().GetSecureStorageStatus() }
func (a *App) EnsureMasterPassword(password string) error { return a.currentService().EnsureMasterPassword(password) }
func (a *App) LockSecureStorage() { a.currentService().LockSecureStorage() }
func (a *App) UpdateSettings(input settings.AppSettings) error { return a.currentService().UpdateSettings(input) }
func (a *App) SelectAIProvider(providerID string) error { return a.currentService().SelectAIProvider(providerID) }
func (a *App) SaveCloudProvider(model, endpoint, token string) error { return a.currentService().SaveCloudProvider(model, endpoint, token) }
func (a *App) SaveLocalProvider(downloadURL string) error { return a.currentService().SaveLocalProvider(downloadURL) }
func (a *App) DownloadLocalModel(downloadURL string) error { return a.currentService().DownloadLocalModel(downloadURL) }
func (a *App) StartLocalModel() error { return a.currentService().StartLocalModel() }
func (a *App) StopLocalModel() { a.currentService().StopLocalModel() }
func (a *App) ListCloudModels(endpoint, token string) ([]string, error) { return a.currentService().ListCloudModels(endpoint, token) }
func (a *App) StartCloudProviderAuth(endpoint string) (app.CloudProviderAuthSession, error) { return a.currentService().StartCloudProviderAuth(endpoint) }
func (a *App) GetCloudProviderAuthSession(sessionID string) (app.CloudProviderAuthSession, error) { return a.currentService().GetCloudProviderAuthSession(sessionID) }
func (a *App) SendChatMessage(message, activeSessionID string) error {
	message, contextSessionID := prepareAIChatMessage(message, activeSessionID)
	return a.currentService().SendChatMessage(a.ctx, message, contextSessionID)
}

// SendPlainChatMessage is the sidebar's ordinary model-chat endpoint. It accepts
// no session identifier and cannot invoke Eiksy command tools.
func (a *App) SendPlainChatMessage(message string) error {
	return a.currentService().SendPlainChatMessage(a.ctx, message)
}
func (a *App) ClearChat() error { return a.currentService().ClearChat() }
func (a *App) UpdateCommandPolicy(policy ai.CommandPolicy) error { return a.currentService().UpdateCommandPolicy(policy) }

func (a *App) ResolveCommandPolicyRequest(requestID, mode string) error {
	service := a.currentService()
	state := service.GetShellState().AI
	var request ai.CommandRequest
	for _, pending := range state.CommandPolicy.PendingRequests {
		if pending.ID == requestID {
			request = pending
			break
		}
	}
	permissionMode := ai.CommandPermissionMode(mode)
	err := service.ResolveCommandPolicyRequest(requestID, permissionMode)
	if request.ID != "" {
		service.RecordCommandPolicyResolutionForApp(request, permissionMode, err)
	}
	return err
}

func (a *App) GetCommandAuditTrail() []ai.CommandAuditEvent { return a.currentService().GetCommandAuditTrail() }
func (a *App) AcceptSSHHostKey(tabID string) error { return a.currentService().AcceptSSHHostKey(tabID) }

func (a *App) currentService() *app.Service {
	if a.service == nil {
		debuglog.Printf("currentService: service was nil; creating memory service")
		svc := app.NewService(memory.NewStore(), sshmanager.NewManager(), sftpmanager.NewManager())
		svc.SetRuntimeContext(a.ctx, func(eventName string, data ...interface{}) { runtime.EventsEmit(a.ctx, eventName, data...) })
		a.service = svc
	}
	return a.service
}

func (a *App) autoImportInitialSSHConfig() {
	debuglog.Printf("startup: checking automatic SSH config import")
	state := a.service.GetShellState()
	if len(state.SessionProfiles) > 0 || state.Settings.SSHConfigAutoLoaded { debuglog.Printf("startup: SSH auto-import not needed"); return }
	settingsSnapshot := state.Settings
	settingsSnapshot.SSHConfigAutoLoaded = true
	if err := a.service.UpdateSettings(settingsSnapshot); err != nil { debuglog.Printf("startup: unable to persist SSH auto-import marker: %v", err); a.service.EmitLog("warn", "Unable to persist SSH auto-import marker.") }
	homeDir, err := os.UserHomeDir()
	if err != nil || homeDir == "" { debuglog.Printf("startup: unable to resolve home directory: %v", err); return }
	configPath := filepath.Join(homeDir, ".ssh", "config")
	debuglog.Printf("startup: checking SSH config path %q", configPath)
	content, err := os.ReadFile(configPath)
	if err != nil || len(content) == 0 { debuglog.Printf("startup: SSH config unavailable or empty: %v", err); return }
	imported, err := a.service.ImportSSHConfig(string(content))
	if err != nil { debuglog.Printf("startup: SSH config import failed: %v", err); a.service.EmitLog("warn", "Automatic SSH config import skipped: "+err.Error()); return }
	a.service.EmitLog("info", "Automatically imported "+strconv.Itoa(len(imported))+" SSH session(s) from ~/.ssh/config.")
	debuglog.Printf("startup: imported %d SSH session(s)", len(imported))
}
