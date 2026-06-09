package apicontext

import (
	"strings"
	"testing"
)

func TestGetAPIContextManagementNoOptions(t *testing.T) {
	cfg := GetAPIContextManagement(nil)
	if cfg != nil {
		t.Error("expected nil when no strategies apply")
	}
}

func TestGetAPIContextManagementThinking(t *testing.T) {
	cfg := GetAPIContextManagement(&APIContextManagementOptions{
		HasThinking: true,
	})
	if cfg == nil {
		t.Fatal("expected config with thinking strategy")
	}
	if len(cfg.Edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(cfg.Edits))
	}
	if cfg.Edits[0].Type != "clear_thinking_mc" {
		t.Errorf("expected clear_thinking_mc, got %s", cfg.Edits[0].Type)
	}
	keepStr, ok := cfg.Edits[0].Keep.(string)
	if !ok || keepStr != "all" {
		t.Errorf("expected keep='all', got %v", cfg.Edits[0].Keep)
	}
}

func TestGetAPIContextManagementRedactThinking(t *testing.T) {
	cfg := GetAPIContextManagement(&APIContextManagementOptions{
		HasThinking:            true,
		IsRedactThinkingActive: true,
	})
	if cfg != nil {
		t.Error("expected nil when redact-thinking is active")
	}
}

func TestGetAPIContextManagementClearAllThinking(t *testing.T) {
	cfg := GetAPIContextManagement(&APIContextManagementOptions{
		HasThinking:      true,
		ClearAllThinking: true,
	})
	if cfg == nil {
		t.Fatal("expected config")
	}
	keep, ok := cfg.Edits[0].Keep.(*ThresholdConfig)
	if !ok {
		t.Fatalf("expected *ThresholdConfig, got %T", cfg.Edits[0].Keep)
	}
	if keep.Type != "thinking_turns" || keep.Value != 1 {
		t.Errorf("expected {thinking_turns, 1}, got %+v", keep)
	}
}

func TestGetAPIContextManagementToolResults(t *testing.T) {
	cfg := GetAPIContextManagement(&APIContextManagementOptions{
		UseAPIClearToolResults: true,
		UseAPIClearToolUses:    false,
	})
	if cfg == nil {
		t.Fatal("expected config")
	}
	if len(cfg.Edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(cfg.Edits))
	}
	edit := cfg.Edits[0]
	if edit.Type != "clear_tool_uses_mc" {
		t.Errorf("expected clear_tool_uses_mc, got %s", edit.Type)
	}
	if edit.Trigger == nil {
		t.Error("expected trigger")
	}
	if edit.ClearAtLeast == nil {
		t.Error("expected clear_at_least")
	}
	inputs, ok := edit.ClearToolInputs.([]string)
	if !ok {
		t.Fatalf("expected []string ClearToolInputs, got %T", edit.ClearToolInputs)
	}
	if len(inputs) == 0 {
		t.Error("expected non-empty ClearToolInputs")
	}
}

func TestGetAPIContextManagementToolUses(t *testing.T) {
	cfg := GetAPIContextManagement(&APIContextManagementOptions{
		UseAPIClearToolResults: false,
		UseAPIClearToolUses:    true,
	})
	if cfg == nil {
		t.Fatal("expected config")
	}
	edit := cfg.Edits[0]
	if len(edit.ExcludeTools) == 0 {
		t.Error("expected non-empty ExcludeTools")
	}
	if edit.ClearToolInputs != nil {
		t.Error("expected ClearToolInputs to be nil for tool-uses strategy")
	}
}

func TestGetAPIContextManagementEnvOverrides(t *testing.T) {
	cfg := GetAPIContextManagement(&APIContextManagementOptions{
		UseAPIClearToolResults: true,
		APIMaxInputTokens:      100_000,
		APITargetInputTokens:   20_000,
	})
	if cfg == nil {
		t.Fatal("expected config")
	}
	edit := cfg.Edits[0]
	if edit.Trigger.Value != 100_000 {
		t.Errorf("expected trigger=100000, got %d", edit.Trigger.Value)
	}
	if edit.ClearAtLeast.Value != 80_000 {
		t.Errorf("expected clear_at_least=80000, got %d", edit.ClearAtLeast.Value)
	}
}

func TestContextManagementConfigJSON(t *testing.T) {
	cfg := &ContextManagementConfig{
		Edits: []ContextEditStrategy{
			{
				Type: "clear_thinking_mc",
				Keep: "all",
			},
			{
				Type: "clear_tool_uses_mc",
				Trigger: &ThresholdConfig{
					Type:  "input_tokens",
					Value: 180_000,
				},
				ClearToolInputs: []string{"bash", "grep"},
			},
		},
	}
	b, err := cfg.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON failed: %v", err)
	}
	if !strings.Contains(string(b), `"type":"clear_thinking_mc"`) {
		t.Error("expected clear_thinking in JSON")
	}
	if !strings.Contains(string(b), `"keep":"all"`) {
		t.Error("expected keep=all in JSON")
	}
	if !strings.Contains(string(b), `"type":"clear_tool_uses_mc"`) {
		t.Error("expected clear_tool_uses in JSON")
	}
}
