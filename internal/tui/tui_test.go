package tui

import (
	"strings"
	"testing"

	agentai "eiksy/internal/ai"
	appservice "eiksy/internal/app"
	domainai "eiksy/internal/domain/ai"
	domainsettings "eiksy/internal/domain/settings"
	securestorage "eiksy/internal/securestorage"
	sftpdomain "eiksy/internal/domain/sftp"
	domainsessions "eiksy/internal/domain/sessions"
	tea "github.com/charmbracelet/bubbletea"
)

func TestModelHandlesWindowSize(t *testing.T) {
	m := NewModel()
	next, cmd := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	if cmd != nil {
		t.Fatal("expected no command")
	}

	got := next.(Model)
	if got.width != 120 || got.height != 40 {
		t.Fatalf("expected 120x40, got %dx%d", got.width, got.height)
	}
}

func TestModelDoesNotQuitOnQ(t *testing.T) {
	m := NewModel()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd != nil {
		t.Fatal("q must remain ordinary input")
	}
}

func TestModelQuitsOnCtrlQ(t *testing.T) {
	m := NewModel()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlQ})
	if cmd == nil {
		t.Fatal("expected quit command")
	}
}

func TestModelHasInitialTabs(t *testing.T) {
	m := NewModel()
	want := []string{"Sessions", "Terminal", "Files", "Tools", "Settings", "AI"}
	if len(m.tabs) != len(want) {
		t.Fatalf("expected %d tabs, got %d", len(want), len(m.tabs))
	}
	for i, title := range want {
		if m.tabs[i].Title != title {
			t.Fatalf("expected tab %d to be %q, got %q", i, title, m.tabs[i].Title)
		}
	}
	if m.activeTab != 0 {
		t.Fatalf("expected initial tab 0, got %d", m.activeTab)
	}
}

func TestModelNavigatesTabs(t *testing.T) {
	m := NewModel()

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	if cmd != nil {
		t.Fatal("expected no command")
	}
	m = next.(Model)
	if m.activeTab != 1 {
		t.Fatalf("expected active tab 1, got %d", m.activeTab)
	}

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	m = next.(Model)
	if m.activeTab != 0 {
		t.Fatalf("expected active tab 0, got %d", m.activeTab)
	}

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	m = next.(Model)
	if m.activeTab != len(m.tabs)-1 {
		t.Fatalf("expected wrap to last tab, got %d", m.activeTab)
	}
}

func TestModelViewContainsTabs(t *testing.T) {
	m := NewModel()
	view := m.View()
	for _, want := range []string{"EIKSY", "Think. Connect. Operate.", "Sessions", "Terminal", "Files", "Tools", "F2 Sessions", "F10 exit"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected view to contain %q, got %q", want, view)
		}
	}
}

func TestTabSessionReference(t *testing.T) {
	tab := Tab{Title: "Chat"}

	tab.BindSession("session-1", "server-01")
	if tab.Session == nil {
		t.Fatal("expected session reference")
	}
	if tab.Session.ID != "session-1" || tab.Session.Title != "server-01" {
		t.Fatalf("unexpected session reference: %+v", tab.Session)
	}

	tab.UnbindSession()
	if tab.Session != nil {
		t.Fatal("expected session reference to be cleared")
	}
}

func TestTabBindSessionIgnoresEmptyID(t *testing.T) {
	tab := Tab{Title: "Chat"}
	tab.BindSession("  ", "server-01")
	if tab.Session != nil {
		t.Fatal("expected empty session ID not to create a reference")
	}
}

func TestModelChatInput(t *testing.T) {
	m := NewModel()
	for _, r := range []rune("hello") {
		next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		if cmd != nil {
			t.Fatal("expected no command")
		}
		m = next.(Model)
	}
	if m.input != "hello" {
		t.Fatalf("expected input %q, got %q", "hello", m.input)
	}

	// Printable h/l characters must be treated as chat input, not navigation.

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("expected no command")
	}
	m = next.(Model)
	if m.input != "" {
		t.Fatalf("expected input to clear, got %q", m.input)
	}
	if len(m.messages) != 1 || m.messages[0].Role != "You" || m.messages[0].Content != "hello" {
		t.Fatalf("unexpected messages: %+v", m.messages)
	}
	if !strings.Contains(m.View(), "You: hello") {
		t.Fatal("expected submitted message in chat view")
	}
}

type aiTestBackend struct {
	runtimeTestBackend
	sent []string
	aiState domainai.WorkspaceState
	savedCloud struct{ model, endpoint, token string }
	savedLocalURL string
	downloadedLocalURL string
	startedLocal bool
	stoppedLocal bool
	cloudModels []string
	cloudAuthSession appservice.CloudProviderAuthSession
	cloudAuthPolls int
}

func (b *aiTestBackend) SendChatMessage(message, activeSessionID string) error {
	b.sent = append(b.sent, message+"|"+activeSessionID)
	return nil
}

func (b *aiTestBackend) GetShellState() appservice.ShellState { return appservice.ShellState{AI: b.aiState} }
func (b *aiTestBackend) UpdateCommandPolicy(policy domainai.CommandPolicy) error { b.aiState.CommandPolicy = policy; return nil }
func (b *aiTestBackend) SaveCloudProvider(model, endpoint, token string) error {
	b.savedCloud = struct{ model, endpoint, token string }{model: model, endpoint: endpoint, token: token}
	for i := range b.aiState.Providers {
		if b.aiState.Providers[i].Class == domainai.ProviderClassOpenAICompatible {
			b.aiState.Providers[i].Model = model
			b.aiState.Providers[i].Endpoint = endpoint
			b.aiState.Providers[i].Selected = true
			b.aiState.Providers[i].Configured = true
			b.aiState.Providers[i].Status = "ready"
		}
	}
	return nil
}
func (b *aiTestBackend) SaveLocalProvider(downloadURL string) error { b.savedLocalURL = downloadURL; return nil }
func (b *aiTestBackend) DownloadLocalModel(downloadURL string) error { b.downloadedLocalURL = downloadURL; return nil }
func (b *aiTestBackend) StartLocalModel() error { b.startedLocal = true; return nil }
func (b *aiTestBackend) StopLocalModel() error { b.stoppedLocal = true; return nil }
func (b *aiTestBackend) ListCloudModels(endpoint, token string) ([]string, error) { b.cloudModels = []string{"gpt-test", "gpt-other"}; return b.cloudModels, nil }
func (b *aiTestBackend) StartCloudProviderAuth(endpoint string) (appservice.CloudProviderAuthSession, error) { b.cloudAuthSession = appservice.CloudProviderAuthSession{ID:"auth-1", Status:"pending", AuthURL:"https://example.test/auth", Message:"Waiting for browser authorization.", Endpoint:endpoint}; return b.cloudAuthSession, nil }
func (b *aiTestBackend) GetCloudProviderAuthSession(sessionID string) (appservice.CloudProviderAuthSession, error) { b.cloudAuthPolls++; b.cloudAuthSession.Status="completed"; b.cloudAuthSession.Message="Token received from browser authorization."; return b.cloudAuthSession, nil }

