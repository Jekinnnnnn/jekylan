package engine

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
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
	queryFunc      func(ctx context.Context, params query.Params) <-chan query.Event
	disableCompact bool
	totalUsage     *message.Usage // cumulative token usage across turns

	cancel context.CancelFunc // cancel the current query context

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

	// cacheBreakpoints controls Anthropic prompt caching (0=off, 3=max).
	cacheBreakpoints int

	// coordinatorMode enables coordinator orchestration behavior.
	// When true, the engine uses coordinator system prompt and tool whitelist.
	coordinatorMode bool

	// memoryWorker runs in a background goroutine and handles conversation
	// compaction, skill execution summarization, and skill-collector tracking.
	memoryWorker memory.Worker

	// workflowCompleted is set when the assistant calls the workflow_complete tool
	// in the current SubmitMessage. It signals that a workflow-mode skill has finished.
	workflowCompleted bool

	// lastResult stores the most recent query result.
	lastResult query.Result

	// hasResult indicates whether lastResult is valid.
	hasResult bool

	// textBuffer accumulates assistant_text deltas and flushes them
	// at message boundaries (usage, tool_use, error, result events).
	textBuffer strings.Builder

	// toolsMu protects the tools registry when it is swapped at runtime
	// (e.g. during playbook execution).
	toolsMu sync.RWMutex

	// Internal event loop — all state mutations happen here.
	cmdCh    chan engineCmd
	stopOnce sync.Once
	stopWg   sync.WaitGroup

	// activeTurnCh points to the current turn's event output channel.
	// nil when no turn is active.
	activeTurnCh chan<- EngineEvent
}

type engineCmd interface{ isEngineCmd() }

type cmdTurn struct {
	ctx     context.Context
	prompt  string
	eventCh chan<- EngineEvent
	done    chan<- struct{}
}

func (cmdTurn) isEngineCmd() {}

type cmdNotify struct{ text string }

func (cmdNotify) isEngineCmd() {}

type cmdAddUsage struct{ usage *message.Usage }

func (cmdAddUsage) isEngineCmd() {}

type cmdInterrupt struct{}

func (cmdInterrupt) isEngineCmd() {}

type cmdReset struct{}

func (cmdReset) isEngineCmd() {}

type cmdGetMessages struct{ resp chan<- []message.Message }

func (cmdGetMessages) isEngineCmd() {}

type cmdGetTotalUsage struct{ resp chan<- *message.Usage }

func (cmdGetTotalUsage) isEngineCmd() {}

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

// WithCacheBreakpoints sets the number of Anthropic prompt cache breakpoints.
func WithCacheBreakpoints(n int) EngineOption {
	return func(e *Engine) { e.cacheBreakpoints = n }
}

// WithCoordinatorMode enables coordinator orchestration mode.
func WithCoordinatorMode(enabled bool) EngineOption {
	return func(e *Engine) { e.coordinatorMode = enabled }
}

// WithQueryFunc replaces the default query function. Intended for tests.
func WithQueryFunc(fn func(ctx context.Context, params query.Params) <-chan query.Event) EngineOption {
	return func(e *Engine) { e.queryFunc = fn }
}

// NewEngine creates a new conversation engine.
func NewEngine(tools *tool.Registry, model string, maxTurns int, thinkingBudget int64, systemPrompt string, client llm.Client, disableCompact bool, opts ...EngineOption) *Engine {
	e := &Engine{
		messages:       make([]message.Message, 0),
		tools:          tools,
		model:          model,
		maxTurns:       maxTurns,
		thinkingBudget: thinkingBudget,
		systemPrompt:   systemPrompt,
		client:         client,
		queryFunc:      query.Query,
		disableCompact: disableCompact,
		cmdCh:          make(chan engineCmd, 64),
	}
	for _, opt := range opts {
		opt(e)
	}
	if e.coordinatorMode {
		// Coordinator mode: reduce maxTurns because coordinator delegates work
		// to workers rather than doing substantive work itself.
		if e.maxTurns > 10 {
			e.maxTurns = 10
		}
	}
	if e.memoryDir != "" {
		e.memoryWorker = memory.NewMemoryWorker(e.memoryDir, func(ctx context.Context, msgs []message.Message, systemPrompt string) <-chan query.Event {
			e.toolsMu.RLock()
			tools := e.tools
			e.toolsMu.RUnlock()
			return query.Query(ctx, query.Params{
				Messages:         msgs,
				SystemPrompt:     systemPrompt,
				Tools:            tools,
				Client:           e.client,
				DisableCompact:   true,
				MaxTurns:         3,
				CacheBreakpoints: e.cacheBreakpoints,
			})
		})
		e.memoryWorker.Start()
	}
	e.stopWg.Add(1)
	go e.loop()
	return e
}

