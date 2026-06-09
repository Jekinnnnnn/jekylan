package playbook

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	orderedListRegex   = regexp.MustCompile(`^(\d+\.)\s+(.*)$`)
	unorderedListRegex = regexp.MustCompile(`^([-+*])\s+(.*)$`)
	agentTagRegex      = regexp.MustCompile(`\(agent:\s*([\w-]+)\)`)
)

// ExecutionPlan is the parsed workflow structure.
type ExecutionPlan struct {
	Phases []Phase
}

// Phase is a group of steps executed either sequentially or concurrently.
type Phase struct {
	Steps    []Step
	Parallel bool
}

// Step describes a single agent invocation.
type Step struct {
	AgentType   string
	Description string
	Prompt      string
	OutputVar   string
	Confirm     bool
	SubPlan     *ExecutionPlan
}

// stepNode is an intermediate representation used during parsing.
type stepNode struct {
	agentType   string
	description string
	prompt      string
	outputVar   string
	confirm     bool
	ordered     bool
	subPlan     *ExecutionPlan
}

// ParsePlan parses playbook markdown content into an ExecutionPlan.
func ParsePlan(content string) (*ExecutionPlan, error) {
	lines := strings.Split(content, "\n")
	steps, _, err := parseStepNodes(lines, 0, 0)
	if err != nil {
		return nil, err
	}
	if len(steps) == 0 {
		return nil, fmt.Errorf("no steps found in playbook content")
	}
	return buildExecutionPlan(steps), nil
}

func buildExecutionPlan(nodes []stepNode) *ExecutionPlan {
	plan := &ExecutionPlan{}
	if len(nodes) == 0 {
		return plan
	}
	// Group consecutive nodes with the same ordered property into phases.
	start := 0
	for start < len(nodes) {
		parallel := !nodes[start].ordered
		end := start + 1
		for end < len(nodes) && nodes[end].ordered == nodes[start].ordered {
			end++
		}
		phase := Phase{Parallel: parallel}
		for i := start; i < end; i++ {
			phase.Steps = append(phase.Steps, Step{
				AgentType:   nodes[i].agentType,
				Description: nodes[i].description,
				Prompt:      nodes[i].prompt,
				OutputVar:   nodes[i].outputVar,
				Confirm:     nodes[i].confirm,
				SubPlan:     nodes[i].subPlan,
			})
		}
		plan.Phases = append(plan.Phases, phase)
		start = end
	}
	return plan
}

// parseStepNodes recursively parses list items at a given base indent.
// It returns the parsed nodes and the index of the next unconsumed line.
func parseStepNodes(lines []string, start, baseIndent int) ([]stepNode, int, error) {
	var nodes []stepNode
	i := start
	for i < len(lines) {
		line := lines[i]
		if isEmptyLine(line) {
			i++
			continue
		}
		indent := measureIndent(line)
		if indent < baseIndent {
			break
		}
		if indent > baseIndent {
			// Child content of the previous node; skip here because we collect it explicitly.
			i++
			continue
		}

		rest := strings.TrimLeft(line, " \t")
		isOrdered, text := matchListItem(rest)
		if text == "" {
			i++
			continue
		}

		node, nextIdx := parseStepAt(lines, i, baseIndent, isOrdered, text)
		nodes = append(nodes, node)
		i = nextIdx
	}
	return nodes, i, nil
}

// parseStepAt parses a single step starting at line i, including its child content.
// It returns the parsed node and the index of the next line after this step's content.
func parseStepAt(lines []string, i, baseIndent int, isOrdered bool, text string) (stepNode, int) {
	node := stepNode{ordered: isOrdered}
	node.agentType, node.description = parseStepTitle(text)

	childStart := i + 1
	childEnd := childStart
	for childEnd < len(lines) {
		cl := lines[childEnd]
		if isEmptyLine(cl) {
			childEnd++
			continue
		}
		if measureIndent(cl) <= baseIndent {
			break
		}
		childEnd++
	}

	if childStart < childEnd {
		childLines := lines[childStart:childEnd]
		childListIndent := findChildListIndent(childLines)
		if childListIndent != -1 {
			if allChildrenAreProperties(childLines, childListIndent) {
				parseChildProperties(childLines, childListIndent, &node)
			} else {
				subNodes, _, _ := parseStepNodes(lines, childStart, childListIndent)
				if len(subNodes) > 0 {
					node.subPlan = buildExecutionPlan(subNodes)
				}
			}
		}
	}

	return node, childEnd
}

