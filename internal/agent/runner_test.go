package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Jekinnnnnn/jekylan/internal/llm"
	"github.com/Jekinnnnnn/jekylan/internal/message"
	"github.com/Jekinnnnnn/jekylan/internal/query"
	"github.com/Jekinnnnnn/jekylan/internal/tool"
)

// fakeTool is a minimal Tool implementation for testing.
type fakeTool struct{ name string }

func (f fakeTool) Name() string        { return f.name }
func (f fakeTool) Description() string { return "desc " + f.name }
func (f fakeTool) InputSchema() map[string]any {
	return map[string]any{"type": "object"}
}
func (f fakeTool) Call(ctx context.Context, input map[string]any) (string, error) {
	return "", nil
}
func (f fakeTool) SystemPrompt() string { return "" }

// mockQueryRunner captures the params it receives and emits canned events.
// If block is true, the channel stays open until the context is cancelled
// (after all events are sent).
type mockQueryRunner struct {
	mu             sync.Mutex
	capturedParams *query.Params
	events         []query.Event
	block          bool
}

func (m *mockQueryRunner) run(ctx context.Context, params query.Params) <-chan query.Event {
	m.mu.Lock()
	m.capturedParams = &params
	m.mu.Unlock()
	out := make(chan query.Event)
	go func() {
		defer close(out)
		for _, evt := range m.events {
			select {
			case out <- evt:
			case <-ctx.Done():
				return
			}
		}
		if m.block {
			select {
			case <-ctx.Done():
			}
		}
	}()
	return out
}

func (m *mockQueryRunner) params() *query.Params {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.capturedParams
}

func TestRunner_BasicFlow(t *testing.T) {
	mq := &mockQueryRunner{
		events: []query.Event{
			{Type: "assistant_text", Text: "Hello "},
			{Type: "assistant_text", Text: "world"},
			{Type: "result", Result: query.Result{Success: true, Text: "Hello world"}},
		},
	}
	r := NewRunner(RunnerOptions{
		Definition: &Definition{Name: "test", SystemPrompt: "You are a test agent."},
		QueryRunner: mq.run,
	})

	var complete *RunEvent
	for evt := range r.Run(context.Background(), "say hello") {
		switch evt.Type {
		case RunEventProgress:
			t.Fatalf("unexpected progress event (no longer emitted)")
		case RunEventComplete:
			complete = &evt
		case RunEventError:
			t.Fatalf("unexpected error: %s", evt.Error)
		}
	}

	if complete == nil {
		t.Fatal("expected complete event")
	}
	if complete.Result != "Hello world" {
		t.Fatalf("expected 'Hello world', got: %s", complete.Result)
	}
}

func TestRunner_ToolFiltering(t *testing.T) {
	// Build a parent registry with three fake tools.
	parentTools := tool.NewRegistry(
		fakeTool{name: "a"},
		fakeTool{name: "b"},
		fakeTool{name: "c"},
	)

	mq := &mockQueryRunner{
		events: []query.Event{
			{Type: "result", Result: query.Result{Success: true, Text: "ok"}},
		},
	}
	r := NewRunner(RunnerOptions{
		Definition: &Definition{
			Name:       "test",
			ToolsAllow: []string{"a", "c"},
			ToolsDeny:  []string{"c"},
		},
		Tools:       parentTools,
		QueryRunner: mq.run,
	})

	for range r.Run(context.Background(), "do it") {
	}

	if mq.params() == nil {
		t.Fatal("expected query to be called")
	}
	filtered := mq.params().Tools
	if filtered == nil {
		t.Fatal("expected filtered tools")
	}
	if filtered.Find("a") == nil {
		t.Fatal("expected tool 'a' to be present")
	}
	if filtered.Find("b") != nil {
		t.Fatal("expected tool 'b' to be excluded")
	}
	if filtered.Find("c") != nil {
		t.Fatal("expected tool 'c' to be denied")
	}
}

