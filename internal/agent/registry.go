package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Registry holds agent definitions by name.
type Registry struct {
	defs map[string]*Definition
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{defs: make(map[string]*Definition)}
}

// Register adds or overwrites a definition.
func (r *Registry) Register(d *Definition) {
	r.defs[d.Name] = d
}

// Get looks up a definition by name. Returns nil if not found.
func (r *Registry) Get(name string) *Definition {
	return r.defs[name]
}

// List returns all registered definitions. Order is non-deterministic.
func (r *Registry) List() []*Definition {
	out := make([]*Definition, 0, len(r.defs))
	for _, d := range r.defs {
		out = append(out, d)
	}
	return out
}

// LoadFromDir scans dir for *.md files and registers each parsed definition.
// Files with invalid frontmatter are skipped with a stderr warning.
// If dir does not exist, it returns nil (no-op).
func (r *Registry) LoadFromDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[agent] skip %s: read: %v\n", entry.Name(), err)
			continue
		}
		d, err := parseAgentMarkdown(string(data))
		if err != nil {
			fmt.Fprintf(os.Stderr, "[agent] skip %s: parse: %v\n", entry.Name(), err)
			continue
		}
		r.Register(d)
	}
	return nil
}

// agentFrontmatter is the YAML shape inside the --- frontmatter block.
type agentFrontmatter struct {
	Name         string   `yaml:"name"`
	Description  string   `yaml:"description"`
	SystemPrompt string   `yaml:"system_prompt"`
	ToolsAllow   []string `yaml:"tools"`
	ToolsDeny    []string `yaml:"disallowed_tools"`
	Model        string   `yaml:"model"`
	MaxTurns     int      `yaml:"max_turns"`
	Effort       string   `yaml:"effort"`
}

// parseAgentMarkdown extracts YAML frontmatter and uses the remainder as the
// system prompt body (unless system_prompt is already set in frontmatter).
func parseAgentMarkdown(data string) (*Definition, error) {
	data = strings.TrimSpace(data)
	if !strings.HasPrefix(data, "---") {
		return nil, fmt.Errorf("missing YAML frontmatter")
	}

	rest := data[3:] // strip leading ---
	before, after, ok := strings.Cut(rest, "\n---")
	if !ok {
		return nil, fmt.Errorf("unterminated YAML frontmatter")
	}

	var fm agentFrontmatter
	if err := yaml.Unmarshal([]byte(before), &fm); err != nil {
		return nil, fmt.Errorf("invalid YAML: %w", err)
	}

	if fm.Name == "" {
		return nil, fmt.Errorf("missing required field 'name'")
	}

	sysPrompt := fm.SystemPrompt
	if sysPrompt == "" {
		// Use the markdown body after the frontmatter as the system prompt.
		body := strings.TrimSpace(after)
		sysPrompt = body
	}

	return &Definition{
		Name:         fm.Name,
		Description:  fm.Description,
		SystemPrompt: sysPrompt,
		ToolsAllow:   fm.ToolsAllow,
		ToolsDeny:    fm.ToolsDeny,
		Model:        fm.Model,
		MaxTurns:     fm.MaxTurns,
		Effort:       fm.Effort,
	}, nil
}
