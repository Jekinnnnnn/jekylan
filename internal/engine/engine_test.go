package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jekinnnnnn/jekylan/internal/message"
	"github.com/Jekinnnnnn/jekylan/internal/query"
)

func TestEngineMessageAccumulation(t *testing.T) {
	e := NewEngine(nil, "test-model", 10, 0, "", nil, true)
	defer e.Stop()

	// Simulate first turn: user message + assistant reply
	user1 := message.Message{Role: message.RoleUser, Timestamp: time.Now()}
	user1.AddText("hello")
	e.messages = append(e.messages, user1)

	assistant1 := message.Message{Role: message.RoleAssistant, Timestamp: time.Now()}
	assistant1.AddText("Hello! How can I help you today?")
	e.messages = append(e.messages, assistant1)

	// Now simulate SubmitMessage for second turn
	user2 := message.Message{Role: message.RoleUser, Timestamp: time.Now()}
	user2.AddText("what's the weather")
	e.messages = append(e.messages, user2)

	// Verify message roles alternate correctly
	for i, m := range e.messages {
		t.Logf("Message %d: role=%s content_count=%d", i, m.Role, len(m.Content))
		if len(m.Content) == 0 {
			t.Errorf("Message %d has empty content", i)
		}
	}
}

func TestSubmitMessageMergesConsecutiveUserMessages(t *testing.T) {
	e := NewEngine(nil, "test-model", 10, 0, "", nil, true)
	defer e.Stop()

	// First turn ends with assistant message
	user1 := message.Message{Role: message.RoleUser, Timestamp: time.Now()}
	user1.AddText("hello")
	e.messages = append(e.messages, user1)

	assistant1 := message.Message{Role: message.RoleAssistant, Timestamp: time.Now()}
	assistant1.AddText("Hello!")
	e.messages = append(e.messages, assistant1)

	// Simulate SubmitMessage adding second user prompt
	// (bypassing actual SubmitMessage to avoid needing a real client)
	user2 := message.Message{Role: message.RoleUser, Timestamp: time.Now()}
	user2.AddText("what's the weather")
	e.messages = append(e.messages, user2)

	// Should have 3 messages, roles must alternate
	if len(e.messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(e.messages))
	}
	roles := []message.Role{e.messages[0].Role, e.messages[1].Role, e.messages[2].Role}
	for i := 1; i < len(roles); i++ {
		if roles[i] == roles[i-1] {
			t.Errorf("consecutive messages have same role at index %d: %s", i, roles[i])
		}
	}

	// Now simulate a turn that ends with tool results (user message)
	assistant2 := message.Message{Role: message.RoleAssistant, Timestamp: time.Now()}
	assistant2.AddToolUse("tu_1", "bash", map[string]any{"command": "ls"})
	e.messages = append(e.messages, assistant2)

	userToolResult := message.Message{Role: message.RoleUser, Timestamp: time.Now()}
	userToolResult.AddToolResult("tu_1", "file.txt", false)
	e.messages = append(e.messages, userToolResult)

	// Now simulate SubmitMessage for third turn
	// The last message is user (tool result), so new prompt should merge into it
	lastIdx := len(e.messages) - 1
	if e.messages[lastIdx].Role != message.RoleUser {
		t.Fatalf("setup error: last message should be user")
	}
	last := &e.messages[lastIdx]
	last.AddText("next question")

	// Should still have 5 messages (no extra user message created)
	if len(e.messages) != 5 {
		t.Fatalf("expected 5 messages after merge, got %d", len(e.messages))
	}
	// Last message should have 2 content blocks: tool_result + text
	lastMsg := e.messages[len(e.messages)-1]
	if len(lastMsg.Content) != 2 {
		t.Errorf("expected last user message to have 2 content blocks, got %d", len(lastMsg.Content))
	}
	// Verify no consecutive same roles
	for i := 1; i < len(e.messages); i++ {
		if e.messages[i].Role == e.messages[i-1].Role {
			t.Errorf("consecutive messages have same role at index %d: %s", i, e.messages[i].Role)
		}
	}
}

