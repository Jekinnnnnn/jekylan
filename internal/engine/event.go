package engine

import (
	"fmt"
	"strings"

	"github.com/Jekinnnnnn/jekylan/internal/message"
)

// EngineEventType identifies the kind of event emitted by Engine.Turn.
type EngineEventType string

const (
	EventTextDelta    EngineEventType = "text_delta"
	EventToolUse      EngineEventType = "tool_use"
	EventUsage        EngineEventType = "usage"
	EventUserMessage  EngineEventType = "user_message"
	EventCompacted    EngineEventType = "compacted"
	EventTurnResult   EngineEventType = "turn_result"
	EventTurnError    EngineEventType = "turn_error"
	EventNotification EngineEventType = "notification"
)

// ToolUseInfo captures an assistant tool invocation.
type ToolUseInfo struct {
	ToolUseID string
	ToolName  string
	ToolInput map[string]any
}

// TurnResult captures the terminal state of a turn.
type TurnResult struct {
	Success    bool
	Text       string
	StopReason string
	NumTurns   int
	Error      string
}

// EngineEvent is the unified event stream produced by Engine.Turn.
// It is independent of query.Event so transport layers can serialize
// it without importing the query package.
type EngineEvent struct {
	Type EngineEventType

	// TextDelta is non-empty streaming text (assistant_text aggregated).
	TextDelta string

	// ToolUse is set for EventToolUse.
	ToolUse ToolUseInfo

	// Usage is set for EventUsage.
	Usage *message.Usage

	// Message is set for EventUserMessage (tool results).
	Message message.Message

	// Result is set for EventTurnResult.
	Result TurnResult

	// Error is set for EventTurnError.
	Error string

	// CompactedMsgCount is set for EventCompacted.
	CompactedMsgCount int

	// Notification is set for EventNotification.
	Notification string
}

// FormatEvent converts an EngineEvent into a human-readable string.
// Returns empty string for events that have no direct text representation.
func FormatEvent(evt EngineEvent) string {
	switch evt.Type {
	case EventTextDelta:
		return evt.TextDelta
	case EventToolUse:
		return fmt.Sprintf("\n[Tool use: %s(%s)]\n", evt.ToolUse.ToolName, evt.ToolUse.ToolUseID)
	case EventUsage:
		u := evt.Usage
		total := u.InputTokens + u.OutputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens
		return fmt.Sprintf("\n[Tokens: input=%d output=%d cache_create=%d cache_read=%d total=%d]\n",
			u.InputTokens, u.OutputTokens, u.CacheCreationInputTokens, u.CacheReadInputTokens, total)
	case EventUserMessage:
		var sb strings.Builder
		sb.WriteString("\n=== Tool Results ===\n")
		for _, block := range evt.Message.Content {
			if b, ok := block.(message.ToolResultBlock); ok {
				fmt.Fprintf(&sb, "[%s] %s\n", b.ToolUseID, b.Content)
			}
		}
		sb.WriteString("=== Assistant ===\n")
		return sb.String()
	case EventCompacted:
		return fmt.Sprintf("Session compacted: %d messages\n", evt.CompactedMsgCount)
	case EventTurnResult:
		return "\n"
	case EventTurnError:
		return fmt.Sprintf("\nError: %s\n", evt.Error)
	case EventNotification:
		return "\n" + evt.Notification + "\n"
	}
	return ""
}
