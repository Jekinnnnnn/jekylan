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

func TestAgentTool_Interface(t *testing.T) {
	var _ tool.Tool = AgentTool{}
}

func TestAgentTool_Call(t *testing.T) {
	mq := &mockQueryRunner{
		block: true,
		events: []query.Event{
			{Type: "result", Result: query.Result{Success: true, Text: "  result text  "}},
		},
	}
	at := AgentTool{
		AgentRegistry: NewBuiltinRegistry(),
		ParentTools:   tool.NewRegistry(fakeTool{name: "grep"}),
		QueryRunner:   mq.run,
		Model:         "test-model",
	}

	result, err := at.Call(context.Background(), map[string]any{
		"description": "test task",
		"prompt":      "do something",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "background") {
		t.Fatalf("expected background placeholder, got: %s", result)
	}

	// Wait for async runner to start and verify params.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if mq.params() != nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if mq.params() == nil {
		t.Fatal("expected query to be called")
	}
	if mq.params().Model != "test-model" {
		t.Fatalf("expected model 'test-model', got: %+v", mq.params())
	}
}

func TestAgentTool_Async(t *testing.T) {
	mq := &mockQueryRunner{
		block: true,
		events: []query.Event{
			{Type: "result", Result: query.Result{Success: true, Text: "async result"}},
		},
	}
	at := AgentTool{
		AgentRegistry: NewBuiltinRegistry(),
		ParentTools:   tool.NewRegistry(),
		QueryRunner:   mq.run,
	}

	result, err := at.Call(context.Background(), map[string]any{
		"description": "bg task",
		"prompt":      "do something",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "background") {
		t.Fatalf("expected background placeholder, got: %s", result)
	}

	// The async goroutine should eventually complete.
	done := make(chan struct{})
	go func() {
		for {
			if mq.params() != nil {
				close(done)
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("async runner was not started")
	}
}

func TestAgentTool_UnknownAgentType(t *testing.T) {
	at := AgentTool{
		AgentRegistry: NewBuiltinRegistry(),
	}
	_, err := at.Call(context.Background(), map[string]any{
		"description": "test",
		"prompt":      "do something",
		"agent_type":  "nonexistent",
	})
	if err == nil {
		t.Fatal("expected error for unknown agent type")
	}
	if !strings.Contains(err.Error(), "unknown agent type") {
		t.Fatalf("expected 'unknown agent type' error, got: %v", err)
	}
}

func TestAgentTool_EmptyPrompt(t *testing.T) {
	at := AgentTool{
		AgentRegistry: NewBuiltinRegistry(),
	}
	_, err := at.Call(context.Background(), map[string]any{
		"description": "test",
		"prompt":      "   ",
	})
	if err == nil {
		t.Fatal("expected error for empty prompt")
	}
}

func TestAgentTool_DefaultAgentType(t *testing.T) {
	mq := &mockQueryRunner{
		block: true,
		events: []query.Event{
			{Type: "result", Result: query.Result{Success: true, Text: "ok"}},
		},
	}
	at := AgentTool{
		AgentRegistry: NewBuiltinRegistry(),
		ParentTools:   tool.NewRegistry(),
		QueryRunner:   mq.run,
	}

	_, err := at.Call(context.Background(), map[string]any{
		"description": "test",
		"prompt":      "do something",
		// no agent_type
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if mq.params() != nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if mq.params() == nil {
		t.Fatal("expected query to be called")
	}
	// Default is general-purpose which allows all tools.
}

func TestAgentTool_SystemPrompt(t *testing.T) {
	at := AgentTool{AgentRegistry: NewBuiltinRegistry()}
	sp := at.SystemPrompt()
	if !strings.Contains(sp, "general-purpose") {
		t.Fatalf("expected general-purpose in system prompt, got: %s", sp)
	}
	if !strings.Contains(sp, "worker") {
		t.Fatalf("expected worker in system prompt, got: %s", sp)
	}
}

func TestAgentTool_Fork(t *testing.T) {
	parentTranscript := func() []message.Message {
		userMsg := message.Message{Role: message.RoleUser}
		userMsg.AddText("parent task")

		assistantMsg := message.Message{Role: message.RoleAssistant}
		assistantMsg.AddText("I'll delegate")
		assistantMsg.AddToolUse("tu-agent", "agent", map[string]any{"prompt": "sub"})

		return []message.Message{userMsg, assistantMsg}
	}

	mq := &mockQueryRunner{
		block: true,
		events: []query.Event{
			{Type: "result", Result: query.Result{Success: true, Text: "forked result"}},
		},
	}
	at := AgentTool{
		AgentRegistry: NewBuiltinRegistry(),
		ParentTools:   tool.NewRegistry(),
		QueryRunner:   mq.run,
		Transcript:    parentTranscript,
	}

	result, err := at.Call(context.Background(), map[string]any{
		"description": "review code",
		"prompt":      "Review this code for bugs.",
		"fork":        true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "background") {
		t.Fatalf("expected background placeholder, got: %s", result)
	}

	// Verify fork mode was used (messages should include parent history).
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if mq.params() != nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if mq.params() == nil {
		t.Fatal("expected query params")
	}
	msgs := mq.params().Messages
	if len(msgs) != 4 {
		t.Fatalf("expected 4 fork messages (user+assistant+tool_result+directive), got %d", len(msgs))
	}
	// Verify synthetic tool_result was appended.
	tr := msgs[2].ToolResults()
	if len(tr) != 1 || tr[0].ToolUseID != "tu-agent" {
		t.Fatalf("expected synthetic tool_result for tu-agent, got: %+v", tr)
	}
}

func TestAgentTool_CoordinatorMode(t *testing.T) {
	mq := &mockQueryRunner{
		events: []query.Event{
			{Type: "result", Result: query.Result{Success: true, Text: "ok"}},
		},
	}
	at := AgentTool{
		AgentRegistry:   NewBuiltinRegistry(),
		ParentTools:     tool.NewRegistry(),
		QueryRunner:     mq.run,
		CoordinatorMode: true,
	}

	// All agent calls are async; coordinator mode additionally defaults to worker.
	result, err := at.Call(context.Background(), map[string]any{
		"description": "test task",
		"prompt":      "do something",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "background") {
		t.Fatalf("expected forced async in coordinator mode, got: %s", result)
	}

	// Coordinator mode defaults to worker agent type when none is specified.
	// (Without coordinator mode, default would be general-purpose.)
	// We verify by checking the tool result doesn't error.
	mq2 := &mockQueryRunner{
		block: true,
		events: []query.Event{
			{Type: "result", Result: query.Result{Success: true, Text: "ok"}},
		},
	}
	at2 := AgentTool{
		AgentRegistry:   NewBuiltinRegistry(),
		ParentTools:     tool.NewRegistry(),
		QueryRunner:     mq2.run,
		Model:           "parent-model",
		CoordinatorMode: true,
	}
	result2, err := at2.Call(context.Background(), map[string]any{
		"description": "test",
		"prompt":      "do it",
		"model":       "override-model", // should be ignored in coordinator mode
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result2, "background") {
		t.Fatalf("expected async launch, got: %s", result2)
	}
}
