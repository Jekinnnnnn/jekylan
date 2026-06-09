package apicontext

import (
	"encoding/json"
)

// Default thresholds for API context management.
const (
	DefaultAPIMaxInputTokens    = 180_000
	DefaultAPITargetInputTokens = 40_000
)

// Tool names eligible for API-level result clearing.
var (
	ToolsClearableResults = []string{
		"bash",
		"glob",
		"grep",
		"file_read",
		"web_fetch",
		"web_search",
	}

	ToolsClearableUses = []string{
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
	UseAPIClearToolResults bool
	UseAPIClearToolUses    bool
	APIMaxInputTokens      int
	APITargetInputTokens   int
}

// GetAPIContextManagement returns API-native context-management configuration.
// When no strategies apply it returns nil.
func GetAPIContextManagement(options *APIContextManagementOptions) *ContextManagementConfig {
	hasThinking := false
	isRedactThinkingActive := false
	clearAllThinking := false
	useClearToolResults := false
	useClearToolUses := false
	triggerThreshold := DefaultAPIMaxInputTokens
	keepTarget := DefaultAPITargetInputTokens

	if options != nil {
		hasThinking = options.HasThinking
		isRedactThinkingActive = options.IsRedactThinkingActive
		clearAllThinking = options.ClearAllThinking
		useClearToolResults = options.UseAPIClearToolResults
		useClearToolUses = options.UseAPIClearToolUses
		if options.APIMaxInputTokens > 0 {
			triggerThreshold = options.APIMaxInputTokens
		}
		if options.APITargetInputTokens > 0 {
			keepTarget = options.APITargetInputTokens
		}
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

	if !useClearToolResults && !useClearToolUses {
		if len(strategies) > 0 {
			return &ContextManagementConfig{Edits: strategies}
		}
		return nil
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
			ClearToolInputs: ToolsClearableResults,
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
			ExcludeTools: ToolsClearableUses,
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