func (b *aiTestBackend) ResolveCommandPolicyRequest(requestID, mode string) error {
	b.aiState.CommandPolicy.PendingRequests = nil
	return nil
}

func (b *aiTestBackend) SelectAIProvider(providerID string) error {
	for i := range b.aiState.Providers {
		b.aiState.Providers[i].Selected = b.aiState.Providers[i].ID == providerID
	}
	return nil
}

func TestModelRunsCloudBrowserAuthThroughSharedBackend(t *testing.T) {
	backend := &aiTestBackend{}
	m := NewModel().WithBackend(backend)
	m.activeTab = 5
	m.aiProviderForm = &aiProviderConfigForm{
		providerIndex: 0,
		class:         domainai.ProviderClassOpenAICompatible,
		endpoint:      "https://sourcegraph.example.com/.api/llm/openai/v1",
	}
	opened := ""
	m.openBrowserURL = func(url string) error {
		opened = url
		return nil
	}

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	if cmd == nil {
		t.Fatal("expected browser auth command")
	}
	m = next.(Model)

	msg := cmd()
	next, cmd = m.Update(msg)
	if cmd == nil {
		t.Fatal("expected auth poll command")
	}
	m = next.(Model)

	if opened != "https://example.test/auth" {
		t.Fatalf("expected browser URL to open, got %q", opened)
	}
	if !m.aiCloudAuthPending {
		t.Fatal("expected auth polling to remain active")
	}

	next, cmd = m.Update(aiCloudAuthPoll{sessionID: "auth-1"})
	if cmd == nil {
		t.Fatal("expected auth completion command")
	}
	m = next.(Model)

	msg = cmd()
	next, cmd = m.Update(msg)
	m = next.(Model)

	if m.aiCloudAuthPending {
		t.Fatal("expected browser auth to complete")
	}
	if !strings.Contains(m.aiCloudAuthMessage, "Token received") {
		t.Fatalf("unexpected auth message: %q", m.aiCloudAuthMessage)
	}
	if backend.cloudAuthPolls != 1 {
		t.Fatalf("expected one auth poll, got %d", backend.cloudAuthPolls)
	}
}

func TestModelAISelectsProviderThroughSharedBackend(t *testing.T) {
	backend := &aiTestBackend{aiState: domainai.WorkspaceState{
		Providers: []domainai.ProviderDescriptor{
			{ID: "cloud", Name: "Cloud", Model: "gpt-test", Status: "ready", Selected: true, Configured: true},
			{ID: "local", Name: "Local", Model: "qwen-test", Status: "stopped", Configured: true},
		},
	}}
	m := NewModel().WithBackend(backend)
	m.activeTab = 5

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if cmd != nil {
		t.Fatal("expected provider navigation without command")
	}
	m = next.(Model)
	if m.aiProviderIndex != 1 {
		t.Fatalf("expected second provider selected, got %d", m.aiProviderIndex)
	}

	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected provider activation command")
	}
	m = next.(Model)
	if result := cmd(); result == nil {
		t.Fatal("expected provider activation result")
	}
	if !backend.aiState.Providers[1].Selected {
		t.Fatal("expected local provider to be selected")
	}
}

func TestModelAISendsMessageThroughSharedBackend(t *testing.T) {
	backend := &aiTestBackend{}
	m := NewModel().WithBackend(backend)
	m.activeTab = 5
	m.input = "inspect the host"

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected AI request command")
	}
	m = next.(Model)
	if m.input != "" {
		t.Fatal("expected input to clear")
	}
	if len(m.messages) != 1 || m.messages[0].Role != "You" {
		t.Fatalf("unexpected messages: %+v", m.messages)
	}
	if result := cmd(); result == nil {
		t.Fatal("expected AI completion message")
	}
	if len(backend.sent) != 1 || backend.sent[0] != "inspect the host|" {
		t.Fatalf("unexpected AI requests: %#v", backend.sent)
	}
}

func TestModelChatIgnoresEmptyInput(t *testing.T) {
	m := NewModel()
	m.input = "   "
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if len(m.messages) != 0 {
		t.Fatal("expected empty input not to create a message")
	}
}


func TestModelRendersAgentToolEvents(t *testing.T) {
	m := NewModel()

	next, cmd := m.Update(agentai.Event{Type: agentai.EventToolStarted, Tool: "ssh.exec"})
	if cmd != nil {
		t.Fatal("expected no command")
	}
	m = next.(Model)

	next, _ = m.Update(agentai.Event{Type: agentai.EventToolOutput, Content: "systemctl status nginx"})
	m = next.(Model)

	next, _ = m.Update(agentai.Event{Type: agentai.EventToolFinished, Tool: "ssh.exec"})
	m = next.(Model)

	view := m.View()
	for _, want := range []string{"[ssh.exec] finished", "systemctl status nginx"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected view to contain %q, got %q", want, view)
		}
	}
	if len(m.messages) != 0 {
		t.Fatal("tool events must not create chat messages")
	}
}

func TestModelRendersAgentErrorsAndCancellation(t *testing.T) {
	m := NewModel()

	next, _ := m.Update(agentai.Event{Type: agentai.EventError, Err: testError("provider unavailable")})
	m = next.(Model)
	next, _ = m.Update(agentai.Event{Type: agentai.EventCancellation})
	m = next.(Model)

	view := m.View()
	for _, want := range []string{"System: provider unavailable", "System: AI request cancelled"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected view to contain %q, got %q", want, view)
		}
	}
}

type testError string

func (e testError) Error() string { return string(e) }


type testApprovalResolver struct {
	requestID string
	mode      string
	err       error
}

func (r *testApprovalResolver) ResolveApproval(requestID, mode string) error {
	r.requestID = requestID
	r.mode = mode
	return r.err
}

func TestModelRendersAndResolvesApproval(t *testing.T) {
	resolver := &testApprovalResolver{}
	m := NewModel().WithApprovalResolver(resolver)

	next, cmd := m.Update(agentai.Event{
		Type: agentai.EventApprovalRequired,
		Tool: "ssh.exec",
		Approval: &agentai.ApprovalRequest{
			RequestID: "cmdreq-1",
			ToolID:    "shell",
			SessionID: "session-1",
			Command:   "systemctl restart nginx",
			Reason:    "change",
		},
	})
	if cmd != nil {
		t.Fatal("expected no command while rendering approval")
	}
	m = next.(Model)

	view := m.View()
	for _, want := range []string{
		"ACTION REQUEST",
		"systemctl restart nginx",
		"change",
		"[Y] now",
		"[S] session",
		"[A] always",
		"[N] deny",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected approval view to contain %q, got %q", want, view)
		}
	}

	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if cmd == nil {
		t.Fatal("expected approval command")
	}
	m = next.(Model)
	if m.approval != nil {
		t.Fatal("expected approval prompt to clear immediately")
	}
	result := cmd()
	if result == nil {
		t.Fatal("expected approval result message")
	}
	next, _ = m.Update(result)
	m = next.(Model)
	if resolver.requestID != "cmdreq-1" || resolver.mode != "now" {
		t.Fatalf("unexpected approval resolution: request=%q mode=%q", resolver.requestID, resolver.mode)
	}
	if !strings.Contains(m.View(), "Approval now applied.") {
		t.Fatal("expected approval result in chat")
	}
}

