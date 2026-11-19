package modelbench

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOllamaAdapterParsesToolCallsAndFirstTokenLatency(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" { t.Fatalf("unexpected path %s", r.URL.Path) }
		var req ollamaChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil { t.Fatal(err) }
		if req.Model != "test-model" || !req.Stream || len(req.Tools) != 7 { t.Fatalf("unexpected request: %+v", req) }
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = fmt.Fprintln(w, `{"message":{"role":"assistant","tool_calls":[{"function":{"name":"files.list","arguments":{"path":"/workspace"}}}]},"done":false}`)
		_, _ = fmt.Fprintln(w, `{"message":{"role":"assistant","content":""},"done":true}`)
	}))
	defer server.Close()

	adapter := NewOllamaAdapter(server.URL, "test-model")
	got, err := adapter.Generate(context.Background(), Request{Scenario: Scenario{ID:"case-1", Input:"List files"}})
	if err != nil { t.Fatal(err) }
	if got.FirstTokenMS < 0 || len(got.Calls) != 1 || got.Calls[0].Name != "files.list" || got.Calls[0].Arguments["path"] != "/workspace" {
		t.Fatalf("unexpected response: %+v", got)
	}
}

func TestOllamaAdapterTreatsTextAsClarification(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = fmt.Fprintln(w, `{"message":{"role":"assistant","content":"Which file do you mean?"},"done":true}`)
	}))
	defer server.Close()
	got, err := NewOllamaAdapter(server.URL, "test-model").Generate(context.Background(), Request{})
	if err != nil { t.Fatal(err) }
	if got.Clarification != "Which file do you mean?" || len(got.Calls) != 0 { t.Fatalf("unexpected response: %+v", got) }
}

func TestOllamaAdapterRejectsHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "model not found", http.StatusNotFound)
	}))
	defer server.Close()
	_, err := NewOllamaAdapter(server.URL, "missing").Generate(context.Background(), Request{})
	if err == nil || !strings.Contains(err.Error(), "HTTP 404") { t.Fatalf("expected HTTP error, got %v", err) }
}
