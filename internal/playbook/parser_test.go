package playbook

import (
	"testing"
)

func TestParsePlanSimpleOrdered(t *testing.T) {
	content := `
## 步骤
1. calc-step1: 数据识别
   - prompt: "初始数据: ${input}"
   - output: step1_result

2. calc-step2: 预处理
   - prompt: "数据乘10: ${step1_result}"
   - output: step2_result
`
	plan, err := ParsePlan(content)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if len(plan.Phases) != 1 {
		t.Fatalf("expected 1 phase, got %d", len(plan.Phases))
	}
	phase := plan.Phases[0]
	if phase.Parallel {
		t.Error("expected sequential phase")
	}
	if len(phase.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(phase.Steps))
	}
	if phase.Steps[0].AgentType != "calc-step1" {
		t.Errorf("step 0 agent type = %q, want calc-step1", phase.Steps[0].AgentType)
	}
	if phase.Steps[0].Prompt != "初始数据: ${input}" {
		t.Errorf("step 0 prompt = %q", phase.Steps[0].Prompt)
	}
	if phase.Steps[0].OutputVar != "step1_result" {
		t.Errorf("step 0 output = %q", phase.Steps[0].OutputVar)
	}
	if phase.Steps[1].AgentType != "calc-step2" {
		t.Errorf("step 1 agent type = %q", phase.Steps[1].AgentType)
	}
	if phase.Steps[1].Prompt != "数据乘10: ${step1_result}" {
		t.Errorf("step 1 prompt = %q", phase.Steps[1].Prompt)
	}
}

func TestParsePlanParallel(t *testing.T) {
	content := `
- calc-step1: 任务A
  - prompt: "do A"
- calc-step2: 任务B
  - prompt: "do B"
`
	plan, err := ParsePlan(content)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if len(plan.Phases) != 1 {
		t.Fatalf("expected 1 phase, got %d", len(plan.Phases))
	}
	phase := plan.Phases[0]
	if !phase.Parallel {
		t.Error("expected parallel phase")
	}
	if len(phase.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(phase.Steps))
	}
}

func TestParsePlanWithConfirm(t *testing.T) {
	content := `
1. calc-step2
   - prompt: "process"
   - confirm: true
`
	plan, err := ParsePlan(content)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if !plan.Phases[0].Steps[0].Confirm {
		t.Error("expected confirm=true")
	}
}

func TestParsePlanNested(t *testing.T) {
	content := `
1. calc-step1: 父步骤
   1. calc-step2: 子步骤A
      - prompt: "child A"
   2. calc-step3: 子步骤B
      - prompt: "child B"
`
	plan, err := ParsePlan(content)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if len(plan.Phases) != 1 {
		t.Fatalf("expected 1 phase, got %d", len(plan.Phases))
	}
	step := plan.Phases[0].Steps[0]
	if step.SubPlan == nil {
		t.Fatal("expected sub-plan")
	}
	if len(step.SubPlan.Phases) != 1 {
		t.Fatalf("expected 1 sub phase, got %d", len(step.SubPlan.Phases))
	}
	if step.SubPlan.Phases[0].Parallel {
		t.Error("expected sequential sub-plan")
	}
	if len(step.SubPlan.Phases[0].Steps) != 2 {
		t.Fatalf("expected 2 sub-steps, got %d", len(step.SubPlan.Phases[0].Steps))
	}
}

func TestParsePlanEmpty(t *testing.T) {
	_, err := ParsePlan("just some text without lists")
	if err == nil {
		t.Fatal("expected error for empty plan")
	}
}

func TestSubstVars(t *testing.T) {
	vars := map[string]string{"a": "1", "b": "2"}
	out, err := substVars("${a} + ${b}", vars)
	if err != nil {
		t.Fatalf("subst failed: %v", err)
	}
	if out != "1 + 2" {
		t.Errorf("got %q", out)
	}
}

func TestSubstVarsUndefined(t *testing.T) {
	vars := map[string]string{"a": "1"}
	_, err := substVars("${a} + ${missing}", vars)
	if err == nil {
		t.Fatal("expected error for undefined variable")
	}
}

func TestTrimQuotes(t *testing.T) {
	if trimQuotes(`"hello"`) != "hello" {
		t.Error("double quotes")
	}
	if trimQuotes(`'hello'`) != "hello" {
		t.Error("single quotes")
	}
	if trimQuotes(`He said "hello"`) != `He said "hello"` {
		t.Errorf("inner quotes preserved: got %q", trimQuotes(`He said "hello"`))
	}
	if trimQuotes("no quotes") != "no quotes" {
		t.Error("no quotes")
	}
}
