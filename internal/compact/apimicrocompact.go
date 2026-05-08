package compact

import (
	"encoding/json"
)

// Default values for context management strategies.
const (
	defaultMaxInputTokens    = 180_000
	defaultTargetInputTokens = 40_000
)

// Tool names eligible for API-level result clearing.
var (
	toolsClearableResults = []string{
		"bash",
		"glob",
		"grep",
		"file_read",
		"web_fetch",
		"web_search",
	}

	toolsClearableUses = []string{
		"file_edit",
		"file_write",
		"notebook_edit",
	}
)

// ThresholdConfig represents a typed threshold value used in context edit strategies.
type ThresholdConfig struct {
	Type  string `json:"type"`
	Value int    `json:"value"`
}

// ContextEditStrategy describes a single API-native context edit.
// The Type field discriminates between "clear_tool_uses_mc" and
// "clear_thinking_mc".  Optional fields are nil when unused.
type ContextEditStrategy struct {
	Type            string           `json:"type"`
	Trigger         *ThresholdConfig `json:"trigger,omitempty"`
	Keep            any              `json:"keep,omitempty"`              // *ThresholdConfig or string
	ClearToolInputs any              `json:"clear_tool_inputs,omitempty"` // bool or []string
	ExcludeTools    []string         `json:"exclude_tools,omitempty"`
	ClearAtLeast    *ThresholdConfig `json:"clear_at_least,omitempty"`
}

// ContextManagementConfig is the top-level wrapper sent to the API.
type ContextManagementConfig struct {
	Edits []ContextEditStrategy `json:"edits"`
}

// APIContextManagementOptions controls which strategies are generated.
type APIContextManagementOptions struct {
	HasThinking            bool
	IsRedactThinkingActive bool
	ClearAllThinking       bool
}

// GetAPIContextManagement returns API-native context-management configuration.
// When no strategies apply it returns nil.
func GetAPIContextManagement(options *APIContextManagementOptions) *ContextManagementConfig {
	hasThinking := false
	isRedactThinkingActive := false
	clearAllThinking := false

	if options != nil {
		hasThinking = options.HasThinking
		isRedactThinkingActive = options.IsRedactThinkingActive
		clearAllThinking = options.ClearAllThinking
	}

	strategies := make([]ContextEditStrategy, 0)

	// Preserve thinking blocks in previous assistant turns. Skip when
	// redact-thinking is active — redacted blocks have no model-visible content.
	// When clearAllThinking is set (>1h idle = cache miss), keep only the last
	// thinking turn — the API schema requires value >= 1.
	if hasThinking && !isRedactThinkingActive {
		var keep any = "all"
		if clearAllThinking {
			keep = &ThresholdConfig{Type: "thinking_turns", Value: 1}
		}
		strategies = append(strategies, ContextEditStrategy{
			Type: "clear_thinking_mc",
			Keep: keep,
		})
	}

	opts := GetOptions()

	useClearToolResults := opts.UseAPIClearToolResults
	useClearToolUses := opts.UseAPIClearToolUses

	if !useClearToolResults && !useClearToolUses {
		if len(strategies) > 0 {
			return &ContextManagementConfig{Edits: strategies}
		}
		return nil
	}

	triggerThreshold := defaultMaxInputTokens
	if opts.APIMaxInputTokens > 0 {
		triggerThreshold = opts.APIMaxInputTokens
	}

	keepTarget := defaultTargetInputTokens
	if opts.APITargetInputTokens > 0 {
		keepTarget = opts.APITargetInputTokens
	}

	if useClearToolResults {
		strategies = append(strategies, ContextEditStrategy{
			Type: "clear_tool_uses_mc",
			Trigger: &ThresholdConfig{
				Type:  "input_tokens",
				Value: triggerThreshold,
			},
			ClearAtLeast: &ThresholdConfig{
				Type:  "input_tokens",
				Value: triggerThreshold - keepTarget,
			},
			ClearToolInputs: toolsClearableResults,
		})
	}

	if useClearToolUses {
		strategies = append(strategies, ContextEditStrategy{
			Type: "clear_tool_uses_mc",
			Trigger: &ThresholdConfig{
				Type:  "input_tokens",
				Value: triggerThreshold,
			},
			ClearAtLeast: &ThresholdConfig{
				Type:  "input_tokens",
				Value: triggerThreshold - keepTarget,
			},
			ExcludeTools: toolsClearableUses,
		})
	}

	if len(strategies) > 0 {
		return &ContextManagementConfig{Edits: strategies}
	}
	return nil
}

// ToJSON serialises the configuration to JSON (convenience helper).
func (c *ContextManagementConfig) ToJSON() ([]byte, error) {
	return json.Marshal(c)
}
