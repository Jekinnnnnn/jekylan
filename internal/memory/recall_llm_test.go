package memory

import (
	"context"
	"fmt"
	"testing"

	"github.com/Jekinnnnnn/jekylan/internal/llm"
	"github.com/Jekinnnnnn/jekylan/internal/message"
	"github.com/Jekinnnnnn/jekylan/internal/tool"
)

// mockLLMClient is a test double for llm.Client.
type mockLLMClient struct {
	response   string
	err        error
	lastSystem string
	lastMsgs   []message.Message
}

func (m *mockLLMClient) StreamMessages(ctx context.Context, msgs []message.Message, systemPrompt string, tools *tool.Registry, thinkingBudget int64, cacheBreakpoints int) (<-chan llm.StreamEvent, error) {
	m.lastSystem = systemPrompt
	m.lastMsgs = msgs

	ch := make(chan llm.StreamEvent, 1)
	if m.err != nil {
		close(ch)
		return ch, m.err
	}
	ch <- llm.StreamEvent{Type: "assistant_text", TextDelta: m.response}
	close(ch)
	return ch, nil
}

func (m *mockLLMClient) lastUserContent() string {
	if len(m.lastMsgs) == 0 {
		return ""
	}
	for _, block := range m.lastMsgs[0].Content {
		if tb, ok := block.(message.TextBlock); ok {
			return tb.Text
		}
	}
	return ""
}

func TestLLMSelector(t *testing.T) {
	ctx := context.Background()
	memories := []MemoryHeader{
		{Filename: "go.md", FilePath: "/mem/go.md", MtimeMs: 1000, Description: "Go expertise", Type: MemoryTypeUser},
		{Filename: "react.md", FilePath: "/mem/react.md", MtimeMs: 2000, Description: "React tips", Type: MemoryTypeReference},
		{Filename: "python.md", FilePath: "/mem/python.md", MtimeMs: 3000, Description: "Python notes", Type: MemoryTypeProject},
	}

	mock := &mockLLMClient{
		response: `{"selected_memories":["go.md","python.md"]}`,
	}
	selector := NewLLMSelector(mock)

	result := selector.Select(ctx, "Help with Go concurrency", memories, nil)
	if len(result) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result))
	}
	if result[0].Path != "/mem/go.md" {
		t.Errorf("expected go.md first, got %s", result[0].Path)
	}
	if result[1].Path != "/mem/python.md" {
		t.Errorf("expected python.md second, got %s", result[1].Path)
	}

	// Verify the prompt contains the query and manifest
	userContent := mock.lastUserContent()
	if !contains(userContent, "Help with Go concurrency") {
		t.Error("expected user prompt to contain the query")
	}
	if !contains(userContent, "Go expertise") {
		t.Error("expected user prompt to contain memory descriptions")
	}
	// Verify system prompt was passed
	if !contains(mock.lastSystem, "selecting memories") {
		t.Error("expected system prompt to be passed")
	}
}

func TestLLMSelectorWithRecentTools(t *testing.T) {
	ctx := context.Background()
	memories := []MemoryHeader{
		{Filename: "grep.md", FilePath: "/mem/grep.md", MtimeMs: 1000, Description: "Grep tool usage", Type: MemoryTypeReference},
	}

	mock := &mockLLMClient{
		response: `{"selected_memories":[]}`,
	}
	selector := NewLLMSelector(mock)
	selector.SetRecentTools([]string{"grep"})

	selector.Select(ctx, "Search files", memories, nil)

	// Verify recent tools section is included in the prompt
	userContent := mock.lastUserContent()
	if !contains(userContent, "Recently used tools: grep") {
		t.Error("expected user prompt to contain recent tools section")
	}
}

func TestLLMSelectorAlreadySurfaced(t *testing.T) {
	ctx := context.Background()
	memories := []MemoryHeader{
		{Filename: "go.md", FilePath: "/mem/go.md", MtimeMs: 1000, Description: "Go expertise", Type: MemoryTypeUser},
		{Filename: "react.md", FilePath: "/mem/react.md", MtimeMs: 2000, Description: "React tips", Type: MemoryTypeReference},
	}

	mock := &mockLLMClient{
		response: `{"selected_memories":["go.md","react.md"]}`,
	}
	selector := NewLLMSelector(mock)

	surfaced := map[string]bool{"/mem/go.md": true}
	result := selector.Select(ctx, "anything", memories, surfaced)

	// go.md should be filtered out before reaching the LLM
	for _, r := range result {
		if r.Path == "/mem/go.md" {
			t.Error("expected already surfaced file to be excluded")
		}
	}
}

