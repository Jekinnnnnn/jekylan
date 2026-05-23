package compact

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/Jekinnnnnn/jekylan/internal/llm"
	"github.com/Jekinnnnnn/jekylan/internal/message"
)

// Result is the outcome of a compaction operation.
type Result struct {
	Messages          []message.Message
	PostCompactTokens int
}

const compactSystemPrompt = "You are a helpful AI assistant tasked with summarizing conversations."

// CompactConversation generates a summary of the provided messages and returns
// a compact boundary marker followed by a user message containing the summary.
func CompactConversation(ctx context.Context, client llm.Client, model string, msgs []message.Message) (*Result, error) {
	if len(msgs) == 0 {
		return nil, fmt.Errorf("not enough messages to compact")
	}

	// Try session memory compaction first (no custom instructions support).
	if sessionResult := TrySessionMemoryCompaction(msgs, 0); sessionResult != nil {
		return sessionResult, nil
	}

	fmt.Fprintf(os.Stderr, "[jekylan-debug] legacy compact: generating summary for %d messages\n", len(msgs))
	summary, err := streamCompactSummary(ctx, client, model, msgs)
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(os.Stderr, "[jekylan-debug] legacy compact: summary length=%d chars\n", len(summary))

	boundary := message.Message{Role: message.RoleSystem, Timestamp: time.Now()}
	boundary.AddText("[Compact boundary: earlier conversation summarized below]")

	formattedSummary := FormatCompactSummary(summary)
	summaryContent := GetCompactUserSummaryMessage(formattedSummary, true, "", false)

	summaryMsg := message.Message{Role: message.RoleUser, Timestamp: time.Now()}
	summaryMsg.AddText(summaryContent)

	result := &Result{
		Messages: []message.Message{boundary, summaryMsg},
	}
	result.PostCompactTokens = EstimateMessageTokens(result.Messages)
	return result, nil
}

func streamCompactSummary(ctx context.Context, client llm.Client, model string, msgs []message.Message) (string, error) {
	compactPrompt := GetCompactPrompt("")
	summaryReq := message.Message{Role: message.RoleUser}
	summaryReq.AddText(compactPrompt)
	summaryMsgs := append(append([]message.Message(nil), msgs...), summaryReq)

	stream, err := client.StreamMessages(ctx, summaryMsgs, compactSystemPrompt, nil, 0, 0)
	if err != nil {
		return "", err
	}

	var summary string
	for evt := range stream {
		switch evt.Type {
		case "assistant_text":
			summary += evt.TextDelta
		case "error":
			return "", fmt.Errorf("compact stream error: %s", evt.TextDelta)
		}
	}

	if summary == "" {
		return "", fmt.Errorf("failed to generate conversation summary - response did not contain valid text content")
	}

	return summary, nil
}
