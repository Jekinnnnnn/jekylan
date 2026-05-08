package compact

import (
	"github.com/Jekinnnnnn/jekylan/internal/message"
)

// TODO
// SnipResult is the outcome of a snip compaction attempt.
type SnipResult struct {
	Messages    []message.Message
	Executed    bool
	TokensFreed int
}

// IsSnipRuntimeEnabled returns whether snip compaction is enabled.
// Mirrors the TS stub which always returns false.
func IsSnipRuntimeEnabled() bool {
	return false
}

// SnipCompactIfNeeded attempts snip compaction. TS stub returns input unchanged.
func SnipCompactIfNeeded(msgs []message.Message) SnipResult {
	return SnipResult{Messages: msgs, Executed: false, TokensFreed: 0}
}
