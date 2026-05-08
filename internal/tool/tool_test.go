package tool

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- FileReadTool tests ---

func TestFileReadToolBasic(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "test.txt")
	os.WriteFile(f, []byte("line1\nline2\nline3\n"), 0644)

	tool := FileReadTool{}
	result, err := tool.Call(context.Background(), map[string]any{
		"file_path": f,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "1\tline1") {
		t.Errorf("expected line numbers, got: %s", result)
	}
}

func TestFileReadToolOffsetLimit(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "test.txt")
	os.WriteFile(f, []byte("a\nb\nc\nd\ne\n"), 0644)

	tool := FileReadTool{}
	result, err := tool.Call(context.Background(), map[string]any{
		"file_path": f,
		"offset":    2,
		"limit":     2,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(result), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %s", len(lines), result)
	}
	if !strings.Contains(lines[0], "2\tb") {
		t.Errorf("expected line 2 to be 'b', got: %s", lines[0])
	}
	if !strings.Contains(lines[1], "3\tc") {
		t.Errorf("expected line 3 to be 'c', got: %s", lines[1])
	}
}

func TestFileReadToolTracker(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "test.txt")
	os.WriteFile(f, []byte("hello"), 0644)

	tracker := NewFileStateTracker()
	tool := FileReadTool{Tracker: tracker}
	tool.Call(context.Background(), map[string]any{"file_path": f})

	state, ok := tracker.GetState(f)
	if !ok {
		t.Fatal("expected file to be tracked")
	}
	if state.Content != "hello" {
		t.Errorf("expected content 'hello', got: %s", state.Content)
	}
}

// --- FileWriteTool tests ---

func TestFileWriteToolCreate(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "newfile.txt")

	tracker := NewFileStateTracker()
	tool := FileWriteTool{Tracker: tracker}
	result, err := tool.Call(context.Background(), map[string]any{
		"file_path": f,
		"content":   "new content",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "created") {
		t.Errorf("expected 'created' in result, got: %s", result)
	}

	data, _ := os.ReadFile(f)
	if string(data) != "new content" {
		t.Errorf("expected 'new content', got: %s", string(data))
	}
}

func TestFileWriteToolReadFirstRejection(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "existing.txt")
	os.WriteFile(f, []byte("original"), 0644)

	tracker := NewFileStateTracker()
	tool := FileWriteTool{Tracker: tracker}
	_, err := tool.Call(context.Background(), map[string]any{
		"file_path": f,
		"content":   "new content",
	})
	if err == nil {
		t.Fatal("expected error for unread file")
	}
	if !strings.Contains(err.Error(), "not been read yet") {
		t.Errorf("expected read-first error, got: %v", err)
	}
}

func TestFileWriteToolStaleWriteRejection(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "existing.txt")
	os.WriteFile(f, []byte("original"), 0644)

	tracker := NewFileStateTracker()
	// Record read
	tracker.RecordRead(f, "original", getFileMtime(f))

	// Simulate external modification — sleep to ensure mtime changes
	// even on filesystems with 1-second granularity
	time.Sleep(1100 * time.Millisecond)
	os.WriteFile(f, []byte("modified externally"), 0644)

	tool := FileWriteTool{Tracker: tracker}
	_, err := tool.Call(context.Background(), map[string]any{
		"file_path": f,
		"content":   "new content",
	})
	if err == nil {
		t.Fatal("expected error for stale file")
	}
	if !strings.Contains(err.Error(), "modified since read") {
		t.Errorf("expected stale-write error, got: %v", err)
	}
}

// --- FileEditTool tests ---

func TestFileEditToolBasic(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "edit.txt")
	os.WriteFile(f, []byte("hello world\n"), 0644)

	tracker := NewFileStateTracker()
	tracker.RecordRead(f, "hello world\n", getFileMtime(f))

	tool := FileEditTool{Tracker: tracker}
	result, err := tool.Call(context.Background(), map[string]any{
		"file_path":  f,
		"old_string": "world",
		"new_string": "universe",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "Edited file") {
		t.Errorf("expected 'Edited file' in result, got: %s", result)
	}

	data, _ := os.ReadFile(f)
	if string(data) != "hello universe\n" {
		t.Errorf("expected 'hello universe\\n', got: %q", string(data))
	}
}

func TestFileEditToolReplaceAll(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "edit.txt")
	os.WriteFile(f, []byte("foo bar foo\n"), 0644)

	tracker := NewFileStateTracker()
	tracker.RecordRead(f, "foo bar foo\n", getFileMtime(f))

	tool := FileEditTool{Tracker: tracker}
	_, err := tool.Call(context.Background(), map[string]any{
		"file_path":   f,
		"old_string":  "foo",
		"new_string":  "baz",
		"replace_all": true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, _ := os.ReadFile(f)
	if string(data) != "baz bar baz\n" {
		t.Errorf("expected 'baz bar baz\\n', got: %q", string(data))
	}
}

func TestFileEditToolMultipleMatchesNoReplaceAll(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "edit.txt")
	os.WriteFile(f, []byte("foo bar foo\n"), 0644)

	tracker := NewFileStateTracker()
	tracker.RecordRead(f, "foo bar foo\n", getFileMtime(f))

	tool := FileEditTool{Tracker: tracker}
	_, err := tool.Call(context.Background(), map[string]any{
		"file_path":  f,
		"old_string": "foo",
		"new_string": "baz",
	})
	if err == nil {
		t.Fatal("expected error for multiple matches without replace_all")
	}
	if !strings.Contains(err.Error(), "matches") {
		t.Errorf("expected multiple matches error, got: %v", err)
	}
}

