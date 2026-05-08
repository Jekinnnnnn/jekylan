package compact

import (
	"os"
	"strings"
	"sync"
)

var (
	lastSummarizedMessageID string
	lastSummarizedIDMu      sync.RWMutex
)

// GetLastSummarizedMessageId returns the UUID of the last message that was
// included in a compaction summary. Empty when no compaction has occurred yet.
func GetLastSummarizedMessageId() string {
	lastSummarizedIDMu.RLock()
	defer lastSummarizedIDMu.RUnlock()
	return lastSummarizedMessageID
}

// SetLastSummarizedMessageId sets the last summarized message ID.
func SetLastSummarizedMessageId(id string) {
	lastSummarizedIDMu.Lock()
	defer lastSummarizedIDMu.Unlock()
	lastSummarizedMessageID = id
}

// GetSessionMemoryPath returns the filesystem path for the session memory file.
func GetSessionMemoryPath() string {
	if path := GetOptions().SessionMemoryPath; path != "" {
		return path
	}
	return ".session_memory"
}

// GetSessionMemoryContent reads the session memory from disk.
// Returns an empty string when the file does not exist or cannot be read.
func GetSessionMemoryContent() string {
	path := GetSessionMemoryPath()
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}

// IsSessionMemoryEmpty returns true when the session memory content is blank
// or matches the default template (no actual content extracted).
func IsSessionMemoryEmpty(content string) bool {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return true
	}
	// Detect the default template marker.
	if strings.Contains(trimmed, "Session Memory Template") {
		return true
	}
	return false
}

// TruncateSessionMemoryForCompact truncates oversized session memory sections
// to prevent them from consuming the entire post-compact token budget.
// Returns the (possibly) truncated content and a flag indicating truncation.
func TruncateSessionMemoryForCompact(content string) (truncated string, wasTruncated bool) {
	const maxChars = 160_000 // ~40K tokens at 4 chars/token
	if len(content) <= maxChars {
		return content, false
	}
	marker := "\n\n[Some session memory sections were truncated for compaction]"
	return content[:maxChars-len(marker)] + marker, true
}

// WaitForSessionMemoryExtraction waits for any in-progress session memory
// extraction to complete. In the MVP there is no async extraction, so it
// returns immediately.
func WaitForSessionMemoryExtraction() {}
