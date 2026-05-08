package compact

import (
	"context"
	"testing"

	"github.com/Jekinnnnnn/jekylan/internal/message"
)

func TestRoughTokenCount(t *testing.T) {
	cases := []struct {
		input    string
		expected int
	}{
		{"", 0},           // 0/4 = 0
		{"a", 0},          // 1/4 = 0.25 -> round = 0
		{"ab", 1},         // 2/4 = 0.5  -> round = 1
		{"abc", 1},        // 3/4 = 0.75 -> round = 1
		{"abcd", 1},       // 4/4 = 1
		{"abcdefgh", 2},   // 8/4 = 2
		{"abcdefghij", 3}, // 10/4 = 2.5 -> round = 3
	}

	for _, c := range cases {
		got := RoughTokenCount(c.input)
		if got != c.expected {
			t.Errorf("RoughTokenCount(%q) = %d, want %d", c.input, got, c.expected)
		}
	}
}

func TestRoughTokenCountWithCustomRatio(t *testing.T) {
	// JSON at 2 bytes/token: len(`{"a":1}`) = 7 -> round(7/2) = 4
	got := RoughTokenCount(`{"a":1}`, 2.0)
	want := 4
	if got != want {
		t.Errorf("RoughTokenCount JSON = %d, want %d", got, want)
	}
}

func TestBytesPerTokenForFileType(t *testing.T) {
	if BytesPerTokenForFileType("json") != 2.0 {
		t.Error("expected json = 2.0")
	}
	if BytesPerTokenForFileType("jsonl") != 2.0 {
		t.Error("expected jsonl = 2.0")
	}
	if BytesPerTokenForFileType("go") != 4.0 {
		t.Error("expected go = 4.0")
	}
}

func TestRoughTokenCountForFile(t *testing.T) {
	// len(`{"key":"value"}`) = 15 -> round(15/2) = 8
	got := RoughTokenCountForFile(`{"key":"value"}`, "data.json")
	want := 8
	if got != want {
		t.Errorf("RoughTokenCountForFile = %d, want %d", got, want)
	}
}

func TestRoughTokenCountForBlock(t *testing.T) {
	// tool_use JSON: name="bash"(4) + `{"cmd":"ls"}`(12) = 16 -> round(16/4)=4
	tests := []struct {
		name  string
		block message.ContentBlock
		want  int
	}{
		{"text", message.TextBlock{Text: "abcd"}, 1},
		{"thinking", message.ThinkingBlock{Thinking: "abcd"}, 1},
		{"redacted", message.RedactedThinkingBlock{Data: "abcd"}, 1},
		{"tool_result", message.ToolResultBlock{Content: "abcd"}, 1},
		{"tool_use", message.ToolUseBlock{Name: "bash", Input: map[string]any{"cmd": "ls"}}, 4},
	}

	for _, tt := range tests {
		got := RoughTokenCountForBlock(tt.block)
		if got != tt.want {
			t.Errorf("RoughTokenCountForBlock(%s) = %d, want %d", tt.name, got, tt.want)
		}
	}
}

func TestEstimateMessageTokens(t *testing.T) {
	msgs := []message.Message{
		{Role: message.RoleUser, Content: []message.ContentBlock{message.TextBlock{Text: "hello world"}}},
	}
	// 11 chars -> round(11/4)=3
	got := EstimateMessageTokens(msgs)
	if got != 3 {
		t.Errorf("EstimateMessageTokens = %d, want 3", got)
	}
}

func TestCountTokensFallbackWhenClientIsNil(t *testing.T) {
	msgs := []message.Message{
		{Role: message.RoleUser, Content: []message.ContentBlock{message.TextBlock{Text: "hello world"}}},
	}
	got := CountTokens(context.Background(), nil, msgs, nil)
	want := EstimateMessageTokens(msgs)
	if got != want {
		t.Errorf("CountTokens(nil client) = %d, want %d (rough fallback)", got, want)
	}
}

func TestGetTokenUsage(t *testing.T) {
	usage := &message.Usage{InputTokens: 10, OutputTokens: 5}
	msg := message.Message{Role: message.RoleAssistant, Usage: usage}
	got := GetTokenUsage(msg)
	if got != usage {
		t.Error("GetTokenUsage should return usage for assistant message")
	}

	userMsg := message.Message{Role: message.RoleUser, Usage: usage}
	if GetTokenUsage(userMsg) != nil {
		t.Error("GetTokenUsage should return nil for non-assistant message")
	}

	noUsage := message.Message{Role: message.RoleAssistant}
	if GetTokenUsage(noUsage) != nil {
		t.Error("GetTokenUsage should return nil when usage is nil")
	}
}

