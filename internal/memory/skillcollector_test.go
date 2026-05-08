package memory

import (
	"reflect"
	"testing"
)

func TestSkillCollectorSnapshotRestoreRoundTrip(t *testing.T) {
	sc := NewSkillCollector("")
	sc.executions["shouzu"] = []SkillExecutionRecord{
		{StartMessageIndex: 10, EndMessageIndex: 25, Completed: true},
		{StartMessageIndex: 40, EndMessageIndex: 0, Completed: false},
	}
	sc.executions["brainstorm"] = []SkillExecutionRecord{
		{StartMessageIndex: 0, EndMessageIndex: 5, Completed: true},
	}
	sc.activeWorkflow = "shouzu"
	sc.pending = []string{"shouzu"}

	execs, active := sc.Snapshot()
	if active != "shouzu" {
		t.Errorf("Snapshot active: got %q want %q", active, "shouzu")
	}
	if len(execs) != 2 || len(execs["shouzu"]) != 2 || len(execs["brainstorm"]) != 1 {
		t.Errorf("Snapshot executions shape unexpected: %+v", execs)
	}

	// Mutating the snapshot must not affect the collector's live state
	// (defensive copy).
	execs["shouzu"][0].EndMessageIndex = 999
	if sc.executions["shouzu"][0].EndMessageIndex == 999 {
		t.Errorf("Snapshot returned a shared slice — mutation leaked back to live state")
	}

	restored := NewSkillCollector("")
	restored.pending = []string{"stale"}
	restored.Restore(execs, active)

	if restored.activeWorkflow != "shouzu" {
		t.Errorf("Restore activeWorkflow: got %q", restored.activeWorkflow)
	}
	if restored.pending != nil {
		t.Errorf("Restore must clear pending, got %+v", restored.pending)
	}
	if !reflect.DeepEqual(restored.executions, execs) {
		t.Errorf("Restore executions mismatch")
	}
}

func TestSkillCollectorSnapshotEmpty(t *testing.T) {
	sc := NewSkillCollector("")
	execs, active := sc.Snapshot()
	if execs != nil || active != "" {
		t.Errorf("empty Snapshot should be (nil, \"\"), got (%+v, %q)", execs, active)
	}

	// active workflow with no executions still snapshots the workflow name.
	sc.activeWorkflow = "shouzu"
	_, active = sc.Snapshot()
	if active != "shouzu" {
		t.Errorf("Snapshot should preserve activeWorkflow even with no executions, got %q", active)
	}
}

func TestSkillCollectorRestoreNil(t *testing.T) {
	sc := NewSkillCollector("")
	sc.executions["x"] = []SkillExecutionRecord{{StartMessageIndex: 1}}
	sc.Restore(nil, "")
	if sc.executions == nil {
		t.Errorf("Restore(nil) must reinitialize executions to empty map, got nil")
	}
	if len(sc.executions) != 0 {
		t.Errorf("Restore(nil) must clear existing executions, got %+v", sc.executions)
	}
	if sc.activeWorkflow != "" {
		t.Errorf("Restore(nil, \"\") must clear activeWorkflow, got %q", sc.activeWorkflow)
	}
}
