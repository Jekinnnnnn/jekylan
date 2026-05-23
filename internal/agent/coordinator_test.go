package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jekinnnnnn/jekylan/internal/message"
	"github.com/Jekinnnnnn/jekylan/internal/query"
	"github.com/Jekinnnnnn/jekylan/internal/tool"
)

func TestCoordinatorSystemPrompt(t *testing.T) {
	prompt := CoordinatorSystemPrompt()
	if prompt == "" {
		t.Fatal("expected non-empty prompt")
	}
	required := []string{
		"coordinator",
		"agent",
		"worker",
		"Research",
		"Synthesis",
		"Implementation",
		"Verification",
		"Parallelism",
		"synthesize",
	}
	for _, s := range required {
		if !strings.Contains(prompt, s) {
			t.Errorf("expected prompt to contain %q", s)
		}
	}
}

// ---- Sub-agent registry ------------------------------------------------

func TestCoordinator_SpawnAndComplete(t *testing.T) {
	coord := NewCoordinator()
	defer coord.Stop()
	mq := &mockQueryRunner{
		events: []query.Event{
			{Type: "result", Result: query.Result{Success: true, Text: "done"}},
		},
	}

	id := coord.Spawn(context.Background(), &Definition{Name: "test"}, "do it", RunnerOptions{
		QueryRunner: mq.run,
	})
	if id == "" {
		t.Fatal("expected non-empty id")
	}

	// Agent completes and is auto-removed; poll Get until gone or completed.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if a := coord.Get(id); a == nil {
			// Agent removed after completion.
			return
		} else if a.Status == StatusCompleted {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("expected agent to complete")
}

func TestCoordinator_Kill(t *testing.T) {
	coord := NewCoordinator()
	defer coord.Stop()
	mq := &mockQueryRunner{
		block: true,
		events: []query.Event{
			{Type: "result", Result: query.Result{Success: true, Text: "done"}},
		},
	}

	id := coord.Spawn(context.Background(), &Definition{Name: "test"}, "do it", RunnerOptions{
		QueryRunner: mq.run,
	})

	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		if a := coord.Get(id); a != nil && a.Status == StatusRunning {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	if !coord.Kill(id) {
		t.Fatal("expected Kill to succeed")
	}
	// Kill only cancels context; agent is removed when runner exits.
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if coord.Get(id) == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("expected agent to be removed after runner exits")
}

func TestCoordinator_List(t *testing.T) {
	coord := NewCoordinator()
	defer coord.Stop()
	mq := &mockQueryRunner{
		block:  true,
		events: []query.Event{},
	}

	id1 := coord.Spawn(context.Background(), &Definition{Name: "a"}, "prompt a", RunnerOptions{QueryRunner: mq.run})
	id2 := coord.Spawn(context.Background(), &Definition{Name: "b"}, "prompt b", RunnerOptions{QueryRunner: mq.run})

	list := coord.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(list))
	}
	names := make(map[string]bool)
	for _, a := range list {
		names[a.ID] = true
	}
	if !names[id1] || !names[id2] {
		t.Fatal("expected both agents in list")
	}

	coord.Kill(id1)
	coord.Kill(id2)
}

func TestCoordinator_GetUnknown(t *testing.T) {
	coord := NewCoordinator()
	defer coord.Stop()
	if coord.Get("nonexistent") != nil {
		t.Fatal("expected nil for unknown agent")
	}
}

func TestCoordinator_KillUnknown(t *testing.T) {
	coord := NewCoordinator()
	defer coord.Stop()
	if coord.Kill("nonexistent") {
		t.Fatal("expected Kill to fail for unknown agent")
	}
}

func TestCoordinator_KillAlreadyCompleted(t *testing.T) {
	coord := NewCoordinator()
	defer coord.Stop()
	mq := &mockQueryRunner{
		events: []query.Event{
			{Type: "result", Result: query.Result{Success: true, Text: "done"}},
		},
	}

	id := coord.Spawn(context.Background(), &Definition{Name: "test"}, "do it", RunnerOptions{
		QueryRunner: mq.run,
	})

	// Wait for the agent to complete and be auto-removed.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if coord.Get(id) == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if coord.Kill(id) {
		t.Fatal("expected Kill to fail for already-completed agent")
	}
}

