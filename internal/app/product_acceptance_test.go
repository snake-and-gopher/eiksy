package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"eiksy/internal/domain/ai"
	"eiksy/internal/domain/sessions"
	"eiksy/internal/storage/memory"
)

// TestThinkConnectOperateAcceptance verifies the product contract as one
// application workflow: Think -> Connect -> Operate -> Think.
func TestThinkConnectOperateAcceptance(t *testing.T) {
	store := memory.NewStore()
	profile := sessions.Profile{ID: "acceptance-host", Name: "acceptance-host", Group: "Production", Tags: []string{"linux", "acceptance"}, ProtocolID: "ssh", Host: "prod.example.internal", Port: 22, Username: "ops"}
	if err := store.UpsertSessionProfile(profile); err != nil { t.Fatalf("seed resource: %v", err) }
	ssh := &nativeTestSSHManager{}
	service := NewService(store, ssh, nil)

	// CONNECT: create and explicitly connect the resource.
	tab, err := service.LaunchSession(profile.ID)
	if err != nil { t.Fatalf("launch session: %v", err) }
	if err := service.ConnectSession(tab.ID); err != nil { t.Fatalf("connect session: %v", err) }
	resource, err := service.GetResource(profile.ID)
	if err != nil { t.Fatalf("get connected resource: %v", err) }
	if resource.ActiveSessionID != tab.ID || resource.Status != "connected" { t.Fatalf("resource is not connected: %+v", resource) }

	// THINK: the AI-visible context contains useful operational metadata only.
	infra, err := service.GetAIInfrastructureContext(tab.ID)
	if err != nil { t.Fatalf("build AI context: %v", err) }
	if infra.ActiveSession == nil { t.Fatal("expected active AI infrastructure context") }
	if infra.ActiveSession.Host != profile.Host || infra.ActiveSession.Username != profile.Username { t.Fatalf("unexpected AI context: %+v", infra.ActiveSession) }
	if strings.Contains(infra.ActiveSession.Host, "secret") { t.Fatal("AI context contains unexpected secret material") }

	var mu sync.Mutex
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct { Messages []nativeChatMessage `json:"messages"` }
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil { t.Fatalf("decode AI request: %v", err) }
		mu.Lock(); requestCount++; count := requestCount; mu.Unlock()
		if count == 1 {
			args, err := json.Marshal(map[string]string{"sessionId": tab.ID, "command": "uname -a", "reason": "inspect connected host"})
			if err != nil { t.Fatalf("marshal tool arguments: %v", err) }
			response := map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "", "tool_calls": []any{map[string]any{"id": "accept-call", "type": "function", "function": map[string]any{"name": "ssh.exec", "arguments": string(args)}}}}}}}
			body, err := json.Marshal(response)
			if err != nil { t.Fatalf("marshal AI response: %v", err) }
			_, _ = w.Write([]byte(body)); return
		}
		foundResult := false
		for _, message := range request.Messages { if message.Role == "tool" && message.ToolCallID == "accept-call" && strings.Contains(message.Content, `"status":"executed"`) { foundResult = true } }
		if !foundResult { t.Fatalf("expected operation result in continuation: %#v", request.Messages) }
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"The host check completed successfully."}}]}`))
	}))
	defer server.Close()

	state := store.AIState()
	state.Providers = []ai.ProviderDescriptor{{ID: "acceptance-provider", Name: "Acceptance AI", Class: ai.ProviderClassOpenAICompatible, Model: "test-model", Endpoint: server.URL + "/v1", Selected: true, Configured: true}}
	state.ChatSessionID = "acceptance-chat"
	state.CommandPolicy = ai.CommandPolicy{Tools: []ai.CommandTool{{ID: nativeSSHExecPolicyToolID, Enabled: true}}}
	if err := store.UpdateAIState(state); err != nil { t.Fatalf("configure AI provider: %v", err) }

	// THINK -> OPERATE: AI proposes a command; execution is paused for approval.
	if err := service.SendChatMessage(context.Background(), "Check the connected host.", tab.ID); err != nil { t.Fatalf("send AI request: %v", err) }
	pending := store.AIState().PendingNativeToolCall
	if pending == nil { t.Fatal("expected explicit approval before execution") }
	if len(ssh.commands) != 0 { t.Fatalf("command executed before approval: %#v", ssh.commands) }

	// OPERATE: user approves; Eiksy executes through the existing connector.
	if err := service.ResolveCommandPolicyRequest(pending.RequestID, ai.CommandPermissionModeNow); err != nil { t.Fatalf("approve operation: %v", err) }
	if len(ssh.commands) != 1 || ssh.commands[0] != tab.ID+":uname -a\n" { t.Fatalf("unexpected executed commands: %#v", ssh.commands) }

	// OPERATE -> THINK: bounded operation result returns to the AI conversation.
	finalState := store.AIState()
	if finalState.PendingNativeToolCall != nil || len(finalState.CommandPolicy.PendingRequests) != 0 { t.Fatal("approval continuation state was not cleared") }
	if len(finalState.AgentMessages) < 2 {
		t.Fatalf("expected conversation history, got %#v", finalState.AgentMessages)
	}
	foundUser := false
	foundFinal := false
	for _, message := range finalState.AgentMessages {
		if message.Role == "user" && message.Content == "Check the connected host." {
			foundUser = true
		}
		if message.Role == "assistant" && message.Content == "The host check completed successfully." {
			foundFinal = true
		}
	}
	if !foundUser || !foundFinal {
		t.Fatalf("unexpected final conversation: %#v", finalState.AgentMessages)
	}
	audit := service.GetCommandAuditTrail()
	var executedAudit *ai.CommandAuditEvent
	for i := range audit {
		if audit[i].SessionID == tab.ID && audit[i].Approval == string(ai.CommandPermissionModeNow) && audit[i].Result == "executed" {
			event := audit[i]
			executedAudit = &event
			break
		}
	}
	if executedAudit == nil {
		t.Fatalf("expected executed operation audit event, got %+v", audit)
	}
}
