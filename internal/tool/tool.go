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
// The last tool gets a cache_control breakpoint for prompt caching.
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
	// Place a cache breakpoint on the last tool definition.
	if len(out) > 0 {
		last := &out[len(out)-1]
		if last.OfTool != nil {
			last.OfTool.CacheControl = anthropic.NewCacheControlEphemeralParam()
		}
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

// Subset returns a new Registry containing only tools whose names are in
// allow, minus any whose names are in deny. allow=["*"] means all tools
// except those in deny.
func (r *Registry) Subset(allow, deny []string) *Registry {
	hasDeny := make(map[string]bool, len(deny))
	for _, d := range deny {
		hasDeny[d] = true
	}

	var out []Tool
	if len(allow) == 1 && allow[0] == "*" {
		for _, t := range r.tools {
			if !hasDeny[t.Name()] {
				out = append(out, t)
			}
		}
	} else {
		hasAllow := make(map[string]bool, len(allow))
		for _, a := range allow {
			hasAllow[a] = true
		}
		for _, t := range r.tools {
			if hasAllow[t.Name()] && !hasDeny[t.Name()] {
				out = append(out, t)
			}
		}
	}
	return NewRegistry(out...)
}
