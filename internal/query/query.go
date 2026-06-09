package query

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/Jekinnnnnn/jekylan/internal/compact"
	"github.com/Jekinnnnnn/jekylan/internal/llm"
	"github.com/Jekinnnnnn/jekylan/internal/message"
	"github.com/Jekinnnnnn/jekylan/internal/tokens"
	"github.com/Jekinnnnnn/jekylan/internal/tool"
)

func debugLog(enabled bool, format string, args ...any) {
	if enabled {
		fmt.Fprintf(os.Stderr, "[jekylan-debug] "+format, args...)
	}
}

// ErrQueryAborted is returned when the query is interrupted via context cancellation.
var ErrQueryAborted = fmt.Errorf("query aborted")

// QuerySource identifies where a query originates.
type QuerySource string

const (
	QuerySourceEngine        QuerySource = "engine"
	QuerySourceAgent         QuerySource = "agent"
	QuerySourceCompact       QuerySource = "compact"
	QuerySourceSessionMemory QuerySource = "session_memory"
)

// Params holds the configuration for a single query loop.
type Params struct {
	Messages       []message.Message
	SystemPrompt   string
	Tools          *tool.Registry
	Model          string
	MaxTurns       int
	ThinkingBudget int64
	Client         llm.Client
	DisableCompact bool
	QuerySource    QuerySource
	// CacheBreakpoints controls Anthropic prompt caching breakpoints.
	// 0 = disabled, 1 = system, 2 = system+tools, 3 = system+tools+messages.
	CacheBreakpoints int
	// FallbackModel is the model to switch to when the primary model is
	// overloaded (Anthropic HTTP 529). Empty disables fallback.
	FallbackModel string
	// ConfirmTool is called before executing a risky tool. If non-nil and the
	// tool is flagged as risky, the callback blocks until the user approves or
	// rejects the operation. A returned error or !approved aborts the tool call.
	ConfirmTool func(ctx context.Context, toolName string, input map[string]any) (bool, error)
	// Debug enables verbose debug logging to stderr.
	Debug bool
}

// Event represents an output from the query loop.
type Event struct {
	Type string

	Text string

	ToolUseID string
	ToolName  string
	ToolInput map[string]any

	Message message.Message

	Result Result

	Messages []message.Message // compaction_result only: the compacted message list
}

// Result is the terminal event of a query loop.
type Result struct {
	Success    bool
	Text       string
	StopReason string
	NumTurns   int
	Error      string
}