func TestRunner_Cancel(t *testing.T) {
	mq := &mockQueryRunner{
		events: []query.Event{
			{Type: "assistant_text", Text: "start"},
		},
		block: true,
	}
	// The mock never emits a result, so the loop blocks until context cancellation.
	ctx, cancel := context.WithCancel(context.Background())
	r := NewRunner(RunnerOptions{
		Definition:  &Definition{Name: "test"},
		QueryRunner: mq.run,
	})

	done := make(chan struct{})
	var lastEvt RunEvent
	go func() {
		for evt := range r.Run(ctx, "long task") {
			lastEvt = evt
		}
		close(done)
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return after cancel")
	}

	// Cancellation is silent — no error event, channel just closes.
	if lastEvt.Type == RunEventError {
		t.Fatalf("expected silent cancel, got error: %s", lastEvt.Error)
	}
}

func TestRunner_FinalizeFallback(t *testing.T) {
	// Result.Text is empty, but a previous assistant message has text.
	mq := &mockQueryRunner{
		events: []query.Event{
			{Type: "usage", Message: message.Message{
				Role: message.RoleAssistant,
				Content: []message.ContentBlock{
					message.TextBlock{Text: "previous answer"},
				},
			}},
			{Type: "usage", Message: message.Message{
				Role: message.RoleAssistant,
				Content: []message.ContentBlock{
					message.ToolUseBlock{ID: "1", Name: "x"},
				},
			}},
			{Type: "result", Result: query.Result{Success: true, Text: "   "}},
		},
	}
	r := NewRunner(RunnerOptions{
		Definition:  &Definition{Name: "test"},
		QueryRunner: mq.run,
	})

	var complete *RunEvent
	for evt := range r.Run(context.Background(), "prompt") {
		if evt.Type == RunEventComplete {
			complete = &evt
		}
	}
	if complete == nil {
		t.Fatal("expected complete event")
	}
	if complete.Result != "previous answer" {
		t.Fatalf("expected fallback to 'previous answer', got: %s", complete.Result)
	}
}

func TestRunner_MaxTurnsError(t *testing.T) {
	mq := &mockQueryRunner{
		events: []query.Event{
			{Type: "result", Result: query.Result{Success: false, Error: "Reached maximum number of turns (3)"}},
		},
	}
	r := NewRunner(RunnerOptions{
		Definition:  &Definition{Name: "test"},
		QueryRunner: mq.run,
	})

	var lastEvt RunEvent
	for evt := range r.Run(context.Background(), "prompt") {
		lastEvt = evt
	}
	if lastEvt.Type != RunEventError {
		t.Fatalf("expected error, got: %+v", lastEvt)
	}
	if !strings.Contains(lastEvt.Error, "maximum") {
		t.Fatalf("expected max-turns error, got: %s", lastEvt.Error)
	}
}

func TestRunner_SystemPrompt(t *testing.T) {
	mq := &mockQueryRunner{
		events: []query.Event{
			{Type: "result", Result: query.Result{Success: true, Text: "ok"}},
		},
	}
	parentTools := tool.NewRegistry(fakeTool{name: "grep"})
	r := NewRunner(RunnerOptions{
		Definition: &Definition{
			Name:         "test",
			SystemPrompt: "You are a test agent.",
			ToolsAllow:   []string{"*"},
		},
		Tools:       parentTools,
		QueryRunner: mq.run,
	})

	for range r.Run(context.Background(), "prompt") {
	}

	if mq.params() == nil {
		t.Fatal("expected query to be called")
	}
	sys := mq.params().SystemPrompt
	if !strings.Contains(sys, "You are a test agent.") {
		t.Fatalf("expected definition system prompt, got: %s", sys)
	}
	if !strings.Contains(sys, "Available tools:") {
		t.Fatalf("expected tools listing, got: %s", sys)
	}
	if !strings.Contains(sys, "grep") {
		t.Fatalf("expected tool name in listing, got: %s", sys)
	}
	if !strings.Contains(sys, "Pause & Summarize") {
		t.Fatalf("expected Pause & Summarize section, got: %s", sys)
	}
}

