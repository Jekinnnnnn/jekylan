package query

import (
	"encoding/json"
	"time"

	"github.com/Jekinnnnnn/jekylan/internal/llm"
	"github.com/Jekinnnnnn/jekylan/internal/message"
)

// streamParser encapsulates the state machine that turns a stream of
// llm.StreamEvent values into an assistant message and side-effect events.
// It handles both Anthropic (content_block_*) and OpenAI (assistant_*)
// streaming conventions.
type streamParser struct {
	out                  chan<- Event
	assistantMsg         message.Message
	currentText          string
	currentToolUse       *message.ToolUseBlock
	currentToolInputJSON string
	currentThinking      string
	currentSignature     string
	currentRedacted      string
	currentBlockType     string
	currentUsage         *message.Usage
	currentResponseID    string
	hasToolUse           bool
	stopReason           string
	isPTL                bool
	debug                bool
}

func newStreamParser(out chan<- Event, debug bool) *streamParser {
	return &streamParser{
		out:          out,
		assistantMsg: message.Message{Role: message.RoleAssistant},
		debug:        debug,
	}
}

// process consumes a single stream event and updates the parser state.
// It returns breakLoop=true when the caller should stop reading from the
// stream (e.g. prompt-too-long detected inside the stream). It returns
// abortErr non-empty when the entire query goroutine should abort.
func (sp *streamParser) process(evt llm.StreamEvent) (breakLoop bool, abortErr string) {
	switch evt.Type {
	case StreamEventTypeMessageStart:
		if evt.ResponseID != "" {
			sp.currentResponseID = evt.ResponseID
		}
		if u := usageFromStreamEvent(evt); u != nil {
			sp.currentUsage = mergeUsage(sp.currentUsage, u)
		}
	case StreamEventTypeUsage:
		if u := usageFromStreamEvent(evt); u != nil {
			sp.currentUsage = mergeUsage(sp.currentUsage, u)
		}
	case StreamEventTypeContentBlockStart:
		sp.currentBlockType = evt.BlockType
		switch evt.BlockType {
		case BlockTypeText:
			sp.currentText = ""
		case BlockTypeToolUse:
			sp.currentToolUse = &message.ToolUseBlock{
				ID:   evt.BlockID,
				Name: evt.BlockName,
			}
			if evt.BlockInput != nil {
				sp.currentToolUse.Input = evt.BlockInput
			}
			sp.currentToolInputJSON = ""
		case BlockTypeThinking:
			sp.currentThinking = evt.BlockThinking
			sp.currentSignature = evt.BlockSignature
		case BlockTypeRedactedThinking:
			sp.currentRedacted = evt.BlockRedacted
		}
	case StreamEventTypeAssistantText:
		if evt.TextDelta != "" {
			sp.currentText += evt.TextDelta
			sp.out <- Event{Type: EventTypeAssistantText, Text: evt.TextDelta}
		}
	case StreamEventTypeAssistantToolUse:
		if evt.ResponseID != "" && sp.currentResponseID == "" {
			sp.currentResponseID = evt.ResponseID
		}
		if sp.currentToolUse == nil || sp.currentToolUse.ID != evt.ToolUseID {
			if sp.currentToolUse != nil {
				var finalized bool
				sp.currentToolUse, finalized = sp.finalizeToolUse()
				if finalized {
					sp.hasToolUse = true
				}
			}
			sp.currentToolUse = &message.ToolUseBlock{
				ID:   evt.ToolUseID,
				Name: evt.ToolName,
			}
			sp.currentToolInputJSON = evt.InputJSON
		} else {
			sp.currentToolInputJSON += evt.InputJSON
		}
	case StreamEventTypeContentBlockDelta:
		if evt.TextDelta != "" {
			sp.currentText += evt.TextDelta
			sp.out <- Event{Type: EventTypeAssistantText, Text: evt.TextDelta}
		} else if evt.InputJSON != "" && sp.currentToolUse != nil {
			sp.currentToolInputJSON += evt.InputJSON
		} else if evt.ThinkingDelta != "" {
			sp.currentThinking += evt.ThinkingDelta
		} else if evt.SignatureDelta != "" {
			sp.currentSignature += evt.SignatureDelta
		}
	case StreamEventTypeContentBlockStop:
		switch sp.currentBlockType {
		case BlockTypeToolUse:
			var finalized bool
			sp.currentToolUse, finalized = sp.finalizeToolUse()
			if finalized {
				sp.hasToolUse = true
			}
			sp.currentToolInputJSON = ""
		case BlockTypeThinking:
			sp.assistantMsg.AddThinking(sp.currentThinking, sp.currentSignature)
			sp.currentThinking = ""
			sp.currentSignature = ""
		case BlockTypeRedactedThinking:
			sp.assistantMsg.AddRedactedThinking(sp.currentRedacted)
			sp.currentRedacted = ""
		default:
			sp.assistantMsg.AddText(sp.currentText)
			sp.currentText = ""
		}
		sp.currentBlockType = ""
	case StreamEventTypeMessageDelta:
		if evt.StopReason != "" {
			sp.stopReason = evt.StopReason
		}
		if u := usageFromStreamEvent(evt); u != nil {
			sp.currentUsage = mergeUsage(sp.currentUsage, u)
		}
	case StreamEventTypeError:
		if llm.IsPromptTooLongErrorString(evt.TextDelta) {
			sp.assistantMsg = newPromptTooLongAssistantMessage(evt.TextDelta)
			sp.isPTL = true
			return true, ""
		}
		return false, evt.TextDelta
	}
	return false, ""
}

// flushOpenAI finalizes any accumulated state that was never closed by
// content_block_stop. OpenAI streams omit content_block_start/stop, so
// pending text and tool_use blocks must be flushed after the stream ends.
func (sp *streamParser) flushOpenAI() {
	if sp.currentText != "" {
		sp.assistantMsg.AddText(sp.currentText)
		sp.currentText = ""
	}
	if sp.currentToolUse != nil {
		var finalized bool
		sp.currentToolUse, finalized = sp.finalizeToolUse()
		if finalized {
			sp.hasToolUse = true
		}
		sp.currentToolInputJSON = ""
	}
}

// finalizeAssistantMsg stamps usage/responseID/timestamp onto the assistant
// message and returns it.
func (sp *streamParser) finalizeAssistantMsg() message.Message {
	sp.assistantMsg.Usage = sp.currentUsage
	sp.assistantMsg.ResponseID = sp.currentResponseID
	sp.assistantMsg.Timestamp = time.Now()
	return sp.assistantMsg
}

// finalizeToolUse parses accumulated tool input JSON, adds the tool_use to
// the assistant message, and emits an event. Returns true if a tool_use was
// finalized.
func (sp *streamParser) finalizeToolUse() (*message.ToolUseBlock, bool) {
	if sp.currentToolUse == nil {
		return nil, false
	}
	if sp.currentToolInputJSON != "" {
		var inputMap map[string]any
		if err := json.Unmarshal([]byte(sp.currentToolInputJSON), &inputMap); err != nil {
			debugLog(sp.debug, "tool input JSON parse error: %v\n", err)
		} else {
			sp.currentToolUse.Input = inputMap
		}
	}
	sp.assistantMsg.AddToolUse(sp.currentToolUse.ID, sp.currentToolUse.Name, sp.currentToolUse.Input)
	sp.out <- Event{
		Type:      EventTypeAssistantToolUse,
		ToolUseID: sp.currentToolUse.ID,
		ToolName:  sp.currentToolUse.Name,
		ToolInput: sp.currentToolUse.Input,
	}
	return nil, true
}
