package tool

import (
	"os"
	"sync"
	"time"
)

// FileState records the state of a file at the time it was read.
type FileState struct {
	Content string
	Mtime   time.Time
}

// FileStateTracker tracks files that have been read, enabling read-first
// enforcement for write/edit tools and stale-write detection.
type FileStateTracker struct {
	mu     sync.RWMutex
	states map[string]FileState
}

// NewFileStateTracker creates a new tracker.
func NewFileStateTracker() *FileStateTracker {
	return &FileStateTracker{
		states: make(map[string]FileState),
	}
}

// RecordRead records that a file was read with the given content and mtime.
func (t *FileStateTracker) RecordRead(path string, content string, mtime time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.states == nil {
		t.states = make(map[string]FileState)
	}
	t.states[path] = FileState{Content: content, Mtime: mtime}
}

// GetState returns the recorded state for a file, if any.
func (t *FileStateTracker) GetState(path string) (FileState, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	state, ok := t.states[path]
	return state, ok
}

// WasReadSince reports whether the file was read and the recorded mtime
// exactly matches the current file's mtime.
func (t *FileStateTracker) WasReadSince(path string, currentMtime time.Time) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	state, ok := t.states[path]
	if !ok {
		return false
	}
	return state.Mtime.Equal(currentMtime)
}

// Remove removes a file's recorded state.
func (t *FileStateTracker) Remove(path string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.states, path)
}

// getFileMtime returns a file's modification time, or zero time on error.
func getFileMtime(path string) time.Time {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}