func TestFileEditToolReadFirstRejection(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "edit.txt")
	os.WriteFile(f, []byte("hello world\n"), 0644)

	tracker := NewFileStateTracker()
	tool := FileEditTool{Tracker: tracker}
	_, err := tool.Call(context.Background(), map[string]any{
		"file_path":  f,
		"old_string": "world",
		"new_string": "universe",
	})
	if err == nil {
		t.Fatal("expected error for unread file")
	}
	if !strings.Contains(err.Error(), "not been read yet") {
		t.Errorf("expected read-first error, got: %v", err)
	}
}

func TestFileEditToolIdenticalStrings(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "edit.txt")
	os.WriteFile(f, []byte("hello\n"), 0644)

	tracker := NewFileStateTracker()
	tracker.RecordRead(f, "hello\n", getFileMtime(f))

	tool := FileEditTool{Tracker: tracker}
	_, err := tool.Call(context.Background(), map[string]any{
		"file_path":  f,
		"old_string": "hello",
		"new_string": "hello",
	})
	if err == nil {
		t.Fatal("expected error for identical strings")
	}
	if !strings.Contains(err.Error(), "identical") {
		t.Errorf("expected identical strings error, got: %v", err)
	}
}

// --- GlobTool tests ---

func hasRipgrep() bool {
	_, err1 := os.Stat("/usr/bin/rg")
	_, err2 := os.Stat("/usr/local/bin/rg")
	return err1 == nil || err2 == nil
}

func TestGlobToolBasic(t *testing.T) {
	if !hasRipgrep() {
		t.Skip("ripgrep not installed")
	}

	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, "a.go"), []byte("a"), 0644)
	os.WriteFile(filepath.Join(tmp, "b.go"), []byte("b"), 0644)
	os.WriteFile(filepath.Join(tmp, "c.txt"), []byte("c"), 0644)

	tool := GlobTool{}
	result, err := tool.Call(context.Background(), map[string]any{
		"pattern": "*.go",
		"path":    tmp,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "a.go") {
		t.Errorf("expected a.go in result, got: %s", result)
	}
	if !strings.Contains(result, "b.go") {
		t.Errorf("expected b.go in result, got: %s", result)
	}
	if strings.Contains(result, "c.txt") {
		t.Errorf("did not expect c.txt in result, got: %s", result)
	}
}

// --- GrepTool tests ---

func TestGrepToolBasic(t *testing.T) {
	// Skip if ripgrep is not installed
	if _, err := os.Stat("/usr/bin/rg"); err != nil {
		if _, err := os.Stat("/usr/local/bin/rg"); err != nil {
			t.Skip("ripgrep not installed")
		}
	}

	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, "a.go"), []byte("package main\nfunc main() {}\n"), 0644)
	os.WriteFile(filepath.Join(tmp, "b.txt"), []byte("hello world\n"), 0644)

	tool := GrepTool{}
	result, err := tool.Call(context.Background(), map[string]any{
		"pattern":     "func main",
		"path":        tmp,
		"output_mode": "content",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "func main") {
		t.Errorf("expected match in result, got: %s", result)
	}
}

func TestGrepToolFilesWithMatches(t *testing.T) {
	if _, err := os.Stat("/usr/bin/rg"); err != nil {
		if _, err := os.Stat("/usr/local/bin/rg"); err != nil {
			t.Skip("ripgrep not installed")
		}
	}

	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, "a.go"), []byte("package main\n"), 0644)
	os.WriteFile(filepath.Join(tmp, "b.txt"), []byte("hello\n"), 0644)

	tool := GrepTool{}
	result, err := tool.Call(context.Background(), map[string]any{
		"pattern":     "package",
		"path":        tmp,
		"output_mode": "files_with_matches",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "a.go") {
		t.Errorf("expected a.go in result, got: %s", result)
	}
}

// --- FileStateTracker tests ---

func TestFileStateTracker(t *testing.T) {
	tracker := NewFileStateTracker()

	tracker.RecordRead("/tmp/test", "content", getFileMtime("/tmp/test"))

	state, ok := tracker.GetState("/tmp/test")
	if !ok {
		t.Fatal("expected state to exist")
	}
	if state.Content != "content" {
		t.Errorf("expected content 'content', got: %s", state.Content)
	}

	tracker.Remove("/tmp/test")
	_, ok = tracker.GetState("/tmp/test")
	if ok {
		t.Error("expected state to be removed")
	}
}
