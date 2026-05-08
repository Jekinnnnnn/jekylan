package engine

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/Jekinnnnnn/jekylan/internal/memory"
	"github.com/Jekinnnnnn/jekylan/internal/message"
)

func TestSessionDataRoundTripWithSkillState(t *testing.T) {
	original := SessionData{
		Messages: []message.Message{
			{Role: message.RoleUser, Timestamp: time.Now()},
			{Role: message.RoleAssistant, Timestamp: time.Now()},
		},
		SavedAt: time.Now().UTC().Truncate(time.Second),
		SkillExecutions: map[string][]memory.SkillExecutionRecord{
			"shouzu": {
				{StartMessageIndex: 10, EndMessageIndex: 25, Completed: true},
				{StartMessageIndex: 40, EndMessageIndex: 0, Completed: false},
			},
			"brainstorm": {
				{StartMessageIndex: 0, EndMessageIndex: 5, Completed: true},
			},
		},
		ActiveWorkflow: "shouzu",
	}

	b, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var loaded SessionData
	if err := json.Unmarshal(b, &loaded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if loaded.ActiveWorkflow != original.ActiveWorkflow {
		t.Errorf("ActiveWorkflow: got %q want %q", loaded.ActiveWorkflow, original.ActiveWorkflow)
	}
	if !reflect.DeepEqual(loaded.SkillExecutions, original.SkillExecutions) {
		t.Errorf("SkillExecutions mismatch:\n got=%+v\nwant=%+v", loaded.SkillExecutions, original.SkillExecutions)
	}
	if !loaded.SavedAt.Equal(original.SavedAt) {
		t.Errorf("SavedAt: got %v want %v", loaded.SavedAt, original.SavedAt)
	}
}

func TestSessionDataBackwardCompat(t *testing.T) {
	// Legacy session.json without skill_executions / active_workflow.
	legacy := []byte(`{
		"messages": [],
		"saved_at": "2026-05-04 10:00:00"
	}`)
	var loaded SessionData
	if err := json.Unmarshal(legacy, &loaded); err != nil {
		t.Fatalf("unmarshal legacy: %v", err)
	}
	if loaded.SkillExecutions != nil {
		t.Errorf("expected nil SkillExecutions, got %+v", loaded.SkillExecutions)
	}
	if loaded.ActiveWorkflow != "" {
		t.Errorf("expected empty ActiveWorkflow, got %q", loaded.ActiveWorkflow)
	}
}
