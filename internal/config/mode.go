package config

// ModeConfig defines the configuration for a runtime mode.
type ModeConfig struct {
	Name         string
	SystemPrompt string // empty means use cfg.SystemPrompt
	MaxTurns     int    // 0 means use cfg.MaxTurns
}

// BuiltinModes returns the built-in mode configurations.
// SystemPrompt fields are left empty here to avoid import cycles;
// callers in main.go should populate them with agent package prompts.
func BuiltinModes() map[string]ModeConfig {
	return map[string]ModeConfig{
		"default": {
			Name: "default",
		},
		"coordinator": {
			Name:     "coordinator",
			MaxTurns: 10,
		},
		"playbook": {
			Name:     "playbook",
			MaxTurns: 10,
		},
	}
}
