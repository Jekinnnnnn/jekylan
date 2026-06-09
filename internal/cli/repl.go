package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/Jekinnnnnn/jekylan/internal/engine"
	"github.com/Jekinnnnnn/jekylan/internal/query"
)

// RunSingleShot processes a single prompt and returns the final result.
func RunSingleShot(ctx context.Context, eng *engine.Engine, sessionPath, prompt string) query.Result {
	if sessionPath != "" {
		if err := eng.LoadSession(sessionPath); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to load session: %v\n", err)
		} else if len(eng.Messages()) > 0 {
			fmt.Fprintf(os.Stderr, "Loaded %d messages from session\n", len(eng.Messages()))
		}
	}

	fmt.Println("=== Assistant ===")
	var result engine.TurnResult

	for evt := range eng.Turn(ctx, prompt) {
		if text := engine.FormatEvent(evt); text != "" {
			fmt.Print(text)
		}
		if evt.Type == engine.EventTurnResult {
			result = evt.Result
		}
		if evt.Type == engine.EventTurnError {
			result = engine.TurnResult{Success: false, Error: evt.Error}
		}
	}

	fmt.Println("\n=== Result ===")
	if result.Success {
		fmt.Printf("Success | turns=%d | stop_reason=%s | %s\n", result.NumTurns, result.StopReason, eng.TotalUsage())
	} else {
		fmt.Printf("Error: %s | turns=%d | %s\n", result.Error, result.NumTurns, eng.TotalUsage())
		os.Exit(1)
	}
	if sessionPath != "" {
		_ = eng.SaveSession(sessionPath)
	}

	return query.Result{
		Success:    result.Success,
		Text:       result.Text,
		StopReason: result.StopReason,
		NumTurns:   result.NumTurns,
		Error:      result.Error,
	}
}

// RunREPL starts an interactive read-eval-print loop.
// readInput is called repeatedly to obtain user input lines.
// onOutput is called with formatted output strings.
func RunREPL(ctx context.Context, eng *engine.Engine, sessionPath string, readInput func() (string, error), onOutput func(string)) {
	// Signal handler: Ctrl+C interrupts the current turn without exiting.
	sigStop := make(chan struct{})
	go func() {
		sigNotify := make(chan os.Signal, 1)
		signal.Notify(sigNotify, os.Interrupt, syscall.SIGTERM)
		defer signal.Stop(sigNotify)
		for {
			select {
			case <-sigNotify:
				eng.Interrupt()
			case <-sigStop:
				return
			}
		}
	}()
	defer close(sigStop)

	for {
		line, err := readInput()
		if err != nil {
			if sessionPath != "" {
				_ = eng.SaveSession(sessionPath)
			}
			return
		}

		switch line {
		case "/quit", "/exit":
			if sessionPath != "" {
				if err := eng.SaveSession(sessionPath); err != nil {
					onOutput(fmt.Sprintf("Failed to save session: %v\n", err))
				} else {
					onOutput(fmt.Sprintf("Session saved (%d messages)\n", len(eng.Messages())))
				}
			}
			return
		case "/reset":
			eng.Reset()
			onOutput("Conversation reset.\n")
			continue
		case "/stop":
			eng.Interrupt()
			onOutput("Interrupted.\n")
			continue
		case "":
			continue
		default:
			if after, ok := strings.CutPrefix(line, "/compact-skill-exp "); ok {
				skillName := strings.TrimSpace(after)
				if skillName == "" {
					onOutput("Usage: /compact-skill-exp <skill-name>\n")
				} else if err := eng.CompactAnalysis(skillName); err != nil {
					onOutput(fmt.Sprintf("Compact failed: %v\n", err))
				}
				continue
			}
		}

		// Normal query.
		for evt := range eng.Turn(ctx, line) {
			if text := engine.FormatEvent(evt); text != "" {
				onOutput(text)
			}
		}

		if sessionPath != "" {
			_ = eng.SaveSession(sessionPath)
		}
	}
}
