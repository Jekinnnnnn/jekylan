package memory

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Jekinnnnnn/jekylan/internal/message"
)

// CompactMessages applies deletion rules to filter noise from conversation
// messages and returns the result as a Markdown Dialogue Log (MDL) string.
func CompactMessages(msgs []message.Message) string {
	var sb strings.Builder

	// Extract metadata.
	skillName := extractSkillName(msgs)
	date := extractDate(msgs)
	startTime, endTime := extractTimeRange(msgs)

	// Frontmatter.
	fmt.Fprintf(&sb, "---\n")
	fmt.Fprintf(&sb, "skill: %s\n", skillName)
	fmt.Fprintf(&sb, "date: %s\n", date)
	fmt.Fprintf(&sb, "processing_time: %s→%s\n", startTime, endTime)
	fmt.Fprintf(&sb, "---\n\n")
	fmt.Fprintf(&sb, "# 对话记录\n\n")

	// Filter and format each message.
	for i := 0; i < len(msgs); i++ {
		m := msgs[i]
		compacted := compactMessage(m)
		if compacted == nil {
			continue
		}

		// Detect batch of repeated tool results and merge them.
		if isRepeatedToolResultStart(compacted) {
			merged, consumed := mergeRepeatedToolResults(msgs[i:])
			if merged != nil {
				formatMessageToMDL(&sb, *merged)
				i += consumed - 1
				continue
			}
		}

		formatMessageToMDL(&sb, *compacted)
	}

	return sb.String()
}

// formatMessageToMDL writes a single message in MDL format.
func formatMessageToMDL(sb *strings.Builder, m message.Message) {
	switch m.Role {
	case message.RoleUser:
		if hasToolResultOnly(m) {
			for _, c := range m.Content {
				if tr, ok := c.(message.ToolResultBlock); ok {
					content := cleanToolResult(tr.Content)
					if tr.IsError {
						content = "[ERROR] " + content
					}
					fmt.Fprintf(sb, "## [user:tool_result]\n%s\n\n", content)
				}
			}
		} else {
			for _, c := range m.Content {
				if t, ok := c.(message.TextBlock); ok {
					fmt.Fprintf(sb, "## [user]\n%s\n\n", t.Text)
				}
			}
		}

	case message.RoleAssistant:
		fmt.Fprintf(sb, "## [assistant]\n")
		for _, c := range m.Content {
			switch block := c.(type) {
			case message.TextBlock:
				fmt.Fprintf(sb, "%s\n", block.Text)
			case message.ToolUseBlock:
				fmt.Fprintf(sb, "→ [%s] %s\n", block.Name, formatToolInput(block))
			}
		}
		fmt.Fprintf(sb, "\n")
	}
}

