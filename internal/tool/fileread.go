package tool

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// FileReadTool reads files from disk with optional offset/limit and line numbers.
type FileReadTool struct {
	Tracker *FileStateTracker
}

func (t FileReadTool) Name() string        { return "file_read" }
func (t FileReadTool) Description() string { return "Read a file from the local filesystem." }
func (t FileReadTool) SystemPrompt() string {
	return `To read files use file_read instead of cat, head, tail, or sed`
}

func (t FileReadTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"file_path": map[string]any{
				"type":        "string",
				"description": "Absolute or relative path to the file.",
			},
			"offset": map[string]any{
				"type":        "integer",
				"description": "Line number to start reading from (1-based). Default: 1.",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Number of lines to read.",
			},
		},
		"required": []string{"file_path"},
	}
}

func (t FileReadTool) Call(ctx context.Context, input map[string]any) (string, error) {
	path, ok := input["file_path"].(string)
	if !ok || path == "" {
		return "", fmt.Errorf("missing or invalid 'file_path' parameter")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("file does not exist: %s", path)
		}
		return "", fmt.Errorf("read file error: %w", err)
	}

	content := string(data)

	// Apply offset/limit if specified
	offset := 1
	if v, ok := getInt(input, "offset"); ok && v > 0 {
		offset = v
	}
	limit := 0
	if v, ok := getInt(input, "limit"); ok && v > 0 {
		limit = v
	}

	if offset > 1 || limit > 0 {
		content = applyLineRange(content, offset, limit)
	}

	// Add line numbers
	content = addLineNumbers(content, offset)

	// Record read state for write/edit enforcement
	if t.Tracker != nil {
		mtime := getFileMtime(path)
		t.Tracker.RecordRead(path, string(data), mtime)
	}

	return content, nil
}

// applyLineRange extracts a range of lines from content.
func applyLineRange(content string, offset, limit int) string {
	lines := strings.Split(content, "\n")
	if offset > len(lines) {
		return ""
	}
	// convert to 0-based
	start := max(offset-1, 0)
	end := len(lines)
	if limit > 0 {
		end = min(start+limit, len(lines))
	}
	return strings.Join(lines[start:end], "\n")
}

// addLineNumbers prefixes each line with its line number.
func addLineNumbers(content string, startLine int) string {
	lines := strings.Split(content, "\n")
	var out strings.Builder
	for i, line := range lines {
		fmt.Fprintf(&out, "%d\t%s\n", startLine+i, line)
	}
	return out.String()
}
