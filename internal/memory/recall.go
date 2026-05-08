package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Jekinnnnnn/jekylan/internal/llm"
	"github.com/Jekinnnnnn/jekylan/internal/message"
)

// RelevantMemory represents a memory file deemed relevant to a query.
type RelevantMemory struct {
	Path    string
	MtimeMs int64
}

// MemorySelector selects relevant memories from a list of headers.
// Different implementations may use keyword matching, LLM side-queries,
// vector search, or other strategies.
type MemorySelector interface {
	Select(ctx context.Context, query string, headers []MemoryHeader, alreadySurfaced map[string]bool) []RelevantMemory
}

// FindRelevantMemories finds memory files relevant to a query by scanning
// the memory directory and building the right selector internally.
// If skillCollector is provided, a query-aware selector that considers active
// workflow skills is used; otherwise plain keyword matching is used.
func FindRelevantMemories(ctx context.Context, query string, memoryDir string, alreadySurfaced map[string]bool, skillCollector *SkillCollector) []RelevantMemory {
	headers, err := ScanMemoryFiles(memoryDir)
	if err != nil {
		return nil
	}

	var selector MemorySelector
	if skillCollector != nil {
		selector = &queryAwareSelector{skillCollector: skillCollector}
	} else {
		selector = KeywordSelector{}
	}
	return selector.Select(ctx, query, headers, alreadySurfaced)
}

// --- Workflow-aware selector ---

// workflowAwareSelector wraps keyword matching with skill-collector workflow
// awareness. It unconditionally includes the active workflow's feedback memory
// (unless already surfaced), then appends keyword-matched memories.
type workflowAwareSelector struct {
	keywordSelector MemorySelector
	workflowPath    string
}

func newWorkflowAwareSelector(workflowPath string) *workflowAwareSelector {
	return &workflowAwareSelector{
		keywordSelector: KeywordSelector{},
		workflowPath:    workflowPath,
	}
}

func (s *workflowAwareSelector) Select(ctx context.Context, query string, headers []MemoryHeader, alreadySurfaced map[string]bool) []RelevantMemory {
	var result []RelevantMemory

	// Phase 1: active workflow feedback
	if s.workflowPath != "" && (alreadySurfaced == nil || !alreadySurfaced[s.workflowPath]) {
		var mtime int64
		for _, h := range headers {
			if h.FilePath == s.workflowPath {
				mtime = h.MtimeMs
				break
			}
		}
		result = append(result, RelevantMemory{Path: s.workflowPath, MtimeMs: mtime})
	}

	// Phase 2: keyword matching
	keywordResults := s.keywordSelector.Select(ctx, query, headers, alreadySurfaced)

	// Deduplicate: skip keyword results already in result (workflow feedback may
	// also match keywords, but we only want it once).
	selectedPaths := make(map[string]bool, len(result))
	for _, r := range result {
		selectedPaths[r.Path] = true
	}

	for _, r := range keywordResults {
		if !selectedPaths[r.Path] {
			result = append(result, r)
		}
	}

	return result
}

// queryAwareSelector dynamically chooses between workflow-aware selection and
// plain keyword matching based on the query and skill collector state.
type queryAwareSelector struct {
	skillCollector *SkillCollector
}

func (s *queryAwareSelector) Select(ctx context.Context, query string, headers []MemoryHeader, alreadySurfaced map[string]bool) []RelevantMemory {
	if s.skillCollector != nil {
		// Highest priority: active workflow
		if fbPath := s.skillCollector.ActiveWorkflowFeedbackPath(); fbPath != "" {
			return newWorkflowAwareSelector(fbPath).Select(ctx, query, headers, alreadySurfaced)
		}
		// Second priority: query matches a skill's keywords
		if fbPath := s.skillCollector.MatchSkillFeedbackPath(query); fbPath != "" {
			return newWorkflowAwareSelector(fbPath).Select(ctx, query, headers, alreadySurfaced)
		}
	}
	// Fallback: plain keyword matching
	return KeywordSelector{}.Select(ctx, query, headers, alreadySurfaced)
}

// --- KeywordSelector: simple keyword-based matching ---

// KeywordSelector matches memories whose filename or description contains
// any word from the query. Fast, no external dependencies.
type KeywordSelector struct{}

func (KeywordSelector) Select(_ context.Context, query string, headers []MemoryHeader, alreadySurfaced map[string]bool) []RelevantMemory {
	keywords := extractKeywords(query)

	var result []RelevantMemory
	for _, h := range headers {
		if alreadySurfaced != nil && alreadySurfaced[h.FilePath] {
			continue
		}
		if matchesKeywords(h, keywords) {
			result = append(result, RelevantMemory{
				Path:    h.FilePath,
				MtimeMs: h.MtimeMs,
			})
		}
	}
	return result
}

func extractKeywords(query string) []string {
	words := strings.Fields(strings.ToLower(query))
	var keywords []string
	seen := make(map[string]bool)
	for _, w := range words {
		w = strings.TrimFunc(w, func(r rune) bool {
			return r == '.' || r == ',' || r == '?' || r == '!' || r == ';' || r == ':' || r == '"' || r == '\''
		})
		if len(w) >= 2 && !seen[w] {
			keywords = append(keywords, w)
			seen[w] = true
		}
		// For CJK text, also add individual characters as keywords.
		// This allows "开始收租" to match "收租" in memory descriptions.
		for _, r := range w {
			if isCJK(r) {
				s := string(r)
				if !seen[s] {
					keywords = append(keywords, s)
					seen[s] = true
				}
			}
		}
	}
	return keywords
}

