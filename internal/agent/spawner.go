package agent

import (
	"context"
	"fmt"

	"github.com/Jekinnnnnn/jekylan/internal/llm"
	"github.com/Jekinnnnnn/jekylan/internal/query"
	"github.com/Jekinnnnnn/jekylan/internal/tool"
)

// AgentSpawner is the minimal interface required by playbook.Executor to
// spawn and lifecycle-manage sub-agents. It decouples the executor from the
// full agent/llm/tool infrastructure.
type AgentSpawner interface {
	Spawn(ctx context.Context, agentType, prompt string) (id string, err error)
	Wait(id string) *RunningAgent
	Kill(id string) bool
}

// Spawner implements AgentSpawner using the production agent stack.
type Spawner struct {
	Coord          *Coordinator
	AgentReg       *Registry
	ParentTools    *tool.Registry
	Client         llm.Client
	Model          string
	ThinkingBudget int64
	ClientFactory  func(model string) (llm.Client, error)
	QueryRunner    QueryRunner
}

// Spawn looks up the agent definition, builds RunnerOptions, and spawns the agent.
func (s *Spawner) Spawn(ctx context.Context, agentType, prompt string) (string, error) {
	def := s.AgentReg.Get(agentType)
	if def == nil && agentType != "" {
		return "", fmt.Errorf("unknown agent type: %s", agentType)
	}

	qr := s.QueryRunner
	if qr == nil {
		qr = query.Query
	}
	runnerOpts := RunnerOptions{
		Definition:     def,
		Tools:          s.ParentTools,
		Client:         s.Client,
		Model:          s.Model,
		ThinkingBudget: s.ThinkingBudget,
		ClientFactory:  s.ClientFactory,
		QueryRunner:    qr,
	}

	id := s.Coord.Spawn(ctx, def, prompt, runnerOpts)
	return id, nil
}

// Wait blocks until the agent with the given ID completes and returns its result.
func (s *Spawner) Wait(id string) *RunningAgent {
	return s.Coord.Wait(id)
}

// Kill terminates a running agent.
func (s *Spawner) Kill(id string) bool {
	return s.Coord.Kill(id)
}