func TestExtractLastResultText(t *testing.T) {
	mkText := func(role message.Role, text string) message.Message {
		m := message.Message{Role: role}
		m.AddText(text)
		return m
	}

	// Falls back to earlier assistant when the last one has empty text.
	msgs := []message.Message{
		mkText(message.RoleUser, "u1"),
		mkText(message.RoleAssistant, "a1"),
		mkText(message.RoleUser, "u2"),
		mkText(message.RoleAssistant, ""),
	}
	if got := extractLastResultText(msgs); got != "a1" {
		t.Fatalf("expected 'a1', got: %s", got)
	}

	// Falls back to tool_result content when assistant has no text but tool uses.
	assistantWithTool := message.Message{Role: message.RoleAssistant}
	assistantWithTool.AddToolUse("tu-1", "calc", map[string]any{"a": 1, "b": 2})
	userWithResult := message.Message{Role: message.RoleUser}
	userWithResult.AddToolResult("tu-1", "3", false)

	msgs2 := []message.Message{
		mkText(message.RoleUser, "u1"),
		mkText(message.RoleAssistant, "a1"),
		assistantWithTool,
		userWithResult,
	}
	if got := extractLastResultText(msgs2); got != "3" {
		t.Fatalf("expected '3', got: %s", got)
	}
}

func TestExtractUsage(t *testing.T) {
	msgs := []message.Message{
		{Role: message.RoleAssistant, TurnMetadata: message.TurnMetadata{Usage: &message.Usage{InputTokens: 10}}},
		{Role: message.RoleAssistant, TurnMetadata: message.TurnMetadata{Usage: &message.Usage{InputTokens: 20}}},
	}
	if got := extractUsage(msgs); got == nil || got.InputTokens != 20 {
		t.Fatalf("expected usage with 20 input tokens, got: %+v", got)
	}
}

func TestDefaultResultConsumer_Complete(t *testing.T) {
	events := make(chan RunEvent)
	go func() {
		defer close(events)
		events <- RunEvent{Type: RunEventProgress, Text: "working"}
		events <- RunEvent{Type: RunEventComplete, Result: "final result", Usage: &message.Usage{InputTokens: 10}}
	}()

	consumer := DefaultResultConsumer{}
	res, err := consumer.Consume(events)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Text != "final result" {
		t.Fatalf("expected 'final result', got: %s", res.Text)
	}
	if res.Usage == nil || res.Usage.InputTokens != 10 {
		t.Fatalf("expected usage with 10 input tokens, got: %+v", res.Usage)
	}
}

func TestDefaultResultConsumer_Error(t *testing.T) {
	events := make(chan RunEvent)
	go func() {
		defer close(events)
		events <- RunEvent{Type: RunEventError, Error: "something broke"}
	}()

	consumer := DefaultResultConsumer{}
	_, err := consumer.Consume(events)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "something broke") {
		t.Fatalf("expected error to contain 'something broke', got: %v", err)
	}
}

func TestDefaultResultConsumer_EmptyResult(t *testing.T) {
	events := make(chan RunEvent)
	go func() {
		defer close(events)
		events <- RunEvent{Type: RunEventComplete, Result: ""}
	}()

	consumer := DefaultResultConsumer{}
	res, err := consumer.Consume(events)
	if err != nil {
		t.Fatalf("expected no error for empty result, got: %v", err)
	}
	if res.Text != "" {
		t.Fatalf("expected empty result text, got: %s", res.Text)
	}
}

func TestRunner_CustomResultConsumer(t *testing.T) {
	// Custom consumer that appends a prefix to the result.
	customConsumer := &prefixConsumer{prefix: "[custom] "}

	mq := &mockQueryRunner{
		events: []query.Event{
			{Type: "result", Result: query.Result{Success: true, Text: "hello"}},
		},
	}
	r := NewRunner(RunnerOptions{
		Definition:  &Definition{Name: "test", ResultConsumer: customConsumer},
		QueryRunner: mq.run,
	})

	var complete *RunEvent
	for evt := range r.Run(context.Background(), "say hello") {
		if evt.Type == RunEventComplete {
			complete = &evt
		}
	}
	if complete == nil {
		t.Fatal("expected complete event")
	}
	// The runner itself still emits the raw result in the complete event;
	// the consumer is used by AgentTool/Tracker, not by Runner internally.
	if complete.Result != "hello" {
		t.Fatalf("expected 'hello', got: %s", complete.Result)
	}
}

// prefixConsumer is a test ResultConsumer that prefixes the result.
type prefixConsumer struct {
	prefix string
}

func (p *prefixConsumer) Consume(events <-chan RunEvent) (ConsumerResult, error) {
	var result string
	for evt := range events {
		if evt.Type == RunEventComplete {
			result = evt.Result
		}
	}
	if result == "" {
		return ConsumerResult{}, fmt.Errorf("no result")
	}
	return ConsumerResult{Text: p.prefix + result}, nil
}

