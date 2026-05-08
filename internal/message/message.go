package message

import (
	"encoding/json"
	"strings"
	"time"

	sdk "github.com/anthropics/anthropic-sdk-go"
	oai "github.com/openai/openai-go"
)

// Role represents the message role in a conversation.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleSystem    Role = "system"
)

// ContentBlock is a discriminated union of all possible content blocks.
// It is kept as an empty interface so the package can hold both Anthropic
// and OpenAI conversions without favouring one SDK in the interface.
type ContentBlock any

// TextBlock is a plain text content block.
type TextBlock struct {
	Text string
}

// ToAnthropicBlock converts to Anthropic SDK param.
func (t TextBlock) ToAnthropicBlock() sdk.ContentBlockParamUnion {
	return sdk.NewTextBlock(t.Text)
}

// ToolUseBlock represents a tool invocation from the assistant.
type ToolUseBlock struct {
	ID    string
	Name  string
	Input map[string]any
}

// ToAnthropicBlock converts to Anthropic SDK param.
func (t ToolUseBlock) ToAnthropicBlock() sdk.ContentBlockParamUnion {
	return sdk.ContentBlockParamUnion{
		OfToolUse: &sdk.ToolUseBlockParam{
			ID:    t.ID,
			Name:  t.Name,
			Input: t.Input,
		},
	}
}

// ToolResultBlock represents the result of a tool execution.
type ToolResultBlock struct {
	ToolUseID string
	Content   string
	IsError   bool
}

// ToAnthropicBlock converts to Anthropic SDK param.
func (t ToolResultBlock) ToAnthropicBlock() sdk.ContentBlockParamUnion {
	return sdk.NewToolResultBlock(t.ToolUseID, t.Content, t.IsError)
}

// ThinkingBlock represents the assistant's thinking process.
type ThinkingBlock struct {
	Thinking  string
	Signature string
}

// ToAnthropicBlock converts to Anthropic SDK param.
func (t ThinkingBlock) ToAnthropicBlock() sdk.ContentBlockParamUnion {
	return sdk.NewThinkingBlock(t.Signature, t.Thinking)
}

// RedactedThinkingBlock represents a redacted thinking block.
type RedactedThinkingBlock struct {
	Data string
}

// ToAnthropicBlock converts to Anthropic SDK param.
func (t RedactedThinkingBlock) ToAnthropicBlock() sdk.ContentBlockParamUnion {
	return sdk.NewRedactedThinkingBlock(t.Data)
}

// Usage records token consumption from an API response.
type Usage struct {
	InputTokens              int64
	OutputTokens             int64
	CacheCreationInputTokens int64
	CacheReadInputTokens     int64
}

// Message is a single turn in the conversation.
type Message struct {
	Role       Role
	Content    []ContentBlock
	Timestamp  time.Time
	Usage        *Usage
	ResponseID   string
	APIError     string // non-empty when this assistant message represents an API error
	ErrorDetails string // raw error details for API error messages
}

// ToAnthropicMessage converts the message to an Anthropic SDK MessageParam.
func (m Message) ToAnthropicMessage() sdk.MessageParam {
	blocks := make([]sdk.ContentBlockParamUnion, len(m.Content))
	for i, c := range m.Content {
		switch b := c.(type) {
		case TextBlock:
			blocks[i] = b.ToAnthropicBlock()
		case ToolUseBlock:
			blocks[i] = b.ToAnthropicBlock()
		case ToolResultBlock:
			blocks[i] = b.ToAnthropicBlock()
		case ThinkingBlock:
			blocks[i] = b.ToAnthropicBlock()
		case RedactedThinkingBlock:
			blocks[i] = b.ToAnthropicBlock()
		}
	}
	switch m.Role {
	case RoleAssistant:
		return sdk.NewAssistantMessage(blocks...)
	default:
		return sdk.NewUserMessage(blocks...)
	}
}

// ToOpenAIMessages converts the message to one or more OpenAI SDK
// ChatCompletionMessageParamUnion values. Assistant messages always produce
// exactly one message. User messages may produce multiple messages when they
// contain both text and tool results (OpenAI requires tool results to be
// separate "tool" role messages).
func (m Message) ToOpenAIMessages() []oai.ChatCompletionMessageParamUnion {
	switch m.Role {
	case RoleAssistant:
		var assistant oai.ChatCompletionAssistantMessageParam
		var toolCalls []oai.ChatCompletionMessageToolCallParam
		var textBuf strings.Builder
		for _, block := range m.Content {
			switch b := block.(type) {
			case TextBlock:
				textBuf.WriteString(b.Text)
			case ToolUseBlock:
				inputJSON, _ := json.Marshal(b.Input)
				toolCalls = append(toolCalls, oai.ChatCompletionMessageToolCallParam{
					ID:   b.ID,
					Type: "function",
					Function: oai.ChatCompletionMessageToolCallFunctionParam{
						Name:      b.Name,
						Arguments: string(inputJSON),
					},
				})
			}
		}
		assistant.Content.OfString = oai.String(textBuf.String())
		if len(toolCalls) > 0 {
			assistant.ToolCalls = toolCalls
		}
		return []oai.ChatCompletionMessageParamUnion{{OfAssistant: &assistant}}
	case RoleUser:
		var content strings.Builder
		var out []oai.ChatCompletionMessageParamUnion
		for _, block := range m.Content {
			switch b := block.(type) {
			case TextBlock:
				content.WriteString(b.Text)
			case ToolResultBlock:
				out = append(out, oai.ToolMessage(b.Content, b.ToolUseID))
			}
		}
		if content.Len() > 0 {
			out = append(out, oai.UserMessage(content.String()))
		}
		return out
	default:
		return []oai.ChatCompletionMessageParamUnion{oai.UserMessage("")}
	}
}

