package tool

import (
	"context"
	"fmt"
	"strings"

	"github.com/Jekinnnnnn/jekylan/internal/skill"
)

// SkillTool invokes a skill by name and returns its content.
type SkillTool struct {
	Registry *skill.Registry
}

func (t SkillTool) Name() string { return "skill" }
func (t SkillTool) Description() string {
	return "Invoke a skill by name to inject its prompt into the conversation."
}
func (t SkillTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"skill": map[string]any{
				"type":        "string",
				"description": "The skill name. E.g., \"commit\", \"review\", or \"pdf\"",
			},
			"args": map[string]any{
				"type":        "string",
				"description": "Optional arguments for the skill",
			},
		},
		"required": []string{"skill"},
	}
}

func (t SkillTool) Call(ctx context.Context, input map[string]any) (string, error) {
	skillName, ok := input["skill"].(string)
	if !ok || strings.TrimSpace(skillName) == "" {
		return "", fmt.Errorf("missing or invalid 'skill' parameter")
	}
	skillName = strings.TrimSpace(skillName)

	// Strip leading slash if present
	skillName = strings.TrimPrefix(skillName, "/")

	if t.Registry == nil {
		return "", fmt.Errorf("skill registry not available")
	}

	s := t.Registry.Find(skillName)
	if s == nil {
		return "", fmt.Errorf("unknown skill: %s", skillName)
	}

	if s.DisableModelInvocation {
		return "", fmt.Errorf("skill %s cannot be invoked via the skill tool (disable-model-invocation)", skillName)
	}

	var args string
	if v, ok := input["args"].(string); ok {
		args = v
	}

	return s.RenderContent(args), nil
}

// SystemPrompt returns the SkillTool system prompt including the fixed
// instruction text and a listing of available skills.
func (t SkillTool) SystemPrompt() string {
	if t.Registry == nil || len(t.Registry.All()) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(`Execute a skill within the main conversation

When users ask you to perform tasks, check if any of the available skills match. Skills provide specialized capabilities and domain knowledge.

When users reference a "slash command" or "/<something>" (e.g., "/commit", "/review-pr"), they are referring to a skill. Use this tool to invoke it.

How to invoke:
- Use this tool with the skill name and optional arguments
- Examples:
  - ` + "`" + `skill: "pdf"` + "`" + ` - invoke the pdf skill
  - ` + "`" + `skill: "commit", args: "-m 'Fix bug'"` + "`" + ` - invoke with arguments
  - ` + "`" + `skill: "review-pr", args: "123"` + "`" + ` - invoke with arguments
  - ` + "`" + `skill: "ms-office-suite:pdf"` + "`" + ` - invoke using fully qualified name

Important:
- Available skills are listed below
- When a skill matches the user's request, this is a BLOCKING REQUIREMENT: invoke the relevant Skill tool BEFORE generating any other response about the task
- NEVER mention a skill without actually calling this tool
- Do not invoke a skill that is already running
- Do not use this tool for built-in CLI commands (like /help, /clear, etc.)
- If you see a <command-name> tag in the current conversation turn, the skill has ALREADY been loaded - follow the instructions directly instead of calling this tool again

Available skills:
`)

	for _, s := range t.Registry.All() {
		if !s.UserInvocable {
			continue
		}
		desc := s.Description
		if s.WhenToUse != "" {
			desc = desc + " - " + s.WhenToUse
		}
		if len(desc) > 250 {
			desc = desc[:249] + "\u2026"
		}
		fmt.Fprintf(&sb, "- %s: %s\n", s.Name, desc)
	}

	return sb.String()
}
