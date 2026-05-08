package engine

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Jekinnnnnn/jekylan/internal/llm"
	"github.com/Jekinnnnnn/jekylan/internal/memory"
	"github.com/Jekinnnnnn/jekylan/internal/message"
	"github.com/Jekinnnnnn/jekylan/internal/query"
	"github.com/Jekinnnnnn/jekylan/internal/tool"
)

// Engine manages a conversation session.
type Engine struct {
	messages       []message.Message
	tools          *tool.Registry
	model          string
	maxTurns       int
	thinkingBudget int64
	systemPrompt   string
	client         llm.Client
	disableCompact bool
	totalUsage     *message.Usage // cumulative token usage across turns

	cancel context.CancelFunc // cancel the current SubmitMessage context

	// MemoryDir is the path to the file-based memory directory (memory).
	// When set, the memory system prompt section is built from this directory.
	memoryDir string

	// surfacedMemories tracks memory files already injected this turn
	// to avoid duplicate recall within a single SubmitMessage.
	surfacedMemories map[string]bool

	// recentTools tracks recently used tool names (up to 5, deduped)
	// for memory recall noise filtering.
	recentTools []string

	// Token budget tracking (reserved for future implementation)
	// Supports human-readable formats like "500K", "1M", "2.5M".
	tokenBudget string

	// sessionPath is the filesystem path for session persistence.
	// When empty, session persistence is disabled.
	sessionPath string

	// memoryWorker runs in a background goroutine and handles conversation
	// compaction, skill execution summarization, and skill-collector tracking.
	memoryWorker *memory.MemoryWorker

	// workflowCompleted is set when the assistant calls the workflow_complete tool
	// in the current SubmitMessage. It signals that a workflow-mode skill has finished.
	workflowCompleted bool

	// lastResult stores the most recent query result.
	lastResult query.Result

	// hasResult indicates whether lastResult is valid.
	hasResult bool
}

// EngineOption configures an Engine via functional options.
type EngineOption func(*Engine)

// WithMemoryDir sets the file-based memory directory for the engine.
func WithMemoryDir(dir string) EngineOption {
	return func(e *Engine) { e.memoryDir = dir }
}

// WithTokenBudget sets a per-session token budget (reserved).
// Supports human-readable formats like "500K", "1M", "2.5M".
func WithTokenBudget(budget string) EngineOption {
	return func(e *Engine) { e.tokenBudget = budget }
}

// WithSessionPath sets the session persistence file path.
func WithSessionPath(path string) EngineOption {
	return func(e *Engine) { e.sessionPath = path }
}

// NewEngine creates a new conversation engine.
func NewEngine(tools *tool.Registry, model string, maxTurns int, thinkingBudget int64, systemPrompt string, client llm.Client, disableCompact bool, opts ...EngineOption) *Engine {
	e := &Engine{
		messages:       make([]message.Message, 0),
		tools:          tools,
		model:          model,
		maxTurns:       maxTurns,
		thinkingBudget: thinkingBudget,
		systemPrompt:   buildFullSystemPrompt(systemPrompt, model),
		client:         client,
		disableCompact: disableCompact,
	}
	for _, opt := range opts {
		opt(e)
	}
	if e.memoryDir != "" {
		e.memoryWorker = memory.NewMemoryWorker(e.memoryDir, func(ctx context.Context, msgs []message.Message, systemPrompt string) <-chan query.Event {
			return query.Query(ctx, query.Params{
				Messages:       msgs,
				SystemPrompt:   systemPrompt,
				Tools:          e.tools,
				Client:         e.client,
				DisableCompact: true,
				MaxTurns:       3,
			})
		})
		e.memoryWorker.Start()
	}
	return e
}

func (e *Engine) Messages() []message.Message {
	return append([]message.Message(nil), e.messages...)
}

// TotalUsage returns the cumulative token usage across all turns.
func (e *Engine) TotalUsage() *message.Usage {
	if e.totalUsage == nil {
		return nil
	}
	return &message.Usage{
		InputTokens:              e.totalUsage.InputTokens,
		OutputTokens:             e.totalUsage.OutputTokens,
		CacheCreationInputTokens: e.totalUsage.CacheCreationInputTokens,
		CacheReadInputTokens:     e.totalUsage.CacheReadInputTokens,
	}
}

// Interrupt cancels the currently in-flight query (if any).
// The next SubmitMessage call will automatically replace the cancel func.
func (e *Engine) Interrupt() {
	if e.cancel != nil {
		e.cancel()
		e.cancel = nil
	}
}

