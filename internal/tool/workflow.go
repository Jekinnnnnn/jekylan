package tool

import (
	"context"
	"fmt"
)

// WorkflowCompleteTool is invoked by the assistant to explicitly mark a
// workflow skill as finished. The engine intercepts this tool use and uses it
// as the signal that a multi-turn workflow has completed.
type WorkflowCompleteTool struct{}

func (t WorkflowCompleteTool) Name() string        { return "workflow_complete" }
func (t WorkflowCompleteTool) SystemPrompt() string { return "" }

func (t WorkflowCompleteTool) Description() string {
	return `Mark the current workflow skill execution as complete.

Use this tool **only** when all steps of a workflow skill have been finished successfully.
Call it after the final user confirmation or after delivering the final deliverable.
Do not call this tool in the middle of a workflow.`
}

func (t WorkflowCompleteTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"skill_name": map[string]any{
				"type":        "string",
				"description": "Optional name of the workflow skill being completed. If omitted, the engine will infer it from the active workflow.",
			},
		},
	}
}

func (t WorkflowCompleteTool) Call(ctx context.Context, input map[string]any) (string, error) {
	skillName := ""
	if v, ok := input["skill_name"].(string); ok {
		skillName = v
	}
	if skillName != "" {
		return fmt.Sprintf("Workflow %q marked as complete.", skillName), nil
	}
	return "Workflow marked as complete.", nil
}
