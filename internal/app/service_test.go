package app

import (
	"bytes"
	"errors"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"eiksy/internal/domain/ai"
	"eiksy/internal/domain/sessions"
	"eiksy/internal/domain/settings"
	"eiksy/internal/domain/resources"
	"eiksy/internal/domain/workspace"
	"eiksy/internal/securestorage"
	"eiksy/internal/storage/memory"
)


type lockedAISecretStore struct {
	*memory.Store
}

func (s lockedAISecretStore) LoadSecret(string) (string, error) {
	return "", securestorage.ErrMasterPasswordRequired
}

func TestActiveConfiguredProviderRequiresMasterPasswordWhenTokenIsLocked(t *testing.T) {
	base := memory.NewStore()
	service := NewService(base, nil, nil)
	if err := service.SaveCloudProvider("model", "https://ai.example.test/v1", "saved-token"); err != nil {
		t.Fatalf("save cloud provider: %v", err)
	}

	lockedService := NewService(lockedAISecretStore{Store: base}, nil, nil)
	if _, err := lockedService.activeConfiguredProvider(base.AIState()); !errors.Is(err, securestorage.ErrMasterPasswordRequired) {
		t.Fatalf("expected master password required error, got %v", err)
	}
}

func TestProviderByIDRequiresMasterPasswordWhenTokenIsLocked(t *testing.T) {
	base := memory.NewStore()
	service := NewService(base, nil, nil)
	if err := service.SaveCloudProvider("model", "https://ai.example.test/v1", "saved-token"); err != nil {
		t.Fatalf("save cloud provider: %v", err)
	}

	lockedService := NewService(lockedAISecretStore{Store: base}, nil, nil)
	if _, err := lockedService.providerByID(base.AIState(), "openai-compatible-cloud"); !errors.Is(err, securestorage.ErrMasterPasswordRequired) {
		t.Fatalf("expected master password required error, got %v", err)
	}
}

func TestConnectSSHMarksSessionErrorWhenCredentialsCannotBeLoaded(t *testing.T) {
	store := memory.NewStore()
	ssh := &recordingSSHManager{}
	service := NewService(store, ssh, nil)
	profile := sessions.Profile{
		ID: "ssh-error",
		Name: "ssh-error",
		ProtocolID: "ssh",
		Host: "host",
		Port: 22,
		Username: "ops",
		Options: map[string]string{"auth_method": "password"},
	}
	if err := store.UpsertSessionProfile(profile); err != nil {
		t.Fatalf("seed profile: %v", err)
	}
	tab, err := service.LaunchSession(profile.ID)
	if err != nil {
		t.Fatalf("launch session: %v", err)
	}
	store.LockSecureStorage()
	if err := service.ConnectSSH(context.Background(), tab.ID, profile.ID); err == nil {
		t.Fatal("expected credential loading error")
	}
	updated, ok := service.runtimeTab(tab.ID)
	if !ok {
		t.Fatal("runtime tab not found")
	}
	if updated.Status != "error" {
		t.Fatalf("expected session status error, got %q", updated.Status)
	}
}

func TestLoginVaultRejectsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(bytes.Repeat([]byte("x"), int(maxVaultResponseSize+1)))
	}))
	defer server.Close()

	service := NewService(memory.NewStore(), nil, nil)
	baseURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse test server url: %v", err)
	}

	_, err = service.loginVault(baseURL, vaultAuthMethodDomain, "ops", "password")
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected oversized response error, got %v", err)
	}
}

func TestRenewVaultTokenRejectsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(bytes.Repeat([]byte("x"), int(maxVaultResponseSize+1)))
	}))
	defer server.Close()

	service := NewService(memory.NewStore(), nil, nil)
	baseURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse test server url: %v", err)
	}

	err = service.renewVaultToken(baseURL, "vault-token")
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected oversized response error, got %v", err)
	}
}

func TestGetShellStateIncludesScaffoldedDomains(t *testing.T) {
	service := NewService(memory.NewStore(), nil, nil)
	state := service.GetShellState()
	if len(state.Protocols) != 2 { t.Fatalf("expected 2 protocols, got %d", len(state.Protocols)) }
	if state.SessionProfiles == nil { t.Fatal("expected session profiles slice") }
	if len(state.AI.Providers) != 2 { t.Fatalf("expected 2 ai providers, got %d", len(state.AI.Providers)) }
}

func TestLaunchSessionCreatesRuntimeTabAndHistory(t *testing.T) {
	service := NewService(seedStore(t), nil, nil)
	before := service.GetShellState()
	launched, err := service.LaunchSession("artifact-mirror")
	if err != nil { t.Fatalf("launch session: %v", err) }
	after := service.GetShellState()
	if launched.ProfileID != "artifact-mirror" { t.Fatalf("expected profile artifact-mirror, got %s", launched.ProfileID) }
	if len(after.ActiveSessions) != len(before.ActiveSessions)+1 { t.Fatalf("expected active session count to grow, before=%d after=%d", len(before.ActiveSessions), len(after.ActiveSessions)) }
	if len(after.SessionHistory) != len(before.SessionHistory)+1 { t.Fatalf("expected launch history count to grow, before=%d after=%d", len(before.SessionHistory), len(after.SessionHistory)) }
	if len(after.SessionHistory) == 0 || after.SessionHistory[0].ProfileID != "artifact-mirror" { t.Fatalf("expected newest launch history entry to be artifact-mirror, got %+v", after.SessionHistory) }
	var launchedProfileFound bool
	for _, profile := range after.SessionProfiles {
		if profile.ID == "artifact-mirror" {
			launchedProfileFound = true
			if profile.LastLaunchedAt == "" { t.Fatal("expected launched profile to record last launch time") }
			break
		}
	}
	if !launchedProfileFound { t.Fatal("expected launched profile to remain in shell state") }
}

func TestLaunchHistoryIsCapped(t *testing.T) {
	service := NewService(seedStore(t), nil, nil)
	for range 150 { if _, err := service.LaunchSession("artifact-mirror"); err != nil { t.Fatalf("launch session: %v", err) } }
	state := service.GetShellState()
	if len(state.SessionHistory) != 100 { t.Fatalf("expected capped launch history of 100 entries, got %d", len(state.SessionHistory)) }
}

func TestCreateSessionProfileStoresPasswordOutsideProfile(t *testing.T) {
	store := memory.NewStore()
	service := NewService(store, nil, nil)
	input := sessions.ProfileInput{ID: "prod-ssh", Name: "prod-ssh", ProtocolID: "ssh", Host: "prod.internal", Port: 22, Username: "ops", Password: "keep-me", Options: map[string]string{"auth_method": "password"}}
	if err := service.CreateSessionProfileInput(input); err != nil { t.Fatalf("create session profile: %v", err) }
	profile, ok := store.SessionProfile("prod-ssh")
	if !ok { t.Fatal("session profile not found") }
	if !profile.HasPassword { t.Fatal("profile must expose password presence without carrying the secret value") }
	password, err := store.LoadSecret(securestorage.SessionPasswordKey("prod-ssh"))
	if err != nil { t.Fatalf("load password: %v", err) }
	if password != "keep-me" { t.Fatalf("expected password in secure storage, got %q", password) }
}

