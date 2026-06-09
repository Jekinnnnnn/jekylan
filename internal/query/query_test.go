package query

import (
	"context"
	"strings"
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

func (s *stubClient) StreamMessages(ctx context.Context, msgs []message.Message, systemPrompt string, tools *tool.Registry, thinkingBudget int64, cacheBreakpoints int) (<-chan llm.StreamEvent, error) {
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
		ch <- llm.StreamEvent{Type: StreamEventTypeMessageStart}
		ch <- llm.StreamEvent{
			Type:      StreamEventTypeContentBlockStart,
			BlockType: BlockTypeToolUse,
			BlockID:   toolUseID,
			BlockName: "bash",
		}
		ch <- llm.StreamEvent{Type: StreamEventTypeContentBlockStop}
		ch <- llm.StreamEvent{Type: StreamEventTypeMessageDelta, StopReason: StopReasonToolUse}
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
		case EventTypeUsage:
			sawUsage = true
			cancel()
		case EventTypeUserMessage:
			sawUserMsg = true
			synthMsg = evt.Message
		case EventTypeError:
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
		case EventTypeUsage:
			if usageIdx < 0 {
				usageIdx = i
			}
		case EventTypeUserMessage:
			if userMsgIdx < 0 {
				userMsgIdx = i
			}
		case EventTypeError:
			if errorIdx < 0 {
				errorIdx = i
			}
		}
	}
	if !(usageIdx < userMsgIdx && userMsgIdx < errorIdx) {
		t.Fatalf("event order should be usage < user_message < error; got %v (idx u=%d u_msg=%d err=%d)", orderedTypes, usageIdx, userMsgIdx, errorIdx)
	}

	toolResults := synthMsg.ToolResults()
	if len(toolResults) != 1 {
		t.Fatalf("synthesized user_message should have 1 tool_result block, got %d (content=%+v)", len(toolResults), synthMsg.Content)
	}
	tr := toolResults[0]
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
		ch <- llm.StreamEvent{Type: StreamEventTypeMessageStart}
		for _, id := range ids {
			ch <- llm.StreamEvent{
				Type:      StreamEventTypeContentBlockStart,
				BlockType: BlockTypeToolUse,
				BlockID:   id,
				BlockName: "bash",
			}
			ch <- llm.StreamEvent{Type: StreamEventTypeContentBlockStop}
		}
		ch <- llm.StreamEvent{Type: StreamEventTypeMessageDelta, StopReason: StopReasonToolUse}
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
		if evt.Type == EventTypeUsage {
			cancel()
		}
		if evt.Type == EventTypeUserMessage {
			synthMsg = evt.Message
			sawUserMsg = true
		}
	}

	if !sawUserMsg {
		t.Fatal("expected synthesized user_message event")
	}
	toolResults := synthMsg.ToolResults()
	if len(toolResults) != len(ids) {
		t.Fatalf("expected %d tool_result blocks, got %d", len(ids), len(toolResults))
	}
	for i, want := range ids {
		tr := toolResults[i]
		if tr.ToolUseID != want {
			t.Errorf("content[%d] tool_use_id: want %q, got %q", i, want, tr.ToolUseID)
		}
		if !tr.IsError {
			t.Errorf("content[%d] IsError: want true", i)
		}
	}
}

// confirmTool is a minimal tool for testing ConfirmTool callbacks.
type confirmTestTool struct{ name string }

func (f confirmTestTool) Name() string        { return f.name }
func (f confirmTestTool) Description() string { return "desc " + f.name }
func (f confirmTestTool) InputSchema() map[string]any {
	return map[string]any{"type": "object"}
}
func (f confirmTestTool) Call(ctx context.Context, input map[string]any) (string, error) {
	return "executed", nil
}
func (f confirmTestTool) SystemPrompt() string { return "" }

