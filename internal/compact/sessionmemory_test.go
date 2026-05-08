package compact

import (
	"os"
	"testing"

	"github.com/Jekinnnnnn/jekylan/internal/message"
)

func TestGetLastSummarizedMessageId(t *testing.T) {
	SetLastSummarizedMessageId("msg-1")
	if got := GetLastSummarizedMessageId(); got != "msg-1" {
		t.Errorf("expected msg-1, got %s", got)
	}
	SetLastSummarizedMessageId("")
	if got := GetLastSummarizedMessageId(); got != "" {
		t.Errorf("expected empty, got %s", got)
	}
}

func TestIsSessionMemoryEmpty(t *testing.T) {
	if !IsSessionMemoryEmpty("") {
		t.Error("expected empty for blank string")
	}
	if !IsSessionMemoryEmpty("   ") {
		t.Error("expected empty for whitespace")
	}
	if !IsSessionMemoryEmpty("Session Memory Template") {
		t.Error("expected empty for template marker")
	}
	if IsSessionMemoryEmpty("real content here") {
		t.Error("expected non-empty for real content")
	}
}

func TestTruncateSessionMemoryForCompact(t *testing.T) {
	short := "short content"
	out, wasTruncated := TruncateSessionMemoryForCompact(short)
	if out != short || wasTruncated {
		t.Error("expected no truncation for short content")
	}

	long := make([]byte, 200_000)
	for i := range long {
		long[i] = 'a'
	}
	out, wasTruncated = TruncateSessionMemoryForCompact(string(long))
	if !wasTruncated {
		t.Error("expected truncation for long content")
	}
	if len(out) >= len(long) {
		t.Error("expected output shorter than input")
	}
}

func TestHasTextBlocks(t *testing.T) {
	msgWithText := message.Message{
		Role:    message.RoleUser,
		Content: []message.ContentBlock{message.TextBlock{Text: "hello"}},
	}
	if !hasTextBlocks(msgWithText) {
		t.Error("expected true for text block")
	}

	msgNoText := message.Message{
		Role:    message.RoleUser,
		Content: []message.ContentBlock{message.ToolResultBlock{ToolUseID: "t1", Content: "result"}},
	}
	if hasTextBlocks(msgNoText) {
		t.Error("expected false for non-text block")
	}
}

func TestGetToolResultIds(t *testing.T) {
	msg := message.Message{
		Role: message.RoleUser,
		Content: []message.ContentBlock{
			message.ToolResultBlock{ToolUseID: "t1", Content: "a"},
			message.TextBlock{Text: "hi"},
			message.ToolResultBlock{ToolUseID: "t2", Content: "b"},
		},
	}
	ids := getToolResultIds(msg)
	if len(ids) != 2 || ids[0] != "t1" || ids[1] != "t2" {
		t.Errorf("expected [t1 t2], got %v", ids)
	}
}

func TestHasToolUseWithIds(t *testing.T) {
	msg := message.Message{
		Role: message.RoleAssistant,
		Content: []message.ContentBlock{
			message.ToolUseBlock{ID: "t1", Name: "bash"},
		},
	}
	if !hasToolUseWithIds(msg, map[string]struct{}{"t1": {}}) {
		t.Error("expected true when ID matches")
	}
	if hasToolUseWithIds(msg, map[string]struct{}{"t2": {}}) {
		t.Error("expected false when ID does not match")
	}
}

func TestAdjustIndexToPreserveAPIInvariants(t *testing.T) {
	msgs := []message.Message{
		{Role: message.RoleAssistant, Content: []message.ContentBlock{message.ToolUseBlock{ID: "t1", Name: "bash"}}},
		{Role: message.RoleUser, Content: []message.ContentBlock{message.ToolResultBlock{ToolUseID: "t1", Content: "result"}}},
	}
	got := adjustIndexToPreserveAPIInvariants(msgs, 1)
	if got != 0 {
		t.Errorf("expected adjusted index 0, got %d", got)
	}

	got2 := adjustIndexToPreserveAPIInvariants(msgs, 0)
	if got2 != 0 {
		t.Errorf("expected 0, got %d", got2)
	}
}

