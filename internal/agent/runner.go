// Package agent hosts agent-mode building blocks. A Runner drives a single
// sub-agent's LLM event loop. The Coordinator (see coordinator.go) owns the
// per-agent registry and forwards runner events to its Notifications channel
// so the parent engine can inject them into the conversation.
package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Jekinnnnnn/jekylan/internal/llm"
	"github.com/Jekinnnnnn/jekylan/internal/message"
	"github.com/Jekinnnnnn/jekylan/internal/query"
	"github.com/Jekinnnnnn/jekylan/internal/tool"
)

// pauseAndSummarizePrompt is injected into every sub-agent system prompt.
const pauseAndSummarizePrompt = `## Pause & Summarize

When you encounter any of the following situations, you must pause the current workflow, summarize what you have just accomplished in concise language, then stop executing and wait for the user's next message. Do not use any tools.

### Triggers for Pausing

**1. Batch operations all succeeded**
When multiple identical operations (e.g., creating todos, sending messages, pushing data) have all completed, do not output raw JSON or RPC responses one by one. Instead, summarize them into a single concise statement.

- Good: "15 todos created successfully."
- Good: "Sending 8 webhook notifications"
- Bad: Outputting each individual {"errcode":0,"errmsg":"ok",...} JSON block
- Bad: Outputting unprocessed MCP RPC raw responses

**2. File operations completed**
After using tools such as file_write or file_edit, you must pause and report which files were modified and the nature of the changes.

- Good: "Updating rent_data.csv with corrected water meter columns"
- Good: "Writing validate.ts with null check fix"
- Bad: Silently proceeding to the next step without informing the user
- Bad: Outputting the full unprocessed diff as the final result

**3. Awaiting user confirmation**
When an operation requires user confirmation before proceeding, call the ` + "`confirm`" + ` tool with a ` + "`summary`" + ` parameter describing what was done and what happens next. The tool will block until the user confirms, then return so you can continue.

- Good: Calling ` + "`confirm`" + ` with summary="CSV corrected. Water meter data for floors 2-4 and rooms 101-203 aligned. After confirmation, I will populate the Excel template."
- Bad: Proceeding directly to the next step without waiting for the user's reply
- Bad: Outputting text asking for confirmation without actually calling ` + "`confirm`" + ` (text alone does not block)

### Summary Format Requirements

- Opening phrase: Use present participle (-ing) or concise completion tense, 3–10 words summarizing the action just performed
- Batch results: Use the "count + action + result" format; do not expand every individual detail
- File operations: State the file name and nature of the change; do not list every line of the diff
- Confirmation request: Must include an explicit phrase such as "Please confirm... I will proceed after confirmation", so the user knows you are waiting

### Behavior After Pausing

After outputting the summary, stop immediately. Do not call any tools, and do not proactively advance to the next step. Wait for the user's next message.`

// QueryRunner executes an LLM query with full params. In production this is query.Query.
type QueryRunner func(ctx context.Context, params query.Params) <-chan query.Event

// TranscriptFunc returns a copy of the agent's current message slice. The
// caller is responsible for any locking required to make this safe.
type TranscriptFunc func() []message.Message

// RunnerOptions configures a single sub-agent execution.
type RunnerOptions struct {
	Definition     *Definition
	Transcript     TranscriptFunc
	Tools          *tool.Registry // parent registry; will be filtered by Definition
	Client         llm.Client
	Model          string
	ThinkingBudget int64
	QueryRunner    QueryRunner
	// ClientFactory builds a fresh LLM client when Definition.Model overrides
	// the engine's default Model. nil means model overrides reuse the default
	// Client (the LLM call may then mismatch the requested model).
	ClientFactory func(model string) (llm.Client, error)
	// Fork, when true, builds the initial message list from the parent's
	// transcript via BuildForkMessages instead of a fresh user prompt.
	Fork bool
	// DisableCompact prevents context compaction inside the sub-agent query.
	DisableCompact bool
	// InjectPausePrompt controls whether the pause-and-summarize system prompt
	// is appended to every sub-agent. Defaults to true for backward compatibility.
	InjectPausePrompt bool
}

func (o RunnerOptions) effectiveMaxTurns() int {
	if o.Definition != nil {
		return o.Definition.EffectiveMaxTurns()
	}
	return 3
}

// ConfirmRequest is sent by a Runner when it needs user approval for a risky
// tool call. The consumer must send a response on RespCh.
type ConfirmRequest struct {
	ToolName string
	Input    map[string]any
	RespCh   chan ConfirmResponse
}

