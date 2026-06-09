package compact

import (
	"strings"
	"sync"
	"time"

	"github.com/Jekinnnnnn/jekylan/internal/message"
	"github.com/Jekinnnnnn/jekylan/internal/tokens"
)

// SessionMemoryCompactConfig holds thresholds for session-memory compaction.
type SessionMemoryCompactConfig struct {
	MinTokens            int
	MinTextBlockMessages int
	MaxTokens            int
}

// Default session-memory compact configuration.
var defaultSMCompactConfig = SessionMemoryCompactConfig{
	MinTokens:            10_000,
	MinTextBlockMessages: 5,
	MaxTokens:            40_000,
}

// smCompactConfig is the active session-memory compact configuration.
// It is kept in sync with global options by syncSMCompactConfigFromOptions.
var (
	smCompactConfig   = defaultSMCompactConfig
	smCompactConfigMu sync.RWMutex
)

// SetSessionMemoryCompactConfig updates the session-memory compact configuration.
func SetSessionMemoryCompactConfig(cfg SessionMemoryCompactConfig) {
	smCompactConfigMu.Lock()
	defer smCompactConfigMu.Unlock()
	smCompactConfig = cfg
}

// GetSessionMemoryCompactConfig returns a copy of the current configuration.
func GetSessionMemoryCompactConfig() SessionMemoryCompactConfig {
	smCompactConfigMu.RLock()
	defer smCompactConfigMu.RUnlock()
	return smCompactConfig
}

// ResetSessionMemoryCompactConfig resets config to defaults.
func ResetSessionMemoryCompactConfig() {
	smCompactConfigMu.Lock()
	defer smCompactConfigMu.Unlock()
	smCompactConfig = defaultSMCompactConfig
}

// hasTextBlocks returns true when the message contains visible text content.
func hasTextBlocks(msg message.Message) bool {
	for _, block := range msg.Content {
		if _, ok := block.(message.TextBlock); ok {
			return true
		}
	}
	return false
}

// getToolResultIds extracts tool_use IDs from tool_result blocks in a message.
func getToolResultIds(msg message.Message) []string {
	if msg.Role != message.RoleUser {
		return nil
	}
	var ids []string
	for _, block := range msg.Content {
		if tr, ok := block.(message.ToolResultBlock); ok {
			ids = append(ids, tr.ToolUseID)
		}
	}
	return ids
}

// hasToolUseWithIds returns true when the assistant message contains a tool_use
// whose ID is in the provided set.
func hasToolUseWithIds(msg message.Message, ids map[string]struct{}) bool {
	if msg.Role != message.RoleAssistant {
		return false
	}
	for _, block := range msg.Content {
		if tu, ok := block.(message.ToolUseBlock); ok {
			if _, exists := ids[tu.ID]; exists {
				return true
			}
		}
	}
	return false
}

// adjustIndexToPreserveAPIInvariants walks backwards from startIndex to ensure
// tool_use/tool_result pairs and split assistant records (same ResponseID) are
// not broken across the compaction boundary.
func adjustIndexToPreserveAPIInvariants(msgs []message.Message, startIndex int) int {
	if startIndex <= 0 || startIndex >= len(msgs) {
		return startIndex
	}

	adjusted := startIndex

	// Step 1: Handle tool_use/tool_result pairs.
	allToolResultIds := make(map[string]struct{})
	for i := startIndex; i < len(msgs); i++ {
		for _, id := range getToolResultIds(msgs[i]) {
			allToolResultIds[id] = struct{}{}
		}
	}

	if len(allToolResultIds) > 0 {
		toolUseIdsInKeptRange := make(map[string]struct{})
		for i := adjusted; i < len(msgs); i++ {
			if msgs[i].Role == message.RoleAssistant {
				for _, block := range msgs[i].Content {
					if tu, ok := block.(message.ToolUseBlock); ok {
						toolUseIdsInKeptRange[tu.ID] = struct{}{}
					}
				}
			}
		}

		neededToolUseIds := make(map[string]struct{})
		for id := range allToolResultIds {
			if _, exists := toolUseIdsInKeptRange[id]; !exists {
				neededToolUseIds[id] = struct{}{}
			}
		}

		for i := adjusted - 1; i >= 0 && len(neededToolUseIds) > 0; i-- {
			if hasToolUseWithIds(msgs[i], neededToolUseIds) {
				adjusted = i
				if msgs[i].Role == message.RoleAssistant {
					for _, block := range msgs[i].Content {
						if tu, ok := block.(message.ToolUseBlock); ok {
							delete(neededToolUseIds, tu.ID)
						}
					}
				}
			}
		}
	}

	// Step 2: Handle split assistant records sharing the same ResponseID.
	messageIdsInKeptRange := make(map[string]struct{})
	for i := adjusted; i < len(msgs); i++ {
		if msgs[i].Role == message.RoleAssistant && msgs[i].ResponseID != "" {
			messageIdsInKeptRange[msgs[i].ResponseID] = struct{}{}
		}
	}

	for i := adjusted - 1; i >= 0; i-- {
		if msgs[i].Role == message.RoleAssistant && msgs[i].ResponseID != "" {
			if _, exists := messageIdsInKeptRange[msgs[i].ResponseID]; exists {
				adjusted = i
			}
		}
	}

	return adjusted
}

// estimateSingleMessageTokens returns a rough token count for a single message
// without allocating a new slice.
func estimateSingleMessageTokens(msg message.Message) int {
	total := 0
	for _, block := range msg.Content {
		total += tokens.RoughTokenCountForBlock(block)
	}
	return total
}