// TestQuery_ConfirmToolBlocksExecution verifies that when ConfirmTool is set
// and the assistant emits a tool_use, the callback is invoked before the tool
// is executed. If the callback returns !approved, the tool result is an error.
func TestQuery_ConfirmToolBlocksExecution(t *testing.T) {
	const toolUseID = "call_confirm_test_1"

	callCount := 0
	streamFn := func(ctx context.Context) (<-chan llm.StreamEvent, error) {
		callCount++
		ch := make(chan llm.StreamEvent, 8)
		if callCount == 1 {
			ch <- llm.StreamEvent{Type: StreamEventTypeMessageStart}
			ch <- llm.StreamEvent{
				Type:      StreamEventTypeContentBlockStart,
				BlockType: BlockTypeToolUse,
				BlockID:   toolUseID,
				BlockName: "bash",
			}
			ch <- llm.StreamEvent{Type: StreamEventTypeContentBlockStop}
			ch <- llm.StreamEvent{Type: StreamEventTypeMessageDelta, StopReason: StopReasonToolUse}
		} else {
			ch <- llm.StreamEvent{Type: StreamEventTypeAssistantText, TextDelta: "Rejected"}
			ch <- llm.StreamEvent{Type: StreamEventTypeMessageDelta, StopReason: StopReasonStop}
		}
		close(ch)
		return ch, nil
	}

	stub := &stubClient{streamFn: streamFn}

	var confirmCalled bool
	confirmTool := func(ctx context.Context, toolName string, input map[string]any) (bool, error) {
		confirmCalled = true
		return false, nil // reject
	}

	user := message.Message{Role: message.RoleUser, Timestamp: time.Now()}
	user.AddText("test")

	// Provide a registry with a bash tool so the tool is found and ConfirmTool
	// is reached. The tool itself is never executed because ConfirmTool rejects.
	bashTool := confirmTestTool{name: "bash"}

	events := Query(context.Background(), Params{
		Messages:       []message.Message{user},
		Client:         stub,
		Tools:          tool.NewRegistry(bashTool),
		DisableCompact: true,
		ConfirmTool:    confirmTool,
	})

	var sawUserMsg bool
	var toolResult message.ToolResultBlock
	for evt := range events {
		if evt.Type == EventTypeUserMessage {
			sawUserMsg = true
			tr := evt.Message.ToolResults()
			if len(tr) > 0 {
				toolResult = tr[0]
			}
		}
	}

	if !confirmCalled {
		t.Fatal("expected ConfirmTool to be called")
	}
	if !sawUserMsg {
		t.Fatal("expected user_message with tool result")
	}
	if toolResult.ToolUseID != toolUseID {
		t.Fatalf("expected tool_use_id %q, got %q", toolUseID, toolResult.ToolUseID)
	}
	if !toolResult.IsError {
		t.Fatal("expected tool result to be an error (rejected)")
	}
	if !strings.Contains(toolResult.Content, "not approved") {
		t.Fatalf("expected 'not approved' in result, got: %q", toolResult.Content)
	}
}

// TestQuery_OpenAITextOnly verifies the OpenAI streaming path where text arrives
// as assistant_text events without content_block_start/stop boundaries.
func TestQuery_OpenAITextOnly(t *testing.T) {
	streamFn := func(ctx context.Context) (<-chan llm.StreamEvent, error) {
		ch := make(chan llm.StreamEvent, 8)
		ch <- llm.StreamEvent{Type: StreamEventTypeAssistantText, TextDelta: "Hello "}
		ch <- llm.StreamEvent{Type: StreamEventTypeAssistantText, TextDelta: "world"}
		ch <- llm.StreamEvent{Type: StreamEventTypeMessageDelta, StopReason: StopReasonStop}
		close(ch)
		return ch, nil
	}

	stub := &stubClient{streamFn: streamFn}

	user := message.Message{Role: message.RoleUser, Timestamp: time.Now()}
	user.AddText("hi")

	events := Query(context.Background(), Params{
		Messages:       []message.Message{user},
		Client:         stub,
		Tools:          tool.NewRegistry(),
		DisableCompact: true,
	})

	var textDeltas []string
	var sawUsage, sawResult bool
	var result Result
	for evt := range events {
		switch evt.Type {
		case EventTypeAssistantText:
			textDeltas = append(textDeltas, evt.Text)
		case EventTypeUsage:
			sawUsage = true
		case EventTypeResult:
			sawResult = true
			result = evt.Result
		}
	}

	if !sawResult {
		t.Fatal("expected result event")
	}
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if result.Text != "Hello world" {
		t.Fatalf("expected 'Hello world', got %q", result.Text)
	}
	if len(textDeltas) != 2 || textDeltas[0] != "Hello " || textDeltas[1] != "world" {
		t.Fatalf("expected 2 text deltas, got %v", textDeltas)
	}
	if !sawUsage {
		t.Fatal("expected usage event")
	}
}

