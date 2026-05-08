package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/Jekinnnnnn/jekylan/internal/message"
	"github.com/Jekinnnnnn/jekylan/internal/tool"
	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// AnthropicClient wraps the Anthropic SDK for streaming message generation.
type AnthropicClient struct {
	inner             sdk.Client
	model             string
	maxTokens         int
	contextManagement any
}

// NewAnthropicClient creates a new Anthropic streaming client.
func NewAnthropicClient(model, apiKey, baseURL string, maxTokens int) *AnthropicClient {
	opts := []option.RequestOption{option.WithAPIKey(apiKey)}
	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	if maxTokens <= 0 {
		maxTokens = 8196
	}
	return &AnthropicClient{
		inner:     sdk.NewClient(opts...),
		model:     model,
		maxTokens: maxTokens,
	}
}

// SetContextManagement sets the API-native context-management configuration.
// It is injected into the request body via option.WithJSONSet.
func (c *AnthropicClient) SetContextManagement(v any) {
	c.contextManagement = v
}

// SetModel swaps the model string used for subsequent requests.
func (c *AnthropicClient) SetModel(model string) {
	c.model = model
}

// StreamMessages sends a streaming request to the Anthropic API and yields events.
func (c *AnthropicClient) StreamMessages(ctx context.Context, msgs []message.Message, systemPrompt string, tools *tool.Registry, thinkingBudget int64) (<-chan StreamEvent, error) {
	out := make(chan StreamEvent)

	params := sdk.MessageNewParams{
		MaxTokens: int64(c.maxTokens),
		Model:     c.model,
	}

	for i, m := range msgs {
		if len(m.Content) == 0 {
			return nil, fmt.Errorf("message %d (role=%s) has empty content", i, m.Role)
		}
		params.Messages = append(params.Messages, m.ToAnthropicMessage())
	}

	if systemPrompt != "" {
		params.System = []sdk.TextBlockParam{{Text: systemPrompt}}
	}

	if tools != nil {
		params.Tools = tools.ToAnthropicSDK()
	}

	if thinkingBudget >= 1024 {
		params.Thinking = sdk.ThinkingConfigParamUnion{
			OfEnabled: &sdk.ThinkingConfigEnabledParam{
				BudgetTokens: thinkingBudget,
			},
		}
	}

	var reqOpts []option.RequestOption
	if c.contextManagement != nil {
		reqOpts = append(reqOpts, option.WithJSONSet("context_management", c.contextManagement))
	}

	if os.Getenv("JEKYLAN_DEBUG") != "" {
		b, _ := json.MarshalIndent(params, "", "  ")
		fmt.Fprintf(os.Stderr, "[jekylan-debug] Anthropic request:\n%s\n", string(b))
	}

	stream := c.inner.Messages.NewStreaming(ctx, params, reqOpts...)
	if stream.Err() != nil {
		var apiErr *sdk.Error
		if errors.As(stream.Err(), &apiErr) && apiErr.StatusCode == 529 {
			return nil, ErrFallbackTriggered
		}
		return nil, stream.Err()
	}

	go func() {
		defer close(out)
		for stream.Next() {
			event := stream.Current()
			c.sendEvent(out, event)
		}
		if err := stream.Err(); err != nil {
			out <- StreamEvent{Type: "error", TextDelta: err.Error()}
		}
	}()

	return out, nil
}

// CountTokens uses the Anthropic CountTokens API for exact token counting.
func (c *AnthropicClient) CountTokens(ctx context.Context, msgs []message.Message, _ *tool.Registry) (int64, error) {
	params := sdk.MessageCountTokensParams{
		Model: sdk.Model(c.model),
	}
	for _, m := range msgs {
		params.Messages = append(params.Messages, m.ToAnthropicMessage())
	}
	resp, err := c.inner.Messages.CountTokens(ctx, params)
	if err != nil {
		return 0, err
	}
	return resp.InputTokens, nil
}

func (c *AnthropicClient) sendEvent(out chan<- StreamEvent, event sdk.MessageStreamEventUnion) {
	switch variant := event.AsAny().(type) {
	case sdk.MessageStartEvent:
		se := StreamEvent{Type: "message_start"}
		if variant.Message.ID != "" {
			se.ResponseID = variant.Message.ID
		}
		if variant.Message.Usage.InputTokens > 0 || variant.Message.Usage.OutputTokens > 0 {
			se.Usage = &message.Usage{
				InputTokens:              variant.Message.Usage.InputTokens,
				OutputTokens:             variant.Message.Usage.OutputTokens,
				CacheCreationInputTokens: variant.Message.Usage.CacheCreationInputTokens,
				CacheReadInputTokens:     variant.Message.Usage.CacheReadInputTokens,
			}
		}
		out <- se
	case sdk.ContentBlockStartEvent:
		se := StreamEvent{Type: "content_block_start", Index: variant.Index}
		if variant.ContentBlock.Type != "" {
			se.BlockType = variant.ContentBlock.Type
		}
		switch cb := variant.ContentBlock.AsAny().(type) {
		case sdk.TextBlock:
			se.BlockType = "text"
		case sdk.ToolUseBlock:
			se.BlockType = "tool_use"
			se.BlockID = cb.ID
			se.BlockName = cb.Name
			if len(cb.Input) > 0 {
				var m map[string]any
				if err := json.Unmarshal(cb.Input, &m); err == nil {
					se.BlockInput = m
				}
			}
		case sdk.ThinkingBlock:
			se.BlockType = "thinking"
			se.BlockThinking = cb.Thinking
			se.BlockSignature = cb.Signature
		case sdk.RedactedThinkingBlock:
			se.BlockType = "redacted_thinking"
			se.BlockRedacted = cb.Data
		}
		out <- se
	case sdk.ContentBlockDeltaEvent:
		se := StreamEvent{Type: "content_block_delta", Index: variant.Index}
		switch delta := variant.Delta.AsAny().(type) {
		case sdk.TextDelta:
			se.TextDelta = delta.Text
		case sdk.InputJSONDelta:
			se.InputJSON = delta.PartialJSON
		case sdk.ThinkingDelta:
			se.ThinkingDelta = delta.Thinking
		case sdk.SignatureDelta:
			se.SignatureDelta = delta.Signature
		}
		out <- se
	case sdk.ContentBlockStopEvent:
		out <- StreamEvent{Type: "content_block_stop", Index: variant.Index}
	case sdk.MessageDeltaEvent:
		se := StreamEvent{Type: "message_delta"}
		if variant.Delta.StopReason != "" {
			se.StopReason = string(variant.Delta.StopReason)
		}
		if variant.Delta.StopSequence != "" {
			se.StopSequence = variant.Delta.StopSequence
		}
		se.Usage = &message.Usage{
			InputTokens:              variant.Usage.InputTokens,
			OutputTokens:             variant.Usage.OutputTokens,
			CacheCreationInputTokens: variant.Usage.CacheCreationInputTokens,
			CacheReadInputTokens:     variant.Usage.CacheReadInputTokens,
		}
		out <- se
	case sdk.MessageStopEvent:
		out <- StreamEvent{Type: "message_stop"}
	default:
		fmt.Printf("unknown event type: %+v\n", variant)
	}
}
