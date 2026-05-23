package memory

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestFindRelevantMemories(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	content1 := `---
name: go_expert
description: User is a senior Go engineer
type: user
---

User writes Go.`
	if err := os.WriteFile(filepath.Join(tmpDir, "go.md"), []byte(content1), 0644); err != nil {
		t.Fatal(err)
	}

	content2 := `---
name: react_notes
description: React frontend tips
type: reference
---

React patterns.`
	if err := os.WriteFile(filepath.Join(tmpDir, "react.md"), []byte(content2), 0644); err != nil {
		t.Fatal(err)
	}

	// Create MEMORY.md referencing both files
	memIdx := "- [Go](go.md) — Go expert\n- [React](react.md) — React tips\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "MEMORY.md"), []byte(memIdx), 0644); err != nil {
		t.Fatal(err)
	}

	// Query matching "go" should return go.md
	results := FindRelevantMemories(ctx, "I need help with Go code", tmpDir, nil, nil)
	foundGo := false
	for _, r := range results {
		if filepath.Base(r.Path) == "go.md" {
			foundGo = true
			break
		}
	}
	if !foundGo {
		t.Error("expected go.md to be relevant to Go query")
	}

	// alreadySurfaced should filter out
	surfaced := map[string]bool{filepath.Join(tmpDir, "go.md"): true}
	results = FindRelevantMemories(ctx, "Go code", tmpDir, surfaced, nil)
	for _, r := range results {
		if filepath.Base(r.Path) == "go.md" {
			t.Error("expected already surfaced file to be excluded")
		}
	}
}

func TestFindRelevantMemoriesEmptyDir(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	results := FindRelevantMemories(ctx, "anything", tmpDir, nil, nil)
	if len(results) != 0 {
		t.Errorf("expected 0 results for empty dir, got %d", len(results))
	}
}
