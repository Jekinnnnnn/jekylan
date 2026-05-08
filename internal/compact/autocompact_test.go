package compact

import (
	"context"
	"testing"

	"github.com/Jekinnnnnn/jekylan/internal/message"
)

func TestGetAutoCompactThreshold(t *testing.T) {
	model := "glm5.1"
	window := getContextWindowForModel(model)
	reserved := min(getMaxOutputTokensForModel(model), maxOutputTokensForSummary)
	effective := window - reserved
	want := effective - autoCompactBufferTokens

	got := getAutoCompactThreshold(model)
	if got != want {
		t.Errorf("getAutoCompactThreshold = %d, want %d", got, want)
	}
}

func TestCalculateTokenWarningState(t *testing.T) {
	model := "glm5.1"
	threshold := getAutoCompactThreshold(model)

	// Well below threshold
	state := CalculateTokenWarningState(0, model)
	if state.IsAboveAutoCompactThreshold {
		t.Error("expected not above auto-compact threshold at 0 tokens")
	}
	if state.IsAtBlockingLimit {
		t.Error("expected not at blocking limit at 0 tokens")
	}

	// At auto-compact threshold
	state = CalculateTokenWarningState(threshold, model)
	if !state.IsAboveAutoCompactThreshold {
		t.Error("expected above auto-compact threshold at threshold")
	}

	// At blocking limit
	blockingLimit := getEffectiveContextWindowSize(model) - manualCompactBufferTokens
	state = CalculateTokenWarningState(blockingLimit, model)
	if !state.IsAtBlockingLimit {
		t.Error("expected at blocking limit")
	}
	if !state.IsAboveWarningThreshold {
		t.Error("expected above warning threshold at blocking limit")
	}
	if !state.IsAboveErrorThreshold {
		t.Error("expected above error threshold at blocking limit")
	}
}

func TestShouldAutoCompact(t *testing.T) {
	model := "glm5.1"
	threshold := getAutoCompactThreshold(model)
	ctx := context.Background()

	// Tiny messages should not trigger.
	msgs := []message.Message{
		{Role: message.RoleUser, Content: []message.ContentBlock{message.TextBlock{Text: "hi"}}},
	}
	if ShouldAutoCompact(ctx, msgs, model, nil, "", 0) {
		t.Error("ShouldAutoCompact should be false for tiny messages")
	}

	// Recursion guards.
	if ShouldAutoCompact(ctx, msgs, model, nil, QuerySourceSessionMemory, 0) {
		t.Error("ShouldAutoCompact should be false for session_memory source")
	}
	if ShouldAutoCompact(ctx, msgs, model, nil, QuerySourceCompact, 0) {
		t.Error("ShouldAutoCompact should be false for compact source")
	}

	// Huge message should trigger.
	bigText := make([]byte, threshold*4) // enough chars to blow past threshold
	msgs = []message.Message{
		{Role: message.RoleUser, Content: []message.ContentBlock{message.TextBlock{Text: string(bigText)}}},
	}
	if !ShouldAutoCompact(ctx, msgs, model, nil, "", 0) {
		t.Error("ShouldAutoCompact should be true for huge messages")
	}

	// Snip tokens freed should offset.
	if ShouldAutoCompact(ctx, msgs, model, nil, "", threshold+1) {
		t.Error("ShouldAutoCompact should be false when snip frees enough tokens")
	}
}

func TestIsAutoCompactEnabled(t *testing.T) {
	SetOptions(Options{})
	if !isAutoCompactEnabled() {
		t.Error("expected enabled by default")
	}

	SetOptions(Options{DisableCompact: true})
	if isAutoCompactEnabled() {
		t.Error("expected disabled when DisableCompact=true")
	}

	SetOptions(Options{DisableAutoCompact: true})
	if isAutoCompactEnabled() {
		t.Error("expected disabled when DisableAutoCompact=true")
	}
}

func TestAutoCompactIfNeededCircuitBreaker(t *testing.T) {
	ctx := context.Background()
	tracking := &AutoCompactTrackingState{ConsecutiveFailures: maxConsecutiveFailures}
	wasCompacted, _, failures, err := AutoCompactIfNeeded(ctx, nil, "glm5.1", nil, "", tracking, 0)
	if wasCompacted {
		t.Error("expected no compaction when circuit breaker is tripped")
	}
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if failures != maxConsecutiveFailures {
		t.Errorf("expected failures=%d, got %d", maxConsecutiveFailures, failures)
	}
}

func TestAutoCompactIfNeededDisableCompact(t *testing.T) {
	ctx := context.Background()
	SetOptions(Options{DisableCompact: true})
	wasCompacted, _, failures, err := AutoCompactIfNeeded(ctx, nil, "glm5.1", nil, "", nil, 0)
	if wasCompacted {
		t.Error("expected no compaction when DisableCompact")
	}
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if failures != 0 {
		t.Errorf("expected failures=0, got %d", failures)
	}
}
