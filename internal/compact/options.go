package compact

import "sync"

// Options holds all compact-related configuration that was previously
// read from environment variables.
type Options struct {
	// Microcompact
	TimeBasedMCGap     float64
	TimeBasedMCKeep    int
	DisableTimeBasedMC bool

	// API Context Management
	UserType               string
	UseAPIClearToolResults bool
	UseAPIClearToolUses    bool
	APIMaxInputTokens      int
	APITargetInputTokens   int

	// Session Memory
	SessionMemoryPath string

	// Auto Compact
	AutoCompactWindow             int
	AutoCompactPctOverride        float64
	AutoCompactThresholdOverride  int
	BlockingLimitOverride         int
	DisableCompact                bool
	DisableAutoCompact            bool

	// Session Memory Compact
	SMCompactMinTokens            int
	SMCompactMinTextBlockMessages int
	SMCompactMaxTokens            int
	EnableSMCompact               bool
}

var defaultOptions = Options{
	TimeBasedMCGap:     10.0,
	TimeBasedMCKeep:    3,
	DisableTimeBasedMC: false,

	APIMaxInputTokens:    180_000,
	APITargetInputTokens: 40_000,

	SMCompactMinTokens:            10_000,
	SMCompactMinTextBlockMessages: 5,
	SMCompactMaxTokens:            40_000,
}

var (
	globalOpts   = defaultOptions
	globalOptsMu sync.RWMutex
)

// SetOptions sets the global compact options.
func SetOptions(opts Options) {
	globalOptsMu.Lock()
	globalOpts = opts
	globalOptsMu.Unlock()
	// Synchronize session-memory compact config from global options.
	syncSMCompactConfigFromOptions(opts)
}

// GetOptions returns a copy of the current global options.
func GetOptions() Options {
	globalOptsMu.RLock()
	defer globalOptsMu.RUnlock()
	return globalOpts
}

// syncSMCompactConfigFromOptions updates the session-memory compact config
// from global options so they stay in sync.
func syncSMCompactConfigFromOptions(opts Options) {
	cfg := defaultSMCompactConfig
	if opts.SMCompactMinTokens > 0 {
		cfg.MinTokens = opts.SMCompactMinTokens
	}
	if opts.SMCompactMinTextBlockMessages > 0 {
		cfg.MinTextBlockMessages = opts.SMCompactMinTextBlockMessages
	}
	if opts.SMCompactMaxTokens > 0 {
		cfg.MaxTokens = opts.SMCompactMaxTokens
	}
	smCompactConfig = cfg
}
