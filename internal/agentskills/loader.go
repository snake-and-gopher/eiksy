package agentskills

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"eiksy/internal/domain/ai"
)

const (
	maxProjectSkills       = 20
	maxProjectSkillBytes   = 12 << 10
	maxProjectSkillContent = 48 << 10
	maxSkillNameLength     = 64
	maxSkillDescription    = 1024
)

// LoadProjectSkills discovers Agent Skills in .agents/skills, starting at
// workingDir and walking up to the nearest Git repository root. It reads only
// direct child skill directories and never follows symlinks.
func LoadProjectSkills(workingDir string) []ai.AgentSkill {
	root := findSkillsDirectory(workingDir)
	if root == "" {
		return nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	skills := make([]ai.AgentSkill, 0, min(len(entries), maxProjectSkills))
	totalBytes := 0
	for _, entry := range entries {
		if len(skills) >= maxProjectSkills {
			break
		}
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		skill, size, ok := readSkill(root, entry.Name())
		if !ok || totalBytes+size > maxProjectSkillContent {
			continue
		}
		totalBytes += size
		skills = append(skills, skill)
	}
	return skills
}

func findSkillsDirectory(workingDir string) string {
	trimmed := strings.TrimSpace(workingDir)
	if trimmed == "" || trimmed == "(unavailable)" {
		return ""
	}
	start, err := filepath.Abs(trimmed)
	if err != nil {
		return ""
	}
	start = filepath.Clean(start)

	// First locate the nearest repository root. Outside a Git repository,
	// only the exact working directory is considered; this avoids inheriting
	// skills from an unrelated parent directory such as the user's home.
	repoRoot := ""
	for current := start; ; current = filepath.Dir(current) {
		if isGitRoot(current) {
			repoRoot = current
			break
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}
	if repoRoot == "" {
		return safeSkillsDirectory(start)
	}

	for current := start; ; current = filepath.Dir(current) {
		if candidate := safeSkillsDirectory(current); candidate != "" {
			return candidate
		}
		if current == repoRoot {
			return ""
		}
		parent := filepath.Dir(current)
		if parent == current {
			return ""
		}
	}
}

func safeSkillsDirectory(projectDir string) string {
	candidate := filepath.Join(projectDir, ".agents", "skills")
	info, err := os.Lstat(candidate)
	if err != nil {
		return ""
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return ""
	}
	return candidate
}

func isGitRoot(path string) bool {
	info, err := os.Lstat(filepath.Join(path, ".git"))
	return err == nil && (info.IsDir() || info.Mode().IsRegular())
}

func readSkill(root, directoryName string) (ai.AgentSkill, int, bool) {
	if !validSkillName(directoryName) {
		return ai.AgentSkill{}, 0, false
	}
	dir := filepath.Join(root, directoryName)
	dirInfo, err := os.Lstat(dir)
	if err != nil || !dirInfo.IsDir() || dirInfo.Mode()&os.ModeSymlink != 0 {
		return ai.AgentSkill{}, 0, false
	}
	path := filepath.Join(dir, "SKILL.md")
	fileInfo, err := os.Lstat(path)
	if err != nil || !fileInfo.Mode().IsRegular() || fileInfo.Mode()&os.ModeSymlink != 0 || fileInfo.Size() > maxProjectSkillBytes {
		return ai.AgentSkill{}, 0, false
	}
	content, err := os.ReadFile(path)
	if err != nil || len(content) > maxProjectSkillBytes {
		return ai.AgentSkill{}, 0, false
	}
	name, description, instructions, ok := parseSkill(content)
	if !ok || name != directoryName {
		return ai.AgentSkill{}, 0, false
	}
	return ai.AgentSkill{
		ID:           "project-" + name,
		Name:         name,
		Description:  description,
		Instructions: instructions,
		Enabled:      true,
	}, len(content), true
}

func parseSkill(content []byte) (string, string, string, bool) {
	text := strings.TrimPrefix(string(content), "\ufeff")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.Split(text, "\n")
	if len(lines) < 4 || strings.TrimSpace(lines[0]) != "---" {
		return "", "", "", false
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" || strings.TrimSpace(lines[i]) == "..." {
			end = i
			break
		}
	}
	if end < 0 {
		return "", "", "", false
	}
	metadata := map[string]string{}
	for _, line := range lines[1:end] {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, ":")
		if !found {
			return "", "", "", false
		}
		key = strings.TrimSpace(key)
		if key != "name" && key != "description" {
			continue
		}
		if _, exists := metadata[key]; exists {
			return "", "", "", false
		}
		value, ok := parseScalar(strings.TrimSpace(value))
		if !ok {
			return "", "", "", false
		}
		metadata[key] = value
	}
	name := strings.TrimSpace(metadata["name"])
	description := strings.TrimSpace(metadata["description"])
	instructions := strings.TrimSpace(strings.Join(lines[end+1:], "\n"))
	if !validSkillName(name) || description == "" || len(description) > maxSkillDescription || instructions == "" {
		return "", "", "", false
	}
	return name, description, instructions, true
}

func parseScalar(value string) (string, bool) {
	if value == "" {
		return "", false
	}
	if strings.HasPrefix(value, "\"") {
		parsed, err := strconv.Unquote(value)
		return parsed, err == nil
	}
	if strings.HasPrefix(value, "'") {
		if len(value) < 2 || !strings.HasSuffix(value, "'") {
			return "", false
		}
		return strings.ReplaceAll(value[1:len(value)-1], "''", "'"), true
	}
	if strings.HasPrefix(value, "[") || strings.HasPrefix(value, "{") || strings.HasPrefix(value, "|") || strings.HasPrefix(value, ">") {
		return "", false
	}
	return value, true
}

func validSkillName(name string) bool {
	if name == "" || len(name) > maxSkillNameLength || name[0] == '-' || name[len(name)-1] == '-' {
		return false
	}
	previousHyphen := false
	for _, r := range name {
		if r == '-' {
			if previousHyphen {
				return false
			}
			previousHyphen = true
			continue
		}
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return false
		}
		previousHyphen = false
	}
	return true
}
