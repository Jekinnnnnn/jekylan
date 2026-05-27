package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/Jekinnnnnn/jekylan/internal/agent"
	"github.com/Jekinnnnnn/jekylan/internal/compact"
	"github.com/Jekinnnnnn/jekylan/internal/config"
	"github.com/Jekinnnnnn/jekylan/internal/engine"
	"github.com/Jekinnnnnn/jekylan/internal/llm"
	"github.com/Jekinnnnnn/jekylan/internal/memory"
	"github.com/Jekinnnnnn/jekylan/internal/message"
	"github.com/Jekinnnnnn/jekylan/internal/playbook"
	"github.com/Jekinnnnnn/jekylan/internal/query"
	"github.com/Jekinnnnnn/jekylan/internal/skill"
	"github.com/Jekinnnnnn/jekylan/internal/tool"
	"github.com/chzyer/readline"
)

func main() {
	var cfgPath = flag.String("config", "config.yaml", "Path to YAML config file")
	var prompt = flag.String("p", "", "User prompt (single-shot mode)")
	var mode = flag.String("mode", "default", "Operation mode: default | coordinator | single-shot")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	apiKey := os.Getenv("JEKYLAN_API_KEY")
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "API key not set. Set JEKYLAN_API_KEY environment variable.")
		os.Exit(1)
	}

	compactOpts := compact.Options{
		TimeBasedMCGap:                cfg.TimeBasedMCGap,
		TimeBasedMCKeep:               cfg.TimeBasedMCKeep,
		DisableTimeBasedMC:            cfg.DisableTimeBasedMC,
		UseAPIClearToolResults:        cfg.UseAPIClearToolResults,
		UseAPIClearToolUses:           cfg.UseAPIClearToolUses,
		APIMaxInputTokens:             cfg.APIMaxInputTokens,
		APITargetInputTokens:          cfg.APITargetInputTokens,
		AutoCompactWindow:             cfg.AutoCompactWindow,
		AutoCompactPctOverride:        cfg.AutoCompactPctOverride,
		BlockingLimitOverride:         cfg.BlockingLimitOverride,
		DisableCompact:                cfg.DisableCompact,
		DisableAutoCompact:            cfg.DisableAutoCompact,
		SMCompactMinTokens:            cfg.SMCompactMinTokens,
		SMCompactMinTextBlockMessages: cfg.SMCompactMinTextBlockMessages,
		SMCompactMaxTokens:            cfg.SMCompactMaxTokens,
		EnableSMCompact:               cfg.EnableSMCompact,
	}
	if cfg.EnableMemory && compactOpts.SessionMemoryPath == "" {
		compactOpts.SessionMemoryPath = memory.GetSessionMemoryPath(memory.GetMemoryDir(cfg.MemoryDir))
	}
	compact.SetOptions(compactOpts)

	client, err := llm.DefaultFactory.NewClient(string(cfg.Provider), cfg.Model, apiKey, cfg.BaseURL, cfg.StreamMaxTokens)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create client: %v\n", err)
		os.Exit(1)
	}
	if client == nil {
		fmt.Fprintln(os.Stderr, "Unsupported provider")
		os.Exit(1)
	}

	var skillRegistry *skill.Registry
	if cfg.SkillsDir != "" {
		skillRegistry, err = skill.LoadDir(cfg.SkillsDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to load skills: %v\n", err)
			os.Exit(1)
		}
		if len(skillRegistry.All()) > 0 {
			fmt.Fprintf(os.Stderr, "Loaded %d skills from %s\n", len(skillRegistry.All()), cfg.SkillsDir)
		}
	}

	// Apply defaults for directory paths (without mutating cfg).
	agentsDir := cfg.AgentsDir
	if agentsDir == "" {
		agentsDir = "agents"
	}
	playbookDir := cfg.PlaybookDir
	if playbookDir == "" {
		playbookDir = "playbooks"
	}

	// Load agent definitions (built-in + project-level).
	agentRegistry := agent.NewBuiltinRegistry()
	if err := agentRegistry.LoadFromDir(agentsDir); err != nil {
		fmt.Fprintf(os.Stderr, "[agent] failed to load project agents: %v\n", err)
	}

	// Load playbooks.
	playbookRegistry, err := playbook.LoadDir(playbookDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[playbook] failed to load playbooks: %v\n", err)
	}
	if len(playbookRegistry.All()) > 0 {
		fmt.Fprintf(os.Stderr, "Loaded %d playbooks\n", len(playbookRegistry.All()))
	}

	// Coordinator: single point of management for background sub-agents.
	coord := agent.NewCoordinator(agent.WithDebug(cfg.Debug))
	defer coord.Stop()

	// EngineDriver: drives the engine REPL loop in coordinator mode.
	driver := agent.NewEngineDriver()
	defer driver.Stop()

	// Mode selection: default | coordinator | playbook | single-shot
	modes := config.BuiltinModes()
	selectedMode, ok := modes[*mode]
	if !ok {
		selectedMode = modes["default"]
	}

	switch *mode {
	case "coordinator":
		cfg.SystemPrompt = agent.CoordinatorSystemPrompt()
		fmt.Fprintln(os.Stderr, "Coordinator mode enabled")
	case "playbook":
		cfg.SystemPrompt = agent.PassiveCoordinatorSystemPrompt()
		fmt.Fprintln(os.Stderr, "Playbook mode enabled")
	}

	if selectedMode.MaxTurns > 0 {
		cfg.MaxTurns = selectedMode.MaxTurns
	}

	// Create the Agent tool (ParentTools wired after engine creation).
	// Use a pointer so the copy stored in the tool registry receives updates.
	agentTool := &agent.AgentTool{
		AgentRegistry:   agentRegistry,
		Coordinator:     coord,
		Client:          client,
		Model:           cfg.Model,
		ThinkingBudget:  cfg.ThinkingBudget,
		ClientFactory:   llm.DefaultFactory.NewClientFunc(string(cfg.Provider), apiKey, cfg.BaseURL, cfg.StreamMaxTokens),
		CoordinatorMode: *mode == "coordinator",
	}

	spawner := &agent.Spawner{
		Coord:          coord,
		AgentReg:       agentRegistry,
		Client:         client,
		Model:          cfg.Model,
		ThinkingBudget: cfg.ThinkingBudget,
		ClientFactory:  llm.DefaultFactory.NewClientFunc(string(cfg.Provider), apiKey, cfg.BaseURL, cfg.StreamMaxTokens),
	}

	// Determine denied tools based on mode.
	var deniedTools []string
	switch *mode {
	case "coordinator", "playbook":
		deniedTools = []string{"bash", "file_read", "file_write", "file_edit", "grep", "glob"}
	}
	if *mode == "playbook" {
		deniedTools = append(deniedTools, "agent")
	}

	// Sub-agents need the full tool registry even in coordinator/playbook mode,
	// because they are the ones that actually perform file/bash operations.
	fullRegistry := buildToolRegistry(cfg.Tools, skillRegistry, playbookRegistry, agentTool, []string{}, coord)
	registry := buildToolRegistry(cfg.Tools, skillRegistry, playbookRegistry, agentTool, deniedTools, coord)

	var opts []engine.EngineOption
	opts = append(opts, engine.WithTokenBudget(cfg.TokenBudget))
	if cfg.EnableMemory {
		opts = append(opts, engine.WithMemoryDir(cfg.MemoryDir))
	}

	sessionPath := cfg.SessionFile
	if sessionPath == "" {
		sessionPath = memory.GetSessionPath(memory.GetMemoryDir(cfg.MemoryDir))
	}
	opts = append(opts, engine.WithSessionPath(sessionPath))
	if *mode == "coordinator" || *mode == "playbook" {
		opts = append(opts, engine.WithCoordinatorMode(true))
	}

	eng := engine.NewEngine(registry, cfg.Model, cfg.MaxTurns, cfg.ThinkingBudget, cfg.SystemPrompt, client, cfg.DisableCompact, opts...)

	// Wire the Agent tool to the engine's transcript and tool registry.
	// Sub-agents always get the full registry so they can use bash/file tools.
	agentTool.Transcript = func() []message.Message { return eng.Messages() }
	agentTool.ParentTools = fullRegistry
	spawner.ParentTools = fullRegistry

	// Bridge sub-agent usage into the engine so session files include totals.
	coord.SetUsageSink(eng.AddUsage)

	ctx := context.Background()

	switch *mode {
	case "single-shot":
		if *prompt == "" {
			fmt.Fprintln(os.Stderr, "single-shot mode requires -p prompt")
			os.Exit(1)
		}
		runSingleShot(ctx, eng, sessionPath, *prompt)
	case "coordinator":
		if *prompt != "" {
			runSingleShot(ctx, eng, sessionPath, *prompt)
			return
		}
		runCoordinatorREPL(ctx, driver, coord, eng, sessionPath, playbookRegistry, spawner)
	case "playbook":
		if *prompt != "" {
			runSingleShot(ctx, eng, sessionPath, *prompt)
			return
		}
		runPlaybookREPL(ctx, driver, coord, eng, sessionPath, playbookRegistry, spawner)
	default:
		if *prompt != "" {
			runSingleShot(ctx, eng, sessionPath, *prompt)
			return
		}
		runREPL(ctx, coord, eng, sessionPath, playbookRegistry, spawner)
	}
}

