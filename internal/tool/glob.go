package tool

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// GlobTool finds files matching a glob pattern.
type GlobTool struct{}

func (GlobTool) Name() string        { return "glob" }
func (GlobTool) Description() string { return "Fast file pattern matching tool that works with any codebase size." }
func (GlobTool) SystemPrompt() string {
	return `Do NOT use the bash tool to run find or glob commands. Use the dedicated Glob tool instead.`
}

func (GlobTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"pattern": map[string]any{
				"type":        "string",
				"description": "Glob pattern to match files against (e.g., '*.go', 'src/**/*.ts').",
			},
			"path": map[string]any{
				"type":        "string",
				"description": "Directory to search in. Defaults to current working directory.",
			},
		},
		"required": []string{"pattern"},
	}
}

func (GlobTool) Call(ctx context.Context, input map[string]any) (string, error) {
	pattern, ok := input["pattern"].(string)
	if !ok || pattern == "" {
		return "", fmt.Errorf("missing or invalid 'pattern' parameter")
	}

	searchPath := "."
	if v, ok := input["path"].(string); ok && v != "" {
		searchPath = v
	}

	// If pattern is an absolute path, extract base directory
	if filepath.IsAbs(pattern) {
		dir := filepath.Dir(pattern)
		base := filepath.Base(pattern)
		if dir != "/" {
			searchPath = dir
			pattern = base
		}
	}

	// Validate search path exists and is a directory
	info, err := os.Stat(searchPath)
	if err != nil {
		return "", fmt.Errorf("path error: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("path is not a directory: %s", searchPath)
	}

	// Build ripgrep --files command with glob filter
	args := []string{"--files", searchPath, "--glob", pattern}

	cmd := exec.CommandContext(ctx, "rg", args...)
	out, err := cmd.CombinedOutput()
	output := string(out)

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return "", nil
		}
		return output, fmt.Errorf("glob error: %w", err)
	}

	// Convert absolute paths to relative, sort by mtime
	lines := strings.Split(strings.TrimSpace(output), "\n")
	var files []struct {
		path  string
		mtime int64
	}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Convert to relative path from searchPath
		rel := line
		if filepath.IsAbs(line) {
			if r, err := filepath.Rel(searchPath, line); err == nil {
				rel = r
			}
		}
		p := line
		if !filepath.IsAbs(p) {
			p = filepath.Join(searchPath, p)
		}
		info, err := os.Stat(p)
		var mtime int64
		if err == nil {
			mtime = info.ModTime().Unix()
		}
		files = append(files, struct {
			path  string
			mtime int64
		}{path: rel, mtime: mtime})
	}

	// Sort by modification time (newest first)
	sort.Slice(files, func(i, j int) bool {
		if files[i].mtime != files[j].mtime {
			return files[i].mtime > files[j].mtime
		}
		return files[i].path < files[j].path
	})

	// Limit to 100 results
	const maxResults = 100
	truncated := false
	if len(files) > maxResults {
		files = files[:maxResults]
		truncated = true
	}

	var outLines []string
	for _, f := range files {
		outLines = append(outLines, f.path)
	}
	result := strings.Join(outLines, "\n")
	if truncated {
		result += fmt.Sprintf("\n... (truncated, %d total matches)\n", len(files))
	}
	return result, nil
}
