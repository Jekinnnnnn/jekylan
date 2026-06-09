package memory

import (
	"strings"
	"testing"
	"time"

	"github.com/Jekinnnnnn/jekylan/internal/message"
)

func TestExtractDate(t *testing.T) {
	ts := time.Date(2024, 6, 15, 10, 30, 0, 0, time.UTC)
	msgs := []message.Message{
		{Role: message.RoleUser, Timestamp: ts},
	}
	if got := extractDate(msgs); got != "2024-06-15" {
		t.Errorf("extractDate = %q, want 2024-06-15", got)
	}

	// No timestamps -> empty string
	if got := extractDate([]message.Message{{Role: message.RoleUser}}); got != "" {
		t.Errorf("extractDate(no timestamps) = %q, want empty", got)
	}
}

func TestExtractTimeRange(t *testing.T) {
	ts1 := time.Date(2024, 6, 15, 9, 0, 0, 0, time.UTC)
	ts2 := time.Date(2024, 6, 15, 10, 30, 0, 0, time.UTC)
	msgs := []message.Message{
		{Role: message.RoleUser, Timestamp: ts1},
		{Role: message.RoleAssistant, Timestamp: ts2},
	}
	start, end := extractTimeRange(msgs)
	if start != "09:00" || end != "10:30" {
		t.Errorf("extractTimeRange = %s→%s, want 09:00→10:30", start, end)
	}

	// No timestamps -> empty strings
	start, end = extractTimeRange([]message.Message{{Role: message.RoleUser}})
	if start != "" || end != "" {
		t.Errorf("extractTimeRange(no timestamps) = %s→%s, want empty", start, end)
	}
}

func TestHasToolResultOnly(t *testing.T) {
	if hasToolResultOnly(message.Message{Role: message.RoleUser}) {
		t.Error("empty message should not be tool-result-only")
	}
	msg := message.Message{Role: message.RoleUser}
	msg.AddToolResult("t1", "result", false)
	if !hasToolResultOnly(msg) {
		t.Error("tool_result-only message should match")
	}
	msg2 := message.Message{Role: message.RoleUser}
	msg2.AddText("hello")
	if hasToolResultOnly(msg2) {
		t.Error("text-only message should not match")
	}
}

func TestCleanToolResult(t *testing.T) {
	cases := []struct {
		input, want string
	}{
		{`line1\nline2`, "line1\nline2"},
		{`tab\there`, "tab\there"},
		{`say \"hello\"`, "say \"hello\""},
		{"a\\b", "a\\b"},
		{"a\n\n\n\nb", "a\n\nb"},
	}
	for _, c := range cases {
		got := cleanToolResult(c.input)
		if got != c.want {
			t.Errorf("cleanToolResult(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

func TestFormatToolInput(t *testing.T) {
	cases := []struct {
		name string
		block message.ToolUseBlock
		want string
	}{
		{"bash with command", message.ToolUseBlock{Name: "bash", Input: map[string]any{"command": "ls -la"}}, "ls -la"},
		{"bash without command", message.ToolUseBlock{Name: "bash", Input: map[string]any{}}, ""},
		{"skill", message.ToolUseBlock{Name: "skill", Input: map[string]any{"skill": "calc", "args": "1+1"}}, "calc 1+1"},
		{"unknown", message.ToolUseBlock{Name: "foo", Input: map[string]any{"x": 1}}, `{"x":1}`},
	}
	for _, c := range cases {
		got := formatToolInput(c.block)
		if got != c.want {
			t.Errorf("formatToolInput(%s) = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestCompactMessagesBasic(t *testing.T) {
	ts := time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC)
	msgs := []message.Message{
		{Role: message.RoleUser, Timestamp: ts},
	}
	msgs[0].AddText("hello")

	out := CompactMessages(msgs)
	if !strings.Contains(out, "---") {
		t.Error("expected frontmatter")
	}
	if !strings.Contains(out, "## [user]") {
		t.Error("expected user section")
	}
}

func TestCompactMessagesFiltersThinking(t *testing.T) {
	msg := message.Message{Role: message.RoleAssistant}
	msg.Content = []message.ContentBlock{
		message.ThinkingBlock{Thinking: "internal reasoning"},
	}
	out := CompactMessages([]message.Message{msg})
	if strings.Contains(out, "internal reasoning") {
		t.Error("thinking blocks should be filtered out")
	}
}

func TestCompactMessagesFiltersEmptyToolResults(t *testing.T) {
	userMsg := message.Message{Role: message.RoleUser}
	userMsg.AddToolResult("t1", "", false)
	out := CompactMessages([]message.Message{userMsg})
	if strings.Contains(out, "t1") {
		t.Error("empty tool results should be filtered out")
	}
}
