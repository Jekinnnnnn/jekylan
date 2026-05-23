package playbook

import (
	"context"
	"strings"
	"testing"

	"github.com/Jekinnnnnn/jekylan/internal/agent"
)

// mockSpawner captures the prompt passed to Spawn for inspection.
type mockSpawner struct {
	lastPrompt string
	lastType   string
}

func (m *mockSpawner) Spawn(ctx context.Context, agentType, prompt string) (string, error) {
	m.lastType = agentType
	m.lastPrompt = prompt
	return "agent-0", nil
}

func (m *mockSpawner) Wait(id string) *agent.RunningAgent {
	return &agent.RunningAgent{
		ID:     id,
		Status: agent.StatusCompleted,
		Result: "ok",
	}
}

func (m *mockSpawner) Kill(id string) bool { return true }

func TestExecutor_SpawnStep_ConfirmAppendsDirective(t *testing.T) {
	spawner := &mockSpawner{}
	exec := NewExecutor(spawner)

	step := Step{
		AgentType: "test",
		Prompt:    "calculate 1 + 1",
		Confirm:   true,
	}

	_, err := exec.spawnStep(context.Background(), step)
	if err != nil {
		t.Fatalf("spawnStep failed: %v", err)
	}

	if !strings.Contains(spawner.lastPrompt, "calculate 1 + 1") {
		t.Error("expected original prompt to be preserved")
	}
	if !strings.Contains(spawner.lastPrompt, "confirm tool") {
		t.Error("expected confirm directive to be appended")
	}
	if !strings.Contains(spawner.lastPrompt, "output the final result") {
		t.Error("expected final-result instruction to be appended")
	}
}

func TestExecutor_SpawnStep_NoConfirm_NoDirective(t *testing.T) {
	spawner := &mockSpawner{}
	exec := NewExecutor(spawner)

	step := Step{
		AgentType: "test",
		Prompt:    "calculate 1 + 1",
		Confirm:   false,
	}

	_, err := exec.spawnStep(context.Background(), step)
	if err != nil {
		t.Fatalf("spawnStep failed: %v", err)
	}

	if strings.Contains(spawner.lastPrompt, "confirm tool") {
		t.Error("expected no confirm directive when Confirm=false")
	}
}

func TestExecutor_ExecuteStep_StoresResult(t *testing.T) {
	spawner := &mockSpawner{}
	exec := NewExecutor(spawner)

	step := Step{
		AgentType: "test",
		Prompt:    "calculate",
		OutputVar: "result",
	}

	err := exec.executeStep(context.Background(), 0, 0, step)
	if err != nil {
		t.Fatalf("executeStep failed: %v", err)
	}
	if exec.vars["result"] != "ok" {
		t.Fatalf("expected var result=ok, got %q", exec.vars["result"])
	}
}