func TestSelectAIProviderMarksCloudProviderSelected(t *testing.T) {
	service := NewService(memory.NewStore(), nil, nil)
	if err := service.SelectAIProvider("openai-compatible-cloud"); err != nil { t.Fatalf("select ai provider: %v", err) }
	state := service.GetShellState()
	cloudProvider := mustFindProviderByID(t, state, "openai-compatible-cloud")
	if !cloudProvider.Selected { t.Fatal("expected cloud provider to be selected") }
}

func TestSaveCloudProviderStoresEndpointAndConfiguration(t *testing.T) {
	service := NewService(memory.NewStore(), nil, nil)
	if err := service.SaveCloudProvider("gpt-5.6", "https://models.example.com/v1", "secret-token"); err != nil { t.Fatalf("save cloud provider: %v", err) }
	state := service.GetShellState()
	cloudProvider := mustFindProviderByID(t, state, "openai-compatible-cloud")
	if !cloudProvider.Selected { t.Fatal("expected cloud provider to be selected") }
	if !cloudProvider.Configured { t.Fatal("expected cloud provider to be configured") }
	if cloudProvider.Endpoint != "https://models.example.com/v1" { t.Fatalf("expected cloud endpoint to be saved, got %q", cloudProvider.Endpoint) }
	if cloudProvider.Model != "gpt-5.6" { t.Fatalf("expected cloud model to be saved, got %q", cloudProvider.Model) }
	if cloudProvider.Status != "ready" { t.Fatalf("expected cloud provider status ready, got %q", cloudProvider.Status) }
	if !cloudProvider.HasToken { t.Fatal("expected cloud provider token flag to be set") }
	token, err := service.store.LoadSecret(securestorage.AIProviderTokenKey("openai-compatible-cloud"))
	if err != nil { t.Fatalf("load saved cloud token: %v", err) }
	if token != "secret-token" { t.Fatalf("expected cloud token to be saved, got %q", token) }
}

func TestSaveCloudProviderPreservesTokenWhenBlank(t *testing.T) {
	service := NewService(memory.NewStore(), nil, nil)
	if err := service.SaveCloudProvider("gpt-5.6", "https://models.example.com/v1", "secret-token"); err != nil { t.Fatalf("save cloud provider: %v", err) }
	if err := service.SaveCloudProvider("gpt-5.7", "https://models.example.com/v2", ""); err != nil { t.Fatalf("save cloud provider with empty token: %v", err) }
	state := service.GetShellState()
	cloudProvider := mustFindProviderByID(t, state, "openai-compatible-cloud")
	token, err := service.store.LoadSecret(securestorage.AIProviderTokenKey("openai-compatible-cloud"))
	if err != nil { t.Fatalf("load preserved cloud token: %v", err) }
	if token != "secret-token" { t.Fatalf("expected cloud token to be preserved, got %q", token) }
	if cloudProvider.Model != "gpt-5.7" { t.Fatalf("expected updated model to be saved, got %q", cloudProvider.Model) }
	if cloudProvider.Endpoint != "https://models.example.com/v2" { t.Fatalf("expected updated endpoint to be saved, got %q", cloudProvider.Endpoint) }
}

func TestSaveLocalProviderStoresCustomURLAndSelectsLocalProvider(t *testing.T) {
	service := NewService(memory.NewStore(), nil, nil)
	if err := service.SaveLocalProvider("https://models.example.com/qwen3.gguf"); err != nil { t.Fatalf("save local provider: %v", err) }
	state := service.GetShellState()
	localProvider := mustFindProviderByID(t, state, "local-qwen3-4b")
	if !localProvider.Selected { t.Fatal("expected local provider to be selected") }
	if localProvider.DownloadURL != "https://models.example.com/qwen3.gguf" { t.Fatalf("expected local provider url to be saved, got %q", localProvider.DownloadURL) }
	if localProvider.Status != "download required" { t.Fatalf("expected local provider to require download, got %q", localProvider.Status) }
}

func TestDownloadAndStartLocalModel(t *testing.T) {
	homeDir := t.TempDir(); t.Setenv("HOME", homeDir)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("gguf")) })); defer server.Close()
	binDir := t.TempDir(); scriptPath := filepath.Join(binDir, "llama-server")
	script := "#!/usr/bin/env python3\nimport json\nfrom http.server import BaseHTTPRequestHandler, HTTPServer\n\nclass Handler(BaseHTTPRequestHandler):\n    def do_GET(self):\n        if self.path == '/v1/models':\n            body = json.dumps({'data': [{'id': 'Qwen3-4B-Q4_K_M'}]}).encode()\n            self.send_response(200); self.send_header('Content-Type', 'application/json'); self.send_header('Content-Length', str(len(body))); self.end_headers(); self.wfile.write(body); return\n        self.send_response(404); self.end_headers()\n    def log_message(self, format, *args): pass\n\nHTTPServer(('127.0.0.1', 8012), Handler).serve_forever()\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil { t.Fatalf("write llama-server stub: %v", err) }
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	service := NewService(memory.NewStore(), nil, nil)
	if err := service.DownloadLocalModel(server.URL + "/Qwen3-4B-Q4_K_M.gguf"); err != nil { t.Fatalf("download local model: %v", err) }
	state := service.GetShellState(); localProvider := mustFindProviderByID(t, state, "local-qwen3-4b")
	if !fileExists(localProvider.LocalPath) { t.Fatalf("expected local model file to exist at %q", localProvider.LocalPath) }
	if localProvider.Status != "stopped" { t.Fatalf("expected downloaded local model to be stopped, got %q", localProvider.Status) }
	if err := service.StartLocalModel(); err != nil { t.Fatalf("start local model: %v", err) }
	t.Cleanup(func() { _ = service.StopLocalModel() })
	state = service.GetShellState(); localProvider = mustFindProviderByID(t, state, "local-qwen3-4b")
	if localProvider.Status != "running" { t.Fatal("expected local provider to be running") }
	if err := service.StopLocalModel(); err != nil { t.Fatalf("stop local model: %v", err) }
	state = service.GetShellState(); localProvider = mustFindProviderByID(t, state, "local-qwen3-4b")
	if localProvider.Status == "running" { t.Fatal("expected local provider to stop") }
}

