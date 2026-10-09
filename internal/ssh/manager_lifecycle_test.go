package sshmanager

import "testing"

func TestDisconnectClearsPerTabState(t *testing.T) {
	m := NewManager()
	m.handlers["tab-1"] = func(string) {}
	m.pendingKeys["tab-1"] = &PendingHostKey{Hostname: "host"}

	if err := m.Disconnect("tab-1"); err != nil {
		t.Fatalf("disconnect: %v", err)
	}

	if _, ok := m.handlers["tab-1"]; ok {
		t.Fatal("expected output handler to be removed")
	}
	if _, ok := m.pendingKeys["tab-1"]; ok {
		t.Fatal("expected pending host key to be removed")
	}
}

func TestStaleSessionCannotDisconnectReconnectedTab(t *testing.T) {
	m := NewManager()
	oldConn := &connection{}
	newConn := &connection{}
	m.connections["tab-1"] = newConn

	if err := m.disconnectConnection("tab-1", oldConn); err != nil {
		t.Fatalf("stale disconnect: %v", err)
	}

	if m.connections["tab-1"] != newConn {
		t.Fatal("stale session disconnected the active replacement connection")
	}
}

func TestActiveSessionDisconnectRemovesConnection(t *testing.T) {
	m := NewManager()
	active := &connection{}
	m.connections["tab-1"] = active
	m.handlers["tab-1"] = func(string) {}
	m.pendingKeys["tab-1"] = &PendingHostKey{Hostname: "host"}

	if err := m.disconnectConnection("tab-1", active); err != nil {
		t.Fatalf("active disconnect: %v", err)
	}

	if _, ok := m.connections["tab-1"]; ok {
		t.Fatal("expected active connection to be removed")
	}
	if _, ok := m.handlers["tab-1"]; ok {
		t.Fatal("expected output handler to be removed")
	}
	if _, ok := m.pendingKeys["tab-1"]; ok {
		t.Fatal("expected pending host key to be removed")
	}
}

func TestConnectPreservesOutputHandlerWhenReplacingConnection(t *testing.T) {
	m := NewManager()
	called := false
	handler := func(string) { called = true }
	m.SetOutputHandler("tab-1", handler)

	// A refused local connection fails before a shell starts, but Connect must
	// not silently discard the callback registered by the terminal UI.
	if err := m.Connect(t.Context(), "tab-1", "127.0.0.1", 1, "user", "password", nil); err == nil {
		t.Fatal("expected connection attempt to fail")
	}

	m.mu.RLock()
	got := m.handlers["tab-1"]
	m.mu.RUnlock()
	if got == nil {
		t.Fatal("Connect removed the terminal output handler")
	}
	got("test")
	if !called {
		t.Fatal("preserved output handler was not callable")
	}
}
