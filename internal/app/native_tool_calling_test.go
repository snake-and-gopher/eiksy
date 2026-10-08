package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"eiksy/internal/domain/ai"
	"eiksy/internal/domain/sessions"
	sftpdomain "eiksy/internal/domain/sftp"
	"eiksy/internal/domain/workspace"
	"eiksy/internal/storage/memory"
)

type nativeTestSSHManager struct {
	mu         sync.Mutex
	commands   []string
	currentDir string
}

func (m *nativeTestSSHManager) Connect(context.Context, string, string, int, string, string, map[string]string) error { return nil }
func (m *nativeTestSSHManager) SendInput(sessionID, data string) error { m.mu.Lock(); defer m.mu.Unlock(); m.commands = append(m.commands, sessionID+":"+data); return nil }
func (m *nativeTestSSHManager) ResizeTerminal(string, int, int) error { return nil }
func (m *nativeTestSSHManager) Disconnect(string) error { return nil }
func (m *nativeTestSSHManager) SetOutputHandler(string, func(string)) {}
func (m *nativeTestSSHManager) GetCurrentDir(string) (string, error) { if m.currentDir != "" { return m.currentDir, nil }; return ".", nil }
func (m *nativeTestSSHManager) AcceptHostKey(string) error { return nil }
type nativeTestSFTPManager struct {
	entries []sftpdomain.FileEntry
	writes  []string
	readContent string
	readErr error
	writeErr error
}

func (m *nativeTestSFTPManager) Connect(context.Context, string, string, int, string, string, map[string]string) error { return nil }
func (m *nativeTestSFTPManager) Connected(string) bool { return true }
func (m *nativeTestSFTPManager) ListDir(string, string) ([]sftpdomain.FileEntry, error) { return m.entries, nil }
func (m *nativeTestSFTPManager) Stat(string, string) (sftpdomain.FileEntry, error) { return sftpdomain.FileEntry{Name: "secret.conf", Path: "/etc/secret.conf", Size: 42, Mode: "-rw-------", ModTime: "2026-09-19T00:00:00Z"}, nil }
func (m *nativeTestSFTPManager) ReadFile(string, string) (string, error) { return m.readContent, m.readErr }
func (m *nativeTestSFTPManager) WriteFile(_ string, path, content string) error {
	m.writes = append(m.writes, path+":"+content)
	return m.writeErr
}
func (m *nativeTestSFTPManager) UploadFile(string, string, string) error { return nil }
func (m *nativeTestSFTPManager) DownloadFile(string, string, string) error { return nil }
func (m *nativeTestSFTPManager) Disconnect(string) error { return nil }
func (m *nativeTestSSHManager) ExecCommandResult(_ context.Context, tabID, command string) (sessions.CommandExecutionResult, error) { m.mu.Lock(); defer m.mu.Unlock(); m.commands = append(m.commands, tabID+":"+command+"\n"); return sessions.CommandExecutionResult{Success:true, ExitCode:0, Stdout:"mock output"}, nil }


func TestDispatchNativeSFTPWriteRequiresApprovalAndDoesNotWrite(t *testing.T) {
	store := memory.NewStore()
	profile := sessions.Profile{ID: "host-1", Name: "host-1", ProtocolID: "ssh", Host: "host", Port: 22, Username: "ops"}
	if err := store.UpsertSessionProfile(profile); err != nil { t.Fatalf("seed profile: %v", err) }
	store.OpenRuntimeTab(workspace.Tab{ID: "session-1", ProfileID: profile.ID, ProtocolID: "ssh", Status: "connected"})
	sftp := &nativeTestSFTPManager{}
	service := NewService(store, nil, sftp)
	call := nativeToolCall{ID: "call-write", Type: "function"}
	call.Function.Name = nativeSFTPWriteToolName
	call.Function.Arguments = `{"sessionId":"session-1","path":"/etc/eiksy.conf","content":"safe=true","reason":"apply approved configuration"}`
	result, pending, err := service.dispatchNativeToolCall(call, ai.CommandPolicy{}, "session-1", "provider-1", "change config", nil)
	if err != nil { t.Fatalf("dispatch returned unexpected error: %v", err) }
	if !pending { t.Fatal("SFTP write must require approval") }
	if !strings.Contains(result, "approval_required") { t.Fatalf("expected approval-required result, got %q", result) }
	if len(sftp.writes) != 0 { t.Fatalf("SFTP write occurred before approval: %#v", sftp.writes) }
	state := store.AIState()
	if state.PendingNativeToolCall == nil || state.PendingNativeToolCall.ToolName != nativeSFTPWriteToolName { t.Fatalf("expected pending SFTP write, got %#v", state.PendingNativeToolCall) }
}