// ConfirmResponse is the user's approval decision for a ConfirmRequest.
type ConfirmResponse struct {
	Approved bool
}

// Runner executes a sub-agent query loop and yields progress / result events.
type Runner struct {
	opt  RunnerOptions
	msgs []message.Message

	// confirmReqCh carries confirmation requests from the runner to the
	// coordinator. Buffered so the runner never blocks on send.
	confirmReqCh chan ConfirmRequest
}

// NewRunner creates a Runner. QueryRunner must not be nil.
func NewRunner(opt RunnerOptions) *Runner {
	if opt.QueryRunner == nil {
		panic("agent.NewRunner: QueryRunner is required")
	}
	return &Runner{opt: opt, confirmReqCh: make(chan ConfirmRequest, 4)}
}

// Confirmations returns the channel of confirmation requests produced when
// the runner encounters a risky tool call.
func (r *Runner) Confirmations() <-chan ConfirmRequest { return r.confirmReqCh }

// effectiveClient picks the LLM client + model to use for this run. If the
// Definition pins a Model that differs from the engine's default and a
// ClientFactory is provided, it builds a fresh client. On factory failure
// it falls back to the default client+model rather than panicking, so the
// query layer can surface the error like any other API failure.
func (r *Runner) effectiveClient() (llm.Client, string) {
	defModel := ""
	if r.opt.Definition != nil {
		defModel = r.opt.Definition.Model
	}
	if defModel == "" || defModel == r.opt.Model {
		return r.opt.Client, r.opt.Model
	}
	if r.opt.ClientFactory == nil {
		return r.opt.Client, r.opt.Model
	}
	c, err := r.opt.ClientFactory(defModel)
	if err != nil || c == nil {
		return r.opt.Client, r.opt.Model
	}
	return c, defModel
}

// filteredTools returns the parent tool registry narrowed by the agent's
// Definition.ToolsAllow / ToolsDeny, with the confirm tool always included.
func (r *Runner) filteredTools() *tool.Registry {
	var base *tool.Registry
	if r.opt.Tools != nil && r.opt.Definition != nil {
		base = r.opt.Tools.Subset(r.opt.Definition.ToolsAllow, r.opt.Definition.ToolsDeny)
	} else {
		base = r.opt.Tools
	}
	if base == nil {
		return base
	}
	// Inject the confirm blocking tool so every sub-agent can request
	// user confirmation with a contextual summary.
	if base.Find("confirm") == nil {
		base = tool.NewRegistry(append(base.All(), tool.ConfirmBlockTool{})...)
	}
	return base
}

const (
	RunEventProgress = "progress"
	RunEventComplete = "complete"
	RunEventError    = "error"
)

// Query event types consumed from the query package.
const (
	queryEventResult      = "result"
	queryEventError       = "error"
	queryEventUserMessage = "user_message"
	queryEventUsage       = "usage"
)

// RunEvent represents an output from the sub-agent execution.
type RunEvent struct {
	Type   string // progress, complete, error
	Text   string // assistant text delta (progress)
	Result string // final result text (complete)
	Usage  *message.Usage
	Error  string
}

// ResultConsumer defines how to consume runner output events and extract the
// final result. Different agent types can provide custom consumers.
type ResultConsumer interface {
	Consume(events <-chan RunEvent) (ConsumerResult, error)
}

// ConsumerResult is the final result after consuming a RunEvent stream.
type ConsumerResult struct {
	Text  string
	Usage *message.Usage
}

// DefaultResultConsumer is the default implementation: consumes the RunEvent
// channel and collects the Result from complete events (or Error from error
// events).
type DefaultResultConsumer struct{}

// Consume implements ResultConsumer.
func (d DefaultResultConsumer) Consume(events <-chan RunEvent) (ConsumerResult, error) {
	var result string
	var usage *message.Usage
	var runErr string
	for evt := range events {
		switch evt.Type {
		case RunEventComplete:
			result = evt.Result
			usage = evt.Usage
		case RunEventError:
			runErr = evt.Error
		}
	}
	if runErr != "" {
		return ConsumerResult{}, errors.New(runErr)
	}
	return ConsumerResult{Text: result, Usage: usage}, nil
}

// Run starts the sub-agent execution. The returned channel is closed when
// the run finishes (complete, error, or context cancellation).
func (r *Runner) Run(ctx context.Context, prompt string) <-chan RunEvent {
	out := make(chan RunEvent)
	go func() {
		defer close(out)
		defer close(r.confirmReqCh)
		r.run(ctx, prompt, out)
	}()
	return out
}