// Stop shuts down the Engine's internal event loop and waits for it to exit.
// It is safe to call multiple times.
func (e *Engine) Stop() {
	e.stopOnce.Do(func() {
		close(e.cmdCh)
	})
	e.stopWg.Wait()
	if e.memoryWorker != nil {
		e.memoryWorker.Stop()
	}
}

func (e *Engine) loop() {
	defer e.stopWg.Done()
	for cmd := range e.cmdCh {
		switch c := cmd.(type) {
		case cmdTurn:
			e.activeTurnCh = c.eventCh
			e.runTurn(c)
			e.activeTurnCh = nil
		case cmdNotify:
			e.handleNotify(c.text)
		case cmdAddUsage:
			e.accumulateUsage(c.usage)
		case cmdInterrupt:
			if e.cancel != nil {
				e.cancel()
			}
		case cmdReset:
			e.reset()
		case cmdGetMessages:
			c.resp <- append([]message.Message(nil), e.messages...)
		case cmdGetTotalUsage:
			c.resp <- e.totalUsageSnapshot()
		}
	}
}

// Turn submits a user prompt and returns a read-only event channel.
// The caller must consume the channel until it is closed.
// Turn must not be called after Stop.
func (e *Engine) Turn(ctx context.Context, prompt string) <-chan EngineEvent {
	out := make(chan EngineEvent, 64)
	done := make(chan struct{})

	go func() {
		defer close(out)
		<-done
	}()

	// This send will panic if Stop has been called (cmdCh closed).
	// Callers must ensure Turn is not used after Stop.
	e.cmdCh <- cmdTurn{ctx: ctx, prompt: prompt, eventCh: out, done: done}
	return out
}

func (e *Engine) runTurn(cmd cmdTurn) {
	defer close(cmd.done)

	e.hasResult = false
	e.lastResult = query.Result{}
	e.workflowCompleted = false
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
		last.AddText(cmd.prompt)
	} else {
		userMsg := message.Message{Role: message.RoleUser, Timestamp: time.Now()}
		userMsg.AddText(cmd.prompt)
		e.messages = append(e.messages, userMsg)
	}

	// Derive a cancelable child context so Interrupt() can stop the stream.
	queryCtx, cancel := context.WithCancel(cmd.ctx)
	e.cancel = cancel
	defer func() { e.cancel = nil }()

	// Build the message list for this query, injecting relevant memories
	// from memory before the latest user message.
	queryMsgs := e.injectRelevantMemories(queryCtx, cmd.prompt, append([]message.Message(nil), e.messages...))

	e.toolsMu.RLock()
	tools := e.tools
	e.toolsMu.RUnlock()

	events := e.queryFunc(queryCtx, query.Params{
		Messages:         queryMsgs,
		SystemPrompt:     BuildSystemPrompt(e.systemPrompt, e.model, tools, e.tokenBudget),
		Tools:            tools,
		Model:            e.model,
		MaxTurns:         e.maxTurns,
		ThinkingBudget:   e.thinkingBudget,
		Client:           e.client,
		DisableCompact:   e.disableCompact,
		CacheBreakpoints: e.cacheBreakpoints,
		QuerySource:      query.QuerySourceEngine,
	})

	for {
		select {
		case evt, ok := <-events:
			if !ok {
				e.finalizeTurn()
				// Flush any remaining text before turn ends.
				if text := e.flushText(); text != "" {
					cmd.eventCh <- EngineEvent{Type: EventTextDelta, TextDelta: text}
				}
				return
			}
			e.handleQueryEvent(evt, cmd.eventCh)
		case c, ok := <-e.cmdCh:
			if !ok {
				// Engine stopped: cancel query, drain events, exit.
				cancel()
				for range events {
				}
				return
			}
			switch c := c.(type) {
			case cmdInterrupt:
				cancel()
			case cmdNotify:
				e.handleNotify(c.text)
				if e.activeTurnCh != nil {
					e.activeTurnCh <- EngineEvent{Type: EventNotification, Notification: c.text}
				}
			case cmdAddUsage:
				e.accumulateUsage(c.usage)
			}
		}
	}
}

