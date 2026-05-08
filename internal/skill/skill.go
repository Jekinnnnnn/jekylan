package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Skill represents a loaded skill (a prompt with frontmatter metadata).
type Skill struct {
	Name         string
	Description  string
	WhenToUse    string
	Content      string
	AllowedTools []string
	SkillRoot    string
}

// Registry holds loaded skills indexed by name.
type Registry struct {
	skills map[string]*Skill
}

// NewRegistry creates an empty skill registry.
func NewRegistry() *Registry {
	return &Registry{skills: make(map[string]*Skill)}
}

// Register adds a skill to the registry.
func (r *Registry) Register(s *Skill) {
	if r.skills == nil {
		r.skills = make(map[string]*Skill)
	}
	r.skills[s.Name] = s
}

// Find looks up a skill by name. Returns nil if not found.
func (r *Registry) Find(name string) *Skill {
	return r.skills[name]
}

// All returns all registered skills.
func (r *Registry) All() []*Skill {
	out := make([]*Skill, 0, len(r.skills))
	for _, s := range r.skills {
		out = append(out, s)
	}
	return out
}

// frontmatterData holds the parsed YAML frontmatter from a SKILL.md file.
type frontmatterData struct {
	Name         string   `yaml:"name"`
	Description  string   `yaml:"description"`
	WhenToUse    string   `yaml:"when_to_use"`
	AllowedTools []string `yaml:"allowed-tools"`
	Enabled      *bool    `yaml:"enabled"`
}

// LoadDir loads all skills from a directory.
// Each skill must be in a subdirectory with a SKILL.md file:
//
//	skills/commit/SKILL.md
//	skills/review/SKILL.md
func LoadDir(basePath string) (*Registry, error) {
	r := NewRegistry()

	entries, err := os.ReadDir(basePath)
	if err != nil {
		if os.IsNotExist(err) {
			return r, nil
		}
		return r, fmt.Errorf("read skills dir: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		skillDir := filepath.Join(basePath, entry.Name())
		skillFile := filepath.Join(skillDir, "SKILL.md")

		data, err := os.ReadFile(skillFile)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return r, fmt.Errorf("read skill file %s: %w", skillFile, err)
		}

		skill, err := parseSkillFile(entry.Name(), string(data), skillDir)
		if err != nil {
			return r, fmt.Errorf("parse skill %s: %w", entry.Name(), err)
		}
		if skill == nil {
			continue
		}

		r.Register(skill)
	}

	return r, nil
}

// parseSkillFile parses a SKILL.md file into a Skill.
func parseSkillFile(skillName, rawContent, skillDir string) (*Skill, error) {
	frontmatter, content, err := splitFrontmatter(rawContent)
	if err != nil {
		return nil, err
	}

	var fm frontmatterData
	if frontmatter != "" {
		if err := yaml.Unmarshal([]byte(frontmatter), &fm); err != nil {
			return nil, fmt.Errorf("parse frontmatter: %w", err)
		}
	}

	// Explicitly disabled — signal to LoadDir to skip this skill.
	if fm.Enabled != nil && !*fm.Enabled {
		return nil, nil
	}

	// Prefer name from frontmatter, fall back to directory name.
	name := skillName
	if fm.Name != "" {
		name = fm.Name
	}

	description := fm.Description
	if description == "" {
		description = extractDescriptionFromMarkdown(content)
	}

	return &Skill{
		Name:         name,
		Description:  description,
		WhenToUse:    fm.WhenToUse,
		Content:      content,
		AllowedTools: fm.AllowedTools,
		SkillRoot:    skillDir,
	}, nil
}

// splitFrontmatter splits a markdown file into YAML frontmatter and body.
// Supports the --- delimited format.
func splitFrontmatter(content string) (frontmatter, body string, err error) {
	trimmed := strings.TrimSpace(content)
	if !strings.HasPrefix(trimmed, "---") {
		return "", content, nil
	}

	parts := strings.SplitN(trimmed, "---", 3)
	if len(parts) < 3 {
		return "", content, nil
	}

	return strings.TrimSpace(parts[1]), strings.TrimSpace(parts[2]), nil
}

// extractDescriptionFromMarkdown tries to extract a description from the first
// paragraph of markdown content.
func extractDescriptionFromMarkdown(content string) string {
	lines := strings.Split(content, "\n")
	var desc strings.Builder
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if desc.Len() > 0 {
				break
			}
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		desc.WriteString(trimmed)
		desc.WriteString(" ")
		if desc.Len() > 200 {
			break
		}
	}
	return strings.TrimSpace(desc.String())
}

// SubstituteArgs replaces $ARGUMENTS and positional $1, $2... placeholders
// in the skill content with the provided arguments.
func SubstituteArgs(content, args string, argNames []string) string {
	result := content

	// Replace $ARGUMENTS with the raw args string
	result = strings.ReplaceAll(result, "$ARGUMENTS", args)

	// Replace positional placeholders $1, $2...
	argParts := splitArgs(args)
	for i, part := range argParts {
		placeholder := fmt.Sprintf("$%d", i+1)
		result = strings.ReplaceAll(result, placeholder, part)
	}

	// Replace named arguments like ${name} if argNames is provided
	if len(argNames) > 0 {
		for i, name := range argNames {
			if i < len(argParts) {
				placeholder := fmt.Sprintf("${%s}", name)
				result = strings.ReplaceAll(result, placeholder, argParts[i])
			}
		}
	}

	return result
}

// splitArgs splits an argument string respecting quoted substrings.
func splitArgs(s string) []string {
	var parts []string
	var current strings.Builder
	inQuote := false
	quoteChar := rune(0)

	for _, r := range s {
		if inQuote {
			if r == quoteChar {
				inQuote = false
				quoteChar = 0
			} else {
				current.WriteRune(r)
			}
		} else {
			switch r {
			case '"', '\'':
				inQuote = true
				quoteChar = r
			case ' ', '\t':
				if current.Len() > 0 {
					parts = append(parts, current.String())
					current.Reset()
				}
			default:
				current.WriteRune(r)
			}
		}
	}
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}
	return parts
}

// RenderContent prepares the final skill content for injection into the
// conversation. It substitutes arguments and adds the base directory header.
func (s *Skill) RenderContent(args string) string {
	content := s.Content

	if s.SkillRoot != "" {
		content = fmt.Sprintf("Base directory for this skill: %s\n\n%s", s.SkillRoot, content)
	}

	content = strings.ReplaceAll(content, "${SKILL_DIR}", s.SkillRoot)

	return content
}