// isCJK reports whether r is a CJK Unified Ideograph or related character.
func isCJK(r rune) bool {
	return (r >= '一' && r <= '鿿') || // CJK Unified Ideographs
		(r >= '㐀' && r <= '䶿') || // CJK Extension A
		(r >= '⺀' && r <= '⻿') || // CJK Radicals
		(r >= '　' && r <= '〿') || // CJK Symbols and Punctuation
		(r >= '가' && r <= '힯') || // Hangul Syllables
		(r >= '぀' && r <= 'ゟ') || // Hiragana
		(r >= '゠' && r <= 'ヿ') // Katakana
}

func matchesKeywords(h MemoryHeader, keywords []string) bool {
	if len(keywords) == 0 {
		return true
	}
	text := strings.ToLower(h.Filename + " " + h.Description)
	for _, kw := range keywords {
		if strings.Contains(text, kw) {
			return true
		}
	}
	return false
}

// --- LLMSelector: LLM-based relevance ranking ---

const selectMemoriesSystemPrompt = `You are selecting memories that will be useful to Harness Agent as it processes a user's query. You will be given the user's query and a list of available memory files with their filenames and descriptions.

Return a list of filenames for the memories that will clearly be useful to Harness Agent as it processes the user's query (up to 5). Only include memories that you are certain will be helpful based on their name and description.
- If you are unsure if a memory will be useful in processing the user's query, then do not include it in your list. Be selective and discerning.
- If there are no memories in the list that would clearly be useful, feel free to return an empty list.
- If a list of recently-used tools is provided, do not select memories that are usage reference or API documentation for those tools (Harness Agent is already exercising them). DO still select memories containing warnings, gotchas, or known issues about those tools — active use is exactly when those matter.`

// LLMSelector uses an LLM side-query to select the most relevant memories.
// It delegates the actual LLM call to llm.Client — the same interface
// used by the engine for streaming chat.
type LLMSelector struct {
	client      llm.Client
	recentTools []string
	maxMemories int
}

// NewLLMSelector creates an LLMSelector with the given LLM client.
// If maxMemories is <= 0, it defaults to 5.
func NewLLMSelector(client llm.Client) *LLMSelector {
	return &LLMSelector{
		client:      client,
		maxMemories: 5,
	}
}

// SetRecentTools informs the selector of recently used tool names,
// so it can skip tool-reference memories that would be noise.
func (s *LLMSelector) SetRecentTools(tools []string) {
	s.recentTools = tools
}

// Select asks the LLM to choose the most relevant memories for the query.
func (s *LLMSelector) Select(ctx context.Context, query string, headers []MemoryHeader, alreadySurfaced map[string]bool) []RelevantMemory {
	// Filter out already-surfaced memories before sending to LLM
	var candidates []MemoryHeader
	for _, h := range headers {
		if alreadySurfaced != nil && alreadySurfaced[h.FilePath] {
			continue
		}
		candidates = append(candidates, h)
	}

	if len(candidates) == 0 {
		return nil
	}

	selected := s.selectWithLLM(ctx, query, candidates)

	// Map filenames back to full headers
	byFilename := make(map[string]MemoryHeader, len(candidates))
	for _, h := range candidates {
		byFilename[h.Filename] = h
	}

	var result []RelevantMemory
	for _, filename := range selected {
		if h, ok := byFilename[filename]; ok {
			result = append(result, RelevantMemory{
				Path:    h.FilePath,
				MtimeMs: h.MtimeMs,
			})
		}
	}
	return result
}

func (s *LLMSelector) selectWithLLM(ctx context.Context, query string, memories []MemoryHeader) []string {
	validFilenames := make(map[string]bool, len(memories))
	for _, m := range memories {
		validFilenames[m.Filename] = true
	}

	manifest := FormatMemoryManifest(memories)

	// When actively using a tool, surfacing that tool's
	// reference docs is noise — the conversation already contains working usage.
	var toolsSection string
	if len(s.recentTools) > 0 {
		toolsSection = fmt.Sprintf("\n\nRecently used tools: %s", strings.Join(s.recentTools, ", "))
	}

	userContent := fmt.Sprintf("Query: %s\n\nAvailable memories:\n%s%s", query, manifest, toolsSection)

	// Build a single-turn message — same pattern the engine uses for
	// streaming chat, but without tool registry or thinking budget.
	userMsg := message.Message{Role: message.RoleUser}
	userMsg.AddText(userContent)

	stream, err := s.client.StreamMessages(ctx, []message.Message{userMsg}, selectMemoriesSystemPrompt, nil, 0)
	if err != nil {
		return nil
	}

	var text strings.Builder
	for evt := range stream {
		if evt.Type == "assistant_text" {
			text.WriteString(evt.TextDelta)
		}
	}

	var parsed struct {
		SelectedMemories []string `json:"selected_memories"`
	}
	if err := json.Unmarshal([]byte(text.String()), &parsed); err != nil {
		return nil
	}

	// Validate returned filenames and cap at maxMemories
	var result []string
	for _, filename := range parsed.SelectedMemories {
		if validFilenames[filename] {
			result = append(result, filename)
			if len(result) >= s.maxMemories {
				break
			}
		}
	}
	return result
}
