package agent

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/Jekinnnnnn/jekylan/internal/message"
)

// Status represents the lifecycle state of a running sub-agent.
type Status string

const (
	StatusRunning   Status = "running"
	StatusCompleted Status = "completed"
	StatusKilled    Status = "killed"
	StatusError     Status = "error"
)

// RunningAgent tracks a single sub-agent instance.
type RunningAgent struct {
	ID         string
	Definition *Definition
	Prompt     string
	Status     Status
	Result     string
	Error      string
	Usage      *message.Usage
	Cancel     context.CancelFunc
}

// internalEvent is a marker interface for events handled by the coordinator's
// single event loop goroutine.
type internalEvent any

type spawnReq struct {
	ctx    context.Context
	def    *Definition
	prompt string
	opts   RunnerOptions
	resp   chan<- string
}

type killReq struct {
	id   string
	resp chan<- bool
}

type confirmReq struct {
	agentID  string
	approved bool
	resp     chan<- bool
}

type getReq struct {
	id   string
	resp chan<- *RunningAgent
}

type listReq struct {
	resp chan<- []*RunningAgent
}

type totalUsageReq struct {
	resp chan<- *message.Usage
}

type hasPendingConfirmReq struct {
	resp chan<- string
}

type stopReq struct {
	resp chan<- struct{}
}

type waitReq struct {
	id   string
	resp chan *RunningAgent
}

type agentDoneEvent struct {
	id     string
	status Status
	result string
	err    string
	usage  *message.Usage
}

type agentConfirmEvent struct {
	id       string
	toolName string
	input    map[string]any
	respCh   chan ConfirmResponse
}

// Coordinator owns the sub-agent registry and drives lifecycle management
// (spawn/kill/wait/confirm/usage) in its own goroutine. All state is managed
// by a single event-loop goroutine; there are no locks.
//
// The event loop is started automatically by NewCoordinator(). Public
// methods (Spawn, Kill, Confirm, Get, List) send requests into the
// event channel and block on a response channel.
type Coordinator struct {
	// Internal event channel — single goroutine owns all state.
	events chan internalEvent

	// State — accessed ONLY by the event-loop goroutine.
	agents              map[string]*RunningAgent
	completedAgents     map[string]*RunningAgent // agents that finished before Wait was called
	nextID              int
	pendingConfirms     map[string]chan ConfirmResponse
	pendingConfirmOrder []string // FIFO order of pending confirm requests
	totalUsage          *message.Usage

	// Playbook integration: callers can block until an agent finishes.
	waiters map[string][]chan *RunningAgent

	// Agent lifecycle notices — buffered so sends never block the loop.
	notices   chan string
	closeOnce sync.Once

	// usageSink is called on the event-loop goroutine whenever an agent
	// finishes with non-nil usage. It bridges sub-agent usage to the engine.
	usageSink func(*message.Usage)

	// debug prints sub-agent streaming output to stderr when true.
	debug bool
}

// CoordinatorOption configures a Coordinator.
type CoordinatorOption func(*Coordinator)

// WithDebug enables verbose sub-agent output printing.
func WithDebug(debug bool) CoordinatorOption {
	return func(c *Coordinator) {
		c.debug = debug
	}
}

// NewCoordinator builds a Coordinator with empty sub-agent registry and
// starts the background event loop.
func NewCoordinator(opts ...CoordinatorOption) *Coordinator {
	c := &Coordinator{
		events:              make(chan internalEvent, 16),
		agents:              make(map[string]*RunningAgent),
		completedAgents:     make(map[string]*RunningAgent),
		pendingConfirms:     make(map[string]chan ConfirmResponse),
		waiters:             make(map[string][]chan *RunningAgent),
		pendingConfirmOrder: make([]string, 0),
		notices:             make(chan string, 256),
	}
	for _, opt := range opts {
		opt(c)
	}
	go c.loop()
	return c
}

