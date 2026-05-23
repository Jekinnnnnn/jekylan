package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRegistry_Builtin(t *testing.T) {
	r := NewBuiltinRegistry()
	if d := r.Get("general-purpose"); d == nil {
		t.Fatal("expected general-purpose agent")
	}
	if d := r.Get("worker"); d == nil {
		t.Fatal("expected worker agent")
	}
	if d := r.Get("nonexistent"); d != nil {
		t.Fatal("expected nil for unknown agent")
	}
}

func TestRegistry_List(t *testing.T) {
	r := NewBuiltinRegistry()
	list := r.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(list))
	}
	names := make(map[string]bool)
	for _, d := range list {
		names[d.Name] = true
	}
	if !names["general-purpose"] || !names["worker"] {
		t.Fatalf("expected general-purpose, worker in list, got: %v", names)
	}
}

func TestRegistry_MergePriority(t *testing.T) {
	r := NewRegistry()
	r.Register(&Definition{Name: "a", Description: "first"})
	r.Register(&Definition{Name: "a", Description: "second"})
	if d := r.Get("a"); d.Description != "second" {
		t.Fatalf("expected second to overwrite first, got: %s", d.Description)
	}
}

func TestRegistry_LoadFromDir(t *testing.T) {
	tmp := t.TempDir()

	content := `---
name: custom-explorer
description: Custom explorer
tools:
  - grep
  - glob
---

You are a custom explorer.
`
	os.WriteFile(filepath.Join(tmp, "custom.md"), []byte(content), 0644)

	r := NewRegistry()
	if err := r.LoadFromDir(tmp); err != nil {
		t.Fatalf("LoadFromDir failed: %v", err)
	}

	d := r.Get("custom-explorer")
	if d == nil {
		t.Fatal("expected custom-explorer agent")
	}
	if d.Description != "Custom explorer" {
		t.Fatalf("expected description 'Custom explorer', got: %s", d.Description)
	}
	if len(d.ToolsAllow) != 2 || d.ToolsAllow[0] != "grep" {
		t.Fatalf("unexpected tools_allow: %v", d.ToolsAllow)
	}
	if !strings.Contains(d.SystemPrompt, "You are a custom explorer") {
		t.Fatalf("expected body as system prompt, got: %s", d.SystemPrompt)
	}
}

func TestRegistry_LoadFromDir_MissingDir(t *testing.T) {
	r := NewRegistry()
	if err := r.LoadFromDir("/does/not/exist"); err != nil {
		t.Fatalf("expected nil for missing dir, got: %v", err)
	}
}

func TestRegistry_LoadFromDir_SkipInvalid(t *testing.T) {
	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, "bad.md"), []byte("no frontmatter"), 0644)

	r := NewRegistry()
	if err := r.LoadFromDir(tmp); err != nil {
		t.Fatalf("LoadFromDir failed: %v", err)
	}
	if len(r.List()) != 0 {
		t.Fatalf("expected 0 agents, got %d", len(r.List()))
	}
}

func TestRegistry_LoadFromDir_SystemPromptFromFrontmatter(t *testing.T) {
	tmp := t.TempDir()
	content := `---
name: frontmatter-prompt
system_prompt: Prompt from frontmatter
---

Body should be ignored.
`
	os.WriteFile(filepath.Join(tmp, "fm.md"), []byte(content), 0644)

	r := NewRegistry()
	if err := r.LoadFromDir(tmp); err != nil {
		t.Fatalf("LoadFromDir failed: %v", err)
	}

	d := r.Get("frontmatter-prompt")
	if d == nil {
		t.Fatal("expected agent")
	}
	if d.SystemPrompt != "Prompt from frontmatter" {
		t.Fatalf("expected frontmatter prompt, got: %s", d.SystemPrompt)
	}
}

func TestParseAgentMarkdown_Errors(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"no frontmatter", "just text"},
		{"unterminated frontmatter", "---\nname: x"},
		{"missing name", "---\ndescription: ok\n---\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseAgentMarkdown(tc.input)
			if err == nil {
				t.Fatal("expected error")
			}
		})
	}
}
