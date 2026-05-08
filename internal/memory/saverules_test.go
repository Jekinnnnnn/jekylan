package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadSaveRules_EmptyDir(t *testing.T) {
	rs, err := LoadSaveRules("")
	if err != nil {
		t.Fatalf("expected no error for empty dir, got %v", err)
	}
	if len(rs.Rules) != 0 {
		t.Fatalf("expected 0 rules, got %d", len(rs.Rules))
	}
}

func TestLoadSaveRules_NonExistentDir(t *testing.T) {
	rs, err := LoadSaveRules("/nonexistent/path/for/save/rules")
	if err != nil {
		t.Fatalf("expected no error for non-existent dir, got %v", err)
	}
	if len(rs.Rules) != 0 {
		t.Fatalf("expected 0 rules, got %d", len(rs.Rules))
	}
}

func TestLoadSaveRules_FeedbackRules(t *testing.T) {
	tmpDir := t.TempDir()
	rulesDir := filepath.Join(tmpDir, saveRulesDir)
	if err := os.MkdirAll(rulesDir, 0755); err != nil {
		t.Fatal(err)
	}

	content := `type: feedback
enabled: true
conditions:
  - name: behavioral_correction
    trigger: "User corrects your approach"
    action: save
    notes: "Include the Why"
  - name: routine_ack
    trigger: "User says ok"
    action: skip
exclusions:
  - "One-off fixes"
custom_prompt: |
  When saving feedback memories, structure them as:
  1. The rule
  2. **Why:** the reason
`
	if err := os.WriteFile(filepath.Join(rulesDir, "feedback.yaml"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	rs, err := LoadSaveRules(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rule := rs.Rules[MemoryTypeFeedback]
	if rule == nil {
		t.Fatal("expected feedback rule")
	}
	if len(rule.Conditions) != 2 {
		t.Fatalf("expected 2 conditions, got %d", len(rule.Conditions))
	}
	if rule.Conditions[0].Name != "behavioral_correction" {
		t.Errorf("expected first condition name 'behavioral_correction', got %q", rule.Conditions[0].Name)
	}
	if rule.Conditions[1].Action != "skip" {
		t.Errorf("expected second condition action 'skip', got %q", rule.Conditions[1].Action)
	}
	if len(rule.Exclusions) != 1 {
		t.Fatalf("expected 1 exclusion, got %d", len(rule.Exclusions))
	}
	if !strings.Contains(rule.CustomPrompt, "**Why:**") {
		t.Error("expected custom prompt to contain Why")
	}
}

func TestLoadSaveRules_DisabledRule(t *testing.T) {
	tmpDir := t.TempDir()
	rulesDir := filepath.Join(tmpDir, saveRulesDir)
	if err := os.MkdirAll(rulesDir, 0755); err != nil {
		t.Fatal(err)
	}

	content := `type: feedback
enabled: false
conditions:
  - name: should_not_load
    trigger: "Test"
    action: save
`
	if err := os.WriteFile(filepath.Join(rulesDir, "disabled.yaml"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	rs, err := LoadSaveRules(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if rs.Rules[MemoryTypeFeedback] != nil {
		t.Error("expected disabled rule to not be loaded")
	}
}

func TestLoadSaveRules_AllType(t *testing.T) {
	tmpDir := t.TempDir()
	rulesDir := filepath.Join(tmpDir, saveRulesDir)
	if err := os.MkdirAll(rulesDir, 0755); err != nil {
		t.Fatal(err)
	}

	content := `type: all
enabled: true
conditions:
  - name: explicit_request
    trigger: "User explicitly asks to remember"
    action: save
`
	if err := os.WriteFile(filepath.Join(rulesDir, "global.yaml"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	rs, err := LoadSaveRules(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, mt := range memoryTypes {
		if rs.Rules[mt] == nil {
			t.Errorf("expected rule for type %q from 'all' config", mt)
		}
	}
}

func TestBuildSaveRulesSection_Empty(t *testing.T) {
	rs := &SaveRuleset{Rules: make(map[MemoryType]*SaveRuleConfig)}
	if got := rs.BuildSaveRulesSection(); got != "" {
		t.Fatalf("expected empty section, got %q", got)
	}
}

func TestBuildSaveRulesSection_Rendering(t *testing.T) {
	rs := &SaveRuleset{
		Rules: map[MemoryType]*SaveRuleConfig{
			MemoryTypeFeedback: {
				Type: "feedback",
				Conditions: []SaveRuleCondition{
					{Name: "correction", Trigger: "User corrects you", Action: "save"},
					{Name: "routine", Trigger: "User says ok", Action: "skip", Notes: "Do not save"},
				},
				Exclusions:   []string{"One-off fixes"},
				CustomPrompt: "Structure as: rule, Why, How to apply",
			},
		},
	}

	section := rs.BuildSaveRulesSection()
	if section == "" {
		t.Fatal("expected non-empty section")
	}

	// Check header
	if !strings.Contains(section, "## When to save: detailed rules") {
		t.Error("expected header '## When to save: detailed rules'")
	}

	// Check type header
	if !strings.Contains(section, "### feedback memories") {
		t.Error("expected '### feedback memories'")
	}

	// Check save condition
	if !strings.Contains(section, "**correction**: User corrects you") {
		t.Error("expected '**correction**: User corrects you'")
	}

	// Check exclusions
	if !strings.Contains(section, "Never save:") {
		t.Error("expected 'Never save:'")
	}
	if !strings.Contains(section, "One-off fixes") {
		t.Error("expected 'One-off fixes' in exclusions")
	}
}

func TestBuildMemoryLines_WithSaveRules(t *testing.T) {
	tmpDir := t.TempDir()
	rulesDir := filepath.Join(tmpDir, saveRulesDir)
	if err := os.MkdirAll(rulesDir, 0755); err != nil {
		t.Fatal(err)
	}

	content := `type: feedback
enabled: true
conditions:
  - name: correction
    trigger: "User corrects you"
    action: save
`
	if err := os.WriteFile(filepath.Join(rulesDir, "feedback.yaml"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	saveRules, _ := LoadSaveRules(tmpDir)
	lines := BuildMemoryLines(tmpDir, saveRules)
	result := strings.Join(lines, "\n")

	if !strings.Contains(result, "## When to save: detailed rules") {
		t.Error("expected save rules section in BuildMemoryLines output")
	}
	if !strings.Contains(result, "**correction**: User corrects you") {
		t.Error("expected condition in BuildMemoryLines output")
	}
}

func TestBuildMemoryLines_WithoutSaveRules(t *testing.T) {
	tmpDir := t.TempDir()
	lines := BuildMemoryLines(tmpDir, nil)
	result := strings.Join(lines, "\n")

	if strings.Contains(result, "## When to save: detailed rules") {
		t.Error("did not expect save rules section when no save-rules dir exists")
	}
}
