package message

import (
	"encoding/json"
	"testing"
	"time"
)

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

func TestRoundTripJSON(t *testing.T) {
	msg := Message{
		Role:      RoleAssistant,
		Timestamp: time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC),
		TurnMetadata: TurnMetadata{
			Usage:           &Usage{InputTokens: 100, OutputTokens: 50, CacheCreationInputTokens: 10, CacheReadInputTokens: 20},
			ResponseID:      "resp-123",
			CacheBreakpoint: true,
		},
	}
	msg.AddText("hello")
	msg.AddToolUse("tu1", "bash", map[string]any{"command": "ls"})
	msg.AddToolResult("tu1", "result", false)
	msg.AddThinking("thinking...", "sig")
	msg.AddRedactedThinking("redacted-data")

	b, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded Message
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.Role != msg.Role {
		t.Errorf("role: got %q, want %q", decoded.Role, msg.Role)
	}
	if decoded.ResponseID != msg.ResponseID {
		t.Errorf("response_id: got %q, want %q", decoded.ResponseID, msg.ResponseID)
	}
	if !decoded.CacheBreakpoint {
		t.Error("cache_breakpoint lost")
	}
	if decoded.Usage == nil || decoded.Usage.InputTokens != 100 || decoded.Usage.OutputTokens != 50 {
		t.Errorf("usage mismatch: got %+v", decoded.Usage)
	}
	if len(decoded.Content) != len(msg.Content) {
		t.Fatalf("content length: got %d, want %d", len(decoded.Content), len(msg.Content))
	}
}

func TestTextContent(t *testing.T) {
	m := Message{Role: RoleUser}
	m.AddText("hello")
	m.AddText(" world")
	if got := m.TextContent(); got != "hello world" {
		t.Errorf("TextContent: got %q, want %q", got, "hello world")
	}
}

func TestToolUses(t *testing.T) {
	m := Message{Role: RoleAssistant}
	m.AddToolUse("tu1", "bash", nil)
	m.AddToolUse("tu2", "skill", nil)
	uses := m.ToolUses()
	if len(uses) != 2 {
		t.Fatalf("expected 2 tool uses, got %d", len(uses))
	}
	if uses[0].ID != "tu1" || uses[1].ID != "tu2" {
		t.Errorf("tool use IDs mismatch: %+v", uses)
	}
}

func TestToolResults(t *testing.T) {
	m := Message{Role: RoleUser}
	m.AddToolResult("tu1", "r1", false)
	m.AddToolResult("tu2", "r2", true)
	results := m.ToolResults()
	if len(results) != 2 {
		t.Fatalf("expected 2 tool results, got %d", len(results))
	}
	if results[0].Content != "r1" || results[1].IsError != true {
		t.Errorf("tool results mismatch: %+v", results)
	}
}

func TestParseHumanTime(t *testing.T) {
	if ts, err := parseHumanTime(""); err != nil || !ts.IsZero() {
		t.Errorf("empty string: expected zero time, got %v, err=%v", ts, err)
	}
	if ts, err := parseHumanTime("2024-06-15 10:00:00"); err != nil || ts.Year() != 2024 {
		t.Errorf("valid time: expected 2024, got %v, err=%v", ts, err)
	}
	if _, err := parseHumanTime("invalid"); err == nil {
		t.Error("invalid time: expected error")
	}
}

func TestAddMethods(t *testing.T) {
	var m Message
	m.AddText("t")
	m.AddToolUse("id", "name", map[string]any{"k": "v"})
	m.AddToolResult("tid", "res", true)
	m.AddThinking("think", "sig")
	m.AddRedactedThinking("data")
	if len(m.Content) != 5 {
		t.Fatalf("expected 5 blocks, got %d", len(m.Content))
	}
}
