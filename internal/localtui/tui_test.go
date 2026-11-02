package localtui

import (
	"context"
	"errors"
	"strings"
	"testing"

	agentai "eiksy/internal/ai"
	appservice "eiksy/internal/app"
	domainai "eiksy/internal/domain/ai"
	"eiksy/internal/securestorage"

	tea "github.com/charmbracelet/bubbletea"
)

type fakeBackend struct {
	status securestorage.Status
	state appservice.ShellState
	resolveErr error
	resolveCalls int
	lastMode domainai.CommandPermissionMode
}

func (b *fakeBackend) GetSecureStorageStatus() securestorage.Status { return b.status }
func (b *fakeBackend) EnsureMasterPassword(password string) error {
	if password == "" { return errors.New("master password is required") }
	b.status.Configured, b.status.Unlocked = true, true
	return nil
}
func (b *fakeBackend) GetLocalAgentState() appservice.LocalAgentState { return appservice.LocalAgentState{AI:b.state.AI} }
func (b *fakeBackend) SelectAIProvider(id string) error {
	for i := range b.state.AI.Providers { b.state.AI.Providers[i].Selected = b.state.AI.Providers[i].ID == id }
	return nil
}
func (b *fakeBackend) SaveCloudProvider(model, endpoint, token string) error { return nil }
func (b *fakeBackend) SaveLocalProvider(downloadURL string) error { return nil }
func (b *fakeBackend) DownloadLocalModel(downloadURL string) error { return nil }
func (b *fakeBackend) StartLocalModel() error { return nil }
func (b *fakeBackend) StopLocalModel() error { return nil }
func (b *fakeBackend) ListCloudModels(endpoint, token string) ([]string, error) { return []string{"model-a"}, nil }
func (b *fakeBackend) ResolveCommandPolicyRequest(requestID string, mode domainai.CommandPermissionMode) error {
	b.resolveCalls++
	b.lastMode = mode
	if b.resolveErr != nil {
		err := b.resolveErr
		b.resolveErr = nil
		b.status.Unlocked = false
		return err
	}
	return nil
}

type fakeAgent struct { calls int; firstErr error }

func (a *fakeAgent) Run(ctx context.Context, sessionID, input string) (<-chan agentai.Event, error) {
	a.calls++
	events := make(chan agentai.Event, 1)
	if a.calls == 1 && a.firstErr != nil { events <- agentai.Event{Type:agentai.EventError, Err:a.firstErr} } else { events <- agentai.Event{Type: agentai.EventMessageFinished} }
	close(events)
	return events, nil
}

func TestNewModelPromptsToUnlockSecureStorage(t *testing.T) {
	backend := &fakeBackend{status: securestorage.Status{Available:true, Configured:true, Unlocked:false}}
	model := NewModelWithBackend(backend, &fakeAgent{})
	if model.screen != screenUnlockPassword { t.Fatalf("expected unlock screen, got %v", model.screen) }
}

func TestMasterPasswordSetupRequiresConfirmation(t *testing.T) {
	backend := &fakeBackend{status: securestorage.Status{Available:true, Configured:false, Unlocked:false}}
	model := NewModelWithBackend(backend, &fakeAgent{})
	if model.screen != screenSetupPassword { t.Fatalf("expected setup screen, got %v", model.screen) }

	model.password = "test-master-password"
	_, _ = model.updatePasswordKey(tea.KeyMsg{Type:tea.KeyEnter})
	if model.screen != screenConfirmPassword { t.Fatalf("expected confirmation screen, got %v", model.screen) }
	model.confirm = "test-master-password"
	_, cmd := model.updatePasswordKey(tea.KeyMsg{Type:tea.KeyEnter})
	if cmd == nil { t.Fatal("expected master password setup command") }
	message := cmd()
	updated, _ := model.Update(message)
	model = updated.(Model)
	if model.screen != screenChat { t.Fatalf("expected chat screen after setup, got %v", model.screen) }
	if !backend.status.Unlocked || !backend.status.Configured { t.Fatal("expected secure storage to be configured and unlocked") }
}