func TestDispatchNativeSFTPWriteRejectsDifferentSession(t *testing.T) {
	service := NewService(memory.NewStore(), nil, &nativeTestSFTPManager{})
	call := nativeToolCall{ID: "call-write", Type: "function"}
	call.Function.Name = nativeSFTPWriteToolName
	call.Function.Arguments = `{"sessionId":"session-2","path":"/tmp/test","content":"x"}`
	result, pending, err := service.dispatchNativeToolCall(call, ai.CommandPolicy{}, "session-1", "provider-1", "write", nil)
	if err != nil { t.Fatalf("dispatch returned unexpected error: %v", err) }
	if pending { t.Fatal("cross-session write must not create approval") }
	if !strings.Contains(result, "does not match the active Eiksy session") { t.Fatalf("expected active-session error, got %q", result) }
}

func TestDispatchNativeSFTPWriteRejectsOversizedContent(t *testing.T) {
	store := memory.NewStore()
	profile := sessions.Profile{ID: "host-1", Name: "host-1", ProtocolID: "ssh", Host: "host", Port: 22, Username: "ops"}
	if err := store.UpsertSessionProfile(profile); err != nil { t.Fatalf("seed profile: %v", err) }
	store.OpenRuntimeTab(workspace.Tab{ID: "session-1", ProfileID: profile.ID, ProtocolID: "ssh", Status: "connected"})
	service := NewService(store, nil, &nativeTestSFTPManager{})
	call := nativeToolCall{ID: "call-write", Type: "function"}
	call.Function.Name = nativeSFTPWriteToolName
	call.Function.Arguments = fmt.Sprintf(`{"sessionId":"session-1","path":"/tmp/test","content":%q}`, strings.Repeat("x", maxNativeSFTPWriteSize+1))
	result, pending, err := service.dispatchNativeToolCall(call, ai.CommandPolicy{}, "session-1", "provider-1", "write", nil)
	if err != nil { t.Fatalf("dispatch returned unexpected error: %v", err) }
	if pending { t.Fatal("oversized write must not create approval") }
	if !strings.Contains(result, "request_too_large") { t.Fatalf("expected size error, got %q", result) }
}

