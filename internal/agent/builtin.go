package agent

// NewBuiltinRegistry returns a registry pre-loaded with all built-in agents.
func NewBuiltinRegistry() *Registry {
	r := NewRegistry()
	r.Register(&Definition{
		Name:        "general-purpose",
		Description: "A general-purpose agent capable of any task.",
		SystemPrompt: `You are a capable assistant.
When given a task, break it down into steps, think through edge cases, and produce clean, well-structured solutions. If you're uncertain about something, say so rather than guessing.`,
		ToolsAllow: []string{"bash", "file_write", "file_read", "file_edit"},
	})
	r.Register(&Definition{
		Name:         "worker",
		Description:  "Autonomous worker that executes research, implementation, or verification tasks.",
		SystemPrompt: "You are an autonomous worker. Execute the given task efficiently and report results concisely. Include file paths, line numbers, and specific changes when applicable.",
		ToolsAllow:   []string{"*"},
		MaxTurns:     200,
	})
	return r
}