func TestGetTokenCountFromUsage(t *testing.T) {
	u := &message.Usage{InputTokens: 10, OutputTokens: 5, CacheCreationInputTokens: 3, CacheReadInputTokens: 2}
	if got := GetTokenCountFromUsage(u); got != 20 {
		t.Errorf("GetTokenCountFromUsage = %d, want 20", got)
	}
	if got := GetTokenCountFromUsage(nil); got != 0 {
		t.Errorf("GetTokenCountFromUsage(nil) = %d, want 0", got)
	}
}

func TestTokenCountFromLastAPIResponse(t *testing.T) {
	msgs := []message.Message{
		{Role: message.RoleUser, Content: []message.ContentBlock{message.TextBlock{Text: "hi"}}},
		{Role: message.RoleAssistant, Usage: &message.Usage{InputTokens: 100, OutputTokens: 50}},
	}
	if got := TokenCountFromLastAPIResponse(msgs); got != 150 {
		t.Errorf("TokenCountFromLastAPIResponse = %d, want 150", got)
	}

	// No usage
	msgs2 := []message.Message{
		{Role: message.RoleUser, Content: []message.ContentBlock{message.TextBlock{Text: "hi"}}},
	}
	if got := TokenCountFromLastAPIResponse(msgs2); got != 0 {
		t.Errorf("TokenCountFromLastAPIResponse(no usage) = %d, want 0", got)
	}
}

func TestTokenCountWithEstimation(t *testing.T) {
	// Case 1: No usage → falls back to rough estimate.
	msgs := []message.Message{
		{Role: message.RoleUser, Content: []message.ContentBlock{message.TextBlock{Text: "hello world"}}},
	}
	got := TokenCountWithEstimation(msgs)
	want := EstimateMessageTokens(msgs)
	if got != want {
		t.Errorf("TokenCountWithEstimation(no usage) = %d, want %d", got, want)
	}

	// Case 2: Usage on last assistant message.
	msgs2 := []message.Message{
		{Role: message.RoleUser, Content: []message.ContentBlock{message.TextBlock{Text: "hello world"}}},
		{Role: message.RoleAssistant, Usage: &message.Usage{InputTokens: 100, OutputTokens: 50}},
	}
	got2 := TokenCountWithEstimation(msgs2)
	// 150 from usage + 0 new messages after
	if got2 != 150 {
		t.Errorf("TokenCountWithEstimation = %d, want 150", got2)
	}

	// Case 3: Split assistant records with same ResponseID + interleaved tool_results.
	msgs3 := []message.Message{
		{Role: message.RoleUser, Content: []message.ContentBlock{message.TextBlock{Text: "hello"}}},
		{Role: message.RoleAssistant, ResponseID: "resp-1", Usage: &message.Usage{InputTokens: 100, OutputTokens: 50}},
		{Role: message.RoleUser, Content: []message.ContentBlock{message.ToolResultBlock{ToolUseID: "t1", Content: "result1"}}},
		{Role: message.RoleAssistant, ResponseID: "resp-1"},
		{Role: message.RoleUser, Content: []message.ContentBlock{message.ToolResultBlock{ToolUseID: "t2", Content: "result2"}}},
	}
	got3 := TokenCountWithEstimation(msgs3)
	// Walk back to first split (index 1). Estimate msgs[2:] = tool_result1 + split2 + tool_result2.
	// tool_result1: "result1"=7 -> round(7/4)=2
	// split2 has no content blocks -> 0
	// tool_result2: "result2"=7 -> round(7/4)=2
	// total = 150 + 2 + 0 + 2 = 154
	want3 := 150 + 2 + 0 + 2
	if got3 != want3 {
		t.Errorf("TokenCountWithEstimation(split) = %d, want %d", got3, want3)
	}

	// Case 4: Different ResponseID stops walking.
	msgs4 := []message.Message{
		{Role: message.RoleAssistant, ResponseID: "resp-1", Usage: &message.Usage{InputTokens: 50, OutputTokens: 10}},
		{Role: message.RoleUser, Content: []message.ContentBlock{message.TextBlock{Text: "a"}}},
		{Role: message.RoleAssistant, ResponseID: "resp-2", Usage: &message.Usage{InputTokens: 100, OutputTokens: 20}},
	}
	got4 := TokenCountWithEstimation(msgs4)
	// Last usage at index 2, walk back: prior at index 0 has different ResponseID → stop.
	// Estimate msgs[3:] = 0. Total = 120.
	if got4 != 120 {
		t.Errorf("TokenCountWithEstimation(diff resp) = %d, want 120", got4)
	}
}