func TestLLMSelectorInvalidJSON(t *testing.T) {
	ctx := context.Background()
	memories := []MemoryHeader{
		{Filename: "go.md", FilePath: "/mem/go.md", MtimeMs: 1000, Description: "Go expertise", Type: MemoryTypeUser},
	}

	mock := &mockLLMClient{
		response: `not valid json`,
	}
	selector := NewLLMSelector(mock)

	result := selector.Select(ctx, "anything", memories, nil)
	if len(result) != 0 {
		t.Errorf("expected 0 results for invalid JSON, got %d", len(result))
	}
}

func TestLLMSelectorStreamError(t *testing.T) {
	ctx := context.Background()
	memories := []MemoryHeader{
		{Filename: "go.md", FilePath: "/mem/go.md", MtimeMs: 1000, Description: "Go expertise", Type: MemoryTypeUser},
	}

	mock := &mockLLMClient{
		err: fmt.Errorf("network error"),
	}
	selector := NewLLMSelector(mock)

	result := selector.Select(ctx, "anything", memories, nil)
	if len(result) != 0 {
		t.Errorf("expected 0 results on error, got %d", len(result))
	}
}

func TestLLMSelectorMaxMemories(t *testing.T) {
	ctx := context.Background()
	memories := []MemoryHeader{
		{Filename: "a.md", FilePath: "/mem/a.md", MtimeMs: 1000, Description: "A", Type: MemoryTypeUser},
		{Filename: "b.md", FilePath: "/mem/b.md", MtimeMs: 2000, Description: "B", Type: MemoryTypeUser},
		{Filename: "c.md", FilePath: "/mem/c.md", MtimeMs: 3000, Description: "C", Type: MemoryTypeUser},
		{Filename: "d.md", FilePath: "/mem/d.md", MtimeMs: 4000, Description: "D", Type: MemoryTypeUser},
		{Filename: "e.md", FilePath: "/mem/e.md", MtimeMs: 5000, Description: "E", Type: MemoryTypeUser},
		{Filename: "f.md", FilePath: "/mem/f.md", MtimeMs: 6000, Description: "F", Type: MemoryTypeUser},
	}

	mock := &mockLLMClient{
		response: `{"selected_memories":["a.md","b.md","c.md","d.md","e.md","f.md"]}`,
	}
	selector := NewLLMSelector(mock)
	selector.maxMemories = 3

	result := selector.Select(ctx, "anything", memories, nil)
	if len(result) != 3 {
		t.Errorf("expected max 3 results, got %d", len(result))
	}
}

func TestLLMSelectorIgnoresUnknownFilenames(t *testing.T) {
	ctx := context.Background()
	memories := []MemoryHeader{
		{Filename: "go.md", FilePath: "/mem/go.md", MtimeMs: 1000, Description: "Go expertise", Type: MemoryTypeUser},
	}

	mock := &mockLLMClient{
		response: `{"selected_memories":["go.md","nonexistent.md"]}`,
	}
	selector := NewLLMSelector(mock)

	result := selector.Select(ctx, "anything", memories, nil)
	if len(result) != 1 {
		t.Fatalf("expected 1 valid result, got %d", len(result))
	}
	if result[0].Path != "/mem/go.md" {
		t.Errorf("expected go.md, got %s", result[0].Path)
	}
}

func TestLLMSelectorSystemPrompt(t *testing.T) {
	ctx := context.Background()
	mock := &mockLLMClient{}
	selector := NewLLMSelector(mock)

	memories := []MemoryHeader{
		{Filename: "go.md", FilePath: "/mem/go.md", MtimeMs: 1000, Description: "Go", Type: MemoryTypeUser},
	}
	selector.Select(ctx, "test", memories, nil)

	userContent := mock.lastUserContent()
	if !contains(userContent, "Query:") {
		t.Error("expected user prompt to contain 'Query:' prefix")
	}
}
