package playbook

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Registry holds loaded playbooks indexed by name.
type Registry struct {
	playbooks map[string]*Playbook
}

// NewRegistry creates an empty playbook registry.
func NewRegistry() *Registry {
	return &Registry{playbooks: make(map[string]*Playbook)}
}

// Register adds a playbook to the registry.
func (r *Registry) Register(p *Playbook) {
	if r.playbooks == nil {
		r.playbooks = make(map[string]*Playbook)
	}
	r.playbooks[p.Name] = p
}

// Find looks up a playbook by name. Returns nil if not found.
func (r *Registry) Find(name string) *Playbook {
	return r.playbooks[name]
}

// All returns all registered playbooks.
func (r *Registry) All() []*Playbook {
	out := make([]*Playbook, 0, len(r.playbooks))
	for _, p := range r.playbooks {
		out = append(out, p)
	}
	return out
}

// LoadDir loads all playbooks from a directory.
// Each playbook must be a .md file directly inside the directory:
//
//	playbooks/calculate.md
//	playbooks/deploy.md
func LoadDir(basePath string) (*Registry, error) {
	r := NewRegistry()

	entries, err := os.ReadDir(basePath)
	if err != nil {
		if os.IsNotExist(err) {
			return r, nil
		}
		return r, fmt.Errorf("read playbooks dir: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		playbookPath := filepath.Join(basePath, entry.Name())
		data, err := os.ReadFile(playbookPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[playbook] skip %s: read: %v\n", entry.Name(), err)
			continue
		}

		playbookName := strings.TrimSuffix(entry.Name(), ".md")
		p, err := parsePlaybookFile(playbookName, string(data), basePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[playbook] skip %s: parse: %v\n", entry.Name(), err)
			continue
		}
		if p == nil {
			continue
		}

		r.Register(p)
	}

	return r, nil
}
