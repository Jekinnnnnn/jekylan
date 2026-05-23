package agent

import (
	"testing"

	"github.com/Jekinnnnnn/jekylan/internal/message"
)

func TestBuildForkMessages(t *testing.T) {
	// Parent conversation: user -> assistant (with tool_use)
	userMsg := message.Message{Role: message.RoleUser}
	userMsg.AddText("do something")

	assistantMsg := message.Message{Role: message.RoleAssistant}
	assistantMsg.AddText("I'll help")
	assistantMsg.AddToolUse("tu-1", "agent", map[string]any{"prompt": "subtask"})

	parentMsgs := []message.Message{userMsg, assistantMsg}

	result := BuildForkMessages(parentMsgs, "Your task is to review the code.")

	// Should have: user + assistant + synthetic tool_result + directive
	if len(result) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(result))
	}

	// Verify deep copy: modifying original should not affect result.
	userMsg.AddText(" extra")
	if result[0].TextContent() != "do something" {
		t.Fatal("expected deep copy of messages")
	}

	// Message 0: user
	if result[0].Role != message.RoleUser {
		t.Fatalf("msg[0] expected user, got %s", result[0].Role)
	}

	// Message 1: assistant (with tool_use preserved)
	if result[1].Role != message.RoleAssistant {
		t.Fatalf("msg[1] expected assistant, got %s", result[1].Role)
	}
	toolUses := result[1].ToolUses()
	if len(toolUses) != 1 || toolUses[0].ID != "tu-1" {
		t.Fatalf("expected tool_use preserved, got %+v", toolUses)
	}

	// Message 2: synthetic tool_result
	if result[2].Role != message.RoleUser {
		t.Fatalf("msg[2] expected user (tool_result), got %s", result[2].Role)
	}
	tr := result[2].ToolResults()
	if len(tr) != 1 || tr[0].ToolUseID != "tu-1" {
		t.Fatalf("expected synthetic tool_result for tu-1, got %+v", tr)
	}
	if tr[0].IsError {
		t.Fatal("synthetic tool_result should not be an error")
	}

	// Message 3: directive
	if result[3].Role != message.RoleUser {
		t.Fatalf("msg[3] expected user (directive), got %s", result[3].Role)
	}
	if result[3].TextContent() != "Your task is to review the code." {
		t.Fatalf("unexpected directive: %s", result[3].TextContent())
	}
}

func TestBuildForkMessages_NoToolUse(t *testing.T) {
	// Assistant message without tool_use — no synthetic tool_result needed.
	userMsg := message.Message{Role: message.RoleUser}
	userMsg.AddText("hello")

	assistantMsg := message.Message{Role: message.RoleAssistant}
	assistantMsg.AddText("Hi there")

	parentMsgs := []message.Message{userMsg, assistantMsg}
	result := BuildForkMessages(parentMsgs, "directive")

	// Should have: user + assistant + directive (no synthetic tool_result)
	if len(result) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(result))
	}
	if result[2].TextContent() != "directive" {
		t.Fatalf("expected directive, got: %s", result[2].TextContent())
	}
}

func TestBuildForkMessages_EmptyParent(t *testing.T) {
	result := BuildForkMessages(nil, "start fresh")

	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}
	if result[0].TextContent() != "start fresh" {
		t.Fatalf("expected directive, got: %s", result[0].TextContent())
	}
}

func TestBuildForkMessages_MultipleToolUses(t *testing.T) {
	assistantMsg := message.Message{Role: message.RoleAssistant}
	assistantMsg.AddToolUse("tu-1", "bash", map[string]any{"cmd": "ls"})
	assistantMsg.AddToolUse("tu-2", "agent", map[string]any{"prompt": "sub"})

	parentMsgs := []message.Message{assistantMsg}
	result := BuildForkMessages(parentMsgs, "do work")

	if len(result) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(result))
	}
	tr := result[1].ToolResults()
	if len(tr) != 2 {
		t.Fatalf("expected 2 synthetic tool_results, got %d", len(tr))
	}
	if tr[0].ToolUseID != "tu-1" || tr[1].ToolUseID != "tu-2" {
		t.Fatalf("unexpected tool_result order: %+v", tr)
	}
}

func TestBuildForkMessages_ToolInputDeepCopy(t *testing.T) {
	assistantMsg := message.Message{Role: message.RoleAssistant}
	originalInput := map[string]any{"key": "value"}
	assistantMsg.AddToolUse("tu-1", "test", originalInput)

	parentMsgs := []message.Message{assistantMsg}
	result := BuildForkMessages(parentMsgs, "test")

	// Modify original input.
	originalInput["key"] = "modified"

	// Result should still have the original value.
	toolUses := result[0].ToolUses()
	if toolUses[0].Input["key"] != "value" {
		t.Fatal("expected deep copy of tool_use input")
	}
}
