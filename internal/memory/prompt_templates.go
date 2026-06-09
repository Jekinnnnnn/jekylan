package memory

// MemoryType represents one of the four memory taxonomy types.
type MemoryType string

const (
	MemoryTypeUser      MemoryType = "user"
	MemoryTypeFeedback  MemoryType = "feedback"
	MemoryTypeProject   MemoryType = "project"
	MemoryTypeReference MemoryType = "reference"
)

var memoryTypes = []MemoryType{
	MemoryTypeUser,
	MemoryTypeFeedback,
	MemoryTypeProject,
	MemoryTypeReference,
}

// ParseMemoryType parses a raw frontmatter value into a MemoryType.
// Invalid or missing values return the empty string.
func ParseMemoryType(raw string) MemoryType {
	for _, t := range memoryTypes {
		if string(t) == raw {
			return t
		}
	}
	return ""
}

// TypesSectionIndividual returns the `## Types of memory` section for
// single-directory (individual-only) mode.
func TypesSectionIndividual() string {
	return `## Types of memory

There are several discrete types of memory that you can store in your memory system:

<types>
<type>
    <name>user</name>
    <description>Information about the user's role, expertise, and preferences. Use these to tailor your communication style and explanation depth.</description>
    <when_to_save>When you learn any details about the user's role, preferences, responsibilities, or knowledge</when_to_save>
    <how_to_use>When your work should be informed by the user's profile or perspective. For example, if the user is asking you to explain a part of the code, you should answer that question in a way that is tailored to the specific details that they will find most valuable or that helps them build their mental model in relation to domain knowledge they already have.</how_to_use>
    <examples>
    user: I'm a data scientist investigating what logging we have in place
    assistant: [saves user memory: user is a data scientist, currently focused on observability/logging]

    user: I've been writing Go for ten years but this is my first time touching the React side of this repo
    assistant: [saves user memory: deep Go expertise, new to React and this project's frontend — frame frontend explanations in terms of backend analogues]
    </examples>
</type>
<type>
    <name>feedback</name>
    <description>User guidance on how to approach work — corrections to avoid and approaches to keep. Record both failures and successes.</description>
    <when_to_save>Any time the user corrects your approach ("no not that", "don't", "stop doing X") OR confirms a non-obvious approach worked ("yes exactly", "perfect, keep doing that", accepting an unusual choice without pushback). Corrections are easy to notice; confirmations are quieter — watch for them. In both cases, save what is applicable to future conversations, especially if surprising or not obvious from the code. Include *why* so you can judge edge cases later.</when_to_save>
    <how_to_use>Let these memories guide your behavior so that the user does not need to offer the same guidance twice.</how_to_use>
    <body_structure>Lead with the rule itself, then a **Why:** line (the reason the user gave — often a past incident or strong preference) and a **How to apply:** line (when/where this guidance kicks in). Knowing *why* lets you judge edge cases instead of blindly following the rule.</body_structure>
    <examples>
    user: don't mock the database in these tests — we got burned last quarter when mocked tests passed but the prod migration failed
    assistant: [saves feedback memory: integration tests must hit a real database, not mocks. Reason: prior incident where mock/prod divergence masked a broken migration]

    user: stop summarizing what you just did at the end of every response, I can read the diff
    assistant: [saves feedback memory: this user wants terse responses with no trailing summaries]

    user: yeah the single bundled PR was the right call here, splitting this one would've just been churn
    assistant: [saves feedback memory: for refactors in this area, user prefers one bundled PR over many small ones. Confirmed after I chose this approach — a validated judgment call, not a correction]
    </examples>
</type>
<type>
    <name>project</name>
    <description>Ongoing work, goals, and decisions not derivable from code or git history. Helps understand context and motivation behind user requests.</description>
    <when_to_save>When you learn who is doing what, why, or by when. These states change relatively quickly so try to keep your understanding of this up to date. Always convert relative dates in user messages to absolute dates when saving (e.g., "Thursday" → "2026-03-05"), so the memory remains interpretable after time passes.</when_to_save>
    <how_to_use>Use these memories to more fully understand the details and nuance behind the user's request and make better informed suggestions.</how_to_use>
    <body_structure>Lead with the fact or decision, then a **Why:** line (the motivation — often a constraint, deadline, or stakeholder ask) and a **How to apply:** line (how this should shape your suggestions). Project memories decay fast, so the why helps future-you judge whether the memory is still load-bearing.</body_structure>
    <examples>
    user: we're freezing all non-critical merges after Thursday — mobile team is cutting a release branch
    assistant: [saves project memory: merge freeze begins 2026-03-05 for mobile release cut. Flag any non-critical PR work scheduled after that date]

    user: the reason we're ripping out the old auth middleware is that legal flagged it for storing session tokens in a way that doesn't meet the new compliance requirements
    assistant: [saves project memory: auth middleware rewrite is driven by legal/compliance requirements around session token storage, not tech-debt cleanup — scope decisions should favor compliance over ergonomics]
    </examples>
</type>
<type>
    <name>reference</name>
    <description>Pointers to external resources and where to find up-to-date information.</description>
    <when_to_save>When you learn about resources in external systems and their purpose. For example, that bugs are tracked in a specific project in Linear or that feedback can be found in a specific Slack channel.</when_to_save>
    <how_to_use>When the user references an external system or information that may be in an external system.</how_to_use>
    <examples>
    user: check the Linear project "INGEST" if you want context on these tickets, that's where we track all pipeline bugs
    assistant: [saves reference memory: pipeline bugs are tracked in Linear project "INGEST"]

    user: the Grafana board at grafana.internal/d/api-latency is what oncall watches — if you're touching request handling, that's the thing that'll page someone
    assistant: [saves reference memory: grafana.internal/d/api-latency is the oncall latency dashboard — check it when editing request-path code]
    </examples>
</type>
</types>`
}