// calculateMessagesToKeepIndex computes the first index of messages that should
// be preserved after compaction. It starts from lastSummarizedIndex+1 and
// expands backwards until minTokens, minTextBlockMessages, or maxTokens is hit.
func calculateMessagesToKeepIndex(msgs []message.Message, lastSummarizedIndex int) int {
	if len(msgs) == 0 {
		return 0
	}

	cfg := GetSessionMemoryCompactConfig()

	startIndex := lastSummarizedIndex + 1
	if lastSummarizedIndex < 0 {
		startIndex = len(msgs)
	}

	totalTokens := 0
	textBlockMessageCount := 0
	for i := startIndex; i < len(msgs); i++ {
		totalTokens += estimateSingleMessageTokens(msgs[i])
		if hasTextBlocks(msgs[i]) {
			textBlockMessageCount++
		}
	}

	if totalTokens >= cfg.MaxTokens {
		return adjustIndexToPreserveAPIInvariants(msgs, startIndex)
	}
	if totalTokens >= cfg.MinTokens && textBlockMessageCount >= cfg.MinTextBlockMessages {
		return adjustIndexToPreserveAPIInvariants(msgs, startIndex)
	}

	// Expand backwards. Floor at 0 (no compact boundary message type in Go MVP).
	for i := startIndex - 1; i >= 0; i-- {
		totalTokens += estimateSingleMessageTokens(msgs[i])
		if hasTextBlocks(msgs[i]) {
			textBlockMessageCount++
		}
		startIndex = i

		if totalTokens >= cfg.MaxTokens {
			break
		}
		if totalTokens >= cfg.MinTokens && textBlockMessageCount >= cfg.MinTextBlockMessages {
			break
		}
	}

	return adjustIndexToPreserveAPIInvariants(msgs, startIndex)
}

// shouldUseSessionMemoryCompaction checks whether the session-memory compaction
// experiment is enabled via global options.
func shouldUseSessionMemoryCompaction() bool {
	opts := GetOptions()
	if opts.EnableSMCompact {
		return true
	}
	return false
}

// isCompactBoundaryMessage returns true for system messages that serve as compact
// boundary markers.
func isCompactBoundaryMessage(msg message.Message) bool {
	if msg.Role != message.RoleSystem {
		return false
	}
	for _, block := range msg.Content {
		if tb, ok := block.(message.TextBlock); ok {
			if strings.Contains(tb.Text, "[Compact boundary") {
				return true
			}
		}
	}
	return false
}

// createCompactionResultFromSessionMemory builds a Result from session memory content.
func createCompactionResultFromSessionMemory(
	msgs []message.Message,
	sessionMemory string,
	messagesToKeep []message.Message,
	transcriptPath string,
) *Result {
	_ = tokens.TokenCountFromLastAPIResponse(msgs) // reserved for telemetry

	boundary := message.Message{Role: message.RoleSystem, Timestamp: time.Now()}
	boundary.AddText("[Compact boundary: earlier conversation summarized below]")

	truncatedContent, wasTruncated := TruncateSessionMemoryForCompact(sessionMemory)
	summaryContent := GetCompactUserSummaryMessage(truncatedContent, true, transcriptPath, true)
	if wasTruncated {
		memoryPath := GetSessionMemoryPath()
		summaryContent += "\n\nSome session memory sections were truncated for length. The full session memory can be viewed at: " + memoryPath
	}

	summaryMsg := message.Message{Role: message.RoleUser, Timestamp: time.Now()}
	summaryMsg.AddText(summaryContent)

	resultMsgs := []message.Message{boundary, summaryMsg}
	resultMsgs = append(resultMsgs, messagesToKeep...)

	return &Result{Messages: resultMsgs}
}

// TrySessionMemoryCompaction attempts to use session memory instead of legacy
// compaction. Returns nil when session memory is unavailable or the result would
// still exceed the auto-compact threshold.
func TrySessionMemoryCompaction(
	msgs []message.Message,
	autoCompactThreshold int,
) *Result {
	if !shouldUseSessionMemoryCompaction() {
		return nil
	}

	WaitForSessionMemoryExtraction()

	lastSummarizedID := GetLastSummarizedMessageId()
	sessionMemory := GetSessionMemoryContent()

	if sessionMemory == "" {
		return nil
	}
	if IsSessionMemoryEmpty(sessionMemory) {
		return nil
	}

	var lastSummarizedIndex int
	if lastSummarizedID != "" {
		found := false
		for i, msg := range msgs {
			// Use ResponseID as the closest proxy to msg.uuid in the TS source.
			if msg.ResponseID == lastSummarizedID {
				lastSummarizedIndex = i
				found = true
				break
			}
		}
		if !found {
			return nil
		}
	} else {
		lastSummarizedIndex = len(msgs) - 1
	}

	startIndex := calculateMessagesToKeepIndex(msgs, lastSummarizedIndex)

	var messagesToKeep []message.Message
	for i := startIndex; i < len(msgs); i++ {
		if !isCompactBoundaryMessage(msgs[i]) {
			messagesToKeep = append(messagesToKeep, msgs[i])
		}
	}

	result := createCompactionResultFromSessionMemory(msgs, sessionMemory, messagesToKeep, "")
	result.PostCompactTokens = tokens.EstimateMessageTokens(result.Messages)

	if autoCompactThreshold > 0 && result.PostCompactTokens >= autoCompactThreshold {
		return nil
	}

	return result
}
