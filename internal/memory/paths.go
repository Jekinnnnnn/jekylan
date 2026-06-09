package memory

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// MemoryBaseDir returns the base directory for persistent memory storage.
// Resolution order:
//  1. JEKYLAN_MEMORY_DIR env var (explicit override)
//  2. ~/.harness (legacy, used if it exists for backward compatibility)
//  3. ~/.jekylan (default config home)
func MemoryBaseDir() string {
	if dir := os.Getenv("JEKYLAN_MEMORY_DIR"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	legacy := filepath.Join(home, ".harness")
	if _, err := os.Stat(legacy); err == nil {
		return legacy
	}
	return filepath.Join(home, ".jekylan")
}

// GetMemoryDir returns the auto-memory directory path for the current project.
// If a custom memoryDir is provided (e.g. from config), it is used directly.
// Otherwise resolves to <memoryBase>/projects/<sanitized-cwd>/memory/.
func GetMemoryDir(customDir string) string {
	if customDir != "" {
		return customDir
	}

	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}

	projectsDir := filepath.Join(MemoryBaseDir(), "projects")
	return filepath.Join(projectsDir, sanitizePath(cwd), "memory")
}

// IsMemoryPath checks if an absolute path is within the given memory directory.
func IsMemoryPath(memoryDir, absolutePath string) bool {
	abs, err := filepath.Abs(absolutePath)
	if err != nil {
		return false
	}
	memAbs, err := filepath.Abs(memoryDir)
	if err != nil {
		return false
	}
	return strings.HasPrefix(abs, memAbs)
}

// EnsureMemoryDir ensures a memory directory exists. Idempotent.
func EnsureMemoryDir(memoryDir string) error {
	return os.MkdirAll(memoryDir, 0755)
}

// GetMemoryEntrypoint returns the path to MEMORY.md inside the memory directory.
func GetMemoryEntrypoint(memoryDir string) string {
	return filepath.Join(memoryDir, entrypointName)
}

// GetSessionMemoryPath returns the default session memory file path.
// When memoryDir is non-empty, the session memory is stored inside the
// memory so that all persistence lives in one place.
func GetSessionMemoryPath(memoryDir string) string {
	if memoryDir == "" {
		return ".session_memory"
	}
	return filepath.Join(memoryDir, "session_memory.md")
}

// GetSessionPath returns the path for the session persistence file.
// When memoryDir is non-empty, the session is stored inside the memory.
func GetSessionPath(memoryDir string) string {
	if memoryDir == "" {
		return "session.json"
	}
	return filepath.Join(memoryDir, "session.json")
}

// sanitizePath produces a filesystem-safe directory name from an arbitrary path.
func sanitizePath(p string) string {
	// Replace separators and other unsafe characters
	s := strings.ReplaceAll(p, string(filepath.Separator), "_")
	if runtime.GOOS == "windows" {
		s = strings.ReplaceAll(s, ":", "_")
	}
	// Trim leading dots and trailing spaces/dots
	s = strings.Trim(s, " .")
	if s == "" {
		return "default"
	}
	return s
}
