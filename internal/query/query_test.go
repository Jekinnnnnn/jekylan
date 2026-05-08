package query

import (
	"context"
	"testing"
	"time"

	"github.com/Jekinnnnnn/jekylan/internal/llm"
	"github.com/Jekinnnnnn/jekylan/internal/message"
	"github.com/Jekinnnnnn/jekylan/internal/tool"
)

// stubClient is a minimal llm.Client for testing query loop behavior.
type stubClient struct {
	streamFn func(ctx context.Context) (<-chan llm.StreamEvent, error)
}

func (s *stubClient) StreamMessages(ctx context.Context, msgs []message.Message, systemPrompt string, tools *tool.Registry, thinkingBudget int64) (<-chan llm.StreamEvent, error) {
	return s.streamFn(ctx)
}

// TestCancelAfterUsageSynthesizesToolResult verifies that when ctx is canceled
// after the assistant message (containing tool_use) has been emitted but
// before tool execution starts, the query loop synthesizes a user_message
// with placeholder ToolResultBlocks for each orphan tool_use ID. This is the
// reactive layer of the /stop fix and must work for both Anthropic and
// OpenAI providers (the synthesis happens in the provider-agnostic layer).
func TestCancelAfterUsageSynthesizesToolResult(t *testing.T) {
	const toolUseID = "call_test_synth_1"

	streamFn := func(ctx context.Context) (<-chan llm.StreamEvent, error) {
		ch := make(chan llm.StreamEvent, 8)
		ch <- llm.StreamEvent{Type: "message_start"}
		ch <- llm.StreamEvent{
			Type:      "content_block_start",
			BlockType: "tool_use",
			BlockID:   toolUseID,
			BlockName: "bash",
		}
		ch <- llm.StreamEvent{Type: "content_block_stop"}
		ch <- llm.StreamEvent{Type: "message_delta", StopReason: "tool_use"}
		close(ch)
		return ch, nil
	}

	stub := &stubClient{streamFn: streamFn}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	user := message.Message{Role: message.RoleUser, Timestamp: time.Now()}
	user.AddText("test")

	events := Query(ctx, Params{
		Messages:       []message.Message{user},
		Client:         stub,
		Tools:          tool.NewRegistry(),
		DisableCompact: true,
	})

	var sawUsage, sawUserMsg, sawError bool
	var synthMsg message.Message
	var orderedTypes []string
	for evt := range events {
		orderedTypes = append(orderedTypes, evt.Type)
		switch evt.Type {
		case "usage":
			sawUsage = true
			cancel()
		case "user_message":
			sawUserMsg = true
			synthMsg = evt.Message
		case "error":
			sawError = true
		}
	}

	if !sawUsage {
		t.Fatalf("expected usage event in stream; saw %v", orderedTypes)
	}
	if !sawUserMsg {
		t.Fatalf("expected synthesized user_message event after cancel; saw %v", orderedTypes)
	}
	if !sawError {
		t.Fatalf("expected error event; saw %v", orderedTypes)
	}

	usageIdx, userMsgIdx, errorIdx := -1, -1, -1
	for i, typ := range orderedTypes {
		switch typ {
		case "usage":
			if usageIdx < 0 {
				usageIdx = i
			}
		case "user_message":
			if userMsgIdx < 0 {
				userMsgIdx = i
			}
		case "error":
			if errorIdx < 0 {
				errorIdx = i
			}
		}
	}
	if !(usageIdx < userMsgIdx && userMsgIdx < errorIdx) {
		t.Fatalf("event order should be usage < user_message < error; got %v (idx u=%d u_msg=%d err=%d)", orderedTypes, usageIdx, userMsgIdx, errorIdx)
	}

	if len(synthMsg.Content) != 1 {
		t.Fatalf("synthesized user_message should have 1 tool_result block, got %d (content=%+v)", len(synthMsg.Content), synthMsg.Content)
	}
	tr, ok := synthMsg.Content[0].(message.ToolResultBlock)
	if !ok {
		t.Fatalf("content[0] is not a ToolResultBlock: %T", synthMsg.Content[0])
	}
	if tr.ToolUseID != toolUseID {
		t.Errorf("synthesized tool_use_id: want %q, got %q", toolUseID, tr.ToolUseID)
	}
	if tr.Content != "[Tool execution interrupted]" {
		t.Errorf("synthesized content: want \"[Tool execution interrupted]\", got %q", tr.Content)
	}
	if !tr.IsError {
		t.Error("synthesized IsError: want true, got false")
	}
	if synthMsg.Role != message.RoleUser {
		t.Errorf("synthesized message role: want user, got %s", synthMsg.Role)
	}
}

// TestCancelAfterUsageMultipleToolUses verifies the synthesis covers all
// orphan tool_use IDs when the assistant emits multiple tool calls in one turn.
func TestCancelAfterUsageMultipleToolUses(t *testing.T) {
	ids := []string{"call_a", "call_b", "call_c"}

	streamFn := func(ctx context.Context) (<-chan llm.StreamEvent, error) {
		ch := make(chan llm.StreamEvent, 16)
		ch <- llm.StreamEvent{Type: "message_start"}
		for _, id := range ids {
			ch <- llm.StreamEvent{
				Type:      "content_block_start",
				BlockType: "tool_use",
				BlockID:   id,
				BlockName: "bash",
			}
			ch <- llm.StreamEvent{Type: "content_block_stop"}
		}
		ch <- llm.StreamEvent{Type: "message_delta", StopReason: "tool_use"}
		close(ch)
		return ch, nil
	}

	stub := &stubClient{streamFn: streamFn}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	user := message.Message{Role: message.RoleUser, Timestamp: time.Now()}
	user.AddText("multi")

	events := Query(ctx, Params{
		Messages:       []message.Message{user},
		Client:         stub,
		Tools:          tool.NewRegistry(),
		DisableCompact: true,
	})

	var synthMsg message.Message
	var sawUserMsg bool
	for evt := range events {
		if evt.Type == "usage" {
			cancel()
		}
		if evt.Type == "user_message" {
			synthMsg = evt.Message
			sawUserMsg = true
		}
	}

	if !sawUserMsg {
		t.Fatal("expected synthesized user_message event")
	}
	if len(synthMsg.Content) != len(ids) {
		t.Fatalf("expected %d tool_result blocks, got %d", len(ids), len(synthMsg.Content))
	}
	for i, want := range ids {
		tr, ok := synthMsg.Content[i].(message.ToolResultBlock)
		if !ok {
			t.Fatalf("content[%d] is not a ToolResultBlock: %T", i, synthMsg.Content[i])
		}
		if tr.ToolUseID != want {
			t.Errorf("content[%d] tool_use_id: want %q, got %q", i, want, tr.ToolUseID)
		}
		if !tr.IsError {
			t.Errorf("content[%d] IsError: want true", i)
		}
	}
}