func TestRunner_Fork(t *testing.T) {
	// Parent transcript: user asks, assistant with tool_use.
	parentTranscript := func() []message.Message {
		userMsg := message.Message{Role: message.RoleUser}
		userMsg.AddText("parent task")

		assistantMsg := message.Message{Role: message.RoleAssistant}
		assistantMsg.AddText("I'll delegate")
		assistantMsg.AddToolUse("tu-agent", "agent", map[string]any{"prompt": "sub"})

		return []message.Message{userMsg, assistantMsg}
	}

	mq := &mockQueryRunner{
		events: []query.Event{
			{Type: "result", Result: query.Result{Success: true, Text: "fork result"}},
		},
	}
	r := NewRunner(RunnerOptions{
		Definition:  &Definition{Name: "test"},
		Transcript:  parentTranscript,
		QueryRunner: mq.run,
		Fork:        true,
	})

	var complete *RunEvent
	for evt := range r.Run(context.Background(), "Your task is to review code.") {
		if evt.Type == RunEventComplete {
			complete = &evt
		}
	}

	if complete == nil {
		t.Fatal("expected complete event")
	}
	if complete.Result != "fork result" {
		t.Fatalf("expected 'fork result', got: %s", complete.Result)
	}

	// Verify the params sent to query runner.
	if mq.params() == nil {
		t.Fatal("expected query params to be captured")
	}
	msgs := mq.params().Messages
	// Should be: user + assistant + synthetic tool_result + directive
	if len(msgs) != 4 {
		t.Fatalf("expected 4 messages in fork mode, got %d", len(msgs))
	}
	// Last message should be the directive.
	if msgs[3].Role != message.RoleUser || msgs[3].TextContent() != "Your task is to review code." {
		t.Fatalf("expected directive as last message, got: %+v", msgs[3])
	}
	// Message before directive should be synthetic tool_result.
	tr := msgs[2].ToolResults()
	if len(tr) != 1 || tr[0].ToolUseID != "tu-agent" {
		t.Fatalf("expected synthetic tool_result for tu-agent, got: %+v", tr)
	}
}

// TestRunner_ConfirmationFlow verifies that ConfirmTool callback is wired
// into query.Params by the runner.
func TestRunner_ConfirmationFlow(t *testing.T) {
	var capturedParams *query.Params
	confirmRun := func(ctx context.Context, params query.Params) <-chan query.Event {
		capturedParams = &params
		out := make(chan query.Event)
		go func() {
			defer close(out)
			out <- query.Event{Type: "result", Result: query.Result{Success: true, Text: "done"}}
		}()
		return out
	}

	r := NewRunner(RunnerOptions{
		Definition:  &Definition{Name: "test"},
		QueryRunner: confirmRun,
	})

	for range r.Run(context.Background(), "prompt") {
	}

	if capturedParams == nil {
		t.Fatal("expected query to be called")
	}
	if capturedParams.ConfirmTool == nil {
		t.Fatal("expected ConfirmTool to be set in query.Params")
	}
}

func TestDropOrphanToolUses(t *testing.T) {
	mkAssistantToolUse := func(id string) message.Message {
		m := message.Message{Role: message.RoleAssistant}
		m.AddToolUse(id, "tool", nil)
		return m
	}
	mkUserToolResult := func(id string) message.Message {
		m := message.Message{Role: message.RoleUser}
		m.AddToolResult(id, "ok", false)
		return m
	}
	mkText := func(role message.Role) message.Message {
		m := message.Message{Role: role}
		m.AddText("hi")
		return m
	}

	t.Run("all paired", func(t *testing.T) {
		in := []message.Message{
			mkText(message.RoleUser),
			mkAssistantToolUse("a1"),
			mkUserToolResult("a1"),
		}
		got := dropOrphanToolUses(in)
		if len(got) != len(in) {
			t.Fatalf("expected %d msgs, got %d", len(in), len(got))
		}
	})

	t.Run("trailing orphan", func(t *testing.T) {
		in := []message.Message{
			mkText(message.RoleUser),
			mkAssistantToolUse("a1"),
		}
		got := dropOrphanToolUses(in)
		if len(got) != 1 {
			t.Fatalf("expected 1 msg, got %d", len(got))
		}
		if got[0].Role != message.RoleUser {
			t.Fatalf("expected the user msg to remain, got role %q", got[0].Role)
		}
	})

	t.Run("mid orphan", func(t *testing.T) {
		in := []message.Message{
			mkText(message.RoleUser),
			mkAssistantToolUse("a1"),
			mkText(message.RoleUser),
			mkAssistantToolUse("a2"),
			mkUserToolResult("a2"),
		}
		got := dropOrphanToolUses(in)
		if len(got) != 4 {
			t.Fatalf("expected 4 msgs, got %d", len(got))
		}
		for _, m := range got {
			for _, u := range m.ToolUses() {
				if u.ID == "a1" {
					t.Fatalf("orphan a1 should have been dropped")
				}
			}
		}
	})

	t.Run("plain text only", func(t *testing.T) {
		in := []message.Message{
			mkText(message.RoleUser),
			mkText(message.RoleAssistant),
		}
		got := dropOrphanToolUses(in)
		if len(got) != 2 {
			t.Fatalf("expected 2 msgs, got %d", len(got))
		}
	})
}

