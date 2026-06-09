package llm

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Jekinnnnnn/jekylan/internal/message"
)

func TestToAnthropicMessage_CacheBreakpoint(t *testing.T) {
	m := message.Message{Role: message.RoleUser, TurnMetadata: message.TurnMetadata{CacheBreakpoint: true}}
	m.AddText("first text")
	m.AddToolResult("tool-1", "result", false)
	m.AddText("last text")

	am := toAnthropicMessage(m)
	b, _ := json.Marshal(am)
	s := string(b)

	if !strings.Contains(s, "cache_control") {
		t.Fatalf("expected cache_control in JSON, got: %s", s)
	}
	if !strings.Contains(s, "ephemeral") {
		t.Fatalf("expected ephemeral in cache_control, got: %s", s)
	}
}

func TestToAnthropicMessage_CacheBreakpoint_LastTextBlock(t *testing.T) {
	m := message.Message{Role: message.RoleAssistant, TurnMetadata: message.TurnMetadata{CacheBreakpoint: true}}
	m.AddText("text one")
	m.AddText("text two") // this should get the breakpoint

	am := toAnthropicMessage(m)
	b, _ := json.Marshal(am)
	s := string(b)

	// Count occurrences of cache_control — should be exactly 1 (on the last text block).
	count := 0
	for i := 0; i < len(s)-len("cache_control"); i++ {
		if s[i:i+len("cache_control")] == "cache_control" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 cache_control marker, got %d", count)
	}
}

func TestToAnthropicMessage_NoCacheBreakpoint(t *testing.T) {
	m := message.Message{Role: message.RoleUser}
	m.AddText("hello")

	am := toAnthropicMessage(m)
	b, _ := json.Marshal(am)
	s := string(b)

	if strings.Contains(s, "cache_control") {
		t.Fatalf("expected no cache_control without CacheBreakpoint, got: %s", s)
	}
}

func TestAnthropicMessageSerializationTwoTurns(t *testing.T) {
	user1 := message.Message{Role: message.RoleUser}
	user1.AddText("hello")

	assistant1 := message.Message{Role: message.RoleAssistant}
	assistant1.AddText("Hello! How can I help you today?")

	user2 := message.Message{Role: message.RoleUser}
	user2.AddText("what's the weather")

	msgs := []message.Message{user1, assistant1, user2}

	for i, m := range msgs {
		am := toAnthropicMessage(m)
		b, _ := json.MarshalIndent(am, "", "  ")
		t.Logf("Message %d (role=%s):\n%s\n", i, m.Role, string(b))
	}
}

func TestToOpenAIMessages_Assistant(t *testing.T) {
	m := message.Message{Role: message.RoleAssistant}
	m.AddText("hello")
	m.AddToolUse("tu1", "bash", map[string]any{"command": "ls"})

	msgs := toOpenAIMessages(m)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	b, _ := json.Marshal(msgs[0])
	s := string(b)
	if !strings.Contains(s, "hello") {
		t.Errorf("expected text content, got: %s", s)
	}
	if !strings.Contains(s, "tool_calls") {
		t.Errorf("expected tool_calls, got: %s", s)
	}
}

func TestToOpenAIMessages_UserWithToolResults(t *testing.T) {
	m := message.Message{Role: message.RoleUser}
	m.AddText("run it")
	m.AddToolResult("tu1", "result", false)

	msgs := toOpenAIMessages(m)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
}

func TestToOpenAIMessages_RoleSystem(t *testing.T) {
	m := message.Message{Role: message.RoleSystem}
	m.AddText("system prompt")

	msgs := toOpenAIMessages(m)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	b, _ := json.Marshal(msgs[0])
	if !strings.Contains(string(b), "system prompt") {
		t.Errorf("expected system prompt, got: %s", string(b))
	}
}

func TestToAnthropicMessage_RoleSystem(t *testing.T) {
	m := message.Message{Role: message.RoleSystem}
	m.AddText("sys")
	am := toAnthropicMessage(m)
	b, _ := json.Marshal(am)
	if !strings.Contains(string(b), "user") {
		t.Errorf("expected RoleSystem to fallback to user message, got: %s", string(b))
	}
}