func TestWaitForLocalModelReadyFailsWhenRequestedModelMissing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Header().Set("Content-Type", "application/json"); _, _ = w.Write([]byte(`{"data":[{"id":"other-model"}]}`)) })); defer server.Close()
	service := NewService(memory.NewStore(), nil, nil)
	err := service.waitForLocalModelReady(server.URL, "qwen3-required", time.Second)
	if err == nil || !strings.Contains(err.Error(), "qwen3-required") { t.Fatalf("expected missing model readiness error, got %v", err) }
}

func TestListCloudModelsFetchesAndSortsUniqueModels(t *testing.T) {
	var authHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { authHeader = r.Header.Get("Authorization"); if r.URL.Path != "/v1/models" { t.Fatalf("expected /v1/models path, got %s", r.URL.Path) }; w.Header().Set("Content-Type", "application/json"); _, _ = w.Write([]byte(`{"data":[{"id":"gpt-5.6"},{"id":"gpt-4.1"},{"id":"gpt-5.6"},{"id":" "}]}`)) })); defer server.Close()
	service := NewService(memory.NewStore(), nil, nil)
	models, err := service.ListCloudModels(server.URL+"/v1", "secret-token")
	if err != nil { t.Fatalf("list cloud models: %v", err) }
	if !slices.Equal(models, []string{"gpt-4.1", "gpt-5.6"}) { t.Fatalf("unexpected models list: %#v", models) }
	if !strings.HasPrefix(authHeader, "Bearer ") { t.Fatalf("expected bearer authorization header, got %q", authHeader) }
}

func TestListCloudModelsUsesSavedTokenWhenInputBlank(t *testing.T) {
	var authHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { authHeader = r.Header.Get("Authorization"); w.Header().Set("Content-Type", "application/json"); _, _ = w.Write([]byte(`{"data":[{"id":"gpt-5.6"}]}`)) })); defer server.Close()
	service := NewService(memory.NewStore(), nil, nil)
	if err := service.SaveCloudProvider("gpt-5.6", server.URL+"/v1", "saved-token"); err != nil { t.Fatalf("save cloud provider: %v", err) }
	if _, err := service.ListCloudModels(server.URL+"/v1", ""); err != nil { t.Fatalf("list cloud models: %v", err) }
	if !strings.HasPrefix(authHeader, "Bearer ") { t.Fatalf("expected bearer authorization header, got %q", authHeader) }
}


func TestListCloudModelsRejectsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(bytes.Repeat([]byte("x"), int(maxAIModelListResponseSize+1)))
	}))
	defer server.Close()

	service := NewService(memory.NewStore(), nil, nil)
	_, err := service.ListCloudModels(server.URL, "")
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected oversized response error, got %v", err)
	}
}

func TestDownloadLocalModelPreservesExistingConfigurationOnOversizedDownload(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	oldPath := filepath.Join(homeDir, ".eiksy", "models", "existing.gguf")
	if err := os.MkdirAll(filepath.Dir(oldPath), 0o755); err != nil {
		t.Fatalf("create model directory: %v", err)
	}
	if err := os.WriteFile(oldPath, []byte("existing-model"), 0o600); err != nil {
		t.Fatalf("write existing model: %v", err)
	}

	service := NewService(memory.NewStore(), nil, nil)
	state := service.store.AIState()
	index := providerIndexByID(state.Providers, localAIProviderID)
	state.Providers[index].DownloadURL = "https://old.example/existing.gguf"
	state.Providers[index].Model = "existing"
	state.Providers[index].LocalPath = oldPath
	state.Providers[index].Configured = true
	state.Providers[index].Status = "stopped"
	if err := service.store.UpdateAIState(state); err != nil {
		t.Fatalf("seed local provider: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.FormatInt(maxAIModelDownloadSize+1, 10))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	if err := service.DownloadLocalModel(server.URL + "/new-model.gguf"); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected oversized download error, got %v", err)
	}

	after := service.store.AIState()
	provider := after.Providers[index]
	if provider.DownloadURL != "https://old.example/existing.gguf" || provider.LocalPath != oldPath || provider.Model != "existing" {
		t.Fatalf("existing local model configuration changed after failed download: %+v", provider)
	}
	if !fileExists(oldPath) {
		t.Fatal("existing local model was removed")
	}
}

func TestStartCloudProviderAuthBuildsSourcegraphCallbackURL(t *testing.T) {
	service := NewService(memory.NewStore(), nil, nil); defer stopCloudAuthSessionForTest(service)
	session, err := service.StartCloudProviderAuth("https://sourcegraph.example.com/.api/llm/openai/v1")
	if err != nil { t.Fatalf("start cloud provider auth: %v", err) }
	if session.Status != "pending" { t.Fatalf("expected pending auth session, got %q", session.Status) }
	authURL, err := url.Parse(session.AuthURL); if err != nil { t.Fatalf("parse auth url: %v", err) }
	if authURL.Scheme != "https" || authURL.Host != "sourcegraph.example.com" { t.Fatalf("unexpected auth url origin: %s", session.AuthURL) }
	if authURL.Path != "/user/settings/tokens/new/callback" { t.Fatalf("unexpected auth url path: %s", authURL.Path) }
	if !strings.HasPrefix(authURL.Query().Get("requestFrom"), "CODY_CLI-") { t.Fatalf("unexpected requestFrom value: %q", authURL.Query().Get("requestFrom")) }
}

func TestStartCloudProviderAuthRejectsUnsupportedEndpoint(t *testing.T) {
	service := NewService(memory.NewStore(), nil, nil)
	_, err := service.StartCloudProviderAuth("https://api.openai.com/v1")
	if err == nil || !strings.Contains(err.Error(), "Sourcegraph/Cody-compatible") { t.Fatalf("expected unsupported endpoint error, got %v", err) }
}

func TestCloudProviderAuthSessionCompletesFromLocalhostCallback(t *testing.T) {
	service := NewService(memory.NewStore(), nil, nil); defer stopCloudAuthSessionForTest(service)
	session, err := service.StartCloudProviderAuth("https://sourcegraph.example.com/.api/llm/openai/v1"); if err != nil { t.Fatalf("start cloud provider auth: %v", err) }
	authURL, err := url.Parse(session.AuthURL); if err != nil { t.Fatalf("parse auth url: %v", err) }
	requestFrom := authURL.Query().Get("requestFrom"); port := strings.TrimPrefix(requestFrom, "CODY_CLI-"); if port == requestFrom || port == "" { t.Fatalf("unexpected requestFrom value: %q", requestFrom) }
	resp, err := http.Post("http://localhost:" + port + "/api/sourcegraph/token?state=" + url.QueryEscape(authURL.Query().Get("state")), "application/json", bytes.NewBufferString(`{"token":"browser-token"}`)); if err != nil { t.Fatalf("invoke callback: %v", err) }; resp.Body.Close()
	if resp.StatusCode != http.StatusOK { t.Fatalf("expected callback status 200, got %d", resp.StatusCode) }
	completed, err := service.GetCloudProviderAuthSession(session.ID); if err != nil { t.Fatalf("get auth session: %v", err) }
	if completed.Status != "completed" { t.Fatalf("expected completed auth session, got %q", completed.Status) }
	storedToken, err := service.store.LoadSecret(securestorage.AIProviderTokenKey("openai-compatible-cloud")); if err != nil { t.Fatalf("load stored browser token: %v", err) }; if storedToken != "browser-token" { t.Fatalf("expected browser token in secure storage, got %q", storedToken) }
}