// ---- ClientFactory (model override) ------------------------------------

// stubClient is a minimal llm.Client used only to verify identity in
// ClientFactory tests — StreamMessages is never called by mockQueryRunner.
type stubClient struct{ name string }

func (s *stubClient) StreamMessages(ctx context.Context, msgs []message.Message, systemPrompt string, tools *tool.Registry, thinkingBudget int64, cacheBreakpoints int) (<-chan llm.StreamEvent, error) {
	return nil, nil
}

// TestRunner_ModelOverrideUsesClientFactory verifies that when
// Definition.Model differs from RunnerOptions.Model and a ClientFactory
// is provided, the runner builds a fresh client and passes it (along
// with the overridden model name) into query.Params.
func TestRunner_ModelOverrideUsesClientFactory(t *testing.T) {
	mq := &mockQueryRunner{
		events: []query.Event{
			{Type: "result", Result: query.Result{Success: true, Text: "ok"}},
		},
	}
	defaultClient := &stubClient{name: "default"}
	overrideClient := &stubClient{name: "override"}

	var factoryCalledWith string
	factory := func(model string) (llm.Client, error) {
		factoryCalledWith = model
		return overrideClient, nil
	}

	r := NewRunner(RunnerOptions{
		Definition:    &Definition{Name: "test", Model: "alt-model"},
		QueryRunner:   mq.run,
		Client:        defaultClient,
		Model:         "default-model",
		ClientFactory: factory,
	})

	for range r.Run(context.Background(), "prompt") {
	}

	if factoryCalledWith != "alt-model" {
		t.Fatalf("expected factory to be called with 'alt-model', got %q", factoryCalledWith)
	}
	params := mq.params()
	if params == nil {
		t.Fatal("expected query params to be captured")
	}
	if params.Client != overrideClient {
		t.Fatalf("expected query.Params.Client to be the factory's override client, got %v", params.Client)
	}
	if params.Model != "alt-model" {
		t.Fatalf("expected query.Params.Model to be 'alt-model', got %q", params.Model)
	}
}

// TestRunner_NoModelOverrideKeepsDefaultClient verifies that when the
// Definition does not override the model (or matches the default),
// the runner uses the default Client even if ClientFactory is set.
func TestRunner_NoModelOverrideKeepsDefaultClient(t *testing.T) {
	mq := &mockQueryRunner{
		events: []query.Event{
			{Type: "result", Result: query.Result{Success: true, Text: "ok"}},
		},
	}
	defaultClient := &stubClient{name: "default"}

	var factoryCalls int
	factory := func(model string) (llm.Client, error) {
		factoryCalls++
		return &stubClient{name: "override"}, nil
	}

	// No Definition.Model override.
	r := NewRunner(RunnerOptions{
		Definition:    &Definition{Name: "test"},
		QueryRunner:   mq.run,
		Client:        defaultClient,
		Model:         "default-model",
		ClientFactory: factory,
	})

	for range r.Run(context.Background(), "prompt") {
	}

	if factoryCalls != 0 {
		t.Fatalf("expected factory to NOT be called, got %d calls", factoryCalls)
	}
	if mq.params() == nil || mq.params().Client != defaultClient {
		t.Fatalf("expected default client to be used")
	}
}
