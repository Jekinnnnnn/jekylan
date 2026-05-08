package query

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/Jekinnnnnn/jekylan/internal/compact"
	"github.com/Jekinnnnnn/jekylan/internal/llm"
	"github.com/Jekinnnnnn/jekylan/internal/message"
	"github.com/Jekinnnnnn/jekylan/internal/tool"
)

// ErrQueryAborted is returned when the query is interrupted via context cancellation.
var ErrQueryAborted = fmt.Errorf("query aborted")

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
	QuerySource    string
	// FallbackModel is the model to switch to when the primary model is
	// overloaded (Anthropic HTTP 529). Empty disables fallback.
	FallbackModel string
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
			out <- Event{Type: "error", Result: Result{Error: "LLM client is nil"}}
			return
		}

		msgs := append([]message.Message(nil), params.Messages...)
		var autoCompactTracking compact.AutoCompactTrackingState
		turnCount := 1

		for {
			if err := ctx.Err(); err != nil {
				out <- Event{Type: "error", Result: Result{Error: ErrQueryAborted.Error()}}
				return
			}

			if params.MaxTurns > 0 && turnCount > params.MaxTurns {
				out <- Event{
					Type: "result",
					Result: Result{
						Success:  false,
						Error:    fmt.Sprintf("Reached maximum number of turns (%d)", params.MaxTurns),
						NumTurns: turnCount,
					},
				}
				return
			}

			// --- Compaction pipeline ---
			var snipResult compact.SnipResult
			compactedThisTurn := false
			if !params.DisableCompact {
				// 1. Snip (stub in MVP)
				snipResult = compact.SnipCompactIfNeeded(msgs)
				msgs = snipResult.Messages

				// 2. Microcompact
				mcResult := compact.MicrocompactMessages(msgs)
				if mcResult.Changed {
					fmt.Fprintf(os.Stderr, "[jekylan-debug] tri micro compact")
					for _, m := range mcResult.Messages {
						for _, block := range m.Content {
							if tr, ok := block.(message.ToolResultBlock); ok {
								fmt.Fprintf(os.Stderr, "[jekylan-debug] micro tool_result %s: %s\n", tr.ToolUseID, tr.Content)
							}
						}
					}
					msgs = mcResult.Messages
				}

				// 3. Auto-compact
				if compact.ShouldAutoCompact(ctx, msgs, params.Model, client, compact.QuerySource(params.QuerySource), snipResult.TokensFreed) {
					fmt.Fprintf(os.Stderr, "[jekylan-debug] auto-compact starting (turn %d)\n", turnCount)
					wasCompacted, compactResult, nextFailures, err := compact.AutoCompactIfNeeded(ctx, msgs, params.Model, client, compact.QuerySource(params.QuerySource), &autoCompactTracking, snipResult.TokensFreed)
					autoCompactTracking.ConsecutiveFailures = nextFailures
					if err != nil {
						fmt.Fprintf(os.Stderr, "[jekylan-debug] auto-compact error: %v\n", err)
						out <- Event{Type: "error", Result: Result{Error: fmt.Sprintf("autocompact failed: %v", err)}}
						return
					}
					if wasCompacted && compactResult != nil {
						fmt.Fprintf(os.Stderr, "[jekylan-debug] auto-compact success: %d -> %d messages\n", len(msgs), len(compactResult.Messages))
						msgs = compactResult.Messages
						out <- Event{Type: "compaction_result", Messages: compactResult.Messages}
						autoCompactTracking.Compacted = true
						autoCompactTracking.TurnCounter = turnCount
						autoCompactTracking.TurnID = fmt.Sprintf("turn-%d", turnCount)
						compactedThisTurn = true
					}
				} else {
					autoCompactTracking.ConsecutiveFailures = 0
				}
			}

			// Block if we've hit the hard blocking limit (only when auto-compact
			// is ON and compaction did not fire this turn). Skip for compact/
			// session_memory queries to avoid deadlocking forked agents.
			if !compactedThisTurn &&
				params.QuerySource != "compact" &&
				params.QuerySource != "session_memory" {
				tokenCount := compact.TokenCountWithEstimation(msgs) - snipResult.TokensFreed
				state := compact.CalculateTokenWarningState(tokenCount, params.Model)
				if state.IsAtBlockingLimit {
					out <- Event{Type: "error", Result: Result{Error: "Context is too large. Run /compact to reduce context size."}}
					return
				}
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

				stream, err := client.StreamMessages(ctx, msgs, params.SystemPrompt, params.Tools, params.ThinkingBudget)
				if err != nil {
					if llm.IsPromptTooLongError(err) {
						assistantMsg = newPromptTooLongAssistantMessage(err)
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
					out <- Event{Type: "error", Result: Result{Error: err.Error()}}
					return
				}

				assistantMsg = message.Message{Role: message.RoleAssistant}
				var currentText string
				var currentToolUse *message.ToolUseBlock
				var currentToolInputJSON string
				var currentThinking string
				var currentSignature string
				var currentRedacted string
				var currentBlockType string
				var currentUsage *message.Usage
				var currentResponseID string
				hasToolUse = false
				stopReason = ""

			streamLoop:
				for evt := range stream {
					if err := ctx.Err(); err != nil {
						out <- Event{Type: "error", Result: Result{Error: ErrQueryAborted.Error()}}
						return
					}
					switch evt.Type {
					case "message_start":
						if evt.ResponseID != "" {
							currentResponseID = evt.ResponseID
						}
						if evt.Usage != nil {
							currentUsage = mergeUsage(currentUsage, evt.Usage)
						}
					case "usage":
						if evt.Usage != nil {
							currentUsage = mergeUsage(currentUsage, evt.Usage)
						}
					case "content_block_start":
						currentBlockType = evt.BlockType
						switch evt.BlockType {
						case "text":
							currentText = ""
						case "tool_use":
							currentToolUse = &message.ToolUseBlock{
								ID:   evt.BlockID,
								Name: evt.BlockName,
							}
							if evt.BlockInput != nil {
								currentToolUse.Input = evt.BlockInput
							}
							currentToolInputJSON = ""
						case "thinking":
							currentThinking = evt.BlockThinking
							currentSignature = evt.BlockSignature
						case "redacted_thinking":
							currentRedacted = evt.BlockRedacted
						}
					case "assistant_text":
						if evt.TextDelta != "" {
							currentText += evt.TextDelta
							out <- Event{Type: "assistant_text", Text: evt.TextDelta}
						}
					case "assistant_tool_use":
						if evt.ResponseID != "" && currentResponseID == "" {
							currentResponseID = evt.ResponseID
						}
						if currentToolUse == nil || currentToolUse.ID != evt.ToolUseID {
							currentToolUse = &message.ToolUseBlock{
								ID:   evt.ToolUseID,
								Name: evt.ToolName,
							}
							currentToolInputJSON = ""
						}
						currentToolInputJSON += evt.InputJSON
					case "content_block_delta":
						if evt.TextDelta != "" {
							currentText += evt.TextDelta
							out <- Event{Type: "assistant_text", Text: evt.TextDelta}
						} else if evt.InputJSON != "" && currentToolUse != nil {
							currentToolInputJSON += evt.InputJSON
						} else if evt.ThinkingDelta != "" {
							currentThinking += evt.ThinkingDelta
						} else if evt.SignatureDelta != "" {
							currentSignature += evt.SignatureDelta
						}
					case "content_block_stop":
						switch currentBlockType {
						case "tool_use":
							if currentToolInputJSON != "" {
								var inputMap map[string]any
								if err := json.Unmarshal([]byte(currentToolInputJSON), &inputMap); err != nil {
									fmt.Fprintf(os.Stderr, "[jekylan-debug] tool input JSON parse error: %v\n", err)
								} else {
									currentToolUse.Input = inputMap
								}
							}
							assistantMsg.AddToolUse(currentToolUse.ID, currentToolUse.Name, currentToolUse.Input)
							hasToolUse = true
							// Emit the event with the fully-parsed input so downstream
							// handlers (e.g. skill collector) can inspect final args.
							out <- Event{
								Type:      "assistant_tool_use",
								ToolUseID: currentToolUse.ID,
								ToolName:  currentToolUse.Name,
								ToolInput: currentToolUse.Input,
							}
							currentToolUse = nil
							currentToolInputJSON = ""
						case "thinking":
							assistantMsg.Content = append(assistantMsg.Content, message.ThinkingBlock{
								Thinking:  currentThinking,
								Signature: currentSignature,
							})
							currentThinking = ""
							currentSignature = ""
						case "redacted_thinking":
							assistantMsg.Content = append(assistantMsg.Content, message.RedactedThinkingBlock{
								Data: currentRedacted,
							})
							currentRedacted = ""
						default:
							assistantMsg.AddText(currentText)
							currentText = ""
						}
						currentBlockType = ""
					case "message_delta":
						if evt.StopReason != "" {
							stopReason = evt.StopReason
						}
						if evt.Usage != nil {
							currentUsage = mergeUsage(currentUsage, evt.Usage)
						}
					case "error":
						if llm.IsPromptTooLongErrorString(evt.TextDelta) {
							assistantMsg = newPromptTooLongAssistantMessageFromText(evt.TextDelta)
							isPTL = true
							break streamLoop
						}
						out <- Event{Type: "error", Result: Result{Error: evt.TextDelta}}
						return
					}
				}

				if isPTL {
					break
				}

				// OpenAI does not emit content_block_start/stop; flush any
				// accumulated text that was never committed.
				if currentText != "" {
					assistantMsg.AddText(currentText)
					currentText = ""
				}

				// OpenAI may have pending tool_use that was never closed by content_block_stop
				if currentToolUse != nil {
					if currentToolInputJSON != "" {
						var inputMap map[string]any
						if err := json.Unmarshal([]byte(currentToolInputJSON), &inputMap); err != nil {
								fmt.Fprintf(os.Stderr, "[jekylan-debug] tool input JSON parse error: %v\n", err)
						} else {
							currentToolUse.Input = inputMap
						}
					}
					assistantMsg.AddToolUse(currentToolUse.ID, currentToolUse.Name, currentToolUse.Input)
					hasToolUse = true
					// Emit the event with the fully-parsed input so downstream
					// handlers (e.g. skill collector) can inspect final args.
					out <- Event{
						Type:      "assistant_tool_use",
						ToolUseID: currentToolUse.ID,
						ToolName:  currentToolUse.Name,
						ToolInput: currentToolUse.Input,
					}
					currentToolUse = nil
					currentToolInputJSON = ""
				}

				assistantMsg.Usage = currentUsage
				assistantMsg.ResponseID = currentResponseID
				assistantMsg.Timestamp = time.Now()
			}

			if !isPTL {
				msgs = append(msgs, assistantMsg)
				out <- Event{Type: "usage", Message: assistantMsg}
			}

			// --- Prompt-too-long recovery ---
			if isPTL {
				recoveryResult := compact.ReactiveCompactOnPromptTooLong(msgs, nil)
				if recoveryResult.OK && recoveryResult.Result != nil {
					msgs = recoveryResult.Result.Messages
					continue
				}
				out <- Event{
					Type: "result",
					Result: Result{
						Success:    false,
						Error:      assistantMsg.TextContent(),
						StopReason: "prompt_too_long",
						NumTurns:   turnCount,
					},
				}
				return
			}

			// --- Max output tokens recovery ---
			if !hasToolUse && stopReason == "max_output_tokens" {
				recoveryMsg := message.Message{Role: message.RoleUser, Timestamp: time.Now()}
				recoveryMsg.AddText(
					"Output token limit hit. Resume directly — no apology, no recap of what you were doing. " +
						"Pick up mid-thought if that is where the cut happened. Break remaining work into smaller pieces.",
				)
				msgs = append(msgs, recoveryMsg)
				out <- Event{Type: "user_message", Message: recoveryMsg}
				continue
			}

			if !hasToolUse {
				if stopReason == "" {
					stopReason = "end_turn"
				}
				out <- Event{
					Type: "result",
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
					out <- Event{Type: "user_message", Message: synthMsg}
				}
				out <- Event{Type: "error", Result: Result{Error: ErrQueryAborted.Error()}}
				return
			}

			toolUses := assistantMsg.ToolUses()
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
						r, err := t.Call(ctx, block.Input)
						if err != nil {
							result = err.Error()
							isErr = true
						} else {
							result = r
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

			msgs = append(msgs, userMsg)
			out <- Event{Type: "user_message", Message: userMsg}
			turnCount++
		}
	}()

	return out
}

func newPromptTooLongAssistantMessage(err error) message.Message {
	msg := message.Message{Role: message.RoleAssistant, Timestamp: time.Now()}
	msg.AddText("Prompt is too long")
	msg.APIError = "invalid_request"
	if err != nil {
		msg.ErrorDetails = err.Error()
	}
	return msg
}

func newPromptTooLongAssistantMessageFromText(text string) message.Message {
	msg := message.Message{Role: message.RoleAssistant, Timestamp: time.Now()}
	msg.AddText("Prompt is too long")
	msg.APIError = "invalid_request"
	msg.ErrorDetails = text
	return msg
}

// mergeUsage merges non-zero fields from src into dst. If dst is nil, src is
// returned directly. This prevents input_tokens from message_start being
// overwritten by output-only usage in message_delta.
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