func TestCloudProviderAuthSessionAcceptsPostedAccessToken(t *testing.T) {
	service := NewService(memory.NewStore(), nil, nil); defer stopCloudAuthSessionForTest(service)
	session, err := service.StartCloudProviderAuth("https://sourcegraph.example.com/.api/llm/openai/v1"); if err != nil { t.Fatalf("start cloud provider auth: %v", err) }
	authURL, err := url.Parse(session.AuthURL); if err != nil { t.Fatalf("parse auth url: %v", err) }
	requestFrom := authURL.Query().Get("requestFrom"); port := strings.TrimPrefix(requestFrom, "CODY_CLI-"); if port == requestFrom || port == "" { t.Fatalf("unexpected requestFrom value: %q", requestFrom) }
	resp, err := http.Post("http://127.0.0.1:"+port+"/api/sourcegraph/token?state="+url.QueryEscape(authURL.Query().Get("state")), "application/json", bytes.NewBufferString(`{"accessToken":"posted-token"}`)); if err != nil { t.Fatalf("post callback: %v", err) }; resp.Body.Close()
	if resp.StatusCode != http.StatusOK { t.Fatalf("expected callback status 200, got %d", resp.StatusCode) }
	completed, err := service.GetCloudProviderAuthSession(session.ID); if err != nil { t.Fatalf("get auth session: %v", err) }
	if completed.Status != "completed" { t.Fatalf("expected completed auth session, got %q", completed.Status) }
	storedToken, err := service.store.LoadSecret(securestorage.AIProviderTokenKey("openai-compatible-cloud")); if err != nil { t.Fatalf("load stored posted token: %v", err) }; if storedToken != "posted-token" { t.Fatalf("expected posted token in secure storage, got %q", storedToken) }
}

func TestClearChatStartsFreshSession(t *testing.T) {
	store := memory.NewStore()
	service := NewService(store, nil, nil)

	state := store.AIState()
	previousSessionID := state.ChatSessionID
	state.Messages = []ai.ChatMessage{
		{Role: "user", Content: "old conversation"},
	}
	state.AgentMessages = []ai.ChatMessage{{Role: "assistant", Content: "agent conversation"}}
	state.CommandPolicy.PendingRequests = []ai.CommandRequest{
		{ID: "request-1", ToolID: "shell", SessionID: "session-1", Command: "uname -a"},
	}
	state.PendingNativeToolCall = &ai.PendingNativeToolCall{
		RequestID: "request-1",
		ProviderID: "provider-1",
		ToolCallID: "tool-1",
		ToolName: "ssh.exec",
		UserMessage: "run uname",
		SessionID: "session-1",
	}
	if err := store.UpdateAIState(state); err != nil {
		t.Fatalf("seed AI session state: %v", err)
	}

	if err := service.ClearChat(); err != nil {
		t.Fatalf("clear chat: %v", err)
	}

	updated := store.AIState()
	if updated.Messages == nil || len(updated.Messages) != 0 {
		t.Fatalf("expected chat messages to be cleared, got %#v", updated.Messages)
	}
	if updated.ChatSessionID == "" || updated.ChatSessionID == previousSessionID {
		t.Fatalf("expected a new chat session id, got %q", updated.ChatSessionID)
	}
	if len(updated.CommandPolicy.PendingRequests) != 1 || updated.CommandPolicy.PendingRequests[0].ID != "request-1" {
		t.Fatalf("expected pending agent approval to remain untouched, got %#v", updated.CommandPolicy.PendingRequests)
	}
	if updated.PendingNativeToolCall == nil || updated.PendingNativeToolCall.RequestID != "request-1" {
		t.Fatalf("expected pending agent tool call to remain untouched, got %#v", updated.PendingNativeToolCall)
	}
	if len(updated.AgentMessages) != 1 || updated.AgentMessages[0].Content != "agent conversation" {
		t.Fatalf("expected agent history to remain untouched, got %#v", updated.AgentMessages)
	}
}

func TestResolveCommandPolicyRequestWithSessionApprovalExecutesCommand(t *testing.T) {
	store := memory.NewStore(); profile := sessions.Profile{ID: "ssh-host", Name: "ssh-host", ProtocolID: "ssh", Host: "host", Port: 22, Username: "ops"}; if err := store.UpsertSessionProfile(profile); err != nil { t.Fatalf("seed profile: %v", err) }
	ssh := &recordingSSHManager{}; service := NewService(store, ssh, nil)
	tab := workspace.Tab{ID: "runtime-1", ProfileID: profile.ID, ProtocolID: "ssh", Status: "connected"}
	store.OpenRuntimeTab(tab)
	state := store.AIState(); state.CommandPolicy.PendingRequests = []ai.CommandRequest{{ID: "request-1", ToolID: "shell", SessionID: tab.ID, Command: "uname -a"}}; store.UpdateAIState(state)
	if err := service.ResolveCommandPolicyRequest("request-1", ai.CommandPermissionModeSession); err != nil { t.Fatalf("resolve request: %v", err) }
	updated := store.AIState(); if len(updated.CommandPolicy.PendingRequests) != 0 { t.Fatalf("expected pending requests to be cleared, got %d", len(updated.CommandPolicy.PendingRequests)) }
	if !hasCommandRule(updated.CommandPolicy.CommandRules, "shell", tab.ID, "uname -a", ai.CommandPermissionAllow) { t.Fatalf("expected exact shell command grant for session %s", tab.ID) }
	if len(ssh.inputs) != 1 { t.Fatalf("expected exactly one command dispatch, got %d", len(ssh.inputs)) }; if ssh.inputs[0].tabID != tab.ID { t.Fatalf("expected command to run in tab %s, got %s", tab.ID, ssh.inputs[0].tabID) }; if ssh.inputs[0].payload != "uname -a\n" { t.Fatalf("unexpected command payload %q", ssh.inputs[0].payload) }
}

