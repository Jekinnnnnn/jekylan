package cli

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Jekinnnnnn/jekylan/internal/engine"
	"github.com/Jekinnnnnn/jekylan/internal/message"
	"github.com/Jekinnnnnn/jekylan/internal/query"
)

// fakeQueryFunc returns a query function that emits the provided events.
func fakeQueryFunc(evts ...query.Event) func(context.Context, query.Params) <-chan query.Event {
	return func(_ context.Context, _ query.Params) <-chan query.Event {
		out := make(chan query.Event)
		go func() {
			defer close(out)
			for _, evt := range evts {
				out <- evt
			}
		}()
		return out
	}
}

func TestRunREPLQuitCommand(t *testing.T) {
	eng := engine.NewEngine(nil, "test-model", 10, 0, "", nil, true)
	defer eng.Stop()

	var output strings.Builder
	inputs := []string{"/quit"}
	idx := 0
	readInput := func() (string, error) {
		if idx >= len(inputs) {
			return "", errors.New("eof")
		}
		line := inputs[idx]
		idx++
		return line, nil
	}

	RunREPL(context.Background(), eng, "", readInput, func(s string) {
		output.WriteString(s)
	})

	if output.Len() != 0 {
		t.Errorf("expected no output for /quit, got: %q", output.String())
	}
}

func TestRunREPLResetCommand(t *testing.T) {
	eng := engine.NewEngine(nil, "test-model", 10, 0, "", nil, true)
	defer eng.Stop()

	// Seed a message so we can verify reset clears it.
	eng.LoadSession("") // no-op but ensures engine is ready

	var output strings.Builder
	inputs := []string{"/reset", "/quit"}
	idx := 0
	readInput := func() (string, error) {
		if idx >= len(inputs) {
			return "", errors.New("eof")
		}
		line := inputs[idx]
		idx++
		return line, nil
	}

	RunREPL(context.Background(), eng, "", readInput, func(s string) {
		output.WriteString(s)
	})

	if !strings.Contains(output.String(), "Conversation reset.") {
		t.Errorf("expected reset output, got: %q", output.String())
	}
}

func TestRunREPLNormalQuery(t *testing.T) {
	assistantMsg := message.Message{Role: message.RoleAssistant, Timestamp: time.Now()}
	assistantMsg.AddText("hi there")

	eng := engine.NewEngine(nil, "test-model", 10, 0, "", nil, true,
		engine.WithQueryFunc(fakeQueryFunc(
			query.Event{Type: query.EventTypeAssistantText, Text: "hi there"},
			query.Event{Type: query.EventTypeUsage, Message: assistantMsg},
			query.Event{Type: query.EventTypeResult, Result: query.Result{Success: true, StopReason: "end_turn", NumTurns: 1}},
		)),
	)
	defer eng.Stop()

	var output strings.Builder
	inputs := []string{"hello", "/quit"}
	idx := 0
	readInput := func() (string, error) {
		if idx >= len(inputs) {
			return "", errors.New("eof")
		}
		line := inputs[idx]
		idx++
		return line, nil
	}

	RunREPL(context.Background(), eng, "", readInput, func(s string) {
		output.WriteString(s)
	})

	if !strings.Contains(output.String(), "hi there") {
		t.Errorf("expected assistant text in output, got: %q", output.String())
	}
}

func TestRunREPLCompactSkillExpCommand(t *testing.T) {
	eng := engine.NewEngine(nil, "test-model", 10, 0, "", nil, true)
	defer eng.Stop()

	var output strings.Builder
	inputs := []string{"/compact-skill-exp ", "/quit"}
	idx := 0
	readInput := func() (string, error) {
		if idx >= len(inputs) {
			return "", errors.New("eof")
		}
		line := inputs[idx]
		idx++
		return line, nil
	}

	RunREPL(context.Background(), eng, "", readInput, func(s string) {
		output.WriteString(s)
	})

	if !strings.Contains(output.String(), "Usage: /compact-skill-exp") {
		t.Errorf("expected usage message, got: %q", output.String())
	}
}

func TestRunREPLInterruptCommand(t *testing.T) {
	eng := engine.NewEngine(nil, "test-model", 10, 0, "", nil, true,
		engine.WithQueryFunc(fakeQueryFunc(
			query.Event{Type: query.EventTypeResult, Result: query.Result{Success: true, StopReason: "end_turn", NumTurns: 1}},
		)),
	)
	defer eng.Stop()

	var output strings.Builder
	inputs := []string{"hello", "/stop", "/quit"}
	idx := 0
	readInput := func() (string, error) {
		if idx >= len(inputs) {
			return "", errors.New("eof")
		}
		line := inputs[idx]
		idx++
		return line, nil
	}

	RunREPL(context.Background(), eng, "", readInput, func(s string) {
		output.WriteString(s)
	})

	if !strings.Contains(output.String(), "Interrupted.") {
		t.Errorf("expected interrupt output, got: %q", output.String())
	}
}