func runSingleShot(ctx context.Context, eng *engine.Engine, sessionPath, prompt string) {
	if sessionPath != "" {
		if err := eng.LoadSession(sessionPath); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to load session: %v\n", err)
		} else if len(eng.Messages()) > 0 {
			fmt.Fprintf(os.Stderr, "Loaded %d messages from session\n", len(eng.Messages()))
		}
	}

	fmt.Println("=== Assistant ===")
	var result query.Result

	err := eng.Run(engine.RunOptions{
		OnOutput:    func(s string) { fmt.Print(s) },
		OnResult:    func(r query.Result) { result = r },
		Context:     ctx,
		SessionPath: sessionPath,
		Prompt:      prompt,
	})

	fmt.Println("\n=== Result ===")
	if result.Success {
		fmt.Printf("Success | turns=%d | stop_reason=%s | %s\n", result.NumTurns, result.StopReason, eng.TotalUsage())
	} else {
		fmt.Printf("Error: %s | turns=%d | %s\n", result.Error, result.NumTurns, eng.TotalUsage())
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Engine error: %v\n", err)
	}
}

func runREPL(ctx context.Context, coord *agent.Coordinator, eng *engine.Engine, sessionPath string, playbookReg *playbook.Registry, spawner agent.AgentSpawner) {
	recoverSession(eng, sessionPath)

	rl, err := readline.New("> ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize readline: %v\n", err)
		return
	}
	defer rl.Close()

	fmt.Println("=== jekylan REPL ===")
	fmt.Println("Commands: /stop, /reset, /quit, /exit, /compact-skill-exp <skill name>, /playbook <name>, /agents, /stop-agent <id>")
	fmt.Println()

	// Wrap Readline to intercept /playbook commands and forward confirmations.
	readInput := func() (string, error) {
		for {
			line, err := rl.Readline()
			if err != nil {
				return line, err
			}
			if after, ok := strings.CutPrefix(line, "/playbook "); ok {
				name := strings.TrimSpace(after)
				if name == "" {
					fmt.Println("Usage: /playbook <name>")
					continue
				}
				p := playbookReg.Find(name)
				if p == nil {
					fmt.Printf("Playbook not found: %s\n", name)
					continue
				}
				plan, err := playbook.ParsePlan(p.Content)
				if err != nil {
					fmt.Printf("Failed to parse playbook: %v\n", err)
					continue
				}
				go func() {
					originalTools := eng.GetTools()
					filteredTools := originalTools.Subset([]string{"*"}, []string{"agent"})
					eng.SetTools(filteredTools)
					defer func() {
						eng.SetTools(originalTools)
					}()

					executor := playbook.NewExecutor(spawner)
					vars, err := executor.Execute(ctx, plan)
					if err != nil {
						fmt.Printf("Playbook failed: %v\n", err)
						return
					}
					fmt.Printf("Playbook %q completed.\n", name)
					for k, v := range vars {
						fmt.Printf("- %s: %s\n", k, v)
					}
				}()
				continue
			}
			if line == "/agents" {
				agents := coord.List()
				if len(agents) == 0 {
					fmt.Println("No running agents.")
				} else {
					for _, a := range agents {
						fmt.Printf("[agent] %s status=%s\n", a.ID, a.Status)
					}
				}
				continue
			}
			if after, ok := strings.CutPrefix(line, "/stop-agent "); ok {
				id := strings.TrimSpace(after)
				if id == "" {
					fmt.Println("Usage: /stop-agent <id>")
				} else if coord.Kill(id) {
					fmt.Printf("Agent %s stopped.\n", id)
				} else {
					fmt.Printf("Agent %s not found or not running.\n", id)
				}
				continue
			}
			// Forward natural-language confirmations to pending agents.
			if id := coord.HasPendingConfirm(); id != "" {
				if isConfirmWord(line) {
					coord.Confirm(id, true)
					fmt.Printf("[confirm] forwarded to %s\n", id)
					if next := coord.HasPendingConfirm(); next != "" {
						fmt.Printf("[confirm] %s is also waiting for confirmation\n", next)
					}
					continue
				}
				if isCancelWord(line) {
					coord.Confirm(id, false)
					fmt.Printf("[confirm] cancelled for %s\n", id)
					if next := coord.HasPendingConfirm(); next != "" {
						fmt.Printf("[confirm] %s is also waiting for confirmation\n", next)
					}
					continue
				}
			}
			return line, nil
		}
	}

	// Start the engine loop; it blocks until /quit or signal.
	eng.Run(engine.RunOptions{
		ReadInput:  readInput,
		OnOutput:   func(s string) { fmt.Print(s) },
		OnQueryEnd: rl.Refresh,
		OnResult: func(r query.Result) {
			if r.Success {
				fmt.Printf("\n[turn complete | %s]\n", eng.TotalUsage())
			}
		},
		Context:     ctx,
		SessionPath: sessionPath,
	})

	fmt.Println("Goodbye.")
}

