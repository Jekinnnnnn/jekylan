package agent

import (
	"context"
	"fmt"
	"io"
	"sync"
	"sync/atomic"

	"github.com/Jekinnnnnn/jekylan/internal/engine"
)

// engineDriverEvent is a marker interface for events handled by the driver's
// single event loop goroutine.
type engineDriverEvent any

type startEngineReq struct {
	ctx         context.Context
	eng         *engine.Engine
	sessionPath string
	resp        chan<- struct{}
}

type stopEngineReq struct {
	resp chan<- struct{}
}

// EngineDriver runs an engine.Engine in its own goroutine and bridges
// input/output channels.
type EngineDriver struct {
	inputCh  chan string
	outputCh chan string
	doneCh   chan struct{}
	closeOnce sync.Once

	events chan engineDriverEvent

	// engineRunning prevents multiple concurrent engine loops.
	engineRunning atomic.Bool
}

// NewEngineDriver builds an EngineDriver and starts the background event loop.
func NewEngineDriver() *EngineDriver {
	d := &EngineDriver{
		inputCh:  make(chan string),
		outputCh: make(chan string, 256),
		doneCh:   make(chan struct{}),
		events:   make(chan engineDriverEvent, 16),
	}
	go d.loop()
	return d
}

// Input returns the channel a host writes user input lines to. Close it
// to signal end of input.
func (d *EngineDriver) Input() chan<- string { return d.inputCh }

// Output returns the channel a host reads engine output strings from.
// It is closed when the event-loop goroutine exits.
func (d *EngineDriver) Output() <-chan string { return d.outputCh }

// Done returns a channel closed when the event-loop goroutine has finished.
func (d *EngineDriver) Done() <-chan struct{} { return d.doneCh }

// Start tells the driver to run the parent engine's REPL loop.
func (d *EngineDriver) Start(ctx context.Context, eng *engine.Engine, sessionPath string) {
	if eng == nil {
		panic("agent.EngineDriver: Start called with nil engine")
	}
	resp := make(chan struct{}, 1)
	d.events <- startEngineReq{ctx: ctx, eng: eng, sessionPath: sessionPath, resp: resp}
	<-resp
}

// Stop gracefully shuts down the driver, cancelling the engine and closing
// channels. It is safe to call multiple times.
func (d *EngineDriver) Stop() {
	select {
	case <-d.doneCh:
		return // already stopped
	default:
	}
	resp := make(chan struct{}, 1)
	select {
	case d.events <- stopEngineReq{resp: resp}:
		<-resp
	case <-d.doneCh:
	}
}

func (d *EngineDriver) loop() {
	var currentEng *engine.Engine
	var sessionPath string

	for {
		select {
		case evt := <-d.events:
			switch e := evt.(type) {
			case startEngineReq:
				if d.engineRunning.Load() {
					e.resp <- struct{}{}
					continue
				}
				currentEng = e.eng
				sessionPath = e.sessionPath
				d.engineRunning.Store(true)
				go d.runEngineLoop(e.ctx, currentEng, sessionPath)
				e.resp <- struct{}{}

			case stopEngineReq:
				if currentEng != nil {
					currentEng.Stop()
				}
				d.engineRunning.Store(false)
				d.closeOnce.Do(func() {
					close(d.doneCh)
					close(d.outputCh)
				})
				e.resp <- struct{}{}
				return
			}
		}
	}
}

func (d *EngineDriver) runEngineLoop(ctx context.Context, eng *engine.Engine, sessionPath string) {
	defer d.engineRunning.Store(false)

	readInput := func() (string, error) {
		line, ok := <-d.inputCh
		if !ok {
			return "", io.EOF
		}
		return line, nil
	}

	onOutput := func(s string) {
		select {
		case d.outputCh <- s:
		default:
		}
	}

	for {
		line, err := readInput()
		if err != nil {
			break
		}
		if line == "" {
			continue
		}
		if line == "/quit" || line == "/exit" {
			if sessionPath != "" {
				_ = eng.SaveSession(sessionPath)
			}
			break
		}
		if line == "/reset" {
			eng.Reset()
			onOutput("Conversation reset.\n")
			continue
		}
		if line == "/stop" {
			eng.Interrupt()
			onOutput("Interrupted.\n")
			continue
		}

		for evt := range eng.Turn(ctx, line) {
			if text := engine.FormatEvent(evt); text != "" {
				onOutput(text)
			}
			if evt.Type == engine.EventTurnResult && evt.Result.Success {
				onOutput(fmt.Sprintf("\n[turn complete | %s]\n", eng.TotalUsage()))
			}
		}

		if sessionPath != "" {
			_ = eng.SaveSession(sessionPath)
		}
	}

	d.closeOnce.Do(func() {
		close(d.doneCh)
		close(d.outputCh)
	})
}
