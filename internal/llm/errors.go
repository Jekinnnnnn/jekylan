package llm

import (
	"regexp"
	"strings"
)

const promptTooLongErrorMessage = "Prompt is too long"

var promptTooLongTokenRegex = regexp.MustCompile(`(?i)prompt is too long[^0-9]*(\d+)\s*tokens?\s*>\s*(\d+)`)

// IsPromptTooLongError returns true when the error indicates the prompt
// exceeded the model's context window.
func IsPromptTooLongError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), strings.ToLower(promptTooLongErrorMessage))
}

// IsPromptTooLongErrorString checks a raw error string for PTL.
func IsPromptTooLongErrorString(s string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(promptTooLongErrorMessage))
}

// ParsePromptTooLongTokenCounts extracts actual and limit token counts from a
// raw prompt-too-long API error message like
// "prompt is too long: 137500 tokens > 135000 maximum".
func ParsePromptTooLongTokenCounts(raw string) (actualTokens int64, limitTokens int64) {
	match := promptTooLongTokenRegex.FindStringSubmatch(raw)
	if len(match) < 3 {
		return 0, 0
	}
	// Use manual parsing to avoid importing strconv for this simple case.
	actualTokens = parseInt64(match[1])
	limitTokens = parseInt64(match[2])
	return actualTokens, limitTokens
}

func parseInt64(s string) int64 {
	var n int64
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int64(c-'0')
	}
	return n
}
