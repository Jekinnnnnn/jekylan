package agent

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/Jekinnnnnn/jekylan/internal/engine"
	"github.com/Jekinnnnnn/jekylan/internal/query"
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
// input/output channels. It is the extracted REPL bridge formerly inside
// Coordinator.
type EngineDriver struct {
	inputCh  chan string
	outputCh chan string
	doneCh   chan struct{}
	closeOnce sync.Once

	events chan engineDriverEvent

	// state — owned by event-loop goroutine
	engine          *engine.Engine
	engineOutput    <-chan string
	engineDone      <-chan struct{}
	engineCtxCancel context.CancelFunc
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
	for {
		select {
		case evt := <-d.events:
			switch e := evt.(type) {
			case startEngineReq:
				out := make(chan string, 64)
				done := make(chan struct{})
				d.engineOutput = out
				d.engineDone = done
				engineCtx, engineCtxCancel := context.WithCancel(e.ctx)
				d.engineCtxCancel = engineCtxCancel

				go func() {
					defer close(done)
					readInput := func() (string, error) {
						line, ok := <-d.inputCh
						if !ok {
							return "", io.EOF
						}
						return line, nil
					}
					d.engine = e.eng
					if err := e.eng.Run(engine.RunOptions{
						ReadInput: readInput,
						OnOutput:  func(s string) { out <- s },
						OnResult: func(r query.Result) {
							if r.Success {
								select {
								case out <- fmt.Sprintf("\n[turn complete | %s]\n", e.eng.TotalUsage()):
								default:
								}
							}
						},
						Context:     engineCtx,
						SessionPath: e.sessionPath,
					}); err != nil {
						select {
						case out <- fmt.Sprintf("[engine error] %v\n", err):
						default:
						}
					}
				}()
				e.resp <- struct{}{}

			case stopEngineReq:
				if d.engineCtxCancel != nil {
					d.engineCtxCancel()
				}
				d.closeOnce.Do(func() {
					close(d.doneCh)
					close(d.outputCh)
				})
				e.resp <- struct{}{}
				return
			}

		case text := <-d.engineOutput:
			if text != "" {
				select {
				case d.outputCh <- text:
				default:
				}
			}
		case <-d.engineDone:
			if d.engineCtxCancel != nil {
				d.engineCtxCancel()
			}
			d.closeOnce.Do(func() {
				close(d.doneCh)
				close(d.outputCh)
			})
			return
		}
	}
}
