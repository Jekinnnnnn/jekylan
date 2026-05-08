package message

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestAnthropicMessageSerializationTwoTurns(t *testing.T) {
	user1 := Message{Role: RoleUser}
	user1.AddText("hello")

	assistant1 := Message{Role: RoleAssistant}
	assistant1.AddText("Hello! How can I help you today?")

	user2 := Message{Role: RoleUser}
	user2.AddText("what's the weather")

	msgs := []Message{user1, assistant1, user2}

	for i, m := range msgs {
		am := m.ToAnthropicMessage()
		b, _ := json.MarshalIndent(am, "", "  ")
		fmt.Printf("\nMessage %d (role=%s):\n%s\n", i, m.Role, string(b))
	}
}