func TestResolveCommandPolicyRequestPersistsAlwaysGrantOnExecutionFailure(t *testing.T) {
	store := memory.NewStore(); profile := sessions.Profile{ID: "ssh-host-fail", Name: "ssh-host-fail", ProtocolID: "ssh", Host: "host", Port: 22, Username: "ops"}; if err := store.UpsertSessionProfile(profile); err != nil { t.Fatalf("seed profile: %v", err) }
	ssh := &recordingSSHManager{sendInputErr: fmt.Errorf("write failed")}; service := NewService(store, ssh, nil); tab, err := service.LaunchSession(profile.ID); if err != nil { t.Fatalf("launch session: %v", err) }
	state := store.AIState(); state.CommandPolicy.PendingRequests = []ai.CommandRequest{{ID: "request-fail", ToolID: "shell", SessionID: tab.ID, Command: "hostname"}}; store.UpdateAIState(state)
	if err := service.ResolveCommandPolicyRequest("request-fail", ai.CommandPermissionModeAlways); err != nil { t.Fatalf("resolve request: %v", err) }
	updated := store.AIState(); if len(updated.CommandPolicy.PendingRequests) != 0 { t.Fatalf("expected pending request to be removed, got %d", len(updated.CommandPolicy.PendingRequests)) }; if !hasCommandRule(updated.CommandPolicy.CommandRules, "shell", "", "hostname", ai.CommandPermissionAllow) { t.Fatalf("expected exact global command grant to persist, got %+v", updated.CommandPolicy.CommandRules) }
}


func TestResolveCommandPolicyRequestDenyDoesNotExecuteCommand(t *testing.T) {
	store := memory.NewStore()
	profile := sessions.Profile{ID: "ssh-host-deny", Name: "ssh-host-deny", ProtocolID: "ssh", Host: "host", Port: 22, Username: "ops"}
	if err := store.UpsertSessionProfile(profile); err != nil {
		t.Fatalf("seed profile: %v", err)
	}
	ssh := &recordingSSHManager{}
	service := NewService(store, ssh, nil)
	tab := workspace.Tab{ID: "runtime-deny", ProfileID: profile.ID, ProtocolID: "ssh", Status: "connected"}
	store.OpenRuntimeTab(tab)

	state := store.AIState()
	state.CommandPolicy.PendingRequests = []ai.CommandRequest{{ID: "request-deny", ToolID: "shell", SessionID: tab.ID, Command: "rm -f important.txt"}}
	if err := store.UpdateAIState(state); err != nil {
		t.Fatalf("seed pending approval: %v", err)
	}

	if err := service.ResolveCommandPolicyRequest("request-deny", ai.CommandPermissionModeDeny); err != nil {
		t.Fatalf("deny request: %v", err)
	}
	updated := store.AIState()
	if len(updated.CommandPolicy.PendingRequests) != 0 {
		t.Fatalf("expected denied request to be cleared, got %d", len(updated.CommandPolicy.PendingRequests))
	}
	if len(ssh.inputs) != 0 {
		t.Fatalf("denied command must not execute, got %d dispatches", len(ssh.inputs))
	}
	if hasCommandRule(updated.CommandPolicy.CommandRules, "shell", tab.ID, "rm -f important.txt", ai.CommandPermissionAllow) {
		t.Fatal("deny must not create an allow rule")
	}
}

func TestResolveCommandPolicyRequestNowApprovalIsOneShot(t *testing.T) {
	store := memory.NewStore()
	profile := sessions.Profile{ID: "ssh-host-now", Name: "ssh-host-now", ProtocolID: "ssh", Host: "host", Port: 22, Username: "ops"}
	if err := store.UpsertSessionProfile(profile); err != nil {
		t.Fatalf("seed profile: %v", err)
	}
	ssh := &recordingSSHManager{}
	service := NewService(store, ssh, nil)
	tab := workspace.Tab{ID: "runtime-now", ProfileID: profile.ID, ProtocolID: "ssh", Status: "connected"}
	store.OpenRuntimeTab(tab)

	state := store.AIState()
	state.CommandPolicy.PendingRequests = []ai.CommandRequest{{ID: "request-now", ToolID: "shell", SessionID: tab.ID, Command: "uname -a"}}
	if err := store.UpdateAIState(state); err != nil {
		t.Fatalf("seed pending approval: %v", err)
	}

	if err := service.ResolveCommandPolicyRequest("request-now", ai.CommandPermissionModeNow); err != nil {
		t.Fatalf("approve request: %v", err)
	}
	updated := store.AIState()
	if len(updated.CommandPolicy.PendingRequests) != 0 {
		t.Fatalf("expected one-shot approval to be cleared, got %d", len(updated.CommandPolicy.PendingRequests))
	}
	if hasCommandRule(updated.CommandPolicy.CommandRules, "shell", tab.ID, "uname -a", ai.CommandPermissionAllow) {
		t.Fatal("now approval must not persist an allow rule")
	}
	if len(ssh.inputs) != 1 {
		t.Fatalf("expected exactly one command dispatch, got %d", len(ssh.inputs))
	}

	if err := service.ResolveCommandPolicyRequest("request-now", ai.CommandPermissionModeNow); err == nil {
		t.Fatal("expected consumed approval request to be unavailable")
	}
	if len(ssh.inputs) != 1 {
		t.Fatalf("consumed approval must not execute again, got %d dispatches", len(ssh.inputs))
	}
}

func TestResolveCommandPolicyRequestSessionApprovalScopesGrantToSession(t *testing.T) {
	store := memory.NewStore()
	profile := sessions.Profile{ID: "ssh-host-scope", Name: "ssh-host-scope", ProtocolID: "ssh", Host: "host", Port: 22, Username: "ops"}
	if err := store.UpsertSessionProfile(profile); err != nil {
		t.Fatalf("seed profile: %v", err)
	}
	ssh := &recordingSSHManager{}
	service := NewService(store, ssh, nil)
	tab := workspace.Tab{ID: "runtime-scope", ProfileID: profile.ID, ProtocolID: "ssh", Status: "connected"}
	store.OpenRuntimeTab(tab)

	state := store.AIState()
	state.CommandPolicy.PendingRequests = []ai.CommandRequest{{ID: "request-scope", ToolID: "shell", SessionID: tab.ID, Command: "systemctl status eiksy"}}
	if err := store.UpdateAIState(state); err != nil {
		t.Fatalf("seed pending approval: %v", err)
	}

	if err := service.ResolveCommandPolicyRequest("request-scope", ai.CommandPermissionModeSession); err != nil {
		t.Fatalf("approve session request: %v", err)
	}
	updated := store.AIState()
	if !hasCommandRule(updated.CommandPolicy.CommandRules, "shell", tab.ID, "systemctl status eiksy", ai.CommandPermissionAllow) {
		t.Fatalf("expected session-scoped allow rule, got %+v", updated.CommandPolicy.CommandRules)
	}
	for _, rule := range updated.CommandPolicy.CommandRules {
		if rule.Pattern == "systemctl status eiksy" && rule.SessionID == "" {
			t.Fatal("session approval must not create a global allow rule")
		}
	}
}

