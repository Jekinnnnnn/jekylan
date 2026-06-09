package message

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Role represents the message role in a conversation.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleSystem    Role = "system"
)

// ContentBlock is a discriminated union of all possible content block types.
type ContentBlock interface {
	Kind() string
}

const (
	KindText             = "text"
	KindToolUse          = "tool_use"
	KindToolResult       = "tool_result"
	KindThinking         = "thinking"
	KindRedactedThinking = "redacted_thinking"
)

// TextBlock is a plain text content block.
type TextBlock struct {
	Text string
}

func (TextBlock) Kind() string { return KindText }

// ToolUseBlock represents a tool invocation from the assistant.
type ToolUseBlock struct {
	ID    string
	Name  string
	Input map[string]any
}

func (ToolUseBlock) Kind() string { return KindToolUse }

// ToolResultBlock represents the result of a tool execution.
type ToolResultBlock struct {
	ToolUseID string
	Content   string
	IsError   bool
}

func (ToolResultBlock) Kind() string { return KindToolResult }

// ThinkingBlock represents the assistant's thinking process.
type ThinkingBlock struct {
	Thinking  string
	Signature string
}

func (ThinkingBlock) Kind() string { return KindThinking }

// RedactedThinkingBlock represents a redacted thinking block.
type RedactedThinkingBlock struct {
	Data string
}

func (RedactedThinkingBlock) Kind() string { return KindRedactedThinking }

// Usage records token consumption from an API response.
type Usage struct {
	InputTokens              int64
	OutputTokens             int64
	CacheCreationInputTokens int64
	CacheReadInputTokens     int64
}

// String returns a compact representation of token usage, including cache
// statistics when present.
func (u Usage) String() string {
	s := fmt.Sprintf("input=%d output=%d", u.InputTokens, u.OutputTokens)
	if u.CacheCreationInputTokens > 0 || u.CacheReadInputTokens > 0 {
		s += fmt.Sprintf(" cache_create=%d cache_read=%d", u.CacheCreationInputTokens, u.CacheReadInputTokens)
	}
	return s
}

// TurnMetadata carries API-response metadata and control flags for a message.
type TurnMetadata struct {
	Usage           *Usage `json:"usage,omitempty"`
	ResponseID      string `json:"response_id,omitempty"`
	APIError        string `json:"api_error,omitempty"`
	ErrorDetails    string `json:"error_details,omitempty"`
	CacheBreakpoint bool   `json:"cache_breakpoint,omitempty"`
}

// Message is a single turn in the conversation.
type Message struct {
	Role      Role
	Content   []ContentBlock
	Timestamp time.Time
	TurnMetadata
}

// AddText appends a text block to the message.
func (m *Message) AddText(text string) {
	m.Content = append(m.Content, TextBlock{Text: text})
}

// AddToolUse appends a tool_use block.
func (m *Message) AddToolUse(id, name string, input map[string]any) {
	m.Content = append(m.Content, ToolUseBlock{ID: id, Name: name, Input: input})
}

// AddToolResult appends a tool_result block.
func (m *Message) AddToolResult(toolUseID, content string, isError bool) {
	m.Content = append(m.Content, ToolResultBlock{ToolUseID: toolUseID, Content: content, IsError: isError})
}

// AddThinking appends a thinking block.
func (m *Message) AddThinking(thinking, signature string) {
	m.Content = append(m.Content, ThinkingBlock{Thinking: thinking, Signature: signature})
}

// AddRedactedThinking appends a redacted_thinking block.
func (m *Message) AddRedactedThinking(data string) {
	m.Content = append(m.Content, RedactedThinkingBlock{Data: data})
}

// TextContent returns the concatenated text of all text blocks.
func (m Message) TextContent() string {
	var out strings.Builder
	for _, c := range m.Content {
		if t, ok := c.(TextBlock); ok {
			out.WriteString(t.Text)
		}
	}
	return out.String()
}

