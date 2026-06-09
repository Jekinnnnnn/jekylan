package engine

import (
	"embed"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"

	"github.com/Jekinnnnnn/jekylan/internal/tool"
)

//go:embed prompts/*.md
var promptFS embed.FS

// BuildSystemPrompt constructs the complete system prompt for a query turn.
// It starts with the base prompt (Agent instructions + environment),
// appends tool-specific system prompt contributions, and includes reserved
// sections for AgentTool, token budget, and memory system.
func BuildSystemPrompt(basePrompt, model string, tools *tool.Registry, tokenBudget string) string {
	// 1. Base prompt (intro, system, doing tasks, actions, tools, tone, env)
	var prompt strings.Builder
	prompt.WriteString(buildFullSystemPrompt(basePrompt, model))

	// 2. Tool-specific system prompt contributions (e.g. SkillTool listing)
	if tools != nil {
		for _, t := range tools.All() {
			if sp := t.SystemPrompt(); sp != "" {
				prompt.WriteString("\n\n" + sp)
			}
		}
	}

	// 3. AgentTool section (reserved)
	prompt.WriteString(getAgentToolSection())

	// 4. Token budget section (reserved)
	prompt.WriteString(getTokenBudgetSection(tokenBudget))

	return prompt.String()
}

// staticPromptSections holds the sections that never change during a process
// lifetime (everything except environment, which depends on cwd/model).
var (
	cachedStaticSections     string
	cachedStaticSectionsOnce sync.Once
)

// getStaticPromptSections returns the concatenated static sections, computed once.
func getStaticPromptSections() string {
	cachedStaticSectionsOnce.Do(func() {
		sections := []string{
			readPromptFile("intro.md"),
			readPromptFile("system.md"),
			readPromptFile("doing_tasks.md"),
			readPromptFile("actions.md"),
			readPromptFile("using_tools.md"),
			readPromptFile("tone_and_style.md"),
			readPromptFile("output_efficiency.md"),
		}
		cachedStaticSections = joinSections(sections)
	})
	return cachedStaticSections
}

// readPromptFile reads a prompt section from the embedded filesystem.
func readPromptFile(name string) string {
	b, err := promptFS.ReadFile("prompts/" + name)
	if err != nil {
		return ""
	}
	return string(b)
}

// buildFullSystemPrompt constructs the default system prompt for jekylan,
// drawing from the system prompt structure.
// If the user provided a custom systemPrompt, it is used as the base and
// these sections are appended.
func buildFullSystemPrompt(basePrompt, model string) string {
	staticSections := getStaticPromptSections()
	if basePrompt != "" {
		// User provided a meaningful custom prompt; use it as-is but
		// still append the essential tool and environment guidance.
		sections := []string{
			basePrompt,
			staticSections,
			getEnvironmentSection(model),
		}
		return joinSections(sections)
	}

	// Default full prompt
	sections := []string{
		staticSections,
		getEnvironmentSection(model),
	}
	return joinSections(sections)
}

func joinSections(sections []string) string {
	var out []string
	for _, s := range sections {
		if s != "" {
			out = append(out, s)
		}
	}
	return strings.Join(out, "\n\n")
}

func getEnvironmentSection(model string) string {
	cwd, err := os.Getwd()
	if err != nil || cwd == "" {
		cwd = "."
	}

	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "unknown"
	}
	if strings.Contains(shell, "zsh") {
		shell = "zsh"
	} else if strings.Contains(shell, "bash") {
		shell = "bash"
	}

	platform := runtime.GOOS
	if platform == "windows" {
		shell += " (use Unix shell syntax, not Windows — e.g., /dev/null not NUL, forward slashes in paths)"
	}

	osVersion := runtime.GOOS + " " + runtime.GOARCH

	template := readPromptFile("environment.md")
	if template == "" {
		return ""
	}
	template = strings.ReplaceAll(template, "{{CWD}}", cwd)
	template = strings.ReplaceAll(template, "{{PLATFORM}}", platform)
	template = strings.ReplaceAll(template, "{{SHELL}}", shell)
	template = strings.ReplaceAll(template, "{{OS_VERSION}}", osVersion)
	template = strings.ReplaceAll(template, "{{MODEL}}", model)
	return template
}

// --- Reserved sections for future features ---

// getAgentToolSection returns the AgentTool guidance when agents are enabled.
// Reserved for future AgentTool implementation.
func getAgentToolSection() string {
	return ""
}

// getTokenBudgetSection returns token budget instructions when a budget is set.
// Reserved for future token budget implementation.
func getTokenBudgetSection(tokenBudget string) string {
	if tokenBudget == "" {
		return ""
	}
	return fmt.Sprintf(`

# Token Budget
When the user specifies a token target (e.g., "+500k", "spend 2M tokens", "use 1B tokens"), your output token count will be shown each turn. Keep working until you approach the target — plan your work to fill it productively. The target is a hard minimum, not a suggestion. If you stop early, the system will automatically continue you.

Current token budget: %s`, tokenBudget)
}
