package engine

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Jekinnnnnn/jekylan/internal/skill"
	"github.com/Jekinnnnnn/jekylan/internal/tool"
)

func TestBuildFullSystemPromptDefault(t *testing.T) {
	prompt := buildFullSystemPrompt("", "glm5.1")

	if !strings.Contains(prompt, "Use the instructions below") {
		t.Error("expected intro section")
	}
	if !strings.Contains(prompt, "# System") {
		t.Error("expected system section")
	}
	if !strings.Contains(prompt, "# Doing tasks") {
		t.Error("expected doing tasks section")
	}
	if !strings.Contains(prompt, "# Executing actions with care") {
		t.Error("expected actions section")
	}
	if !strings.Contains(prompt, "# Using your tools") {
		t.Error("expected using tools section")
	}
	if !strings.Contains(prompt, "# Tone and style") {
		t.Error("expected tone and style section")
	}
	if !strings.Contains(prompt, "# Communicating with the user") {
		t.Error("expected output efficiency section")
	}
	if !strings.Contains(prompt, "# Environment") {
		t.Error("expected environment section")
	}

	cwd, _ := os.Getwd()
	if !strings.Contains(prompt, cwd) {
		t.Error("expected working directory in env section")
	}
	if !strings.Contains(prompt, runtime.GOOS) {
		t.Error("expected platform in env section")
	}
	if !strings.Contains(prompt, "glm5.1") {
		t.Error("expected model in env section")
	}
}

func TestGetUsingToolsSection(t *testing.T) {
	section := getUsingToolsSection()
	if !strings.Contains(section, "bash") {
		t.Error("expected bash tool reference")
	}
	if !strings.Contains(section, "parallel") {
		t.Error("expected parallel tool call guidance")
	}
}

func TestBuildSystemPrompt(t *testing.T) {
	reg := skill.NewRegistry()
	reg.Register(&skill.Skill{
		Name:          "test",
		Description:   "Test skill",
		Content:       "content",
		UserInvocable: true,
	})
	tools := tool.NewRegistry(tool.SkillTool{Registry: reg})

	prompt := BuildSystemPrompt("", "glm5.1", tools, "", "")

	// Base sections
	if !strings.Contains(prompt, "Use the instructions below") {
		t.Error("expected base intro")
	}

	// Tool contribution (SkillTool system prompt)
	if !strings.Contains(prompt, "Execute a skill") {
		t.Error("expected SkillTool system prompt contribution")
	}
}

func TestBuildSystemPromptWithTokenBudget(t *testing.T) {
	prompt := BuildSystemPrompt("", "model", tool.NewRegistry(), "1000000", "")
	if !strings.Contains(prompt, "Token Budget") {
		t.Error("expected token budget section")
	}
}

func TestBuildSystemPromptWithMemory(t *testing.T) {
	// Memory section is no longer injected into the main system prompt;
	// it is handled by the MemoryWorker goroutine instead.
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "MEMORY.md"), []byte("- [Test](test.md) — test hook"), 0644); err != nil {
		t.Fatal(err)
	}
	prompt := BuildSystemPrompt("", "model", tool.NewRegistry(), "", tmpDir)
	if strings.Contains(prompt, "auto memory") {
		t.Error("memory section should NOT be in main system prompt")
	}
	if strings.Contains(prompt, "MEMORY.md") {
		t.Error("MEMORY.md content should NOT be in main system prompt")
	}
}
