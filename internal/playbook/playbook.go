package playbook

import (
	"fmt"

	"github.com/Jekinnnnnn/jekylan/internal/markdownutil"
	"gopkg.in/yaml.v3"
)

// Playbook represents a loaded playbook (a workflow definition with frontmatter metadata).
type Playbook struct {
	Name        string
	Description string
	WhenToUse   string
	Content     string // markdown body after frontmatter
	PlaybookRoot string
}

// frontmatterData holds the parsed YAML frontmatter from a playbook .md file.
type frontmatterData struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	WhenToUse   string `yaml:"when_to_use"`
	Enabled     *bool  `yaml:"enabled"`
}

// parsePlaybookFile parses a playbook markdown file into a Playbook.
func parsePlaybookFile(playbookName, rawContent, playbookDir string) (*Playbook, error) {
	frontmatter, content, err := markdownutil.SplitFrontmatter(rawContent)
	if err != nil {
		return nil, err
	}

	var fm frontmatterData
	if frontmatter != "" {
		if err := yaml.Unmarshal([]byte(frontmatter), &fm); err != nil {
			return nil, fmt.Errorf("parse frontmatter: %w", err)
		}
	}

	// Explicitly disabled — skip.
	if fm.Enabled != nil && !*fm.Enabled {
		return nil, nil
	}

	// Prefer name from frontmatter, fall back to directory/file name.
	name := playbookName
	if fm.Name != "" {
		name = fm.Name
	}

	description := fm.Description
	if description == "" {
		description = markdownutil.ExtractDescriptionFromMarkdown(content)
	}

	return &Playbook{
		Name:         name,
		Description:  description,
		WhenToUse:    fm.WhenToUse,
		Content:      content,
		PlaybookRoot: playbookDir,
	}, nil
}

