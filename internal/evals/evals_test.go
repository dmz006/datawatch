package evals

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGradeStringMatchSubstring(t *testing.T) {
	r := Grade(Case{
		Name:     "ok",
		Input:    "Hello WORLD output",
		Expected: "world",
		Grader:   Grader{Type: GraderStringMatch},
	})
	if !r.Pass || r.Score != 1.0 {
		t.Errorf("ci-substring: pass=%v score=%v", r.Pass, r.Score)
	}
}

func TestGradeStringMatchStrict(t *testing.T) {
	r := Grade(Case{
		Name:     "strict-fail",
		Input:    "Hello",
		Expected: "hello",
		Grader:   Grader{Type: GraderStringMatch, Strict: true},
	})
	if r.Pass {
		t.Errorf("strict should be case-sensitive: %+v", r)
	}
}

func TestGradeRegexMatch(t *testing.T) {
	r := Grade(Case{
		Name:   "re",
		Input:  "result: 42",
		Grader: Grader{Type: GraderRegexMatch, Pattern: `result:\s*\d+`},
	})
	if !r.Pass {
		t.Errorf("regex match: %+v", r)
	}
}

func TestGradeRegexMissPattern(t *testing.T) {
	// Pattern empty -> falls back to Expected
	r := Grade(Case{
		Name:     "expected-as-regex",
		Input:    "hello42",
		Expected: `\d+`,
		Grader:   Grader{Type: GraderRegexMatch},
	})
	if !r.Pass {
		t.Errorf("expected fallback: %+v", r)
	}
}

func TestGradeBinaryTestExitCode(t *testing.T) {
	r := Grade(Case{
		Name:   "true",
		Grader: Grader{Type: GraderBinaryTest, Command: "true"},
	})
	if !r.Pass {
		t.Errorf("true should pass: %+v", r)
	}
	r2 := Grade(Case{
		Name:   "false",
		Grader: Grader{Type: GraderBinaryTest, Command: "false"},
	})
	if r2.Pass {
		t.Errorf("false should fail: %+v", r2)
	}
}

func TestGradeBinaryTestUsesInputEnv(t *testing.T) {
	r := Grade(Case{
		Name:   "echo-input",
		Input:  "abc",
		Grader: Grader{Type: GraderBinaryTest, Command: `[ "$INPUT" = "abc" ]`},
	})
	if !r.Pass {
		t.Errorf("expected pass with INPUT env: %+v", r)
	}
}

func TestGradeLLMRubricStubbed(t *testing.T) {
	r := Grade(Case{Name: "x", Input: "y", Grader: Grader{Type: GraderLLMRubric, Rubric: "always pass"}})
	if r.Pass {
		t.Errorf("llm_rubric stub should not auto-pass: %+v", r)
	}
	if !strings.Contains(r.Feedback, "manual review needed") {
		t.Errorf("expected manual-review feedback: %q", r.Feedback)
	}
}

func TestGradeUnknownGrader(t *testing.T) {
	r := Grade(Case{Name: "x", Grader: Grader{Type: "nope"}})
	if r.Pass {
		t.Errorf("unknown should fail: %+v", r)
	}
}

func TestRunnerExecuteCapabilityModeAllPass(t *testing.T) {
	dir := t.TempDir()
	r := NewRunner(dir)
	suite := &Suite{
		Name: "smoke", Mode: ModeCapability,
		Cases: []Case{
			{Name: "a", Input: "hello world", Expected: "hello", Grader: Grader{Type: GraderStringMatch}},
			{Name: "b", Input: "result: 7", Grader: Grader{Type: GraderRegexMatch, Pattern: `result:\s*\d+`}},
		},
	}
	run, err := r.Execute(suite)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !run.Pass {
		t.Errorf("expected pass: pass_rate=%v threshold=%v", run.PassRate, run.Threshold)
	}
	if run.PassRate != 1.0 {
		t.Errorf("pass_rate: %v", run.PassRate)
	}
	// On disk?
	path := filepath.Join(r.RunsDir(), run.ID+".json")
	if _, err := os.Stat(path); err != nil {
		t.Errorf("run not persisted: %v", err)
	}
}