func TestModelDeniesApprovalWithoutResolver(t *testing.T) {
	m := NewModel()
	next, _ := m.Update(agentai.Event{
		Type: agentai.EventApprovalRequired,
		Approval: &agentai.ApprovalRequest{
			RequestID: "cmdreq-2",
			ToolID:    "shell",
			SessionID: "session-1",
			Command:   "uname -a",
		},
	})
	m = next.(Model)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if cmd != nil {
		t.Fatal("expected local fallback without resolver")
	}
	m = next.(Model)
	if m.approval != nil {
		t.Fatal("expected approval prompt to clear")
	}
	if !strings.Contains(m.View(), "Approval unavailable for cmdreq-2.") {
		t.Fatal("expected denial fallback message")
	}
}


func TestModelChatInputStillSubmitsWithSessionBrowser(t *testing.T) {
	m := NewModel().WithChatSessions([]SessionRef{{ID: "chat-1", Title: "API tests"}})
	m.input = "hello"
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("expected chat submission to remain local")
	}
	m = next.(Model)
	if len(m.messages) != 1 || m.messages[0].Content != "hello" {
		t.Fatalf("expected chat message, got %+v", m.messages)
	}
}

func TestModelSessionBrowserNavigation(t *testing.T) {
	m := NewModel().WithChatSessions([]SessionRef{
		{ID: "chat-1", Title: "API tests"},
		{ID: "chat-2", Title: "Production debug"},
		{ID: "chat-3", Title: "Deploy"},
	})

	if got := m.ActiveSession(); got == nil || got.ID != "chat-1" {
		t.Fatalf("expected first active session, got %+v", got)
	}
	if m.tabs[0].Session == nil || m.tabs[0].Session.ID != "chat-1" {
		t.Fatalf("expected chat tab to reference first session: %+v", m.tabs[0].Session)
	}

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = next.(Model)
	if got := m.ActiveSession(); got == nil || got.ID != "chat-2" {
		t.Fatalf("expected second active session, got %+v", got)
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = next.(Model)
	if got := m.ActiveSession(); got == nil || got.ID != "chat-1" {
		t.Fatalf("expected navigation back to first session, got %+v", got)
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = next.(Model)
	if got := m.ActiveSession(); got == nil || got.ID != "chat-3" {
		t.Fatalf("expected session navigation to wrap, got %+v", got)
	}
}

type runtimeTestBackend struct {
	profiles []domainsessions.Profile
	launched appservice.RuntimeSessionView
	connected []string
	disconnected []string
	closed []string
	sent []string
}

func (b *runtimeTestBackend) ListChatSessions() []domainai.ChatSession { return nil }
func (b *runtimeTestBackend) CreateChatSession(string) (domainai.ChatSession, error) { return domainai.ChatSession{}, nil }
func (b *runtimeTestBackend) SelectChatSession(string) error { return nil }
func (b *runtimeTestBackend) ForkChatSession(string, string) (domainai.ChatSession, error) { return domainai.ChatSession{}, nil }
func (b *runtimeTestBackend) GetSettings() domainsettings.AppSettings { return domainsettings.AppSettings{} }
func (b *runtimeTestBackend) UpdateSettings(domainsettings.AppSettings) error { return nil }
func (b *runtimeTestBackend) ListSessionProfiles() []domainsessions.Profile { return append([]domainsessions.Profile(nil), b.profiles...) }
func (b *runtimeTestBackend) CreateSessionProfileInput(domainsessions.ProfileInput) error { return nil }
func (b *runtimeTestBackend) DeleteSessionProfile(string) error { return nil }
func (b *runtimeTestBackend) LaunchSession(profileID string) (appservice.RuntimeSessionView, error) {
	b.launched = appservice.RuntimeSessionView{ID:"ssh-1",Title:"prod",ProtocolID:"ssh",ProfileID:profileID,Status:"connecting"}
	return b.launched, nil
}
func (b *runtimeTestBackend) ConnectSession(sessionID string) error { b.connected = append(b.connected, sessionID); return nil }
func (b *runtimeTestBackend) ReconnectSession(sessionID string) error { b.connected = append(b.connected, "reconnect:"+sessionID); return nil }
func (b *runtimeTestBackend) DisconnectSession(sessionID string) error { b.disconnected = append(b.disconnected, sessionID); return nil }
func (b *runtimeTestBackend) CloseSession(sessionID string) error { b.closed = append(b.closed, sessionID); return nil }
func (b *runtimeTestBackend) SendSSHInput(sessionID, data string) error { b.sent = append(b.sent, sessionID+":"+data); return nil }
func (b *runtimeTestBackend) AcceptSSHHostKey(string) error { return nil }
func (b *runtimeTestBackend) ListSFTPFiles(string, string) ([]sftpdomain.FileEntry, error) { return nil, nil }
func (b *runtimeTestBackend) NavigateSFTP(string, string) ([]sftpdomain.FileEntry, error) { return nil, nil }
func (b *runtimeTestBackend) ReadSFTPFile(string, string) (string, error) { return "", nil }

type sftpRuntimeTestBackend struct {
	runtimeTestBackend
	listed []string
	read []string
	saved []string
	uploadSelected []string
	uploaded []string
	downloadDir string
	downloaded []string
}

func (b *sftpRuntimeTestBackend) ListSFTPFiles(sessionID, targetPath string) ([]sftpdomain.FileEntry, error) {
	b.listed = append(b.listed, sessionID+":"+targetPath)
	return []sftpdomain.FileEntry{
		{Name:"config", Path:"/etc/config", IsDir:true},
		{Name:"readme.txt", Path:"/etc/readme.txt", IsDir:false},
	}, nil
}

func (b *sftpRuntimeTestBackend) NavigateSFTP(sessionID, targetPath string) ([]sftpdomain.FileEntry, error) {
	return b.ListSFTPFiles(sessionID, targetPath)
}

func (b *sftpRuntimeTestBackend) ReadSFTPFile(sessionID, filePath string) (string, error) {
	b.read = append(b.read, sessionID+":"+filePath)
	return "hello from remote", nil
}

func (b *sftpRuntimeTestBackend) SaveSFTPFile(sessionID, filePath, content string) error {
	b.saved = append(b.saved, sessionID+":"+filePath+":"+content)
	return nil
}

func (b *sftpRuntimeTestBackend) SelectUploadFiles() ([]string, error) {
	return append([]string(nil), b.uploadSelected...), nil
}

func (b *sftpRuntimeTestBackend) UploadSFTPFiles(sessionID, remoteDir string, localPaths []string) error {
	b.uploaded = append(b.uploaded, sessionID+":"+remoteDir+":"+strings.Join(localPaths, ","))
	return nil
}

func (b *sftpRuntimeTestBackend) SelectDownloadDirectory() (string, error) {
	return b.downloadDir, nil
}

func (b *sftpRuntimeTestBackend) DownloadSFTPFiles(sessionID, localDir string, remotePaths []string) error {
	b.downloaded = append(b.downloaded, sessionID+":"+localDir+":"+strings.Join(remotePaths, ","))
	return nil
}

func TestModelSFTPFilesBrowseAndRead(t *testing.T) {
	backend := &sftpRuntimeTestBackend{runtimeTestBackend: runtimeTestBackend{
		profiles: []domainsessions.Profile{{ID:"prod",Name:"prod",ProtocolID:"ssh",Host:"host",Port:22,Username:"ops"}},
	}}
	m := NewModel().WithBackend(backend).WithTerminalSession("ssh-1", "prod")
	m.activeTab = 2

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil { t.Fatal("expected SFTP listing command") }
	m = next.(Model)
	result := cmd()
	next, _ = m.Update(result)
	m = next.(Model)
	if len(backend.listed) != 1 || backend.listed[0] != "ssh-1:" { t.Fatalf("unexpected SFTP list calls: %v", backend.listed) }
	if len(m.sftpEntries) != 2 || m.sftpSelected != 0 { t.Fatalf("unexpected SFTP entries: %+v selected=%d", m.sftpEntries, m.sftpSelected) }

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = next.(Model)
	if m.sftpSelected != 1 { t.Fatalf("expected second file selected, got %d", m.sftpSelected) }

	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil { t.Fatal("expected SFTP read command") }
	m = next.(Model)
	result = cmd()
	next, _ = m.Update(result)
	m = next.(Model)
	if len(backend.read) != 1 || backend.read[0] != "ssh-1:/etc/readme.txt" { t.Fatalf("unexpected SFTP read calls: %v", backend.read) }
	if m.fileContent != "hello from remote" { t.Fatalf("unexpected file content: %q", m.fileContent) }
	if !strings.Contains(m.View(), "FILE: /etc/readme.txt") || !strings.Contains(m.View(), "hello from remote") { t.Fatal("expected remote file content in Files view") }
}

func TestModelSFTPEditAndSave(t *testing.T) {
	backend := &sftpRuntimeTestBackend{runtimeTestBackend: runtimeTestBackend{}}
	m := NewModel().WithBackend(backend).WithTerminalSession("ssh-1", "prod")
	m.activeTab = 2
	m.fileContent = "hello from remote"
	m.sftpEditPath = "/etc/readme.txt"

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlE})
	if cmd != nil { t.Fatal("expected local editor activation") }
	m = next.(Model)
	if !m.sftpEdit { t.Fatal("expected SFTP editor to open") }

	for i := 0; i < 5; i++ {
		next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
		m = next.(Model)
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	for _, r := range []rune("updated") {
		next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = next.(Model)
	}

	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if cmd == nil { t.Fatal("expected SFTP save command") }
	m = next.(Model)
	result := cmd()
	next, _ = m.Update(result)
	m = next.(Model)

	if len(backend.saved) != 1 || backend.saved[0] != "ssh-1:/etc/readme.txt:hello\nupdated from remote" {
		t.Fatalf("unexpected SFTP save calls: %v", backend.saved)
	}
	if m.sftpEdit { t.Fatal("expected editor to close after save") }
	if m.fileContent != "hello\nupdated from remote" { t.Fatalf("unexpected saved content: %q", m.fileContent) }
}

func TestModelSFTPUploadFiles(t *testing.T) {
	backend := &sftpRuntimeTestBackend{
		runtimeTestBackend: runtimeTestBackend{},
		uploadSelected: []string{"/tmp/one.txt", "/tmp/two.txt"},
	}
	m := NewModel().WithBackend(backend).WithTerminalSession("ssh-1", "prod")
	m.activeTab = 2
	m.sftpPath = "/etc/config"

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	if cmd == nil { t.Fatal("expected SFTP upload command") }
	m = next.(Model)
	result := cmd()
	next, _ = m.Update(result)
	m = next.(Model)

	if len(backend.uploaded) != 1 || backend.uploaded[0] != "ssh-1:/etc/config:/tmp/one.txt,/tmp/two.txt" {
		t.Fatalf("unexpected SFTP upload calls: %v", backend.uploaded)
	}
	if !strings.Contains(m.View(), "Uploaded 2 file(s) to /etc/config.") {
		t.Fatal("expected upload confirmation")
	}
}

func TestModelSFTPDownloadSelectedFile(t *testing.T) {
	backend := &sftpRuntimeTestBackend{
		runtimeTestBackend: runtimeTestBackend{},
		downloadDir: "/tmp/downloads",
	}
	m := NewModel().WithBackend(backend).WithTerminalSession("ssh-1", "prod")
	m.activeTab = 2
	m.sftpPath = "/etc/config"
	m.sftpEntries = []sftpdomain.FileEntry{
		{Name: "readme.txt", Path: "/etc/config/readme.txt"},
	}

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	if cmd == nil { t.Fatal("expected SFTP download command") }
	m = next.(Model)
	result := cmd()
	next, _ = m.Update(result)
	m = next.(Model)

	if len(backend.downloaded) != 1 || backend.downloaded[0] != "ssh-1:/tmp/downloads:/etc/config/readme.txt" {
		t.Fatalf("unexpected SFTP download calls: %v", backend.downloaded)
	}
	if !strings.Contains(m.View(), "Downloaded /etc/config/readme.txt.") {
		t.Fatal("expected download confirmation")
	}
}

func TestModelSFTPDirectoryNavigation(t *testing.T) {
	backend := &sftpRuntimeTestBackend{runtimeTestBackend: runtimeTestBackend{}}
	m := NewModel().WithBackend(backend).WithTerminalSession("ssh-1", "prod")
	m.activeTab = 2
	m.sftpEntries = []sftpdomain.FileEntry{{Name:"config",Path:"/etc/config",IsDir:true}}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil { t.Fatal("expected directory navigation command") }
	_ = next
	result := cmd()
	next, _ = m.Update(result)
	m = next.(Model)
	if m.sftpPath != "/etc/config" { t.Fatalf("expected directory path, got %q", m.sftpPath) }
	if len(backend.listed) != 1 || backend.listed[0] != "ssh-1:/etc/config" { t.Fatalf("unexpected directory list calls: %v", backend.listed) }
}

func TestModelSFTPParentDirectoryNavigation(t *testing.T) {
	backend := &sftpRuntimeTestBackend{runtimeTestBackend: runtimeTestBackend{}}
	m := NewModel().WithBackend(backend).WithTerminalSession("ssh-1", "prod")
	m.activeTab = 2
	m.sftpPath = "/etc/config"
	m.sftpEntries = []sftpdomain.FileEntry{{Name:"readme.txt", Path:"/etc/config/readme.txt"}}

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if cmd == nil { t.Fatal("expected parent directory navigation command") }
	m = next.(Model)
	result := cmd()
	next, _ = m.Update(result)
	m = next.(Model)

	if m.sftpPath != "/etc" { t.Fatalf("expected parent directory /etc, got %q", m.sftpPath) }
	if len(backend.listed) != 1 || backend.listed[0] != "ssh-1:/etc" { t.Fatalf("unexpected parent navigation calls: %v", backend.listed) }
}

func TestModelRuntimeSessionLifecycle(t *testing.T) {
	backend := &runtimeTestBackend{profiles: []domainsessions.Profile{{ID:"prod",Name:"prod",ProtocolID:"ssh",Host:"host",Port:22,Username:"ops"}}}
	m := NewModel().WithBackend(backend)

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil { t.Fatal("expected launch command") }
	m = next.(Model)
	result := cmd()
	if result == nil { t.Fatal("expected launch result") }
	next, cmd = m.Update(result)
	m = next.(Model)
	if cmd == nil { t.Fatal("expected connect command") }
	result = cmd()
	next, _ = m.Update(result)
	m = next.(Model)

	if len(backend.connected) != 1 || backend.connected[0] != "ssh-1" { t.Fatalf("expected ssh connection, got %v", backend.connected) }
	if m.tabs[1].Session == nil || m.tabs[1].Session.ID != "ssh-1" { t.Fatalf("expected terminal binding, got %+v", m.tabs[1].Session) }

	m.terminalInput = "ls"
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil { t.Fatal("expected terminal send command") }
	_ = cmd()
	m = next.(Model)
	if len(backend.sent) != 1 || backend.sent[0] != "ssh-1:ls\n" { t.Fatalf("unexpected terminal input: %v", backend.sent) }

	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	if cmd == nil { t.Fatal("expected disconnect command") }
	m = next.(Model)
	_ = cmd()
	if len(backend.disconnected) != 1 || backend.disconnected[0] != "ssh-1" { t.Fatalf("unexpected disconnects: %v", backend.disconnected) }

	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyCtrlW})
	if cmd == nil { t.Fatal("expected close command") }
	m = next.(Model)
	_ = cmd()
	if len(backend.closed) != 1 || backend.closed[0] != "ssh-1" { t.Fatalf("unexpected closes: %v", backend.closed) }
}

