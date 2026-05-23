package message

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestToAnthropicMessage_CacheBreakpoint(t *testing.T) {
	m := Message{Role: RoleUser, CacheBreakpoint: true}
	m.AddText("first text")
	m.AddToolResult("tool-1", "result", false)
	m.AddText("last text")

	am := m.ToAnthropicMessage()
	b, _ := json.Marshal(am)
	s := string(b)

	if !contains(s, "cache_control") {
		t.Fatalf("expected cache_control in JSON, got: %s", s)
	}
	if !contains(s, "ephemeral") {
		t.Fatalf("expected ephemeral in cache_control, got: %s", s)
	}
}

func TestToAnthropicMessage_CacheBreakpoint_LastTextBlock(t *testing.T) {
	m := Message{Role: RoleAssistant, CacheBreakpoint: true}
	m.AddText("text one")
	m.AddText("text two") // this should get the breakpoint

	am := m.ToAnthropicMessage()
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
	m := Message{Role: RoleUser}
	m.AddText("hello")

	am := m.ToAnthropicMessage()
	b, _ := json.Marshal(am)
	s := string(b)

	if contains(s, "cache_control") {
		t.Fatalf("expected no cache_control without CacheBreakpoint, got: %s", s)
	}
}

func contains(s, substr string) bool { return strings.Contains(s, substr) }

func TestUsageString(t *testing.T) {
	u := Usage{InputTokens: 100, OutputTokens: 50}
	if s := u.String(); s != "input=100 output=50" {
		t.Fatalf("expected 'input=100 output=50', got: %s", s)
	}

	u2 := Usage{InputTokens: 100, OutputTokens: 50, CacheCreationInputTokens: 10, CacheReadInputTokens: 200}
	if s := u2.String(); s != "input=100 output=50 cache_create=10 cache_read=200" {
		t.Fatalf("expected cache stats, got: %s", s)
	}
}

func TestAnthropicMessageSerializationTwoTurns(t *testing.T) {
	user1 := Message{Role: RoleUser}
	user1.AddText("hello")

	assistant1 := Message{Role: RoleAssistant}
	assistant1.AddText("Hello! How can I help you today?")

	user2 := Message{Role: RoleUser}
	user2.AddText("what's the weather")

	msgs := []Message{user1, assistant1, user2}

	for i, m := range msgs {
		am := m.ToAnthropicMessage()
		b, _ := json.MarshalIndent(am, "", "  ")
		fmt.Printf("\nMessage %d (role=%s):\n%s\n", i, m.Role, string(b))
	}
}
