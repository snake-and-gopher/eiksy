package app

import (
	"testing"

	"eiksy/internal/domain/ai"
)

func TestMatchCommandPattern(t *testing.T) {
	tests := []struct {
		pattern string
		command string
		want    bool
	}{
		{"pwd", "pwd", true},
		{"pwd", "pwd /tmp", false},
		{"ls *", "ls -la /tmp", true},
		{"ls *", "lsof /tmp", false},
	}
	for _, tt := range tests {
		if got := matchCommandPattern(tt.pattern, tt.command); got != tt.want {
			t.Fatalf("matchCommandPattern(%q, %q) = %v, want %v", tt.pattern, tt.command, got, tt.want)
		}
	}
}

func TestEvaluateCommandPolicyDefaultRules(t *testing.T) {
	policy := ai.CommandPolicy{
		Tools:        []ai.CommandTool{{ID: "shell", Enabled: true}},
		CommandRules: defaultCommandRules(),
	}

	tests := []struct {
		command string
		want    commandPolicyDecision
	}{
		{"pwd", commandPolicyDecisionAllow},
		{"ls -la /tmp", commandPolicyDecisionAllow},
		{"df -h", commandPolicyDecisionAllow},
		{"systemctl status ssh", commandPolicyDecisionAllow},
		{"systemctl restart ssh", commandPolicyDecisionAsk},
		{"cat /etc/hosts", commandPolicyDecisionAsk},
		{"rm -rf /tmp/test", commandPolicyDecisionDeny},
		{"shutdown -h now", commandPolicyDecisionDeny},
	}

	for _, tt := range tests {
		got, _ := evaluateCommandPolicy(policy, "shell", "session-1", tt.command)
		if got != tt.want {
			t.Errorf("evaluateCommandPolicy(%q) = %q, want %q", tt.command, got, tt.want)
		}
	}
}

func TestEvaluateCommandPolicyRejectsShellComposition(t *testing.T) {
	policy := ai.CommandPolicy{
		Tools:        []ai.CommandTool{{ID: "shell", Enabled: true}},
		CommandRules: []ai.CommandRule{{Pattern: "ls *", Action: ai.CommandPermissionAllow}},
	}

	for _, command := range []string{
		"ls /tmp; rm -rf /",
		"ls /tmp && shutdown -h now",
		"ls /tmp | cat",
		"ls $(whoami)",
	} {
		got, _ := evaluateCommandPolicy(policy, "shell", "session-1", command)
		if got != commandPolicyDecisionDeny {
			t.Errorf("evaluateCommandPolicy(%q) = %q, want deny", command, got)
		}
	}
}

func TestEvaluateCommandPolicyUnknownCommandAsks(t *testing.T) {
	policy := ai.CommandPolicy{
		Tools:        []ai.CommandTool{{ID: "shell", Enabled: true}},
		CommandRules: defaultCommandRules(),
	}

	got, _ := evaluateCommandPolicy(policy, "shell", "session-1", "journalctl -u ssh")
	if got != commandPolicyDecisionAsk {
		t.Fatalf("unknown command decision = %q, want ask", got)
	}
}

func TestEvaluateCommandPolicyScopesAndPrecedence(t *testing.T) {
	policy := ai.CommandPolicy{
		Tools: []ai.CommandTool{{ID: "shell", Enabled: true}, {ID: "other", Enabled: true}},
		CommandRules: []ai.CommandRule{
			{Pattern: "cat *", Action: ai.CommandPermissionAsk},
			{ToolID: "shell", Pattern: "cat /etc/hosts", Action: ai.CommandPermissionAllow},
			{ToolID: "shell", SessionID: "session-2", Pattern: "cat /etc/hosts", Action: ai.CommandPermissionDeny},
			{ToolID: "other", Pattern: "cat /etc/hosts", Action: ai.CommandPermissionAllow},
		},
	}

	if got, _ := evaluateCommandPolicy(policy, "shell", "session-1", "cat /etc/hosts"); got != commandPolicyDecisionAllow {
		t.Fatalf("exact approval = %q, want allow", got)
	}
	if got, _ := evaluateCommandPolicy(policy, "shell", "session-2", "cat /etc/hosts"); got != commandPolicyDecisionDeny {
		t.Fatalf("explicit session deny = %q, want deny", got)
	}
	if got, _ := evaluateCommandPolicy(policy, "shell", "session-1", "cat /etc/passwd"); got != commandPolicyDecisionAsk {
		t.Fatalf("broad ask = %q, want ask", got)
	}
	if got, _ := evaluateCommandPolicy(policy, "other", "session-1", "cat /etc/hosts"); got != commandPolicyDecisionAllow {
		t.Fatalf("tool-scoped rule = %q, want allow", got)
	}
}

