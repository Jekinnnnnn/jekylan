package llm

import (
	"context"
	"errors"
	"fmt"

	"github.com/Jekinnnnnn/jekylan/internal/message"
	"github.com/Jekinnnnnn/jekylan/internal/tool"
)

// ErrFallbackTriggered is returned by the Anthropic client when the API
// signals a model overload (HTTP 529). The query loop should retry the
// request with a fallback model.
var ErrFallbackTriggered = fmt.Errorf("model overloaded, fallback required")

// IsFallbackError returns true when the error indicates the primary model
// is overloaded and a fallback should be tried.
func IsFallbackError(err error) bool {
	return errors.Is(err, ErrFallbackTriggered)
}

// ModelSwitcher allows the query loop to swap the model at runtime
// (e.g. when a fallback is triggered).
type ModelSwitcher interface {
	SetModel(model string)
}

// StreamEvent represents a single event from an LLM streaming response.
type StreamEvent struct {
	Type           string
	TextDelta      string
	InputJSON      string
	ThinkingDelta  string
	SignatureDelta string
	StopReason     string
	StopSequence   string
	Index          int64
	BlockType      string
	BlockID        string
	BlockName      string
	BlockInput     map[string]any
	BlockThinking  string
	BlockSignature string
	BlockRedacted  string

	// Aliases for easier access across providers
	ToolName  string // alias for BlockName
	ToolUseID string // alias for BlockID

	// Usage fields (populated by OpenAI when stream_options.include_usage is true)
	UsagePromptTokens     int64
	UsageCompletionTokens int64
	UsageTotalTokens      int64

	// Full usage struct from the API response (set for both Anthropic and OpenAI).
	Usage *message.Usage

	// ResponseID is the API response ID (e.g. Anthropic message.id or OpenAI chunk.id).
	ResponseID string
}

// Client is the common interface for LLM streaming clients.
type Client interface {
	StreamMessages(ctx context.Context, msgs []message.Message, systemPrompt string, tools *tool.Registry, thinkingBudget int64, cacheBreakpoints int) (<-chan StreamEvent, error)
}

// TokenCounter is the interface for exact token counting.
// Implementations are provider-specific: Anthropic uses the CountTokens API,
// OpenAI falls back to rough estimation (no native count endpoint).
type TokenCounter interface {
	CountTokens(ctx context.Context, msgs []message.Message, tools *tool.Registry) (int64, error)
}

// Factory creates a Client based on provider configuration.
type Factory struct{}

// NewClient creates a concrete LLM client.
func (f Factory) NewClient(provider, model, apiKey, baseURL string, maxTokens int) (Client, error) {
	switch provider {
	case "anthropic":
		return NewAnthropicClient(model, apiKey, baseURL, maxTokens), nil
	case "openai":
		return NewOpenAIClient(model, apiKey, baseURL, maxTokens), nil
	default:
		return nil, nil
	}
}

// NewClientFunc returns a closure that calls f.NewClient with the given
// fixed provider / apiKey / baseURL / maxTokens, varying only the model.
// Useful for callers (e.g. agent sub-agent spawners) that need to build a
// fresh client when a particular agent overrides the engine's default model.
func (f Factory) NewClientFunc(provider, apiKey, baseURL string, maxTokens int) func(model string) (Client, error) {
	return func(model string) (Client, error) {
		return f.NewClient(provider, model, apiKey, baseURL, maxTokens)
	}
}

// DefaultFactory is the default client factory.
var DefaultFactory = Factory{}