// Query runs the multi-turn conversation loop with the LLM API.
func Query(ctx context.Context, params Params) <-chan Event {
	out := make(chan Event)

	go func() {
		defer close(out)

		client := params.Client
		if client == nil {
			out <- Event{Type: EventTypeError, Result: Result{Error: "LLM client is nil"}}
			return
		}

		msgs := append([]message.Message(nil), params.Messages...)
		var autoCompactTracking compact.AutoCompactTrackingState
		turnCount := 1

		for {
			if err := ctx.Err(); err != nil {
				out <- Event{Type: EventTypeError, Result: Result{Error: ErrQueryAborted.Error()}}
				return
			}

			if params.MaxTurns > 0 && turnCount > params.MaxTurns {
				out <- Event{
					Type: EventTypeResult,
					Result: Result{
						Success:  false,
						Error:    fmt.Sprintf("Reached maximum number of turns (%d)", params.MaxTurns),
						NumTurns: turnCount,
					},
				}
				return
			}

			// --- Compaction pipeline ---
			var err error
			msgs, _, err = runCompactionPipeline(ctx, msgs, params, turnCount, &autoCompactTracking, out)
			if err != nil {
				out <- Event{Type: EventTypeError, Result: Result{Error: err.Error()}}
				return
			}

			// --- Streaming with fallback model retry ---
			attemptWithFallback := true
			var assistantMsg message.Message
			var hasToolUse bool
			var stopReason string
			var isPTL bool

			for attemptWithFallback {
				attemptWithFallback = false
				isPTL = false

				stream, err := client.StreamMessages(ctx, msgs, params.SystemPrompt, params.Tools, params.ThinkingBudget, params.CacheBreakpoints)
				if err != nil {
					if llm.IsPromptTooLongError(err) {
						assistantMsg = newPromptTooLongAssistantMessage(err.Error())
						isPTL = true
						break
					}
					if llm.IsFallbackError(err) && params.FallbackModel != "" {
						if ms, ok := client.(llm.ModelSwitcher); ok {
							ms.SetModel(params.FallbackModel)
						}
						attemptWithFallback = true
						continue
					}
					out <- Event{Type: EventTypeError, Result: Result{Error: err.Error()}}
					return
				}

				parser := newStreamParser(out, params.Debug)
				for evt := range stream {
					if err := ctx.Err(); err != nil {
						out <- Event{Type: EventTypeError, Result: Result{Error: ErrQueryAborted.Error()}}
						return
					}
					breakLoop, abortErr := parser.process(evt)
					if abortErr != "" {
						out <- Event{Type: EventTypeError, Result: Result{Error: abortErr}}
						return
					}
					if breakLoop {
						break
					}
				}

				isPTL = parser.isPTL
				if isPTL {
					break
				}

				parser.flushOpenAI()
				assistantMsg = parser.finalizeAssistantMsg()
				hasToolUse = parser.hasToolUse
				stopReason = parser.stopReason
			}

			if !isPTL {
				msgs = append(msgs, assistantMsg)
				out <- Event{Type: EventTypeUsage, Message: assistantMsg}
			}

			// --- Recovery paths ---
			if isPTL {
				if newMsgs, ok := maybeRecoverPromptTooLong(msgs, assistantMsg, turnCount, out); ok {
					msgs = newMsgs
					continue
				}
				return
			}

			if newMsgs, ok := maybeRecoverMaxOutputTokens(msgs, hasToolUse, stopReason, out); ok {
				msgs = newMsgs
				continue
			}

			if !hasToolUse {
				if stopReason == "" {
					stopReason = StopReasonEndTurn
				}
				out <- Event{
					Type: EventTypeResult,
					Result: Result{
						Success:    true,
						Text:       assistantMsg.TextContent(),
						StopReason: stopReason,
						NumTurns:   turnCount,
					},
				}
				return
			}

			if err := ctx.Err(); err != nil {
				if hasToolUse {
					synthMsg := message.Message{Role: message.RoleUser, Timestamp: time.Now()}
					for _, tu := range assistantMsg.ToolUses() {
						synthMsg.AddToolResult(tu.ID, "[Tool execution interrupted]", true)
					}
					msgs = append(msgs, synthMsg)
					out <- Event{Type: EventTypeUserMessage, Message: synthMsg}
				}
				out <- Event{Type: EventTypeError, Result: Result{Error: ErrQueryAborted.Error()}}
				return
			}

			userMsg := executeTools(ctx, assistantMsg.ToolUses(), params)
			msgs = append(msgs, userMsg)
			out <- Event{Type: EventTypeUserMessage, Message: userMsg}
			turnCount++
		}
	}()

	return out
}