func TestEvaluateCommandPolicyRejectsSecretExpansion(t *testing.T) {
	policy := ai.CommandPolicy{
		Tools:        []ai.CommandTool{{ID: "shell", Enabled: true}},
		CommandRules: []ai.CommandRule{{Pattern: "echo *", Action: ai.CommandPermissionAllow}},
	}
	for _, command := range []string{"echo $TOKEN", "echo ${TOKEN}", "echo foo\\bar"} {
		if got, _ := evaluateCommandPolicy(policy, "shell", "session-1", command); got != commandPolicyDecisionDeny {
			t.Errorf("secret/escape command %q = %q, want deny", command, got)
		}
	}
}


func TestEvaluateCommandPolicySeparatesLocalAndSSHTargets(t *testing.T) {
	policy := ai.CommandPolicy{
		Tools: []ai.CommandTool{{ID: "shell", Enabled: true}},
		CommandRules: []ai.CommandRule{
			{Target: commandTargetSSH, ToolID: "shell", Pattern: "pwd", Action: ai.CommandPermissionAllow},
			{Target: commandTargetLocal, ToolID: "shell", Pattern: "pwd", Action: ai.CommandPermissionAsk},
		},
	}
	if got, _ := evaluateCommandPolicyForTarget(policy, commandTargetSSH, "shell", "session-1", "pwd"); got != commandPolicyDecisionAllow {
		t.Fatalf("SSH target decision = %q, want allow", got)
	}
	if got, _ := evaluateCommandPolicyForTarget(policy, commandTargetLocal, "shell", "", "pwd"); got != commandPolicyDecisionAsk {
		t.Fatalf("local target decision = %q, want ask", got)
	}
}

func TestEvaluateCommandPolicyTargetSpecificDenyCannotBeOverriddenByOtherTarget(t *testing.T) {
	policy := ai.CommandPolicy{
		Tools: []ai.CommandTool{{ID: "shell", Enabled: true}},
		CommandRules: []ai.CommandRule{
			{Target: commandTargetSSH, ToolID: "shell", Pattern: "rm *", Action: ai.CommandPermissionDeny},
			{Target: commandTargetLocal, ToolID: "shell", Pattern: "rm *", Action: ai.CommandPermissionAsk},
		},
	}
	if got, _ := evaluateCommandPolicyForTarget(policy, commandTargetLocal, "shell", "", "rm -rf tmp"); got != commandPolicyDecisionAsk {
		t.Fatalf("local target decision = %q, want ask", got)
	}
	if got, _ := evaluateCommandPolicyForTarget(policy, commandTargetSSH, "shell", "session-1", "rm -rf tmp"); got != commandPolicyDecisionDeny {
		t.Fatalf("SSH target decision = %q, want deny", got)
	}
}

func TestNormalizeCommandTargetDefaultsToSSH(t *testing.T) {
	if got := normalizeCommandTarget(""); got != commandTargetSSH {
		t.Fatalf("empty target = %q, want %q", got, commandTargetSSH)
	}
	if got := normalizeCommandTarget(" LOCAL "); got != commandTargetLocal {
		t.Fatalf("normalized target = %q, want %q", got, commandTargetLocal)
	}
}