// TestQuery_OpenAIToolUse verifies the OpenAI path where tool calls arrive as
// assistant_tool_use events (no content_block_start/stop).
func TestQuery_OpenAIToolUse(t *testing.T) {
	callCount := 0
	streamFn := func(ctx context.Context) (<-chan llm.StreamEvent, error) {
		callCount++
		ch := make(chan llm.StreamEvent, 8)
		if callCount == 1 {
			// First call: emit a tool_use
			ch <- llm.StreamEvent{
				Type:      StreamEventTypeAssistantToolUse,
				ToolUseID: "call_oai_1",
				ToolName:  "bash",
				InputJSON: `{"command":"echo hi"}`,
			}
			ch <- llm.StreamEvent{Type: StreamEventTypeMessageDelta, StopReason: StopReasonToolCalls}
		} else {
			// Second call: plain text response after tool execution
			ch <- llm.StreamEvent{Type: StreamEventTypeAssistantText, TextDelta: "Done!"}
			ch <- llm.StreamEvent{Type: StreamEventTypeMessageDelta, StopReason: StopReasonStop}
		}
		close(ch)
		return ch, nil
	}

	stub := &stubClient{streamFn: streamFn}

	user := message.Message{Role: message.RoleUser, Timestamp: time.Now()}
	user.AddText("run echo")

	bashTool := confirmTestTool{name: "bash"}

	events := Query(context.Background(), Params{
		Messages:       []message.Message{user},
		Client:         stub,
		Tools:          tool.NewRegistry(bashTool),
		DisableCompact: true,
	})

	var sawToolUse, sawUserMsg, sawResult bool
	var toolResultContent string
	for evt := range events {
		switch evt.Type {
		case EventTypeAssistantToolUse:
			sawToolUse = true
			if evt.ToolName != "bash" {
				t.Fatalf("expected tool name 'bash', got %q", evt.ToolName)
			}
		case EventTypeUserMessage:
			sawUserMsg = true
			trs := evt.Message.ToolResults()
			if len(trs) > 0 {
				toolResultContent = trs[0].Content
			}
		case EventTypeResult:
			sawResult = true
		}
	}

	if !sawToolUse {
		t.Fatal("expected assistant_tool_use event")
	}
	if !sawUserMsg {
		t.Fatal("expected user_message with tool result")
	}
	if toolResultContent != "executed" {
		t.Fatalf("expected 'executed', got %q", toolResultContent)
	}
	if !sawResult {
		t.Fatal("expected result event after tool execution")
	}
}

