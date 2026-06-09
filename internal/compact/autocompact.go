package compact

import (
	"context"
	"fmt"
	"math"
	"os"

	"github.com/Jekinnnnnn/jekylan/internal/llm"
	"github.com/Jekinnnnnn/jekylan/internal/message"
	"github.com/Jekinnnnnn/jekylan/internal/tokens"
)

const (
	autoCompactBufferTokens      = 13000
	warningThresholdBufferTokens = 20000
	errorThresholdBufferTokens   = 20000
	manualCompactBufferTokens    = 3000
	maxOutputTokensForSummary    = 20000
	maxConsecutiveFailures       = 3
)

// QuerySource identifies where a query originates.
type QuerySource string

const (
	QuerySourceSessionMemory QuerySource = "session_memory"
	QuerySourceCompact       QuerySource = "compact"
	QuerySourceMarbleOrigami QuerySource = "marble_origami"
)

// AutoCompactTrackingState tracks compaction status across turns.
type AutoCompactTrackingState struct {
	Compacted           bool
	TurnCounter         int
	TurnID              string
	ConsecutiveFailures int
}

// RecompactionInfo carries diagnostic context about prior compactions.
type RecompactionInfo struct {
	IsRecompactionInChain     bool
	TurnsSincePreviousCompact int
	PreviousCompactTurnID     string
	AutoCompactThreshold      int
	QuerySource               QuerySource
}

// getContextWindowForModel returns a hard-coded context window for known models.
func getContextWindowForModel(model string) int {
	switch model {
	default:
		// Fallback for OpenAI and unknown models.
		return 128_000
	}
}

// getMaxOutputTokensForModel returns a hard-coded max output token limit.
func getMaxOutputTokensForModel(model string) int {
	switch model {
	default:
		return 4096
	}
}

// getEffectiveContextWindowSize returns the context window minus reserved summary tokens.
func getEffectiveContextWindowSize(model string) int {
	reserved := min(getMaxOutputTokensForModel(model), maxOutputTokensForSummary)
	window := getContextWindowForModel(model)

	if opts := GetOptions(); opts.AutoCompactWindow > 0 {
		window = min(window, opts.AutoCompactWindow)
	}

	return window - reserved
}

// getAutoCompactThreshold returns the token threshold at which auto-compaction should trigger.
func getAutoCompactThreshold(model string) int {
	if opts := GetOptions(); opts.AutoCompactThresholdOverride > 0 {
		return opts.AutoCompactThresholdOverride
	}

	effective := getEffectiveContextWindowSize(model)
	threshold := effective - autoCompactBufferTokens
	if threshold < 0 {
		threshold = 0
	}

	if opts := GetOptions(); opts.AutoCompactPctOverride > 0 && opts.AutoCompactPctOverride <= 100 {
		pctThreshold := int(float64(effective) * (opts.AutoCompactPctOverride / 100.0))
		return min(pctThreshold, threshold)
	}

	return threshold
}

// TokenWarningState holds the results of a token-warning calculation.
type TokenWarningState struct {
	PercentLeft                 int
	IsAboveWarningThreshold     bool
	IsAboveErrorThreshold       bool
	IsAboveAutoCompactThreshold bool
	IsAtBlockingLimit           bool
}

// CalculateTokenWarningState computes warning flags from current token usage.
func CalculateTokenWarningState(tokenUsage int, model string) TokenWarningState {
	autoCompactThreshold := getAutoCompactThreshold(model)
	threshold := autoCompactThreshold
	if !isAutoCompactEnabled() {
		threshold = getEffectiveContextWindowSize(model)
	}

	percentLeft := 0
	if threshold > 0 {
		percentLeft = max(0, int(math.Round(float64(threshold-tokenUsage)/float64(threshold)*100)))
	}

	warningThreshold := threshold - warningThresholdBufferTokens
	errorThreshold := threshold - errorThresholdBufferTokens

	isAboveWarningThreshold := tokenUsage >= warningThreshold
	isAboveErrorThreshold := tokenUsage >= errorThreshold
	isAboveAutoCompactThreshold := isAutoCompactEnabled() && tokenUsage >= autoCompactThreshold

	actualWindow := getEffectiveContextWindowSize(model)
	defaultBlockingLimit := actualWindow - manualCompactBufferTokens

	blockingLimit := defaultBlockingLimit
	if opts := GetOptions(); opts.BlockingLimitOverride > 0 {
		blockingLimit = opts.BlockingLimitOverride
	}

	isAtBlockingLimit := tokenUsage >= blockingLimit

	return TokenWarningState{
		PercentLeft:                 percentLeft,
		IsAboveWarningThreshold:     isAboveWarningThreshold,
		IsAboveErrorThreshold:       isAboveErrorThreshold,
		IsAboveAutoCompactThreshold: isAboveAutoCompactThreshold,
		IsAtBlockingLimit:           isAtBlockingLimit,
	}
}

