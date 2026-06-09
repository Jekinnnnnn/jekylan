package compact

import "github.com/Jekinnnnnn/jekylan/internal/message"

// This file holds stub implementations for compaction strategies that are not
// yet implemented in the Go port. They are preserved so callers in other
// packages (query, autocompact) continue to compile.

// ---- Context Collapse stubs ----

// IsContextCollapseEnabled returns whether the context-collapse feature is active.
func IsContextCollapseEnabled() bool { return false }

// ---- Reactive Compact stubs ----

// IsReactiveCompactEnabled returns whether reactive compaction is enabled.
func IsReactiveCompactEnabled() bool { return false }

// ReactiveCompactResult is the outcome of a reactive compaction attempt.
type ReactiveCompactResult struct {
	OK     bool
	Reason string
	Result *Result
}

// ReactiveCompactOnPromptTooLong attempts reactive compaction after a prompt-too-long error.
func ReactiveCompactOnPromptTooLong(messages []message.Message, options map[string]any) ReactiveCompactResult {
	return ReactiveCompactResult{OK: false}
}

// TryReactiveCompact attempts a reactive compact.
func TryReactiveCompact(messages []message.Message) (*Result, error) { return nil, nil }

// ---- Snip stubs ----

// SnipResult is the outcome of a snip compaction attempt.
type SnipResult struct {
	Messages    []message.Message
	Executed    bool
	TokensFreed int
}

// IsSnipRuntimeEnabled returns whether snip compaction is enabled.
func IsSnipRuntimeEnabled() bool { return false }

// SnipCompactIfNeeded attempts snip compaction.
func SnipCompactIfNeeded(msgs []message.Message) SnipResult {
	return SnipResult{Messages: msgs, Executed: false, TokensFreed: 0}
}
