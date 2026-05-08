package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Provider represents the LLM API provider.
type Provider string

const (
	ProviderAnthropic Provider = "anthropic"
	ProviderOpenAI    Provider = "openai"
)

// Config is the top-level YAML configuration.
type Config struct {
	Provider        Provider `yaml:"provider"`
	Model           string   `yaml:"model"`
	BaseURL         string   `yaml:"base_url,omitempty"`
	MaxTurns        int      `yaml:"max_turns"`
	ThinkingBudget  int64    `yaml:"thinking_budget,omitempty"`
	StreamMaxTokens int      `yaml:"stream_max_tokens,omitempty"`
	SystemPrompt    string   `yaml:"system_prompt"`
	Tools           []string `yaml:"tools"`
	DisableCompact  bool     `yaml:"disable_compact,omitempty"`

	// Compact options (previously environment variables)
	TimeBasedMCGap                float64 `yaml:"time_based_mc_gap,omitempty"`
	TimeBasedMCKeep               int     `yaml:"time_based_mc_keep,omitempty"`
	DisableTimeBasedMC            bool    `yaml:"disable_time_based_mc,omitempty"`
	UseAPIClearToolResults        bool    `yaml:"use_api_clear_tool_results,omitempty"`
	UseAPIClearToolUses           bool    `yaml:"use_api_clear_tool_uses,omitempty"`
	APIMaxInputTokens             int     `yaml:"api_max_input_tokens,omitempty"`
	APITargetInputTokens          int     `yaml:"api_target_input_tokens,omitempty"`
	AutoCompactWindow             int     `yaml:"auto_compact_window,omitempty"`
	AutoCompactPctOverride        float64 `yaml:"auto_compact_pct_override,omitempty"`
	BlockingLimitOverride         int     `yaml:"blocking_limit_override,omitempty"`
	DisableAutoCompact            bool    `yaml:"disable_auto_compact,omitempty"`
	SkillsDir                     string  `yaml:"skills_dir,omitempty"`
	SMCompactMinTokens            int     `yaml:"sm_compact_min_tokens,omitempty"`
	SMCompactMinTextBlockMessages int     `yaml:"sm_compact_min_text_block_messages,omitempty"`
	SMCompactMaxTokens            int     `yaml:"sm_compact_max_tokens,omitempty"`
	EnableSMCompact               bool    `yaml:"enable_sm_compact,omitempty"`

	// Agent tool (reserved for future implementation)
	AgentEnabled bool   `yaml:"agent_enabled,omitempty"`
	AgentsDir    string `yaml:"agents_dir,omitempty"`

	// Token budget (reserved for future implementation)
	// Supports human-readable formats like "500K", "1M", "2.5M".
	TokenBudget string `yaml:"token_budget,omitempty"`

	// Memory system (reserved for future implementation)
	EnableMemory bool   `yaml:"enable_memory,omitempty"`
	MemoryDir    string `yaml:"memory_dir,omitempty"`

	// Session persistence
	SessionFile string `yaml:"session_file,omitempty"`
}

// Load reads the YAML configuration from the given path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config file: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// Validate checks required fields.
func (c *Config) Validate() error {
	if c.Provider != ProviderAnthropic && c.Provider != ProviderOpenAI {
		return fmt.Errorf("provider must be 'anthropic' or 'openai'")
	}
	if c.Model == "" {
		return fmt.Errorf("model is required")
	}
	return nil
}
