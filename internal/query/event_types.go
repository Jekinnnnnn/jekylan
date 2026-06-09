package query

// Event types emitted by the Query loop.
const (
	EventTypeError            = "error"
	EventTypeResult           = "result"
	EventTypeUsage            = "usage"
	EventTypeUserMessage      = "user_message"
	EventTypeAssistantText    = "assistant_text"
	EventTypeAssistantToolUse = "assistant_tool_use"
	EventTypeCompactionResult = "compaction_result"
)

// Stream event types consumed from llm.StreamEvent.
const (
	StreamEventTypeMessageStart      = "message_start"
	StreamEventTypeUsage             = "usage"
	StreamEventTypeContentBlockStart = "content_block_start"
	StreamEventTypeAssistantText     = "assistant_text"
	StreamEventTypeAssistantToolUse  = "assistant_tool_use"
	StreamEventTypeContentBlockDelta = "content_block_delta"
	StreamEventTypeContentBlockStop  = "content_block_stop"
	StreamEventTypeMessageDelta      = "message_delta"
	StreamEventTypeError             = "error"
)

// Content block types.
const (
	BlockTypeText             = "text"
	BlockTypeToolUse          = "tool_use"
	BlockTypeThinking         = "thinking"
	BlockTypeRedactedThinking = "redacted_thinking"
)

// Stop reasons.
const (
	StopReasonMaxOutputTokens = "max_output_tokens"
	StopReasonToolUse         = "tool_use"
	StopReasonToolCalls       = "tool_calls"
	StopReasonStop            = "stop"
	StopReasonEndTurn         = "end_turn"
	StopReasonPromptTooLong   = "prompt_too_long"
)