type testSessionSelector struct {
	sessionID string
	err       error
}

func (s *testSessionSelector) SelectChatSession(sessionID string) error {
	s.sessionID = sessionID
	return s.err
}

func TestModelSessionBrowserSelectsThroughApplicationService(t *testing.T) {
	selector := &testSessionSelector{}
	m := NewModel().
		WithChatSessions([]SessionRef{{ID: "chat-1", Title: "API tests"}}).
		WithSessionSelector(selector)

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected session selection command")
	}
	m = next.(Model)
	if selector.sessionID != "" {
		t.Fatal("selection should be deferred to the command")
	}

	result := cmd()
	if result == nil {
		t.Fatal("expected session selection result")
	}
	next, _ = m.Update(result)
	m = next.(Model)
	if selector.sessionID != "chat-1" {
		t.Fatalf("expected application service to receive chat-1, got %q", selector.sessionID)
	}
	if !strings.Contains(m.View(), "Session chat-1 selected.") {
		t.Fatal("expected selection confirmation")
	}
}

func TestModelSessionBrowserView(t *testing.T) {
	m := NewModel().WithChatSessions([]SessionRef{
		{ID: "chat-1", Title: "API tests"},
		{ID: "chat-2", Title: "Production debug"},
	})
	view := m.View()
	for _, want := range []string{"Sessions", "Session 1/2", "ACTIVE SESSION", "API tests"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected view to contain %q, got %q", want, view)
		}
	}
}