// TestQuery_OpenAIMultipleToolCalls verifies that when the OpenAI stream emits
// multiple assistant_tool_use events with different IDs, each one is finalized
// into the assistant message. Prior to the fix, only the last tool call was
// preserved because the old currentToolUse was overwritten without finalizing.
func TestQuery_OpenAIMultipleToolCalls(t *testing.T) {
	callCount := 0
	streamFn := func(ctx context.Context) (<-chan llm.StreamEvent, error) {
		callCount++
		ch := make(chan llm.StreamEvent, 8)
		if callCount == 1 {
			// First call: two tool_use calls
			ch <- llm.StreamEvent{
				Type:      StreamEventTypeAssistantToolUse,
				ToolUseID: "call_1",
				ToolName:  "bash",
				InputJSON: `{"command":"echo 1"}`,
			}
			ch <- llm.StreamEvent{
				Type:      StreamEventTypeAssistantToolUse,
				ToolUseID: "call_2",
				ToolName:  "bash",
				InputJSON: `{"command":"echo 2"}`,
			}
			ch <- llm.StreamEvent{Type: StreamEventTypeMessageDelta, StopReason: StopReasonToolCalls}
		} else {
			ch <- llm.StreamEvent{Type: StreamEventTypeAssistantText, TextDelta: "Done!"}
			ch <- llm.StreamEvent{Type: StreamEventTypeMessageDelta, StopReason: StopReasonStop}
		}
		close(ch)
		return ch, nil
	}

	stub := &stubClient{streamFn: streamFn}

	user := message.Message{Role: message.RoleUser, Timestamp: time.Now()}
	user.AddText("run two commands")

	bashTool := confirmTestTool{name: "bash"}

	events := Query(context.Background(), Params{
		Messages:       []message.Message{user},
		Client:         stub,
		Tools:          tool.NewRegistry(bashTool),
		DisableCompact: true,
	})

	var toolUseIDs []string
	var sawUserMsg bool
	var toolResults []message.ToolResultBlock
	for evt := range events {
		switch evt.Type {
		case EventTypeAssistantToolUse:
			toolUseIDs = append(toolUseIDs, evt.ToolUseID)
		case EventTypeUserMessage:
			sawUserMsg = true
			toolResults = evt.Message.ToolResults()
		}
	}

	if len(toolUseIDs) != 2 {
		t.Fatalf("expected 2 assistant_tool_use events, got %d (ids=%v)", len(toolUseIDs), toolUseIDs)
	}
	if toolUseIDs[0] != "call_1" || toolUseIDs[1] != "call_2" {
		t.Fatalf("expected tool use IDs [call_1, call_2], got %v", toolUseIDs)
	}
	if !sawUserMsg {
		t.Fatal("expected user_message with tool results")
	}
	if len(toolResults) != 2 {
		t.Fatalf("expected 2 tool_results, got %d", len(toolResults))
	}
	for i, tr := range toolResults {
		if tr.Content != "executed" {
			t.Errorf("tool result %d: expected 'executed', got %q", i, tr.Content)
		}
	}
}

// TestQuery_UsageFromLegacyFields verifies that when a provider sends usage
// via the legacy UsagePromptTokens / UsageCompletionTokens fields instead of
// the unified Usage pointer, the query loop still captures the token stats.
func TestQuery_UsageFromLegacyFields(t *testing.T) {
	streamFn := func(ctx context.Context) (<-chan llm.StreamEvent, error) {
		ch := make(chan llm.StreamEvent, 8)
		ch <- llm.StreamEvent{Type: StreamEventTypeMessageStart, UsagePromptTokens: 100, UsageCompletionTokens: 50}
		ch <- llm.StreamEvent{Type: StreamEventTypeAssistantText, TextDelta: "Hello"}
		ch <- llm.StreamEvent{Type: StreamEventTypeMessageDelta, StopReason: StopReasonStop, UsagePromptTokens: 100, UsageCompletionTokens: 50}
		close(ch)
		return ch, nil
	}

	stub := &stubClient{streamFn: streamFn}

	user := message.Message{Role: message.RoleUser, Timestamp: time.Now()}
	user.AddText("hi")

	events := Query(context.Background(), Params{
		Messages:       []message.Message{user},
		Client:         stub,
		Tools:          tool.NewRegistry(),
		DisableCompact: true,
	})

	var sawUsage bool
	for evt := range events {
		if evt.Type == EventTypeUsage {
			sawUsage = true
			u := evt.Message.Usage
			if u == nil {
				t.Fatal("expected usage on usage event, got nil")
			}
			if u.InputTokens != 100 {
				t.Errorf("input tokens: want 100, got %d", u.InputTokens)
			}
			if u.OutputTokens != 50 {
				t.Errorf("output tokens: want 50, got %d", u.OutputTokens)
			}
		}
	}

	if !sawUsage {
		t.Fatal("expected usage event")
	}
}

