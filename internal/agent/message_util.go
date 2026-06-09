package agent

import (
	"strings"

	"github.com/Jekinnnnnn/jekylan/internal/message"
)

// dropOrphanToolUses returns a new slice with assistant messages removed when
// they contain a tool_use block that has no matching tool_result. The model
// would otherwise see an unfinished tool call and try to continue it instead
// of producing a summary.
func dropOrphanToolUses(msgs []message.Message) []message.Message {
	resultIDs := make(map[string]struct{})
	for _, m := range msgs {
		for _, r := range m.ToolResults() {
			resultIDs[r.ToolUseID] = struct{}{}
		}
	}
	out := make([]message.Message, 0, len(msgs))
	for _, m := range msgs {
		if m.Role == message.RoleAssistant {
			orphan := false
			for _, u := range m.ToolUses() {
				if _, ok := resultIDs[u.ID]; !ok {
					orphan = true
					break
				}
			}
			if orphan {
				continue
			}
		}
		out = append(out, m)
	}
	return out
}

// extractLastResultText walks backwards through messages and returns the text
// content of the most recent assistant message that has non-empty text. If the
// assistant message has no text but contains tool_use blocks, it falls back to
// the corresponding tool_result content.
func extractLastResultText(msgs []message.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != message.RoleAssistant {
			continue
		}
		text := strings.TrimSpace(msgs[i].TextContent())
		if text != "" {
			return text
		}
		// No text but has tool uses: look for matching tool results in
		// subsequent user messages.
		toolUses := msgs[i].ToolUses()
		if len(toolUses) > 0 {
			resultMap := make(map[string]string)
			for j := i + 1; j < len(msgs); j++ {
				if msgs[j].Role != message.RoleUser {
					continue
				}
				for _, tr := range msgs[j].ToolResults() {
					if _, ok := resultMap[tr.ToolUseID]; !ok {
						resultMap[tr.ToolUseID] = tr.Content
					}
				}
			}
			var b strings.Builder
			for _, tu := range toolUses {
				if content, ok := resultMap[tu.ID]; ok {
					if b.Len() > 0 {
						b.WriteString("\n")
					}
					b.WriteString(content)
				}
			}
			if b.Len() > 0 {
				return b.String()
			}
		}
	}
	return ""
}

// extractUsage returns the usage from the last assistant message that has one.
func extractUsage(msgs []message.Message) *message.Usage {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Usage != nil {
			return msgs[i].Usage
		}
	}
	return nil
}