type testSessionForker struct {
	sessionID string
	title     string
	result    SessionRef
	err       error
}

func (f *testSessionForker) ForkChatSession(sessionID, title string) (domainai.ChatSession, error) {
	f.sessionID = sessionID
	f.title = title
	return domainai.ChatSession{ID: f.result.ID, Title: f.result.Title}, f.err
}

func TestModelForksActiveSessionThroughApplicationService(t *testing.T) {
	forker := &testSessionForker{result: SessionRef{ID: "chat-2", Title: "API tests (fork)"}}
	m := NewModel().
		WithChatSessions([]SessionRef{{ID: "chat-1", Title: "API tests"}}).
		WithSessionForker(forker)

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlF})
	if cmd == nil { t.Fatal("expected fork command") }
	m = next.(Model)
	result := cmd()
	if result == nil { t.Fatal("expected fork result") }
	next, _ = m.Update(result)
	m = next.(Model)

	if forker.sessionID != "chat-1" || forker.title != "API tests (fork)" {
		t.Fatalf("unexpected fork request: id=%q title=%q", forker.sessionID, forker.title)
	}
	if len(m.sessions) != 2 || m.activeSession != 1 {
		t.Fatalf("expected fork to become active session: %+v active=%d", m.sessions, m.activeSession)
	}
	if m.tabs[0].Session == nil || m.tabs[0].Session.ID != "chat-2" {
		t.Fatalf("expected chat tab to follow fork: %+v", m.tabs[0].Session)
	}
	if !strings.Contains(m.View(), "Session API tests (fork) forked.") {
		t.Fatal("expected fork confirmation")
	}
}

func TestModelForkUnavailable(t *testing.T) {
	m := NewModel().WithChatSessions([]SessionRef{{ID: "chat-1", Title: "API tests"}})
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlF})
	if cmd != nil { t.Fatal("expected local fallback") }
	m = next.(Model)
	if !strings.Contains(m.View(), "Session forking unavailable.") {
		t.Fatal("expected unavailable message")
	}
}


func TestModelCommandPaletteOpensFiltersAndCloses(t *testing.T) {
	m := NewModel()
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	if cmd != nil { t.Fatal("expected no command") }
	m = next.(Model)
	if m.palette == nil { t.Fatal("expected command palette to open") }
	if !strings.Contains(m.View(), "COMMAND PALETTE") || !strings.Contains(m.View(), "Fork active session") {
		t.Fatal("expected command palette contents")
	}

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	m = next.(Model)
	if m.palette.Query != "f" || !strings.Contains(m.View(), "Fork active session") {
		t.Fatalf("expected palette query filtering, got %+v", m.palette)
	}

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)
	if m.palette != nil { t.Fatal("expected palette to close") }
}

func TestModelCommandPaletteRunsNavigationCommand(t *testing.T) {
	m := NewModel()
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	m = next.(Model)

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if cmd != nil { t.Fatal("expected no command while filtering") }
	m = next.(Model)
	if !strings.Contains(m.View(), "Next tab") {
		t.Fatal("expected next tab command in filtered palette")
	}

	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil { t.Fatal("expected navigation command to execute locally") }
	m = next.(Model)
	if m.palette != nil || m.activeTab != 1 {
		t.Fatalf("expected palette closed and next tab selected: palette=%+v tab=%d", m.palette, m.activeTab)
	}
}

func TestModelCommandPaletteQuit(t *testing.T) {
	m := NewModel()
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	m = next.(Model)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	m = next.(Model)
	if cmd != nil { t.Fatal("expected no command while filtering") }
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil { t.Fatal("expected quit command from palette") }
	m = next.(Model)
	if m.palette != nil { t.Fatal("expected palette to close") }
}


