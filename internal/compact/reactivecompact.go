package compact

import (
	"github.com/Jekinnnnnn/jekylan/internal/message"
)

// TODO
// ReactiveCompactResult is the outcome of a reactive compaction attempt.
type ReactiveCompactResult struct {
	OK     bool
	Reason string
	Result *Result
}

// IsReactiveCompactEnabled returns whether reactive compaction is enabled.
// Mirrors the TS stub which always returns false.
func IsReactiveCompactEnabled() bool {
	return false
}

// ReactiveCompactOnPromptTooLong attempts reactive compaction after a prompt-too-long error.
// TS stub always returns ok=false.
func ReactiveCompactOnPromptTooLong(messages []message.Message, options map[string]any) ReactiveCompactResult {
	return ReactiveCompactResult{OK: false}
}

// TryReactiveCompact attempts a reactive compact. TS stub always returns nil.
func TryReactiveCompact(messages []message.Message) (*Result, error) {
	return nil, nil
}
