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

func newUnlockedLocalAgentService(t *testing.T) *Service {
	t.Helper()
	store := memory.NewStore()
	if err := store.EnsureMasterPassword("test-master-password"); err != nil { t.Fatal(err) }
	service, err := NewLocalAgentService(store)
	if err != nil { t.Fatal(err) }
	return service
}

func TestLocalAgentServiceHasNoSSHOrSFTPManagers(t *testing.T) {
	service, err := NewLocalAgentService(memory.NewStore())
	if err != nil { t.Fatalf("create local agent service: %v", err) }
	if service.localExecutor == nil { t.Fatal("local executor was not initialized") }
	if service.sshManager != nil || service.sftpManager != nil { t.Fatal("local agent service must not initialize SSH or SFTP managers") }
}

func TestLocalFileWriteRequiresApprovalByDefault(t *testing.T) {
	service := newUnlockedLocalAgentService(t)
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
	service := newUnlockedLocalAgentService(t)
	target := filepath.Join(t.TempDir(), "allowed.txt")
	state := service.store.AIState()
	content := "approved by policy"
	policyCommand := localPolicyCommand(nativeLocalFileWriteToolName, target, content)
	state.CommandPolicy.CommandRules = []domainai.CommandRule{{Target:commandTargetLocal,ToolID:nativeLocalFilesPolicyToolID,Pattern:policyCommand,Action:domainai.CommandPermissionAllow}}
	if err := service.store.UpdateAIState(state); err != nil { t.Fatal(err) }
	call := localCall(nativeLocalFileWriteToolName, `{"path":`+mustJSON(t,target)+`,"content":`+mustJSON(t,content)+`}`)
	result, pending, err := service.dispatchLocalToolCall(context.Background(), call, state.CommandPolicy, "provider-1", "write a file", nil)
	if err != nil { t.Fatalf("dispatch local write: %v", err) }
	if pending { t.Fatalf("unexpected approval: %s", result) }
	written, readErr := os.ReadFile(target)
	if readErr != nil { t.Fatalf("read written file: %v", readErr) }
	if string(written) != "approved by policy" { t.Fatalf("unexpected file content %q", written) }
	info, err := os.Stat(target)
	if err != nil { t.Fatal(err) }
	if info.Mode().Perm()&0o077 != 0 { t.Fatalf("expected private file permissions, got %o", info.Mode().Perm()) }
}

func TestLocalShellRejectsShellOperators(t *testing.T) {
	service := newUnlockedLocalAgentService(t)
	call := localCall(nativeLocalExecToolName, `{"command":"echo safe && touch /tmp/not-allowed"}`)
	result, pending, err := service.dispatchLocalToolCall(context.Background(), call, service.store.AIState().CommandPolicy, "provider-1", "test", nil)
	if err != nil { t.Fatal(err) }
	if pending { t.Fatal("unsafe shell syntax must be denied, not approved") }
	if !strings.Contains(result, "denied") { t.Fatalf("expected policy denial, got %s", result) }
}

func mustJSON(t *testing.T, value string) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil { t.Fatal(err) }
	return string(encoded)
}



func TestLocalShellRejectsPrivilegeEscalation(t *testing.T) {
	service := newUnlockedLocalAgentService(t)
	for _, command := range []string{"sudo id", "/usr/bin/sudo id", "runas.exe cmd.exe", "doas whoami", "powershell Start-Process cmd.exe -Verb RunAs"} {
		call := localCall(nativeLocalExecToolName, `{"command":`+mustJSON(t,command)+`}`)
		result, pending, err := service.dispatchLocalToolCall(context.Background(), call, service.store.AIState().CommandPolicy, "provider-1", "test", nil)
		if err != nil { t.Fatal(err) }
		if pending || !strings.Contains(result, "privilege escalation") { t.Fatalf("expected hard denial for %q, got pending=%v result=%s", command, pending, result) }
	}
}