func TestRunnerListSuitesEmptyDir(t *testing.T) {
	dir := t.TempDir()
	r := NewRunner(dir)
	got, err := r.ListSuites()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("non-empty: %v", got)
	}
}

func TestRunnerLoadSuite(t *testing.T) {
	dir := t.TempDir()
	r := NewRunner(dir)
	if err := os.MkdirAll(r.SuitesDir(), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	yaml := `name: smoke
mode: capability
pass_threshold: 0.5
cases:
  - name: a
    input: "ok"
    expected: "ok"
    grader: { type: string_match, strict: true }
`
	if err := os.WriteFile(filepath.Join(r.SuitesDir(), "smoke.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	s, err := r.LoadSuite("smoke")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if s.Name != "smoke" || s.Mode != ModeCapability || s.PassThreshold != 0.5 || len(s.Cases) != 1 {
		t.Errorf("loaded: %+v", s)
	}
	suites, err := r.ListSuites()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(suites) != 1 || suites[0] != "smoke" {
		t.Errorf("list: %v", suites)
	}
}

func TestRunnerLoadRun(t *testing.T) {
	dir := t.TempDir()
	r := NewRunner(dir)
	suite := &Suite{Name: "t", Cases: []Case{{Name: "a", Input: "x", Expected: "x", Grader: Grader{Type: GraderStringMatch, Strict: true}}}}
	run, err := r.Execute(suite)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	got, err := r.LoadRun(run.ID)
	if err != nil {
		t.Fatalf("load run: %v", err)
	}
	if got.ID != run.ID || got.Suite != "t" {
		t.Errorf("loaded: %+v", got)
	}
}

func TestRunnerListRunsFilteredAndLimited(t *testing.T) {
	dir := t.TempDir()
	r := NewRunner(dir)
	for _, n := range []string{"a", "a", "b"} {
		_, _ = r.Execute(&Suite{Name: n, Cases: []Case{{Name: "c", Input: "ok", Expected: "ok", Grader: Grader{Type: GraderStringMatch, Strict: true}}}})
	}
	all, err := r.ListRuns("", 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("all: %d", len(all))
	}
	onlyA, _ := r.ListRuns("a", 0)
	if len(onlyA) != 2 {
		t.Errorf("a: %d", len(onlyA))
	}
	limit1, _ := r.ListRuns("", 1)
	if len(limit1) != 1 {
		t.Errorf("limit1: %d", len(limit1))
	}
}

func TestRegressionThresholdDefault(t *testing.T) {
	dir := t.TempDir()
	r := NewRunner(dir)
	if err := os.MkdirAll(r.SuitesDir(), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	yaml := `name: r
mode: regression
cases:
  - name: a
    input: ok
    expected: ok
    grader: { type: string_match, strict: true }
`
	if err := os.WriteFile(filepath.Join(r.SuitesDir(), "r.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	s, _ := r.LoadSuite("r")
	if s.PassThreshold != 0.99 {
		t.Errorf("regression threshold: %v", s.PassThreshold)
	}
}

func TestCapabilityThresholdDefault(t *testing.T) {
	dir := t.TempDir()
	r := NewRunner(dir)
	if err := os.MkdirAll(r.SuitesDir(), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	yaml := `name: c
mode: capability
cases:
  - name: a
    input: ok
    expected: ok
    grader: { type: string_match, strict: true }
`
	if err := os.WriteFile(filepath.Join(r.SuitesDir(), "c.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	s, _ := r.LoadSuite("c")
	if s.PassThreshold != 0.7 {
		t.Errorf("capability threshold: %v", s.PassThreshold)
	}
}
