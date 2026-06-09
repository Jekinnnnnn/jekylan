package playbooktool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Jekinnnnnn/jekylan/internal/agent"
	"github.com/Jekinnnnnn/jekylan/internal/playbook"
)

// PlaybookTool implements the "playbook" tool that executes a structured
// multi-agent workflow defined in a playbook markdown file.
type PlaybookTool struct {
	PlaybookRegistry *playbook.Registry
	Spawner          agent.AgentSpawner
	LifecycleHook    func(running bool)
}

func (t PlaybookTool) Name() string        { return "playbook" }
func (t PlaybookTool) Description() string { return "Execute a structured multi-agent workflow playbook." }
func (t PlaybookTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{
				"type":        "string",
				"description": "Name of the playbook to execute",
			},
			"args": map[string]any{
				"type":        "string",
				"description": "Initial input data (assigned to variable 'input')",
			},
		},
		"required": []string{"name"},
	}
}

func (t PlaybookTool) Call(ctx context.Context, input map[string]any) (string, error) {
	name, _ := input["name"].(string)
	args, _ := input["args"].(string)

	if t.PlaybookRegistry == nil {
		return "", fmt.Errorf("playbook registry not configured")
	}

	p := t.PlaybookRegistry.Find(name)
	if p == nil {
		return "", fmt.Errorf("playbook not found: %s", name)
	}

	plan, err := playbook.ParsePlan(p.Content)
	if err != nil {
		return "", fmt.Errorf("parse playbook %q: %w", name, err)
	}

	executor := playbook.NewExecutor(t.Spawner, playbook.WithLifecycleHook(t.LifecycleHook))

	if args != "" {
		// Try JSON object first for multi-variable injection.
		var obj map[string]string
		if err := json.Unmarshal([]byte(args), &obj); err == nil {
			for k, v := range obj {
				executor.SetVar(k, v)
			}
		} else {
			// Fallback: treat as single "input" variable.
			executor.SetVar("input", args)
		}
	}

	vars, err := executor.Execute(ctx, plan)
	if err != nil {
		return "", fmt.Errorf("playbook %q failed: %w", name, err)
	}

	var result strings.Builder
	result.WriteString(fmt.Sprintf("Playbook %q completed successfully.\n", name))
	for k, v := range vars {
		result.WriteString(fmt.Sprintf("- %s: %s\n", k, truncateStr(v, 200)))
	}
	return result.String(), nil
}

func (t PlaybookTool) SystemPrompt() string {
	if t.PlaybookRegistry == nil || len(t.PlaybookRegistry.All()) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Available playbooks:\n")
	for _, p := range t.PlaybookRegistry.All() {
		fmt.Fprintf(&b, "- %s: %s", p.Name, p.Description)
		if p.WhenToUse != "" {
			fmt.Fprintf(&b, " (trigger: %s)", p.WhenToUse)
		}
		b.WriteString("\n")
	}
	b.WriteString("\nWhen a user request matches a playbook's purpose, invoke the playbook tool instead of manually orchestrating agents.")
	return b.String()
}

func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