func (e *Engine) handleQueryEvent(evt query.Event, out chan<- EngineEvent) {
	switch evt.Type {
	case query.EventTypeUsage:
		e.messages = append(e.messages, evt.Message)
		e.accumulateUsage(evt.Message.Usage)
		if text := e.flushText(); text != "" {
			out <- EngineEvent{Type: EventTextDelta, TextDelta: text}
		}
		if u := evt.Message.Usage; u != nil {
			out <- EngineEvent{Type: EventUsage, Usage: u}
		}

	case query.EventTypeUserMessage:
		e.messages = append(e.messages, evt.Message)
		out <- EngineEvent{Type: EventUserMessage, Message: evt.Message}

	case query.EventTypeAssistantText:
		e.textBuffer.WriteString(evt.Text)

	case query.EventTypeAssistantToolUse:
		e.recordRecentTool(evt.ToolName)
		if evt.ToolName == "workflow_complete" {
			e.workflowCompleted = true
		}
		if e.memoryWorker != nil {
			sc := e.memoryWorker.SkillCollector()
			if sc != nil && evt.ToolName == "skill" && evt.ToolInput != nil {
				if skillName, ok := evt.ToolInput["skill"].(string); ok {
					// startIdx = len-1 captures the message immediately before the skill
					// invocation (the user request or prior context) so the skill's
					// execution range includes preceding context.
					startIdx := max(len(e.messages)-1, 0)
					sc.OnSkillInvocation(skillName, startIdx)
				}
			}
		}
		if text := e.flushText(); text != "" {
			out <- EngineEvent{Type: EventTextDelta, TextDelta: text}
		}
		out <- EngineEvent{Type: EventToolUse, ToolUse: ToolUseInfo{
			ToolUseID: evt.ToolUseID,
			ToolName:  evt.ToolName,
			ToolInput: evt.ToolInput,
		}}

	case query.EventTypeCompactionResult:
		e.messages = evt.Messages
		if e.memoryWorker != nil {
			if sc := e.memoryWorker.SkillCollector(); sc != nil {
				sc.Reset()
			}
		}
		if text := e.flushText(); text != "" {
			out <- EngineEvent{Type: EventTextDelta, TextDelta: text}
		}
		out <- EngineEvent{Type: EventCompacted, CompactedMsgCount: len(e.messages)}

	case query.EventTypeResult:
		e.lastResult = evt.Result
		e.hasResult = true
		if text := e.flushText(); text != "" {
			out <- EngineEvent{Type: EventTextDelta, TextDelta: text}
		}
		if !evt.Result.Success {
			out <- EngineEvent{Type: EventTurnError, Error: evt.Result.Error}
		} else {
			out <- EngineEvent{Type: EventTurnResult, Result: TurnResult{
				Success:    evt.Result.Success,
				Text:       evt.Result.Text,
				StopReason: evt.Result.StopReason,
				NumTurns:   evt.Result.NumTurns,
			}}
		}

	case query.EventTypeError:
		e.lastResult = evt.Result
		e.hasResult = true
		if text := e.flushText(); text != "" {
			out <- EngineEvent{Type: EventTextDelta, TextDelta: text}
		}
		out <- EngineEvent{Type: EventTurnError, Error: evt.Result.Error}
	}
}

func (e *Engine) finalizeTurn() {
	if e.hasResult && e.memoryWorker != nil {
		e.memoryWorker.HandleTurnEnd(e.messages, e.workflowCompleted, e.lastResult.StopReason)
		e.workflowCompleted = false
	}
}

func (e *Engine) handleNotify(text string) {
	sysMsg := message.Message{Role: message.RoleSystem, Timestamp: time.Now()}
	sysMsg.AddText(text)
	e.messages = append(e.messages, sysMsg)
}

// Messages returns a deep copy of the conversation history.
// It is safe to call from any goroutine.
func (e *Engine) Messages() []message.Message {
	resp := make(chan []message.Message, 1)
	e.cmdCh <- cmdGetMessages{resp: resp}
	return <-resp
}

// GetTools returns the current tool registry.
func (e *Engine) GetTools() *tool.Registry {
	e.toolsMu.RLock()
	defer e.toolsMu.RUnlock()
	return e.tools
}

