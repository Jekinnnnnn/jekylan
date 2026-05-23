package agent

import (
	"maps"
	"time"

	"github.com/Jekinnnnnn/jekylan/internal/message"
)

// BuildForkMessages constructs a message list for a forked sub-agent.
// It deep-copies the parent's messages, appends a synthetic tool_result
// for any orphan tool_use blocks in the last assistant message, and adds
// a directive user message.
func BuildForkMessages(parentMsgs []message.Message, directive string) []message.Message {
	// Deep copy parent messages.
	result := make([]message.Message, len(parentMsgs))
	for i, m := range parentMsgs {
		result[i] = deepCopyMessage(m)
	}

	// If the last message is an assistant with tool_use blocks, append
	// synthetic tool_results so the sub-agent sees a complete turn.
	if len(result) > 0 {
		last := result[len(result)-1]
		if last.Role == message.RoleAssistant {
			toolUses := last.ToolUses()
			if len(toolUses) > 0 {
				synthMsg := message.Message{Role: message.RoleUser, Timestamp: time.Now()}
				for _, tu := range toolUses {
					synthMsg.AddToolResult(tu.ID, "[Agent spawned successfully]", false)
				}
				result = append(result, synthMsg)
			}
		}
	}

	// Append the directive as a user message.
	directiveMsg := message.Message{Role: message.RoleUser, Timestamp: time.Now()}
	directiveMsg.AddText(directive)
	result = append(result, directiveMsg)

	return result
}

func deepCopyMessage(m message.Message) message.Message {
	copied := m
	copied.Content = make([]message.ContentBlock, len(m.Content))
	for i, block := range m.Content {
		copied.Content[i] = deepCopyBlock(block)
	}
	return copied
}

func deepCopyBlock(block message.ContentBlock) message.ContentBlock {
	switch b := block.(type) {
	case message.TextBlock:
		return message.TextBlock{Text: b.Text}
	case message.ToolUseBlock:
		inputCopy := make(map[string]any, len(b.Input))
		maps.Copy(inputCopy, b.Input)
		return message.ToolUseBlock{ID: b.ID, Name: b.Name, Input: inputCopy}
	case message.ToolResultBlock:
		return message.ToolResultBlock{ToolUseID: b.ToolUseID, Content: b.Content, IsError: b.IsError}
	case message.ThinkingBlock:
		return message.ThinkingBlock{Thinking: b.Thinking, Signature: b.Signature}
	case message.RedactedThinkingBlock:
		return message.RedactedThinkingBlock{Data: b.Data}
	default:
		return block
	}
}