func TestResolveCommandPolicyRequestFailsClosedOnPendingToolMismatch(t *testing.T) {
	store := memory.NewStore()
	profile := sessions.Profile{ID: "ssh-pending-mismatch", Name: "ssh-pending-mismatch", ProtocolID: "ssh", Host: "host", Port: 22, Username: "ops"}
	if err := store.UpsertSessionProfile(profile); err != nil { t.Fatalf("seed profile: %v", err) }
	ssh := &recordingSSHManager{}
	service := NewService(store, ssh, nil)
	tab := workspace.Tab{ID: "runtime-pending-mismatch", ProfileID: profile.ID, ProtocolID: "ssh", Status: "connected"}
	store.OpenRuntimeTab(tab)

	state := store.AIState()
	state.CommandPolicy.PendingRequests = []ai.CommandRequest{{ID: "request-mismatch", ToolID: "shell", SessionID: tab.ID, Command: "uname -a"}}
	state.PendingNativeToolCall = &ai.PendingNativeToolCall{RequestID: "request-mismatch", ToolName: nativeSFTPWriteToolName, SessionID: tab.ID, ToolArguments: `{"sessionId":"runtime-pending-mismatch","path":"/tmp/file","content":"unexpected"}`}
	if err := store.UpdateAIState(state); err != nil { t.Fatalf("seed pending state: %v", err) }

	if err := service.ResolveCommandPolicyRequest("request-mismatch", ai.CommandPermissionModeNow); err == nil { t.Fatal("expected mismatched pending tool to be rejected") }
	updated := store.AIState()
	if len(ssh.inputs) != 0 { t.Fatalf("mismatched pending tool must not execute, got %d dispatches", len(ssh.inputs)) }
	if hasCommandRule(updated.CommandPolicy.CommandRules, "shell", "", "uname -a", ai.CommandPermissionAllow) { t.Fatal("mismatched pending tool must not create an allow rule") }
	if updated.PendingNativeToolCall == nil { t.Fatal("expected pending continuation to remain for explicit recovery") }
}

func TestResolveCommandPolicyRequestFailsClosedOnPendingSessionMismatch(t *testing.T) {
	store := memory.NewStore()
	profile := sessions.Profile{ID: "ssh-pending-session", Name: "ssh-pending-session", ProtocolID: "ssh", Host: "host", Port: 22, Username: "ops"}
	if err := store.UpsertSessionProfile(profile); err != nil { t.Fatalf("seed profile: %v", err) }
	ssh := &recordingSSHManager{}
	service := NewService(store, ssh, nil)
	tab := workspace.Tab{ID: "runtime-pending-session", ProfileID: profile.ID, ProtocolID: "ssh", Status: "connected"}
	store.OpenRuntimeTab(tab)

	state := store.AIState()
	state.CommandPolicy.PendingRequests = []ai.CommandRequest{{ID: "request-session-mismatch", ToolID: "shell", SessionID: tab.ID, Command: "uname -a"}}
	state.PendingNativeToolCall = &ai.PendingNativeToolCall{RequestID: "request-session-mismatch", ToolName: "shell", SessionID: "different-session"}
	if err := store.UpdateAIState(state); err != nil { t.Fatalf("seed pending state: %v", err) }

	if err := service.ResolveCommandPolicyRequest("request-session-mismatch", ai.CommandPermissionModeNow); err == nil { t.Fatal("expected mismatched pending session to be rejected") }
	updated := store.AIState()
	if len(ssh.inputs) != 0 { t.Fatalf("mismatched pending session must not execute, got %d dispatches", len(ssh.inputs)) }
	if hasCommandRule(updated.CommandPolicy.CommandRules, "shell", "", "uname -a", ai.CommandPermissionAllow) { t.Fatal("mismatched pending session must not create an allow rule") }
}
func TestBuildPortForwardSpecsWithRemoteTarget(t *testing.T) { specs := buildPortForwardSpecs("8080,9000-9001", "db.internal", "5432"); if specs != "8080:db.internal:5432,9000:db.internal:5432,9001:db.internal:5432" { t.Fatalf("unexpected specs: %s", specs) } }
func TestBuildPortForwardSpecsFallsBackToSamePortWhenRemotePortIsEmpty(t *testing.T) { specs := buildPortForwardSpecs("8080-8081", "db.internal", ""); if specs != "8080:db.internal:8080,8081:db.internal:8081" { t.Fatalf("unexpected specs: %s", specs) } }

func TestUpdateSettingsKeePassPasswordPersistsOnBlankUpdate(t *testing.T) {
	store := memory.NewStore(); service := NewService(store, nil, nil); initial := store.Settings(); initial.VaultProvider = "keepass"; initial.KeePassDatabasePath = "/tmp/dev.kdbx"
	if err := store.StoreSecret(securestorage.KeePassPasswordKey(), "keepass-secret"); err != nil { t.Fatalf("store keepass password: %v", err) }; if err := store.UpdateSettings(initial); err != nil { t.Fatalf("seed settings: %v", err) }
	updated := store.Settings(); updated.KeePassPassword = ""; if err := service.UpdateSettings(updated); err != nil { t.Fatalf("update settings: %v", err) }; reloaded := store.Settings(); if !reloaded.HasKeePassPassword { t.Fatal("expected keepass password flag to persist") }; password, err := store.LoadSecret(securestorage.KeePassPasswordKey()); if err != nil { t.Fatalf("load keepass password: %v", err) }; if password != "keepass-secret" { t.Fatalf("expected keepass password to persist, got %q", password) }
}

func TestUpdateSettingsVaultPasswordPersistsOnBlankUpdate(t *testing.T) {
	store := memory.NewStore(); service := NewService(store, nil, nil); initial := store.Settings(); initial.VaultAuthMethod = "domain"; initial.VaultLogin = "CORP\\ops"
	if err := store.StoreSecret(securestorage.VaultPasswordKey(), "vault-secret"); err != nil { t.Fatalf("store vault password: %v", err) }; if err := store.UpdateSettings(initial); err != nil { t.Fatalf("seed settings: %v", err) }
	updated := store.Settings(); updated.VaultPassword = ""; if err := service.UpdateSettings(updated); err != nil { t.Fatalf("update settings: %v", err) }; reloaded := store.Settings(); if !reloaded.HasVaultPassword { t.Fatal("expected vault password flag to persist") }; password, err := store.LoadSecret(securestorage.VaultPasswordKey()); if err != nil { t.Fatalf("load vault password: %v", err) }; if password != "vault-secret" { t.Fatalf("expected vault password to persist, got %q", password) }
}

