package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseEntrypointLinks(t *testing.T) {
	tmpDir := t.TempDir()

	// No MEMORY.md → nil
	links := ParseEntrypointLinks(tmpDir)
	if links != nil {
		t.Error("expected nil when MEMORY.md doesn't exist")
	}

	// Empty MEMORY.md → empty map
	if err := os.WriteFile(filepath.Join(tmpDir, "MEMORY.md"), []byte(""), 0644); err != nil {
		t.Fatal(err)
	}
	links = ParseEntrypointLinks(tmpDir)
	if len(links) != 0 {
		t.Error("expected empty map for empty MEMORY.md")
	}

	// MEMORY.md with links
	content := `- [User](user_role.md) — user is a senior Go engineer
- [React](feedback/react.md) — React tips
- [Project](project/init.md) — project setup notes
`
	if err := os.WriteFile(filepath.Join(tmpDir, "MEMORY.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	links = ParseEntrypointLinks(tmpDir)
	if len(links) != 3 {
		t.Fatalf("expected 3 links, got %d", len(links))
	}
	for _, path := range []string{"user_role.md", "feedback/react.md", "project/init.md"} {
		if !links[path] {
			t.Errorf("expected link %q to be found", path)
		}
	}
}

func TestScanMemoryFilesOnlyIndexed(t *testing.T) {
	tmpDir := t.TempDir()

	// Create MEMORY.md referencing only user_role.md
	memIdx := "- [User](user_role.md) — user role\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "MEMORY.md"), []byte(memIdx), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a referenced file
	referenced := `---
name: user_role
description: User is a senior Go engineer
type: user
---

User writes Go.`
	if err := os.WriteFile(filepath.Join(tmpDir, "user_role.md"), []byte(referenced), 0644); err != nil {
		t.Fatal(err)
	}

	// Create an unreferenced file (simulates skill-executions)
	os.MkdirAll(filepath.Join(tmpDir, "skill-executions"), 0755)
	unref := `---
skill: test
date: 2026-05-01
---

Execution log content.`
	if err := os.WriteFile(filepath.Join(tmpDir, "skill-executions", "log.md"), []byte(unref), 0644); err != nil {
		t.Fatal(err)
	}

	headers, err := ScanMemoryFiles(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(headers) != 1 {
		t.Fatalf("expected 1 header (only indexed file), got %d", len(headers))
	}
	if headers[0].Filename != "user_role.md" {
		t.Errorf("expected user_role.md, got %q", headers[0].Filename)
	}
}

func TestScanMemoryFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Create MEMORY.md referencing both files
	memIdx := "- [User](user_role.md) — user role\n- [Plain](plain.md) — plain notes\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "MEMORY.md"), []byte(memIdx), 0644); err != nil {
		t.Fatal(err)
	}

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

