package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	maxMemoryFiles      = 200
	frontmatterMaxLines = 30
)

// MemoryHeader holds parsed frontmatter for a single memory file.
type MemoryHeader struct {
	Filename    string
	FilePath    string
	MtimeMs     int64
	Description string
	Type        MemoryType
}

// frontmatterData holds the parsed YAML frontmatter from a memory file.
type frontmatterData struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Type        string `yaml:"type"`
}

// ScanMemoryFiles scans a memory directory recursively for .md files, reads their
// frontmatter, and returns a header list sorted newest-first
// (capped at maxMemoryFiles).
func ScanMemoryFiles(memoryDir string) ([]MemoryHeader, error) {
	var headers []MemoryHeader
	var walk func(dir string) error
	walk = func(dir string) error {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		headers = make([]MemoryHeader, 0, len(entries))
		for _, entry := range entries {
			path := filepath.Join(dir, entry.Name())
			if entry.IsDir() {
				if err := walk(path); err != nil {
					return err
				}
				continue
			}
			name := entry.Name()
			if !strings.HasSuffix(name, ".md") || name == entrypointName {
				continue
			}
			info, err := os.Stat(path)
			if err != nil {
				continue
			}
			content, err := readFileInRange(path, frontmatterMaxLines)
			if err != nil {
				continue
			}
			fm, body := splitFrontmatter(content)
			var fd frontmatterData
			if fm != "" {
				_ = yaml.Unmarshal([]byte(fm), &fd)
			}
			desc := fd.Description
			if desc == "" {
				desc = extractFirstParagraph(body)
			}
			// Use relative path from memoryDir as the Filename for display.
			relPath, _ := filepath.Rel(memoryDir, path)
			headers = append(headers, MemoryHeader{
				Filename:    relPath,
				FilePath:    path,
				MtimeMs:     info.ModTime().UnixMilli(),
				Description: desc,
				Type:        ParseMemoryType(fd.Type),
			})
		}
		return nil
	}

	if err := walk(memoryDir); err != nil {
		return nil, err
	}

	// Sort newest-first
	sort.Slice(headers, func(i, j int) bool {
		return headers[i].MtimeMs > headers[j].MtimeMs
	})

	if len(headers) > maxMemoryFiles {
		headers = headers[:maxMemoryFiles]
	}
	return headers, nil
}

// FormatMemoryManifest formats memory headers as a text manifest: one line per
// file with [type] filename (timestamp): description.
func FormatMemoryManifest(memories []MemoryHeader) string {
	var lines []string
	for _, m := range memories {
		tag := ""
		if m.Type != "" {
			tag = fmt.Sprintf("[%s] ", m.Type)
		}
		ts := time.UnixMilli(m.MtimeMs).Format(time.RFC3339)
		if m.Description != "" {
			lines = append(lines, fmt.Sprintf("- %s%s (%s): %s", tag, m.Filename, ts, m.Description))
		} else {
			lines = append(lines, fmt.Sprintf("- %s%s (%s)", tag, m.Filename, ts))
		}
	}
	return strings.Join(lines, "\n")
}

// readFileInRange reads up to maxLines from a file.
func readFileInRange(filePath string, maxLines int) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) > maxLines {
		lines = lines[:maxLines]
	}
	return strings.Join(lines, "\n"), nil
}

// splitFrontmatter splits a markdown file into YAML frontmatter and body.
func splitFrontmatter(content string) (frontmatter, body string) {
	trimmed := strings.TrimSpace(content)
	if !strings.HasPrefix(trimmed, "---") {
		return "", content
	}
	parts := strings.SplitN(trimmed, "---", 3)
	if len(parts) < 3 {
		return "", content
	}
	return strings.TrimSpace(parts[1]), strings.TrimSpace(parts[2])
}

// extractFirstParagraph extracts the first non-empty paragraph from markdown content.
func extractFirstParagraph(content string) string {
	lines := strings.Split(content, "\n")
	var para strings.Builder
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if para.Len() > 0 {
				break
			}
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		para.WriteString(trimmed)
		para.WriteString(" ")
		if para.Len() > 200 {
			break
		}
	}
	return strings.TrimSpace(para.String())
}