func TestApplySSHForwardingSettingsUsesRemoteHostAndPort(t *testing.T) {
	store := memory.NewStore(); target := sessions.Profile{ID: "jump-host", Name: "jump-host", ProtocolID: "ssh", Host: "jump.internal", Port: 22, Username: "ops"}; source := sessions.Profile{ID: "source-host", Name: "source-host", ProtocolID: "ssh", Host: "source.internal", Port: 22, Username: "ops"}
	if err := store.UpsertSessionProfile(target); err != nil { t.Fatalf("seed target profile: %v", err) }; if err := store.UpsertSessionProfile(source); err != nil { t.Fatalf("seed source profile: %v", err) }
	cfg := store.Settings(); cfg.PortForwardRules = []settings.PortForwardRule{{LocalPort: "15432", RemoteHost: "db.internal", RemotePort: "5432", HostID: "jump-host", Enabled: true}}; if err := store.UpdateSettings(cfg); err != nil { t.Fatalf("update settings: %v", err) }
	service := NewService(store, nil, nil); withRules := service.applySSHForwardingSettings(source); if got := withRules.Options["local_forwards"]; got != "15432:db.internal:5432" { t.Fatalf("unexpected forward specs: %q", got) }
}

func stopCloudAuthSessionForTest(service *Service) { service.authMu.Lock(); defer service.authMu.Unlock(); service.stopCloudAuthLocked() }