// TestQuery_MaxTurnsExceeded verifies that the query loop stops after MaxTurns.
func TestQuery_MaxTurnsExceeded(t *testing.T) {
	callCount := 0
	streamFn := func(ctx context.Context) (<-chan llm.StreamEvent, error) {
		callCount++
		ch := make(chan llm.StreamEvent, 8)
		ch <- llm.StreamEvent{
			Type:      StreamEventTypeAssistantToolUse,
			ToolUseID: "call_max_1",
			ToolName:  "bash",
			InputJSON: `{"command":"echo"}`,
		}
		ch <- llm.StreamEvent{Type: StreamEventTypeMessageDelta, StopReason: StopReasonToolCalls}
		close(ch)
		return ch, nil
	}

	stub := &stubClient{streamFn: streamFn}
	user := message.Message{Role: message.RoleUser, Timestamp: time.Now()}
	user.AddText("test")

	bashTool := confirmTestTool{name: "bash"}

	events := Query(context.Background(), Params{
		Messages:       []message.Message{user},
		Client:         stub,
		Tools:          tool.NewRegistry(bashTool),
		DisableCompact: true,
		MaxTurns:       2,
	})

	var sawResult bool
	var result Result
	for evt := range events {
		if evt.Type == EventTypeResult {
			sawResult = true
			result = evt.Result
		}
	}

	if !sawResult {
		t.Fatal("expected result event")
	}
	if result.Success {
		t.Fatalf("expected failure due to max turns, got success")
	}
	if result.NumTurns != 3 {
		t.Fatalf("expected NumTurns=3 (third turn exceeded limit), got %d", result.NumTurns)
	}
}

// TestQuery_MaxOutputTokensRecovery verifies that when the assistant stops with
// max_output_tokens, a recovery user message is injected and the loop continues.
func TestQuery_MaxOutputTokensRecovery(t *testing.T) {
	callCount := 0
	streamFn := func(ctx context.Context) (<-chan llm.StreamEvent, error) {
		callCount++
		ch := make(chan llm.StreamEvent, 8)
		if callCount == 1 {
			ch <- llm.StreamEvent{Type: StreamEventTypeAssistantText, TextDelta: "Partial"}
			ch <- llm.StreamEvent{Type: StreamEventTypeMessageDelta, StopReason: StopReasonMaxOutputTokens}
		} else {
			ch <- llm.StreamEvent{Type: StreamEventTypeAssistantText, TextDelta: " rest"}
			ch <- llm.StreamEvent{Type: StreamEventTypeMessageDelta, StopReason: StopReasonStop}
		}
		close(ch)
		return ch, nil
	}

	stub := &stubClient{streamFn: streamFn}
	user := message.Message{Role: message.RoleUser, Timestamp: time.Now()}
	user.AddText("test")

	events := Query(context.Background(), Params{
		Messages:       []message.Message{user},
		Client:         stub,
		Tools:          tool.NewRegistry(),
		DisableCompact: true,
	})

	var sawUserMsg, sawResult bool
	var result Result
	for evt := range events {
		if evt.Type == EventTypeUserMessage {
			sawUserMsg = true
		}
		if evt.Type == EventTypeResult {
			sawResult = true
			result = evt.Result
		}
	}

	if !sawUserMsg {
		t.Fatal("expected user_message recovery event")
	}
	if !sawResult {
		t.Fatal("expected result event")
	}
	if !result.Success {
		t.Fatalf("expected success after recovery, got error: %s", result.Error)
	}
	// The first turn text "Partial" was emitted as assistant_text events.
	// The final Result.Text only contains the second turn's text.
	if result.Text != " rest" {
		t.Fatalf("expected ' rest', got %q", result.Text)
	}
}

