package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	agentai "eiksy/internal/ai"
	domainai "eiksy/internal/domain/ai"
	"eiksy/internal/domain/workspace"
	"eiksy/internal/storage/memory"
)

func TestAIBackendAgentRunUsesExistingAIService(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"backend reply"}}]}`))
	}))
	defer server.Close()

	store := memory.NewStore()
	state := store.AIState()
	state.Messages = nil
	state.Providers = []domainai.ProviderDescriptor{{
		ID:         "provider-1",
		Class:      domainai.ProviderClassOpenAICompatible,
		Model:      "test-model",
		Endpoint:   server.URL + "/v1",
		Selected:   true,
		Configured: true,
	}}
	if err := store.UpdateAIState(state); err != nil {
		t.Fatalf("seed AI state: %v", err)
	}

	service := NewService(store, nil, nil)
	agent := NewAIBackendAgent(service)

	events, err := agent.Run(context.Background(), "session-1", "hello")
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	var got []agentai.Event
	for event := range events {
		got = append(got, event)
	}

	if len(got) != 3 {
		t.Fatalf("expected 3 events, got %d: %#v", len(got), got)
	}
	if got[0].Type != agentai.EventMessageStarted {
		t.Fatalf("expected message started, got %q", got[0].Type)
	}
	if got[1].Type != agentai.EventTextDelta || got[1].Content != "backend reply" {
		t.Fatalf("unexpected text event: %#v", got[1])
	}
	if got[2].Type != agentai.EventMessageFinished || got[2].Content != "backend reply" {
		t.Fatalf("unexpected completion event: %#v", got[2])
	}

	finalState := store.AIState()
	if len(finalState.AgentMessages) != 2 ||
		finalState.AgentMessages[0].Role != "user" ||
		finalState.AgentMessages[0].Content != "hello" ||
		finalState.AgentMessages[1].Role != "assistant" ||
		finalState.AgentMessages[1].Content != "backend reply" {
		t.Fatalf("unexpected persisted messages: %#v", finalState.AgentMessages)
	}
}

func TestAIBackendAgentRunReportsConfigurationError(t *testing.T) {
	store := memory.NewStore()
	state := store.AIState()
	state.Messages = nil
	state.Providers = nil
	if err := store.UpdateAIState(state); err != nil {
		t.Fatalf("seed AI state: %v", err)
	}

	agent := NewAIBackendAgent(NewService(store, nil, nil))
	events, err := agent.Run(context.Background(), "session-1", "hello")
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	var got []agentai.Event
	for event := range events {
		got = append(got, event)
	}
	if len(got) != 2 {
		t.Fatalf("expected message started and error, got %#v", got)
	}
	if got[0].Type != agentai.EventMessageStarted {
		t.Fatalf("expected message started, got %q", got[0].Type)
	}
	if got[1].Type != agentai.EventError || got[1].Err == nil {
		t.Fatalf("expected error event, got %#v", got[1])
	}
}

func TestAIBackendAgentRunReportsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	agent := NewAIBackendAgent(NewService(memory.NewStore(), nil, nil))
	events, err := agent.Run(ctx, "session-1", "hello")
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	event, ok := <-events
	if !ok {
		t.Fatal("expected cancellation event")
	}
	if event.Type != agentai.EventCancellation {
		t.Fatalf("expected cancellation, got %q", event.Type)
	}
}


func TestAIBackendAgentRunStreamsNativeToolLifecycle(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.Header().Set("Content-Type", "application/json")
		if requestCount == 1 {
			_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"diag-1","type":"function","function":{"name":"ssh.diagnostics","arguments":"{\"sessionId\":\"session-1\",\"operation\":\"summary\"}"}}]}}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"diagnostics complete"}}]}`))
	}))
	defer server.Close()

	store := memory.NewStore()
	store.OpenRuntimeTab(workspace.Tab{ID: "session-1", ProtocolID: "ssh", Status: "connected"})
	state := store.AIState()
	state.Messages = nil
	state.ChatSessionID = "chat-1"
	state.Providers = []domainai.ProviderDescriptor{{
		ID: "provider-1", Class: domainai.ProviderClassOpenAICompatible, Model: "test-model",
		Endpoint: server.URL + "/v1", Selected: true, Configured: true,
	}}
	state.CommandPolicy = domainai.CommandPolicy{Tools: []domainai.CommandTool{{ID: nativeSSHExecPolicyToolID, Enabled: true}}}
	if err := store.UpdateAIState(state); err != nil {
		t.Fatalf("seed AI state: %v", err)
	}

	service := NewService(store, &nativeTestSSHManager{}, nil)
	events, err := NewAIBackendAgent(service).Run(context.Background(), "session-1", "inspect host")
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	var got []agentai.Event
	for event := range events {
		got = append(got, event)
	}
	if len(got) != 6 {
		t.Fatalf("expected 6 events, got %d: %#v", len(got), got)
	}
	wantTypes := []agentai.EventType{
		agentai.EventMessageStarted, agentai.EventToolStarted, agentai.EventToolOutput,
		agentai.EventToolFinished, agentai.EventTextDelta, agentai.EventMessageFinished,
	}
	for i, want := range wantTypes {
		if got[i].Type != want {
			t.Fatalf("event %d: expected %q, got %q", i, want, got[i].Type)
		}
	}
	if got[1].Tool != nativeSSHDiagnosticsToolName || got[3].Tool != nativeSSHDiagnosticsToolName {
		t.Fatalf("unexpected tool lifecycle events: %#v", got)
	}
	if got[2].Content == "" {
		t.Fatal("expected bounded tool output event")
	}
	if got[4].Content != "diagnostics complete" || got[5].Content != "diagnostics complete" {
		t.Fatalf("unexpected assistant events: %#v", got)
	}
}
