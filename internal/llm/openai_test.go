package llm

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/Jekinnnnnn/jekylan/internal/message"
	oai "github.com/openai/openai-go"
)

func TestOpenAITwoTurnWithTool(t *testing.T) {
	// Simulate a realistic two-turn conversation with tool use
	user1 := message.Message{Role: message.RoleUser}
	user1.AddText("what's the weather")

	// Assistant: text + tool_call
	assistant1 := message.Message{Role: message.RoleAssistant}
	assistant1.AddText("Let me check the weather for you.")
	assistant1.AddToolUse("call_abc123", "bash", map[string]any{"command": "curl -s wttr.in"})

	// User: tool result + second turn prompt (merged)
	user2 := message.Message{Role: message.RoleUser}
	user2.AddToolResult("call_abc123", "Sunny 25°C", false)
	user2.AddText("what's the weather again")

	msgs := []message.Message{user1, assistant1, user2}

	params := oai.ChatCompletionNewParams{
		Model: "step-3.5-flash",
	}

	params.Messages = append(params.Messages, oai.SystemMessage("You are a helpful assistant."))

	for _, m := range msgs {
		params.Messages = append(params.Messages, m.ToOpenAIMessages()...)
	}

	b, _ := json.MarshalIndent(params, "", "  ")
	fmt.Printf("=== CURRENT (text + tool_calls in same assistant msg) ===\n%s\n\n", string(b))
	_ = os.WriteFile("/tmp/gocc_test_request_current.json", b, 0644)

	// Now test with text and tool_calls split into separate messages
	params2 := oai.ChatCompletionNewParams{
		Model: "step-3.5-flash",
	}
	params2.Messages = append(params2.Messages, oai.SystemMessage("You are a helpful assistant."))
	params2.Messages = append(params2.Messages, oai.UserMessage("what's the weather"))
	// Split: text first, then tool_calls
	var textOnlyAssistant oai.ChatCompletionAssistantMessageParam
	textOnlyAssistant.Content.OfString = oai.String("Let me check the weather for you.")
	params2.Messages = append(params2.Messages, oai.ChatCompletionMessageParamUnion{
		OfAssistant: &textOnlyAssistant,
	})
	var toolOnlyAssistant oai.ChatCompletionAssistantMessageParam
	toolOnlyAssistant.ToolCalls = []oai.ChatCompletionMessageToolCallParam{{
		ID:   "call_abc123",
		Type: "function",
		Function: oai.ChatCompletionMessageToolCallFunctionParam{
			Name:      "bash",
			Arguments: `{"command":"curl -s wttr.in"}`,
		},
	}}
	params2.Messages = append(params2.Messages, oai.ChatCompletionMessageParamUnion{
		OfAssistant: &toolOnlyAssistant,
	})
	params2.Messages = append(params2.Messages, oai.ToolMessage("Sunny 25°C", "call_abc123"))
	params2.Messages = append(params2.Messages, oai.UserMessage("what's the weather again"))

	b2, _ := json.MarshalIndent(params2, "", "  ")
	fmt.Printf("=== ALTERNATIVE (text and tool_calls in separate assistant msgs) ===\n%s\n", string(b2))
	_ = os.WriteFile("/tmp/gocc_test_request_alt.json", b2, 0644)
}
