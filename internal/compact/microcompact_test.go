package compact

import (
	"testing"
	"time"

	"github.com/Jekinnnnnn/jekylan/internal/message"
)

func TestForceTimeBasedMicrocompact(t *testing.T) {
	// Build a conversation with 5 tool_use + tool_result pairs.
	msgs := []message.Message{
		{Role: message.RoleUser, Content: []message.ContentBlock{message.TextBlock{Text: "start"}}},
	}
	for i := 0; i < 5; i++ {
		id := string(rune('a' + i))
		msgs = append(msgs, message.Message{
			Role: message.RoleAssistant,
			Content: []message.ContentBlock{
				message.ToolUseBlock{ID: id, Name: "bash", Input: map[string]any{"cmd": "ls"}},
			},
		})
		msgs = append(msgs, message.Message{
			Role: message.RoleUser,
			Content: []message.ContentBlock{
				message.ToolResultBlock{ToolUseID: id, Content: "result " + id},
			},
		})
	}

	res := ForceTimeBasedMicrocompact(msgs, 2)
	if !res.Changed {
		t.Fatal("expected compaction to change messages")
	}

	// Count cleared results.
	cleared := 0
	kept := 0
	for _, m := range res.Messages {
		for _, block := range m.Content {
			if tr, ok := block.(message.ToolResultBlock); ok {
				if tr.Content == timeBasedMCClearedMessage {
					cleared++
				} else {
					kept++
				}
			}
		}
	}

	if cleared != 3 {
		t.Errorf("expected 3 cleared results, got %d", cleared)
	}
	if kept != 2 {
		t.Errorf("expected 2 kept results, got %d", kept)
	}
}

func TestForceTimeBasedMicrocompactKeepsAllWhenUnderThreshold(t *testing.T) {
	msgs := []message.Message{
		{Role: message.RoleAssistant, Content: []message.ContentBlock{
			message.ToolUseBlock{ID: "x", Name: "bash", Input: map[string]any{}},
		}},
		{Role: message.RoleUser, Content: []message.ContentBlock{
			message.ToolResultBlock{ToolUseID: "x", Content: "result"},
		}},
	}

	res := ForceTimeBasedMicrocompact(msgs, 5)
	if res.Changed {
		t.Error("expected no changes when keepRecent >= tool count")
	}
}

func TestMaybeTimeBasedMicrocompactTriggersWhenStale(t *testing.T) {
	now := time.Now()
	msgs := []message.Message{}
	for i := 0; i < 5; i++ {
		id := string(rune('a' + i))
		msgs = append(msgs, message.Message{
			Role: message.RoleAssistant, Timestamp: now.Add(-20 * time.Minute), Content: []message.ContentBlock{
				message.ToolUseBlock{ID: id, Name: "bash", Input: map[string]any{}},
			},
		})
		msgs = append(msgs, message.Message{
			Role: message.RoleUser, Content: []message.ContentBlock{
				message.ToolResultBlock{ToolUseID: id, Content: "result " + id},
			},
		})
	}

	res := MaybeTimeBasedMicrocompact(msgs, now)
	if res == nil || !res.Changed {
		t.Fatal("expected time-based trigger to fire")
	}
}

func TestMaybeTimeBasedMicrocompactDoesNotTriggerWhenFresh(t *testing.T) {
	now := time.Now()
	msgs := []message.Message{
		{Role: message.RoleAssistant, Timestamp: now.Add(-1 * time.Minute), Content: []message.ContentBlock{
			message.ToolUseBlock{ID: "x", Name: "bash", Input: map[string]any{}},
		}},
		{Role: message.RoleUser, Content: []message.ContentBlock{
			message.ToolResultBlock{ToolUseID: "x", Content: "result"},
		}},
	}

	res := MaybeTimeBasedMicrocompact(msgs, now)
	if res != nil {
		t.Fatal("expected time-based trigger NOT to fire for fresh messages")
	}
}

func TestMaybeTimeBasedMicrocompactDoesNotTriggerWithoutTimestamp(t *testing.T) {
	msgs := []message.Message{
		{Role: message.RoleAssistant, Content: []message.ContentBlock{
			message.ToolUseBlock{ID: "x", Name: "bash", Input: map[string]any{}},
		}},
		{Role: message.RoleUser, Content: []message.ContentBlock{
			message.ToolResultBlock{ToolUseID: "x", Content: "result"},
		}},
	}

	res := MaybeTimeBasedMicrocompact(msgs, time.Now())
	if res != nil {
		t.Fatal("expected no trigger when assistant message has no timestamp")
	}
}

func TestMicrocompactMessagesFiresWhenStale(t *testing.T) {
	now := time.Now()
	msgs := []message.Message{}
	for i := 0; i < 5; i++ {
		id := string(rune('a' + i))
		msgs = append(msgs, message.Message{
			Role: message.RoleAssistant, Timestamp: now.Add(-20 * time.Minute), Content: []message.ContentBlock{
				message.ToolUseBlock{ID: id, Name: "bash", Input: map[string]any{}},
			},
		})
		msgs = append(msgs, message.Message{
			Role: message.RoleUser, Content: []message.ContentBlock{
				message.ToolResultBlock{ToolUseID: id, Content: "result " + id},
			},
		})
	}

	res := MicrocompactMessages(msgs)
	if !res.Changed {
		t.Fatal("expected MicrocompactMessages to fire for stale assistant")
	}
}
