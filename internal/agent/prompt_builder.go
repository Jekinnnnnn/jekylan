package agent

import (
	"fmt"
	"strings"

	"github.com/Jekinnnnnn/jekylan/internal/tool"
)

// buildAgentSystemPrompt constructs the system prompt for a sub-agent run.
func buildAgentSystemPrompt(def *Definition, tools *tool.Registry, injectPause bool) string {
	var b strings.Builder
	if def != nil && def.SystemPrompt != "" {
		b.WriteString(def.SystemPrompt)
		b.WriteString("\n\n")
	}
	if injectPause {
		b.WriteString(pauseAndSummarizePrompt)
		b.WriteString("\n\n")
	}
	if tools != nil {
		all := tools.All()
		if len(all) > 0 {
			b.WriteString("Available tools:\n")
			for _, t := range all {
				fmt.Fprintf(&b, "- %s: %s\n", t.Name(), t.Description())
			}
		}
	}
	return strings.TrimSpace(b.String())
}