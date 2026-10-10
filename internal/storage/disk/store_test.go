package disk

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"eiksy/internal/app"
	"eiksy/internal/domain/ai"
	"eiksy/internal/domain/sessions"
	"eiksy/internal/domain/settings"
	"eiksy/internal/securestorage"

	"github.com/zalando/go-keyring"
	_ "modernc.org/sqlite"
)

type memoryKeyring struct {
	items map[string]string
}

func newMemoryKeyring() *memoryKeyring {
	return &memoryKeyring{items: map[string]string{}}
}

func (m *memoryKeyring) key(service, user string) string {
	return service + ":" + user
}

func (m *memoryKeyring) Get(service, user string) (string, error) {
	value, ok := m.items[m.key(service, user)]
	if !ok {
		return "", keyring.ErrNotFound
	}
	return value, nil
}

func (m *memoryKeyring) Set(service, user, value string) error {
	m.items[m.key(service, user)] = value
	return nil
}

func (m *memoryKeyring) Delete(service, user string) error {
	delete(m.items, m.key(service, user))
	return nil
}

func TestSecretsMoveToEncryptedSQLiteStorage(t *testing.T) {
	baseDir := t.TempDir()
	keyring := newMemoryKeyring()

	store, err := NewStoreAtWithKeyring(baseDir, keyring)
	if err != nil {
		t.Fatalf("create disk store: %v", err)
	}
	service := app.NewService(store, nil, nil)
	if status := service.GetSecureStorageStatus(); !status.Available || status.Configured || status.Unlocked {
		t.Fatalf("unexpected initial secure storage status: %+v", status)
	}
	if err := service.EnsureMasterPassword("master-password"); err != nil {
		t.Fatalf("ensure master password: %v", err)
	}

	cfg := store.Settings()
	cfg.VaultAddress = "https://vault.example.com"
	cfg.VaultMountPoint = "secret"
	cfg.VaultAutoRenewToken = true
	cfg.VaultProvider = "keepass"
	cfg.KeePassDatabasePath = "/tmp/keepass.kdbx"
	cfg.KeePassPassword = "keepass-secret"
	cfg.VaultToken = "vault-secret-token"
	cfg.VaultPassword = "vault-login-password"
	if err := service.UpdateSettings(cfg); err != nil {
		t.Fatalf("update settings: %v", err)
	}
	if err := service.SaveCloudProvider("gpt-5.6", "https://models.example.com/v1", "ai-secret-token"); err != nil {
		t.Fatalf("save cloud provider: %v", err)
	}
	if err := service.CreateSessionProfileInput(sessions.ProfileInput{
		ID: "prod-ssh", Name: "prod-ssh", ProtocolID: "ssh", Host: "prod.internal", Port: 22, Username: "ops",
		Password: "session-password", KeyPassphrase: "ssh-key-passphrase",
		Options: map[string]string{"auth_method": "key", "ssh_private_key_path": "~/.ssh/id_ed25519"},
	}); err != nil {
		t.Fatalf("create session profile: %v", err)
	}

	settingsPath := filepath.Join(baseDir, "settings.json")
	rawSettings, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read settings file: %v", err)
	}
	if strings.Contains(string(rawSettings), "vault-secret-token") || strings.Contains(string(rawSettings), "vault-login-password") || strings.Contains(string(rawSettings), "keepass-secret") || strings.Contains(string(rawSettings), "ai-secret-token") {
		t.Fatalf("settings.json must not contain plaintext secrets: %s", rawSettings)
	}
	var persistedSettings map[string]any
	if err := json.Unmarshal(rawSettings, &persistedSettings); err != nil {
		t.Fatalf("decode settings file: %v", err)
	}
	if _, ok := persistedSettings["vaultToken"]; ok {
		t.Fatalf("vault token must not be serialized to settings.json")
	}
	if _, ok := persistedSettings["vaultPassword"]; ok {
		t.Fatalf("vault password must not be serialized to settings.json")
	}
	if _, ok := persistedSettings["keepassPassword"]; ok {
		t.Fatalf("keepass password must not be serialized to settings.json")
	}
	aiStateRaw := persistedSettings["aiState"].(map[string]any)
	firstProvider := aiStateRaw["providers"].([]any)[0].(map[string]any)
	if _, ok := firstProvider["token"]; ok {
		t.Fatalf("AI token must not be serialized to settings.json")
	}

	sessionsPath := filepath.Join(baseDir, "sessions.json")
	rawSessions, err := os.ReadFile(sessionsPath)
	if err != nil {
		t.Fatalf("read sessions file: %v", err)
	}
	if strings.Contains(string(rawSessions), "session-password") || strings.Contains(string(rawSessions), "ssh-key-passphrase") {
		t.Fatalf("sessions.json must not contain plaintext secrets: %s", rawSessions)
	}
	var persistedProfiles []map[string]any
	if err := json.Unmarshal(rawSessions, &persistedProfiles); err != nil {
		t.Fatalf("decode sessions file: %v", err)
	}
	if len(persistedProfiles) != 1 {
		t.Fatalf("expected one persisted session profile, got %d", len(persistedProfiles))
	}
	if _, ok := persistedProfiles[0]["password"]; ok {
		t.Fatalf("password must not be serialized to sessions.json")
	}
	if _, ok := persistedProfiles[0]["keyPassphrase"]; ok {
		t.Fatalf("key passphrase must not be serialized to sessions.json")
	}

	db, err := sql.Open("sqlite", filepath.Join(baseDir, "secrets.db"))
	if err != nil {
		t.Fatalf("open secrets db: %v", err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT name, value FROM secrets ORDER BY name`)
	if err != nil {
		t.Fatalf("query secrets db: %v", err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name, value string
		if err := rows.Scan(&name, &value); err != nil {
			t.Fatalf("scan secret row: %v", err)
		}
		names = append(names, name)
		if strings.Contains(value, "vault-secret-token") || strings.Contains(value, "vault-login-password") || strings.Contains(value, "keepass-secret") || strings.Contains(value, "ai-secret-token") || strings.Contains(value, "session-password") || strings.Contains(value, "ssh-key-passphrase") {
			t.Fatalf("secret row %q contains plaintext payload: %s", name, value)
		}
	}
	if len(names) != 6 {
		t.Fatalf("expected 6 encrypted secrets, got %d (%v)", len(names), names)
	}
	foundChatHistory := false
	for _, name := range names {
		if name == securestorage.AIChatHistoryKey() {
			foundChatHistory = true
			break
		}
	}
	if !foundChatHistory {
		t.Fatalf("expected encrypted AI chat history secret, got %v", names)
	}

	reloaded, err := NewStoreAtWithKeyring(baseDir, keyring)
	if err != nil {
		t.Fatalf("reload disk store: %v", err)
	}
	if status := reloaded.SecureStorageStatus(); !status.Available || !status.Configured || status.Unlocked {
		t.Fatalf("unexpected reloaded secure storage status: %+v", status)
	}
	if _, err := reloaded.LoadSecret(securestorage.VaultTokenKey()); err != securestorage.ErrMasterPasswordRequired {
		t.Fatalf("expected locked vault token to require master password, got %v", err)
	}
	if err := reloaded.EnsureMasterPassword("master-password"); err != nil {
		t.Fatalf("unlock reloaded secure storage: %v", err)
	}
	vaultToken, err := reloaded.LoadSecret(securestorage.VaultTokenKey())
	if err != nil {
		t.Fatalf("load reloaded vault token: %v", err)
	}
	if vaultToken != "vault-secret-token" {
		t.Fatalf("expected reloaded vault token, got %q", vaultToken)
	}
	keepassPassword, err := reloaded.LoadSecret(securestorage.KeePassPasswordKey())
	if err != nil {
		t.Fatalf("load reloaded keepass password: %v", err)
	}
	if keepassPassword != "keepass-secret" {
		t.Fatalf("expected reloaded keepass password, got %q", keepassPassword)
	}
	vaultPassword, err := reloaded.LoadSecret(securestorage.VaultPasswordKey())
	if err != nil {
		t.Fatalf("load reloaded vault password: %v", err)
	}
	if vaultPassword != "vault-login-password" {
		t.Fatalf("expected reloaded vault password, got %q", vaultPassword)
	}
	aiToken, err := reloaded.LoadSecret(securestorage.AIProviderTokenKey("openai-compatible-cloud"))
	if err != nil {
		t.Fatalf("load reloaded ai token: %v", err)
	}
	if aiToken != "ai-secret-token" {
		t.Fatalf("expected reloaded ai token, got %q", aiToken)
	}
	reloadedAI := reloaded.AIState()
	if len(reloadedAI.Providers) == 0 || !reloadedAI.Providers[0].HasToken {
		t.Fatal("expected AI token presence to be restored from encrypted storage after restart")
	}
	if reloadedAI.Providers[0].Token != "" {
		t.Fatal("AI token value must never be exposed in the settings state")
	}
	profilePassword, err := reloaded.LoadSecret(securestorage.SessionKeyPassphraseKey("prod-ssh"))
	if err != nil {
		t.Fatalf("load reloaded session passphrase: %v", err)
	}
	if profilePassword != "ssh-key-passphrase" {
		t.Fatalf("expected reloaded session passphrase, got %q", profilePassword)
	}
	profile := reloaded.SessionProfiles()[0]
	if !profile.HasKeyPassphrase {
		t.Fatal("expected persisted profile to advertise stored key passphrase")
	}
	if profile.HasPassword {
		t.Fatal("password secret should be removed when auth method is key")
	}
}

func TestUpdateSettingsStoresSecretFlagsWithoutPlaintext(t *testing.T) {
	baseDir := t.TempDir()
	store, err := NewStoreAtWithKeyring(baseDir, newMemoryKeyring())
	if err != nil {
		t.Fatalf("create disk store: %v", err)
	}
	if err := store.EnsureMasterPassword("master-password"); err != nil {
		t.Fatalf("ensure master password: %v", err)
	}
	service := app.NewService(store, nil, nil)

	cfg := settings.AppSettings{
		VaultAddress:    "https://vault.example.com",
		VaultProvider:   "vault",
		VaultAuthMethod: "domain",
		VaultLogin:      "CORP\\ops",
		VaultToken:      "vault-token",
		VaultPassword:   "vault-password",
	}
	if err := service.UpdateSettings(cfg); err != nil {
		t.Fatalf("update settings: %v", err)
	}
	stored := service.GetShellState().Settings
	if !stored.HasVaultToken {
		t.Fatal("expected shell settings to advertise encrypted vault token")
	}
	if stored.VaultToken != "" {
		t.Fatalf("vault token must be scrubbed from shell state, got %q", stored.VaultToken)
	}
	if !stored.HasVaultPassword {
		t.Fatal("expected shell settings to advertise encrypted vault password")
	}
	if stored.VaultPassword != "" {
		t.Fatalf("vault password must be scrubbed from shell state, got %q", stored.VaultPassword)
	}
}


func TestAIChatHistoryIsEncryptedAndReloaded(t *testing.T) {
	baseDir := t.TempDir()
	keyring := newMemoryKeyring()
	store, err := NewStoreAtWithKeyring(baseDir, keyring)
	if err != nil {
		t.Fatalf("create disk store: %v", err)
	}
	if err := store.EnsureMasterPassword("master-password"); err != nil {
		t.Fatalf("unlock secure storage: %v", err)
	}

	state := store.AIState()
	state.Messages = []ai.ChatMessage{
		{Role: "user", Content: "private terminal output"},
		{Role: "assistant", Content: "private AI response"},
	}
	if err := store.UpdateAIState(state); err != nil {
		t.Fatalf("persist AI state: %v", err)
	}

	rawSettings, err := os.ReadFile(filepath.Join(baseDir, "settings.json"))
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	if strings.Contains(string(rawSettings), "private terminal output") || strings.Contains(string(rawSettings), "private AI response") {
		t.Fatal("settings.json must not contain AI chat history")
	}

	db, err := sql.Open("sqlite", filepath.Join(baseDir, "secrets.db"))
	if err != nil {
		t.Fatalf("open secrets db: %v", err)
	}
	defer db.Close()
	var storedHistory string
	if err := db.QueryRow("SELECT value FROM secrets WHERE name = ?", securestorage.AIChatHistoryKey()).Scan(&storedHistory); err != nil {
		t.Fatalf("read encrypted chat history: %v", err)
	}
	if strings.Contains(storedHistory, "private terminal output") || strings.Contains(storedHistory, "private AI response") {
		t.Fatal("secrets.db must not contain plaintext AI chat history")
	}

	reloaded, err := NewStoreAtWithKeyring(baseDir, keyring)
	if err != nil {
		t.Fatalf("reload disk store: %v", err)
	}
	if err := reloaded.EnsureMasterPassword("master-password"); err != nil {
		t.Fatalf("unlock reloaded secure storage: %v", err)
	}
	reloadedState := reloaded.AIState()
	if len(reloadedState.Messages) != 2 || reloadedState.Messages[0].Content != "private terminal output" || reloadedState.Messages[1].Content != "private AI response" {
		t.Fatalf("unexpected reloaded AI history: %+v", reloadedState.Messages)
	}
}



func TestAIChatHistoryClearedFromMemoryWhenSecureStorageLocks(t *testing.T) {
	baseDir := t.TempDir()
	keyring := newMemoryKeyring()
	store, err := NewStoreAtWithKeyring(baseDir, keyring)
	if err != nil {
		t.Fatalf("create disk store: %v", err)
	}
	if err := store.EnsureMasterPassword("master-password"); err != nil {
		t.Fatalf("unlock secure storage: %v", err)
	}

	state := store.AIState()
	state.Messages = []ai.ChatMessage{
		{Role: "user", Content: "private terminal output"},
		{Role: "assistant", Content: "private AI response"},
	}
	if err := store.UpdateAIState(state); err != nil {
		t.Fatalf("persist AI state: %v", err)
	}

	store.LockSecureStorage()
	if messages := store.AIState().Messages; len(messages) != 0 {
		t.Fatalf("expected AI history to be cleared from memory after lock, got %+v", messages)
	}

	if err := store.EnsureMasterPassword("master-password"); err != nil {
		t.Fatalf("unlock secure storage: %v", err)
	}
	restored := store.AIState().Messages
	if len(restored) != 2 || restored[0].Content != "private terminal output" || restored[1].Content != "private AI response" {
		t.Fatalf("expected encrypted history to survive lock/unlock, got %+v", restored)
	}
}

func TestUpdateAIStateReturnsPersistenceErrorAndRollsBack(t *testing.T) {
	baseDir := t.TempDir()
	store, err := NewStoreAtWithKeyring(baseDir, newMemoryKeyring())
	if err != nil {
		t.Fatalf("create disk store: %v", err)
	}

	before := store.AIState()
	settingsPath := filepath.Join(baseDir, "settings.json")
	if err := os.Remove(settingsPath); err != nil {
		t.Fatalf("remove settings file: %v", err)
	}
	if err := os.Mkdir(settingsPath, 0o700); err != nil {
		t.Fatalf("create settings directory: %v", err)
	}

	after := before
	after.ChatSessionID = "changed"
	if err := store.UpdateAIState(after); err == nil {
		t.Fatal("expected AI state persistence error")
	}

	current := store.AIState()
	if current.ChatSessionID != before.ChatSessionID {
		t.Fatalf("AI state changed after failed persistence: got %q, want %q", current.ChatSessionID, before.ChatSessionID)
	}
}

func TestCommandAuditPersistsAcrossStoreReload(t *testing.T) {
	baseDir := t.TempDir()
	keyring := newMemoryKeyring()
	store, err := NewStoreAtWithKeyring(baseDir, keyring)
	if err != nil { t.Fatalf("create disk store: %v", err) }
	event := ai.CommandAuditEvent{ID: "audit-1", At: "2026-09-18T00:00:00Z", ToolID: "ssh.exec", SessionID: "tab-1", Command: "hostname", Result: "success"}
	if err := store.AppendCommandAudit(event); err != nil { t.Fatalf("append audit: %v", err) }
	reloaded, err := NewStoreAtWithKeyring(baseDir, keyring)
	if err != nil { t.Fatalf("reload disk store: %v", err) }
	trail := reloaded.CommandAuditTrail()
	if len(trail) != 1 || trail[0].ID != "audit-1" { t.Fatalf("unexpected persisted audit trail: %+v", trail) }
}

func TestChatSessionsPersistEncryptedAndRestore(t *testing.T) {
	baseDir := t.TempDir()
	keyring := newMemoryKeyring()
	store, err := NewStoreAtWithKeyring(baseDir, keyring)
	if err != nil { t.Fatalf("create store: %v", err) }
	service := app.NewService(store, nil, nil)
	if err := service.EnsureMasterPassword("master-password"); err != nil { t.Fatalf("unlock: %v", err) }
	created, err := service.CreateChatSession("Persistent debug")
	if err != nil { t.Fatalf("create chat session: %v", err) }
	state := store.AIState()
	state.Messages = []ai.ChatMessage{{Role: "user", Content: "private conversation"}}
	if err := store.UpdateAIState(state); err != nil { t.Fatalf("update chat: %v", err) }
	if err := service.SelectChatSession(created.ID); err != nil { t.Fatalf("select chat: %v", err) }

	rawSettings, err := os.ReadFile(filepath.Join(baseDir, "settings.json"))
	if err != nil { t.Fatalf("read settings: %v", err) }
	if strings.Contains(string(rawSettings), "private conversation") || strings.Contains(string(rawSettings), "Persistent debug") { t.Fatal("settings.json must not contain chat content or titles") }

	reopened, err := NewStoreAtWithKeyring(baseDir, keyring)
	if err != nil { t.Fatalf("reopen store: %v", err) }
	reopenedService := app.NewService(reopened, nil, nil)
	if err := reopenedService.EnsureMasterPassword("master-password"); err != nil { t.Fatalf("unlock reopened store: %v", err) }
	found := reopenedService.ListChatSessions()
	if len(found) != 2 { t.Fatalf("expected two chat sessions, got %d", len(found)) }
	if err := reopenedService.SelectChatSession(created.ID); err != nil { t.Fatalf("select restored chat: %v", err) }
	if got := reopened.AIState().Messages; len(got) != 1 || got[0].Content != "private conversation" { t.Fatalf("restored messages mismatch: %+v", got) }
}