// cleanToolResult removes escape sequences and collapses excessive whitespace.
func cleanToolResult(content string) string {
	// Replace literal escape sequences (from nested JSON, etc.).
	content = strings.ReplaceAll(content, `\n`, "\n")
	content = strings.ReplaceAll(content, `\t`, "\t")
	content = strings.ReplaceAll(content, `\"`, `"`)
	content = strings.ReplaceAll(content, `\\`, `\`)
	// Collapse multiple consecutive newlines to at most two.
	for strings.Contains(content, "\n\n\n") {
		content = strings.ReplaceAll(content, "\n\n\n", "\n\n")
	}
	return strings.TrimSpace(content)
}

// formatToolInput formats a tool_use block's input for MDL display.
func formatToolInput(block message.ToolUseBlock) string {
	switch block.Name {
	case "bash":
		if cmd, ok := block.Input["command"].(string); ok {
			return cmd
		}
	case "skill":
		var parts []string
		if skill, ok := block.Input["skill"].(string); ok && skill != "" {
			parts = append(parts, skill)
		}
		if args, ok := block.Input["args"].(string); ok && args != "" {
			parts = append(parts, args)
		}
		return strings.Join(parts, " ")
	default:
		inputJSON, _ := json.Marshal(block.Input)
		return string(inputJSON)
	}
	return ""
}

// extractSkillName finds the first skill tool_use to determine the skill name.
func extractSkillName(msgs []message.Message) string {
	for _, m := range msgs {
		if m.Role == message.RoleAssistant {
			for _, c := range m.Content {
				if tu, ok := c.(message.ToolUseBlock); ok && tu.Name == "skill" {
					if skill, ok := tu.Input["skill"].(string); ok && skill != "" {
						return skill
					}
				}
			}
		}
	}
	return "unknown"
}

// extractDate returns the date from the first message timestamp, or today.
func extractDate(msgs []message.Message) string {
	for _, m := range msgs {
		if !m.Timestamp.IsZero() {
			return m.Timestamp.Format("2006-01-02")
		}
	}
	return time.Now().Format("2006-01-02")
}

// extractTimeRange returns start and end times from message timestamps.
func extractTimeRange(msgs []message.Message) (string, string) {
	var first, last time.Time
	for _, m := range msgs {
		if !m.Timestamp.IsZero() {
			if first.IsZero() {
				first = m.Timestamp
			}
			last = m.Timestamp
		}
	}
	if first.IsZero() {
		now := time.Now().Format("15:04")
		return now, now
	}
	return first.Format("15:04"), last.Format("15:04")
}

// hasToolResultOnly reports whether a user message contains only tool_results.
func hasToolResultOnly(m message.Message) bool {
	if m.Role != message.RoleUser {
		return false
	}
	for _, c := range m.Content {
		if _, ok := c.(message.ToolResultBlock); !ok {
			return false
		}
	}
	return len(m.Content) > 0
}

// compactMessage strips metadata and filters content blocks per deletion rules.
func compactMessage(m message.Message) *message.Message {
	if len(m.Content) == 0 {
		return nil
	}

	out := message.Message{
		Role:      m.Role,
		Content:   nil,
		Timestamp: m.Timestamp,
	}

	for _, c := range m.Content {
		switch block := c.(type) {
		case message.TextBlock:
			out.Content = append(out.Content, block)
		case message.ToolUseBlock:
			out.Content = append(out.Content, block)
		case message.ToolResultBlock:
			if compacted := compactToolResult(block); compacted != nil {
				out.Content = append(out.Content, *compacted)
			}
		case message.ThinkingBlock:
			// Skip thinking blocks.
		case message.RedactedThinkingBlock:
			// Skip redacted thinking blocks.
		}
	}

	if len(out.Content) == 0 {
		return nil
	}
	return &out
}

// compactToolResult filters individual tool results.
func compactToolResult(block message.ToolResultBlock) *message.ToolResultBlock {
	content := block.Content

	// Rule 1: Skill documentation returns (>1000 chars with markdown heading).
	if isSkillDocumentation(content) {
		return &message.ToolResultBlock{
			ToolUseID: block.ToolUseID,
			Content:   "[Skill documentation returned — content omitted]",
			IsError:   block.IsError,
		}
	}

	// Rule 2 & 3: Exploratory / empty / short bash results.
	if isExploratoryOrShortResult(content) {
		return nil
	}

	return &message.ToolResultBlock{
		ToolUseID: block.ToolUseID,
		Content:   content,
		IsError:   block.IsError,
	}
}

// isSkillDocumentation detects tool results that contain full skill docs.
func isSkillDocumentation(content string) bool {
	if len(content) <= 1000 {
		return false
	}
	// Check first few lines for markdown headings typical of skill docs.
	lines := strings.SplitN(content, "\n", 10)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") || strings.HasPrefix(trimmed, "## ") {
			return true
		}
	}
	return false
}

// isExploratoryOrShortResult detects bash results that are empty, very short,
// or look like directory listings.
func isExploratoryOrShortResult(content string) bool {
	if len(content) == 0 {
		return true
	}
	// Filter very short content (typical for mkdir, touch, echo, etc.)
	if len(content) < 20 {
		return true
	}
	if len(content) < 200 {
		// Check if it looks like a directory listing.
		lines := strings.Split(content, "\n")
		isDirList := true
		nonEmpty := 0
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			nonEmpty++
			if nonEmpty == 1 && strings.HasPrefix(line, "total ") {
				continue
			}
			if len(line) < 10 {
				continue // too short to be a real file listing
			}
			// Typical ls -la line starts with d, -, l, or total
			first := line[0]
			if first != 'd' && first != '-' && first != 'l' && first != 't' {
				isDirList = false
				break
			}
		}
		if isDirList && nonEmpty > 0 {
			return true
		}
	}
	return false
}

// isRepeatedToolResultStart checks if a message looks like the start of a batch
// of structurally identical tool results (e.g. multiple todo creations).
func isRepeatedToolResultStart(m *message.Message) bool {
	if m.Role != message.RoleUser {
		return false
	}
	results := m.ToolResults()
	if len(results) != 1 {
		return false
	}
	content := results[0].Content
	// Heuristic: contains success markers and an ID-like field.
	if strings.Contains(content, "\"id\"") && strings.Contains(content, "success") {
		return true
	}
	if strings.Contains(content, "todo_id") || strings.Contains(content, "record_id") {
		return true
	}
	return false
}

// mergeRepeatedToolResults scans forward and merges consecutive user messages
// that each contain a single similar tool result into one combined message.
func mergeRepeatedToolResults(msgs []message.Message) (*message.Message, int) {
	if len(msgs) == 0 {
		return nil, 0
	}

	var merged []message.ToolResultBlock
	consumed := 0

	for i := range msgs {
		m := msgs[i]
		if m.Role != message.RoleUser {
			break
		}
		results := m.ToolResults()
		if len(results) != 1 {
			break
		}
		merged = append(merged, results[0])
		consumed++
	}

	if consumed <= 1 {
		return nil, 0
	}

	out := message.Message{Role: message.RoleUser}
	out.AddToolResult(merged[0].ToolUseID,
		fmt.Sprintf("[Batch of %d similar tool results merged]\n%s", consumed, merged[0].Content),
		merged[0].IsError)
	return &out, consumed
}
