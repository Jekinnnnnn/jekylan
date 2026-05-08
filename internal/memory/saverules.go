package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const saveRulesDir = "save-rules"

// SaveRuleCondition defines a single conditional trigger rule.
type SaveRuleCondition struct {
	Name    string `yaml:"name"`
	Trigger string `yaml:"trigger"`
	Action  string `yaml:"action"` // "save" or "skip"
	Notes   string `yaml:"notes,omitempty"`
}

// SaveRuleConfig is the YAML structure for per-type save rules.
type SaveRuleConfig struct {
	Type          string              `yaml:"type"`
	Enabled       bool                `yaml:"enabled"`
	Description   string              `yaml:"description,omitempty"`
	WhenToSave    string              `yaml:"when_to_save,omitempty"`
	HowToUse      string              `yaml:"how_to_use,omitempty"`
	Examples      []string            `yaml:"examples,omitempty"`
	BodyStructure string              `yaml:"body_structure,omitempty"`
	Conditions    []SaveRuleCondition `yaml:"conditions"`
	Exclusions    []string            `yaml:"exclusions"`
	CustomPrompt  string              `yaml:"custom_prompt,omitempty"`
}

// SaveRuleset holds loaded rules keyed by memory type.
type SaveRuleset struct {
	Rules map[MemoryType]*SaveRuleConfig
}

// LoadSaveRules scans <memoryDir>/save-rules/*.yaml and returns a ruleset.
// If the directory does not exist, returns an empty ruleset without error.
func LoadSaveRules(memoryDir string) (*SaveRuleset, error) {
	rs := &SaveRuleset{Rules: make(map[MemoryType]*SaveRuleConfig)}
	if memoryDir == "" {
		return rs, nil
	}
	dir := filepath.Join(memoryDir, saveRulesDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return rs, nil
		}
		return nil, err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var cfg SaveRuleConfig
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			continue
		}
		if !cfg.Enabled || cfg.Type == "" {
			continue
		}
		if cfg.Type == "all" {
			for _, mt := range memoryTypes {
				rs.Rules[mt] = mergeRule(rs.Rules[mt], &cfg)
			}
		} else {
			mt := ParseMemoryType(cfg.Type)
			if mt != "" {
				rs.Rules[mt] = mergeRule(rs.Rules[mt], &cfg)
			}
		}
	}
	return rs, nil
}

// mergeRule merges a new config into an existing one. Incoming fields override
// existing ones for scalar values; conditions and exclusions are appended.
func mergeRule(existing, incoming *SaveRuleConfig) *SaveRuleConfig {
	if existing == nil {
		return incoming
	}
	existing.Conditions = append(existing.Conditions, incoming.Conditions...)
	existing.Exclusions = append(existing.Exclusions, incoming.Exclusions...)
	if incoming.Description != "" {
		existing.Description = incoming.Description
	}
	if incoming.WhenToSave != "" {
		existing.WhenToSave = incoming.WhenToSave
	}
	if incoming.HowToUse != "" {
		existing.HowToUse = incoming.HowToUse
	}
	if len(incoming.Examples) > 0 {
		existing.Examples = incoming.Examples
	}
	if incoming.BodyStructure != "" {
		existing.BodyStructure = incoming.BodyStructure
	}
	return existing
}

// BuildTypesSection generates the `## Types of memory` section from the
// loaded save-rule configs. Falls back to the hard-coded TypesSectionIndividual
// if no rules are loaded.
// BuildTypesSection generates the `## Types of memory` section from the
// loaded save-rule configs. Falls back to the hard-coded TypesSectionIndividual
// if no rules are loaded.
func (rs *SaveRuleset) BuildTypesSection() string {
	if rs == nil || len(rs.Rules) == 0 {
		return TypesSectionIndividual()
	}
	var sections []string
	sections = append(sections, "## Types of memory", "")
	sections = append(sections, "There are several discrete types of memory that you can store in your memory system:", "")
	sections = append(sections, "<types>")
	for _, mt := range memoryTypes {
		rule := rs.Rules[mt]
		if rule == nil {
			continue
		}
		sections = append(sections, "<type>")
		sections = append(sections, fmt.Sprintf("    <name>%s</name>", mt))
		if rule.Description != "" {
			sections = append(sections, fmt.Sprintf("    <description>%s</description>", rule.Description))
		}
		if rule.WhenToSave != "" {
			sections = append(sections, fmt.Sprintf("    <when_to_save>%s</when_to_save>", rule.WhenToSave))
		}
		if rule.HowToUse != "" {
			sections = append(sections, fmt.Sprintf("    <how_to_use>%s</how_to_use>", rule.HowToUse))
		}
		if len(rule.Examples) > 0 {
			sections = append(sections, "    <examples>")
			for _, ex := range rule.Examples {
				sections = append(sections, fmt.Sprintf("    %s", ex))
			}
			sections = append(sections, "    </examples>")
		}
		if rule.BodyStructure != "" {
			sections = append(sections, fmt.Sprintf("    <body_structure>%s</body_structure>", rule.BodyStructure))
		}
		sections = append(sections, "</type>")
	}
	sections = append(sections, "</types>")
	return strings.Join(sections, "\n")
}

// BuildSaveRulesSection generates the conditional save rules prompt text.
// Returns empty string if no rules are loaded.
func (rs *SaveRuleset) BuildSaveRulesSection() string {
	if rs == nil || len(rs.Rules) == 0 {
		return ""
	}
	var sections []string
	sections = append(sections, "## When to save: detailed rules", "")
	sections = append(sections, "Follow these conditional rules to decide whether to save a memory. When rules conflict, the more specific condition wins over the general type description above.")
	sections = append(sections, "")

	for _, mt := range memoryTypes {
		rule := rs.Rules[mt]
		if rule == nil {
			continue
		}
		sections = append(sections, fmt.Sprintf("### %s memories", mt))
		if len(rule.Conditions) > 0 {
			sections = append(sections, "Save when:")
			for _, c := range rule.Conditions {
				line := fmt.Sprintf("- **%s**: %s", c.Name, c.Trigger)
				if c.Notes != "" {
					line += fmt.Sprintf(" — %s", c.Notes)
				}
				sections = append(sections, line)
			}
		}
		if len(rule.Exclusions) > 0 {
			sections = append(sections, "Never save:")
			for _, ex := range rule.Exclusions {
				sections = append(sections, fmt.Sprintf("  - %s", ex))
			}
		}
		sections = append(sections, "")
	}
	return strings.Join(sections, "\n")
}
