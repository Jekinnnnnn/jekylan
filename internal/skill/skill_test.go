package skill

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseSkillFile(t *testing.T) {
	content := `---
name: commit
description: Generate a git commit message
allowed-tools: [bash, file_read]
when_to_use: Use when the user asks to write a commit message
---

You are a commit message generator. Look at the staged changes and write a concise commit message.
`
	s, err := parseSkillFile("commit", content, "/skills/commit")
	if err != nil {
		t.Fatalf("parseSkillFile failed: %v", err)
	}

	if s.Name != "commit" {
		t.Errorf("Name = %q, want commit", s.Name)
	}
	if s.Description != "Generate a git commit message" {
		t.Errorf("Description = %q", s.Description)
	}
	if s.WhenToUse != "Use when the user asks to write a commit message" {
		t.Errorf("WhenToUse = %q", s.WhenToUse)
	}
	if len(s.AllowedTools) != 2 || s.AllowedTools[0] != "bash" {
		t.Errorf("AllowedTools = %v", s.AllowedTools)
	}
}

func TestParseSkillFileEnabledFalse(t *testing.T) {
	content := `---
name: off
description: A disabled skill
enabled: false
---
Content`

	s, err := parseSkillFile("off", content, "/skills/off")
	if err != nil {
		t.Fatalf("parseSkillFile failed: %v", err)
	}
	if s != nil {
		t.Errorf("expected nil skill for enabled: false, got %+v", s)
	}
}

func TestParseSkillFileEnabledTrueExplicit(t *testing.T) {
	content := `---
name: on
description: An explicitly enabled skill
enabled: true
---
Content`

	s, err := parseSkillFile("on", content, "/skills/on")
	if err != nil {
		t.Fatalf("parseSkillFile failed: %v", err)
	}
	if s == nil {
		t.Fatal("expected non-nil skill for enabled: true")
	}
	if s.Name != "on" {
		t.Errorf("Name = %q, want on", s.Name)
	}
}

func TestParseSkillFileEnabledDefault(t *testing.T) {
	content := `---
name: implicit
description: No enabled key set
---
Content`

	s, err := parseSkillFile("implicit", content, "/skills/implicit")
	if err != nil {
		t.Fatalf("parseSkillFile failed: %v", err)
	}
	if s == nil {
		t.Fatal("expected non-nil skill when enabled is unset (default true)")
	}
}

func TestSubstituteArgs(t *testing.T) {
	content := "Args: $ARGUMENTS, First: $1, Second: $2"
	got := SubstituteArgs(content, "hello world", nil)
	want := "Args: hello world, First: hello, Second: world"
	if got != want {
		t.Errorf("SubstituteArgs = %q, want %q", got, want)
	}
}

func TestSubstituteArgsNamed(t *testing.T) {
	content := "Name: ${name}, Age: ${age}"
	got := SubstituteArgs(content, "Alice 30", []string{"name", "age"})
	want := "Name: Alice, Age: 30"
	if got != want {
		t.Errorf("SubstituteArgs = %q, want %q", got, want)
	}
}

func TestLoadDir(t *testing.T) {
	// Create a temporary skills directory
	dir := t.TempDir()

	// Create skill1/SKILL.md
	skill1Dir := filepath.Join(dir, "skill1")
	os.MkdirAll(skill1Dir, 0755)
	os.WriteFile(filepath.Join(skill1Dir, "SKILL.md"), []byte(`---
name: skill1
description: Test skill 1
---
Content 1`), 0644)

	// Create skill2/SKILL.md
	skill2Dir := filepath.Join(dir, "skill2")
	os.MkdirAll(skill2Dir, 0755)
	os.WriteFile(filepath.Join(skill2Dir, "SKILL.md"), []byte(`---
name: skill2
description: Test skill 2
---
Content 2`), 0644)

	reg, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir failed: %v", err)
	}

	if len(reg.All()) != 2 {
		t.Errorf("expected 2 skills, got %d", len(reg.All()))
	}

	s1 := reg.Find("skill1")
	if s1 == nil {
		t.Fatal("skill1 not found")
	}
	if s1.Description != "Test skill 1" {
		t.Errorf("skill1 Description = %q", s1.Description)
	}

	s2 := reg.Find("skill2")
	if s2 == nil {
		t.Fatal("skill2 not found")
	}
}

func TestLoadDirNonExistent(t *testing.T) {
	reg, err := LoadDir("/nonexistent/path")
	if err != nil {
		t.Fatalf("LoadDir should not error for non-existent dir: %v", err)
	}
	if len(reg.All()) != 0 {
		t.Errorf("expected 0 skills for non-existent dir, got %d", len(reg.All()))
	}
}

func TestLoadDirSkipsDisabled(t *testing.T) {
	dir := t.TempDir()

	enabledDir := filepath.Join(dir, "enabled-skill")
	os.MkdirAll(enabledDir, 0755)
	os.WriteFile(filepath.Join(enabledDir, "SKILL.md"), []byte(`---
name: enabled-skill
description: Active
---
Content`), 0644)

	disabledDir := filepath.Join(dir, "disabled-skill")
	os.MkdirAll(disabledDir, 0755)
	os.WriteFile(filepath.Join(disabledDir, "SKILL.md"), []byte(`---
name: disabled-skill
description: Inactive
enabled: false
---
Content`), 0644)

	reg, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir failed: %v", err)
	}

	if len(reg.All()) != 1 {
		t.Errorf("expected 1 skill (disabled one skipped), got %d", len(reg.All()))
	}
	if reg.Find("disabled-skill") != nil {
		t.Error("expected disabled-skill to be absent from registry")
	}
	if reg.Find("enabled-skill") == nil {
		t.Error("expected enabled-skill to be present in registry")
	}
}
