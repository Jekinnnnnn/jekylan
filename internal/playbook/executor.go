package playbook

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/Jekinnnnnn/jekylan/internal/agent"
)

// Executor drives the execution of an ExecutionPlan.
type Executor struct {
	Spawner       agent.AgentSpawner
	lifecycleHook func(running bool)
	vars          map[string]string
}

// Option configures an Executor.
type Option func(*Executor)

// WithLifecycleHook registers a callback invoked when playbook execution
// starts (true) and ends (false).
func WithLifecycleHook(hook func(running bool)) Option {
	return func(e *Executor) {
		e.lifecycleHook = hook
	}
}

// NewExecutor creates a new playbook executor.
func NewExecutor(spawner agent.AgentSpawner, opts ...Option) *Executor {
	e := &Executor{
		Spawner: spawner,
		vars:    make(map[string]string),
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// SetVar sets an initial variable available to all steps.
func (e *Executor) SetVar(name, value string) {
	e.vars[name] = value
}

// Execute runs the full execution plan.
// It returns the final variable map (including all step outputs).
func (e *Executor) Execute(ctx context.Context, plan *ExecutionPlan) (map[string]string, error) {
	// Lock agent creation while playbook is running.
	if e.lifecycleHook != nil {
		e.lifecycleHook(true)
		defer e.lifecycleHook(false)
	}

	if err := e.executePlan(ctx, plan); err != nil {
		return nil, err
	}
	// Return a copy.
	out := make(map[string]string, len(e.vars))
	for k, v := range e.vars {
		out[k] = v
	}
	return out, nil
}

func (e *Executor) executePlan(ctx context.Context, plan *ExecutionPlan) error {
	for phaseIdx, phase := range plan.Phases {
		if err := e.executePhase(ctx, phaseIdx, phase); err != nil {
			return err
		}
	}
	return nil
}

func (e *Executor) executePhase(ctx context.Context, phaseIdx int, phase Phase) error {
	if !phase.Parallel {
		for stepIdx, step := range phase.Steps {
			if err := e.executeStep(ctx, phaseIdx, stepIdx, step); err != nil {
				return fmt.Errorf("phase %d step %d: %w", phaseIdx, stepIdx, err)
			}
		}
		return nil
	}
	return e.executeParallelPhase(ctx, phaseIdx, phase)
}

func (e *Executor) executeParallelPhase(ctx context.Context, phaseIdx int, phase Phase) error {
	// Spawn all agents first, then wait for all.
	ids := make([]string, len(phase.Steps))
	for i, step := range phase.Steps {
		id, err := e.spawnStep(ctx, step)
		if err != nil {
			for j := 0; j < i; j++ {
				e.Spawner.Kill(ids[j])
			}
			return fmt.Errorf("phase %d step %d spawn: %w", phaseIdx, i, err)
		}
		ids[i] = id
	}

	var firstErr error
	for i, id := range ids {
		ra := e.Spawner.Wait(id)
		if ra == nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("phase %d step %d: agent %s not found", phaseIdx, i, id)
			}
			continue
		}

		if firstErr != nil && ra.Status != agent.StatusCompleted {
			// An earlier step already failed; this agent was either killed by us
			// or failed independently. We only report the first error.
			continue
		}

		if ra.Status != agent.StatusCompleted {
			firstErr = fmt.Errorf("phase %d step %d (%s): status=%s error=%s", phaseIdx, i, ra.ID, ra.Status, ra.Error)
			for j := i + 1; j < len(ids); j++ {
				e.Spawner.Kill(ids[j])
			}
			continue
		}

		step := phase.Steps[i]
		if step.OutputVar != "" {
			e.vars[step.OutputVar] = ra.Result
		}
		if step.SubPlan != nil {
			if err := e.executePlan(ctx, step.SubPlan); err != nil {
				if firstErr == nil {
					firstErr = fmt.Errorf("phase %d step %d sub-plan: %w", phaseIdx, i, err)
				}
			}
		}
	}

	return firstErr
}

func (e *Executor) executeStep(ctx context.Context, phaseIdx, stepIdx int, step Step) error {
	ra, err := e.runAgent(ctx, step)
	if err != nil {
		return err
	}
	if ra.Status != agent.StatusCompleted {
		return fmt.Errorf("status=%s error=%s", ra.Status, ra.Error)
	}
	if step.OutputVar != "" {
		e.vars[step.OutputVar] = ra.Result
	}
	if step.SubPlan != nil {
		if err := e.executePlan(ctx, step.SubPlan); err != nil {
			return fmt.Errorf("sub-plan: %w", err)
		}
	}
	return nil
}

func (e *Executor) spawnStep(ctx context.Context, step Step) (string, error) {
	prompt, err := substVars(step.Prompt, e.vars)
	if err != nil {
		return "", fmt.Errorf("variable substitution: %w", err)
	}

	if step.Confirm {
		prompt += "\n\nImportant: After completing this task, you MUST follow these steps in order:\n" +
			"1. Call the confirm tool, with the summary parameter describing your result\n" +
			"2. Wait for user confirmation\n" +
			"3. After user confirmation, output the final result (the result only, do not repeat the confirm tool's response text)"
	}

	id, err := e.Spawner.Spawn(ctx, step.AgentType, prompt)
	if err != nil {
		return "", err
	}
	return id, nil
}

func (e *Executor) runAgent(ctx context.Context, step Step) (*agent.RunningAgent, error) {
	id, err := e.spawnStep(ctx, step)
	if err != nil {
		return nil, err
	}
	ra := e.Spawner.Wait(id)
	if ra == nil {
		return nil, fmt.Errorf("agent %s not found after wait", id)
	}
	return ra, nil
}

var varRegex = regexp.MustCompile(`\$\{(\w+)\}`)

func substVars(template string, vars map[string]string) (string, error) {
	var undefined []string
	result := varRegex.ReplaceAllStringFunc(template, func(match string) string {
		name := match[2 : len(match)-1]
		if val, ok := vars[name]; ok {
			return val
		}
		undefined = append(undefined, name)
		return match
	})
	if len(undefined) > 0 {
		return "", fmt.Errorf("undefined variables: %s", strings.Join(undefined, ", "))
	}
	return result, nil
}