// TestQuery_FallbackModel verifies that when the client returns a fallback error,
// the query loop retries with the fallback model.
type fallbackStubClient struct {
	stubClient
	modelSet string
}

func (f *fallbackStubClient) SetModel(model string) {
	f.modelSet = model
}

func TestQuery_FallbackModel(t *testing.T) {
	callCount := 0
	fstub := &fallbackStubClient{}
	fstub.streamFn = func(ctx context.Context) (<-chan llm.StreamEvent, error) {
		callCount++
		if callCount == 1 {
			return nil, llm.ErrFallbackTriggered
		}
		ch := make(chan llm.StreamEvent, 8)
		ch <- llm.StreamEvent{Type: StreamEventTypeAssistantText, TextDelta: "Fallback"}
		ch <- llm.StreamEvent{Type: StreamEventTypeMessageDelta, StopReason: StopReasonStop}
		close(ch)
		return ch, nil
	}

	user := message.Message{Role: message.RoleUser, Timestamp: time.Now()}
	user.AddText("test")

	events := Query(context.Background(), Params{
		Messages:       []message.Message{user},
		Client:         fstub,
		Tools:          tool.NewRegistry(),
		DisableCompact: true,
		FallbackModel:  "fallback-model",
	})

	var sawResult bool
	var result Result
	for evt := range events {
		if evt.Type == EventTypeResult {
			sawResult = true
			result = evt.Result
		}
	}

	if !sawResult {
		t.Fatal("expected result event")
	}
	if !result.Success {
		t.Fatalf("expected success after fallback, got error: %s", result.Error)
	}
	if result.Text != "Fallback" {
		t.Fatalf("expected 'Fallback', got %q", result.Text)
	}
	if fstub.modelSet != "fallback-model" {
		t.Fatalf("expected fallback model to be set, got %q", fstub.modelSet)
	}
}

// TestQuery_ThinkingBlocks verifies that thinking and signature deltas are
// captured into ThinkingBlock content in the assistant message.
func TestQuery_ThinkingBlocks(t *testing.T) {
	streamFn := func(ctx context.Context) (<-chan llm.StreamEvent, error) {
		ch := make(chan llm.StreamEvent, 8)
		ch <- llm.StreamEvent{Type: StreamEventTypeMessageStart}
		ch <- llm.StreamEvent{
			Type:      StreamEventTypeContentBlockStart,
			BlockType: BlockTypeThinking,
		}
		ch <- llm.StreamEvent{Type: StreamEventTypeContentBlockDelta, ThinkingDelta: "I think"}
		ch <- llm.StreamEvent{Type: StreamEventTypeContentBlockDelta, ThinkingDelta: " therefore"}
		ch <- llm.StreamEvent{Type: StreamEventTypeContentBlockDelta, SignatureDelta: "sig123"}
		ch <- llm.StreamEvent{Type: StreamEventTypeContentBlockStop}
		ch <- llm.StreamEvent{Type: StreamEventTypeMessageDelta, StopReason: StopReasonStop}
		close(ch)
		return ch, nil
	}

	stub := &stubClient{streamFn: streamFn}
	user := message.Message{Role: message.RoleUser, Timestamp: time.Now()}
	user.AddText("test")

	events := Query(context.Background(), Params{
		Messages:       []message.Message{user},
		Client:         stub,
		Tools:          tool.NewRegistry(),
		DisableCompact: true,
	})

	var sawUsage bool
	var usageMsg message.Message
	for evt := range events {
		if evt.Type == EventTypeUsage {
			sawUsage = true
			usageMsg = evt.Message
		}
	}

	if !sawUsage {
		t.Fatal("expected usage event")
	}
	thinkingBlocks := usageMsg.ThinkingBlocks()
	if len(thinkingBlocks) != 1 {
		t.Fatalf("expected 1 thinking block, got %d", len(thinkingBlocks))
	}
	th := thinkingBlocks[0]
	if th.Thinking != "I think therefore" {
		t.Errorf("thinking text: want %q, got %q", "I think therefore", th.Thinking)
	}
	if th.Signature != "sig123" {
		t.Errorf("signature: want %q, got %q", "sig123", th.Signature)
	}
}

