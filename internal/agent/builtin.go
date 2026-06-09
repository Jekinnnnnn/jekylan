package agent

import _ "embed"

//go:embed prompts/general-purpose.md
var generalPurposePrompt string

//go:embed prompts/worker.md
var workerPrompt string

// NewBuiltinRegistry returns a registry pre-loaded with all built-in agents.
func NewBuiltinRegistry() *Registry {
	r := NewRegistry()
	r.Register(&Definition{
		Name:         "general-purpose",
		Description:  "A general-purpose agent capable of any task.",
		SystemPrompt: generalPurposePrompt,
		ToolsAllow:   []string{"bash", "file_write", "file_read", "file_edit"},
	})
	r.Register(&Definition{
		Name:         "worker",
		Description:  "Autonomous worker that executes research, implementation, or verification tasks.",
		SystemPrompt: workerPrompt,
		ToolsAllow:   []string{"*"},
		MaxTurns:     200,
	})
	return r
}