func TestLocalApprovalDenialDoesNotExecuteAndRecordsDenial(t *testing.T) {
	store := memory.NewStore()
	target := filepath.Join(t.TempDir(), "must-not-exist.txt")
	args, err := json.Marshal(map[string]string{"path":target,"content":"must not be written"})
	if err != nil { t.Fatal(err) }
	messages, err := json.Marshal([]nativeChatMessage{{Role:"user",Content:"write the file"}})
	if err != nil { t.Fatal(err) }

	state := store.AIState()
	state.CommandPolicy.PendingRequests = []domainai.CommandRequest{{
		Target:commandTargetLocal, ID:"request-deny-1", ToolID:nativeLocalFilesPolicyToolID,
		Command:target, RequestedAt:"2026-11-02T00:00:00Z",
	}}
	state.PendingNativeToolCall = &domainai.PendingNativeToolCall{
		Target:commandTargetLocal, RequestID:"request-deny-1", ProviderID:"provider-not-configured",
		ToolCallID:"tool-call-1", ToolName:nativeLocalFileWriteToolName,
		ToolArguments:string(args), UserMessage:"write the file", MessagesJSON:string(messages),
	}
	if err := store.UpdateAIState(state); err != nil { t.Fatal(err) }

	service, err := NewLocalAgentService(store)
	if err != nil { t.Fatal(err) }
	if err := service.ResolveCommandPolicyRequest("request-deny-1", domainai.CommandPermissionModeDeny); err != nil {
		t.Fatalf("deny local operation: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) { t.Fatalf("denied file operation was executed: %v", err) }
	updated := store.AIState()
	if updated.PendingNativeToolCall != nil { t.Fatalf("pending native call was not cleared: %#v", updated.PendingNativeToolCall) }
	if len(updated.CommandPolicy.PendingRequests) != 0 { t.Fatalf("pending approval was not cleared: %#v", updated.CommandPolicy.PendingRequests) }
	if len(updated.AgentMessages) < 2 || !strings.Contains(updated.AgentMessages[len(updated.AgentMessages)-1].Content, "did not execute") {
		t.Fatalf("expected the agent to continue after denial, got %#v", updated.AgentMessages)
	}
	audit := service.GetCommandAuditTrail()
	if len(audit) == 0 || audit[0].Target != commandTargetLocal || audit[0].Approval != "denied" || audit[0].Result != "denied" {
		t.Fatalf("expected a local denial audit event, got %#v", audit)
	}
}

func TestLocalApprovalPreviewBoundsFileWriteContent(t *testing.T) {
	content := strings.Repeat("ж", 900)
	arguments, err := json.Marshal(map[string]string{"path":"/tmp/preview.txt","content":content})
	if err != nil { t.Fatal(err) }
	preview := LocalApprovalPreview(nativeLocalFileWriteToolName, string(arguments))
	if !strings.Contains(preview, `File: "/tmp/preview.txt"`) || !strings.Contains(preview, "Content size: 1800 bytes") {
		t.Fatalf("expected path and byte size in preview, got %q", preview)
	}
	if !strings.Contains(preview, "SHA-256:") { t.Fatalf("expected content fingerprint in preview, got %q", preview) }
	if !strings.Contains(preview, "[preview truncated]") { t.Fatalf("expected bounded preview marker, got %q", preview) }
	if strings.Contains(preview, strings.Repeat("ж", 700)) { t.Fatal("preview should not expose the entire large file content") }
}

func TestLocalPolicyCommandScopesFileWritesByContent(t *testing.T) {
	first := localPolicyCommand(nativeLocalFileWriteToolName, "/tmp/report.txt", "approved content")
	same := localPolicyCommand(nativeLocalFileWriteToolName, "/tmp/report.txt", "approved content")
	changed := localPolicyCommand(nativeLocalFileWriteToolName, "/tmp/report.txt", "different content")
	otherPath := localPolicyCommand(nativeLocalFileWriteToolName, "/tmp/other.txt", "approved content")
	if first != same { t.Fatal("identical writes must have the same policy key") }
	if first == changed { t.Fatal("changing file content must require a new approval policy key") }
	if first == otherPath { t.Fatal("changing the target path must require a new approval policy key") }
}
