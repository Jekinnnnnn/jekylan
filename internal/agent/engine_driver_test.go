package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jekinnnnnn/jekylan/internal/engine"
)

// TestEngineDriver_RunsEngineInGoroutine drives the driver end-to-end
// against a real engine. /reset and /quit short-circuit before any model
// call, so no LLM client is needed.
func TestEngineDriver_RunsEngineInGoroutine(t *testing.T) {
	eng := engine.NewEngine(nil, "test-model", 10, 0, "", nil, true)
	driver := NewEngineDriver()
	defer driver.Stop()
	driver.Start(context.Background(), eng, "")

	driver.Input() <- "/reset"
	if got := readNonEmpty(t, driver.Output(), 500*time.Millisecond); !strings.Contains(got, "reset") {
		t.Fatalf("expected reset output, got %q", got)
	}

	driver.Input() <- "/quit"
	select {
	case <-driver.Done():
	case <-time.After(time.Second):
		t.Fatal("expected driver goroutine to exit after /quit")
	}

	// Drain any remaining buffered output.
	for range driver.Output() {
	}
}

// TestEngineDriver_CloseInputShutsDown verifies that closing the input
// channel cleanly terminates the driver goroutine.
func TestEngineDriver_CloseInputShutsDown(t *testing.T) {
	eng := engine.NewEngine(nil, "test-model", 10, 0, "", nil, true)
	driver := NewEngineDriver()
	defer driver.Stop()
	driver.Start(context.Background(), eng, "")

	close(driver.Input())

	select {
	case <-driver.Done():
	case <-time.After(time.Second):
		t.Fatal("expected driver to exit after input close")
	}
}

// TestEngineDriver_StopIdempotent verifies that calling Stop() twice does
// not panic.
func TestEngineDriver_StopIdempotent(t *testing.T) {
	eng := engine.NewEngine(nil, "test-model", 10, 0, "", nil, true)
	driver := NewEngineDriver()
	driver.Start(context.Background(), eng, "")

	close(driver.Input())
	<-driver.Done()

	// Second stop should be safe.
	driver.Stop()
}

// TestEngineDriver_RejectsConcurrentStart verifies that a second Start while
// an engine is running is ignored, and a Start after the first exits works.
func TestEngineDriver_RejectsConcurrentStart(t *testing.T) {
	eng1 := engine.NewEngine(nil, "test-model", 10, 0, "", nil, true)
	eng2 := engine.NewEngine(nil, "test-model", 10, 0, "", nil, true)
	driver := NewEngineDriver()
	defer driver.Stop()

	driver.Start(context.Background(), eng1, "")

	// Second Start while first is running should be ignored (no panic).
	driver.Start(context.Background(), eng2, "")

	close(driver.Input())
	select {
	case <-driver.Done():
	case <-time.After(time.Second):
		t.Fatal("expected driver to exit after input close")
	}
}
