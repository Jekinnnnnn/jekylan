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
	agents     map[string]*agent.RunningAgent
	killed     []string
}

func (m *mockSpawner) Spawn(ctx context.Context, agentType, prompt string) (string, error) {
	m.lastType = agentType
	m.lastPrompt = prompt
	return "agent-0", nil
}

func (m *mockSpawner) Wait(id string) *agent.RunningAgent {
	if m.agents != nil {
		if ra, ok := m.agents[id]; ok {
			return ra
		}
	}
	return &agent.RunningAgent{
		ID:     id,
		Status: agent.StatusCompleted,
		Result: "ok",
	}
}

func (m *mockSpawner) Kill(id string) bool {
	m.killed = append(m.killed, id)
	return true
}

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

func TestExecutor_ExecuteStep_StoresEmptyResult(t *testing.T) {
	spawner := &mockSpawner{
		agents: map[string]*agent.RunningAgent{
			"agent-0": {ID: "agent-0", Status: agent.StatusCompleted, Result: ""},
		},
	}
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
	if _, ok := exec.vars["result"]; !ok {
		t.Fatal("expected var result to be set even when empty")
	}
	if exec.vars["result"] != "" {
		t.Fatalf("expected var result=\"\", got %q", exec.vars["result"])
	}
}

func TestExecutor_ParallelPhase_PartialFailure(t *testing.T) {
	spawner := &mockSpawner{
		agents: map[string]*agent.RunningAgent{
			"agent-0": {ID: "agent-0", Status: agent.StatusCompleted, Result: "ok0"},
			"agent-1": {ID: "agent-1", Status: agent.StatusError, Error: "boom"},
			"agent-2": {ID: "agent-2", Status: agent.StatusKilled},
		},
	}
	exec := NewExecutor(spawner)

	plan := &ExecutionPlan{
		Phases: []Phase{{
			Parallel: true,
			Steps: []Step{
				{AgentType: "a", OutputVar: "out0"},
				{AgentType: "b"},
				{AgentType: "c", OutputVar: "out2"},
			},
		}},
	}

	// Override Spawn to return predictable IDs.
	spawnCount := 0
	spawnerWithIDs := &mockSpawnerWithIDs{
		mockSpawner: spawner,
		ids:         []string{"agent-0", "agent-1", "agent-2"},
		count:       &spawnCount,
	}
	exec.Spawner = spawnerWithIDs

	err := exec.executePlan(context.Background(), plan)
	if err == nil {
		t.Fatal("expected error for partial failure")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected error to contain 'boom', got %q", err.Error())
	}

	// Successful step should still have its output stored.
	if exec.vars["out0"] != "ok0" {
		t.Fatalf("expected out0=ok0, got %q", exec.vars["out0"])
	}

	// Killed step should not report a separate error.
	if strings.Contains(err.Error(), "killed") {
		t.Fatal("killed agent should not produce its own error message")
	}

	// Agent-2 should have been killed because agent-1 failed.
	found := false
	for _, id := range spawner.killed {
		if id == "agent-2" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected agent-2 to be killed after agent-1 failed, killed=%v", spawner.killed)
	}
}

// mockSpawnerWithIDs wraps a mockSpawner and returns pre-configured IDs.
type mockSpawnerWithIDs struct {
	*mockSpawner
	ids   []string
	count *int
}

func (m *mockSpawnerWithIDs) Spawn(ctx context.Context, agentType, prompt string) (string, error) {
	m.mockSpawner.Spawn(ctx, agentType, prompt)
	id := m.ids[*m.count]
	*m.count++
	return id, nil
}