// Notices returns the channel of agent lifecycle notifications. It is closed
// when the coordinator is stopped.
func (c *Coordinator) Notices() <-chan string { return c.notices }

// Stop gracefully shuts down the coordinator, cancelling all running agents
// and closing the notices channel. It is safe to call multiple times.
func (c *Coordinator) Stop() {
	resp := make(chan struct{}, 1)
	c.events <- stopReq{resp: resp}
	<-resp
}

// SetUsageSink registers a callback that receives sub-agent usage deltas
// whenever an agent completes. The callback is invoked on the coordinator's
// event-loop goroutine.
func (c *Coordinator) SetUsageSink(sink func(*message.Usage)) {
	// Synchronous update is safe because the caller typically sets this
	// before any agents are spawned.
	c.usageSink = sink
}

// Spawn starts a new background sub-agent and returns its ID.
func (c *Coordinator) Spawn(ctx context.Context, def *Definition, prompt string, opts RunnerOptions) string {
	resp := make(chan string, 1)
	c.events <- spawnReq{ctx: ctx, def: def, prompt: prompt, opts: opts, resp: resp}
	return <-resp
}

// Kill cancels a running sub-agent by ID. Returns true only if the agent
// was found and still running. The agent remains in the registry until the
// runner exits and sends an agentDoneEvent.
func (c *Coordinator) Kill(id string) bool {
	resp := make(chan bool, 1)
	c.events <- killReq{id: id, resp: resp}
	return <-resp
}

// Get returns a copy of the running agent state, or nil if not found.
func (c *Coordinator) Get(id string) *RunningAgent {
	resp := make(chan *RunningAgent, 1)
	c.events <- getReq{id: id, resp: resp}
	return <-resp
}

// List returns a snapshot of all tracked sub-agents.
func (c *Coordinator) List() []*RunningAgent {
	resp := make(chan []*RunningAgent, 1)
	c.events <- listReq{resp: resp}
	return <-resp
}

// Confirm sends an approval response to a pending confirmation request for
// the given agent. Returns true if a pending request was found and the
// response was delivered.
func (c *Coordinator) Confirm(agentID string, approved bool) bool {
	resp := make(chan bool, 1)
	c.events <- confirmReq{agentID: agentID, approved: approved, resp: resp}
	return <-resp
}

// HasPendingConfirm returns the ID of the first agent with a pending
// confirmation, or empty string if none.
func (c *Coordinator) HasPendingConfirm() string {
	resp := make(chan string, 1)
	c.events <- hasPendingConfirmReq{resp: resp}
	return <-resp
}

// Wait blocks until the agent with the given ID finishes (or is not found).
// It returns a clone of the completed agent state, or nil if the agent was
// never spawned.
func (c *Coordinator) Wait(id string) *RunningAgent {
	resp := make(chan *RunningAgent, 1)
	c.events <- waitReq{id: id, resp: resp}
	return <-resp
}

// TotalUsage returns the cumulative token usage across all sub-agents.
func (c *Coordinator) TotalUsage() *message.Usage {
	resp := make(chan *message.Usage, 1)
	c.events <- totalUsageReq{resp: resp}
	return <-resp
}

// loop is the single goroutine that owns all Coordinator state.
func (c *Coordinator) loop() {
	for evt := range c.events {
		switch e := evt.(type) {
		case spawnReq:
			c.handleSpawn(e)

		case killReq:
			e.resp <- c.handleKill(e.id)

		case confirmReq:
			e.resp <- c.handleConfirm(e.agentID, e.approved)

		case getReq:
			e.resp <- c.handleGet(e.id)

		case listReq:
			e.resp <- c.handleList()

		case totalUsageReq:
			e.resp <- c.handleTotalUsage()

		case hasPendingConfirmReq:
			e.resp <- c.handleHasPendingConfirm()

		case waitReq:
			c.handleWait(e)

		case agentDoneEvent:
			c.handleAgentDone(e)

		case agentConfirmEvent:
			c.handleAgentConfirm(e)

		case stopReq:
			for _, a := range c.agents {
				if a.Cancel != nil {
					a.Cancel()
				}
			}
			c.closeOnce.Do(func() {
				close(c.notices)
			})
			e.resp <- struct{}{}
			return
		}
	}
}