func TestSendRequiresConfiguredProvider(t *testing.T) {
	backend := &fakeBackend{status:securestorage.Status{Available:true,Configured:true,Unlocked:true}}
	backend.state.AI.Providers = []domainai.ProviderDescriptor{{ID:"cloud",Name:"Cloud",Selected:true,Configured:false}}
	agent := &fakeAgent{}
	model := NewModelWithBackend(backend, agent)
	model.input = "hello"
	_, _ = model.updateChatKey(tea.KeyMsg{Type:tea.KeyEnter})
	if agent.calls != 0 { t.Fatalf("agent should not run without a configured provider; calls=%d", agent.calls) }
	if !strings.Contains(model.status, "/provider") { t.Fatalf("expected provider setup guidance, got %q", model.status) }
}

func TestProviderSelectionOpensCloudConfiguration(t *testing.T) {
	backend := &fakeBackend{status:securestorage.Status{Available:true,Configured:true,Unlocked:true}}
	provider := domainai.ProviderDescriptor{ID:"cloud",Name:"Cloud",Class:domainai.ProviderClassOpenAICompatible,Model:"model-x",Endpoint:"https://api.example.test/v1"}
	backend.state.AI.Providers = []domainai.ProviderDescriptor{provider}
	model := NewModelWithBackend(backend, &fakeAgent{})
	model.screen = screenProviderMenu
	cmd := model.selectedProvider(provider)
	if cmd == nil { t.Fatal("expected provider selection command") }
	message := cmd()
	updated, _ := model.Update(message)
	model = updated.(Model)
	if model.screen != screenCloudConfig { t.Fatalf("expected cloud config screen, got %v", model.screen) }
	if model.cloudModel != "model-x" || model.cloudEndpoint != "https://api.example.test/v1" { t.Fatalf("provider configuration was not populated: %#v", model) }
}

func TestApprovalEventIsRenderedInChat(t *testing.T) {
	model := Model{screen:screenChat}
	model.handleAgentEvent(agentai.Event{Type:agentai.EventApprovalRequired,Approval:&agentai.ApprovalRequest{RequestID:"request-1",ToolID:"local-files",Command:"local.file.write /tmp/example"}})
	if model.approval == nil { t.Fatal("expected approval request") }
	if !strings.Contains(model.View(), "APPROVAL REQUIRED") { t.Fatal("expected approval prompt in terminal view") }
}

func TestApprovedOperationResultUpdatesItsTranscriptEntry(t *testing.T) {
	model := Model{screen:screenChat, transcript:[]transcriptEntry{{
		isTool:true, tool:"local.file.write", status:"approval_required",
		output:`{"status":"approval_required"}`,
	}}}
	model.handleLocalOperation(map[string]any{
		"approval":"now",
		"status":"executed",
		"command":"local.file.write /tmp/example",
		"stdout":"written 12 bytes",
		"exitCode":0,
	})
	if model.transcript[0].status != "executed" { t.Fatalf("expected executed status, got %q", model.transcript[0].status) }
	if !strings.Contains(model.transcript[0].output, "written 12 bytes") { t.Fatalf("expected execution result in transcript, got %q", model.transcript[0].output) }
}