// runCoordinatorREPL drives a Coordinator-wrapped engine. The EngineDriver
// runs the engine event loop in its own goroutine; the main thread shuttles
// readline input into driver.Input() and prints whatever arrives on
// driver.Output(). Agent lifecycle notices from coord.Notices() are also
// printed to stdout and forwarded to the engine.
func runCoordinatorREPL(ctx context.Context, driver *agent.EngineDriver, coord *agent.Coordinator, eng *engine.Engine, sessionPath string, playbookReg *playbook.Registry, spawner agent.AgentSpawner) {
	recoverSession(eng, sessionPath)

	rl, err := readline.New("> ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize readline: %v\n", err)
		return
	}
	defer rl.Close()

	fmt.Println("=== jekylan REPL (coordinator) ===")
	fmt.Println("Commands: /stop, /reset, /quit, /exit, /compact-skill-exp <skill name>, /playbook <name>, /agents, /stop-agent <id>")
	fmt.Println()

	driver.Start(ctx, eng, sessionPath)

	// Drain engine output to stdout.
	go func() {
		for s := range driver.Output() {
			fmt.Print(s)
			rl.Refresh()
		}
		rl.Close()
	}()

	// Forward agent lifecycle notices to the engine. The engine injects them
	// as system messages and also prints them via OnOutput.
	go func() {
		for notice := range coord.Notices() {
			eng.Notify(notice)
			rl.Refresh()
		}
	}()

	for {
		line, err := rl.Readline()
		if err != nil {
			break
		}
		if after, ok := strings.CutPrefix(line, "/playbook "); ok {
			name := strings.TrimSpace(after)
			if name == "" {
				fmt.Println("Usage: /playbook <name>")
				continue
			}
			p := playbookReg.Find(name)
			if p == nil {
				fmt.Printf("Playbook not found: %s\n", name)
				continue
			}
			plan, err := playbook.ParsePlan(p.Content)
			if err != nil {
				fmt.Printf("Failed to parse playbook: %v\n", err)
				continue
			}
			// Run playbook in the background so the loop can keep reading
			// confirmations and other input while agents are working.
			go func() {
				originalTools := eng.GetTools()
				filteredTools := originalTools.Subset([]string{"*"}, []string{"agent"})
				eng.SetTools(filteredTools)
				defer func() {
					eng.SetTools(originalTools)
				}()

				executor := playbook.NewExecutor(spawner)
				vars, err := executor.Execute(ctx, plan)
				if err != nil {
					fmt.Printf("Playbook failed: %v\n", err)
					return
				}
				fmt.Printf("Playbook %q completed.\n", name)
				for k, v := range vars {
					fmt.Printf("- %s: %s\n", k, v)
				}
			}()
			continue
		}
		if line == "/agents" {
			agents := coord.List()
			if len(agents) == 0 {
				fmt.Println("No running agents.")
			} else {
				for _, a := range agents {
					fmt.Printf("[agent] %s status=%s\n", a.ID, a.Status)
				}
			}
			continue
		}
		if after, ok := strings.CutPrefix(line, "/stop-agent "); ok {
			id := strings.TrimSpace(after)
			if id == "" {
				fmt.Println("Usage: /stop-agent <id>")
			} else if coord.Kill(id) {
				fmt.Printf("Agent %s stopped.\n", id)
			} else {
				fmt.Printf("Agent %s not found or not running.\n", id)
			}
			continue
		}
		// Intercept natural-language confirmations and forward to
		// pending agent confirmations.
		if id := coord.HasPendingConfirm(); id != "" {
			if isConfirmWord(line) {
				coord.Confirm(id, true)
				fmt.Printf("[confirm] forwarded to %s\n", id)
				if next := coord.HasPendingConfirm(); next != "" {
					fmt.Printf("[confirm] %s is also waiting for confirmation\n", next)
				}
				continue
			}
			if isCancelWord(line) {
				coord.Confirm(id, false)
				fmt.Printf("[confirm] cancelled for %s\n", id)
				if next := coord.HasPendingConfirm(); next != "" {
					fmt.Printf("[confirm] %s is also waiting for confirmation\n", next)
				}
				continue
			}
		}
		driver.Input() <- line
	}
	close(driver.Input())
	<-driver.Done()

	fmt.Println("Goodbye.")
}

