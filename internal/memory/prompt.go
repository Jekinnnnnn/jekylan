package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	entrypointName     = "MEMORY.md"
	maxEntrypointLines = 200
	maxEntrypointBytes = 25_000
)

// EntrypointTruncation holds the result of truncating MEMORY.md.
type EntrypointTruncation struct {
	Content          string
	LineCount        int
	ByteCount        int
	WasLineTruncated bool
	WasByteTruncated bool
}

// TruncateEntrypointContent truncates MEMORY.md content to line AND byte caps,
// appending a warning that names which cap fired. Line-truncates first (natural
// boundary), then byte-truncates at the last newline before the cap.
func TruncateEntrypointContent(raw string) EntrypointTruncation {
	trimmed := strings.TrimSpace(raw)
	contentLines := strings.Split(trimmed, "\n")
	lineCount := len(contentLines)
	byteCount := len(trimmed)

	wasLineTruncated := lineCount > maxEntrypointLines
	wasByteTruncated := byteCount > maxEntrypointBytes

	if !wasLineTruncated && !wasByteTruncated {
		return EntrypointTruncation{
			Content:          trimmed,
			LineCount:        lineCount,
			ByteCount:        byteCount,
			WasLineTruncated: false,
			WasByteTruncated: false,
		}
	}

	truncated := trimmed
	if wasLineTruncated {
		truncated = strings.Join(contentLines[:maxEntrypointLines], "\n")
	}

	if len(truncated) > maxEntrypointBytes {
		cutAt := strings.LastIndex(truncated[:maxEntrypointBytes], "\n")
		if cutAt > 0 {
			truncated = truncated[:cutAt]
		} else {
			truncated = truncated[:maxEntrypointBytes]
		}
	}

	var reason string
	if wasByteTruncated && !wasLineTruncated {
		reason = fmt.Sprintf("%d bytes (limit: %d) — index entries are too long", byteCount, maxEntrypointBytes)
	} else if wasLineTruncated && !wasByteTruncated {
		reason = fmt.Sprintf("%d lines (limit: %d)", lineCount, maxEntrypointLines)
	} else {
		reason = fmt.Sprintf("%d lines and %d bytes", lineCount, byteCount)
	}

	return EntrypointTruncation{
		Content: truncated + fmt.Sprintf(
			"\n\n> WARNING: %s is %s. Only part of it was loaded. Keep index entries to one line under ~200 chars; move detail into topic files.",
			entrypointName, reason,
		),
		LineCount:        lineCount,
		ByteCount:        byteCount,
		WasLineTruncated: wasLineTruncated,
		WasByteTruncated: wasByteTruncated,
	}
}

// dirExistsGuidance is shared guidance appended to each memory directory prompt line.
const dirExistsGuidance = "This directory already exists — write to it directly with the file_write tool (do not run mkdir or check for its existence)."

// BuildMemoryPrompt builds the typed-memory prompt with MEMORY.md content included.
func BuildMemoryPrompt(memoryDir string) string {
	entrypoint := filepath.Join(memoryDir, entrypointName)

	var entrypointContent string
	if data, err := os.ReadFile(entrypoint); err == nil {
		entrypointContent = string(data)
	}

	// Load optional save rules directly so BuildMemoryLines stays focused on rendering.
	saveRules, _ := LoadSaveRules(memoryDir)
	lines := BuildMemoryLines(memoryDir, saveRules)

	if strings.TrimSpace(entrypointContent) != "" {
		t := TruncateEntrypointContent(entrypointContent)
		lines = append(lines, "", fmt.Sprintf("## %s", entrypointName), "", t.Content)
	} else {
		lines = append(lines, "", fmt.Sprintf("## %s", entrypointName), "",
			fmt.Sprintf("Your %s is currently empty. When you save new memories, they will appear here.", entrypointName))
	}

	return strings.Join(lines, "\n")
}