func TestConnectSSHResolvesSecretRefCredential(t *testing.T) {
	store := memory.NewStore()
	profile := sessions.Profile{
		ID: "ssh-vault-ref",
		Name: "ssh-vault-ref",
		ProtocolID: "ssh",
		Host: "host",
		Port: 22,
		Username: "ops",
		SecretRef: "vault:team/database#password",
		Options: map[string]string{"auth_method": "password"},
	}
	if err := store.UpsertSessionProfile(profile); err != nil {
		t.Fatalf("seed profile: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/secret/data/team/database" {
			t.Fatalf("unexpected Vault path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"password":"runtime-secret"}}`))
	}))
	defer server.Close()

	cfg := store.Settings()
	cfg.VaultAddress = server.URL
	cfg.VaultMountPoint = "secret"
	if err := store.UpdateSettings(cfg); err != nil {
		t.Fatalf("update settings: %v", err)
	}
	if err := store.StoreSecret(securestorage.VaultTokenKey(), "test-token"); err != nil {
		t.Fatalf("store Vault token: %v", err)
	}

	ssh := &recordingSSHManager{}
	service := NewService(store, ssh, nil)
	service.httpClient = server.Client()

	tab, err := service.LaunchSession(profile.ID)
	if err != nil {
		t.Fatalf("launch session: %v", err)
	}
	if err := service.ConnectSSH(context.Background(), tab.ID, profile.ID); err != nil {
		t.Fatalf("connect SSH: %v", err)
	}
	if len(ssh.connectCalls) != 1 {
		t.Fatalf("expected one SSH connect call, got %d", len(ssh.connectCalls))
	}
	if ssh.connectCalls[0].credential != "runtime-secret" {
		t.Fatalf("expected resolved credential, got %q", ssh.connectCalls[0].credential)
	}
	saved, ok := store.SessionProfile(profile.ID)
	if !ok {
		t.Fatal("session profile not found")
	}
	if saved.SecretRef != profile.SecretRef {
		t.Fatalf("expected SecretRef to remain unchanged, got %q", saved.SecretRef)
	}
	if saved.HasPassword {
		t.Fatal("Vault-backed credential must not create a local password flag")
	}
}

func TestConnectionLifecycleUsesResourceSessionState(t *testing.T) {
	store := memory.NewStore()
	profile := sessions.Profile{ID: "ssh-resource", Name: "ssh-resource", ProtocolID: "ssh", Host: "host", Port: 22, Username: "ops"}
	if err := store.UpsertSessionProfile(profile); err != nil {
		t.Fatalf("seed profile: %v", err)
	}
	ssh := &recordingSSHManager{}
	service := NewService(store, ssh, nil)

	tab, err := service.LaunchSession(profile.ID)
	if err != nil {
		t.Fatalf("launch session: %v", err)
	}
	if tab.Status != "connecting" {
		t.Fatalf("expected initial connecting status, got %q", tab.Status)
	}

	if err := service.ConnectSession(tab.ID); err != nil {
		t.Fatalf("connect session: %v", err)
	}
	connected, ok := service.runtimeTab(tab.ID)
	if !ok || connected.Status != "connected" {
		t.Fatalf("expected connected session status, got %+v", connected)
	}

	if err := service.DisconnectSession(tab.ID); err != nil {
		t.Fatalf("disconnect session: %v", err)
	}
	disconnected, ok := service.runtimeTab(tab.ID)
	if !ok || disconnected.Status != "disconnected" {
		t.Fatalf("expected disconnected session status, got %+v", disconnected)
	}
}

func TestConnectionLifecycleRejectsUnknownSession(t *testing.T) {
	service := NewService(memory.NewStore(), &recordingSSHManager{}, nil)
	if err := service.ConnectSession("missing"); err == nil {
		t.Fatal("expected connect to reject unknown session")
	}
	if err := service.DisconnectSession("missing"); err == nil {
		t.Fatal("expected disconnect to reject unknown session")
	}
}

func seedStore(t *testing.T) *memory.Store {
	t.Helper(); store := memory.NewStore(); profiles := []sessions.Profile{{ID: "artifact-mirror", Name: "artifact-mirror", Group: "Shared Services", Tags: []string{"sftp", "artifacts"}, ProtocolID: "sftp", Host: "mirror.internal", Port: 22, Username: "mirrorbot"}, {ID: "ops-linux-admin", Name: "ops-linux-admin", Group: "Production", Tags: []string{"linux", "ssh"}, ProtocolID: "ssh", Host: "prod-shell.internal", Port: 22, Username: "ops"}}
	for _, profile := range profiles { if err := store.UpsertSessionProfile(profile); err != nil { t.Fatalf("seed session profile: %v", err) } }; return store
}

func mustFindProviderByID(t *testing.T, state ShellState, providerID string) ai.ProviderDescriptor { t.Helper(); for _, provider := range state.AI.Providers { if provider.ID == providerID { return provider } }; t.Fatalf("expected provider %q to exist", providerID); return ai.ProviderDescriptor{} }

func hasCommandRule(rules []ai.CommandRule, toolID, sessionID, pattern string, action ai.CommandPermissionAction) bool { for _, rule := range rules { if rule.ToolID == toolID && rule.SessionID == sessionID && rule.Pattern == pattern && rule.Action == action { return true } }; return false }

type sshInputCall struct { tabID string; payload string }
type recordingSSHManager struct {
	inputs []sshInputCall
	connectCalls []sshConnectCall
	sendInputErr error
}
type sshConnectCall struct {
	tabID string
	host string
	port int
	username string
	credential string
}
func (m *recordingSSHManager) Connect(_ context.Context, tabID, host string, port int, username, credential string, _ map[string]string) error {
	m.connectCalls = append(m.connectCalls, sshConnectCall{tabID: tabID, host: host, port: port, username: username, credential: credential})
	return nil
}
func (m *recordingSSHManager) SendInput(tabID, data string) error { m.inputs = append(m.inputs, sshInputCall{tabID: tabID, payload: data}); return m.sendInputErr }
func (m *recordingSSHManager) ResizeTerminal(string, int, int) error { return nil }
func (m *recordingSSHManager) Disconnect(string) error { return nil }
func (m *recordingSSHManager) SetOutputHandler(string, func(data string)) {}
func (m *recordingSSHManager) GetCurrentDir(string) (string, error) { return "", nil }
func (m *recordingSSHManager) AcceptHostKey(string) error { return nil }
func (m *recordingSSHManager) ExecCommandResult(_ context.Context, tabID, command string) (sessions.CommandExecutionResult, error) { m.inputs = append(m.inputs, sshInputCall{tabID: tabID, payload: command+"\n"}); return sessions.CommandExecutionResult{Success:true, ExitCode:0, Stdout:"mock output"}, nil }



func TestExecuteCommandRequiresConnectedSession(t *testing.T) {
	store := memory.NewStore()
	store.OpenRuntimeTab(workspace.Tab{ID: "session-1", ProtocolID: "ssh", Status: "disconnected"})
	ssh := &recordingSSHManager{}
	service := NewService(store, ssh, nil)

	result, err := service.ExecuteCommand("session-1", "uname -a")
	if err == nil || !strings.Contains(err.Error(), "not connected") {
		t.Fatalf("expected disconnected-session error, got %v", err)
	}
	if result.ErrorType != sessions.CommandExecutionErrorConnection {
		t.Fatalf("expected connection error type, got %q", result.ErrorType)
	}
	if len(ssh.inputs) != 0 {
		t.Fatalf("command must not be dispatched for disconnected session, got %+v", ssh.inputs)
	}
}

func TestExecuteCommandUsesExistingConnectedSession(t *testing.T) {
	store := memory.NewStore()
	store.OpenRuntimeTab(workspace.Tab{ID: "session-1", ProtocolID: "ssh", Status: "connected"})
	ssh := &recordingSSHManager{}
	service := NewService(store, ssh, nil)

	result, err := service.ExecuteCommand("session-1", "uname -a")
	if err != nil {
		t.Fatalf("execute command: %v", err)
	}
	if !result.Success || result.ExitCode != 0 {
		t.Fatalf("unexpected command result: %+v", result)
	}
	if len(ssh.inputs) != 1 || ssh.inputs[0].tabID != "session-1" || ssh.inputs[0].payload != "uname -a\n" {
		t.Fatalf("unexpected command dispatch: %+v", ssh.inputs)
	}
}


func TestDeleteSessionProfileRemovesCredentials(t *testing.T) {
	store := memory.NewStore()
	service := NewService(store, nil, nil)
	if err := store.StoreSecret(securestorage.SessionPasswordKey("profile-1"), "secret"); err != nil {
		t.Fatalf("store password: %v", err)
	}
	if err := store.UpsertSessionProfile(sessions.Profile{ID: "profile-1", Name: "test", ProtocolID: "ssh", Host: "example", Port: 22, Username: "user"}); err != nil {
		t.Fatalf("create profile: %v", err)
	}
	if err := service.DeleteSessionProfile("profile-1"); err != nil {
		t.Fatalf("delete profile: %v", err)
	}
	if store.SecretExists(securestorage.SessionPasswordKey("profile-1")) {
		t.Fatal("deleted profile must not retain its password")
	}
}

func TestMemorySecureStorageRejectsSecretAccessWhenLocked(t *testing.T) {
	store := memory.NewStore()
	if err := store.StoreSecret("credential", "secret"); err != nil {
		t.Fatalf("store secret: %v", err)
	}

	store.LockSecureStorage()

	if _, err := store.LoadSecret("credential"); err == nil {
		t.Fatal("expected locked secure storage to reject secret reads")
	}
	if err := store.StoreSecret("credential-2", "secret"); err == nil {
		t.Fatal("expected locked secure storage to reject secret writes")
	}

	if err := store.EnsureMasterPassword("master-password"); err != nil {
		t.Fatalf("unlock secure storage: %v", err)
	}
	value, err := store.LoadSecret("credential")
	if err != nil {
		t.Fatalf("load secret after unlock: %v", err)
	}
	if value != "secret" {
		t.Fatalf("unexpected secret after unlock: %q", value)
	}
}

func TestLockSecureStorageClearsPendingAIApproval(t *testing.T) {
	store := memory.NewStore()
	state := store.AIState()
	state.PendingNativeToolCall = &ai.PendingNativeToolCall{RequestID: "request-1", ProviderID: "provider-1"}
	state.CommandPolicy.PendingRequests = []ai.CommandRequest{{ID: "request-1", Command: "id"}}
	store.UpdateAIState(state)
	service := NewService(store, nil, nil)

	service.LockSecureStorage()

	state = store.AIState()
	if state.PendingNativeToolCall != nil {
		t.Fatal("pending native tool call must be cleared when storage locks")
	}
	if len(state.CommandPolicy.PendingRequests) != 0 {
		t.Fatal("pending command approvals must be cleared when storage locks")
	}
	if len(state.Messages) != 0 {
		t.Fatal("AI history must be cleared from memory when storage locks")
	}
}

func TestGetResourcesExcludesSecretsAndTracksActiveSession(t *testing.T) {
	store := seedStore(t)
	service := NewService(store, nil, nil)

	items := service.GetResources()
	if len(items) != 2 {
		t.Fatalf("expected 2 resources, got %d", len(items))
	}

	var mirror resources.Resource
	for _, item := range items {
		if item.ID == "artifact-mirror" {
			mirror = item
			break
		}
	}
	if mirror.ID == "" {
		t.Fatal("expected artifact-mirror resource")
	}
	if mirror.Kind != "remote" {
		t.Fatalf("expected remote resource kind, got %q", mirror.Kind)
	}
	if len(mirror.Capabilities) == 0 {
		t.Fatal("expected resource capabilities")
	}
	if mirror.Status != "configured" {
		t.Fatalf("expected configured resource status, got %q", mirror.Status)
	}
	if mirror.ActiveSessionID != "" {
		t.Fatal("expected no active session")
	}

	launched, err := service.LaunchSession("artifact-mirror")
	if err != nil {
		t.Fatalf("launch session: %v", err)
	}
	active, err := service.GetResource("artifact-mirror")
	if err != nil {
		t.Fatalf("get active resource: %v", err)
	}
	if active.ActiveSessionID != launched.ID {
		t.Fatalf("expected active session %q, got %q", launched.ID, active.ActiveSessionID)
	}
	if active.Status != "connecting" {
		t.Fatalf("expected connecting resource status, got %q", active.Status)
	}
}