// WhatNotToSaveSection returns the `## What NOT to save in memory` section.
func WhatNotToSaveSection() string {
	return `## What NOT to save in memory

- Code patterns, conventions, architecture, file paths, or project structure — these can be derived by reading the current project state.
- Git history, recent changes, or who-changed-what — ` + "`git log`" + ` / ` + "`git blame`" + ` are authoritative.
- Debugging solutions or fix recipes — the fix is in the code; the commit message has the context.
- Ephemeral task details: in-progress work, temporary state, current conversation context.

These exclusions apply even when the user explicitly asks you to save. If they ask you to save a PR list or activity summary, ask what was *surprising* or *non-obvious* about it — that is the part worth keeping.`
}

// MemoryDriftCaveat is the single drift caveat bullet.
const MemoryDriftCaveat = "- Memory can become stale. If it conflicts with current state, trust what you observe and update or remove the stale entry."

// WhenToAccessSection returns the `## When to access memories` section.
func WhenToAccessSection() string {
	return `## When to access memories
- Access when relevant or the user references prior work
- You MUST access memory when the user explicitly asks to check, recall, or remember
- If the user says to ignore memory: proceed as if MEMORY.md were empty
` + MemoryDriftCaveat
}

// TrustingRecallSection returns the `## Before recommending from memory` section.
func TrustingRecallSection() string {
	return `## Before recommending from memory

Memories about specific files, functions, or flags may be stale. Verify before recommending:

- Check file paths exist
- Grep for function/flag names
- For recent/current state, prefer ` + "`git log`" + ` or reading code over memory`
}

// MemoryFrontmatterExample returns the frontmatter format example.
func MemoryFrontmatterExample() string {
	return "```markdown\n---\nname: {{memory name}}\ndescription: {{one-line description — used to decide relevance in future conversations, so be specific}}\ntype: {{user, feedback, project, reference}}\n---\n\n{{memory content — for feedback/project types, structure as: rule/fact, then **Why:** and **How to apply:** lines}}\n```"
}
