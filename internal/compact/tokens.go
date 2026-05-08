package compact

import (
	"context"
	"encoding/json"
	"math"
	"path/filepath"
	"strings"

	"github.com/Jekinnnnnn/jekylan/internal/llm"
	"github.com/Jekinnnnnn/jekylan/internal/message"
	"github.com/Jekinnnnnn/jekylan/internal/tool"
)

// BytesPerTokenForFileType returns a more accurate bytes-per-token ratio
// for known file types. Dense JSON has many single-character tokens which
// makes the real ratio closer to 2 rather than the default 4.
func BytesPerTokenForFileType(fileExtension string) float64 {
	switch fileExtension {
	case "json", "jsonl", "jsonc":
		return 2.0
	default:
		return 4.0
	}
}

// RoughTokenCount returns a fast approximate token count for a string.
// It divides the string length by bytesPerToken (default 4) and rounds
// to the nearest integer, matching the TS implementation.
func RoughTokenCount(s string, bytesPerToken ...float64) int {
	ratio := 4.0
	if len(bytesPerToken) > 0 {
		ratio = bytesPerToken[0]
	}
	return int(math.Round(float64(len(s)) / ratio))
}

// RoughTokenCountForFile estimates tokens for a file's content, choosing
// an appropriate bytes-per-token ratio based on the file extension.
func RoughTokenCountForFile(content, filename string) int {
	ext := strings.TrimPrefix(filepath.Ext(filename), ".")
	return RoughTokenCount(content, BytesPerTokenForFileType(ext))
}

// RoughTokenCountForBlock estimates tokens for a single ContentBlock,
// applying per-type heuristics from the TS codebase:
//   - text / thinking / redacted_thinking: length / 4
//   - image / document: fixed 2000 tokens
//   - tool_result: content length / 4 (Go MVP uses string content only)
//   - tool_use: name + JSON(input) length / 4
//   - default: JSON-serialized length / 4
func RoughTokenCountForBlock(block message.ContentBlock) int {
	switch b := block.(type) {
	case message.TextBlock:
		return RoughTokenCount(b.Text)
	case message.ToolResultBlock:
		return RoughTokenCount(b.Content)
	case message.ToolUseBlock:
		inputJSON, _ := json.Marshal(b.Input)
		return RoughTokenCount(b.Name + string(inputJSON))
	case message.ThinkingBlock:
		return RoughTokenCount(b.Thinking)
	case message.RedactedThinkingBlock:
		return RoughTokenCount(b.Data)
	default:
		// Fall back to JSON-serialized length.
		raw, _ := json.Marshal(block)
		return RoughTokenCount(string(raw))
	}
}

// EstimateMessageTokens roughly estimates the total tokens for a slice of
// messages. It mirrors roughTokenCountEstimationForMessages from the TS
// codebase and does NOT apply the 4/3 padding used by estimateMessageTokens
// in microCompact.ts — callers that need the conservative padding should
// multiply the result themselves.
func EstimateMessageTokens(msgs []message.Message) int {
	total := 0
	for _, m := range msgs {
		for _, block := range m.Content {
			total += RoughTokenCountForBlock(block)
		}
	}
	return total
}

// CountTokens attempts exact token counting when the client implements
// llm.TokenCounter (Anthropic). For clients without an exact counter
// (OpenAI) it falls back to RoughTokenCount estimation.
func CountTokens(ctx context.Context, client llm.Client, msgs []message.Message, tools *tool.Registry) int {
	if tc, ok := client.(llm.TokenCounter); ok {
		n, err := tc.CountTokens(ctx, msgs, tools)
		if err == nil && n >= 0 {
			return int(n)
		}
	}
	return EstimateMessageTokens(msgs)
}

// GetTokenUsage extracts the API usage from an assistant message if it has
// real (non-synthetic) usage recorded.
func GetTokenUsage(msg message.Message) *message.Usage {
	if msg.Role == message.RoleAssistant && msg.Usage != nil {
		return msg.Usage
	}
	return nil
}

// GetTokenCountFromUsage returns the total token count from a usage record:
// input_tokens + cache_creation_input_tokens + cache_read_input_tokens + output_tokens.
func GetTokenCountFromUsage(u *message.Usage) int {
	if u == nil {
		return 0
	}
	return int(u.InputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens + u.OutputTokens)
}

// TokenCountFromLastAPIResponse scans backward through messages and returns
// the total token count from the last assistant message that carries usage.
func TokenCountFromLastAPIResponse(msgs []message.Message) int {
	for i := len(msgs) - 1; i >= 0; i-- {
		if usage := GetTokenUsage(msgs[i]); usage != nil {
			return GetTokenCountFromUsage(usage)
		}
	}
	return 0
}

// TokenCountWithEstimation is the CANONICAL function for measuring context
// size when checking thresholds (autocompact, session memory init, etc.).
// It uses the last API response's token count plus estimates for any messages
// added since.
//
// When parallel tool calls are streamed, the query loop may emit multiple
// assistant records sharing the same ResponseID (split from the same API
// response). To avoid undercounting interleaved tool_results, after finding a
// usage-bearing record we walk back to the FIRST sibling with the same
// ResponseID so every interleaved tool_result is included in the rough
// estimate.
func TokenCountWithEstimation(msgs []message.Message) int {
	i := len(msgs) - 1
	for i >= 0 {
		msg := msgs[i]
		usage := GetTokenUsage(msg)
		if usage != nil {
			responseID := msg.ResponseID
			if responseID != "" {
				j := i - 1
				for j >= 0 {
					prior := msgs[j]
					priorID := prior.ResponseID
					if priorID == responseID {
						// Earlier split of the same API response — anchor here.
						i = j
					} else if priorID != "" {
						// Hit a different API response — stop walking.
						break
					}
					// priorID == "": a user/tool_result message, possibly
					// interleaved between splits — keep walking.
					j--
				}
			}
			return GetTokenCountFromUsage(usage) + EstimateMessageTokens(msgs[i+1:])
		}
		i--
	}
	return EstimateMessageTokens(msgs)
}