func (c *Coordinator) handleSpawn(e spawnReq) {
	id := fmt.Sprintf("agent-%d", c.nextID)
	c.nextID++

	agentCtx, cancel := context.WithCancel(e.ctx)
	ra := &RunningAgent{
		ID:         id,
		Definition: e.def,
		Prompt:     e.prompt,
		Status:     StatusRunning,
		Cancel:     cancel,
	}
	c.agents[id] = ra
	agentTypeStr := "default"
	if e.def != nil && e.def.Name != "" {
		agentTypeStr = e.def.Name
	}
	c.notify(fmt.Sprintf("[agent] %s spawned (type=%s)", id, agentTypeStr))
	e.resp <- id

	runnerOpts := e.opts
	runnerOpts.Definition = e.def
	runner := NewRunner(runnerOpts)

	// Start runner first — this creates the confirmReqCh that Confirmations()
	// returns. The confirm goroutine must start after Run.
	outCh := runner.Run(agentCtx, e.prompt)

	// Confirm forwarding goroutine.
	go func() {
		for req := range runner.Confirmations() {
			c.events <- agentConfirmEvent{
				id:       id,
				toolName: req.ToolName,
				input:    req.Input,
				respCh:   req.RespCh,
			}
		}
	}()

	// Runner event consumption goroutine.
	go func() {
		var finalStatus Status
		var finalResult, finalErr string
		var finalUsage *message.Usage
		var debugBuf strings.Builder

		for evt := range outCh {
			if c.debug && evt.Type == RunEventProgress {
				debugBuf.WriteString(evt.Text)
			}
			switch evt.Type {
			case RunEventComplete:
				finalStatus = StatusCompleted
				finalResult = evt.Result
				finalUsage = evt.Usage
			case RunEventError:
				finalStatus = StatusError
				finalErr = evt.Error
			}
		}

		if c.debug && debugBuf.Len() > 0 {
			fmt.Fprintf(os.Stderr, "\n=== [agent:%s output] ===\n%s\n=== [agent:%s end] ===\n", id, debugBuf.String(), id)
		}

		// If no complete/error event arrived, the runner exited because of
		// context cancellation (Kill) — treat as killed.
		if finalStatus == "" {
			finalStatus = StatusKilled
		}

		c.events <- agentDoneEvent{
			id:     id,
			status: finalStatus,
			result: finalResult,
			err:    finalErr,
			usage:  finalUsage,
		}
	}()
}

func (c *Coordinator) handleAgentDone(e agentDoneEvent) {
	a, ok := c.agents[e.id]
	if !ok {
		return
	}

	a.Status = e.status
	a.Result = e.result
	a.Error = e.err
	a.Usage = e.usage

	c.accumulateUsage(e.usage)
	if c.usageSink != nil && e.usage != nil {
		c.usageSink(e.usage)
	}

	// Notify waiters (Playbook executor).
	for _, ch := range c.waiters[e.id] {
		ch <- cloneRunningAgent(a)
	}
	delete(c.waiters, e.id)

	var notice string
	switch e.status {
	case StatusCompleted:
		notice = fmt.Sprintf("[agent] %s complete", e.id)
		if e.result != "" {
			notice += ": " + truncate(e.result, 200)
		}
	case StatusError:
		notice = fmt.Sprintf("[agent] %s error", e.id)
		if e.err != "" {
			notice += ": " + truncate(e.err, 200)
		}
	case StatusKilled:
		notice = fmt.Sprintf("[agent] %s killed", e.id)
	default:
		notice = fmt.Sprintf("[agent] %s %s", e.id, e.status)
	}
	c.notify(notice)

	c.completedAgents[e.id] = cloneRunningAgent(a)
	delete(c.agents, e.id)
	delete(c.pendingConfirms, e.id)
	c.removePendingConfirmOrder(e.id)
}