// runCompactionPipeline runs snip, microcompact, auto-compact, and the blocking
// limit check. It emits a compaction_result event when auto-compact fires.
// Returns the (possibly) updated message slice, whether compaction occurred, and
// any fatal error that should abort the query loop.
func runCompactionPipeline(ctx context.Context, msgs []message.Message, params Params, turnCount int, autoCompactTracking *compact.AutoCompactTrackingState, out chan<- Event) (newMsgs []message.Message, compacted bool, err error) {
	var snipResult compact.SnipResult
	if !params.DisableCompact {
		snipResult = compact.SnipCompactIfNeeded(msgs)
		msgs = snipResult.Messages

		mcResult := compact.MicrocompactMessages(msgs)
		if mcResult.Changed {
			debugLog(params.Debug, "tri micro compact\n")
			for _, m := range mcResult.Messages {
				for _, block := range m.Content {
					if tr, ok := block.(message.ToolResultBlock); ok {
						debugLog(params.Debug, "micro tool_result %s: %s\n", tr.ToolUseID, tr.Content)
					}
				}
			}
			msgs = mcResult.Messages
		}

		if compact.ShouldAutoCompact(ctx, msgs, params.Model, params.Client, compact.QuerySource(params.QuerySource), snipResult.TokensFreed) {
			debugLog(params.Debug, "auto-compact starting (turn %d)\n", turnCount)
			wasCompacted, compactResult, nextFailures, compactErr := compact.AutoCompactIfNeeded(ctx, msgs, params.Model, params.Client, compact.QuerySource(params.QuerySource), autoCompactTracking, snipResult.TokensFreed)
			autoCompactTracking.ConsecutiveFailures = nextFailures
			if compactErr != nil {
				debugLog(params.Debug, "auto-compact error: %v\n", compactErr)
				return nil, false, fmt.Errorf("autocompact failed: %w", compactErr)
			}
			if wasCompacted && compactResult != nil {
				debugLog(params.Debug, "auto-compact success: %d -> %d messages\n", len(msgs), len(compactResult.Messages))
				msgs = compactResult.Messages
				out <- Event{Type: EventTypeCompactionResult, Messages: compactResult.Messages}
				autoCompactTracking.Compacted = true
				autoCompactTracking.TurnCounter = turnCount
				autoCompactTracking.TurnID = fmt.Sprintf("turn-%d", turnCount)
				compacted = true
			}
		} else {
			autoCompactTracking.ConsecutiveFailures = 0
		}
	}

	if !compacted &&
		params.QuerySource != QuerySourceCompact &&
		params.QuerySource != QuerySourceSessionMemory {
		tokenCount := tokens.TokenCountWithEstimation(msgs) - snipResult.TokensFreed
		state := compact.CalculateTokenWarningState(tokenCount, params.Model)
		if state.IsAtBlockingLimit {
			return nil, false, fmt.Errorf("Context is too large. Run /compact to reduce context size.")
		}
	}

	return msgs, compacted, nil
}

// executeTools runs all tool_use blocks in parallel and returns a single user
// message containing the ordered tool_result blocks.
func executeTools(ctx context.Context, toolUses []message.ToolUseBlock, params Params) message.Message {
	type indexedToolResult struct {
		idx       int
		toolUseID string
		result    string
		isErr     bool
	}
	resultCh := make(chan indexedToolResult, len(toolUses))

	var wg sync.WaitGroup
	for i, tu := range toolUses {
		wg.Add(1)
		go func(idx int, block message.ToolUseBlock) {
			defer wg.Done()
			t := params.Tools.Find(block.Name)
			var result string
			var isErr bool
			if t == nil {
				result = fmt.Sprintf("Tool %q not found", block.Name)
				isErr = true
			} else {
				if params.ConfirmTool != nil && params.Tools.IsRisky(block.Name) {
					approved, err := params.ConfirmTool(ctx, block.Name, block.Input)
					if err != nil {
						result = fmt.Sprintf("confirmation cancelled: %v", err)
						isErr = true
					} else if !approved {
						result = fmt.Sprintf("tool %q was not approved by user", block.Name)
						isErr = true
					}
				}
				if !isErr {
					r, err := t.Call(ctx, block.Input)
					if err != nil {
						result = err.Error()
						isErr = true
					} else {
						result = r
					}
				}
			}
			resultCh <- indexedToolResult{idx: idx, toolUseID: block.ID, result: result, isErr: isErr}
		}(i, tu)
	}
	wg.Wait()
	close(resultCh)

	sorted := make([]indexedToolResult, len(toolUses))
	for r := range resultCh {
		sorted[r.idx] = r
	}

	userMsg := message.Message{Role: message.RoleUser, Timestamp: time.Now()}
	for _, r := range sorted {
		userMsg.AddToolResult(r.toolUseID, r.result, r.isErr)
	}
	return userMsg
}

