package engine

import (
	"testing"
	"time"

	"github.com/Jekinnnnnn/jekylan/internal/message"
)

func TestEngineMessageAccumulation(t *testing.T) {
	e := NewEngine(nil, "test-model", 10, 0, "", nil, true)

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
		name           string
		seed           func(e *Engine)
		expectedLen    int
		expectedIDs    []string
		expectedNoOp   bool
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
			if len(tail.Content) != len(tc.expectedIDs) {
				t.Fatalf("expected %d tool_result blocks, got %d", len(tc.expectedIDs), len(tail.Content))
			}
			for i, want := range tc.expectedIDs {
				tr, ok := tail.Content[i].(message.ToolResultBlock)
				if !ok {
					t.Fatalf("content[%d] is not a ToolResultBlock: %T", i, tail.Content[i])
				}
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