func (c *Coordinator) handleKill(id string) bool {
	a, ok := c.agents[id]
	if !ok || a.Status != StatusRunning {
		return false
	}
	if a.Cancel != nil {
		a.Cancel()
	}
	// Agent stays in the registry until runner exits and sends agentDoneEvent.
	return true
}

func (c *Coordinator) handleConfirm(agentID string, approved bool) (ok bool) {
	ch, exists := c.pendingConfirms[agentID]
	delete(c.pendingConfirms, agentID)
	c.removePendingConfirmOrder(agentID)
	if !exists {
		return false
	}
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	ch <- ConfirmResponse{Approved: approved}
	return true
}

func (c *Coordinator) handleAgentConfirm(e agentConfirmEvent) {
	c.pendingConfirms[e.id] = e.respCh
	c.pendingConfirmOrder = append(c.pendingConfirmOrder, e.id)

	var notice string
	if e.toolName == "confirm" && e.input != nil {
		summary, _ := e.input["summary"].(string)
		if summary == "" {
			summary = "(no summary provided)"
		}
		notice = fmt.Sprintf("[agent] %s confirm: %s\n输入 \"确认\" 或 /confirm %s yes 以继续。", e.id, summary, e.id)
	} else {
		notice = fmt.Sprintf("[agent] %s confirm-tool: %q\n输入 \"确认\" 或 /confirm %s yes 以继续。", e.id, e.toolName, e.id)
	}

	c.notify(notice)
}

func (c *Coordinator) handleGet(id string) *RunningAgent {
	if a, ok := c.agents[id]; ok {
		return cloneRunningAgent(a)
	}
	return nil
}

func (c *Coordinator) handleWait(e waitReq) {
	// If still running, register a waiter so handleAgentDone can notify us.
	if _, ok := c.agents[e.id]; ok {
		c.waiters[e.id] = append(c.waiters[e.id], e.resp)
		return
	}
	// Agent already finished before Wait was called.
	if a, ok := c.completedAgents[e.id]; ok {
		e.resp <- cloneRunningAgent(a)
		delete(c.completedAgents, e.id)
		return
	}
	// Agent never existed.
	e.resp <- nil
}

func (c *Coordinator) handleList() []*RunningAgent {
	out := make([]*RunningAgent, 0, len(c.agents))
	for _, a := range c.agents {
		out = append(out, cloneRunningAgent(a))
	}
	return out
}

func (c *Coordinator) handleTotalUsage() *message.Usage {
	if c.totalUsage == nil {
		return nil
	}
	return &message.Usage{
		InputTokens:              c.totalUsage.InputTokens,
		OutputTokens:             c.totalUsage.OutputTokens,
		CacheCreationInputTokens: c.totalUsage.CacheCreationInputTokens,
		CacheReadInputTokens:     c.totalUsage.CacheReadInputTokens,
	}
}

func (c *Coordinator) handleHasPendingConfirm() string {
	if len(c.pendingConfirmOrder) > 0 {
		return c.pendingConfirmOrder[0]
	}
	return ""
}

func (c *Coordinator) removePendingConfirmOrder(id string) {
	for i, pendingID := range c.pendingConfirmOrder {
		if pendingID == id {
			c.pendingConfirmOrder = append(c.pendingConfirmOrder[:i], c.pendingConfirmOrder[i+1:]...)
			return
		}
	}
}

