package compact

import (
	"time"

	"github.com/Jekinnnnnn/jekylan/internal/message"
)

const timeBasedMCClearedMessage = "[Old tool result content cleared]"

// compactableTools lists tool names whose results are eligible for micro-compaction.
var compactableTools = map[string]struct{}{
	"bash":      {},
	"file_read": {},
}

// TimeBasedConfig holds the time-based microcompact settings.
type TimeBasedConfig struct {
	Enabled             bool
	GapThresholdMinutes float64
	KeepRecent          int
}

func getTimeBasedMCConfig() TimeBasedConfig {
	opts := GetOptions()
	gap := opts.TimeBasedMCGap
	if gap == 0 {
		gap = defaultOptions.TimeBasedMCGap
	}
	keep := opts.TimeBasedMCKeep
	if keep == 0 {
		keep = defaultOptions.TimeBasedMCKeep
	}
	return TimeBasedConfig{Enabled: !opts.DisableTimeBasedMC, GapThresholdMinutes: gap, KeepRecent: keep}
}

// MicrocompactResult is the outcome of micro-compaction.
type MicrocompactResult struct {
	Messages    []message.Message
	Changed     bool
	TokensSaved int
}

// MicrocompactMessages runs the full microcompact pipeline.
// It first tries the time-based trigger; if that fires, old compactable tool
// results are content-cleared.
func MicrocompactMessages(msgs []message.Message) MicrocompactResult {
	if res := MaybeTimeBasedMicrocompact(msgs, time.Now()); res != nil {
		return *res
	}
	return MicrocompactResult{Messages: msgs}
}

// MaybeTimeBasedMicrocompact evaluates the time gap since the last assistant
// message. When the gap exceeds the configured threshold, it clears all but the
// most recent N compactable tool results.
//
// Returns nil when the trigger does not fire.
func MaybeTimeBasedMicrocompact(msgs []message.Message, now time.Time) *MicrocompactResult {
	config := getTimeBasedMCConfig()
	if !config.Enabled {
		return nil
	}

	lastAssistant := findLastAssistant(msgs)
	if lastAssistant == nil {
		return nil
	}

	if lastAssistant.Timestamp.IsZero() {
		return nil
	}

	gapMinutes := now.Sub(lastAssistant.Timestamp).Minutes()
	if gapMinutes < config.GapThresholdMinutes {
		return nil
	}

	return applyTimeBasedMicrocompact(msgs, config.KeepRecent)
}

// ForceTimeBasedMicrocompact unconditionally clears old compactable tool results,
// keeping the most recent N results. This is exposed for testing and manual /compact.
func ForceTimeBasedMicrocompact(msgs []message.Message, keepRecent int) MicrocompactResult {
	res := applyTimeBasedMicrocompact(msgs, keepRecent)
	if res == nil {
		return MicrocompactResult{Messages: msgs}
	}
	return *res
}

// applyTimeBasedMicrocompact is the shared implementation for both the triggered
// and forced paths. It returns nil when there is nothing to clear.
func applyTimeBasedMicrocompact(msgs []message.Message, keepRecent int) *MicrocompactResult {
	ids := collectCompactableToolIDs(msgs)
	if len(ids) == 0 {
		return nil
	}

	keep := max(1, keepRecent)
	if keep >= len(ids) {
		return nil
	}

	keepSet := make(map[string]struct{}, keep)
	for _, id := range ids[len(ids)-keep:] {
		keepSet[id] = struct{}{}
	}

	changed := false
	tokensSaved := 0
	result := make([]message.Message, len(msgs))
	for i, m := range msgs {
		if m.Role != message.RoleUser || len(m.Content) == 0 {
			result[i] = m
			continue
		}
		newContent := make([]message.ContentBlock, len(m.Content))
		copy(newContent, m.Content)
		msgChanged := false
		for j, block := range newContent {
			if tr, ok := block.(message.ToolResultBlock); ok {
				if _, exists := keepSet[tr.ToolUseID]; !exists && tr.Content != timeBasedMCClearedMessage {
					tokensSaved += RoughTokenCountForBlock(block)
					tr.Content = timeBasedMCClearedMessage
					newContent[j] = tr
					msgChanged = true
				}
			}
		}
		if msgChanged {
			changed = true
			result[i] = message.Message{Role: m.Role, Content: newContent, Timestamp: m.Timestamp}
		} else {
			result[i] = m
		}
	}

	if !changed {
		return nil
	}
	return &MicrocompactResult{Messages: result, Changed: true, TokensSaved: tokensSaved}
}

func collectCompactableToolIDs(msgs []message.Message) []string {
	var ids []string
	for _, m := range msgs {
		if m.Role != message.RoleAssistant {
			continue
		}
		for _, block := range m.Content {
			if tu, ok := block.(message.ToolUseBlock); ok {
				if _, ok := compactableTools[tu.Name]; ok {
					ids = append(ids, tu.ID)
				}
			}
		}
	}
	return ids
}

func findLastAssistant(msgs []message.Message) *message.Message {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == message.RoleAssistant {
			return &msgs[i]
		}
	}
	return nil
}

// TimeBasedMCClearedMessage returns the constant placeholder string.
func TimeBasedMCClearedMessage() string {
	return timeBasedMCClearedMessage
}
