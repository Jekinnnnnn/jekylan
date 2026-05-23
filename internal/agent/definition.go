package agent

// Definition describes a sub-agent: its identity, capabilities, and constraints.
type Definition struct {
	Name           string
	Description    string
	SystemPrompt   string
	ToolsAllow     []string // ["*"] means all tools
	ToolsDeny      []string
	Model          string
	MaxTurns       int
	Effort         string
	ResultConsumer ResultConsumer // nil uses DefaultResultConsumer
}

// EffectiveMaxTurns returns the configured max turns, or a sensible default.
func (d *Definition) EffectiveMaxTurns() int {
	if d.MaxTurns > 0 {
		return d.MaxTurns
	}
	return 3
}