// runPlaybookREPL drives a playbook-mode coordinator. It is similar to
// runCoordinatorREPL but the engine starts with a passive system prompt
// and no agent tool, so the coordinator cannot spawn agents on its own.
// Playbook steps still spawn agents via the Spawner directly.
func runPlaybookREPL(ctx context.Context, driver *agent.EngineDriver, coord *agent.Coordinator, eng *engine.Engine, sessionPath string, playbookReg *playbook.Registry, spawner agent.AgentSpawner) {
	recoverSession(eng, sessionPath)

	rl, err := readline.New("> ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize readline: %v\n", err)
		return
	}
	defer rl.Close()

	fmt.Println("=== jekylan REPL (playbook mode) ===")
	fmt.Println("Commands: /stop, /quit, /exit, /playbook <name>, /agents, /stop-agent <id>")
	fmt.Println("Note: You cannot create agents manually in playbook mode.")
	fmt.Println()

	driver.Start(ctx, eng, sessionPath)

	// Drain engine output to stdout.
	go func() {
		for s := range driver.Output() {
			fmt.Print(s)
			rl.Refresh()
		}
		rl.Close()
	}()

	// Forward agent lifecycle notices to the engine.
	go func() {
		for notice := range coord.Notices() {
			eng.Notify(notice)
			rl.Refresh()
		}
	}()

	for {
		line, err := rl.Readline()
		if err != nil {
			break
		}
		if after, ok := strings.CutPrefix(line, "/playbook "); ok {
			name := strings.TrimSpace(after)
			if name == "" {
				fmt.Println("Usage: /playbook <name>")
				continue
			}
			p := playbookReg.Find(name)
			if p == nil {
				fmt.Printf("Playbook not found: %s\n", name)
				continue
			}
			plan, err := playbook.ParsePlan(p.Content)
			if err != nil {
				fmt.Printf("Failed to parse playbook: %v\n", err)
				continue
			}
			// Playbook mode already has no agent tool, so no need to filter here.
			go func() {
				executor := playbook.NewExecutor(spawner)
				vars, err := executor.Execute(ctx, plan)
				if err != nil {
					fmt.Printf("Playbook failed: %v\n", err)
					return
				}
				fmt.Printf("Playbook %q completed.\n", name)
				for k, v := range vars {
					fmt.Printf("- %s: %s\n", k, v)
				}
			}()
			continue
		}
		if line == "/agents" {
			agents := coord.List()
			if len(agents) == 0 {
				fmt.Println("No running agents.")
			} else {
				for _, a := range agents {
					fmt.Printf("[agent] %s status=%s\n", a.ID, a.Status)
				}
			}
			continue
		}
		if after, ok := strings.CutPrefix(line, "/stop-agent "); ok {
			id := strings.TrimSpace(after)
			if id == "" {
				fmt.Println("Usage: /stop-agent <id>")
			} else if coord.Kill(id) {
				fmt.Printf("Agent %s stopped.\n", id)
			} else {
				fmt.Printf("Agent %s not found or not running.\n", id)
			}
			continue
		}
		// Intercept natural-language confirmations and forward to
		// pending agent confirmations.
		if id := coord.HasPendingConfirm(); id != "" {
			if isConfirmWord(line) {
				coord.Confirm(id, true)
				fmt.Printf("[confirm] forwarded to %s\n", id)
				if next := coord.HasPendingConfirm(); next != "" {
					fmt.Printf("[confirm] %s is also waiting for confirmation\n", next)
				}
				continue
			}
			if isCancelWord(line) {
				coord.Confirm(id, false)
				fmt.Printf("[confirm] cancelled for %s\n", id)
				if next := coord.HasPendingConfirm(); next != "" {
					fmt.Printf("[confirm] %s is also waiting for confirmation\n", next)
				}
				continue
			}
		}
		driver.Input() <- line
	}
	close(driver.Input())
	<-driver.Done()

	fmt.Println("Goodbye.")
}

