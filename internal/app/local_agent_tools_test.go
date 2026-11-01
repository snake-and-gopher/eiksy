package app

import (
	"context"
	"encoding/json"
	"strings"
	"os"
	"path/filepath"
	"testing"

	domainai "eiksy/internal/domain/ai"
	"eiksy/internal/storage/memory"
)

func localCall(name, args string) nativeToolCall {
	var call nativeToolCall
	call.ID = "local-call-1"
	call.Type = "function"
	call.Function.Name = name
	call.Function.Arguments = args
	return call
}

func TestLocalAgentServiceHasNoSSHOrSFTPManagers(t *testing.T) {
	service, err := NewLocalAgentService(memory.NewStore())
	if err != nil { t.Fatalf("create local agent service: %v", err) }
	if service.localExecutor == nil { t.Fatal("local executor was not initialized") }
	if service.sshManager != nil || service.sftpManager != nil { t.Fatal("local agent service must not initialize SSH or SFTP managers") }
}

func TestLocalFileWriteRequiresApprovalByDefault(t *testing.T) {
	service, err := NewLocalAgentService(memory.NewStore())
	if err != nil { t.Fatal(err) }
	target := filepath.Join(t.TempDir(), "approval.txt")
	call := localCall(nativeLocalFileWriteToolName, `{"path":`+mustJSON(t,target)+`,"content":"must-not-exist","reason":"create a test file"}`)
	result, pending, err := service.dispatchLocalToolCall(context.Background(), call, service.store.AIState().CommandPolicy, "provider-1", "write a file", []nativeChatMessage{{Role:"user",Content:"write a file"}})
	if err != nil { t.Fatalf("dispatch local write: %v", err) }
	if !pending { t.Fatalf("expected approval, got result %s", result) }
	if _, err := os.Stat(target); !os.IsNotExist(err) { t.Fatalf("file was written before approval: %v", err) }
	state := service.store.AIState()
	if state.PendingNativeToolCall == nil || state.PendingNativeToolCall.Target != commandTargetLocal { t.Fatalf("expected local pending call, got %#v", state.PendingNativeToolCall) }
	if state.PendingNativeToolCall.SessionID != "" { t.Fatalf("local pending call must not carry a session id: %#v", state.PendingNativeToolCall) }
	if len(state.CommandPolicy.PendingRequests) != 1 || state.CommandPolicy.PendingRequests[0].Target != commandTargetLocal { t.Fatalf("expected target-aware local approval request: %#v", state.CommandPolicy.PendingRequests) }
}

func TestLocalFileWriteExecutesOnlyWhenLocalPolicyAllows(t *testing.T) {
	service, err := NewLocalAgentService(memory.NewStore())
	if err != nil { t.Fatal(err) }
	target := filepath.Join(t.TempDir(), "allowed.txt")
	state := service.store.AIState()
	state.CommandPolicy.CommandRules = []domainai.CommandRule{{Target:commandTargetLocal,ToolID:nativeLocalFilesPolicyToolID,Pattern:target,Action:domainai.CommandPermissionAllow}}
	if err := service.store.UpdateAIState(state); err != nil { t.Fatal(err) }
	call := localCall(nativeLocalFileWriteToolName, `{"path":`+mustJSON(t,target)+`,"content":"approved by policy"}`)
	result, pending, err := service.dispatchLocalToolCall(context.Background(), call, state.CommandPolicy, "provider-1", "write a file", nil)
	if err != nil { t.Fatalf("dispatch local write: %v", err) }
	if pending { t.Fatalf("unexpected approval: %s", result) }
	content, err := os.ReadFile(target)
	if err != nil { t.Fatalf("read written file: %v", err) }
	if string(content) != "approved by policy" { t.Fatalf("unexpected file content %q", content) }
	info, err := os.Stat(target)
	if err != nil { t.Fatal(err) }
	if info.Mode().Perm()&0o077 != 0 { t.Fatalf("expected private file permissions, got %o", info.Mode().Perm()) }
}

func TestLocalShellRejectsShellOperators(t *testing.T) {
	service, err := NewLocalAgentService(memory.NewStore())
	if err != nil { t.Fatal(err) }
	call := localCall(nativeLocalExecToolName, `{"command":"echo safe && touch /tmp/not-allowed"}`)
	result, pending, err := service.dispatchLocalToolCall(context.Background(), call, service.store.AIState().CommandPolicy, "provider-1", "test", nil)
	if err != nil { t.Fatal(err) }
	if pending { t.Fatal("unsafe shell syntax must be denied, not approved") }
	if !contains(result, "denied") { t.Fatalf("expected policy denial, got %s", result) }
}

func mustJSON(t *testing.T, value string) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil { t.Fatal(err) }
	return string(encoded)
}

func contains(value, fragment string) bool { return strings.Contains(value, fragment) }