// ToolUses returns all tool_use blocks in the message.
func (m Message) ToolUses() []ToolUseBlock {
	var out []ToolUseBlock
	for _, c := range m.Content {
		if t, ok := c.(ToolUseBlock); ok {
			out = append(out, t)
		}
	}
	return out
}

// ToolResults returns all tool_result blocks in the message.
func (m Message) ToolResults() []ToolResultBlock {
	var out []ToolResultBlock
	for _, c := range m.Content {
		if t, ok := c.(ToolResultBlock); ok {
			out = append(out, t)
		}
	}
	return out
}

// ThinkingBlocks returns all thinking blocks in the message.
func (m Message) ThinkingBlocks() []ThinkingBlock {
	var out []ThinkingBlock
	for _, c := range m.Content {
		if t, ok := c.(ThinkingBlock); ok {
			out = append(out, t)
		}
	}
	return out
}

// RedactedThinkingBlocks returns all redacted_thinking blocks in the message.
func (m Message) RedactedThinkingBlocks() []RedactedThinkingBlock {
	var out []RedactedThinkingBlock
	for _, c := range m.Content {
		if t, ok := c.(RedactedThinkingBlock); ok {
			out = append(out, t)
		}
	}
	return out
}

// humanTimeFormat is the user-friendly timestamp layout used in session JSON.
const humanTimeFormat = "2006-01-02 15:04:05"

// MarshalJSON implements custom JSON serialization for Message.
// Content blocks are tagged with "_type" to enable round-trip deserialization.
func (m Message) MarshalJSON() ([]byte, error) {
	type rawUsage Usage
	type rawMsg struct {
		Role            string           `json:"role"`
		Content         []map[string]any `json:"content"`
		Timestamp       string           `json:"timestamp"`
		Usage           *rawUsage        `json:"usage,omitempty"`
		ResponseID      string           `json:"response_id,omitempty"`
		APIError        string           `json:"api_error,omitempty"`
		ErrorDetails    string           `json:"error_details,omitempty"`
		CacheBreakpoint bool             `json:"cache_breakpoint,omitempty"`
	}

	rm := rawMsg{
		Role:            string(m.Role),
		Timestamp:       m.Timestamp.Format(humanTimeFormat),
		ResponseID:      m.ResponseID,
		APIError:        m.APIError,
		ErrorDetails:    m.ErrorDetails,
		CacheBreakpoint: m.CacheBreakpoint,
	}
	if m.Usage != nil {
		u := rawUsage(*m.Usage)
		rm.Usage = &u
	}

	rm.Content = make([]map[string]any, len(m.Content))
	for i, block := range m.Content {
		switch b := block.(type) {
		case TextBlock:
			rm.Content[i] = map[string]any{"_type": b.Kind(), "text": b.Text}
		case ToolUseBlock:
			rm.Content[i] = map[string]any{"_type": b.Kind(), "id": b.ID, "name": b.Name, "input": b.Input}
		case ToolResultBlock:
			rm.Content[i] = map[string]any{"_type": b.Kind(), "tool_use_id": b.ToolUseID, "content": b.Content, "is_error": b.IsError}
		case ThinkingBlock:
			rm.Content[i] = map[string]any{"_type": b.Kind(), "thinking": b.Thinking, "signature": b.Signature}
		case RedactedThinkingBlock:
			rm.Content[i] = map[string]any{"_type": b.Kind(), "data": b.Data}
		default:
			rm.Content[i] = map[string]any{"_type": "unknown"}
		}
	}

	return json.Marshal(rm)
}

