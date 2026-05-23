package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/Jekinnnnnn/jekylan/internal/message"
	"github.com/Jekinnnnnn/jekylan/internal/tool"
	oai "github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

// OpenAIClient wraps the OpenAI SDK for streaming chat completion.
type OpenAIClient struct {
	inner     oai.Client
	model     string
	maxTokens int
	lastUsage oai.CompletionUsage
}

func (c *OpenAIClient) recordUsage(u oai.CompletionUsage) {
	c.lastUsage = u
}

// NewOpenAIClient creates a new OpenAI streaming client.
func NewOpenAIClient(model, apiKey, baseURL string, maxTokens int) *OpenAIClient {
	opts := []option.RequestOption{option.WithAPIKey(apiKey)}
	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	return &OpenAIClient{
		inner:     oai.NewClient(opts...),
		model:     model,
		maxTokens: maxTokens,
	}
}

// SetModel swaps the model string used for subsequent requests.
func (c *OpenAIClient) SetModel(model string) {
	c.model = model
}

// StreamMessages sends a streaming request to the OpenAI API and yields events.
func (c *OpenAIClient) StreamMessages(ctx context.Context, msgs []message.Message, systemPrompt string, tools *tool.Registry, thinkingBudget int64, cacheBreakpoints int) (
	<-chan StreamEvent, error) {
	out := make(chan StreamEvent)

	params := oai.ChatCompletionNewParams{
		Model: c.model,
	}

	if c.maxTokens > 0 {
		params.MaxCompletionTokens = oai.Int(int64(c.maxTokens))
	}

	if systemPrompt != "" {
		params.Messages = append(params.Messages, oai.SystemMessage(systemPrompt))
	}

	for _, m := range msgs {
		params.Messages = append(params.Messages, m.ToOpenAIMessages()...)
	}

	if tools != nil {
		params.Tools = tools.ToOpenAISDK()
	}

	_ = thinkingBudget // OpenAI does not support thinking budget in the same way

	params.StreamOptions = oai.ChatCompletionStreamOptionsParam{
		IncludeUsage: oai.Bool(true),
	}

	reqJSON, _ := json.MarshalIndent(params, "", "  ")

	if os.Getenv("JEKYLAN_DEBUG") != "" {
		fmt.Fprintf(os.Stderr, "[jekylan-debug] OpenAI request:\n%s\n", string(reqJSON))
	}

	stream := c.inner.Chat.Completions.NewStreaming(ctx, params)
	if stream.Err() != nil {
		return nil, fmt.Errorf("stream init error: %w", stream.Err())
	}

	go func() {
		defer close(out)
		for stream.Next() {
			chunk := stream.Current()
			c.sendEvent(out, chunk)
		}
		if err := stream.Err(); err != nil {
			out <- StreamEvent{Type: "error", TextDelta: err.Error()}
		}
	}()

	return out, nil
}

// CountTokens returns the token usage from the last streamed assistant message.
// It implements llm.TokenCounter. If no usage has been recorded yet it falls
// back to rough estimation on the client side.
func (c *OpenAIClient) CountTokens(ctx context.Context, msgs []message.Message, tools *tool.Registry) (int64, error) {
	if c.lastUsage.TotalTokens > 0 {
		return c.lastUsage.TotalTokens, nil
	}
	return 0, fmt.Errorf("no usage recorded yet")
}

func (c *OpenAIClient) sendEvent(out chan<- StreamEvent, chunk oai.ChatCompletionChunk) {
	// Usage is sent in a final chunk with empty Choices when
	// stream_options.include_usage is true.
	// Some providers set PromptTokens/CompletionTokens without TotalTokens,
	// or only set CachedTokens.
	if chunk.Usage.TotalTokens > 0 || chunk.Usage.PromptTokens > 0 || chunk.Usage.CompletionTokens > 0 || chunk.Usage.PromptTokensDetails.CachedTokens > 0 {
		c.recordUsage(chunk.Usage)
		out <- StreamEvent{
			Type:                  "usage",
			UsagePromptTokens:     chunk.Usage.PromptTokens,
			UsageCompletionTokens: chunk.Usage.CompletionTokens,
			UsageTotalTokens:      chunk.Usage.TotalTokens,
			Usage: &message.Usage{
				InputTokens:          chunk.Usage.PromptTokens,
				OutputTokens:         chunk.Usage.CompletionTokens,
				CacheReadInputTokens: chunk.Usage.PromptTokensDetails.CachedTokens,
			},
			ResponseID: chunk.ID,
		}
	}
	for _, choice := range chunk.Choices {
		delta := choice.Delta
		if delta.Content != "" {
			out <- StreamEvent{Type: "assistant_text", TextDelta: delta.Content, ResponseID: chunk.ID}
		}
		for _, tc := range delta.ToolCalls {
			if tc.Function.Name != "" || tc.Function.Arguments != "" {
				// OpenAI streams tool calls with an index. We map them to our
				// generic events. Name typically arrives in the first chunk for a
				// given index, and arguments are streamed as partial JSON.
				out <- StreamEvent{
					Type:       "assistant_tool_use",
					ToolName:   tc.Function.Name,
					ToolUseID:  tc.ID,
					BlockName:  tc.Function.Name,
					BlockID:    tc.ID,
					InputJSON:  tc.Function.Arguments,
					Index:      tc.Index,
					ResponseID: chunk.ID,
				}
			}
		}
		if choice.FinishReason != "" {
			out <- StreamEvent{Type: "message_delta", StopReason: string(choice.FinishReason), ResponseID: chunk.ID}
		}
	}
}
