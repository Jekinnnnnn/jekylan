package memory

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Jekinnnnnn/jekylan/internal/message"
	"github.com/Jekinnnnnn/jekylan/internal/query"
	"github.com/fsnotify/fsnotify"
)

// QueryFunc is a function that executes an LLM query with the given messages
// and system prompt, returning a channel of query events.
// The engine provides this function so MemoryWorker does not directly hold
// an LLM client or tool registry.
type QueryFunc func(ctx context.Context, msgs []message.Message, systemPrompt string) <-chan query.Event

// TurnEndWork captures the state at the end of a SubmitMessage turn for
// asynchronous processing by the MemoryWorker.
type TurnEndWork struct {
	Messages          []message.Message
	WorkflowCompleted bool
	StopReason        string
}

// Worker is the interface exposed by MemoryWorker.
// Engine depends on this interface rather than the concrete type.
type Worker interface {
	Start()
	Stop()
	HandleTurnEnd(msgs []message.Message, workflowCompleted bool, stopReason string) bool
	SkillCollector() *SkillCollector
	CompactAnalysis(skillName string) error
}

// MemoryWorker runs in a background goroutine. It receives conversation
// messages through a channel, compacts them (removing noise), and uses
// an LLM with the memory prompt as system message to generate summaries.
type MemoryWorker struct {
	memoryDir      string
	queryFunc      QueryFunc
	turnEndCh      chan TurnEndWork
	done           chan struct{}
	stopOnce       sync.Once
	wg             sync.WaitGroup
	skillCollector *SkillCollector
}

// NewMemoryWorker creates a MemoryWorker. Call Start to begin processing.
// queryFunc is provided by the engine and uses the engine's LLM client and tools.
func NewMemoryWorker(memoryDir string, queryFunc QueryFunc) *MemoryWorker {
	sc := NewSkillCollector(memoryDir)
	sc.LoadFromMemoryDir()
	return &MemoryWorker{
		memoryDir:      memoryDir,
		queryFunc:      queryFunc,
		turnEndCh:      make(chan TurnEndWork, 4),
		done:           make(chan struct{}),
		skillCollector: sc,
	}
}

// SkillCollector returns the internal skill collector for direct access.
func (mw *MemoryWorker) SkillCollector() *SkillCollector {
	return mw.skillCollector
}

// Start begins the background goroutines.
func (mw *MemoryWorker) Start() {
	mw.wg.Add(2)
	go func() {
		defer mw.wg.Done()
		mw.run()
	}()
	go func() {
		defer mw.wg.Done()
		mw.watchSkillExecutions()
	}()
}

// Stop signals the worker to shut down and waits for goroutines to finish.
func (mw *MemoryWorker) Stop() {
	mw.stopOnce.Do(func() {
		close(mw.done)
	})
	mw.wg.Wait()
}

// watchSkillExecutions watches the skill-executions directory for new files.
// When a new .md file is created in a skill subdirectory, it checks if the
// file count has reached the threshold and triggers analysis if so.
func (mw *MemoryWorker) watchSkillExecutions() {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[memory-worker] failed to create fsnotify watcher: %v\n", err)
		return
	}
	defer watcher.Close()

	execDir := filepath.Join(mw.memoryDir, "skill-executions")
	if err := os.MkdirAll(execDir, 0755); err == nil {
		if err := watcher.Add(execDir); err != nil {
			fmt.Fprintf(os.Stderr, "[memory-worker] failed to watch %s: %v\n", execDir, err)
		}
	}

	if entries, err := os.ReadDir(execDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
				subDir := filepath.Join(execDir, entry.Name())
				watcher.Add(subDir)
			}
		}
	}

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if !event.Has(fsnotify.Create) {
				continue
			}
			info, err := os.Stat(event.Name)
			if err != nil {
				continue
			}
			if info.IsDir() {
				if err := watcher.Add(event.Name); err != nil {
					fmt.Fprintf(os.Stderr, "[memory-worker] failed to watch %s: %v\n", event.Name, err)
				}
				continue
			}
			dir := filepath.Dir(event.Name)
			skillName := filepath.Base(dir)
			go mw.checkSkillThreshold(skillName)

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			fmt.Fprintf(os.Stderr, "[memory-worker] fsnotify error: %v\n", err)

		case <-mw.done:
			return
		}
	}
}