func TestResolveNativeSFTPWriteExecutesOnlyAfterApproval(t *testing.T) {
	store := memory.NewStore()
	profile := sessions.Profile{ID: "host-1", Name: "host-1", ProtocolID: "ssh", Host: "host", Port: 22, Username: "ops"}
	if err := store.UpsertSessionProfile(profile); err != nil { t.Fatalf("seed profile: %v", err) }
	store.OpenRuntimeTab(workspace.Tab{ID: "session-1", ProfileID: profile.ID, ProtocolID: "ssh", Status: "connected"})
	sftp := &nativeTestSFTPManager{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"SFTP write completed."}}]}`))
	}))
	defer server.Close()
	state := ai.WorkspaceState{
		Providers: []ai.ProviderDescriptor{{ID: "provider-1", Model: "test", Endpoint: server.URL + "/v1", Class: ai.ProviderClassOpenAICompatible, Configured: true}},
		CommandPolicy: ai.CommandPolicy{Tools: []ai.CommandTool{{ID: "sftp", Enabled: true}}},
	}
	store.UpdateAIState(state)
	service := NewService(store, nil, sftp)
	call := nativeToolCall{ID: "call-write", Type: "function"}
	call.Function.Name = nativeSFTPWriteToolName
	call.Function.Arguments = `{"sessionId":"session-1","path":"/tmp/eiksy.conf","content":"enabled=true","reason":"apply config"}`
	_, pending, err := service.dispatchNativeToolCall(call, state.CommandPolicy, "session-1", "provider-1", "change config", nil)
	if err != nil || !pending { t.Fatalf("expected pending write: pending=%v err=%v", pending, err) }
	request := store.AIState().CommandPolicy.PendingRequests[0]
	if len(sftp.writes) != 0 { t.Fatal("write happened before approval") }
	if err := service.ResolveCommandPolicyRequest(request.ID, ai.CommandPermissionModeNow); err != nil { t.Fatalf("approve write: %v", err) }
	if len(sftp.writes) != 1 || sftp.writes[0] != "/tmp/eiksy.conf:enabled=true" { t.Fatalf("expected one approved write, got %#v", sftp.writes) }
	if len(store.AIState().CommandPolicy.CommandRules) != 0 { t.Fatal("SFTP write approval must not create a reusable command rule") }
}

func TestResolveNativeSFTPWriteDenialDoesNotWrite(t *testing.T) {
	store := memory.NewStore()
	profile := sessions.Profile{ID: "host-1", Name: "host-1", ProtocolID: "ssh", Host: "host", Port: 22, Username: "ops"}
	if err := store.UpsertSessionProfile(profile); err != nil { t.Fatalf("seed profile: %v", err) }
	store.OpenRuntimeTab(workspace.Tab{ID: "session-1", ProfileID: profile.ID, ProtocolID: "ssh", Status: "connected"})
	sftp := &nativeTestSFTPManager{}
	store.UpdateAIState(ai.WorkspaceState{Providers: []ai.ProviderDescriptor{{ID: "provider-1", Configured: true}}, CommandPolicy: ai.CommandPolicy{Tools: []ai.CommandTool{{ID: "sftp", Enabled: true}}}})
	service := NewService(store, nil, sftp)
	call := nativeToolCall{ID: "call-write", Type: "function"}
	call.Function.Name = nativeSFTPWriteToolName
	call.Function.Arguments = `{"sessionId":"session-1","path":"/tmp/eiksy.conf","content":"enabled=true"}`
	_, pending, err := service.dispatchNativeToolCall(call, ai.CommandPolicy{Tools: []ai.CommandTool{{ID: "sftp", Enabled: true}}}, "session-1", "provider-1", "write", nil)
	if err != nil || !pending { t.Fatalf("expected pending write: pending=%v err=%v", pending, err) }
	request := store.AIState().CommandPolicy.PendingRequests[0]
	if err := service.ResolveCommandPolicyRequest(request.ID, ai.CommandPermissionModeDeny); err != nil && !strings.Contains(err.Error(), "unsupported protocol scheme") { t.Fatalf("deny write: %v", err) }
	if len(sftp.writes) != 0 { t.Fatalf("denied write was executed: %#v", sftp.writes) }
}


func TestDispatchNativeSFTPReadReturnsBoundedContent(t *testing.T) {
	store := memory.NewStore()
	profile := sessions.Profile{ID: "host-1", Name: "host-1", ProtocolID: "ssh", Host: "host", Port: 22, Username: "ops"}
	if err := store.UpsertSessionProfile(profile); err != nil { t.Fatalf("seed profile: %v", err) }
	store.OpenRuntimeTab(workspace.Tab{ID: "session-1", ProfileID: profile.ID, ProtocolID: "ssh", Status: "connected"})
	sftp := &nativeTestSFTPManager{readContent: strings.Repeat("x", maxNativeSFTPReadSize+10)}
	store.UpdateAIState(ai.WorkspaceState{CommandPolicy: ai.CommandPolicy{Tools: []ai.CommandTool{{ID: "sftp", Enabled: true}}}})
	service := NewService(store, nil, sftp)
	call := nativeToolCall{ID: "call-read", Type: "function"}
	call.Function.Name = nativeSFTPReadToolName
	call.Function.Arguments = `{"sessionId":"session-1","path":"/tmp/eiksy.conf"}`
	result, pending, err := service.dispatchNativeToolCall(call, store.AIState().CommandPolicy, "session-1", "provider-1", "inspect file", nil)
	if err != nil || pending { t.Fatalf("expected read-only non-pending result: pending=%v err=%v", pending, err) }
	var payload struct { Content string `json:"content"`; Bytes int `json:"bytes"`; Truncated bool `json:"truncated"` }
	if err := json.Unmarshal([]byte(result), &payload); err != nil { t.Fatalf("decode result: %v", err) }
	if len(payload.Content) != maxNativeSFTPReadSize || payload.Bytes != maxNativeSFTPReadSize+10 || !payload.Truncated { t.Fatalf("unexpected bounded read: len=%d bytes=%d truncated=%v", len(payload.Content), payload.Bytes, payload.Truncated) }
}

func TestDispatchNativeSFTPReadEmitsRedactedActivity(t *testing.T) {
	store := memory.NewStore()
	profile := sessions.Profile{ID: "host-1", Name: "host-1", ProtocolID: "ssh", Host: "host", Port: 22, Username: "ops"}
	if err := store.UpsertSessionProfile(profile); err != nil { t.Fatalf("seed profile: %v", err) }
	store.OpenRuntimeTab(workspace.Tab{ID: "session-1", ProfileID: profile.ID, ProtocolID: "ssh", Status: "connected"})
	secret := "TOP-SECRET-CONTENT"
	store.UpdateAIState(ai.WorkspaceState{CommandPolicy: ai.CommandPolicy{Tools: []ai.CommandTool{{ID: "sftp", Enabled: true}}}})
	service := NewService(store, nil, &nativeTestSFTPManager{readContent: secret})
	var eventName string
	var eventData any
	service.SetRuntimeContext(context.Background(), func(name string, data ...interface{}) {
		eventName = name
		if len(data) > 0 { eventData = data[0] }
	})
	call := nativeToolCall{ID: "call-read", Type: "function"}
	call.Function.Name = nativeSFTPReadToolName
	call.Function.Arguments = "{\"sessionId\":\"session-1\",\"path\":\"/tmp/secret.conf\"}"
	_, pending, err := service.dispatchNativeToolCall(call, store.AIState().CommandPolicy, "session-1", "provider-1", "inspect", nil)
	if err != nil || pending { t.Fatalf("expected successful read: pending=%v err=%v", pending, err) }
	if eventName != "ai:operate" { t.Fatalf("expected ai:operate event, got %q", eventName) }
	payload, ok := eventData.(map[string]any)
	if !ok { t.Fatalf("unexpected event payload type %T", eventData) }
	if payload["status"] != "executed" { t.Fatalf("expected executed status, got %#v", payload["status"]) }
	if payload["sessionId"] != "session-1" { t.Fatalf("unexpected session id: %#v", payload["sessionId"]) }
	command, ok := payload["command"].(string)
	if !ok || !strings.Contains(command, "sftp.read /tmp/secret.conf") || !strings.Contains(command, fmt.Sprintf("%d bytes", len(secret))) {
		t.Fatalf("unexpected read activity command: %#v", payload["command"])
	}
	if strings.Contains(fmt.Sprint(payload), secret) { t.Fatal("SFTP read activity leaked file content") }
	if payload["stdout"] != "" || payload["stderr"] != "" { t.Fatal("SFTP read activity must not expose file content as output") }
}

func TestDispatchNativeSFTPReadRejectsDifferentSession(t *testing.T) {
	store := memory.NewStore()
	store.UpdateAIState(ai.WorkspaceState{CommandPolicy: ai.CommandPolicy{Tools: []ai.CommandTool{{ID: "sftp", Enabled: true}}}})
	service := NewService(store, nil, &nativeTestSFTPManager{readContent: "secret"})
	call := nativeToolCall{ID: "call-read", Type: "function"}
	call.Function.Name = nativeSFTPReadToolName
	call.Function.Arguments = `{"sessionId":"session-2","path":"/tmp/test"}`
	result, pending, err := service.dispatchNativeToolCall(call, store.AIState().CommandPolicy, "session-1", "provider-1", "read", nil)
	if err != nil || pending { t.Fatalf("expected rejected non-pending read: pending=%v err=%v", pending, err) }
	if !strings.Contains(result, "does not match the active Eiksy session") { t.Fatalf("expected active-session error, got %q", result) }
}

func TestDispatchNativeSFTPReadRequiresSFTPPolicy(t *testing.T) {
	store := memory.NewStore()
	store.UpdateAIState(ai.WorkspaceState{CommandPolicy: ai.CommandPolicy{Tools: []ai.CommandTool{{ID: "sftp", Enabled: false}}}})
	service := NewService(store, nil, &nativeTestSFTPManager{readContent: "secret"})
	call := nativeToolCall{ID: "call-read", Type: "function"}
	call.Function.Name = nativeSFTPReadToolName
	call.Function.Arguments = `{"sessionId":"session-1","path":"/tmp/test"}`
	result, pending, err := service.dispatchNativeToolCall(call, ai.CommandPolicy{}, "session-1", "provider-1", "read", nil)
	if err != nil || pending { t.Fatalf("expected disabled-tool result: pending=%v err=%v", pending, err) }
	if !strings.Contains(result, "disabled") { t.Fatalf("expected disabled-tool error, got %q", result) }
}
func TestDispatchNativeSFTPListRequiresSFTPPolicy(t *testing.T) {
	store := memory.NewStore()
	store.UpdateAIState(ai.WorkspaceState{CommandPolicy: ai.CommandPolicy{Tools: []ai.CommandTool{{ID: "sftp", Enabled: false}}}})
	service := NewService(store, nil, &nativeTestSFTPManager{entries: []sftpdomain.FileEntry{{Name: "secret.conf", Path: "/etc/secret.conf"}}})
	call := nativeToolCall{ID: "call-list", Type: "function"}
	call.Function.Name = nativeSFTPListToolName
	call.Function.Arguments = `{"sessionId":"session-1","path":"/etc"}`
	result, pending, err := service.dispatchNativeToolCall(call, store.AIState().CommandPolicy, "session-1", "provider-1", "list", nil)
	if err != nil || pending { t.Fatalf("expected disabled-tool result: pending=%v err=%v", pending, err) }
	if !strings.Contains(result, "disabled") { t.Fatalf("expected disabled-tool error, got %q", result) }
}

func TestDispatchNativeSFTPStatReturnsMetadataOnly(t *testing.T) {
	store := memory.NewStore()
	profile := sessions.Profile{ID:"host-1", Name:"host-1", ProtocolID:"ssh", Host:"host", Port:22, Username:"ops"}
	if err := store.UpsertSessionProfile(profile); err != nil { t.Fatal(err) }
	store.OpenRuntimeTab(workspace.Tab{ID:"session-1", ProfileID:profile.ID, ProtocolID:"ssh", Status:"connected"})
	store.UpdateAIState(ai.WorkspaceState{CommandPolicy:ai.CommandPolicy{Tools:[]ai.CommandTool{{ID:"sftp", Enabled:true}}}})
	service := NewService(store, nil, &nativeTestSFTPManager{})
	call := nativeToolCall{ID:"call-stat", Type:"function"}; call.Function.Name=nativeSFTPStatToolName; call.Function.Arguments=`{"sessionId":"session-1","path":"/etc/secret.conf"}`
	result, pending, err := service.dispatchNativeToolCall(call, store.AIState().CommandPolicy, "session-1", "provider-1", "inspect metadata", nil)
	if err != nil || pending { t.Fatalf("unexpected stat result: pending=%v err=%v", pending, err) }
	var payload map[string]any
	if err := json.Unmarshal([]byte(result), &payload); err != nil { t.Fatal(err) }
	if payload["status"] != "ok" || payload["size"] != float64(42) || payload["isDir"] != false { t.Fatalf("unexpected metadata: %#v", payload) }
	if _, ok := payload["content"]; ok { t.Fatal("stat must not return file content") }
}

func TestDispatchNativeSFTPStatRequiresSFTPPolicy(t *testing.T) {
	store := memory.NewStore()
	store.UpdateAIState(ai.WorkspaceState{CommandPolicy:ai.CommandPolicy{Tools:[]ai.CommandTool{{ID:"sftp", Enabled:false}}}})
	service := NewService(store, nil, &nativeTestSFTPManager{})
	call := nativeToolCall{ID:"call-stat", Type:"function"}; call.Function.Name=nativeSFTPStatToolName; call.Function.Arguments=`{"sessionId":"session-1","path":"/tmp/test"}`
	result, pending, err := service.dispatchNativeToolCall(call, store.AIState().CommandPolicy, "session-1", "provider-1", "stat", nil)
	if err != nil || pending || !strings.Contains(result, "disabled") { t.Fatalf("expected disabled stat: pending=%v err=%v result=%q", pending, err, result) }
}

func TestDispatchNativeSFTPListReturnsBoundedEntries(t *testing.T) {
	store := memory.NewStore()
	profile := sessions.Profile{ID: "host-1", Name: "host-1", ProtocolID: "ssh", Host: "host", Port: 22, Username: "ops"}
	if err := store.UpsertSessionProfile(profile); err != nil { t.Fatalf("seed profile: %v", err) }
	entries := make([]sftpdomain.FileEntry, 300)
	for i := range entries { entries[i] = sftpdomain.FileEntry{Name: fmt.Sprintf("file-%d", i), Path: fmt.Sprintf("/tmp/file-%d", i)} }
	service := NewService(store, nil, &nativeTestSFTPManager{entries: entries})
	store.OpenRuntimeTab(workspace.Tab{ID: "session-1", ProfileID: profile.ID, ProtocolID: "ssh", Status: "connected"})
	call := nativeToolCall{ID: "call-sftp", Type: "function"}
	call.Function.Name = nativeSFTPListToolName
	call.Function.Arguments = `{"sessionId":"session-1","path":"/tmp"}`
	result, pending, err := service.dispatchNativeToolCall(call, ai.CommandPolicy{}, "session-1", "provider-1", "inspect files", nil)
	if err != nil { t.Fatalf("dispatch returned unexpected error: %v", err) }
	if pending { t.Fatal("read-only SFTP listing must not require approval") }
	var payload struct { Entries []sftpdomain.FileEntry `json:"entries"`; Truncated bool `json:"truncated"` }
	if err := json.Unmarshal([]byte(result), &payload); err != nil { t.Fatalf("decode result: %v", err) }
	if len(payload.Entries) != maxNativeSFTPListEntries || !payload.Truncated { t.Fatalf("expected bounded/truncated result, got len=%d truncated=%v", len(payload.Entries), payload.Truncated) }
}

func TestDispatchNativeSFTPListEmitsRedactedActivity(t *testing.T) {
	store := memory.NewStore()
	profile := sessions.Profile{ID: "host-1", Name: "host-1", ProtocolID: "ssh", Host: "host", Port: 22, Username: "ops"}
	if err := store.UpsertSessionProfile(profile); err != nil { t.Fatalf("seed profile: %v", err) }
	store.OpenRuntimeTab(workspace.Tab{ID: "session-1", ProfileID: profile.ID, ProtocolID: "ssh", Status: "connected"})
	entries := []sftpdomain.FileEntry{
		{Name: "TOP-SECRET-CONFIG", Path: "/etc/secret.conf"},
		{Name: "public.conf", Path: "/etc/public.conf"},
	}
	store.UpdateAIState(ai.WorkspaceState{CommandPolicy: ai.CommandPolicy{Tools: []ai.CommandTool{{ID: "sftp", Enabled: true}}}})
	service := NewService(store, nil, &nativeTestSFTPManager{entries: entries})
	var eventName string
	var eventData any
	service.SetRuntimeContext(context.Background(), func(name string, data ...interface{}) {
		eventName = name
		if len(data) > 0 { eventData = data[0] }
	})
	call := nativeToolCall{ID: "call-list", Type: "function"}
	call.Function.Name = nativeSFTPListToolName
	call.Function.Arguments = `{"sessionId":"session-1","path":"/etc"}`
	_, pending, err := service.dispatchNativeToolCall(call, store.AIState().CommandPolicy, "session-1", "provider-1", "inspect files", nil)
	if err != nil || pending { t.Fatalf("expected successful read-only listing: pending=%v err=%v", pending, err) }
	if eventName != "ai:operate" { t.Fatalf("expected ai:operate event, got %q", eventName) }
	payload, ok := eventData.(map[string]any)
	if !ok { t.Fatalf("unexpected event payload type %T", eventData) }
	if payload["status"] != "executed" { t.Fatalf("expected executed status, got %#v", payload["status"]) }
	if payload["sessionId"] != "session-1" { t.Fatalf("unexpected session id: %#v", payload["sessionId"]) }
	command, ok := payload["command"].(string)
	if !ok || !strings.Contains(command, "sftp.list /etc") || !strings.Contains(command, "2 entries") {
		t.Fatalf("unexpected list activity command: %#v", payload["command"])
	}
	if strings.Contains(fmt.Sprint(payload), "TOP-SECRET-CONFIG") || strings.Contains(fmt.Sprint(payload), "/etc/secret.conf") {
		t.Fatal("SFTP list activity leaked directory entry data")
	}
	if payload["stdout"] != "" || payload["stderr"] != "" { t.Fatal("SFTP list activity must not expose directory entries as output") }
}

func TestDispatchNativeSFTPListRejectsDifferentSession(t *testing.T) {
	service := NewService(memory.NewStore(), nil, &nativeTestSFTPManager{})
	call := nativeToolCall{ID: "call-sftp", Type: "function"}
	call.Function.Name = nativeSFTPListToolName
	call.Function.Arguments = `{"sessionId":"session-2","path":"/tmp"}`
	result, pending, err := service.dispatchNativeToolCall(call, ai.CommandPolicy{}, "session-1", "provider-1", "inspect files", nil)
	if err != nil { t.Fatalf("dispatch returned unexpected error: %v", err) }
	if pending { t.Fatal("different session must not create pending state") }
	if !strings.Contains(result, "does not match the active Eiksy session") { t.Fatalf("expected active-session validation error, got %q", result) }
}
func TestCallNativeToolCompletionParsesToolCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" { t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path) }
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil { t.Fatalf("decode request: %v", err) }
		if _, ok := request["tools"]; !ok { t.Fatal("expected tools in chat completion request") }
		if request["tool_choice"] != "auto" { t.Fatalf("expected tool_choice=auto, got %#v", request["tool_choice"]) }
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"call-1","type":"function","function":{"name":"ssh.exec","arguments":"{\"sessionId\":\"session-1\",\"command\":\"uname -a\",\"reason\":\"inspect host\"}"}}]}}]}`))
	}))
	defer server.Close()
	service := NewService(memory.NewStore(), nil, nil)
	provider := &ai.ProviderDescriptor{Model: "qwen3", Endpoint: server.URL + "/v1"}
	result, err := service.callNativeToolCompletion(context.Background(), provider, []nativeChatMessage{{Role: "user", Content: "inspect"}}, "chat-1", openAIToolDefinitions())
	if err != nil { t.Fatalf("call native completion: %v", err) }
	if len(result.ToolCalls) != 1 { t.Fatalf("expected one tool call, got %d", len(result.ToolCalls)) }
	if result.ToolCalls[0].Function.Name != nativeSSHExecToolName { t.Fatalf("expected ssh.exec, got %q", result.ToolCalls[0].Function.Name) }
}

