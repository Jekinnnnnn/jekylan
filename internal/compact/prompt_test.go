package compact

import (
	"strings"
	"testing"
)

func TestGetCompactPrompt(t *testing.T) {
	p := GetCompactPrompt("")
	if !strings.Contains(p, "CRITICAL: Respond with TEXT ONLY") {
		t.Error("expected no-tools preamble")
	}
	if !strings.Contains(p, "Primary Request and Intent") {
		t.Error("expected base prompt content")
	}
	if !strings.Contains(p, "REMINDER: Do NOT call any tools") {
		t.Error("expected no-tools trailer")
	}

	p2 := GetCompactPrompt("focus on tests")
	if !strings.Contains(p2, "Additional Instructions:\nfocus on tests") {
		t.Error("expected custom instructions appended")
	}
}

func TestGetPartialCompactPrompt(t *testing.T) {
	p := GetPartialCompactPrompt("", "from")
	if !strings.Contains(p, "RECENT portion of the conversation") {
		t.Error("expected partial prompt")
	}

	pUpTo := GetPartialCompactPrompt("", "up_to")
	if !strings.Contains(pUpTo, "Context for Continuing Work") {
		t.Error("expected up_to prompt")
	}
}

func TestFormatCompactSummary(t *testing.T) {
	raw := `<analysis>
Some thought process here
</analysis>

<summary>
1. Primary Request: test
2. Key Concepts: Go
</summary>`

	got := FormatCompactSummary(raw)
	if strings.Contains(got, "<analysis>") {
		t.Error("analysis block should be stripped")
	}
	if strings.Contains(got, "<summary>") {
		t.Error("summary tags should be replaced")
	}
	if !strings.HasPrefix(got, "Summary:") {
		t.Errorf("expected 'Summary:' prefix, got: %s", got)
	}
	if !strings.Contains(got, "Primary Request") {
		t.Error("expected summary content preserved")
	}
}

func TestGetCompactUserSummaryMessage(t *testing.T) {
	summary := "<summary>Compact content</summary>"
	msg := GetCompactUserSummaryMessage(summary, false, "", false)
	if !strings.Contains(msg, "continued from a previous conversation") {
		t.Error("expected preamble")
	}
	if !strings.Contains(msg, "Compact content") {
		t.Error("expected summary content")
	}

	// With transcript path
	msg2 := GetCompactUserSummaryMessage(summary, false, "/path/to/transcript", false)
	if !strings.Contains(msg2, "/path/to/transcript") {
		t.Error("expected transcript path")
	}

	// With suppress follow-up
	msg3 := GetCompactUserSummaryMessage(summary, true, "", false)
	if !strings.Contains(msg3, "Resume directly") {
		t.Error("expected suppression text")
	}
}