// recoverSession optionally loads an existing session and asks the user
// whether to continue with it. Used by both REPL modes.
func recoverSession(eng *engine.Engine, sessionPath string) {
	if sessionPath == "" {
		return
	}
	info, err := os.Stat(sessionPath)
	if err != nil || info.Size() == 0 {
		return
	}
	if err := eng.LoadSession(sessionPath); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load session: %v\n", err)
		return
	}
	if len(eng.Messages()) == 0 {
		return
	}
	printSessionSummary(eng.Messages())
	fmt.Print("Continue with this session? (y/n): ")
	var answer string
	if _, err := fmt.Scanln(&answer); err != nil {
		return
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		fmt.Fprintf(os.Stderr, "Loaded %d messages from session\n", len(eng.Messages()))
	default:
		eng.Reset()
		fmt.Fprintln(os.Stderr, "Previous session discarded. Starting fresh.")
	}
}

func buildToolRegistry(names []string, skillReg *skill.Registry, playbookReg *playbook.Registry, agentTool tool.Tool, deniedTools []string, coord *agent.Coordinator) *tool.Registry {
	tracker := tool.NewFileStateTracker()

	// Build the full tool set first.
	tools := []tool.Tool{
		tool.BashTool{},
		// Always register the workflow_complete tool so workflow skills can signal completion.
		tool.WorkflowCompleteTool{},
		tool.FileReadTool{Tracker: tracker},
		tool.FileWriteTool{Tracker: tracker},
		tool.FileEditTool{Tracker: tracker},
	}
	for _, name := range names {
		switch name {
		case "grep":
			tools = append(tools, tool.GrepTool{})
		case "glob":
			tools = append(tools, tool.GlobTool{})
		case "skill":
			tools = append(tools, tool.SkillTool{Registry: skillReg})
		case "agent":
			if agentTool != nil {
				tools = append(tools, agentTool)
			}
		}
	}
	if len(tools) == 0 {
		tools = []tool.Tool{tool.BashTool{}, tool.FileReadTool{Tracker: tracker}}
	}
	hasSkill := false
	for _, t := range tools {
		if t.Name() == "skill" {
			hasSkill = true
			break
		}
	}
	if !hasSkill && skillReg != nil && len(skillReg.All()) > 0 {
		tools = append(tools, tool.SkillTool{Registry: skillReg})
	}

	// Apply denied-tools filter.
	if len(deniedTools) > 0 {
		denySet := make(map[string]bool, len(deniedTools))
		for _, d := range deniedTools {
			denySet[d] = true
		}
		var filtered []tool.Tool
		for _, t := range tools {
			if !denySet[t.Name()] {
				filtered = append(filtered, t)
			}
		}
		tools = filtered
	}

	return tool.NewRegistry(tools...)
}

