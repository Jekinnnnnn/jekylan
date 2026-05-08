package tool

import (
	"strings"
	"testing"

	"github.com/Jekinnnnnn/jekylan/internal/skill"
)

func TestSkillToolSystemPrompt(t *testing.T) {
	reg := skill.NewRegistry()
	reg.Register(&skill.Skill{
		Name:          "commit",
		Description:   "Generate a git commit message",
		WhenToUse:     "Use when the user asks to write a commit message",
		Content:       "Commit content",
		UserInvocable: true,
	})
	reg.Register(&skill.Skill{
		Name:          "review",
		Description:   "Review code changes",
		Content:       "Review content",
		UserInvocable: true,
	})
	reg.Register(&skill.Skill{
		Name:          "hidden",
		Description:   "Hidden skill",
		Content:       "Hidden content",
		UserInvocable: false,
	})

	st := SkillTool{Registry: reg}
	prompt := st.SystemPrompt()

	if prompt == "" {
		t.Fatal("expected non-empty system prompt")
	}

	if !strings.Contains(prompt, "Execute a skill within the main conversation") {
		t.Error("expected fixed prompt header")
	}

	if !strings.Contains(prompt, "- commit:") {
		t.Error("expected skill listing to contain 'commit'")
	}

	if !strings.Contains(prompt, "- review:") {
		t.Error("expected skill listing to contain 'review'")
	}

	if strings.Contains(prompt, "hidden") {
		t.Error("non-user-invocable skill should not appear in listing")
	}

	if !strings.Contains(prompt, "BLOCKING REQUIREMENT") {
		t.Error("expected blocking requirement text")
	}
}

func TestSkillToolSystemPromptEmptyRegistry(t *testing.T) {
	st := SkillTool{Registry: skill.NewRegistry()}
	if st.SystemPrompt() != "" {
		t.Error("expected empty prompt for empty registry")
	}
}

func TestSkillToolSystemPromptNilRegistry(t *testing.T) {
	st := SkillTool{}
	if st.SystemPrompt() != "" {
		t.Error("expected empty prompt for nil registry")
	}
}

func TestSkillToolSystemPromptTruncatesLongDesc(t *testing.T) {
	reg := skill.NewRegistry()
	reg.Register(&skill.Skill{
		Name:          "long",
		Description:   strings.Repeat("a", 300),
		Content:       "content",
		UserInvocable: true,
	})

	st := SkillTool{Registry: reg}
	prompt := st.SystemPrompt()

	if !strings.Contains(prompt, "\u2026") {
		t.Error("expected long description to be truncated with ellipsis")
	}
}
