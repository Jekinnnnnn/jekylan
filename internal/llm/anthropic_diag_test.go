package llm

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/Jekinnnnnn/jekylan/internal/message"
	sdk "github.com/anthropics/anthropic-sdk-go"
)

func TestAnthropicMessageParamSerialization(t *testing.T) {
	user1 := message.Message{Role: message.RoleUser}
	user1.AddText("hello")

	assistant1 := message.Message{Role: message.RoleAssistant}
	assistant1.AddText("Hello! How can I help you today?")

	user2 := message.Message{Role: message.RoleUser}
	user2.AddText("what's the weather")

	msgs := []message.Message{user1, assistant1, user2}

	params := sdk.MessageNewParams{
		MaxTokens: 8196,
		Model:     "glm5.1",
	}

	for _, m := range msgs {
		params.Messages = append(params.Messages, m.ToAnthropicMessage())
	}

	b, err := json.MarshalIndent(params, "", "  ")
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	fmt.Printf("Request JSON:\n%s\n", string(b))
}
