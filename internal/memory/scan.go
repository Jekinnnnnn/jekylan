package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Jekinnnnnn/jekylan/internal/markdownutil"
	"gopkg.in/yaml.v3"
)

var linkPattern = regexp.MustCompile(`\[[^\]]*\]\(([^)]+)\)`)

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

// ParseEntrypointLinks reads MEMORY.md and extracts all markdown link paths
// like [Title](path.md), returning a set of relative paths.
func ParseEntrypointLinks(memoryDir string) map[string]bool {
	data, err := os.ReadFile(filepath.Join(memoryDir, entrypointName))
	if err != nil {
		return nil
	}
	links := make(map[string]bool)
	for _, match := range linkPattern.FindAllStringSubmatch(string(data), -1) {
		links[match[1]] = true
	}
	return links
}

// ScanMemoryFiles scans a memory directory recursively for .md files listed in
// MEMORY.md, reads their frontmatter, and returns a header list sorted newest-first
// (capped at maxMemoryFiles).
func ScanMemoryFiles(memoryDir string) ([]MemoryHeader, error) {
	allowed := ParseEntrypointLinks(memoryDir)
	if len(allowed) == 0 {
		return nil, nil
	}

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
			relPath, _ := filepath.Rel(memoryDir, path)
			if !allowed[relPath] {
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
			fm, body, _ := markdownutil.SplitFrontmatter(content)
			var fd frontmatterData
			if fm != "" {
				_ = yaml.Unmarshal([]byte(fm), &fd)
			}
			desc := fd.Description
			if desc == "" {
				desc = markdownutil.ExtractDescriptionFromMarkdown(body)
			}
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