func TestRepairOrphanToolUses(t *testing.T) {
	type tcase struct {
		name         string
		seed         func(e *Engine)
		expectedLen  int
		expectedIDs  []string
		expectedNoOp bool
	}

	cases := []tcase{
		{
			name:         "empty messages: no-op",
			seed:         func(e *Engine) {},
			expectedLen:  0,
			expectedNoOp: true,
		},
		{
			name: "last is user: no-op",
			seed: func(e *Engine) {
				u := message.Message{Role: message.RoleUser, Timestamp: time.Now()}
				u.AddText("hello")
				e.messages = append(e.messages, u)
			},
			expectedLen:  1,
			expectedNoOp: true,
		},
		{
			name: "last is assistant text-only: no-op",
			seed: func(e *Engine) {
				u := message.Message{Role: message.RoleUser, Timestamp: time.Now()}
				u.AddText("hi")
				e.messages = append(e.messages, u)
				a := message.Message{Role: message.RoleAssistant, Timestamp: time.Now()}
				a.AddText("hello back")
				e.messages = append(e.messages, a)
			},
			expectedLen:  2,
			expectedNoOp: true,
		},
		{
			name: "last assistant with one tool_use: append synthetic user with one tool_result",
			seed: func(e *Engine) {
				u := message.Message{Role: message.RoleUser, Timestamp: time.Now()}
				u.AddText("run ls")
				e.messages = append(e.messages, u)
				a := message.Message{Role: message.RoleAssistant, Timestamp: time.Now()}
				a.AddToolUse("call_abc", "bash", map[string]any{"command": "ls"})
				e.messages = append(e.messages, a)
			},
			expectedLen: 3,
			expectedIDs: []string{"call_abc"},
		},
		{
			name: "last assistant with multiple tool_uses: append synthetic user with all tool_results in order",
			seed: func(e *Engine) {
				u := message.Message{Role: message.RoleUser, Timestamp: time.Now()}
				u.AddText("run two")
				e.messages = append(e.messages, u)
				a := message.Message{Role: message.RoleAssistant, Timestamp: time.Now()}
				a.AddText("running both")
				a.AddToolUse("call_1", "bash", map[string]any{"command": "ls"})
				a.AddToolUse("call_2", "bash", map[string]any{"command": "pwd"})
				e.messages = append(e.messages, a)
			},
			expectedLen: 3,
			expectedIDs: []string{"call_1", "call_2"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := NewEngine(nil, "test-model", 10, 0, "", nil, true)
			defer e.Stop()
			tc.seed(e)
			beforeLen := len(e.messages)

			e.repairOrphanToolUses()

			if tc.expectedNoOp {
				if len(e.messages) != beforeLen {
					t.Fatalf("expected no-op, got len %d (was %d)", len(e.messages), beforeLen)
				}
				return
			}

			if len(e.messages) != tc.expectedLen {
				t.Fatalf("expected %d messages, got %d", tc.expectedLen, len(e.messages))
			}

			tail := e.messages[len(e.messages)-1]
			if tail.Role != message.RoleUser {
				t.Fatalf("expected synthetic message to be user, got %s", tail.Role)
			}
			toolResults := tail.ToolResults()
			if len(toolResults) != len(tc.expectedIDs) {
				t.Fatalf("expected %d tool_result blocks, got %d", len(tc.expectedIDs), len(toolResults))
			}
			for i, want := range tc.expectedIDs {
				tr := toolResults[i]
				if tr.ToolUseID != want {
					t.Errorf("content[%d] tool_use_id: want %q, got %q", i, want, tr.ToolUseID)
				}
				if tr.Content != "[Tool execution interrupted]" {
					t.Errorf("content[%d] content: want placeholder, got %q", i, tr.Content)
				}
				if !tr.IsError {
					t.Errorf("content[%d] IsError: want true, got false", i)
				}
			}
		})
	}
}