// TestQuery_RedactedThinkingBlocks verifies that redacted thinking blocks are
// captured into RedactedThinkingBlock content in the assistant message.
func TestQuery_RedactedThinkingBlocks(t *testing.T) {
	streamFn := func(ctx context.Context) (<-chan llm.StreamEvent, error) {
		ch := make(chan llm.StreamEvent, 8)
		ch <- llm.StreamEvent{Type: StreamEventTypeMessageStart}
		ch <- llm.StreamEvent{
			Type:          StreamEventTypeContentBlockStart,
			BlockType:     BlockTypeRedactedThinking,
			BlockRedacted: "redacted-data",
		}
		ch <- llm.StreamEvent{Type: StreamEventTypeContentBlockStop}
		ch <- llm.StreamEvent{Type: StreamEventTypeMessageDelta, StopReason: StopReasonStop}
		close(ch)
		return ch, nil
	}

	stub := &stubClient{streamFn: streamFn}
	user := message.Message{Role: message.RoleUser, Timestamp: time.Now()}
	user.AddText("test")

	events := Query(context.Background(), Params{
		Messages:       []message.Message{user},
		Client:         stub,
		Tools:          tool.NewRegistry(),
		DisableCompact: true,
	})

	var sawUsage bool
	var usageMsg message.Message
	for evt := range events {
		if evt.Type == EventTypeUsage {
			sawUsage = true
			usageMsg = evt.Message
		}
	}

	if !sawUsage {
		t.Fatal("expected usage event")
	}
	redactedBlocks := usageMsg.RedactedThinkingBlocks()
	if len(redactedBlocks) != 1 {
		t.Fatalf("expected 1 redacted_thinking block, got %d", len(redactedBlocks))
	}
	rth := redactedBlocks[0]
	if rth.Data != "redacted-data" {
		t.Errorf("redacted data: want %q, got %q", "redacted-data", rth.Data)
	}
}

// TestQuery_ToolInputJSONParseFallback verifies that when accumulated tool input
// JSON is malformed, the tool_use event preserves the BlockInput that was set
// during content_block_start instead of dropping it to nil.
func TestQuery_ToolInputJSONParseFallback(t *testing.T) {
	callCount := 0
	streamFn := func(ctx context.Context) (<-chan llm.StreamEvent, error) {
		callCount++
		ch := make(chan llm.StreamEvent, 8)
		if callCount == 1 {
			ch <- llm.StreamEvent{Type: StreamEventTypeMessageStart}
			ch <- llm.StreamEvent{
				Type:       StreamEventTypeContentBlockStart,
				BlockType:  BlockTypeToolUse,
				BlockID:    "call_fallback_1",
				BlockName:  "bash",
				BlockInput: map[string]any{"existing": "value"},
			}
			ch <- llm.StreamEvent{Type: StreamEventTypeContentBlockDelta, InputJSON: "{invalid"}
			ch <- llm.StreamEvent{Type: StreamEventTypeContentBlockStop}
			ch <- llm.StreamEvent{Type: StreamEventTypeMessageDelta, StopReason: StopReasonToolUse}
		} else {
			ch <- llm.StreamEvent{Type: StreamEventTypeAssistantText, TextDelta: "Done"}
			ch <- llm.StreamEvent{Type: StreamEventTypeMessageDelta, StopReason: StopReasonStop}
		}
		close(ch)
		return ch, nil
	}

	stub := &stubClient{streamFn: streamFn}

	user := message.Message{Role: message.RoleUser, Timestamp: time.Now()}
	user.AddText("test")

	bashTool := confirmTestTool{name: "bash"}

	events := Query(context.Background(), Params{
		Messages:       []message.Message{user},
		Client:         stub,
		Tools:          tool.NewRegistry(bashTool),
		DisableCompact: true,
	})

	var toolEvt Event
	for evt := range events {
		if evt.Type == EventTypeAssistantToolUse {
			toolEvt = evt
		}
	}

	if toolEvt.ToolUseID == "" {
		t.Fatal("expected assistant_tool_use event")
	}
	if toolEvt.ToolInput == nil {
		t.Fatal("expected ToolInput to be preserved, got nil")
	}
	if v, ok := toolEvt.ToolInput["existing"]; !ok || v != "value" {
		t.Fatalf("expected ToolInput[existing] = 'value', got %+v", toolEvt.ToolInput)
	}
}