func isEmptyLine(line string) bool {
	return strings.TrimRight(line, " \t") == ""
}

func measureIndent(line string) int {
	return len(line) - len(strings.TrimLeft(line, " \t"))
}

func findChildListIndent(childLines []string) int {
	for _, cl := range childLines {
		if isEmptyLine(cl) {
			continue
		}
		cRest := strings.TrimLeft(cl, " \t")
		if _, ct := matchListItem(cRest); ct != "" {
			return measureIndent(cl)
		}
	}
	return -1
}

func parseChildProperties(childLines []string, listIndent int, node *stepNode) {
	for _, cl := range childLines {
		if isEmptyLine(cl) {
			continue
		}
		if measureIndent(cl) != listIndent {
			continue
		}
		cRest := strings.TrimLeft(cl, " \t")
		if _, ct := matchListItem(cRest); ct != "" {
			parseProperty(ct, node)
		}
	}
}

func matchListItem(s string) (isOrdered bool, text string) {
	if m := orderedListRegex.FindStringSubmatch(s); m != nil {
		return true, m[2]
	}
	if m := unorderedListRegex.FindStringSubmatch(s); m != nil {
		return false, m[2]
	}
	return false, ""
}

func parseStepTitle(text string) (agentType, description string) {
	text = strings.TrimSpace(text)

	// Pattern: (agent: calc-step1) anywhere in the line.
	if m := agentTagRegex.FindStringSubmatch(text); m != nil {
		agentType = m[1]
		description = strings.TrimSpace(agentTagRegex.ReplaceAllString(text, ""))
		return
	}

	// Pattern: calc-step1: description
	if idx := strings.Index(text, ":"); idx > 0 {
		agentType = strings.TrimSpace(text[:idx])
		description = strings.TrimSpace(text[idx+1:])
		return
	}

	description = text
	return
}

func isPropertyLine(text string) bool {
	text = strings.TrimSpace(text)
	knownProps := []string{"prompt", "output", "confirm", "description"}
	lower := strings.ToLower(text)
	for _, prop := range knownProps {
		if strings.HasPrefix(lower, prop+":") {
			return true
		}
	}
	return false
}

func allChildrenAreProperties(lines []string, listIndent int) bool {
	for _, line := range lines {
		if strings.TrimRight(line, " \t") == "" {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " \t"))
		if indent != listIndent {
			continue
		}
		rest := strings.TrimLeft(line, " \t")
		if _, text := matchListItem(rest); text != "" {
			if !isPropertyLine(text) {
				return false
			}
		}
	}
	return true
}

func parseProperty(text string, node *stepNode) {
	text = strings.TrimSpace(text)
	lower := strings.ToLower(text)

	if strings.HasPrefix(lower, "prompt:") {
		node.prompt = trimQuotes(strings.TrimSpace(text[len("prompt:"):]))
		return
	}
	if strings.HasPrefix(lower, "output:") {
		node.outputVar = strings.TrimSpace(text[len("output:"):])
		return
	}
	if strings.HasPrefix(lower, "confirm:") {
		val := strings.TrimSpace(text[len("confirm:"):])
		node.confirm = val == "true" || val == "yes" || val == "on"
		return
	}
	if strings.HasPrefix(lower, "description:") {
		node.description = strings.TrimSpace(text[len("description:"):])
		return
	}
}

// trimQuotes removes a single pair of matching outer quotes if present.
func trimQuotes(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
