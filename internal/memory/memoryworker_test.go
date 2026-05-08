package memory

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Jekinnnnnn/jekylan/internal/message"
)

func loadSessionMessages(t *testing.T, path string) []message.Message {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read session: %v", err)
	}
	var data struct {
		Messages []struct {
			Role       string                   `json:"role"`
			Content    []map[string]interface{} `json:"content"`
			Timestamp  string                   `json:"timestamp"`
			Usage      *message.Usage           `json:"usage,omitempty"`
			ResponseID string                   `json:"response_id,omitempty"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(b, &data); err != nil {
		t.Fatalf("parse session: %v", err)
	}

	var msgs []message.Message
	for _, m := range data.Messages {
		msg := message.Message{Role: message.Role(m.Role)}
		if m.Timestamp != "" {
			if ts, err := time.Parse("2006-01-02 15:04:05", m.Timestamp); err == nil {
				msg.Timestamp = ts
			}
		}
		for _, c := range m.Content {
			typ, _ := c["_type"].(string)
			switch typ {
			case "text":
				if text, ok := c["text"].(string); ok {
					msg.Content = append(msg.Content, message.TextBlock{Text: text})
				}
			case "tool_use":
				id, _ := c["id"].(string)
				name, _ := c["name"].(string)
				input, _ := c["input"].(map[string]interface{})
				msg.Content = append(msg.Content, message.ToolUseBlock{ID: id, Name: name, Input: input})
			case "tool_result":
				content, _ := c["content"].(string)
				isError, _ := c["is_error"].(bool)
				toolUseID, _ := c["tool_use_id"].(string)
				msg.Content = append(msg.Content, message.ToolResultBlock{ToolUseID: toolUseID, Content: content, IsError: isError})
			}
		}
		msgs = append(msgs, msg)
	}
	return msgs
}

func TestCompactMessagesWithSession(t *testing.T) {
	msgs := loadSessionMessages(t, "../../session/kimi_2.6.json")
	if len(msgs) == 0 {
		t.Fatal("no messages loaded")
	}
	t.Logf("Original messages: %d", len(msgs))

	mdl := CompactMessages(msgs)

	// Verify MDL structure.
	if !strings.Contains(mdl, "---") {
		t.Error("expected frontmatter")
	}
	if !strings.Contains(mdl, "skill: shouzu") {
		t.Error("expected skill name in frontmatter")
	}
	if !strings.Contains(mdl, "# 对话记录") {
		t.Error("expected dialogue record title")
	}
	if !strings.Contains(mdl, "## [user]") {
		t.Error("expected user section")
	}
	if !strings.Contains(mdl, "## [assistant]") {
		t.Error("expected assistant section")
	}
	if !strings.Contains(mdl, "## [user:tool_result]") {
		t.Error("expected tool_result section")
	}

	// Verify skill docs were replaced.
	skillDocCount := strings.Count(mdl, "[Skill documentation returned")
	t.Logf("Skill docs replaced with placeholder: %d", skillDocCount)
	if skillDocCount == 0 {
		t.Error("expected at least one skill doc placeholder")
	}

	// Verify arrow tool calls are present.
	if !strings.Contains(mdl, "→ [skill]") {
		t.Error("expected skill tool call arrow")
	}
	if !strings.Contains(mdl, "→ [bash]") {
		t.Error("expected bash tool call arrow")
	}

	// Write output file.
	outPath := "../../kimi_2.6_compact.md"
	if err := os.WriteFile(outPath, []byte(mdl), 0644); err != nil {
		t.Fatalf("write output: %v", err)
	}
	t.Logf("Written to %s (%d bytes)", outPath, len(mdl))
}

func TestIsSkillDocumentation(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{"short text", "short", false},
		{"long without heading", strings.Repeat("a", 2000), false},
		{"skill doc", "# 收租工作流\n" + strings.Repeat("x", 2000), true},
		{"water meter doc", "# Water/Electric Meter Reading\n" + strings.Repeat("x", 2000), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isSkillDocumentation(tt.content)
			if got != tt.want {
				t.Errorf("isSkillDocumentation() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsExploratoryOrShortResult(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{"empty", "", true},
		{"short mkdir", "", true},
		{"tiny echo", "done", true},
		{"directory listing", "total 12\ndrwxr-xr-x 2 jekin root 4096 Apr 29 15:50 .", true},
		{"real result", "Room 101, amount 730.00", false},
		{"csv content", "Room,Water,Electricity\n101,10,20", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isExploratoryOrShortResult(tt.content)
			if got != tt.want {
				t.Errorf("isExploratoryOrShortResult() = %v, want %v", got, tt.want)
			}
		})
	}
}