// recordingStubClient records every message slice passed to StreamMessages.
type recordingStubClient struct {
	calls [][]message.Message
	fn    func(callIdx int, msgs []message.Message) <-chan llm.StreamEvent
}

func (r *recordingStubClient) StreamMessages(ctx context.Context, msgs []message.Message, systemPrompt string, tools *tool.Registry, thinkingBudget int64, cacheBreakpoints int) (<-chan llm.StreamEvent, error) {
	idx := len(r.calls)
	r.calls = append(r.calls, append([]message.Message(nil), msgs...))
	ch := make(chan llm.StreamEvent, 16)
	events := r.fn(idx, msgs)
	go func() {
		for evt := range events {
			ch <- evt
		}
		close(ch)
	}()
	return ch, nil
}

// TestQuery_MessagesIncludeToolResultsAcrossTurns verifies that when the
// assistant calls a tool, the tool_result message is included in the message
// history passed to the next API call. This prevents the model from looping
// on the same tool call because it cannot see the previous result.
func TestQuery_MessagesIncludeToolResultsAcrossTurns(t *testing.T) {
	client := &recordingStubClient{
		fn: func(callIdx int, msgs []message.Message) <-chan llm.StreamEvent {
			ch := make(chan llm.StreamEvent, 8)
			if callIdx == 0 {
				// First call: emit a tool_use.
				ch <- llm.StreamEvent{
					Type:      StreamEventTypeAssistantToolUse,
					ToolUseID: "call_1",
					ToolName:  "bash",
					InputJSON: `{"command":"echo hi"}`,
				}
				ch <- llm.StreamEvent{Type: StreamEventTypeMessageDelta, StopReason: StopReasonToolCalls}
			} else if callIdx == 1 {
				// Verify that the second call received the tool_result.
				foundToolResult := false
				for _, m := range msgs {
					for _, block := range m.Content {
						if tr, ok := block.(message.ToolResultBlock); ok {
							if tr.ToolUseID == "call_1" {
								foundToolResult = true
							}
						}
					}
				}
				if !foundToolResult {
					// Emit an error that the test can catch.
					ch <- llm.StreamEvent{Type: StreamEventTypeError, TextDelta: "tool_result missing"}
				} else {
					// Normal text response.
					ch <- llm.StreamEvent{Type: StreamEventTypeAssistantText, TextDelta: "Done!"}
					ch <- llm.StreamEvent{Type: StreamEventTypeMessageDelta, StopReason: StopReasonStop}
				}
			}
			close(ch)
			return ch
		},
	}

	user := message.Message{Role: message.RoleUser, Timestamp: time.Now()}
	user.AddText("run echo")

	bashTool := confirmTestTool{name: "bash"}

	events := Query(context.Background(), Params{
		Messages:       []message.Message{user},
		Client:         client,
		Tools:          tool.NewRegistry(bashTool),
		DisableCompact: true,
	})

	var sawError bool
	var errorText string
	var sawResult bool
	for evt := range events {
		if evt.Type == EventTypeError {
			sawError = true
			errorText = evt.Result.Error
		}
		if evt.Type == EventTypeResult {
			sawResult = true
		}
	}

	if len(client.calls) != 2 {
		t.Fatalf("expected 2 API calls, got %d", len(client.calls))
	}

	if sawError {
		t.Fatalf("second API call reported error: %s", errorText)
	}
	if !sawResult {
		t.Fatal("expected successful result after tool execution")
	}
}