func TestModelNavigationShortcuts(t *testing.T) {
	m := NewModel()
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	m = next.(Model)
	if m.activeTab != 5 {
		t.Fatalf("expected shift+tab to select previous tab, got %d", m.activeTab)
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = next.(Model)
	if m.activeTab != 0 {
		t.Fatalf("expected tab to select next tab, got %d", m.activeTab)
	}
}

func TestModelQuitShortcutDoesNotConsumeChatInput(t *testing.T) {
	m := NewModel()
	for _, r := range []rune{'h', 'q', 'u', 'i', 't'} {
		next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		if cmd != nil {
			t.Fatalf("expected chat input, got quit command for %q", r)
		}
		m = next.(Model)
	}
	if m.input != "hquit" {
		t.Fatalf("expected q to remain chat input when input is non-empty, got %q", m.input)
	}
}

func TestModelShortcutHelpOpensAndCloses(t *testing.T) {
	m := NewModel()
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	if cmd != nil {
		t.Fatal("expected no command")
	}
	m = next.(Model)
	if !m.shortcuts {
		t.Fatal("expected shortcut help to open")
	}
	view := m.View()
	for _, want := range []string{"KEYBOARD", "F2", "F3", "F4", "F5", "F6", "F7", "F10", "Ctrl+Q"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected shortcut help to contain %q, got %q", want, view)
		}
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)
	if m.shortcuts {
		t.Fatal("expected shortcut help to close")
	}
}

func TestModelTerminalViewAcceptsInput(t *testing.T) {
	m := NewModel().WithTerminalSession("runtime-1", "Production")

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = next.(Model)
	if m.activeTab != 1 {
		t.Fatalf("expected terminal tab, got %d", m.activeTab)
	}

	for _, r := range []rune("qecho status") {
		next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		if cmd != nil {
			t.Fatalf("expected terminal input, got command for %q", r)
		}
		m = next.(Model)
	}

	if m.terminalInput != "qecho status" {
		t.Fatalf("expected terminal input to be preserved, got %q", m.terminalInput)
	}

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("expected local terminal submission")
	}
	m = next.(Model)

	if m.terminalInput != "" {
		t.Fatalf("expected terminal input to clear, got %q", m.terminalInput)
	}
	if len(m.terminalLines) != 1 || m.terminalLines[0] != "$ qecho status" {
		t.Fatalf("unexpected terminal lines: %+v", m.terminalLines)
	}
	if !strings.Contains(m.View(), "Terminal") || !strings.Contains(m.View(), "$ qecho status") {
		t.Fatalf("expected terminal content in view, got %q", m.View())
	}
}

func TestModelTerminalViewShowsActiveSession(t *testing.T) {
	m := NewModel().WithTerminalSession("runtime-1", "Production")
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = next.(Model)

	view := m.View()
	for _, want := range []string{"Terminal", "Session: Production", "Connected. Ready for input."} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected terminal view to contain %q, got %q", want, view)
		}
	}
}


func TestModelFilesViewShowsEmptyStateAndSession(t *testing.T) {
	m := NewModel().WithChatSessions([]SessionRef{{ID: "chat-1", Title: "Production"}})
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = next.(Model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = next.(Model)

	view := m.View()
	for _, want := range []string{"Files", "Session 1/1", "Path: .", "No active SSH session.", "Connect a session from Sessions first."} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected files view to contain %q, got %q", want, view)
		}
	}
}

func TestModelFilesViewRequiresRuntimeSession(t *testing.T) {
	m := NewModel()
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = next.(Model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = next.(Model)

	view := m.View()
	for _, want := range []string{"Path: .", "No active SSH session.", "Connect a session from Sessions first."} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected files view to contain %q, got %q", want, view)
		}
	}
}

func TestModelViewShowsContextStatus(t *testing.T) {
	m := NewModel().WithChatSessions([]SessionRef{{ID: "chat-1", Title: "Production"}, {ID: "chat-2", Title: "Deploy"}})
	view := m.View()
	for _, want := range []string{"Sessions", "Session 1/2"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected view to contain %q, got %q", want, view)
		}
	}
}


func TestModelTerminalDoesNotAcceptInputWithoutRuntimeSession(t *testing.T) {
	m := NewModel()
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = next.(Model)

	for _, r := range []rune("echo status") {
		next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		if cmd != nil {
			t.Fatal("expected no command")
		}
		m = next.(Model)
	}

	if m.terminalInput != "" {
		t.Fatalf("terminal must remain read-only without a runtime session, got %q", m.terminalInput)
	}
	if !strings.Contains(m.View(), "No active terminal session.") {
		t.Fatal("expected terminal empty state")
	}
}

func TestModelTerminalBindsRuntimeSessionSeparatelyFromChatSessions(t *testing.T) {
	m := NewModel().
		WithChatSessions([]SessionRef{{ID: "chat-1", Title: "Conversation"}}).
		WithTerminalSession("runtime-1", "Production")

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = next.(Model)

	if !m.hasTerminalSession() || m.tabs[1].Session.ID != "runtime-1" {
		t.Fatalf("expected runtime terminal session, got %+v", m.tabs[1].Session)
	}
	if m.tabs[0].Session == nil || m.tabs[0].Session.ID != "chat-1" {
		t.Fatalf("expected chat session to remain separate, got %+v", m.tabs[0].Session)
	}
}


func TestModelFunctionKeyNavigation(t *testing.T) {
	m := NewModel()
	cases := []struct {
		key  tea.KeyType
		want int
		view string
	}{
		{tea.KeyF2, 0, "Sessions"},
		{tea.KeyF3, 1, "Terminal"},
		{tea.KeyF4, 2, "Files"},
		{tea.KeyF5, 3, "Tools"},
		{tea.KeyF6, 4, "Settings"},
		{tea.KeyF7, 5, "AI"},
	}
	for _, tc := range cases {
		next, cmd := m.Update(tea.KeyMsg{Type: tc.key})
		if cmd != nil {
			t.Fatalf("%s: expected no command", tc.view)
		}
		m = next.(Model)
		if m.activeTab != tc.want {
			t.Fatalf("%s: expected tab %d, got %d", tc.view, tc.want, m.activeTab)
		}
		if !strings.Contains(m.View(), tc.view) {
			t.Fatalf("%s: expected view %q", tc.view, m.View())
		}
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyF7})
	m = next.(Model)
	if m.activeTab != 5 || !strings.Contains(m.View(), "AI") {
		t.Fatalf("expected AI mode, got view=%q", m.View())
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyF10})
	if cmd == nil {
		t.Fatal("expected F10 to quit")
	}
}

type clearChatTestBackend struct {
	runtimeTestBackend
	cleared bool
}

func (b *clearChatTestBackend) ClearChat() error {
	b.cleared = true
	return nil
}

