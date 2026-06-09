package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)


// SkillCollectorConfig defines collection rules for a single skill.
type SkillCollectorConfig struct {
	SkillName        string   `yaml:"skill"`
	Threshold        int      `yaml:"threshold"`
	TargetMemoryType string   `yaml:"target_memory_type"` // e.g. "feedback", "project"
	TargetMemoryName string   `yaml:"target_memory_name"` // filename without extension
	Keywords         []string `yaml:"keywords"`           // keywords for memory recall matching
	WorkflowMode     bool     `yaml:"workflow_mode"`      // true if this skill spans multiple SubmitMessage rounds
}

// SkillExecutionRecord captures the start/end message indices of a skill execution.
// Detailed session content (args, success, stop reason, etc.) is fetched from the
// session message slice using these indices when analysis runs.
type SkillExecutionRecord struct {
	StartMessageIndex int // index in the session's messages at skill invocation
	EndMessageIndex   int // index in the session's messages at execution end
	Completed         bool
}

// SkillCollector tracks skill execution boundaries and triggers analysis.
// It does not persist records locally; all detailed session content is retrieved
// from the engine's message slice when needed.
type SkillCollector struct {
	mu             sync.Mutex
	configs        map[string]*SkillCollectorConfig
	pending        []string                          // ordered list of skill names invoked in current SubmitMessage
	activeWorkflow string                            // skill name of the ongoing workflow (if WorkflowMode)
	executions     map[string][]SkillExecutionRecord // skill name -> execution markers
	memoryDir      string
}

// NewSkillCollector creates a collector.
func NewSkillCollector(memoryDir string) *SkillCollector {
	return &SkillCollector{
		configs:    make(map[string]*SkillCollectorConfig),
		executions: make(map[string][]SkillExecutionRecord),
		memoryDir:  memoryDir,
	}
}

// LoadFromMemoryDir scans <memoryDir>/skill-collectors/*.yaml for collection rules.
func (sc *SkillCollector) LoadFromMemoryDir() {
	if sc.memoryDir == "" {
		return
	}
	dir := filepath.Join(sc.memoryDir, "skill-collectors")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "[skill-collector] failed to read dir %s: %v\n", dir, err)
		}
		return
	}

	sc.mu.Lock()
	defer sc.mu.Unlock()

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[skill-collector] failed to read %s: %v\n", path, err)
			continue
		}
		var cfg SkillCollectorConfig
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			fmt.Fprintf(os.Stderr, "[skill-collector] failed to parse %s: %v\n", path, err)
			continue
		}
		if cfg.SkillName == "" || cfg.Threshold <= 0 {
			fmt.Fprintf(os.Stderr, "[skill-collector] skipping %s: missing skill or invalid threshold\n", path)
			continue
		}
		sc.configs[cfg.SkillName] = &cfg
		fmt.Fprintf(os.Stderr, "[skill-collector] loaded rule for skill %q (threshold=%d)\n", cfg.SkillName, cfg.Threshold)
	}
}

// OnSkillInvocation should be called when the skill tool is invoked.
// startMsgIndex is len(e.messages) at the moment of invocation, used to isolate
// this execution's conversation slice later.
func (sc *SkillCollector) OnSkillInvocation(skillName string, startMsgIndex int) {
	cfg, ok := sc.configs[skillName]
	if !ok {
		return
	}

	sc.mu.Lock()
	defer sc.mu.Unlock()

	// If there's an incomplete record for this skill, the user is starting fresh.
	// Clear the stale incomplete record and active workflow state.
	execs := sc.executions[skillName]
	if len(execs) > 0 {
		last := execs[len(execs)-1]
		if !last.Completed {
			sc.executions[skillName] = nil
			fmt.Fprintf(os.Stderr, "[skill-collector] cleared incomplete record for %s (new invocation)\n", skillName)
		}
	}
	if sc.activeWorkflow == skillName {
		sc.activeWorkflow = ""
	}

	sc.pending = append(sc.pending, skillName)
	sc.executions[skillName] = append(sc.executions[skillName], SkillExecutionRecord{
		StartMessageIndex: startMsgIndex,
	})
	if cfg.WorkflowMode {
		sc.activeWorkflow = skillName
	}
}

// OnExecutionResult should be called when the current SubmitMessage round ends.
// It fills the end index into every pending record in invocation order. For workflow
// skills it accumulates across multiple SubmitMessages until workflowEnded is true.
// Returns the list of completed skill names.
func (sc *SkillCollector) OnExecutionResult(endMsgIndex int, workflowEnded bool) (completed []string) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	// Save pending skill names before clearing.
	updatedSkills := append([]string(nil), sc.pending...)

	// Update records for skills invoked in this SubmitMessage.
	for _, skillName := range sc.pending {
		execs := sc.executions[skillName]
		if len(execs) == 0 {
			continue
		}
		last := &execs[len(execs)-1]
		if last.EndMessageIndex == 0 {
			last.EndMessageIndex = endMsgIndex
		}
	}
	sc.pending = nil

	// For active workflow skills, update the last record on every SubmitMessage
	// so that EndMessageIndex reflects the full workflow execution.
	if sc.activeWorkflow != "" {
		execs := sc.executions[sc.activeWorkflow]
		if len(execs) > 0 {
			last := &execs[len(execs)-1]
			last.EndMessageIndex = endMsgIndex
		}
	}

	// Handle workflow mode: keep the workflow open across SubmitMessages until
	// workflowEnded is true.
	if sc.activeWorkflow != "" {
		skillName := sc.activeWorkflow
		cfg := sc.configs[skillName]
		if cfg != nil && cfg.WorkflowMode && !workflowEnded {
			// Workflow is still waiting for user input.
			return nil
		}
		// Workflow finished.
		sc.activeWorkflow = ""
		execs := sc.executions[skillName]
		if len(execs) > 0 {
			execs[len(execs)-1].Completed = true
		}
		completed = append(completed, skillName)
		return completed
	}

	// Non-workflow mode: mark all updated skills as completed.
	for _, skillName := range updatedSkills {
		execs := sc.executions[skillName]
		if len(execs) == 0 {
			continue
		}
		execs[len(execs)-1].Completed = true
		completed = append(completed, skillName)
	}
	return completed
}

