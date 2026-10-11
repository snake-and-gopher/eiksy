package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"eiksy/internal/domain/ai"
	"eiksy/internal/domain/workspace"
	"eiksy/internal/storage/memory"
)

func TestDispatchNativeDiagnosticsSummaryUsesFixedCommands(t *testing.T) {
	store := memory.NewStore()
	ssh := &nativeTestSSHManager{}
	store.OpenRuntimeTab(workspace.Tab{ID: "session-1", ProtocolID: "ssh", Status: "connected"})
	service := NewService(store, ssh, nil)
	var eventName string
	var eventData any
	service.SetRuntimeContext(context.Background(), func(name string, data ...interface{}) {
		eventName = name
		if len(data) > 0 {
			eventData = data[0]
		}
	})
	policy := ai.CommandPolicy{Tools: []ai.CommandTool{{ID: nativeSSHExecPolicyToolID, Enabled: true}}, CommandRules: defaultCommandRules()}

	call := nativeToolCall{ID: "diag-1", Type: "function"}
	call.Function.Name = nativeSSHDiagnosticsToolName
	call.Function.Arguments = `{"sessionId":"session-1","operation":"summary"}`

	result, pending, err := service.dispatchNativeDiagnostics(call, policy, "session-1", "provider-1")
	if err != nil {
		t.Fatalf("dispatch diagnostics: %v", err)
	}
	if pending {
		t.Fatal("diagnostics must not create an approval request")
	}

	var payload struct {
		Status    string `json:"status"`
		SessionID string `json:"sessionId"`
		Operation string `json:"operation"`
		Checks    []struct {
			Name   string `json:"name"`
			Command string `json:"command"`
		} `json:"checks"`
	}
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		t.Fatalf("decode diagnostics result: %v; result=%s", err, result)
	}
	if payload.Status != "completed" || payload.SessionID != "session-1" || payload.Operation != "summary" {
		t.Fatalf("unexpected diagnostics envelope: %#v", payload)
	}
	if len(payload.Checks) != 6 {
		t.Fatalf("expected six fixed checks, got %d", len(payload.Checks))
	}
	for _, check := range payload.Checks {
		if strings.Contains(check.Command, ";") || strings.Contains(check.Command, "|") || strings.Contains(check.Command, "$") {
			t.Fatalf("diagnostic command contains shell composition: %q", check.Command)
		}
	}
	if len(ssh.commands) != 6 {
		t.Fatalf("expected six SSH executions, got %#v", ssh.commands)
	}
	if eventName != "agent:operate" {
		t.Fatalf("expected agent:operate event, got %q", eventName)
	}
	eventPayload, ok := eventData.(map[string]any)
	if !ok {
		t.Fatalf("unexpected event payload type %T", eventData)
	}
	if eventPayload["status"] != "executed" || eventPayload["sessionId"] != "session-1" {
		t.Fatalf("unexpected diagnostics activity: %#v", eventPayload)
	}
	command, ok := eventPayload["command"].(string)
	if !ok || !strings.Contains(command, "ssh.diagnostics summary") || !strings.Contains(command, "6 checks") {
		t.Fatalf("unexpected diagnostics activity command: %#v", eventPayload["command"])
	}
	if eventPayload["stdout"] != "" || eventPayload["stderr"] != "" {
		t.Fatal("diagnostics activity must not expose command output")
	}
}

func TestDispatchNativeDiagnosticsRejectsUnsupportedOperation(t *testing.T) {
	service := NewService(memory.NewStore(), &nativeTestSSHManager{}, nil)
	call := nativeToolCall{ID: "diag-1", Type: "function"}
	call.Function.Name = nativeSSHDiagnosticsToolName
	call.Function.Arguments = `{"sessionId":"session-1","operation":"custom"}`

	result, pending, err := service.dispatchNativeDiagnostics(call, ai.CommandPolicy{}, "session-1", "provider-1")
	if err != nil {
		t.Fatalf("dispatch returned unexpected error: %v", err)
	}
	if pending {
		t.Fatal("unsupported operation must not create an approval request")
	}
	if !strings.Contains(result, "unsupported diagnostic operation") {
		t.Fatalf("expected unsupported-operation error, got %q", result)
	}
}

func TestDispatchNativeDiagnosticsHonorsCommandPolicy(t *testing.T) {
	store := memory.NewStore()
	ssh := &nativeTestSSHManager{}
	store.OpenRuntimeTab(workspace.Tab{ID: "session-1", ProtocolID: "ssh", Status: "connected"})
	service := NewService(store, ssh, nil)
	policy := ai.CommandPolicy{Tools: []ai.CommandTool{{ID: nativeSSHExecPolicyToolID, Enabled: false}}}

	call := nativeToolCall{ID: "diag-1", Type: "function"}
	call.Function.Name = nativeSSHDiagnosticsToolName
	call.Function.Arguments = `{"sessionId":"session-1","operation":"summary"}`

	result, pending, err := service.dispatchNativeDiagnostics(call, policy, "session-1", "provider-1")
	if err != nil {
		t.Fatalf("dispatch returned unexpected error: %v", err)
	}
	if pending {
		t.Fatal("policy denial must not create an approval request")
	}
	if !strings.Contains(result, "policy_denied") {
		t.Fatalf("expected policy denial, got %q", result)
	}
	if len(ssh.commands) != 0 {
		t.Fatalf("diagnostics must not execute when policy denies the tool: %#v", ssh.commands)
	}
}