// isAutoCompactEnabled checks global config.
func isAutoCompactEnabled() bool {
	opts := GetOptions()
	if opts.DisableCompact || opts.DisableAutoCompact {
		return false
	}
	return true
}

// ShouldAutoCompact returns true when messages exceed the auto-compact threshold.
// It respects query-source guards, feature gates, and snip savings.
func ShouldAutoCompact(ctx context.Context, msgs []message.Message, model string, client llm.Client, querySource QuerySource, snipTokensFreed int) bool {
	// Recursion guards. session_memory and compact are forked agents that
	// would deadlock.
	if querySource == QuerySourceSessionMemory || querySource == QuerySourceCompact {
		return false
	}
	// marble_origami is the ctx-agent — if ITS context blows up and
	// autocompact fires, runPostCompactCleanup calls resetContextCollapse()
	// which destroys the MAIN thread's committed log.
	if IsContextCollapseEnabled() {
		if querySource == QuerySourceMarbleOrigami {
			return false
		}
	}

	if !isAutoCompactEnabled() {
		return false
	}

	// Reactive-only mode: suppress proactive autocompact.
	if IsReactiveCompactEnabled() {
		return false
	}

	// Context-collapse mode: same suppression. Collapse IS the context
	// management system when it's on.
	if IsContextCollapseEnabled() {
		return false
	}

	tokenCount := tokens.TokenCountWithEstimation(msgs) - snipTokensFreed

	state := CalculateTokenWarningState(tokenCount, model)
	if state.IsAboveAutoCompactThreshold {
		fmt.Fprintf(os.Stderr, "[jekylan-debug] auto-compact triggered: tokens=%d threshold=%d\n", tokenCount, getAutoCompactThreshold(model))
	}
	return state.IsAboveAutoCompactThreshold
}

// AutoCompactIfNeeded runs compaction when the token threshold is exceeded.
// It returns the compaction outcome, the updated failure counter, and any error.
func AutoCompactIfNeeded(
	ctx context.Context,
	msgs []message.Message,
	model string,
	client llm.Client,
	querySource QuerySource,
	tracking *AutoCompactTrackingState,
	snipTokensFreed int,
) (wasCompacted bool, result *Result, consecutiveFailures int, err error) {
	if GetOptions().DisableCompact {
		return false, nil, 0, nil
	}

	// Circuit breaker: stop retrying after N consecutive failures.
	if tracking != nil && tracking.ConsecutiveFailures >= maxConsecutiveFailures {
		return false, nil, tracking.ConsecutiveFailures, nil
	}

	shouldCompact := ShouldAutoCompact(ctx, msgs, model, client, querySource, snipTokensFreed)
	if !shouldCompact {
		return false, nil, 0, nil
	}

	recompactionInfo := RecompactionInfo{
		IsRecompactionInChain:     tracking != nil && tracking.Compacted,
		TurnsSincePreviousCompact: -1,
		PreviousCompactTurnID:     "",
		AutoCompactThreshold:      getAutoCompactThreshold(model),
		QuerySource:               querySource,
	}
	if tracking != nil {
		recompactionInfo.TurnsSincePreviousCompact = tracking.TurnCounter
		recompactionInfo.PreviousCompactTurnID = tracking.TurnID
	}

	_ = recompactionInfo // Used for telemetry in future; preserved for structural matching.

	// EXPERIMENT: Try session memory compaction first.
	if sessionResult := TrySessionMemoryCompaction(msgs, recompactionInfo.AutoCompactThreshold); sessionResult != nil {
		fmt.Fprintf(os.Stderr, "[jekylan-debug] auto-compact used session memory: %d -> %d messages\n", len(msgs), len(sessionResult.Messages))
		return true, sessionResult, 0, nil
	}

	// Fall back to legacy compaction.
	fmt.Fprintf(os.Stderr, "[jekylan-debug] auto-compact using legacy compaction\n")
	compactResult, compactErr := CompactConversation(ctx, client, model, msgs)
	if compactErr != nil {
		fmt.Fprintf(os.Stderr, "[jekylan-debug] auto-compact failed: %v\n", compactErr)
		prevFailures := 0
		if tracking != nil {
			prevFailures = tracking.ConsecutiveFailures
		}
		nextFailures := prevFailures + 1
		return false, nil, nextFailures, fmt.Errorf("autocompact failed: %w", compactErr)
	}

	fmt.Fprintf(os.Stderr, "[jekylan-debug] auto-compact done: %d -> %d messages\n", len(msgs), len(compactResult.Messages))
	return true, compactResult, 0, nil
}