func TestAdjustIndexForSplitRecords(t *testing.T) {
	msgs := []message.Message{
		{Role: message.RoleAssistant, ResponseID: "resp-1", Content: []message.ContentBlock{message.ThinkingBlock{Thinking: "think"}}},
		{Role: message.RoleAssistant, ResponseID: "resp-1", Content: []message.ContentBlock{message.TextBlock{Text: "hi"}}},
	}
	got := adjustIndexToPreserveAPIInvariants(msgs, 1)
	if got != 0 {
		t.Errorf("expected adjusted index 0 for split record, got %d", got)
	}
}

func TestCalculateMessagesToKeepIndex(t *testing.T) {
	ResetSessionMemoryCompactConfig()
	cfg := GetSessionMemoryCompactConfig()

	msgs := []message.Message{
		{Role: message.RoleUser, Content: []message.ContentBlock{message.TextBlock{Text: "hello world"}}},
	}
	got := calculateMessagesToKeepIndex(msgs, -1)
	if got != 0 {
		t.Errorf("expected 0 (expanded back to meet minimums), got %d", got)
	}

	got2 := calculateMessagesToKeepIndex(msgs, 0)
	if got2 != 0 {
		t.Errorf("expected 0, got %d", got2)
	}

	bigText := make([]byte, cfg.MinTokens*4+1000)
	for i := range bigText {
		bigText[i] = 'a'
	}
	msgs2 := []message.Message{
		{Role: message.RoleUser, Content: []message.ContentBlock{message.TextBlock{Text: string(bigText)}}},
		{Role: message.RoleUser, Content: []message.ContentBlock{message.TextBlock{Text: "small"}}},
	}
	got3 := calculateMessagesToKeepIndex(msgs2, -1)
	if got3 != 0 {
		t.Errorf("expected 0 (expanded to meet minTokens), got %d", got3)
	}
}

func TestShouldUseSessionMemoryCompaction(t *testing.T) {
	SetOptions(Options{})
	if shouldUseSessionMemoryCompaction() {
		t.Error("expected false by default")
	}

	SetOptions(Options{EnableSMCompact: true})
	if !shouldUseSessionMemoryCompaction() {
		t.Error("expected true when enabled")
	}
}

func TestTrySessionMemoryCompactionDisabled(t *testing.T) {
	SetOptions(Options{})
	result := TrySessionMemoryCompaction(nil, 0)
	if result != nil {
		t.Error("expected nil when disabled")
	}
}

func TestTrySessionMemoryCompactionNoMemory(t *testing.T) {
	SetOptions(Options{EnableSMCompact: true})
	result := TrySessionMemoryCompaction([]message.Message{}, 0)
	if result != nil {
		t.Error("expected nil when no session memory")
	}
}

func TestTrySessionMemoryCompactionWithMemory(t *testing.T) {
	SetOptions(Options{EnableSMCompact: true})

	path := ".session_memory"
	os.WriteFile(path, []byte("session memory content about the conversation"), 0644)
	defer os.Remove(path)

	msgs := []message.Message{
		{Role: message.RoleUser, Content: []message.ContentBlock{message.TextBlock{Text: "hello"}}},
		{Role: message.RoleAssistant, ResponseID: "resp-1", Content: []message.ContentBlock{message.TextBlock{Text: "hi"}}},
	}

	result := TrySessionMemoryCompaction(msgs, 1_000_000)
	if result == nil {
		t.Fatal("expected result when session memory exists")
	}
	if len(result.Messages) < 2 {
		t.Errorf("expected at least boundary + summary, got %d messages", len(result.Messages))
	}
}

func TestTrySessionMemoryCompactionThresholdExceeded(t *testing.T) {
	SetOptions(Options{EnableSMCompact: true})

	path := ".session_memory"
	os.WriteFile(path, []byte("session memory content about the conversation"), 0644)
	defer os.Remove(path)

	msgs := []message.Message{
		{Role: message.RoleUser, Content: []message.ContentBlock{message.TextBlock{Text: "hello"}}},
	}

	result := TrySessionMemoryCompaction(msgs, 1)
	if result != nil {
		t.Error("expected nil when threshold exceeded")
	}
}