// TakeRecordsForAnalysis returns and clears the collected execution markers for a skill.
func (sc *SkillCollector) TakeRecordsForAnalysis(skillName string) []SkillExecutionRecord {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	execs := sc.executions[skillName]
	sc.executions[skillName] = nil
	if sc.activeWorkflow == skillName {
		sc.activeWorkflow = ""
	}
	return execs
}

// Reset clears all in-memory records, active workflow state, and pending state.
func (sc *SkillCollector) Reset() {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	sc.executions = make(map[string][]SkillExecutionRecord)
	sc.pending = nil
	sc.activeWorkflow = ""
}

// Snapshot returns the current execution records and active workflow name for
// session persistence. Pending state is intentionally omitted because it is
// always per-SubmitMessage and never survives a process boundary.
func (sc *SkillCollector) Snapshot() (executions map[string][]SkillExecutionRecord, activeWorkflow string) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	if len(sc.executions) == 0 {
		return nil, sc.activeWorkflow
	}
	out := make(map[string][]SkillExecutionRecord, len(sc.executions))
	for k, v := range sc.executions {
		if len(v) == 0 {
			continue
		}
		out[k] = append([]SkillExecutionRecord(nil), v...)
	}
	if len(out) == 0 {
		return nil, sc.activeWorkflow
	}
	return out, sc.activeWorkflow
}

// Restore loads previously snapshotted state. Pending is always cleared since
// it is per-SubmitMessage. Safe to call with nil/empty inputs.
func (sc *SkillCollector) Restore(executions map[string][]SkillExecutionRecord, activeWorkflow string) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	if executions == nil {
		sc.executions = make(map[string][]SkillExecutionRecord)
	} else {
		sc.executions = executions
	}
	sc.activeWorkflow = activeWorkflow
	sc.pending = nil
}

// ActiveWorkflowFeedbackPath returns the filesystem path to the feedback memory
// for the currently active workflow skill, or empty string if none is active.
func (sc *SkillCollector) ActiveWorkflowFeedbackPath() string {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	if sc.activeWorkflow == "" || sc.memoryDir == "" {
		return ""
	}
	cfg := sc.configs[sc.activeWorkflow]
	if cfg == nil || cfg.TargetMemoryName == "" {
		return ""
	}
	return filepath.Join(sc.memoryDir, cfg.TargetMemoryType, cfg.TargetMemoryName+".md")
}

// MatchSkillFeedbackPath checks if the query matches any workflow-mode skill's
// keywords and returns the corresponding feedback memory path. This allows
// surfacing a skill's feedback memory even before the skill tool is invoked.
func (sc *SkillCollector) MatchSkillFeedbackPath(query string) string {
	if sc.memoryDir == "" {
		return ""
	}
	q := strings.ToLower(query)
	for skillName, cfg := range sc.configs {
		if !cfg.WorkflowMode {
			continue
		}
		for _, kw := range cfg.Keywords {
			if strings.Contains(q, strings.ToLower(kw)) {
				if cfg.TargetMemoryName != "" && cfg.TargetMemoryType != "" {
					return filepath.Join(sc.memoryDir, cfg.TargetMemoryType, cfg.TargetMemoryName+".md")
				}
				// Fallback: use skill name as filename.
				return filepath.Join(sc.memoryDir, cfg.TargetMemoryType, skillName+"_feedback.md")
			}
		}
	}
	return ""
}

// Config returns the collector config for a skill.
func (sc *SkillCollector) Config(skillName string) *SkillCollectorConfig {
	return sc.configs[skillName]
}

// GetCompletedRecords returns all completed execution records for a skill.
func (sc *SkillCollector) GetCompletedRecords(skillName string) []SkillExecutionRecord {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	var result []SkillExecutionRecord
	for _, rec := range sc.executions[skillName] {
		if rec.Completed {
			result = append(result, rec)
		}
	}
	return result
}

// SkillExecutionDir returns the directory for storing skill execution markdown files.
func (sc *SkillCollector) SkillExecutionDir(skillName string) string {
	if sc.memoryDir == "" {
		return ""
	}
	return filepath.Join(sc.memoryDir, "skill-executions", skillName)
}

// ListSkillExecutionMDFiles returns all markdown files for a skill, sorted by name.
func (sc *SkillCollector) ListSkillExecutionMDFiles(skillName string) []string {
	dir := sc.SkillExecutionDir(skillName)
	if dir == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var files []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".md") {
			files = append(files, filepath.Join(dir, name))
		}
	}
	sort.Strings(files)
	return files
}

// CleanSkillExecutionMDFiles removes all markdown files for a skill.
func (sc *SkillCollector) CleanSkillExecutionMDFiles(skillName string) {
	dir := sc.SkillExecutionDir(skillName)
	if dir == "" {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".md") {
			_ = os.Remove(filepath.Join(dir, name))
		}
	}
}

