package tool

import (
	"context"

	"github.com/anthropics/anthropic-sdk-go"
	oai "github.com/openai/openai-go"
)

// Tool is the interface for a tool that the assistant can invoke.
type Tool interface {
	Name() string
	Description() string
	InputSchema() map[string]any
	Call(ctx context.Context, input map[string]any) (string, error)
	// SystemPrompt returns an optional system prompt contribution for this tool.
	// Empty string means no contribution.
	SystemPrompt() string
}

// Registry holds a collection of tools and provides lookup by name.
type Registry struct {
	tools map[string]Tool
}

// NewRegistry creates a new tool registry.
func NewRegistry(tools ...Tool) *Registry {
	r := &Registry{tools: make(map[string]Tool, len(tools))}
	for _, t := range tools {
		r.tools[t.Name()] = t
	}
	return r
}

// Find looks up a tool by name. Returns nil if not found.
func (r *Registry) Find(name string) Tool {
	return r.tools[name]
}

// All returns all registered tools.
func (r *Registry) All() []Tool {
	out := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t)
	}
	return out
}

// ToAnthropicSDK converts the registry's tools to Anthropic SDK ToolUnion format.
func (r *Registry) ToAnthropicSDK() []anthropic.ToolUnionParam {
	out := make([]anthropic.ToolUnionParam, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, anthropic.ToolUnionParam{
			OfTool: &anthropic.ToolParam{
				Name:        t.Name(),
				Description: anthropic.String(t.Description()),
				InputSchema: anthropic.ToolInputSchemaParam{ExtraFields: t.InputSchema()},
			},
		})
	}
	return out
}

// ToOpenAISDK converts the registry's tools to OpenAI SDK ChatCompletionToolParam format.
func (r *Registry) ToOpenAISDK() []oai.ChatCompletionToolParam {
	out := make([]oai.ChatCompletionToolParam, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, oai.ChatCompletionToolParam{
			Function: oai.FunctionDefinitionParam{
				Name:        t.Name(),
				Description: oai.String(t.Description()),
				Parameters:  oai.FunctionParameters(t.InputSchema()),
			},
		})
	}
	return out
}
