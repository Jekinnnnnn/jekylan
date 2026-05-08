package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGetMemoryDirWithCustomDir(t *testing.T) {
	custom := "/custom/mem/dir"
	dir := GetMemoryDir(custom)
	if !strings.HasPrefix(dir, custom) {
		t.Errorf("expected dir to start with %s, got %s", custom, dir)
	}
}

func TestGetMemoryDirWithoutCustom(t *testing.T) {
	dir := GetMemoryDir("")
	if !strings.Contains(dir, "memory") {
		t.Error("expected 'memory' in default path")
	}
}

func TestEnsureMemoryDir(t *testing.T) {
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "nested", "memory")
	if err := EnsureMemoryDir(subDir); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(subDir)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		t.Error("expected directory")
	}
}

func TestGetMemoryEntrypoint(t *testing.T) {
	dir := "/tmp/mem"
	ep := GetMemoryEntrypoint(dir)
	if !strings.HasSuffix(ep, "MEMORY.md") {
		t.Error("expected MEMORY.md suffix")
	}
}