// UnmarshalJSON implements custom JSON deserialization for Message.
func (m *Message) UnmarshalJSON(data []byte) error {
	type rawUsage Usage
	type rawMsg struct {
		Role            string           `json:"role"`
		Content         []map[string]any `json:"content"`
		Timestamp       string           `json:"timestamp"`
		Usage           *rawUsage        `json:"usage,omitempty"`
		ResponseID      string           `json:"response_id,omitempty"`
		APIError        string           `json:"api_error,omitempty"`
		ErrorDetails    string           `json:"error_details,omitempty"`
		CacheBreakpoint bool             `json:"cache_breakpoint,omitempty"`
	}

	var rm rawMsg
	if err := json.Unmarshal(data, &rm); err != nil {
		return err
	}

	m.Role = Role(rm.Role)
	ts, err := parseHumanTime(rm.Timestamp)
	if err != nil {
		return err
	}
	m.Timestamp = ts
	m.ResponseID = rm.ResponseID
	m.APIError = rm.APIError
	m.ErrorDetails = rm.ErrorDetails
	m.CacheBreakpoint = rm.CacheBreakpoint
	if rm.Usage != nil {
		u := Usage(*rm.Usage)
		m.Usage = &u
	}

	m.Content = make([]ContentBlock, len(rm.Content))
	for i, raw := range rm.Content {
		typ, _ := raw["_type"].(string)
		switch typ {
		case KindText:
			text, ok := strVal(raw, "text")
			if !ok {
				return fmt.Errorf("text block: expected string for key %q, got %T", "text", raw["text"])
			}
			m.Content[i] = TextBlock{Text: text}
		case KindToolUse:
			id, ok1 := strVal(raw, "id")
			name, ok2 := strVal(raw, "name")
			input, _ := mapVal(raw, "input")
			if !ok1 {
				return fmt.Errorf("tool_use block: expected string for key %q, got %T", "id", raw["id"])
			}
			if !ok2 {
				return fmt.Errorf("tool_use block: expected string for key %q, got %T", "name", raw["name"])
			}
			m.Content[i] = ToolUseBlock{ID: id, Name: name, Input: input}
		case KindToolResult:
			toolUseID, ok1 := strVal(raw, "tool_use_id")
			content, ok2 := strVal(raw, "content")
			if !ok1 {
				return fmt.Errorf("tool_result block: expected string for key %q, got %T", "tool_use_id", raw["tool_use_id"])
			}
			if !ok2 {
				return fmt.Errorf("tool_result block: expected string for key %q, got %T", "content", raw["content"])
			}
			m.Content[i] = ToolResultBlock{
				ToolUseID: toolUseID,
				Content:   content,
				IsError:   boolVal(raw, "is_error"),
			}
		case KindThinking:
			thinking, ok1 := strVal(raw, "thinking")
			signature, ok2 := strVal(raw, "signature")
			if !ok1 {
				return fmt.Errorf("thinking block: expected string for key %q, got %T", "thinking", raw["thinking"])
			}
			if !ok2 {
				return fmt.Errorf("thinking block: expected string for key %q, got %T", "signature", raw["signature"])
			}
			m.Content[i] = ThinkingBlock{
				Thinking:  thinking,
				Signature: signature,
			}
		case KindRedactedThinking:
			data, ok := strVal(raw, "data")
			if !ok {
				return fmt.Errorf("redacted_thinking block: expected string for key %q, got %T", "data", raw["data"])
			}
			m.Content[i] = RedactedThinkingBlock{Data: data}
		default:
			return fmt.Errorf("unknown content block kind %q", typ)
		}
	}

	return nil
}

func strVal(m map[string]any, key string) (string, bool) {
	v, ok := m[key].(string)
	return v, ok
}

func boolVal(m map[string]any, key string) bool {
	if v, ok := m[key].(bool); ok {
		return v
	}
	return false
}

func mapVal(m map[string]any, key string) (map[string]any, bool) {
	v, ok := m[key].(map[string]any)
	return v, ok
}

// parseHumanTime parses a timestamp string in humanTimeFormat.
func parseHumanTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(humanTimeFormat, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid timestamp %q: %w", s, err)
	}
	return t, nil
}
