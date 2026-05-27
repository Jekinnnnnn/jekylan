package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/Jekinnnnnn/jekylan/internal/llm"
	"github.com/Jekinnnnnn/jekylan/internal/message"
	"github.com/Jekinnnnnn/jekylan/internal/query"
	"github.com/Jekinnnnnn/jekylan/internal/tool"
)

// AgentTool implements the "agent" tool that spawns a sub-agent to handle a
// specific task. It is registered in the parent agent's tool registry.
type AgentTool struct {
	AgentRegistry  *Registry
	ParentTools    *tool.Registry
	Client         llm.Client
	Model          string
	ThinkingBudget int64
	// Transcript, when non-nil, supplies the parent conversation for fork mode.
	// If nil the sub-agent starts with a fresh message list.
	Transcript func() []message.Message
	// Coordinator manages background async agents. If nil, async agents run
	// detached without lifecycle tracking.
	Coordinator *Coordinator
	// QueryRunner overrides the default query.Query. Used for testing.
	QueryRunner QueryRunner
	// ClientFactory builds a fresh LLM client when a sub-agent's Definition
	// overrides the engine's default Model. Optional.
	ClientFactory func(model string) (llm.Client, error)
	// CoordinatorMode, when true, forces all agent spawns to be async workers.
	CoordinatorMode bool
}

func (t AgentTool) Name() string        { return "agent" }
func (t AgentTool) Description() string { return "Launch a new agent to handle a specific task." }
func (t AgentTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"description": map[string]any{
				"type":        "string",
				"description": "Short 3-5 word description of the task",
			},
			"prompt": map[string]any{
				"type":        "string",
				"description": "The full task prompt for the sub-agent",
			},
			"agent_type": map[string]any{
				"type":        "string",
				"description": "Agent type to use (default: general-purpose)",
			},
			"fork": map[string]any{
				"type":        "boolean",
				"description": "Fork the agent with parent conversation context (inherits message history)",
			},
		},
		"required": []string{"description", "prompt"},
	}
}

func (t AgentTool) Call(ctx context.Context, input map[string]any) (string, error) {
	if t.Coordinator != nil && t.Coordinator.IsPlaybookRunning() {
		return "", fmt.Errorf("agent tool is disabled while a playbook is running")
	}

	description, _ := input["description"].(string)
	prompt, _ := input["prompt"].(string)
	agentType, _ := input["agent_type"].(string)
	fork, _ := input["fork"].(bool)

	if strings.TrimSpace(prompt) == "" {
		return "", fmt.Errorf("prompt is required")
	}

	// Coordinator mode overrides.
	if t.CoordinatorMode {
		if agentType == "" {
			agentType = "worker" // default to worker, not general-purpose
		}
	} else {
		if agentType == "" {
			agentType = "general-purpose"
		}
	}

	def := t.AgentRegistry.Get(agentType)
	if def == nil {
		return "", fmt.Errorf("unknown agent type: %s", agentType)
	}

	qr := t.QueryRunner
	if qr == nil {
		qr = query.Query
	}
	runnerOpts := RunnerOptions{
		Definition:     def,
		Transcript:     t.Transcript,
		Tools:          t.ParentTools,
		Client:         t.Client,
		Model:          t.Model,
		ThinkingBudget: t.ThinkingBudget,
		QueryRunner:    qr,
		ClientFactory:  t.ClientFactory,
		Fork:           fork,
	}

	if t.Coordinator != nil {
		id := t.Coordinator.Spawn(context.Background(), def, prompt, runnerOpts)
		return fmt.Sprintf("Agent '%s' started in background (id: %s).", description, id), nil
	}
	// Fallback if no coordinator is wired.
	go func() {
		bgCtx := context.Background()
		for range NewRunner(runnerOpts).Run(bgCtx, prompt) {
			// Events discarded — no lifecycle tracking available.
		}
	}()
	return fmt.Sprintf("Agent '%s' started in background.", description), nil
}

func (t AgentTool) SystemPrompt() string {
	if t.AgentRegistry == nil || len(t.AgentRegistry.List()) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Available agent types:\n")
	for _, d := range t.AgentRegistry.List() {
		fmt.Fprintf(&b, "- %s: %s\n", d.Name, d.Description)
	}
	return b.String()
}