func TestModelClearsAIChatThroughSharedBackend(t *testing.T) {
	backend := &clearChatTestBackend{}
	m := NewModel().WithBackend(backend)
	m.activeTab = 5
	m.messages = []ChatMessage{{Role: "user", Content: "old message"}}
	m.toolCalls = []ToolCallView{{Name: "shell", Status: "done"}}

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlK})
	if cmd == nil { t.Fatal("expected clear chat command") }
	m = next.(Model)
	result := cmd()
	if result == nil { t.Fatal("expected clear chat result") }
	next, _ = m.Update(result)
	m = next.(Model)
	if !backend.cleared { t.Fatal("expected shared ClearChat to be called") }
	if len(m.messages) != 0 || len(m.toolCalls) != 0 { t.Fatalf("expected TUI conversation state to clear: messages=%+v tools=%+v", m.messages, m.toolCalls) }
	if !strings.Contains(m.View(), "No AI messages yet.") { t.Fatal("expected empty AI conversation view") }
}

type sshConfigImportTestBackend struct{runtimeTestBackend;raw string}
func(b *sshConfigImportTestBackend)ImportSSHConfig(raw string)([]domainsessions.Profile,error){b.raw=raw;return []domainsessions.Profile{{ID:"prod-1",Name:"prod-1",ProtocolID:"ssh",Host:"db.internal",Port:22}},nil}
func TestModelImportsSSHConfigThroughSharedBackend(t *testing.T){backend:=&sshConfigImportTestBackend{};m:=NewModel().WithBackend(backend);m.activeTab=4;next,_:=m.Update(tea.KeyMsg{Type:tea.KeyF8});m=next.(Model);if !m.sshConfigForm{t.Fatal("expected SSH config form")};m.sshConfigImport="Host prod-1\n  HostName db.internal\n  User ops\n";next,cmd:=m.Update(tea.KeyMsg{Type:tea.KeyCtrlS});if cmd==nil{t.Fatal("expected import command")};m=next.(Model);result:=cmd();next,_=m.Update(result);m=next.(Model);if !strings.Contains(backend.raw,"Host prod-1"){t.Fatalf("unexpected imported config: %q",backend.raw)};if !strings.Contains(m.View(),"Imported 1 SSH profile(s). "){_ = m}}

func TestModelCommandPolicyTogglesToolThroughSharedBackend(t *testing.T) {
	backend := &aiTestBackend{aiState: domainai.WorkspaceState{CommandPolicy: domainai.CommandPolicy{Tools: []domainai.CommandTool{
		{ID: "shell", Name: "Shell", Enabled: true}, {ID: "sftp", Name: "SFTP", Enabled: false},
	}}}}
	m := NewModel().WithBackend(backend); m.activeTab = 3
	view := m.View()
	for _, want := range []string{"AI command policy", "[ON] Shell", "[OFF] SFTP", "Command rules", "Pending approvals"} {
		if !strings.Contains(view, want) { t.Fatalf("expected tools view to contain %q, got %q", want, view) }
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyDown}); if cmd != nil { t.Fatal("expected tool navigation without command") }; m = next.(Model)
	if m.aiToolIndex != 1 { t.Fatalf("expected second tool selected, got %d", m.aiToolIndex) }
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter}); if cmd == nil { t.Fatal("expected policy update command") }; m = next.(Model)
	result := cmd(); if result == nil { t.Fatal("expected policy update result") }; next, _ = m.Update(result); m = next.(Model)
	if !backend.aiState.CommandPolicy.Tools[1].Enabled { t.Fatal("expected SFTP tool to be enabled") }
	if !strings.Contains(m.View(), "Command policy updated.") { t.Fatal("expected update confirmation") }
}


type editProfileTestBackend struct {
	runtimeTestBackend
	updated domainsessions.ProfileInput
}

func (b *editProfileTestBackend) CreateSessionProfileInput(input domainsessions.ProfileInput) error {
	b.updated = input
	return nil
}

type secureStorageTestBackend struct {
	runtimeTestBackend
	status securestorage.Status
	password string
	locked bool
}

func (b *secureStorageTestBackend) GetSecureStorageStatus() securestorage.Status { return b.status }
func (b *secureStorageTestBackend) EnsureMasterPassword(password string) error { b.password=password; b.status.Configured=true; b.status.Unlocked=true; return nil }
func (b *secureStorageTestBackend) LockSecureStorage() { b.locked=true; b.status.Unlocked=false }

func TestModelSecureStorageLifecycleThroughSharedBackend(t *testing.T) {
	backend := &secureStorageTestBackend{runtimeTestBackend: runtimeTestBackend{}, status: securestorage.Status{Available:true, Configured:false, Unlocked:false}}
	m := NewModel().WithBackend(backend)
	m.activeTab = 4
	if !strings.Contains(m.View(), "not configured") { t.Fatalf("expected secure storage status in settings: %s", m.View()) }
	m.settingsIndex = 3
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s','e','c','r','e','t'}})
	m = updated.(Model)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if cmd == nil { t.Fatal("expected secure storage command") }
	msg := cmd()
	next, _ = m.Update(msg)
	m = next.(Model)
	if backend.password != "secret" || !backend.status.Unlocked { t.Fatalf("expected shared secure storage unlock: %+v", backend.status) }
	if !strings.Contains(m.View(), "unlocked") { t.Fatalf("expected unlocked status: %s", m.View()) }
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlL})
	m = next.(Model)
	if !backend.locked || backend.status.Unlocked { t.Fatalf("expected shared secure storage lock: %+v", backend.status) }
	if !strings.Contains(m.View(), "locked") { t.Fatalf("expected locked status: %s", m.View()) }
}

func TestModelEditsSessionProfileThroughSharedBackend(t *testing.T) {
	backend := &editProfileTestBackend{runtimeTestBackend: runtimeTestBackend{
		profiles: []domainsessions.Profile{{
			ID: "prod-1", Name: "Production", ProtocolID: "ssh",
			Host: "old.example", Port: 22, Username: "ops",
		}},
	}}
	m := NewModel().WithBackend(backend)
	m.activeTab = 0

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlE})
	if cmd != nil {
		t.Fatal("expected local profile editor activation")
	}
	m = next.(Model)
	if m.profileForm == nil || m.profileForm.id != "prod-1" {
		t.Fatalf("expected editor for prod-1, got %+v", m.profileForm)
	}
	if !strings.Contains(m.View(), "EDIT SSH SESSION") {
		t.Fatal("expected edit session form")
	}

	m.profileForm.name = "Production EU"
	m.profileForm.host = "new.example"
	m.profileForm.username = "admin"

	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected profile update command")
	}
	m = next.(Model)
	result := cmd()
	if result == nil {
		t.Fatal("expected profile update result")
	}
	next, _ = m.Update(result)
	m = next.(Model)

	if backend.updated.ID != "prod-1" || backend.updated.Name != "Production EU" ||
		backend.updated.Host != "new.example" || backend.updated.Username != "admin" {
		t.Fatalf("unexpected profile update: %+v", backend.updated)
	}
	if !strings.Contains(m.View(), "Session profile created.") {
		t.Fatal("expected profile update confirmation")
	}
}

