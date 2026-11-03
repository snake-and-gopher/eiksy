package agentskills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSkill(t *testing.T, root, dir, content string) {
	t.Helper()
	path := filepath.Join(root, dir)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "SKILL.md"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadProjectSkillsFindsRepositorySkillsFromNestedDirectory(t *testing.T) {
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	skillsDir := filepath.Join(repo, ".agents", "skills")
	writeSkill(t, skillsDir, "go-review", "---\nname: go-review\ndescription: Review Go changes\n---\nCheck error handling and tests.\n")
	nested := filepath.Join(repo, "internal", "app")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	skills := LoadProjectSkills(nested)
	if len(skills) != 1 {
		t.Fatalf("expected one discovered skill, got %#v", skills)
	}
	if skills[0].Name != "go-review" || skills[0].Description != "Review Go changes" || skills[0].Instructions != "Check error handling and tests." || !skills[0].Enabled {
		t.Fatalf("unexpected skill: %#v", skills[0])
	}
}

func TestLoadProjectSkillsParsesQuotedMetadataAndIgnoresInvalidSkills(t *testing.T) {
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	skillsDir := filepath.Join(repo, ".agents", "skills")
	writeSkill(t, skillsDir, "release-notes", "---\nname: \"release-notes\"\ndescription: 'Prepare ''release'' notes'\n---\nSummarize user-visible changes.\n")
	writeSkill(t, skillsDir, "bad-name", "---\nname: Bad Name\ndescription: invalid\n---\nIgnore this.\n")
	writeSkill(t, skillsDir, "missing-frontmatter", "No metadata\n")

	skills := LoadProjectSkills(repo)
	if len(skills) != 1 || skills[0].Description != "Prepare 'release' notes" {
		t.Fatalf("expected only the valid quoted skill, got %#v", skills)
	}
}

func TestLoadProjectSkillsDoesNotEscapeNonRepositoryDirectory(t *testing.T) {
	parent := t.TempDir()
	writeSkill(t, parent, "parent-skill", "---\nname: parent-skill\ndescription: Must not leak into child projects\n---\nDo not load.\n")
	child := filepath.Join(parent, "child")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := LoadProjectSkills(child); len(got) != 0 {
		t.Fatalf("unexpected parent skill discovery outside a Git repository: %#v", got)
	}
}

func TestLoadProjectSkillsEnforcesAggregateLimit(t *testing.T) {
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	skillsDir := filepath.Join(repo, ".agents", "skills")
	instructions := strings.Repeat("x", maxProjectSkillBytes-200)
	for _, name := range []string{"one", "two", "three", "four", "five"} {
		writeSkill(t, skillsDir, name, "---\nname: "+name+"\ndescription: Test skill\n---\n"+instructions)
	}
	skills := LoadProjectSkills(repo)
	total := 0
	for _, skill := range skills {
		total += len(skill.Instructions)
	}
	if total > maxProjectSkillContent || len(skills) >= 5 {
		t.Fatalf("aggregate limit was not enforced: skills=%d bytes=%d", len(skills), total)
	}
}

func TestLoadProjectSkillsRejectsSymlinkedSkillsDirectory(t *testing.T) {
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	writeSkill(t, outside, "outside-skill", "---\nname: outside-skill\ndescription: Outside\n---\nDo not load.\n")
	if err := os.MkdirAll(filepath.Join(repo, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "outside-skill"), filepath.Join(repo, ".agents", "skills")); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	if got := LoadProjectSkills(repo); len(got) != 0 {
		t.Fatalf("unexpected skill loaded through symlink: %#v", got)
	}
}
