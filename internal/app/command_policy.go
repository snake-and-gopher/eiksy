package app

import (
	"fmt"
	"strings"

	"eiksy/internal/domain/ai"
)

type commandPolicyDecision string

const (
	commandPolicyDecisionAllow commandPolicyDecision = "allow"
	commandPolicyDecisionAsk   commandPolicyDecision = "ask"
	commandPolicyDecisionDeny  commandPolicyDecision = "deny"
)

func defaultCommandRules() []ai.CommandRule {
	return []ai.CommandRule{
		{Pattern: "pwd", Action: ai.CommandPermissionAllow, Description: "Print current directory"},
		{Pattern: "whoami", Action: ai.CommandPermissionAllow, Description: "Print current user"},
		{Pattern: "id", Action: ai.CommandPermissionAllow, Description: "Print user identity"},
		{Pattern: "uname *", Action: ai.CommandPermissionAllow, Description: "Read system information"},
		{Pattern: "hostname", Action: ai.CommandPermissionAllow, Description: "Print hostname"},
		{Pattern: "ls *", Action: ai.CommandPermissionAllow, Description: "List directory contents"},
		{Pattern: "ls", Action: ai.CommandPermissionAllow, Description: "List directory contents"},
		{Pattern: "df -h", Action: ai.CommandPermissionAllow, Description: "Read filesystem usage"},
		{Pattern: "free -h", Action: ai.CommandPermissionAllow, Description: "Read memory usage"},
		{Pattern: "ps *", Action: ai.CommandPermissionAllow, Description: "Read process list"},
		{Pattern: "systemctl status *", Action: ai.CommandPermissionAllow, Description: "Read service status"},
		{Pattern: "systemctl restart *", Action: ai.CommandPermissionAsk, Description: "Restart a service"},
		{Pattern: "systemctl stop *", Action: ai.CommandPermissionAsk, Description: "Stop a service"},
		{Pattern: "systemctl start *", Action: ai.CommandPermissionAsk, Description: "Start a service"},
		{Pattern: "cat /etc/shadow", Action: ai.CommandPermissionDeny, Description: "Direct access to password hashes is prohibited"},
		{Pattern: "cat /etc/gshadow", Action: ai.CommandPermissionDeny, Description: "Direct access to password hashes is prohibited"},
		{Pattern: "cat ~/.ssh/*", Action: ai.CommandPermissionDeny, Description: "Direct access to SSH private material is prohibited"},
		{Pattern: "cat .env*", Action: ai.CommandPermissionDeny, Description: "Direct access to environment secret files is prohibited"},
		{Pattern: "cat *", Action: ai.CommandPermissionAsk, Description: "Read a file; may expose secrets to the AI"},
		{Pattern: "grep *", Action: ai.CommandPermissionAsk, Description: "Search file contents; may expose secrets to the AI"},
		{Pattern: "find *", Action: ai.CommandPermissionAsk, Description: "Search the filesystem"},
		{Pattern: "env", Action: ai.CommandPermissionDeny, Description: "Environment variables may contain credentials and tokens"},
		{Pattern: "printenv *", Action: ai.CommandPermissionDeny, Description: "Environment variables may contain credentials and tokens"},
		{Pattern: "history", Action: ai.CommandPermissionDeny, Description: "Command history may contain sensitive credentials"},
		{Pattern: "rm *", Action: ai.CommandPermissionDeny, Description: "Destructive file removal"},
		{Pattern: "rmdir *", Action: ai.CommandPermissionDeny, Description: "Destructive directory removal"},
		{Pattern: "shutdown *", Action: ai.CommandPermissionDeny, Description: "System shutdown"},
		{Pattern: "reboot *", Action: ai.CommandPermissionDeny, Description: "System reboot"},
		{Pattern: "poweroff *", Action: ai.CommandPermissionDeny, Description: "System poweroff"},
		{Pattern: "halt *", Action: ai.CommandPermissionDeny, Description: "System halt"},
		{Pattern: "kill *", Action: ai.CommandPermissionDeny, Description: "Process termination"},
		{Pattern: "pkill *", Action: ai.CommandPermissionDeny, Description: "Process termination"},
		{Pattern: "killall *", Action: ai.CommandPermissionDeny, Description: "Process termination"},
		{Pattern: "chmod *", Action: ai.CommandPermissionDeny, Description: "Permission modification"},
		{Pattern: "chown *", Action: ai.CommandPermissionDeny, Description: "Ownership modification"},
		{Pattern: "useradd *", Action: ai.CommandPermissionDeny, Description: "Account creation"},
		{Pattern: "userdel *", Action: ai.CommandPermissionDeny, Description: "Account deletion"},
		{Pattern: "passwd *", Action: ai.CommandPermissionDeny, Description: "Password modification"},
		{Pattern: "mkfs *", Action: ai.CommandPermissionDeny, Description: "Filesystem destruction"},
		{Pattern: "dd *", Action: ai.CommandPermissionDeny, Description: "Raw disk modification"},
	}
}

