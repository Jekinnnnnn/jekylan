package markdownutil

import (
	"strings"
)

// SplitFrontmatter splits a markdown file into YAML frontmatter and body.
// Supports the --- delimited format.
func SplitFrontmatter(content string) (frontmatter, body string, err error) {
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

// ExtractDescriptionFromMarkdown tries to extract a description from the first
// paragraph of markdown content.
func ExtractDescriptionFromMarkdown(content string) string {
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
