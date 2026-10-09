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

// TestOperateThinkLoopContinuesAfterOperationResult verifies that an executed
// operation result is fed back to the model so it can reason and request the
// next operation before producing the final answer.
func TestOperateThinkLoopContinuesAfterOperationResult(t *testing.T) {
	store := memory.NewStore()
	profile := sessions.Profile{ID: "loop-host", Name: "loop-host", ProtocolID: "ssh", Host: "loop.example.internal", Port: 22, Username: "ops"}
	if err := store.UpsertSessionProfile(profile); err != nil { t.Fatalf("seed resource: %v", err) }
	ssh := &nativeTestSSHManager{}
	service := NewService(store, ssh, nil)
	tab, err := service.LaunchSession(profile.ID)
	if err != nil { t.Fatalf("launch session: %v", err) }
	if err := service.ConnectSession(tab.ID); err != nil { t.Fatalf("connect session: %v", err) }

	var mu sync.Mutex
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct { Messages []nativeChatMessage `json:"messages"` }
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil { t.Fatalf("decode AI request: %v", err) }
		mu.Lock(); requestCount++; count := requestCount; mu.Unlock()
		containsToolResult := func(command string) bool {
			for _, message := range request.Messages {
				if message.Role == "tool" && strings.Contains(message.Content, `"status":"executed"`) && strings.Contains(message.Content, `"command":"`+command+`"`) { return true }
			}
			return false
		}
		var response map[string]any
		switch count {
		case 1:
			response = nativeToolCallResponse("loop-call-1", tab.ID, "uname -a", "inspect kernel")
		case 2:
			if !containsToolResult("uname -a") { t.Fatalf("second AI turn did not receive the first operation result: %#v", request.Messages) }
			response = nativeToolCallResponse("loop-call-2", tab.ID, "uptime", "inspect load")
		case 3:
			if !containsToolResult("uptime") { t.Fatalf("third AI turn did not receive the second operation result: %#v", request.Messages) }
			response = map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "The host checks are complete."}}}}
		default:
			t.Fatalf("unexpected extra AI request #%d", count)
		}
		body, err := json.Marshal(response)
		if err != nil { t.Fatalf("marshal AI response: %v", err) }
		_, _ = w.Write(body)
	}))
	defer server.Close()

	state := store.AIState()
	state.Providers = []ai.ProviderDescriptor{{ID: "loop-provider", Name: "Loop AI", Class: ai.ProviderClassOpenAICompatible, Model: "test-model", Endpoint: server.URL + "/v1", Selected: true, Configured: true}}
	state.ChatSessionID = "loop-chat"
	state.CommandPolicy = ai.CommandPolicy{
		Tools: []ai.CommandTool{{ID: nativeSSHExecPolicyToolID, Enabled: true}},
		CommandRules: []ai.CommandRule{
			{ToolID: nativeSSHExecPolicyToolID, SessionID: tab.ID, Pattern: "uname -a", Action: ai.CommandPermissionAllow},
			{ToolID: nativeSSHExecPolicyToolID, SessionID: tab.ID, Pattern: "uptime", Action: ai.CommandPermissionAllow},
		},
	}
	if err := store.UpdateAIState(state); err != nil { t.Fatalf("configure AI provider and policy: %v", err) }

	if err := service.SendChatMessage(context.Background(), "Inspect the host and summarize the checks.", tab.ID); err != nil { t.Fatalf("send AI request: %v", err) }
	mu.Lock(); calls := requestCount; mu.Unlock()
	if calls != 3 { t.Fatalf("expected two operation turns followed by final reasoning turn, got %d AI requests", calls) }
	if len(ssh.commands) != 2 { t.Fatalf("expected two executed operations, got %#v", ssh.commands) }
	if ssh.commands[0] != tab.ID+":uname -a\n" || ssh.commands[1] != tab.ID+":uptime\n" { t.Fatalf("unexpected operation order: %#v", ssh.commands) }

	finalState := store.AIState()
	if finalState.PendingNativeToolCall != nil || len(finalState.CommandPolicy.PendingRequests) != 0 { t.Fatal("operation loop left pending continuation state") }
	foundFinal := false
	for _, message := range finalState.AgentMessages { if message.Role == "assistant" && message.Content == "The host checks are complete." { foundFinal = true; break } }
	if !foundFinal { t.Fatalf("final reasoning response was not persisted: %#v", finalState.AgentMessages) }
}

func nativeToolCallResponse(id, sessionID, command, reason string) map[string]any {
	args, _ := json.Marshal(map[string]string{"sessionId": sessionID, "command": command, "reason": reason})
	return map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "", "tool_calls": []any{map[string]any{"id": id, "type": "function", "function": map[string]any{"name": nativeSSHExecToolName, "arguments": string(args)}}}}}}}
}