func TestPendingApprovalIsRestoredIntoTranscript(t *testing.T) {
	backend := &fakeBackend{status:securestorage.Status{Available:true,Configured:true,Unlocked:true}}
	backend.state.AI.AgentMessages = []domainai.ChatMessage{{Role:"assistant",Content:"Earlier answer."}}
	backend.state.AI.PendingNativeToolCall = &domainai.PendingNativeToolCall{
		Target:"local", RequestID:"pending-1", ToolName:"local.file.write", UserMessage:"write the file", ToolArguments:`{"path":"/tmp/example.txt","content":"write this exact content"}`,
	}
	backend.state.AI.CommandPolicy.PendingRequests = []domainai.CommandRequest{{
		Target:"local", ID:"pending-1", ToolID:"local-files", Command:"/tmp/example.txt",
	}}
	model := NewModelWithBackend(backend, &fakeAgent{})
	if model.approval == nil { t.Fatal("expected pending approval to be restored") }
	if !strings.Contains(model.approval.Preview, "write this exact content") { t.Fatalf("expected the write content preview to be restored, got %q", model.approval.Preview) }
	if len(model.transcript) != 3 || model.transcript[1].role != "user" || model.transcript[1].content != "write the file" || !model.transcript[2].isTool { t.Fatalf("expected pending user prompt followed by operation in transcript, got %#v", model.transcript) }
	if !strings.Contains(model.transcript[2].output, "approval_required") { t.Fatalf("expected pending status in transcript, got %q", model.transcript[2].output) }
}

func TestAgentAutomaticallyRetriesPromptAfterSecureStorageUnlock(t *testing.T) {
	backend := &fakeBackend{status:securestorage.Status{Available:true,Configured:true,Unlocked:true}}
	backend.state.AI.Providers = []domainai.ProviderDescriptor{{ID:"cloud",Name:"Cloud",Selected:true,Configured:true}}
	agent := &fakeAgent{firstErr:securestorage.ErrMasterPasswordRequired}
	model := NewModelWithBackend(backend, agent)
	model.input = "inspect the current directory"
	_, wait := model.updateChatKey(tea.KeyMsg{Type:tea.KeyEnter})
	if agent.calls != 1 || wait == nil { t.Fatalf("expected first agent request, calls=%d", agent.calls) }
	event := wait()
	updated, _ := model.Update(event)
	model = updated.(Model)
	if model.screen != screenUnlockPassword { t.Fatalf("expected unlock prompt, got %v", model.screen) }
	if model.retryPrompt != "inspect the current directory" { t.Fatalf("expected pending prompt to be retained, got %q", model.retryPrompt) }

	model.password = "master-password"
	_, unlock := model.updatePasswordKey(tea.KeyMsg{Type:tea.KeyEnter})
	if unlock == nil { t.Fatal("expected unlock command") }
	unlocked, retry := model.Update(unlock())
	model = unlocked.(Model)
	if retry == nil || agent.calls != 2 { t.Fatalf("expected automatic retry after unlock, calls=%d", agent.calls) }
	if model.retryPrompt != "" { t.Fatalf("expected retry prompt to be cleared, got %q", model.retryPrompt) }
	userEntries := 0
	for _, entry := range model.transcript { if !entry.isTool && entry.role == "user" && entry.content == "inspect the current directory" { userEntries++ } }
	if userEntries != 1 { t.Fatalf("expected one user transcript entry after retry, got %d", userEntries) }
}

func TestApprovalAutomaticallyRetriesAfterSecureStorageUnlock(t *testing.T) {
	backend := &fakeBackend{
		status:securestorage.Status{Available:true,Configured:true,Unlocked:true},
		resolveErr:securestorage.ErrMasterPasswordRequired,
	}
	model := NewModelWithBackend(backend, &fakeAgent{})
	model.approval = &agentai.ApprovalRequest{RequestID:"approval-1",ToolID:"local-files",Target:"local",Command:"/tmp/example"}
	_, resolve := model.updateChatKey(tea.KeyMsg{Type:tea.KeyRunes,Runes:[]rune{'y'}})
	if resolve == nil { t.Fatal("expected approval resolution command") }
	updated, _ := model.Update(resolve())
	model = updated.(Model)
	if model.screen != screenUnlockPassword || !model.retryApproval { t.Fatalf("expected vault unlock and approval retry, screen=%v retry=%v", model.screen, model.retryApproval) }

	model.password = "master-password"
	_, unlock := model.updatePasswordKey(tea.KeyMsg{Type:tea.KeyEnter})
	unlocked, retry := model.Update(unlock())
	model = unlocked.(Model)
	if retry == nil { t.Fatal("expected approval to retry automatically after unlock") }
	done := retry()
	updated, _ = model.Update(done)
	model = updated.(Model)
	if backend.resolveCalls != 2 { t.Fatalf("expected approval resolution retry, calls=%d", backend.resolveCalls) }
	if backend.lastMode != domainai.CommandPermissionModeNow { t.Fatalf("expected allow-once mode to be preserved, got %q", backend.lastMode) }
	if model.approval != nil { t.Fatal("expected approval to clear after successful retry") }
}