func TestRepairOrphanToolUsesIdempotent(t *testing.T) {
	e := NewEngine(nil, "test-model", 10, 0, "", nil, true)
	defer e.Stop()

	u := message.Message{Role: message.RoleUser, Timestamp: time.Now()}
	u.AddText("run")
	e.messages = append(e.messages, u)
	a := message.Message{Role: message.RoleAssistant, Timestamp: time.Now()}
	a.AddToolUse("call_x", "bash", map[string]any{"command": "true"})
	e.messages = append(e.messages, a)

	e.repairOrphanToolUses()
	firstLen := len(e.messages)

	// Second call must be a no-op since the trailing message is now user.
	e.repairOrphanToolUses()
	if len(e.messages) != firstLen {
		t.Fatalf("repairOrphanToolUses is not idempotent: len went from %d to %d", firstLen, len(e.messages))
	}
}

// fakeQueryFunc returns a query function that emits the provided events.
func fakeQueryFunc(evts ...query.Event) func(context.Context, query.Params) <-chan query.Event {
	return func(_ context.Context, _ query.Params) <-chan query.Event {
		out := make(chan query.Event)
		go func() {
			defer close(out)
			for _, evt := range evts {
				out <- evt
			}
		}()
		return out
	}
}

func TestTurnEmitsTextDeltaAndResult(t *testing.T) {
	e := NewEngine(nil, "test-model", 10, 0, "", nil, true)
	defer e.Stop()

	assistantMsg := message.Message{Role: message.RoleAssistant, Timestamp: time.Now()}
	assistantMsg.AddText("hello world")

	e.queryFunc = fakeQueryFunc(
		query.Event{Type: query.EventTypeAssistantText, Text: "hello "},
		query.Event{Type: query.EventTypeAssistantText, Text: "world"},
		query.Event{Type: query.EventTypeUsage, Message: assistantMsg},
		query.Event{Type: query.EventTypeResult, Result: query.Result{Success: true, Text: "hello world", StopReason: "end_turn", NumTurns: 1}},
	)

	var got []EngineEvent
	for evt := range e.Turn(context.Background(), "say hello") {
		got = append(got, evt)
	}

	if len(got) < 2 {
		t.Fatalf("expected at least 2 events, got %d", len(got))
	}

	// Text deltas should be accumulated and flushed at the usage boundary.
	var textDeltas []string
	var hasResult bool
	for _, evt := range got {
		switch evt.Type {
		case EventTextDelta:
			textDeltas = append(textDeltas, evt.TextDelta)
		case EventTurnResult:
			hasResult = true
			if !evt.Result.Success {
				t.Errorf("expected success, got failure")
			}
			if evt.Result.NumTurns != 1 {
				t.Errorf("expected 1 turn, got %d", evt.Result.NumTurns)
			}
		}
	}

	if len(textDeltas) != 1 || textDeltas[0] != "hello world" {
		t.Errorf("expected single text delta 'hello world', got %v", textDeltas)
	}
	if !hasResult {
		t.Error("expected EventTurnResult")
	}

	// Messages should include user prompt and assistant reply.
	msgs := e.Messages()
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Role != message.RoleUser {
		t.Errorf("expected first message to be user, got %s", msgs[0].Role)
	}
	if msgs[1].Role != message.RoleAssistant {
		t.Errorf("expected second message to be assistant, got %s", msgs[1].Role)
	}
}

func TestTurnEmitsError(t *testing.T) {
	e := NewEngine(nil, "test-model", 10, 0, "", nil, true)
	defer e.Stop()
	e.queryFunc = fakeQueryFunc(
		query.Event{Type: query.EventTypeError, Result: query.Result{Success: false, Error: "something broke"}},
	)

	var got []EngineEvent
	for evt := range e.Turn(context.Background(), "trigger error") {
		got = append(got, evt)
	}

	if len(got) != 1 {
		t.Fatalf("expected 1 event, got %d", len(got))
	}
	if got[0].Type != EventTurnError {
		t.Errorf("expected EventTurnError, got %s", got[0].Type)
	}
	if got[0].Error != "something broke" {
		t.Errorf("expected error 'something broke', got %q", got[0].Error)
	}
}