func TestDispatchNativeToolCallRejectsDifferentSession(t *testing.T) {
	service := NewService(memory.NewStore(), nil, nil)
	call := nativeToolCall{ID: "call-1", Type: "function"}
	call.Function.Name = nativeSSHExecToolName
	call.Function.Arguments = `{"sessionId":"session-2","command":"uname -a"}`
	result, pending, err := service.dispatchNativeToolCall(call, ai.CommandPolicy{Tools: []ai.CommandTool{{ID: nativeSSHExecPolicyToolID, Enabled: true}}}, "session-1", "provider-1", "inspect", nil)
	if err != nil { t.Fatalf("dispatch returned unexpected error: %v", err) }
	if pending { t.Fatal("different session must not create an approval request") }
	if !strings.Contains(result, "does not match the active Eiksy session") { t.Fatalf("expected active-session validation error, got %q", result) }
}

func TestDispatchNativeToolCallRejectsUnknownArguments(t *testing.T) {
	service := NewService(memory.NewStore(), nil, nil)
	call := nativeToolCall{ID: "call-1", Type: "function"}
	call.Function.Name = nativeSSHExecToolName
	call.Function.Arguments = `{"sessionId":"session-1","command":"uname -a","unexpected":true}`
	result, pending, err := service.dispatchNativeToolCall(call, ai.CommandPolicy{Tools: []ai.CommandTool{{ID: nativeSSHExecPolicyToolID, Enabled: true}}}, "session-1", "provider-1", "inspect", nil)
	if err != nil { t.Fatalf("dispatch returned unexpected error: %v", err) }
	if pending { t.Fatal("invalid arguments must not create an approval request") }
	if result == "" || !strings.Contains(result, "invalid arguments") { t.Fatalf("expected structured invalid-arguments result, got %q", result) }
}

