package tool

import (
	"context"
	"fmt"
)

// ConfirmBlockTool is a dedicated blocking tool for sub-agents. When called,
// it pauses the sub-agent, sends a summary to the coordinator, and blocks
// until the user confirms (via "确认" or /confirm). After confirmation the
// tool returns so the sub-agent can continue.
type ConfirmBlockTool struct{}

func (t ConfirmBlockTool) Name() string        { return "confirm" }
func (t ConfirmBlockTool) SystemPrompt() string { return "" }

func (t ConfirmBlockTool) Description() string {
	return `Pause execution and request user confirmation before proceeding.

Call this tool when:
- You have completed a significant chunk of work and want the user to review before continuing
- A skill or workflow requires per-step user confirmation
- You need the user to verify intermediate results before the next phase

Provide a clear summary of what was accomplished and what will happen next.`
}

func (t ConfirmBlockTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"summary": map[string]any{
				"type":        "string",
				"description": "A summary of what was accomplished and what will happen after user confirmation.",
			},
		},
		"required": []string{"summary"},
	}
}

// Call returns immediately. The actual blocking is handled by the risky-tool
// confirmation pipeline: isRiskyTool("confirm") triggers ConfirmTool which
// blocks until the user approves.
func (t ConfirmBlockTool) Call(ctx context.Context, input map[string]any) (string, error) {
	summary, _ := input["summary"].(string)
	if summary != "" {
		return fmt.Sprintf("User confirmed. Resuming execution.\n\nConfirmed summary: %s", summary), nil
	}
	return "User confirmed. Resuming execution.", nil
}