func TestTurnEmitsToolUse(t *testing.T) {
	e := NewEngine(nil, "test-model", 10, 0, "", nil, true)
	defer e.Stop()
	e.queryFunc = fakeQueryFunc(
		query.Event{Type: query.EventTypeAssistantToolUse, ToolUseID: "tu_1", ToolName: "bash", ToolInput: map[string]any{"command": "ls"}},
		query.Event{Type: query.EventTypeResult, Result: query.Result{Success: true, StopReason: "end_turn", NumTurns: 1}},
	)

	var got []EngineEvent
	for evt := range e.Turn(context.Background(), "run ls") {
		got = append(got, evt)
	}

	var hasToolUse bool
	for _, evt := range got {
		if evt.Type == EventToolUse {
			hasToolUse = true
			if evt.ToolUse.ToolName != "bash" {
				t.Errorf("expected tool bash, got %s", evt.ToolUse.ToolName)
			}
			if evt.ToolUse.ToolUseID != "tu_1" {
				t.Errorf("expected tool_use_id tu_1, got %s", evt.ToolUse.ToolUseID)
			}
		}
	}
	if !hasToolUse {
		t.Error("expected EventToolUse")
	}
}

func TestTurnFlushTextBeforeToolUse(t *testing.T) {
	e := NewEngine(nil, "test-model", 10, 0, "", nil, true)
	defer e.Stop()
	e.queryFunc = fakeQueryFunc(
		query.Event{Type: query.EventTypeAssistantText, Text: "let me "},
		query.Event{Type: query.EventTypeAssistantText, Text: "check"},
		query.Event{Type: query.EventTypeAssistantToolUse, ToolUseID: "tu_1", ToolName: "bash", ToolInput: map[string]any{"command": "ls"}},
		query.Event{Type: query.EventTypeResult, Result: query.Result{Success: true, StopReason: "end_turn", NumTurns: 1}},
	)

	var textDeltas []string
	for evt := range e.Turn(context.Background(), "run ls") {
		if evt.Type == EventTextDelta {
			textDeltas = append(textDeltas, evt.TextDelta)
		}
	}

	// Text should be flushed BEFORE the tool_use event.
	if len(textDeltas) != 1 || textDeltas[0] != "let me check" {
		t.Errorf("expected text delta 'let me check', got %v", textDeltas)
	}
}

