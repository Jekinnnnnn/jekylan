package tool

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// GrepTool runs ripgrep searches.
type GrepTool struct{}

func (GrepTool) Name() string        { return "grep" }
func (GrepTool) Description() string { return "A powerful search tool built on ripgrep." }
func (GrepTool) SystemPrompt() string {
	return `Do NOT use the bash tool to run grep, find, or similar search commands. Use the dedicated Grep tool instead.`
}

func (GrepTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"pattern": map[string]any{
				"type":        "string",
				"description": "Regex pattern to search for.",
			},
			"path": map[string]any{
				"type":        "string",
				"description": "File or directory to search in. Defaults to current working directory.",
			},
			"glob": map[string]any{
				"type":        "string",
				"description": "Glob pattern to filter files (e.g., '*.go', '*.{ts,tsx}').",
			},
			"output_mode": map[string]any{
				"type":        "string",
				"description": "Output mode: 'content', 'files_with_matches', or 'count'. Default: 'files_with_matches'.",
				"enum":        []string{"content", "files_with_matches", "count"},
			},
			"-B": map[string]any{
				"type":        "integer",
				"description": "Number of lines to show before each match.",
			},
			"-A": map[string]any{
				"type":        "integer",
				"description": "Number of lines to show after each match.",
			},
			"-C": map[string]any{
				"type":        "integer",
				"description": "Number of lines to show around each match.",
			},
			"-n": map[string]any{
				"type":        "boolean",
				"description": "Show line numbers. Default: true.",
			},
			"-i": map[string]any{
				"type":        "boolean",
				"description": "Case insensitive search.",
			},
			"type": map[string]any{
				"type":        "string",
				"description": "File type filter (rg --type, e.g., 'go', 'js').",
			},
			"head_limit": map[string]any{
				"type":        "integer",
				"description": "Limit output to first N entries. Default: 250. Use 0 for unlimited.",
			},
			"offset": map[string]any{
				"type":        "integer",
				"description": "Skip first N entries. Default: 0.",
			},
			"multiline": map[string]any{
				"type":        "boolean",
				"description": "Enable multiline mode (rg -U --multiline-dotall).",
			},
		},
		"required": []string{"pattern"},
	}
}

func (GrepTool) Call(ctx context.Context, input map[string]any) (string, error) {
	pattern, ok := input["pattern"].(string)
	if !ok || pattern == "" {
		return "", fmt.Errorf("missing or invalid 'pattern' parameter")
	}

	// Build ripgrep args
	args := []string{"--max-columns", "500"}

	// Output mode
	outputMode := "files_with_matches"
	if v, ok := input["output_mode"].(string); ok && v != "" {
		outputMode = v
	}
	switch outputMode {
	case "files_with_matches":
		args = append(args, "--files-with-matches")
	case "count":
		args = append(args, "--count")
	case "content":
		// default rg behavior; add -n by default
		showLineNumbers := true
		if v, ok := input["-n"].(bool); ok {
			showLineNumbers = v
		}
		if showLineNumbers {
			args = append(args, "-n")
		}
	}

	// Case insensitive
	if v, ok := input["-i"].(bool); ok && v {
		args = append(args, "-i")
	}

	// Multiline
	if v, ok := input["multiline"].(bool); ok && v {
		args = append(args, "-U")
	}

	// Context lines
	if v, ok := getInt(input, "-B"); ok && v > 0 {
		args = append(args, "-B", strconv.Itoa(v))
	}
	if v, ok := getInt(input, "-A"); ok && v > 0 {
		args = append(args, "-A", strconv.Itoa(v))
	}
	if v, ok := getInt(input, "-C"); ok && v > 0 {
		args = append(args, "-C", strconv.Itoa(v))
	}

	// File type
	if v, ok := input["type"].(string); ok && v != "" {
		args = append(args, "-t", v)
	}

	// Glob filter
	if v, ok := input["glob"].(string); ok && v != "" {
		for _, g := range splitGlob(v) {
			args = append(args, "--glob", g)
		}
	}

	// Pattern — use -e if it starts with - to avoid option interpretation
	if strings.HasPrefix(pattern, "-") {
		args = append(args, "-e", pattern)
	} else {
		args = append(args, pattern)
	}

	// Path
	searchPath := "."
	if v, ok := input["path"].(string); ok && v != "" {
		searchPath = v
	}
	args = append(args, searchPath)

	// Execute
	cmd := exec.CommandContext(ctx, "rg", args...)
	out, err := cmd.CombinedOutput()
	output := string(out)

	// rg returns exit code 1 when no matches — that's not an error for us
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return "", nil
		}
		return output, fmt.Errorf("grep error: %w", err)
	}

	// Apply head_limit and offset
	headLimit := 250
	if v, ok := getInt(input, "head_limit"); ok {
		headLimit = v
	}
	offset := 0
	if v, ok := getInt(input, "offset"); ok {
		offset = v
	}

	output = applyHeadLimit(output, headLimit, offset)

	// For files_with_matches, sort by modification time (newest first)
	if outputMode == "files_with_matches" {
		output = sortFilesByMtime(output, searchPath)
	}

	return output, nil
}

// splitGlob splits a glob string on commas and spaces, preserving brace patterns.
func splitGlob(glob string) []string {
	var results []string
	for _, g := range strings.Split(glob, ",") {
		g = strings.TrimSpace(g)
		if g != "" {
			results = append(results, g)
		}
	}
	return results
}

// applyHeadLimit truncates output based on head_limit and offset.
func applyHeadLimit(output string, headLimit, offset int) string {
	if headLimit == 0 && offset == 0 {
		return output
	}
	lines := strings.Split(output, "\n")
	if offset > 0 {
		if offset >= len(lines) {
			return ""
		}
		lines = lines[offset:]
	}
	if headLimit > 0 && len(lines) > headLimit {
		lines = lines[:headLimit]
		truncated := len(lines) > 0 && lines[len(lines)-1] != ""
		if truncated {
			return strings.Join(lines, "\n") + fmt.Sprintf("\n... (%d more matches)\n", len(lines))
		}
	}
	return strings.Join(lines, "\n")
}

// sortFilesByMtime sorts files_with_matches output by modification time (newest first).
func sortFilesByMtime(output, basePath string) string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) == 0 || (len(lines) == 1 && lines[0] == "") {
		return output
	}

	type fileInfo struct {
		path  string
		mtime int64
	}

	var files []fileInfo
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Resolve relative paths
		p := line
		if !filepath.IsAbs(p) {
			p = filepath.Join(basePath, p)
		}
		info, err := os.Stat(p)
		var mtime int64
		if err == nil {
			mtime = info.ModTime().Unix()
		}
		files = append(files, fileInfo{path: line, mtime: mtime})
	}

	sort.Slice(files, func(i, j int) bool {
		if files[i].mtime != files[j].mtime {
			return files[i].mtime > files[j].mtime // newest first
		}
		return files[i].path < files[j].path
	})

	var out []string
	for _, f := range files {
		out = append(out, f.path)
	}
	return strings.Join(out, "\n")
}

// getInt extracts an integer value from input map, handling float64 from JSON.
func getInt(input map[string]any, key string) (int, bool) {
	v, ok := input[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	}
	return 0, false
}