func TestNativeToolSystemPromptDoesNotExposeCredentials(t *testing.T) {
	service := NewService(memory.NewStore(), nil, nil)
	prompt := service.nativeToolSystemPrompt(ai.CommandPolicy{Tools: []ai.CommandTool{{ID: nativeSSHExecPolicyToolID, Enabled: true}}}, "session-1")
	if prompt == "" { t.Fatal("expected system prompt") }
	if containsAny(prompt, "password", "token", "private key", "secret") { t.Fatalf("system prompt appears to expose credential material: %q", prompt) }
}

func TestNativeToolApprovalResumesConversation(t *testing.T) {
	store := memory.NewStore()
	ssh := &nativeTestSSHManager{}
	store.OpenRuntimeTab(workspace.Tab{ID: "session-1", ProtocolID: "ssh", Status: "connected"})
	store.UpdateAIState(ai.WorkspaceState{
		Providers: []ai.ProviderDescriptor{{ID: "provider-1", Model: "qwen3", Endpoint: "", Class: ai.ProviderClassOpenAICompatible, Selected: true, Configured: true}},
		CommandPolicy: ai.CommandPolicy{Tools: []ai.CommandTool{{ID: nativeSSHExecPolicyToolID, Enabled: true}}},
		ChatSessionID: "chat-1",
	})
	var mu sync.Mutex
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct { Messages []nativeChatMessage `json:"messages"` }
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil { t.Fatalf("decode request: %v", err) }
		mu.Lock(); requestCount++; count := requestCount; mu.Unlock()
		if count == 1 {
			_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"call-approval","type":"function","function":{"name":"ssh.exec","arguments":"{\"sessionId\":\"session-1\",\"command\":\"uname -a\",\"reason\":\"inspect host\"}"}}]}}]}`))
			return
		}
		foundToolResult := false
		for _, message := range request.Messages { if message.Role == "tool" && message.ToolCallID == "call-approval" && strings.Contains(message.Content, `"status":"executed"`) { foundToolResult = true } }
		if !foundToolResult { t.Fatalf("expected executed tool result in continuation: %#v", request.Messages) }
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"Command completed successfully."}}]}`))
	}))
	defer server.Close()
	state := store.AIState(); state.Providers[0].Endpoint = server.URL + "/v1"; store.UpdateAIState(state)
	service := NewService(store, ssh, nil)
	if _, handled, err := service.sendChatMessageWithNativeTools(context.Background(), &state.Providers[0], state, "session-1", "inspect host"); err != nil || !handled { t.Fatalf("initial native call failed: handled=%v err=%v", handled, err) }
	pending := store.AIState().PendingNativeToolCall
	if pending == nil { t.Fatal("expected pending native tool call") }
	if pending.ToolCallID != "call-approval" { t.Fatalf("unexpected pending tool call id: %q", pending.ToolCallID) }
	if err := service.ResolveCommandPolicyRequest(pending.RequestID, ai.CommandPermissionModeNow); err != nil { t.Fatalf("resolve approval: %v", err) }
	if len(ssh.commands) != 1 || ssh.commands[0] != "session-1:uname -a\n" { t.Fatalf("expected one executed command, got %#v", ssh.commands) }
	finalState := store.AIState()
	if finalState.PendingNativeToolCall != nil { t.Fatal("expected pending native tool call to be cleared") }
	if len(finalState.CommandPolicy.PendingRequests) != 0 { t.Fatalf("expected pending requests to be cleared, got %d", len(finalState.CommandPolicy.PendingRequests)) }
	if len(finalState.Messages) != 2 || finalState.Messages[0].Role != "user" || finalState.Messages[1].Content != "Command completed successfully." { t.Fatalf("unexpected final chat messages: %#v", finalState.Messages) }
}

