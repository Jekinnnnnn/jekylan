package compact

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
	AutoCompactWindow      int
	AutoCompactPctOverride float64
	BlockingLimitOverride  int
	DisableCompact         bool
	DisableAutoCompact     bool

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

var globalOpts = defaultOptions

// SetOptions sets the global compact options.
func SetOptions(opts Options) {
	globalOpts = opts
}

// GetOptions returns a copy of the current global options.
func GetOptions() Options {
	return globalOpts
}
