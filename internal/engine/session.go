package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Jekinnnnnn/jekylan/internal/memory"
	"github.com/Jekinnnnnn/jekylan/internal/message"
)

// SessionData is the on-disk format for conversation persistence.
type SessionData struct {
	Messages        []message.Message                        `json:"messages"`
	TotalUsage      *message.Usage                           `json:"total_usage,omitempty"`
	SavedAt         time.Time                                `json:"saved_at"`
	SkillExecutions map[string][]memory.SkillExecutionRecord `json:"skill_executions,omitempty"`
	ActiveWorkflow  string                                   `json:"active_workflow,omitempty"`
}

const sessionTimeFormat = "2006-01-02 15:04:05"

// MarshalJSON serializes SessionData with human-readable timestamps.
func (s SessionData) MarshalJSON() ([]byte, error) {
	type alias SessionData
	return json.Marshal(struct {
		alias
		SavedAt string `json:"saved_at"`
	}{
		alias:   alias(s),
		SavedAt: s.SavedAt.Format(sessionTimeFormat),
	})
}

// UnmarshalJSON deserializes SessionData, parsing human-readable timestamps.
func (s *SessionData) UnmarshalJSON(data []byte) error {
	type raw struct {
		Messages        []message.Message                        `json:"messages"`
		TotalUsage      *message.Usage                           `json:"total_usage,omitempty"`
		SavedAt         string                                   `json:"saved_at"`
		SkillExecutions map[string][]memory.SkillExecutionRecord `json:"skill_executions,omitempty"`
		ActiveWorkflow  string                                   `json:"active_workflow,omitempty"`
	}
	var r raw
	if err := json.Unmarshal(data, &r); err != nil {
		return err
	}
	s.Messages = r.Messages
	s.TotalUsage = r.TotalUsage
	s.SkillExecutions = r.SkillExecutions
	s.ActiveWorkflow = r.ActiveWorkflow
	if r.SavedAt != "" {
		if t, err := time.Parse(sessionTimeFormat, r.SavedAt); err == nil {
			s.SavedAt = t
		}
	}
	return nil
}

// SaveSession writes the current engine state to a JSON file.
func (e *Engine) SaveSession(path string) error {
	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create session dir: %w", err)
		}
	}

	data := SessionData{
		Messages:   e.messages,
		TotalUsage: e.totalUsage,
		SavedAt:    time.Now(),
	}
	if e.memoryWorker != nil {
		if sc := e.memoryWorker.SkillCollector(); sc != nil {
			data.SkillExecutions, data.ActiveWorkflow = sc.Snapshot()
		}
	}

	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal session: %w", err)
	}

	if err := os.WriteFile(path, b, 0644); err != nil {
		return fmt.Errorf("write session file: %w", err)
	}

	return nil
}

// LoadSession restores engine state from a JSON file.
// Returns nil when the file does not exist.
func (e *Engine) LoadSession(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var data SessionData
	if err := json.Unmarshal(b, &data); err != nil {
		return fmt.Errorf("parse session file: %w", err)
	}

	e.messages = data.Messages
	e.totalUsage = data.TotalUsage
	if e.memoryWorker != nil {
		if sc := e.memoryWorker.SkillCollector(); sc != nil {
			sc.Restore(data.SkillExecutions, data.ActiveWorkflow)
		}
	}
	return nil
}

// ClearSessionFile removes the session file if it exists.
func (e *Engine) ClearSessionFile(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
