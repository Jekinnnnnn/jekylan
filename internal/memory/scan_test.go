package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScanMemoryFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a memory file with frontmatter
	content := `---
name: user_role
description: User is a senior Go engineer
type: user
---

User has been writing Go for 10 years.
`
	if err := os.WriteFile(filepath.Join(tmpDir, "user_role.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Create MEMORY.md (should be excluded)
	if err := os.WriteFile(filepath.Join(tmpDir, "MEMORY.md"), []byte("- [User](user_role.md)"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a file without frontmatter
	if err := os.WriteFile(filepath.Join(tmpDir, "plain.md"), []byte("Just some notes."), 0644); err != nil {
		t.Fatal(err)
	}

	headers, err := ScanMemoryFiles(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(headers) != 2 {
		t.Fatalf("expected 2 headers, got %d", len(headers))
	}

	// Should be sorted newest-first
	foundUser := false
	for _, h := range headers {
		if h.Filename == "user_role.md" {
			foundUser = true
			if h.Type != MemoryTypeUser {
				t.Errorf("expected type user, got %q", h.Type)
			}
			if h.Description != "User is a senior Go engineer" {
				t.Errorf("expected description from frontmatter, got %q", h.Description)
			}
		}
	}
	if !foundUser {
		t.Error("expected user_role.md in headers")
	}
}

func TestScanMemoryFilesNonExistent(t *testing.T) {
	headers, err := ScanMemoryFiles("/nonexistent/path")
	if err != nil {
		t.Fatalf("expected no error for non-existent dir, got %v", err)
	}
	if len(headers) != 0 {
		t.Errorf("expected 0 headers, got %d", len(headers))
	}
}

func TestFormatMemoryManifest(t *testing.T) {
	memories := []MemoryHeader{
		{Filename: "user.md", MtimeMs: 1713600000000, Description: "Go expert", Type: MemoryTypeUser},
		{Filename: "project.md", MtimeMs: 1713686400000, Description: "", Type: MemoryTypeProject},
	}
	manifest := FormatMemoryManifest(memories)
	if !strings.Contains(manifest, "user.md") {
		t.Error("expected user.md in manifest")
	}
	if !strings.Contains(manifest, "Go expert") {
		t.Error("expected description in manifest")
	}
	if !strings.Contains(manifest, "[user]") {
		t.Error("expected type tag")
	}
	if !strings.Contains(manifest, "project.md") {
		t.Error("expected project.md in manifest")
	}
}

func TestExtractFirstParagraph(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"First line.\n\nSecond paragraph.", "First line."},
		{"# Header\n\nBody text.", "Body text."},
		{"", ""},
		{"\n\n\n", ""},
	}
	for _, tc := range tests {
		got := extractFirstParagraph(tc.input)
		if got != tc.want {
			t.Errorf("extractFirstParagraph(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestSplitFrontmatter(t *testing.T) {
	content := "---\nname: test\n---\n\nBody here."
	fm, body := splitFrontmatter(content)
	if fm != "name: test" {
		t.Errorf("expected frontmatter 'name: test', got %q", fm)
	}
	if body != "Body here." {
		t.Errorf("expected body 'Body here.', got %q", body)
	}

	noFM, body2 := splitFrontmatter("Just body.")
	if noFM != "" {
		t.Error("expected empty frontmatter")
	}
	if body2 != "Just body." {
		t.Error("expected body without frontmatter")
	}
}