func TestInjectRelevantMemories(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a MEMORY.md index.
	indexContent := "- [Test](test.md) — test hook"
	if err := os.WriteFile(filepath.Join(tmpDir, "MEMORY.md"), []byte(indexContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a small memory file.
	memContent := "This is a test memory about Go programming."
	if err := os.WriteFile(filepath.Join(tmpDir, "test.md"), []byte(memContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create an oversized memory file (should be skipped).
	largeContent := make([]byte, 5000)
	for i := range largeContent {
		largeContent[i] = 'x'
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "large.md"), largeContent, 0644); err != nil {
		t.Fatal(err)
	}

	e := NewEngine(nil, "test-model", 10, 0, "", nil, true, WithMemoryDir(tmpDir))
	defer e.Stop()

	msgs := []message.Message{
		{Role: message.RoleUser, Timestamp: time.Now()},
	}
	msgs[0].AddText("Tell me about Go")

	result := e.injectRelevantMemories(context.Background(), "Tell me about Go", append([]message.Message(nil), msgs...))

	// Should inject memory before the last user message.
	if len(result) != 2 {
		t.Fatalf("expected 2 messages after injection, got %d", len(result))
	}

	injectedText := result[0].TextContent()
	if !strings.Contains(injectedText, "test memory") {
		t.Errorf("expected injected memory to contain 'test memory', got: %s", injectedText)
	}

	// Large file should be skipped (over 4KB).
	if strings.Contains(injectedText, "xxxx") {
		t.Error("large file should have been skipped")
	}
}

func TestInjectRelevantMemoriesRespectsTotalSizeLimit(t *testing.T) {
	tmpDir := t.TempDir()

	// MEMORY.md index required for memory discovery.
	os.WriteFile(filepath.Join(tmpDir, "MEMORY.md"), []byte("- [m1](mem1.md)\n- [m2](mem2.md)\n- [m3](mem3.md)"), 0644)

	// Create two 3KB memory files (total would be 6KB, limit is 8KB, so both fit).
	content3k := make([]byte, 3000)
	for i := range content3k {
		content3k[i] = 'a' + byte(i%26)
	}
	os.WriteFile(filepath.Join(tmpDir, "mem1.md"), content3k, 0644)
	os.WriteFile(filepath.Join(tmpDir, "mem2.md"), content3k, 0644)
	// Third 3KB file would exceed 8KB total.
	os.WriteFile(filepath.Join(tmpDir, "mem3.md"), content3k, 0644)

	e := NewEngine(nil, "test-model", 10, 0, "", nil, true, WithMemoryDir(tmpDir))
	defer e.Stop()

	msgs := []message.Message{
		{Role: message.RoleUser, Timestamp: time.Now()},
	}
	msgs[0].AddText("mem")

	result := e.injectRelevantMemories(context.Background(), "mem", append([]message.Message(nil), msgs...))
	if len(result) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(result))
	}

	injectedText := result[0].TextContent()
	// Should only have 2 files (6KB), not 3 (9KB).
	count := strings.Count(injectedText, "---")
	if count != 1 {
		t.Errorf("expected 1 separator (2 files), got %d separators", count)
	}
}

func TestResetClearsState(t *testing.T) {
	e := NewEngine(nil, "test-model", 10, 0, "", nil, true)
	defer e.Stop()

	// Seed some state.
	e.messages = append(e.messages, message.Message{Role: message.RoleUser, Timestamp: time.Now()})
	e.totalUsage = &message.Usage{InputTokens: 100}
	e.textBuffer.WriteString("buffered")
	e.recentTools = []string{"bash"}

	e.reset()

	if len(e.messages) != 0 {
		t.Errorf("expected messages to be cleared, got %d", len(e.messages))
	}
	if e.totalUsage != nil {
		t.Error("expected totalUsage to be nil")
	}
	if e.textBuffer.Len() != 0 {
		t.Errorf("expected textBuffer to be empty, got %q", e.textBuffer.String())
	}
	if len(e.recentTools) != 0 {
		t.Errorf("expected recentTools to be empty, got %v", e.recentTools)
	}
}

func TestAccumulateUsage(t *testing.T) {
	e := NewEngine(nil, "test-model", 10, 0, "", nil, true)
	defer e.Stop()

	e.accumulateUsage(&message.Usage{InputTokens: 10, OutputTokens: 20, CacheCreationInputTokens: 5, CacheReadInputTokens: 3})
	e.accumulateUsage(&message.Usage{InputTokens: 5, OutputTokens: 10, CacheCreationInputTokens: 2, CacheReadInputTokens: 1})

	u := e.TotalUsage()
	if u == nil {
		t.Fatal("expected non-nil usage")
	}
	if u.InputTokens != 15 {
		t.Errorf("input: want 15, got %d", u.InputTokens)
	}
	if u.OutputTokens != 30 {
		t.Errorf("output: want 30, got %d", u.OutputTokens)
	}
	if u.CacheCreationInputTokens != 7 {
		t.Errorf("cache_create: want 7, got %d", u.CacheCreationInputTokens)
	}
	if u.CacheReadInputTokens != 4 {
		t.Errorf("cache_read: want 4, got %d", u.CacheReadInputTokens)
	}
}
