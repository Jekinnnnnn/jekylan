package tool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// FileWriteTool writes files to disk with read-first safety checks.
type FileWriteTool struct {
	Tracker *FileStateTracker
}

func (t FileWriteTool) Name() string        { return "file_write" }
func (t FileWriteTool) Description() string { return "Write a file to the local filesystem." }
func (t FileWriteTool) SystemPrompt() string {
	return ""
}

func (t FileWriteTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"file_path": map[string]any{
				"type":        "string",
				"description": "Absolute or relative path to the file.",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "Content to write to the file.",
			},
		},
		"required": []string{"file_path", "content"},
	}
}

func (t FileWriteTool) Call(ctx context.Context, input map[string]any) (string, error) {
	path, ok := input["file_path"].(string)
	if !ok || path == "" {
		return "", fmt.Errorf("missing or invalid 'file_path' parameter")
	}

	content, ok := input["content"].(string)
	if !ok {
		return "", fmt.Errorf("missing or invalid 'content' parameter")
	}

	// Normalize path
	path = filepath.Clean(path)

	// Check if file exists
	fileExists := false
	info, err := os.Stat(path)
	if err == nil {
		fileExists = true
	}

	// Read-first enforcement: if file exists, it must have been read first
	if fileExists && t.Tracker != nil {
		_, wasRead := t.Tracker.GetState(path)
		if !wasRead {
			return "", fmt.Errorf("file has not been read yet. Read it first before writing to it: %s", path)
		}

		// Stale-write detection
		if !t.Tracker.WasReadSince(path, info.ModTime()) {
			return "", fmt.Errorf("file has been modified since read, either by the user or by a linter: %s", path)
		}
	}

	// Create parent directories if needed
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return "", fmt.Errorf("failed to create directory: %w", err)
		}
	}

	// Write file
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("write file error: %w", err)
	}

	// Update tracker
	if t.Tracker != nil {
		mtime := getFileMtime(path)
		t.Tracker.RecordRead(path, content, mtime)
	}

	action := "created"
	if fileExists {
		action = "updated"
	}
	return fmt.Sprintf("File %s: %s", action, path), nil
}