func (mw *MemoryWorker) checkSkillThreshold(skillName string) {
	sc := mw.skillCollector
	cfg := sc.Config(skillName)
	if cfg == nil || cfg.Threshold <= 0 {
		return
	}
	mdFiles := sc.ListSkillExecutionMDFiles(skillName)
	if len(mdFiles) >= cfg.Threshold {
		mw.TriggerSkillAnalysis(skillName)
	}
}

// HandleTurnEnd notifies the worker that a SubmitMessage turn has ended.
// It copies the message slice so the caller can continue mutating e.messages.
// Returns false if the worker is shutting down.
func (mw *MemoryWorker) HandleTurnEnd(msgs []message.Message, workflowCompleted bool, stopReason string) bool {
	select {
	case mw.turnEndCh <- TurnEndWork{
		Messages:          append([]message.Message(nil), msgs...),
		WorkflowCompleted: workflowCompleted,
		StopReason:        stopReason,
	}:
		return true
	case <-mw.done:
		return false
	}
}

// TriggerSkillAnalysis reads saved markdown files for a skill that reached its
// collection threshold, summarizes each one, and writes the combined result.
// Multiple concurrent triggers for the same skill are safe: each goroutine
// atomically "claims" files via os.Rename, so no file is processed twice.
func (mw *MemoryWorker) TriggerSkillAnalysis(skillName string) {
	sc := mw.skillCollector
	sc.TakeRecordsForAnalysis(skillName) // clear records
	cfg := sc.Config(skillName)
	if cfg == nil {
		return
	}

	mdFiles := sc.ListSkillExecutionMDFiles(skillName)
	if len(mdFiles) == 0 {
		return
	}

	// Create a batch directory to atomically claim files.
	batchDir := filepath.Join(sc.SkillExecutionDir(skillName),
		".batch_"+time.Now().Format("20060102_150405"))
	if err := os.MkdirAll(batchDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "[memory-worker] failed to create batch dir: %v\n", err)
		return
	}

	// Atomically move files into the batch directory. If another goroutine
	// already renamed a file, os.Rename returns an error and we skip it.
	var myFiles []string
	for _, f := range mdFiles {
		dst := filepath.Join(batchDir, filepath.Base(f))
		if err := os.Rename(f, dst); err == nil {
			myFiles = append(myFiles, dst)
		}
	}

	if len(myFiles) == 0 {
		return
	}

	var parts []string
	var emptyCount int
	for _, mdPath := range myFiles {
		data, err := os.ReadFile(mdPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[memory-worker] failed to read md file %s: %v\n", mdPath, err)
			continue
		}
		execution := string(data)
		if execution == "" {
			emptyCount++
			fmt.Fprintf(os.Stderr, "[memory-worker] %s: execution empty for %s\n", skillName, filepath.Base(mdPath))
			continue
		}
		parts = append(parts, execution)
	}

	if len(parts) == 0 {
		fmt.Fprintf(os.Stderr, "[memory-worker] %s: no analysis to write (processed %d files: %d empty)\n",
			skillName, len(myFiles), emptyCount)
		return
	}

	combined := strings.Join(parts, "\n\n---\n\n")

	// Build target path.
	filename := cfg.TargetMemoryName
	if filename == "" {
		filename = fmt.Sprintf("%s_analysis", skillName)
	}
	analysisPath := filepath.Join(mw.memoryDir, cfg.TargetMemoryType, filename+".md")

	// Build prompt for LLM to write the combined analysis via file_write.
	var prompt strings.Builder
	prompt.WriteString("If there are no durable insights worth remembering, output nothing (empty response).\n\n")
	prompt.WriteString("Write the skill execution analysis to the specified file using the file_write tool.\n\n")
	fmt.Fprintf(&prompt, "Target file: %s\n\n", analysisPath)
	prompt.WriteString("If the file already exists, read it first with file_read, then append the new sections and write the complete content back with file_write.\n\n")
	prompt.WriteString("New skill execution sections to append:\n\n")
	prompt.WriteString(combined)
	prompt.WriteString("\n\n")
	prompt.WriteString("The file should use the following frontmatter if it does not already exist:\n")
	fmt.Fprintf(&prompt, "---\nname: %s\ndescription: Auto-generated analysis of %s skill executions\ntype: %s\n---\n", filename, skillName, cfg.TargetMemoryType)
	fmt.Fprintf(&prompt, "\nFollowed by each analysis section with the header: ## Analysis %s\n", time.Now().Format("2006-01-02"))

	msg := message.Message{Role: message.RoleUser, Timestamp: time.Now()}
	msg.AddText(prompt.String())

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var fileWriteCalled bool
	for evt := range mw.queryFunc(ctx, []message.Message{msg}, mw.buildSystemPrompt()) {
		switch evt.Type {
		case "assistant_tool_use":
			if evt.ToolName == "file_write" {
				fileWriteCalled = true
			}
		case "error":
			fmt.Fprintf(os.Stderr, "[memory-worker] analysis write query error for %q: %s\n", skillName, evt.Result.Error)
		case "result":
			if !evt.Result.Success {
				fmt.Fprintf(os.Stderr, "[memory-worker] analysis write query failed for %q: %s\n", skillName, evt.Result.Error)
			}
		}
	}

	if fileWriteCalled {
		os.RemoveAll(batchDir)
		fmt.Fprintf(os.Stderr, "[memory-worker] analysis for %q written via file_write to %s\n", skillName, analysisPath)
	} else {
		fmt.Fprintf(os.Stderr, "[memory-worker] analysis for %q: LLM did not call file_write. Original records preserved in %s\n", skillName, batchDir)
	}
}