// Reset clears all conversation history and usage, and cancels any in-flight query.
func (e *Engine) Reset() {
	e.reset()
}

func (e *Engine) reset() {
	e.Interrupt()
	e.messages = make([]message.Message, 0)
	e.totalUsage = nil
	e.surfacedMemories = nil
	e.recentTools = nil
	if e.sessionPath != "" {
		_ = e.ClearSessionFile(e.sessionPath)
	}
	if e.memoryWorker != nil && e.memoryWorker.SkillCollector() != nil {
		e.memoryWorker.SkillCollector().Reset()
	}
}

// CompactAnalysis delegates to the memory worker to compact the accumulated
// analysis file for the given skill. Returns an error if the skill is not
// configured or the analysis file does not exist.
func (e *Engine) CompactAnalysis(skillName string) error {
	if e.memoryWorker == nil {
		return fmt.Errorf("memory worker not initialized")
	}
	return e.memoryWorker.CompactAnalysis(skillName)
}

// recordRecentTool adds a tool name to the recent-tools list,
// deduplicating and keeping at most 5 entries.
func (e *Engine) recordRecentTool(name string) {
	if name == "" {
		return
	}
	// Remove existing entry to move to front.
	for i, t := range e.recentTools {
		if t == name {
			e.recentTools = append(e.recentTools[:i], e.recentTools[i+1:]...)
			break
		}
	}
	e.recentTools = append(e.recentTools, name)
	if len(e.recentTools) > 5 {
		e.recentTools = e.recentTools[len(e.recentTools)-5:]
	}
}

// injectRelevantMemories recalls memory files relevant to the user's prompt
// and injects their content as a user context message before the latest
// user message. This avoids bloating the system prompt and leverages
// prompt cache prefix sharing.
func (e *Engine) injectRelevantMemories(ctx context.Context, prompt string, msgs []message.Message) []message.Message {
	if e.memoryDir == "" {
		return msgs
	}

	relevant := memory.FindRelevantMemories(ctx, prompt, e.memoryDir, e.surfacedMemories, e.memoryWorker.SkillCollector())

	var memBlocks []string
	for _, r := range relevant {
		if e.surfacedMemories[r.Path] {
			continue
		}
		data, err := os.ReadFile(r.Path)
		if err != nil {
			continue
		}
		memBlocks = append(memBlocks, string(data))
		e.surfacedMemories[r.Path] = true
	}

	if len(memBlocks) == 0 {
		return msgs
	}

	memMsg := message.Message{Role: message.RoleUser, Timestamp: time.Now()}
	memMsg.AddText("Relevant memories for this query:\n\n" + strings.Join(memBlocks, "\n\n---\n\n"))

	// Inject before the last message (the current user prompt).
	if len(msgs) >= 2 {
		return append(append([]message.Message(nil), msgs[:len(msgs)-1]...), memMsg, msgs[len(msgs)-1])
	}
	return append([]message.Message{memMsg}, msgs...)
}

// RunOptions configures the Engine.Run loop.
type RunOptions struct {
	ReadInput   func() (string, error) // REPL mode provides this; single-shot leaves it nil
	OnOutput    func(string)           // required: called with formatted output strings
	OnQueryEnd  func()                 // optional: called when a query finishes
	OnResult    func(query.Result)     // optional: called with the final result
	Context     context.Context        // required
	SessionPath string                 // session file path
	Prompt      string                 // single-shot prompt when ReadInput is nil
}

