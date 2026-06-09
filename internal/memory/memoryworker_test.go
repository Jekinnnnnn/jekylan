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
		Messages []message.Message `json:"messages"`
	}
	if err := json.Unmarshal(b, &data); err != nil {
		t.Fatalf("parse session: %v", err)
	}
	return data.Messages
}

func TestCompactMessagesWithSessionMock(t *testing.T) {
	// Build a realistic conversation that exercises all compaction rules.
	ts := time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC)
	msgs := []message.Message{
		{Role: message.RoleUser, Timestamp: ts},
	}
	msgs[0].AddText("run the shouzu skill")

	assistant1 := message.Message{Role: message.RoleAssistant, Timestamp: ts.Add(time.Minute)}
	assistant1.AddToolUse("tu1", "skill", map[string]any{"skill": "shouzu", "args": "check"})
	msgs = append(msgs, assistant1)

	user1 := message.Message{Role: message.RoleUser, Timestamp: ts.Add(2 * time.Minute)}
	user1.AddToolResult("tu1", "Room 101 rent is 730.00", false)
	msgs = append(msgs, user1)

	assistant2 := message.Message{Role: message.RoleAssistant, Timestamp: ts.Add(3 * time.Minute)}
	assistant2.AddText("Results fetched")
	msgs = append(msgs, assistant2)

	mdl := CompactMessages(msgs)

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
	if !strings.Contains(mdl, "→ [skill]") {
		t.Error("expected skill tool call arrow")
	}
	if !strings.Contains(mdl, "## [user:tool_result]") {
		t.Error("expected tool_result section")
	}
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