func TestToolTranscriptKeepsFailureStatusAfterFinishedEvent(t *testing.T) {
	model := Model{screen:screenChat}
	model.handleAgentEvent(agentai.Event{Type:agentai.EventToolStarted,Tool:"local.exec"})
	model.handleAgentEvent(agentai.Event{Type:agentai.EventToolOutput,Content:`{"status":"execution_failed","result":{"exitCode":1}}`})
	model.handleAgentEvent(agentai.Event{Type:agentai.EventToolFinished,Tool:"local.exec"})
	if len(model.transcript) != 1 { t.Fatalf("expected one tool entry, got %#v", model.transcript) }
	if model.transcript[0].status != "execution_failed" { t.Fatalf("expected failure status, got %q", model.transcript[0].status) }
}

func TestSafeTerminalTextRemovesControlCharacters(t *testing.T) {
	got := safeTerminalText("hello\x1b[31mred\x1b[0m\nnext\x07")
	want := "hello[31mred[0m\nnext"
	if got != want { t.Fatalf("safeTerminalText() = %q, want %q", got, want) }
}

func TestApprovalPromptsForVaultUnlockBeforeExecuting(t *testing.T) {
	backend := &fakeBackend{status:securestorage.Status{Available:true,Configured:true,Unlocked:false}}
	model := NewModelWithBackend(backend, &fakeAgent{})
	model.approval = &agentai.ApprovalRequest{RequestID:"approval-locked",ToolID:"local-files",Target:"local",Command:"/tmp/example"}
	cmd := model.resolveApproval(domainai.CommandPermissionModeNow)
	if cmd != nil || model.screen != screenUnlockPassword || !model.retryApproval {
		t.Fatalf("expected unlock prompt before approval, cmd=%v screen=%v retry=%v", cmd != nil, model.screen, model.retryApproval)
	}
	if backend.resolveCalls != 0 { t.Fatalf("operation reached backend while vault locked: %d calls", backend.resolveCalls) }

	model.password = "master-password"
	_, unlock := model.updatePasswordKey(tea.KeyMsg{Type:tea.KeyEnter})
	unlocked, retry := model.Update(unlock())
	model = unlocked.(Model)
	if retry == nil { t.Fatal("expected approval to continue after unlock") }
	updated, _ := model.Update(retry())
	model = updated.(Model)
	if backend.resolveCalls != 1 || model.approval != nil { t.Fatalf("expected one approved operation after unlock, calls=%d approval=%#v", backend.resolveCalls, model.approval) }
}

func TestDenyApprovalDoesNotRequireVaultUnlock(t *testing.T) {
	backend := &fakeBackend{status:securestorage.Status{Available:true,Configured:true,Unlocked:false}}
	model := NewModelWithBackend(backend, &fakeAgent{})
	model.screen = screenChat // Simulate the vault auto-locking during an active conversation.
	model.approval = &agentai.ApprovalRequest{RequestID:"approval-deny-locked",ToolID:"local-files",Target:"local",Command:"/tmp/example"}
	cmd := model.resolveApproval(domainai.CommandPermissionModeDeny)
	if cmd == nil { t.Fatal("expected denial to proceed without unlocking secure storage") }
	updated, _ := model.Update(cmd())
	model = updated.(Model)
	if backend.resolveCalls != 1 || model.screen == screenUnlockPassword || model.approval != nil {
		t.Fatalf("expected denial without unlock, calls=%d screen=%v approval=%#v", backend.resolveCalls, model.screen, model.approval)
	}
}