// Run starts the engine event loop. It blocks until the loop exits.
// In REPL mode (ReadInput != nil) it loops reading input until /quit.
// In single-shot mode (ReadInput == nil) it processes Prompt once and returns.
func (e *Engine) Run(opts RunOptions) error {
	// Signal handler goroutine — forwards signals to the main loop via quitCh.
	quitCh := make(chan struct{})
	go func() {
		sigNotify := make(chan os.Signal, 1)
		signal.Notify(sigNotify, os.Interrupt, syscall.SIGTERM)
		<-sigNotify
		close(quitCh)
	}()

	if opts.ReadInput == nil {
		// Single-shot mode: process one prompt and return.
		events := e.startQuery(opts.Context, opts.Prompt)
		for {
			select {
			case <-quitCh:
				if opts.SessionPath != "" {
					_ = e.SaveSession(opts.SessionPath)
				}
				return nil
			case evt, ok := <-events:
				if !ok {
					if e.hasResult && e.memoryWorker != nil {
						e.memoryWorker.HandleTurnEnd(e.messages, e.workflowCompleted, e.lastResult.StopReason)
						e.workflowCompleted = false
					}
					if opts.SessionPath != "" {
						_ = e.SaveSession(opts.SessionPath)
					}
					if opts.OnResult != nil {
						opts.OnResult(e.lastResult)
					}
					return nil
				}
				output := e.processEvent(evt)
				if output != "" {
					opts.OnOutput(output)
				}
			}
		}
	}

	// REPL mode: start input goroutine.
	inputCh := make(chan string)
	go func() {
		defer close(inputCh)
		for {
			line, err := opts.ReadInput()
			if err != nil {
				return
			}
			inputCh <- line
		}
	}()

	var events <-chan query.Event

	for {
		select {
		case <-quitCh:
			if opts.SessionPath != "" {
				_ = e.SaveSession(opts.SessionPath)
			}
			return nil

		case text, ok := <-inputCh:
			if !ok {
				return nil
			}
			switch text {
			case "/quit", "/exit":
				if opts.SessionPath != "" {
					if err := e.SaveSession(opts.SessionPath); err != nil {
						opts.OnOutput(fmt.Sprintf("Failed to save session: %v\n", err))
					} else {
						opts.OnOutput(fmt.Sprintf("Session saved (%d messages)\n", len(e.Messages())))
					}
				}
				return nil
			case "/reset":
				e.reset()
				opts.OnOutput("Conversation reset.\n")
			case "/stop":
				e.Interrupt()
				opts.OnOutput("Interrupted.\n")
			case "":
				// ignore empty input
			default:
				if after, ok := strings.CutPrefix(text, "/compact-skill-exp "); ok {
					skillName := strings.TrimSpace(after)
					if skillName == "" {
						opts.OnOutput("Usage: /compact-skill-exp <skill-name>\n")
					} else if err := e.CompactAnalysis(skillName); err != nil {
						opts.OnOutput(fmt.Sprintf("Compact failed: %v\n", err))
					}
					continue
				}
				events = e.startQuery(opts.Context, text)
			}

		case evt, ok := <-events:
			if !ok {
				events = nil
				if e.hasResult && e.memoryWorker != nil {
					e.memoryWorker.HandleTurnEnd(e.messages, e.workflowCompleted, e.lastResult.StopReason)
					e.workflowCompleted = false
				}
				if opts.SessionPath != "" {
					if err := e.SaveSession(opts.SessionPath); err != nil {
						opts.OnOutput(fmt.Sprintf("Failed to save session: %v\n", err))
					} else {
						opts.OnOutput(fmt.Sprintf("Session saved (%d messages)\n", len(e.Messages())))
					}
				}
				if opts.OnQueryEnd != nil {
					opts.OnQueryEnd()
				}
				continue
			}
			output := e.processEvent(evt)
			if output != "" {
				opts.OnOutput(output)
			}
		}
	}
}

// repairOrphanToolUses appends a synthetic user message with placeholder
// ToolResultBlocks when the trailing assistant message contains tool_use
// blocks without matching tool_result blocks. Recovers from /stop, hard-kill
// session saves, and any other path that left e.messages with orphans.
func (e *Engine) repairOrphanToolUses() {
	if len(e.messages) == 0 {
		return
	}
	last := e.messages[len(e.messages)-1]
	if last.Role != message.RoleAssistant {
		return
	}
	toolUses := last.ToolUses()
	if len(toolUses) == 0 {
		return
	}
	repair := message.Message{Role: message.RoleUser, Timestamp: time.Now()}
	for _, tu := range toolUses {
		repair.AddToolResult(tu.ID, "[Tool execution interrupted]", true)
	}
	e.messages = append(e.messages, repair)
}