func normalizeCommandRules(rules []ai.CommandRule) []ai.CommandRule {
	result := make([]ai.CommandRule, 0, len(rules))
	for _, rule := range rules {
		pattern := strings.TrimSpace(rule.Pattern)
		if pattern == "" {
			continue
		}
		action := ai.CommandPermissionAction(strings.ToLower(strings.TrimSpace(string(rule.Action))))
		if action != ai.CommandPermissionAllow && action != ai.CommandPermissionAsk && action != ai.CommandPermissionDeny {
			continue
		}
		result = append(result, ai.CommandRule{
			Target: normalizeCommandTarget(rule.Target), ToolID: strings.ToLower(strings.TrimSpace(rule.ToolID)), SessionID: strings.TrimSpace(rule.SessionID),
			Pattern: pattern, Action: action, Description: strings.TrimSpace(rule.Description),
		})
	}
	return result
}

func matchCommandPattern(pattern, command string) bool {
	pattern = strings.TrimSpace(pattern)
	command = strings.TrimSpace(command)
	if pattern == "" || command == "" {
		return false
	}
	if pattern == command {
		return true
	}
	if !strings.HasSuffix(pattern, "*") {
		return false
	}
	prefix := strings.TrimSuffix(pattern, "*")
	if strings.HasSuffix(prefix, " ") || strings.HasSuffix(prefix, "\t") {
		base := strings.TrimSpace(prefix)
		return command == base || strings.HasPrefix(command, prefix)
	}
	prefix = strings.TrimSpace(prefix)
	return strings.HasPrefix(command, prefix)
}

func containsUnsafeShellSyntax(command string) bool {
	for _, token := range []string{";", "&&", "||", "|", ">", "<", "`", "$", "\\", "\n", "\r"} {
		if strings.Contains(command, token) {
			return true
		}
	}
	return false
}

func commandRuleSpecificity(rule ai.CommandRule) int {
	score := 0
	if rule.Target != "" { score += 4 }
	if rule.SessionID != "" {
		score += 4
	}
	if rule.ToolID != "" {
		score++
	}
	if !strings.HasSuffix(strings.TrimSpace(rule.Pattern), "*") {
		score += 4
	} else {
		score++
	}
	return score
}

func evaluateCommandPolicy(policy ai.CommandPolicy, toolID, sessionID, command string) (commandPolicyDecision, string) {
	return evaluateCommandPolicyForTarget(policy, commandTargetSSH, toolID, sessionID, command)
}

const (
	commandTargetSSH = "ssh"
	commandTargetLocal = "local"
)

func normalizeCommandTarget(target string) string {
	target = strings.ToLower(strings.TrimSpace(target))
	if target == "" { return commandTargetSSH }
	return target
}

func evaluateCommandPolicyForTarget(policy ai.CommandPolicy, target, toolID, sessionID, command string) (commandPolicyDecision, string) {
	target = normalizeCommandTarget(target)
	toolID = strings.ToLower(strings.TrimSpace(toolID))
	sessionID = strings.TrimSpace(sessionID)
	command = strings.TrimSpace(command)
	if toolID == "" || command == "" {
		return commandPolicyDecisionDeny, "tool and command are required"
	}
	if !commandToolEnabled(policy, toolID) {
		return commandPolicyDecisionDeny, fmt.Sprintf("tool %q is disabled", toolID)
	}
	if toolID == "shell" && containsUnsafeShellSyntax(command) {
		return commandPolicyDecisionDeny, "shell operators, expansion and command substitution are not permitted by Command Policy"
	}

	rules := normalizeCommandRules(policy.CommandRules)
	if len(rules) == 0 {
		return commandPolicyDecisionAsk, "no command rule matched"
	}

	bestSpecificity := -1
	bestDecision := commandPolicyDecisionAsk
	bestReason := "no command rule matched"
	for _, rule := range rules {
		if rule.Target != "" && rule.Target != target {
			continue
		}
		if rule.ToolID != "" && rule.ToolID != toolID {
			continue
		}
		if rule.SessionID != "" && rule.SessionID != sessionID {
			continue
		}
		if !matchCommandPattern(rule.Pattern, command) {
			continue
		}
		if rule.Action == ai.CommandPermissionDeny {
			return commandPolicyDecisionDeny, rule.Description
		}
		specificity := commandRuleSpecificity(rule)
		if specificity < bestSpecificity {
			continue
		}
		if specificity == bestSpecificity && bestSpecificity >= 0 && bestDecision == commandPolicyDecisionAsk && rule.Action == ai.CommandPermissionAllow {
			continue
		}
		bestSpecificity = specificity
		bestDecision = commandPolicyDecision(rule.Action)
		bestReason = rule.Description
	}
	return bestDecision, bestReason
}