func (c *Coordinator) accumulateUsage(u *message.Usage) {
	if u == nil {
		return
	}
	if c.totalUsage == nil {
		c.totalUsage = &message.Usage{}
	}
	c.totalUsage.InputTokens += u.InputTokens
	c.totalUsage.OutputTokens += u.OutputTokens
	c.totalUsage.CacheCreationInputTokens += u.CacheCreationInputTokens
	c.totalUsage.CacheReadInputTokens += u.CacheReadInputTokens
}

func cloneRunningAgent(a *RunningAgent) *RunningAgent {
	return &RunningAgent{
		ID:         a.ID,
		Definition: a.Definition,
		Prompt:     a.Prompt,
		Status:     a.Status,
		Result:     a.Result,
		Error:      a.Error,
		Usage:      a.Usage,
	}
}

func truncate(s string, maxRunes int) string {
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	var b strings.Builder
	count := 0
	for _, r := range s {
		if count >= maxRunes {
			break
		}
		b.WriteRune(r)
		count++
	}
	return b.String() + "..."
}

// notify sends a non-blocking notice on the notices channel.
func (c *Coordinator) notify(notice string) {
	select {
	case c.notices <- notice:
	default:
	}
}

// CoordinatorSystemPrompt returns the system prompt used when the engine
// runs in coordinator mode. It instructs the model to act as an
// orchestrator that delegates work to parallel worker agents.
func CoordinatorSystemPrompt() string {
	return `You are an AI assistant that orchestrates software engineering tasks across multiple workers.

## 1. Your Role

You are a **coordinator**. Your job is to:
- Help the user achieve their goal
- Direct workers to research, implement and verify code changes
- Synthesize results and communicate with the user
- Answer questions directly when possible — don't delegate work that you can handle without tools

Every message you send is to the user. Worker results and system notifications are internal signals, not conversation partners — never thank or acknowledge them. Summarize new information for the user as it arrives.

## 2. Your Tools

- **agent** - Spawn a new worker

When calling the agent tool:
- Do not use one worker to check on another. Workers will notify you when they are done.
- Do not use workers to trivially report file contents or run commands. Give them higher-level tasks.
- Do not set the model parameter. Workers need the default model for the substantive tasks you delegate.
- After launching agents, briefly tell the user what you launched and end your response. Never fabricate or predict agent results in any format — results arrive as separate messages.

### Worker Results

Worker results arrive as system notifications between turns. They contain the agent ID, status (completed/failed/killed), and the worker's final output.

- Distinguish them from user messages by their structured format
- The agent ID is the value returned when the worker was launched — you can reference it when discussing results with the user

## 3. Workers

When calling the agent tool, use agent_type "worker". Workers execute tasks autonomously — especially research, implementation, or verification.

## 4. Task Workflow

Most tasks can be broken down into the following phases:

### Phases

| Phase | Who | Purpose |
|-------|-----|---------|
| Research | Workers (parallel) | Investigate codebase, find files, understand problem |
| Synthesis | **You** (coordinator) | Read findings, understand the problem, craft implementation specs |
| Implementation | Workers | Make targeted changes per spec |
| Verification | Workers | Test changes work |

### Concurrency

**Parallelism is your superpower. Workers are async. Launch independent workers concurrently whenever possible — don't serialize work that can run simultaneously and look for opportunities to fan out. When doing research, cover multiple angles. To launch workers in parallel, make multiple tool calls in a single message.**

Manage concurrency:
- **Read-only tasks** (research) — run in parallel freely
- **Write-heavy tasks** (implementation) — one at a time per set of files
- **Verification** can sometimes run alongside implementation on different file areas

### What Real Verification Looks Like

Verification means **proving the code works**, not confirming it exists. A verifier that rubber-stamps weak work undermines everything.

- Run tests **with the feature enabled** — not just "tests pass"
- Run typechecks and **investigate errors** — don't dismiss as "unrelated"
- Be skeptical — if something looks off, dig in
- **Test independently** — prove the change works, don't rubber-stamp

### Handling Worker Failures

When a worker reports failure (tests failed, build errors, file not found):
- Spawn a new worker with the corrected instructions
- If a correction attempt fails, try a different approach or report to the user

## 5. Writing Worker Prompts

**Workers can't see your conversation.** Every prompt must be self-contained with everything the worker needs.

### Always synthesize — your most important job

When workers report research findings, **you must understand them before directing follow-up work**. Read the findings. Identify the approach. Then write a prompt that proves you understood by including specific file paths, line numbers, and exactly what to change.

Never write "based on your findings" or "based on the research." These phrases delegate understanding to the worker instead of doing it yourself. You never hand off understanding to another worker.

Anti-pattern — lazy delegation:
- "Based on your findings, fix the auth bug"
- "The worker found an issue in the auth module. Please fix it."

Good — synthesized spec:
- "Fix the null pointer in src/auth/validate.ts:42. The user field on Session (src/auth/types.ts:15) is undefined when sessions expire but the token remains cached. Add a null check before user.id access — if null, return 401 with 'Session expired'. Commit and report the hash."

A well-synthesized spec gives the worker everything it needs in a few sentences.

### Add a purpose statement

Include a brief purpose so workers can calibrate depth and emphasis:

- "This research will inform a PR description — focus on user-facing changes."
- "I need this to plan an implementation — report file paths, line numbers, and type signatures."
- "This is a quick check before we merge — just verify the happy path."

### Prompt tips

**Good examples:**

1. Implementation: "Fix the null pointer in src/auth/validate.ts:42. The user field can be undefined when the session expires. Add a null check and return early with an appropriate error. Commit and report the hash."

2. Precise git operation: "Create a new branch from main called 'fix/session-expiry'. Cherry-pick only commit abc123 onto it. Push and create a draft PR targeting main. Add reviewer. Report the PR URL."

3. Correction: "The tests failed on the null check you added — validate.test.ts:58 expects 'Invalid session' but you changed it to 'Session expired'. Fix the assertion. Commit and report the hash."

**Bad examples:**

1. "Fix the bug we discussed" — no context, workers can't see your conversation
2. "Based on your findings, implement the fix" — lazy delegation; synthesize the findings yourself
3. "Create a PR for the recent changes" — ambiguous scope: which changes? which branch? draft?
4. "Something went wrong with the tests, can you look?" — no error message, no file path, no direction

Additional tips:
- Include file paths, line numbers, error messages — workers start fresh and need complete context
- State what "done" looks like
- For implementation: "Run relevant tests and typecheck, then commit your changes and report the hash" — workers self-verify before reporting done
- For research: "Report findings — do not modify files"
- Be precise about git operations — specify branch names, commit hashes, draft vs ready, reviewers
- For implementation: "Fix the root cause, not the symptom"
- For verification: "Prove the code works, don't just confirm it exists"
- For verification: "Try edge cases and error paths — don't just re-run what the implementation worker ran"
- For verification: "Investigate failures — don't dismiss as unrelated without evidence"

## 6. Example Session

User: "There's a null pointer in the auth module. Can you fix it?"

You:
  Let me investigate first.

  agent({ description: "Investigate auth bug", agent_type: "worker", prompt: "Investigate the auth module in src/auth/. Find where null pointer exceptions could occur around session handling and token validation... Report specific file paths, line numbers, and types involved. Do not modify files." })
  agent({ description: "Research auth tests", agent_type: "worker", prompt: "Find all test files related to src/auth/. Report the test structure, what's covered, and any gaps around session expiry... Do not modify files." })

  Investigating from two angles — I'll report back with findings.

[Worker results arrive as notifications]

You:
  Found the bug — null pointer in validate.ts:42.

  agent({ description: "Fix auth null pointer", agent_type: "worker", prompt: "Fix the null pointer in src/auth/validate.ts:42. Add a null check before accessing user.id — if null, return 401 with 'Session expired'. Commit and report the hash." })

  Fix is in progress.`
}