// startQuery prepares and launches an LLM query. It must only be called from
// the engine's main goroutine because it mutates engine state.
func (e *Engine) startQuery(ctx context.Context, prompt string) <-chan query.Event {
	// Cancel any previous in-flight query.
	if e.cancel != nil {
		e.cancel()
	}

	// Reset per-turn memory tracking.
	e.surfacedMemories = make(map[string]bool)

	// Defensive: if the previous turn left orphan tool_use blocks (e.g. from
	// a crash mid-query or a session loaded from before the /stop fix),
	// insert placeholder tool_result blocks so the next API request is
	// well-formed.
	e.repairOrphanToolUses()

	// Merge into the last user message if the previous turn ended with
	// tool results, avoiding two consecutive user messages which the API
	// rejects.
	if len(e.messages) > 0 && e.messages[len(e.messages)-1].Role == message.RoleUser {
		last := &e.messages[len(e.messages)-1]
		last.AddText(prompt)
	} else {
		userMsg := message.Message{Role: message.RoleUser, Timestamp: time.Now()}
		userMsg.AddText(prompt)
		e.messages = append(e.messages, userMsg)
	}

	// Derive a cancelable child context so Interrupt() can stop the stream.
	queryCtx, cancel := context.WithCancel(ctx)
	e.cancel = cancel

	// Build the message list for this query, injecting relevant memories
	// from memory before the latest user message.
	queryMsgs := e.injectRelevantMemories(queryCtx, prompt, append([]message.Message(nil), e.messages...))

	inner := query.Query(queryCtx, query.Params{
		Messages:       queryMsgs,
		SystemPrompt:   BuildSystemPrompt(e.systemPrompt, e.model, e.tools, e.tokenBudget, e.memoryDir),
		Tools:          e.tools,
		Model:          e.model,
		MaxTurns:       e.maxTurns,
		ThinkingBudget: e.thinkingBudget,
		Client:         e.client,
		DisableCompact: e.disableCompact,
		QuerySource:    "engine",
	})

	out := make(chan query.Event)
	go func() {
		defer close(out)
		for evt := range inner {
			out <- evt
		}
	}()
	return out
}

// processEvent updates engine state for a single query event and returns the
// formatted output string. It must only be called from the engine's main
// goroutine.
func (e *Engine) processEvent(evt query.Event) string {
	switch evt.Type {
	case "usage":
		e.messages = append(e.messages, evt.Message)
		if u := evt.Message.Usage; u != nil {
			if e.totalUsage == nil {
				e.totalUsage = &message.Usage{}
			}
			e.totalUsage.InputTokens += u.InputTokens
			e.totalUsage.OutputTokens += u.OutputTokens
			e.totalUsage.CacheCreationInputTokens += u.CacheCreationInputTokens
			e.totalUsage.CacheReadInputTokens += u.CacheReadInputTokens
		}
		if u := evt.Message.Usage; u != nil {
			total := u.InputTokens + u.OutputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens
			return fmt.Sprintf("\n[Tokens: input=%d output=%d cache_create=%d cache_read=%d total=%d]\n",
				u.InputTokens, u.OutputTokens, u.CacheCreationInputTokens, u.CacheReadInputTokens, total)
		}

	case "user_message":
		e.messages = append(e.messages, evt.Message)
		var sb strings.Builder
		sb.WriteString("\n=== Tool Results ===\n")
		for _, block := range evt.Message.Content {
			if b, ok := block.(message.ToolResultBlock); ok {
				label := b.ToolUseID
				content := b.Content
				fmt.Fprintf(&sb, "[%s] %s\n", label, content)
			}
		}
		sb.WriteString("=== Assistant ===\n")
		return sb.String()

	case "assistant_text":
		return evt.Text

	case "assistant_tool_use":
		e.recordRecentTool(evt.ToolName)
		if evt.ToolName == "workflow_complete" {
			e.workflowCompleted = true
		}
		sc := e.memoryWorker.SkillCollector()
		if sc != nil && evt.ToolName == "skill" && evt.ToolInput != nil {
			if skillName, ok := evt.ToolInput["skill"].(string); ok {
				startIdx := max(len(e.messages)-1, 0)
				sc.OnSkillInvocation(skillName, startIdx)
			}
		}
		return fmt.Sprintf("\n[Tool use: %s(%s)]\n", evt.ToolName, evt.ToolUseID)

	case "compaction_result":
		e.messages = evt.Messages
		if sc := e.memoryWorker.SkillCollector(); sc != nil {
			sc.Reset()
		}
		return fmt.Sprintf("Session compacted: %d messages\n", len(e.messages))

	case "result":
		e.lastResult = evt.Result
		e.hasResult = true
		if !evt.Result.Success {
			return fmt.Sprintf("\nError: %s\n", evt.Result.Error)
		}
		return "\n"

	case "error":
		e.lastResult = evt.Result
		e.hasResult = true
		return fmt.Sprintf("\nError: %s\n", evt.Result.Error)
	}

	return ""
}