func (mw *MemoryWorker) run() {
	for {
		select {
		case turnEnd := <-mw.turnEndCh:
			mw.handleTurnEnd(turnEnd)
		case <-mw.done:
			return
		}
	}
}

func (mw *MemoryWorker) handleTurnEnd(turnEnd TurnEndWork) {
	sc := mw.skillCollector
	// For workflow-mode skills, the ONLY signal that the workflow is finished
	// is the assistant calling the workflow_complete tool. StopReason can be
	// end_turn, max_tokens, stop_sequence, etc. — none of those should end the
	// workflow; only workflow_complete should.
	shouldEndWorkflow := turnEnd.WorkflowCompleted
	completed := sc.OnExecutionResult(len(turnEnd.Messages), shouldEndWorkflow)

	// Save each completed skill execution as a markdown file.
	for _, skillName := range completed {
		mw.saveSkillExecutionMD(skillName, turnEnd.Messages)
	}
}

// saveSkillExecutionMD saves a completed skill execution as a markdown file
// using CompactMessages. One file per execution.
func (mw *MemoryWorker) saveSkillExecutionMD(skillName string, messages []message.Message) {
	sc := mw.skillCollector
	records := sc.GetCompletedRecords(skillName)
	if len(records) == 0 {
		return
	}
	rec := records[len(records)-1]
	start := rec.StartMessageIndex
	end := rec.EndMessageIndex
	if start < 0 || end > len(messages) || start >= end {
		return
	}
	execMsgs := messages[start:end]
	mdContent := CompactMessages(execMsgs)

	dir := sc.SkillExecutionDir(skillName)
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "[memory-worker] failed to create execution dir: %v\n", err)
		return
	}
	filename := fmt.Sprintf("%s_%d-%d.md", time.Now().Format("20060102_150405.000"), start, end)
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, []byte(mdContent), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "[memory-worker] failed to write execution md: %v\n", err)
		return
	}
	fmt.Fprintf(os.Stderr, "[memory-worker] saved execution md to %s\n", path)
}