// BuildMemoryLines builds the typed-memory behavioral instructions (without MEMORY.md content).
// The caller should load save rules via LoadSaveRules and pass them in; if nil, no conditional
// rules are injected.
func BuildMemoryLines(memoryDir string, rules *SaveRuleset) []string {
	howToSave := []string{
		"## How to save memories",
		"",
		"Saving a memory is a two-step process:",
		"",
		"**Step 1** — write the memory to its own file (e.g., `user_role.md`, `feedback_testing.md`) using this frontmatter format:",
		"",
		MemoryFrontmatterExample(),
		"",
		fmt.Sprintf("**Step 2** — add a pointer to that file in `%s`. `%s` is an index, not a memory — each entry should be one line, under ~150 characters: `- [Title](file.md) — one-line hook`. It has no frontmatter. Never write memory content directly into `%s`.", entrypointName, entrypointName, entrypointName),
		"",
		fmt.Sprintf("- `%s` is always loaded into your conversation context — lines after %d will be truncated, so keep the index concise", entrypointName, maxEntrypointLines),
		"- Keep the name, description, and type fields in memory files up-to-date with the content",
		"- Organize memory semantically by topic, not chronologically",
		"- Update or remove memories that turn out to be wrong or outdated",
		"- Do not write duplicate memories. First check if there is an existing memory you can update before writing a new one.",
	}

	lines := []string{
		"# auto memory",
		"",
		fmt.Sprintf("You have a persistent, file-based memory system at `%s`. %s", memoryDir, dirExistsGuidance),
		"",
		"You should build up this memory system over time so that future conversations can have a complete picture of who the user is, how they'd like to collaborate with you, what behaviors to avoid or repeat, and the context behind the work the user gives you.",
		"",
		"If the user explicitly asks you to remember something, save it immediately as whichever type fits best. If they ask you to forget something, find and remove the relevant entry.",
		"\n",
	}

	// Inject types section from save rules (or fallback to built-in).
	if rules != nil {
		if typesSection := rules.BuildTypesSection(); typesSection != "" {
			lines = append(lines, typesSection, "")
		}
	} else {
		lines = append(lines, TypesSectionIndividual(), "")
	}

	// Inject conditional save rules if present.
	if rules != nil {
		if rulesSection := rules.BuildSaveRulesSection(); rulesSection != "" {
			lines = append(lines, rulesSection, "")
		}
	}

	lines = append(lines,
		WhatNotToSaveSection(),
		"",
		strings.Join(howToSave, "\n"),
		"",
		WhenToAccessSection(),
		"",
		TrustingRecallSection(),
		"",
		"## Memory and other forms of persistence",
		"Memory is one of several persistence mechanisms available to you as you assist the user in a given conversation. The distinction is often that memory can be recalled in future conversations and should not be used for persisting information that is only useful within the scope of the current conversation.",
		"- When to use or update a plan instead of memory: If you are about to start a non-trivial implementation task and would like to reach alignment with the user on your approach you should use a Plan rather than saving this information to memory. Similarly, if you already have a plan within the conversation and you have changed your approach persist that change by updating the plan rather than saving a memory.",
		"- When to use or update tasks instead of memory: When you need to break your work in current conversation into discrete steps or keep track of your progress use tasks instead of saving to memory. Tasks are great for persisting information about the work that needs to be done in the current conversation, but memory should be reserved for information that will be useful in future conversations.",
	)

	lines = append(lines, buildSearchingPastContextSection(memoryDir)...)
	return lines
}

func buildSearchingPastContextSection(memoryDir string) []string {
	memSearch := fmt.Sprintf("grep -rn \"<search term>\" %s --include=\"*.md\"", memoryDir)
	sessionMemPath := GetSessionMemoryPath(memoryDir)
	sessionSearch := fmt.Sprintf("grep -n \"<search term>\" %s", sessionMemPath)
	return []string{
		"",
		"## Searching past context",
		"",
		"When looking for past context:",
		"1. Search topic files in your memory directory:",
		"```",
		memSearch,
		"```",
		"2. Session memory file:",
		"```",
		sessionSearch,
		"```",
		"Use narrow search terms (error messages, file paths, function names) rather than broad keywords.",
	}
}