func TestModelAddsPortForwardRuleThroughSharedSettings(t *testing.T) {
	backend := &runtimeTestBackend{}
	m := NewModel().WithBackend(backend)
	m.activeTab = 4
	m.settingsIndex = m.settingsPortForwardIndex()
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter}); if cmd != nil { t.Fatal("expected local form") }; m = next.(Model)
	if m.portForwardForm == nil { t.Fatal("expected port forwarding form") }
	m.portForwardForm.localPort="8080"; m.portForwardForm.remoteHost="db.internal"; m.portForwardForm.remotePort="5432"; m.portForwardForm.hostID="prod-1"
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter}); if cmd == nil { t.Fatal("expected settings update command") }; m = next.(Model)
	result := cmd(); next, _ = m.Update(result); m = next.(Model)
	if len(m.settings.PortForwardRules) != 1 { t.Fatalf("expected one rule, got %+v", m.settings.PortForwardRules) }
	rule:=m.settings.PortForwardRules[0]
	if rule.LocalPort!="8080" || rule.RemoteHost!="db.internal" || rule.RemotePort!="5432" || rule.HostID!="prod-1" || !rule.Enabled { t.Fatalf("unexpected rule: %+v",rule) }
}

func TestModelConfiguresCloudAIProviderThroughSharedBackend(t *testing.T) {
	backend := &aiTestBackend{aiState: domainai.WorkspaceState{Providers: []domainai.ProviderDescriptor{
		{ID: "cloud", Name: "Cloud", Class: domainai.ProviderClassOpenAICompatible, Model: "old", Endpoint: "https://old.example/v1", Selected: true, Configured: true},
	}}}
	m := NewModel().WithBackend(backend)
	m.activeTab = 5

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlE})
	if cmd != nil { t.Fatal("expected local configuration form") }
	m = next.(Model)
	if m.aiProviderForm == nil || m.aiProviderForm.class != domainai.ProviderClassOpenAICompatible { t.Fatal("expected cloud provider form") }
	m.aiProviderForm.model = "gpt-new"
	m.aiProviderForm.endpoint = "https://api.example/v1"
	m.aiProviderForm.token = "secret"

	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil { t.Fatal("expected cloud save command") }
	m = next.(Model)
	if result := cmd(); result == nil { t.Fatal("expected cloud save result") } else { next, _ = m.Update(result); m = next.(Model) }
	if backend.savedCloud.model != "gpt-new" || backend.savedCloud.endpoint != "https://api.example/v1" || backend.savedCloud.token != "secret" {
		t.Fatalf("unexpected cloud provider save: %+v", backend.savedCloud)
	}
}

func TestModelListsCloudModelsThroughSharedBackend(t *testing.T) {
	backend := &aiTestBackend{aiState: domainai.WorkspaceState{Providers: []domainai.ProviderDescriptor{
		{ID: "cloud", Name: "Cloud", Class: domainai.ProviderClassOpenAICompatible, Endpoint: "https://api.example/v1", Selected: true},
	}}}
	m := NewModel().WithBackend(backend)
	m.activeTab = 5
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlE})
	m = next.(Model)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlL})
	if cmd == nil { t.Fatal("expected cloud model list command") }
	m = next.(Model)
	result := cmd()
	next, _ = m.Update(result)
	m = next.(Model)
	if len(m.aiCloudModels) != 2 || m.aiCloudModels[0] != "gpt-test" { t.Fatalf("unexpected cloud models: %+v", m.aiCloudModels) }
}

func TestModelRunsLocalAIProviderLifecycleThroughSharedBackend(t *testing.T) {
	backend := &aiTestBackend{aiState: domainai.WorkspaceState{Providers: []domainai.ProviderDescriptor{
		{ID: "local", Name: "Local", Class: domainai.ProviderClassLocalOpenAI, DownloadURL: "https://models.example/qwen.gguf", Status: "stopped", Selected: true},
	}}}
	m := NewModel().WithBackend(backend)
	m.activeTab = 5
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlE})
	m = next.(Model)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	if cmd == nil { t.Fatal("expected local download command") }
	m = next.(Model)
	result := cmd()
	next, _ = m.Update(result)
	m = next.(Model)
	if backend.downloadedLocalURL != "https://models.example/qwen.gguf" { t.Fatalf("unexpected download URL: %q", backend.downloadedLocalURL) }

	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	if cmd == nil { t.Fatal("expected local start command") }
	m = next.(Model)
	result = cmd()
	next, _ = m.Update(result)
	m = next.(Model)
	if !backend.startedLocal { t.Fatal("expected local model start") }
}
 

func TestModelResolvesPendingCommandRequestThroughSharedBackend(t *testing.T) {
	backend := &aiTestBackend{aiState: domainai.WorkspaceState{CommandPolicy: domainai.CommandPolicy{
		PendingRequests: []domainai.CommandRequest{{ID:"request-1", ToolID:"shell", Command:"uname -a", SessionID:"session-1"}},
	}}}
	m := NewModel().WithBackend(backend); m.activeTab=3
	next, cmd := m.Update(tea.KeyMsg{Type:tea.KeyDown}); if cmd != nil { t.Fatal("expected approval selection without command") }; m=next.(Model)
	if m.aiApprovalIndex != 1 { t.Fatalf("expected session action, got %d",m.aiApprovalIndex) }
	next, cmd = m.Update(tea.KeyMsg{Type:tea.KeyEnter}); if cmd == nil { t.Fatal("expected approval command") }; m=next.(Model)
	result:=cmd(); if result==nil { t.Fatal("expected approval result") }; next,_=m.Update(result); m=next.(Model)
	if len(backend.aiState.CommandPolicy.PendingRequests)!=0 { t.Fatal("expected pending request to be cleared") }
	if !strings.Contains(m.View(),"Command request resolved: session.") { t.Fatal("expected resolution confirmation") }
}

func TestChatMessagesForEventUsesSeparateAIAndAgentHistories(t *testing.T) {
	state := appservice.ShellState{
		AI: domainai.WorkspaceState{
			Messages: []domainai.ChatMessage{{Role: "assistant", Content: "plain chat"}},
			AgentMessages: []domainai.ChatMessage{
				{Role: "user", Content: "inspect host"},
				{Role: "assistant", Content: "agent result"},
			},
		},
	}

	plain, ok := chatMessagesForEvent(state, "ai:message")
	if !ok || len(plain) != 1 || plain[0].Content != "plain chat" {
		t.Fatalf("unexpected plain chat refresh: ok=%v messages=%#v", ok, plain)
	}

	agent, ok := chatMessagesForEvent(state, "agent:message")
	if !ok || len(agent) != 2 || agent[0].Content != "inspect host" || agent[1].Content != "agent result" {
		t.Fatalf("unexpected Agent refresh: ok=%v messages=%#v", ok, agent)
	}

	if messages, ok := chatMessagesForEvent(state, "agent:operate"); ok || messages != nil {
		t.Fatalf("operation events must not replace chat history: ok=%v messages=%#v", ok, messages)
	}
}
