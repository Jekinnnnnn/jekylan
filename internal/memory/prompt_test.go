package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTruncateEntrypointContent(t *testing.T) {
	// Small content — no truncation
	small := "line1\nline2\nline3"
	tr := TruncateEntrypointContent(small)
	if tr.WasLineTruncated || tr.WasByteTruncated {
		t.Error("expected no truncation for small content")
	}
	if tr.LineCount != 3 {
		t.Errorf("expected line count 3, got %d", tr.LineCount)
	}

	// Line truncation
	var manyLines []string
	for i := 0; i < maxEntrypointLines+10; i++ {
		manyLines = append(manyLines, "x")
	}
	tr = TruncateEntrypointContent(strings.Join(manyLines, "\n"))
	if !tr.WasLineTruncated {
		t.Error("expected line truncation")
	}
	if !strings.Contains(tr.Content, "WARNING") {
		t.Error("expected warning in truncated content")
	}

	// Byte truncation (long single line)
	longLine := strings.Repeat("a", maxEntrypointBytes+100)
	tr = TruncateEntrypointContent(longLine)
	if !tr.WasByteTruncated {
		t.Error("expected byte truncation")
	}
}

func TestBuildMemoryLines(t *testing.T) {
	lines := BuildMemoryLines("/tmp/test-memory", nil)
	if len(lines) == 0 {
		t.Fatal("expected non-empty lines")
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "auto memory") {
		t.Error("expected 'auto memory' header")
	}
	if !strings.Contains(joined, "Types of memory") {
		t.Error("expected types section")
	}
	if !strings.Contains(joined, "How to save memories") {
		t.Error("expected how-to-save section")
	}
	if !strings.Contains(joined, "What NOT to save") {
		t.Error("expected what-not-to-save section")
	}
	if !strings.Contains(joined, "Before recommending from memory") {
		t.Error("expected trusting recall section")
	}
	if !strings.Contains(joined, "Searching past context") {
		t.Error("expected searching past context section")
	}
	if !strings.Contains(joined, "session_memory.md") {
		t.Error("expected session memory search guidance")
	}
}

func TestBuildMemoryPrompt(t *testing.T) {
	tmpDir := t.TempDir()

	// Without MEMORY.md
	prompt := BuildMemoryPrompt(tmpDir)
	if !strings.Contains(prompt, "currently empty") {
		t.Error("expected empty MEMORY.md message")
	}

	// With MEMORY.md
	if err := os.WriteFile(filepath.Join(tmpDir, "MEMORY.md"), []byte("- [User](user.md) — user role"), 0644); err != nil {
		t.Fatal(err)
	}
	prompt = BuildMemoryPrompt(tmpDir)
	if !strings.Contains(prompt, "user role") {
		t.Error("expected MEMORY.md content")
	}
}
