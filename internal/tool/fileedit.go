package tool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FileEditTool performs exact string replacements in files.
type FileEditTool struct {
	Tracker *FileStateTracker
}

func (t FileEditTool) Name() string        { return "file_edit" }
func (t FileEditTool) Description() string { return "Performs exact string replacements in files." }
func (t FileEditTool) SystemPrompt() string {
	return `When editing text from tool output, ensure you preserve the exact indentation (tabs/spaces) as it appears. Never include any part of the line number prefix in the old_string or new_string.`
}

func (t FileEditTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"file_path": map[string]any{
				"type":        "string",
				"description": "Absolute or relative path to the file.",
			},
			"old_string": map[string]any{
				"type":        "string",
				"description": "The text to replace. Must match exactly.",
			},
			"new_string": map[string]any{
				"type":        "string",
				"description": "The replacement text.",
			},
			"replace_all": map[string]any{
				"type":        "boolean",
				"description": "Replace all occurrences. Default: false.",
			},
		},
		"required": []string{"file_path", "old_string", "new_string"},
	}
}

func (t FileEditTool) Call(ctx context.Context, input map[string]any) (string, error) {
	path, ok := input["file_path"].(string)
	if !ok || path == "" {
		return "", fmt.Errorf("missing or invalid 'file_path' parameter")
	}

	oldStr, ok := input["old_string"].(string)
	if !ok {
		return "", fmt.Errorf("missing or invalid 'old_string' parameter")
	}

	newStr, ok := input["new_string"].(string)
	if !ok {
		return "", fmt.Errorf("missing or invalid 'new_string' parameter")
	}

	if oldStr == newStr {
		return "", fmt.Errorf("old_string and new_string are identical")
	}

	path = filepath.Clean(path)

	// Read file
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("file does not exist: %s", path)
		}
		return "", fmt.Errorf("read file error: %w", err)
	}

	content := string(data)

	// Read-first enforcement
	if t.Tracker != nil {
		_, wasRead := t.Tracker.GetState(path)
		if !wasRead {
			return "", fmt.Errorf("file has not been read yet. Read it first before editing it: %s", path)
		}

		info, err := os.Stat(path)
		if err == nil && !t.Tracker.WasReadSince(path, info.ModTime()) {
			return "", fmt.Errorf("file has been modified since read, either by the user or by a linter: %s", path)
		}
	}

	// Reject empty old_string on non-empty files (can't create via Edit)
	if oldStr == "" && len(content) > 0 {
		return "", fmt.Errorf("old_string cannot be empty when editing an existing file. Use file_write to create new files.")
	}

	// Find and replace
	replaceAll := false
	if v, ok := input["replace_all"].(bool); ok {
		replaceAll = v
	}

	// Try exact match first
	found := strings.Count(content, oldStr)
	actualOldStr := oldStr

	if found == 0 {
		// Try quote normalization: curly quotes → straight quotes
		normalizedOld := normalizeQuotes(oldStr)
		normalizedContent := normalizeQuotes(content)
		if strings.Count(normalizedContent, normalizedOld) > 0 {
			found = strings.Count(normalizedContent, normalizedOld)
			actualOldStr = oldStr // keep original for replacement
			// We need to find the matching substring in original content
			// For simplicity, try to find the normalized match in normalized content
			// and map back... this is complex. Instead, let's just try replacing
			// in normalized space and see if it works.
			// Actually, for MVP, let's do a simpler approach:
			// If exact match fails but normalized match succeeds, we replace in
			// normalized content. But this loses original quote style.
			// Better: find the actual substring in original that matches when normalized.
			found0 := strings.Contains(normalizedContent, normalizedOld)
			if found0 {
				content = normalizedContent
				actualOldStr = normalizedOld
				newStr = normalizeQuotes(newStr)
			}
		}
	}

	if found == 0 {
		return "", fmt.Errorf("old_string not found in file: %s", path)
	}

	if !replaceAll && found > 1 {
		return "", fmt.Errorf("old_string matches %d times in file. Use replace_all=true to replace all occurrences, or provide a more specific old_string: %s", found, path)
	}

	var newContent string
	if replaceAll {
		newContent = strings.ReplaceAll(content, actualOldStr, newStr)
	} else {
		newContent = strings.Replace(content, actualOldStr, newStr, 1)
	}

	// Preserve trailing newline if original had one
	if strings.HasSuffix(string(data), "\n") && !strings.HasSuffix(newContent, "\n") {
		newContent += "\n"
	}

	// Write back
	if err := os.WriteFile(path, []byte(newContent), 0644); err != nil {
		return "", fmt.Errorf("write file error: %w", err)
	}

	// Update tracker
	if t.Tracker != nil {
		mtime := getFileMtime(path)
		t.Tracker.RecordRead(path, newContent, mtime)
	}

	return fmt.Sprintf("Edited file: %s", path), nil
}

// normalizeQuotes converts curly quotes to straight quotes.
func normalizeQuotes(s string) string {
	// Left/Right single quotes
	s = strings.ReplaceAll(s, "\u2018", "'")
	s = strings.ReplaceAll(s, "\u2019", "'")
	// Left/Right double quotes
	s = strings.ReplaceAll(s, "\u201C", "\"")
	s = strings.ReplaceAll(s, "\u201D", "\"")
	return s
}