// printSessionSummary prints a human-readable preview of the loaded session.
func printSessionSummary(msgs []message.Message) {
	fmt.Println("\n=== Previous Session ===")
	fmt.Printf("Total messages: %d\n", len(msgs))

	showCount := 8
	if len(msgs) > showCount {
		for i, msg := range msgs[:showCount/2] {
			printMessagePreview(i+1, msg)
		}
		fmt.Printf("  ... (%d messages omitted) ...\n", len(msgs)-showCount+2)
		for i, msg := range msgs[len(msgs)-showCount/2:] {
			printMessagePreview(len(msgs)-showCount/2+i+1, msg)
		}
	} else {
		for i, msg := range msgs {
			printMessagePreview(i+1, msg)
		}
	}
	fmt.Println("========================")
}

func printMessagePreview(idx int, msg message.Message) {
	role := string(msg.Role)
	text := msg.TextContent()

	// Detect tool_use blocks in assistant messages.
	var toolNames []string
	for _, block := range msg.Content {
		if tu, ok := block.(message.ToolUseBlock); ok {
			toolNames = append(toolNames, tu.Name)
		}
	}

	var preview string
	if len(toolNames) > 0 {
		preview = fmt.Sprintf("[tools: %s]", strings.Join(toolNames, ", "))
	} else if text != "" {
		preview = strings.ReplaceAll(text, "\n", " ")
		if len(preview) > 70 {
			preview = strings.TrimSpace(preview[:70]) + "..."
		}
	} else {
		preview = "(no text content)"
	}

	fmt.Printf("  %3d. [%s] %s\n", idx, role, preview)
}

// isConfirmWord returns true when the input is a natural-language
// confirmation that should be forwarded to a pending agent confirm.
func isConfirmWord(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "确认", "是", "yes", "y", "ok", "继续", "同意", "approve", "允许":
		return true
	default:
		return false
	}
}

func isCancelWord(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "取消", "否", "no", "n", "停止", "stop", "拒绝", "reject", " deny", "暂停", "pause":
		return true
	default:
		return false
	}
}