func (r *Runner) run(ctx context.Context, prompt string, out chan<- RunEvent) {
	// Build messages from transcript + user prompt (or fork mode).
	if r.opt.Fork && r.opt.Transcript != nil {
		// Fork mode: preserve the full parent transcript including the
		// agent tool_use that triggered the fork.
		r.msgs = BuildForkMessages(r.opt.Transcript(), prompt)
	} else {
		if r.opt.Transcript != nil {
			r.msgs = dropOrphanToolUses(r.opt.Transcript())
		}
		userMsg := message.Message{Role: message.RoleUser, Timestamp: time.Now()}
		userMsg.AddText(prompt)
		r.msgs = append(r.msgs, userMsg)
	}
	paramMsgs := append([]message.Message(nil), r.msgs...)

	// Filter tools according to the agent definition.
	filteredTools := r.filteredTools()

	// Build system prompt. InjectPausePrompt field exists but is not yet wired
	// into all callers; keep injecting for backward compatibility.
	sysPrompt := buildAgentSystemPrompt(r.opt.Definition, filteredTools, true)

	client, model := r.effectiveClient()
	maxTurns := r.opt.effectiveMaxTurns()
	if r.opt.Definition != nil {
		fmt.Fprintf(os.Stderr, "[agent] %s max_turns=%d effective=%d\n", r.opt.Definition.Name, r.opt.Definition.MaxTurns, maxTurns)
	} else {
		fmt.Fprintf(os.Stderr, "[agent] default max_turns=%d effective=%d\n", 0, maxTurns)
	}
	params := query.Params{
		Messages:       paramMsgs,
		SystemPrompt:   sysPrompt,
		Tools:          filteredTools,
		Model:          model,
		Client:         client,
		ThinkingBudget: r.opt.ThinkingBudget,
		MaxTurns:       maxTurns,
		DisableCompact: r.opt.DisableCompact,
		QuerySource:    query.QuerySourceAgent,
		ConfirmTool: func(ctx context.Context, toolName string, input map[string]any) (bool, error) {
			req := ConfirmRequest{
				ToolName: toolName,
				Input:    input,
				RespCh:   make(chan ConfirmResponse, 1),
			}
			select {
			case r.confirmReqCh <- req:
			case <-ctx.Done():
				return false, ctx.Err()
			}
			select {
			case resp := <-req.RespCh:
				return resp.Approved, nil
			case <-ctx.Done():
				return false, ctx.Err()
			}
		},
	}

	var usageMsgs []message.Message
	var finalResult query.Result
	var hasResult bool

	for evt := range r.opt.QueryRunner(ctx, params) {
		switch evt.Type {
		case queryEventResult:
			finalResult = evt.Result
			hasResult = true
		case queryEventError:
			out <- RunEvent{Type: RunEventError, Error: evt.Result.Error}
			return
		case queryEventUserMessage:
			r.msgs = append(r.msgs, evt.Message)
			// If the user rejected a confirm tool call, treat the run as failed
			// so the playbook stops instead of continuing to the next step.
			for _, tr := range evt.Message.ToolResults() {
				if tr.IsError && strings.Contains(tr.Content, "not approved by user") {
					for i := len(r.msgs) - 2; i >= 0; i-- {
						for _, tu := range r.msgs[i].ToolUses() {
							if tu.ID == tr.ToolUseID && tu.Name == "confirm" {
								out <- RunEvent{Type: RunEventError, Error: "confirmation cancelled by user"}
								return
							}
						}
					}
				}
			}
		case queryEventUsage:
			r.msgs = append(r.msgs, evt.Message)
			usageMsgs = append(usageMsgs, evt.Message)
		}
	}

	if ctx.Err() != nil {
		return // cancelled — not an error
	}

	if !hasResult {
		out <- RunEvent{Type: RunEventError, Error: "query ended without result"}
		return
	}
	if !finalResult.Success {
		out <- RunEvent{Type: RunEventError, Error: finalResult.Error}
		return
	}

	// Extract final result text.
	resultText := strings.TrimSpace(finalResult.Text)
	if resultText == "" {
		resultText = extractLastResultText(r.msgs)
	}

	out <- RunEvent{
		Type:   RunEventComplete,
		Result: resultText,
		Usage:  extractUsage(usageMsgs),
	}
}