func TestLocalOperationWithoutApprovalUpdatesExistingToolTranscript(t *testing.T) {
	model := Model{screen:screenChat, transcript:[]transcriptEntry{{
		isTool:true, tool:"local.exec", status:"running",
	}}}
	model.handleLocalOperation(map[string]any{
		"approval":"not_required",
		"status":"executed",
		"command":"local.exec pwd",
		"exitCode":0,
		"stdout":"/workspace",
		"stderr":"",
		"durationMs":3,
	})
	if len(model.transcript) != 1 {
		t.Fatalf("expected the existing tool entry to be updated, got %d entries", len(model.transcript))
	}
	entry := model.transcript[0]
	if entry.status != "executed" {
		t.Fatalf("expected executed status, got %q", entry.status)
	}
	for _, want := range []string{"local.exec pwd", "/workspace", "exitCode", "durationMs"} {
		if !strings.Contains(entry.output, want) {
			t.Fatalf("expected operation output to contain %q, got %q", want, entry.output)
		}
	}
}

func TestLocalPolicyDenialUpdatesRunningToolEntry(t *testing.T) {
	model := Model{screen:screenChat, transcript:[]transcriptEntry{{
		isTool:true, tool:"local.exec", status:"running",
	}}}
	model.handleLocalOperation(map[string]any{
		"approval":"not_required",
		"status":"denied",
		"command":"local.exec sudo id",
		"exitCode":-1,
		"message":"privilege escalation is not allowed",
	})
	if len(model.transcript) != 1 || model.transcript[0].status != "denied" {
		t.Fatalf("expected denial to update the running tool entry, got %#v", model.transcript)
	}
}

func TestChatInputSupportsRuneAwareEditing(t *testing.T) {
	model := Model{screen:screenChat, input:"a界c", cursor:2}
	model.updateChatKey(tea.KeyMsg{Type:tea.KeyLeft})
	if model.cursor != 1 { t.Fatalf("expected cursor at rune 1, got %d", model.cursor) }
	model.updateChatKey(tea.KeyMsg{Type:tea.KeyRunes, Runes:[]rune{"🙂"[0]}})
	if model.input != "a🙂界c" || model.cursor != 2 {
		t.Fatalf("unexpected rune insertion: input=%q cursor=%d", model.input, model.cursor)
	}
	model.updateChatKey(tea.KeyMsg{Type:tea.KeyBackspace})
	if model.input != "a界c" || model.cursor != 1 {
		t.Fatalf("unexpected backspace: input=%q cursor=%d", model.input, model.cursor)
	}
	model.updateChatKey(tea.KeyMsg{Type:tea.KeyEnd})
	model.updateChatKey(tea.KeyMsg{Type:tea.KeyRunes, Runes:[]rune{"!"}})
	if model.input != "a界c!" { t.Fatalf("unexpected end insertion: %q", model.input) }
}

func TestChatInputSupportsMultilinePrompt(t *testing.T) {
	model := Model{screen:screenChat, input:"first", cursor:5}
	model.updateChatKey(tea.KeyMsg{Type:tea.KeyCtrlJ})
	model.updateChatKey(tea.KeyMsg{Type:tea.KeyRunes, Runes:[]rune("second")})
	if model.input != "first\\nsecond" { t.Fatalf("expected multiline input, got %q", model.input) }
}

func TestTranscriptCanScrollOlderAndReturnToLatest(t *testing.T) {
	model := Model{screen:screenChat, height:24}
	for i:=0; i<20; i++ {
		model.transcript = append(model.transcript, transcriptEntry{role:"user", content:strings.Repeat("line ", 4)+string(rune('A'+i))})
	}
	model.updateChatKey(tea.KeyMsg{Type:tea.KeyPgUp})
	if model.scrollOffset == 0 { t.Fatal("expected PageUp to scroll transcript") }
	model.updateChatKey(tea.KeyMsg{Type:tea.KeyEnd, Alt:true})
	if model.scrollOffset != 0 { t.Fatalf("expected Alt+End to return to latest, got %d", model.scrollOffset) }
}