func (mw *MemoryWorker) buildSystemPrompt() string {
	var b strings.Builder
	b.WriteString("You are a conversation analysis assistant. Your task is to review execution logs, extract key events and actionable insights, and produce concise summaries.\n\n")
	if mw.memoryDir != "" {
		if prompt := BuildMemoryPrompt(mw.memoryDir); prompt != "" {
			b.WriteString(prompt)
			b.WriteString("\n\n")
		}
	}
	return b.String()
}

// CompactAnalysis reads the accumulated analysis file for a skill, sends all
// historical analysis sections to the LLM for deduplication and merging,
// and has the LLM write the compacted result back using the file_write tool.
func (mw *MemoryWorker) CompactAnalysis(skillName string) error {
	cfg := mw.skillCollector.Config(skillName)
	if cfg == nil {
		return fmt.Errorf("skill %q not configured", skillName)
	}

	filename := cfg.TargetMemoryName
	if filename == "" {
		filename = fmt.Sprintf("%s_analysis", skillName)
	}
	path := filepath.Join(mw.memoryDir, cfg.TargetMemoryType, filename+".md")

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("analysis file not found for skill %q", skillName)
		}
		return fmt.Errorf("read analysis file: %w", err)
	}

	frontmatter, sections := parseAnalysisFile(string(data))
	if len(sections) < 2 {
		return fmt.Errorf("only %d analysis section(s), nothing to compact", len(sections))
	}

	prompt := mw.buildCompactPrompt(sections)
	prompt += fmt.Sprintf("\n\nAfter compacting, write the complete file to %s using the file_write tool. If the file already exists, read it first with file_read before writing. The file must preserve the original frontmatter:\n\n%s\n\nFollowed by the compacted analysis with the header: ## Analysis %s (compacted)\n", path, frontmatter, time.Now().Format("2006-01-02"))

	summaryMsg := message.Message{Role: message.RoleUser, Timestamp: time.Now()}
	summaryMsg.AddText(prompt)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	var fileWriteCalled bool

	for evt := range mw.queryFunc(ctx, []message.Message{summaryMsg}, mw.buildSystemPrompt()) {
		switch evt.Type {
		case "assistant_tool_use":
			if evt.ToolName == "file_write" {
				fileWriteCalled = true
			}
		case "error":
			return fmt.Errorf("LLM compact query error: %s", evt.Result.Error)
		case "result":
			if !evt.Result.Success {
				return fmt.Errorf("LLM compact query failed: %s", evt.Result.Error)
			}
		}
	}

	if !fileWriteCalled {
		return fmt.Errorf("LLM did not call file_write to save the compacted analysis")
	}

	fmt.Fprintf(os.Stderr, "[memory-worker] compacted %d sections into 1 for %q (written via file_write)\n",
		len(sections), skillName)
	return nil
}

// parseAnalysisFile splits an analysis markdown file into its YAML frontmatter
// and a slice of analysis section contents (without the ## Analysis headers).
func parseAnalysisFile(data string) (frontmatter string, sections []string) {
	data = strings.TrimSpace(data)

	// Extract frontmatter: ---\n...\n---
	if strings.HasPrefix(data, "---") {
		end := strings.Index(data[3:], "\n---")
		if end != -1 {
			frontmatter = data[:end+7] // include trailing ---
			data = strings.TrimSpace(data[end+7:])
		}
	}

	// Split by ## Analysis headers.
	parts := strings.SplitSeq(data, "\n##")
	for part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		// Remove the date line (first line like "2026-05-01\n\n")
		if idx := strings.Index(part, "\n\n"); idx != -1 {
			part = part[idx+2:]
		} else if idx := strings.Index(part, "\n"); idx != -1 {
			part = part[idx+1:]
		}
		if strings.TrimSpace(part) != "" {
			sections = append(sections, part)
		}
	}
	return
}

func (mw *MemoryWorker) buildCompactPrompt(sections []string) string {
	var b strings.Builder
	for i, section := range sections {
		fmt.Fprintf(&b, "--- Record %d ---\n%s\n\n", i+1, section)
	}
	return b.String()
}