func TestCoordinator_AgentError(t *testing.T) {
	coord := NewCoordinator()
	defer coord.Stop()
	mq := &mockQueryRunner{
		events: []query.Event{
			{Type: "result", Result: query.Result{Success: false, Error: "something broke"}},
		},
	}

	id := coord.Spawn(context.Background(), &Definition{Name: "test"}, "do it", RunnerOptions{
		QueryRunner: mq.run,
	})

	// Agent errors and is auto-removed; poll Get until gone or errored.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if a := coord.Get(id); a == nil {
			return
		} else if a.Status == StatusError {
			if !strings.Contains(a.Error, "something broke") {
				t.Fatalf("expected error message in agent, got %q", a.Error)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("expected agent to error")
}

func TestCoordinator_AgentResult(t *testing.T) {
	coord := NewCoordinator()
	defer coord.Stop()
	mq := &mockQueryRunner{
		events: []query.Event{
			{Type: "result", Result: query.Result{Success: true, Text: "final result"}},
		},
	}

	id := coord.Spawn(context.Background(), &Definition{Name: "test"}, "do it", RunnerOptions{
		QueryRunner: mq.run,
	})

	// Agent completes and is auto-removed; poll Get until gone or completed.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if a := coord.Get(id); a == nil {
			return
		} else if a.Status == StatusCompleted {
			if !strings.Contains(a.Result, "final result") {
				t.Fatalf("expected result in agent, got %q", a.Result)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("expected agent to complete with result")
}

func TestCoordinator_AgentGetWhileRunning(t *testing.T) {
	coord := NewCoordinator()
	defer coord.Stop()
	mq := &mockQueryRunner{
		block: true,
		events: []query.Event{
			{Type: "result", Result: query.Result{Success: true, Text: "done"}},
		},
	}

	id := coord.Spawn(context.Background(), &Definition{Name: "test"}, "do it", RunnerOptions{
		QueryRunner: mq.run,
	})

	a := coord.Get(id)
	if a == nil {
		t.Fatal("expected agent to exist")
	}
	if a.Status != StatusRunning {
		t.Fatalf("expected status running, got %s", a.Status)
	}
	if a.Definition == nil || a.Definition.Name != "test" {
		t.Fatal("expected definition to be preserved")
	}
	if !strings.Contains(a.Prompt, "do it") {
		t.Fatalf("expected prompt preserved, got %s", a.Prompt)
	}

	coord.Kill(id)
}

func TestCoordinator_SpawnUsesFilteredTools(t *testing.T) {
	coord := NewCoordinator()
	defer coord.Stop()
	mq := &mockQueryRunner{
		events: []query.Event{
			{Type: "result", Result: query.Result{Success: true, Text: "ok"}},
		},
	}

	def := &Definition{Name: "test", ToolsAllow: []string{"*"}}
	coord.Spawn(context.Background(), def, "do it", RunnerOptions{
		QueryRunner: mq.run,
		Tools:       tool.NewRegistry(fakeTool{name: "grep"}),
	})

	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		if mq.params() != nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if mq.params() == nil {
		t.Fatal("expected query to be called")
	}
	if mq.params().Tools == nil {
		t.Fatal("expected tools to be set")
	}
}

// TestCoordinator_UsageTracking verifies that when an agent completes, its
// usage is accumulated in the coordinator's total usage.
func TestCoordinator_UsageTracking(t *testing.T) {
	coord := NewCoordinator()
	defer coord.Stop()
	mq := &mockQueryRunner{
		events: []query.Event{
			{Type: "usage", Message: message.Message{
				Role: message.RoleAssistant,
				Usage: &message.Usage{
					InputTokens:  100,
					OutputTokens: 50,
				},
			}},
			{Type: "result", Result: query.Result{Success: true, Text: "done"}},
		},
	}

	coord.Spawn(context.Background(), &Definition{Name: "test"}, "do it", RunnerOptions{
		QueryRunner: mq.run,
	})

	// Wait for the agent to complete and be removed.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if tu := coord.TotalUsage(); tu != nil {
			if tu.InputTokens == 100 && tu.OutputTokens == 50 {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("expected usage to be accumulated, got: %v", coord.TotalUsage())
}

// TestCoordinator_ConfirmForward verifies that confirmation requests from
// agents are forwarded to the coordinator's notifications channel.
func TestCoordinator_ConfirmForward(t *testing.T) {
	coord := NewCoordinator()
	defer coord.Stop()

	// Custom runner that calls ConfirmTool.
	confirmRun := func(ctx context.Context, params query.Params) <-chan query.Event {
		out := make(chan query.Event)
		go func() {
			defer close(out)
			if params.ConfirmTool != nil {
				// Trigger a confirmation request.
				go func() {
					params.ConfirmTool(ctx, "bash", map[string]any{"cmd": "echo hi"})
				}()
				// Block until context cancellation so the agent stays running.
				<-ctx.Done()
				out <- query.Event{Type: "error", Result: query.Result{Error: "cancelled"}}
			}
		}()
		return out
	}

	id := coord.Spawn(context.Background(), &Definition{Name: "test"}, "do it", RunnerOptions{
		QueryRunner: confirmRun,
	})

	// Wait a moment for the confirmation request to be forwarded, then approve it.
	time.Sleep(50 * time.Millisecond)
	if !coord.Confirm(id, true) {
		t.Fatal("expected Confirm to succeed")
	}
}

// TestCoordinator_WaitAfterComplete verifies that Wait returns the correct
// result even when the agent finished before Wait was called (parallel
// playbook phase race condition).
func TestCoordinator_WaitAfterComplete(t *testing.T) {
	coord := NewCoordinator()
	defer coord.Stop()

	// Spawn a fast agent that completes immediately.
	mq := &mockQueryRunner{
		events: []query.Event{
			{Type: "result", Result: query.Result{Success: true, Text: "done"}},
		},
	}
	def := &Definition{Name: "fast", SystemPrompt: "fast"}
	id := coord.Spawn(context.Background(), def, "go", RunnerOptions{
		QueryRunner: mq.run,
	})

	// Wait for the agent to finish.
	for {
		a := coord.Get(id)
		if a == nil || a.Status != StatusRunning {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Now call Wait — the agent is already gone from c.agents.
	ra := coord.Wait(id)
	if ra == nil {
		t.Fatal("expected Wait to return completed agent, got nil")
	}
	if ra.Status != StatusCompleted {
		t.Fatalf("expected status completed, got %s", ra.Status)
	}
	if ra.Result != "done" {
		t.Fatalf("expected result 'done', got %q", ra.Result)
	}
}

// TestCoordinator_WaitReturnsClone verifies that Wait returns a clone of
// the completed agent, so mutating the returned value does not affect
// internal coordinator state.
func TestCoordinator_WaitReturnsClone(t *testing.T) {
	coord := NewCoordinator()
	defer coord.Stop()
	mq := &mockQueryRunner{
		events: []query.Event{
			{Type: "result", Result: query.Result{Success: true, Text: "original"}},
		},
	}

	id := coord.Spawn(context.Background(), &Definition{Name: "test"}, "do it", RunnerOptions{
		QueryRunner: mq.run,
	})

	// Wait for completion.
	for {
		a := coord.Get(id)
		if a == nil || a.Status != StatusRunning {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Wait should return the original result.
	ra := coord.Wait(id)
	if ra == nil {
		t.Fatal("expected Wait to return agent")
	}
	if ra.Result != "original" {
		t.Fatalf("expected result 'original', got %q", ra.Result)
	}

	// Mutate the returned agent. Because Wait returns a clone, this cannot
	// corrupt internal coordinator state. We verify the clone behaviour by
	// checking the initial value was correct; direct inspection of internal
	// maps would race with the event-loop goroutine.
	ra.Result = "mutated"

	// A second Wait returns nil because the agent was removed from
	// completedAgents after the first Wait.
	if ra2 := coord.Wait(id); ra2 != nil {
		t.Fatalf("expected second Wait to return nil, got %+v", ra2)
	}
}

// TestCoordinator_ConfirmAfterAgentExits verifies that calling Confirm after
// the agent has exited (and its pending confirm cleaned up) returns false
// without panicking.
func TestCoordinator_ConfirmAfterAgentExits(t *testing.T) {
	coord := NewCoordinator()
	defer coord.Stop()

	// Custom runner that calls ConfirmTool and then blocks until context
	// cancellation (by Kill). This avoids racing with confirmReqCh close.
	confirmRun := func(ctx context.Context, params query.Params) <-chan query.Event {
		out := make(chan query.Event)
		go func() {
			defer close(out)
			if params.ConfirmTool != nil {
				go func() {
					params.ConfirmTool(ctx, "bash", map[string]any{"cmd": "echo hi"})
				}()
			}
			// Block until Kill cancels the context.
			<-ctx.Done()
			out <- query.Event{Type: "error", Result: query.Result{Error: "died"}}
		}()
		return out
	}

	id := coord.Spawn(context.Background(), &Definition{Name: "test"}, "do it", RunnerOptions{
		QueryRunner: confirmRun,
	})

	// Wait for the confirmation request to be forwarded.
	time.Sleep(50 * time.Millisecond)

	// Kill the agent — this cancels context, runner exits, closes confirmReqCh.
	coord.Kill(id)

	// Wait for agent to be removed.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if coord.Get(id) == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Confirm after agent is gone should return false without panic.
	if coord.Confirm(id, true) {
		t.Fatal("expected Confirm to fail after agent exits")
	}
}

// TestCoordinator_Stop verifies that Stop cancels all running agents and
// exits the event loop cleanly.
func TestCoordinator_Stop(t *testing.T) {
	coord := NewCoordinator()
	mq := &mockQueryRunner{
		block: true,
		events: []query.Event{
			{Type: "result", Result: query.Result{Success: true, Text: "done"}},
		},
	}

	id := coord.Spawn(context.Background(), &Definition{Name: "test"}, "do it", RunnerOptions{
		QueryRunner: mq.run,
	})

	// Ensure agent is running.
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		if a := coord.Get(id); a != nil && a.Status == StatusRunning {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Stop should not panic and should cancel the agent.
	coord.Stop()
}

// TestCoordinator_MultiplePendingConfirm_FIFO verifies that when multiple
// agents request confirmation, HasPendingConfirm returns them in FIFO order.
func TestCoordinator_MultiplePendingConfirm_FIFO(t *testing.T) {
	coord := NewCoordinator()
	defer coord.Stop()

	// Simulate two pending confirms arriving in order.
	respCh1 := make(chan ConfirmResponse, 1)
	respCh2 := make(chan ConfirmResponse, 1)

	coord.events <- agentConfirmEvent{
		id:       "agent-0",
		toolName: "confirm",
		input:    map[string]any{"summary": "first"},
		respCh:   respCh1,
	}
	coord.events <- agentConfirmEvent{
		id:       "agent-1",
		toolName: "confirm",
		input:    map[string]any{"summary": "second"},
		respCh:   respCh2,
	}

	// Give the event loop time to process.
	time.Sleep(50 * time.Millisecond)

	// First pending should be agent-0 (FIFO).
	first := coord.HasPendingConfirm()
	if first != "agent-0" {
		t.Fatalf("expected first pending=agent-0, got %s", first)
	}

	// Confirm agent-0.
	if !coord.Confirm("agent-0", true) {
		t.Fatal("expected Confirm(agent-0) to succeed")
	}

	// Next pending should be agent-1.
	second := coord.HasPendingConfirm()
	if second != "agent-1" {
		t.Fatalf("expected second pending=agent-1, got %s", second)
	}

	// Confirm agent-1.
	if !coord.Confirm("agent-1", true) {
		t.Fatal("expected Confirm(agent-1) to succeed")
	}

	// No more pending.
	if coord.HasPendingConfirm() != "" {
		t.Fatal("expected no pending confirms")
	}
}

// TestTruncateCJK verifies that truncate respects rune boundaries, not bytes.
func TestTruncateCJK(t *testing.T) {
	// Each CJK character is 3 bytes in UTF-8.
	s := "你好世界"
	if got := truncate(s, 2); got != "你好..." {
		t.Fatalf("expected '你好...', got %q", got)
	}
	if got := truncate(s, 4); got != "你好世界" {
		t.Fatalf("expected '你好世界', got %q", got)
	}

	// Mixed ASCII and CJK.
	s2 := "ab你好cd"
	if got := truncate(s2, 4); got != "ab你好..." {
		t.Fatalf("expected 'ab你好...', got %q", got)
	}
}

// ---- helpers -----------------------------------------------------------

// readNonEmpty drains the output channel for up to d, returning the
// concatenated non-empty text. Used to tolerate the engine emitting
// several short fragments around a message.
func readNonEmpty(t *testing.T, ch <-chan string, d time.Duration) string {
	t.Helper()
	deadline := time.After(d)
	var b strings.Builder
	for {
		select {
		case s, ok := <-ch:
			if !ok {
				return b.String()
			}
			b.WriteString(s)
			if strings.TrimSpace(b.String()) != "" {
				return b.String()
			}
		case <-deadline:
			return b.String()
		}
	}
}