// SetTools swaps the tool registry at runtime.
// Safe to call from any goroutine.
func (e *Engine) SetTools(tools *tool.Registry) {
	e.toolsMu.Lock()
	defer e.toolsMu.Unlock()
	e.tools = tools
}

// TotalUsage returns the cumulative token usage across all turns.
// It is safe to call from any goroutine.
func (e *Engine) TotalUsage() *message.Usage {
	resp := make(chan *message.Usage, 1)
	e.cmdCh <- cmdGetTotalUsage{resp: resp}
	return <-resp
}

func (e *Engine) totalUsageSnapshot() *message.Usage {
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

// Notify sends a system notification into the engine.
// It is safe to call from any goroutine; the send is non-blocking.
func (e *Engine) Notify(text string) {
	select {
	case e.cmdCh <- cmdNotify{text: text}:
	default:
	}
}

// AddUsage sends an external usage delta into the engine.
// It is safe to call from any goroutine; the send is non-blocking.
func (e *Engine) AddUsage(u *message.Usage) {
	if u == nil {
		return
	}
	select {
	case e.cmdCh <- cmdAddUsage{usage: u}:
	default:
	}
}

// Interrupt cancels the currently in-flight query (if any).
func (e *Engine) Interrupt() {
	select {
	case e.cmdCh <- cmdInterrupt{}:
	default:
	}
}

// Reset clears all conversation history and usage, and cancels any in-flight query.
func (e *Engine) Reset() {
	select {
	case e.cmdCh <- cmdReset{}:
	default:
	}
}

func (e *Engine) reset() {
	if e.cancel != nil {
		e.cancel()
	}
	e.messages = make([]message.Message, 0)
	e.totalUsage = nil
	e.surfacedMemories = nil
	e.recentTools = nil
	e.textBuffer.Reset()
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
	if e.surfacedMemories == nil {
		e.surfacedMemories = make(map[string]bool)
	}

	var sc *memory.SkillCollector
	if e.memoryWorker != nil {
		sc = e.memoryWorker.SkillCollector()
	}
	relevant := memory.FindRelevantMemories(ctx, prompt, e.memoryDir, e.surfacedMemories, sc)

	const maxFileSize = 4096  // skip files larger than 4KB
	const maxTotalSize = 8192 // cap total injected memory at 8KB

	var memBlocks []string
	var totalSize int
	for _, r := range relevant {
		if e.surfacedMemories[r.Path] {
			continue
		}
		data, err := os.ReadFile(r.Path)
		if err != nil {
			continue
		}
		if len(data) > maxFileSize {
			continue
		}
		if totalSize+len(data) > maxTotalSize {
			continue
		}
		memBlocks = append(memBlocks, string(data))
		totalSize += len(data)
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

	// Check if the message immediately before the assistant message is already
	// a repair placeholder for the same tool uses, to avoid duplicates when
	// startQuery is called repeatedly without a completed turn.
	if len(e.messages) >= 2 {
		next := e.messages[len(e.messages)-2]
		if next.Role == message.RoleUser {
			results := next.ToolResults()
			if len(results) == len(toolUses) {
				matched := 0
				for _, tu := range toolUses {
					for _, tr := range results {
						if tr.ToolUseID == tu.ID {
							matched++
							break
						}
					}
				}
				if matched == len(toolUses) {
					return
				}
			}
		}
	}

	repair := message.Message{Role: message.RoleUser, Timestamp: time.Now()}
	for _, tu := range toolUses {
		repair.AddToolResult(tu.ID, "[Tool execution interrupted]", true)
	}
	e.messages = append(e.messages, repair)
}

// flushText returns the accumulated text buffer content and resets it.
func (e *Engine) flushText() string {
	if e.textBuffer.Len() == 0 {
		return ""
	}
	s := e.textBuffer.String()
	e.textBuffer.Reset()
	return s
}

// accumulateUsage adds a usage delta to the engine's cumulative total.
func (e *Engine) accumulateUsage(u *message.Usage) {
	if u == nil {
		return
	}
	if e.totalUsage == nil {
		e.totalUsage = &message.Usage{}
	}
	e.totalUsage.InputTokens += u.InputTokens
	e.totalUsage.OutputTokens += u.OutputTokens
	e.totalUsage.CacheCreationInputTokens += u.CacheCreationInputTokens
	e.totalUsage.CacheReadInputTokens += u.CacheReadInputTokens
}