// maybeRecoverPromptTooLong attempts to recover from a prompt-too-long error by
// reactively compacting the message history. If recovery succeeds it returns
// the new message slice and true. If recovery fails it emits a terminal result
// event and returns nil, false.
func maybeRecoverPromptTooLong(msgs []message.Message, assistantMsg message.Message, turnCount int, out chan<- Event) ([]message.Message, bool) {
	recoveryResult := compact.ReactiveCompactOnPromptTooLong(msgs, nil)
	if recoveryResult.OK && recoveryResult.Result != nil {
		return recoveryResult.Result.Messages, true
	}
	out <- Event{
		Type: EventTypeResult,
		Result: Result{
			Success:    false,
			Error:      assistantMsg.TextContent(),
			StopReason: StopReasonPromptTooLong,
			NumTurns:   turnCount,
		},
	}
	return nil, false
}

// maybeRecoverMaxOutputTokens handles the max_output_tokens stop reason by
// injecting a user message that asks the model to continue. It returns the
// updated message slice and true when recovery was triggered.
func maybeRecoverMaxOutputTokens(msgs []message.Message, hasToolUse bool, stopReason string, out chan<- Event) ([]message.Message, bool) {
	if hasToolUse || stopReason != StopReasonMaxOutputTokens {
		return msgs, false
	}
	recoveryMsg := message.Message{Role: message.RoleUser, Timestamp: time.Now()}
	recoveryMsg.AddText(
		"Output token limit hit. Resume directly — no apology, no recap of what you were doing. " +
			"Pick up mid-thought if that is where the cut happened. Break remaining work into smaller pieces.",
	)
	msgs = append(msgs, recoveryMsg)
	out <- Event{Type: EventTypeUserMessage, Message: recoveryMsg}
	return msgs, true
}

func newPromptTooLongAssistantMessage(details string) message.Message {
	msg := message.Message{Role: message.RoleAssistant, Timestamp: time.Now()}
	msg.AddText("Prompt is too long")
	msg.APIError = "invalid_request"
	msg.ErrorDetails = details
	return msg
}

// usageFromStreamEvent extracts a Usage struct from a StreamEvent.
// It prefers the explicit Usage pointer, but falls back to the legacy
// UsagePromptTokens / UsageCompletionTokens / UsageTotalTokens fields
// for providers that do not populate the unified Usage field.
func usageFromStreamEvent(evt llm.StreamEvent) *message.Usage {
	if evt.Usage != nil {
		return evt.Usage
	}
	if evt.UsagePromptTokens > 0 || evt.UsageCompletionTokens > 0 || evt.UsageTotalTokens > 0 {
		return &message.Usage{
			InputTokens:  evt.UsagePromptTokens,
			OutputTokens: evt.UsageCompletionTokens,
		}
	}
	return nil
}

// mergeUsage merges non-zero fields from src into dst using overwrite (not
// additive) semantics. If dst is nil, src is returned directly.
//
// Overwrite is correct for Anthropic: message_start reports input_tokens,
// message_delta reports output_tokens — fields are mutually exclusive across
// events. For OpenAI, the final usage chunk contains the complete breakdown,
// so a single merge call suffices.
func mergeUsage(dst, src *message.Usage) *message.Usage {
	if src == nil {
		return dst
	}
	if dst == nil {
		return src
	}
	if src.InputTokens != 0 {
		dst.InputTokens = src.InputTokens
	}
	if src.OutputTokens != 0 {
		dst.OutputTokens = src.OutputTokens
	}
	if src.CacheCreationInputTokens != 0 {
		dst.CacheCreationInputTokens = src.CacheCreationInputTokens
	}
	if src.CacheReadInputTokens != 0 {
		dst.CacheReadInputTokens = src.CacheReadInputTokens
	}
	return dst
}