var _ sessions.Profile

func TestEmitNativeOperationRedactsAndBoundsOutput(t *testing.T) {
	service := NewService(memory.NewStore(), nil, nil)
	var eventName string
	var eventData any
	service.SetRuntimeContext(context.Background(), func(name string, data ...interface{}) {
		eventName = name
		if len(data) > 0 {
			eventData = data[0]
		}
	})
	service.emitNativeOperation("executed", "session-1", "curl --token supersecret", sessions.CommandExecutionResult{
		Success: true,
		ExitCode: 0,
		Stdout: strings.Repeat("x", maxNativeOperationOutput+10),
		Stderr: "Authorization: Bearer super-secret",
		DurationMs: 42,
	}, "not_required", "")
	if eventName != "ai:operate" {
		t.Fatalf("expected ai:operate event, got %q", eventName)
	}
	payload, ok := eventData.(map[string]any)
	if !ok {
		t.Fatalf("unexpected event payload type %T", eventData)
	}
	if strings.Contains(payload["command"].(string), "supersecret") {
		t.Fatal("event command contains secret")
	}
	if len(payload["stdout"].(string)) > maxNativeOperationOutput+len("\n[output truncated]") {
		t.Fatal("event stdout was not bounded")
	}
	if strings.Contains(payload["stderr"].(string), "super-secret") {
		t.Fatal("event stderr contains secret")
	}
}