// ToOpenAIMessage converts the message to a single OpenAI SDK message.
// Deprecated: use ToOpenAIMessages for correct handling of user messages that
// contain multiple tool results.
func (m Message) ToOpenAIMessage() oai.ChatCompletionMessageParamUnion {
	msgs := m.ToOpenAIMessages()
	if len(msgs) > 0 {
		return msgs[0]
	}
	return oai.UserMessage("")
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

// humanTimeFormat is the user-friendly timestamp layout used in session JSON.
const humanTimeFormat = "2006-01-02 15:04:05"

// MarshalJSON implements custom JSON serialization for Message.
// Content blocks are tagged with "_type" to enable round-trip deserialization.
func (m Message) MarshalJSON() ([]byte, error) {
	type rawUsage Usage
	type rawMsg struct {
		Role         string          `json:"role"`
		Content      []map[string]any `json:"content"`
		Timestamp    string           `json:"timestamp"`
		Usage        *rawUsage       `json:"usage,omitempty"`
		ResponseID   string          `json:"response_id,omitempty"`
		APIError     string          `json:"api_error,omitempty"`
		ErrorDetails string          `json:"error_details,omitempty"`
	}

	rm := rawMsg{
		Role:         string(m.Role),
		Timestamp:    m.Timestamp.Format(humanTimeFormat),
		ResponseID:   m.ResponseID,
		APIError:     m.APIError,
		ErrorDetails: m.ErrorDetails,
	}
	if m.Usage != nil {
		u := rawUsage(*m.Usage)
		rm.Usage = &u
	}

	rm.Content = make([]map[string]any, len(m.Content))
	for i, block := range m.Content {
		switch b := block.(type) {
		case TextBlock:
			rm.Content[i] = map[string]any{"_type": "text", "text": b.Text}
		case ToolUseBlock:
			rm.Content[i] = map[string]any{"_type": "tool_use", "id": b.ID, "name": b.Name, "input": b.Input}
		case ToolResultBlock:
			rm.Content[i] = map[string]any{"_type": "tool_result", "tool_use_id": b.ToolUseID, "content": b.Content, "is_error": b.IsError}
		case ThinkingBlock:
			rm.Content[i] = map[string]any{"_type": "thinking", "thinking": b.Thinking, "signature": b.Signature}
		case RedactedThinkingBlock:
			rm.Content[i] = map[string]any{"_type": "redacted_thinking", "data": b.Data}
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
		Role         string           `json:"role"`
		Content      []map[string]any `json:"content"`
		Timestamp    string           `json:"timestamp"`
		Usage        *rawUsage        `json:"usage,omitempty"`
		ResponseID   string           `json:"response_id,omitempty"`
		APIError     string           `json:"api_error,omitempty"`
		ErrorDetails string           `json:"error_details,omitempty"`
	}

	var rm rawMsg
	if err := json.Unmarshal(data, &rm); err != nil {
		return err
	}

	m.Role = Role(rm.Role)
	m.Timestamp = parseHumanTime(rm.Timestamp)
	m.ResponseID = rm.ResponseID
	m.APIError = rm.APIError
	m.ErrorDetails = rm.ErrorDetails
	if rm.Usage != nil {
		u := Usage(*rm.Usage)
		m.Usage = &u
	}

	m.Content = make([]ContentBlock, len(rm.Content))
	for i, raw := range rm.Content {
		typ, _ := raw["_type"].(string)
		switch typ {
		case "text":
			m.Content[i] = TextBlock{Text: strVal(raw, "text")}
		case "tool_use":
			m.Content[i] = ToolUseBlock{
				ID:    strVal(raw, "id"),
				Name:  strVal(raw, "name"),
				Input: mapVal(raw, "input"),
			}
		case "tool_result":
			m.Content[i] = ToolResultBlock{
				ToolUseID: strVal(raw, "tool_use_id"),
				Content:   strVal(raw, "content"),
				IsError:   boolVal(raw, "is_error"),
			}
		case "thinking":
			m.Content[i] = ThinkingBlock{
				Thinking:  strVal(raw, "thinking"),
				Signature: strVal(raw, "signature"),
			}
		case "redacted_thinking":
			m.Content[i] = RedactedThinkingBlock{Data: strVal(raw, "data")}
		}
	}

	return nil
}

func strVal(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func boolVal(m map[string]any, key string) bool {
	if v, ok := m[key].(bool); ok {
		return v
	}
	return false
}

func mapVal(m map[string]any, key string) map[string]any {
	if v, ok := m[key].(map[string]any); ok {
		return v
	}
	return nil
}

// parseHumanTime parses a timestamp string in humanTimeFormat.
func parseHumanTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse(humanTimeFormat, s); err == nil {
		return t
	}
	return time.Time{}
}
