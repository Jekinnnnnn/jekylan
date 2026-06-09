package compact

import (
	_ "embed"
	"regexp"
	"strings"
)

var (
	reAnalysis = regexp.MustCompile(`(?s)\s*<analysis>.*?</analysis>\s*`)
	reSummary  = regexp.MustCompile(`(?s)\s*<summary>\s*(.*?)\s*</summary>\s*`)
)

//go:embed prompts/base.md
var baseCompactPrompt string

//go:embed prompts/partial.md
var partialCompactPrompt string

//go:embed prompts/partial_up_to.md
var partialCompactUpToPrompt string

const noToolsPreamble = `CRITICAL: Respond with TEXT ONLY. Do NOT call any tools.

- Do NOT use file_read, bash, grep, glob, file_edit, file_write, or ANY other tool.
- You already have all the context you need in the conversation above.
- Tool calls will be REJECTED and will waste your only turn — you will fail the task.
- Your entire response must be plain text: an <analysis> block followed by a <summary> block.

`

const noToolsTrailer = `

REMINDER: Do NOT call any tools. Respond with plain text only — an <analysis> block followed by a <summary> block. Tool calls will be rejected and you will fail the task.`

// IsProactiveActive returns whether proactive/autonomous mode is active.
// Mirrors the TS stub which always returns false.
func IsProactiveActive() bool {
	return false
}

// GetPartialCompactPrompt builds the compaction prompt for partial compactions.
func GetPartialCompactPrompt(customInstructions, direction string) string {
	template := partialCompactPrompt
	if direction == "up_to" {
		template = partialCompactUpToPrompt
	}
	prompt := noToolsPreamble + template
	if strings.TrimSpace(customInstructions) != "" {
		prompt += "\n\nAdditional Instructions:\n" + customInstructions
	}
	prompt += noToolsTrailer
	return prompt
}

// GetCompactPrompt builds the compaction prompt for full compactions.
func GetCompactPrompt(customInstructions string) string {
	prompt := noToolsPreamble + baseCompactPrompt
	if strings.TrimSpace(customInstructions) != "" {
		prompt += "\n\nAdditional Instructions:\n" + customInstructions
	}
	prompt += noToolsTrailer
	return prompt
}

// FormatCompactSummary strips the <analysis> drafting scratchpad and replaces
// <summary> XML tags with readable section headers.
func FormatCompactSummary(summary string) string {
	formatted := summary

	// Strip analysis section — it's a drafting scratchpad.
	formatted = reAnalysis.ReplaceAllString(formatted, "\n\n")

	// Extract and format summary section.
	if matches := reSummary.FindStringSubmatch(formatted); len(matches) > 1 {
		content := strings.TrimSpace(matches[1])
		formatted = reSummary.ReplaceAllString(formatted, "Summary:\n"+content)
	}

	// Clean up extra whitespace between sections.
	for strings.Contains(formatted, "\n\n\n") {
		formatted = strings.ReplaceAll(formatted, "\n\n\n", "\n\n")
	}

	return strings.TrimSpace(formatted)
}

// GetCompactUserSummaryMessage wraps a formatted compact summary into the user
// message that is injected post-compaction.
func GetCompactUserSummaryMessage(summary string, suppressFollowUpQuestions bool, transcriptPath string, recentMessagesPreserved bool) string {
	formattedSummary := FormatCompactSummary(summary)

	baseSummary := "This session is being continued from a previous conversation that ran out of context. The summary below covers the earlier portion of the conversation.\n\n" + formattedSummary

	if transcriptPath != "" {
		baseSummary += "\n\nIf you need specific details from before compaction (like exact code snippets, error messages, or content you generated), read the full transcript at: " + transcriptPath
	}

	if recentMessagesPreserved {
		baseSummary += "\n\nRecent messages are preserved verbatim."
	}

	if suppressFollowUpQuestions {
		continuation := baseSummary + "\nContinue the conversation from where it left off without asking the user any further questions. Resume directly — do not acknowledge the summary, do not recap what was happening, do not preface with \"I'll continue\" or similar. Pick up the last task as if the break never happened."

		if IsProactiveActive() {
			continuation += "\n\nYou are running in autonomous/proactive mode. This is NOT a first wake-up — you were already working autonomously before compaction. Continue your work loop: pick up where you left off based on the summary above. Do not greet the user or ask what to work on."
		}

		return continuation
	}

	return baseSummary
}
