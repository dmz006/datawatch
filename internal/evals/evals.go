// Package evals (BL259 Phase 1, v6.10.0) implements the PAI Evals
// framework: structured rubric-based scoring across grader types.
// Replaces the binary verifier with multi-grader scoring.
//
// PAI source: docs/plans/2026-05-02-pai-comparison-analysis.md §3 +
// Recommendation M1.
//
// Concepts:
//
//	Grader  — atomic test: { string_match | regex_match | llm_rubric | binary_test }.
//	Case    — one test row: { name, input, expected, grader }.
//	Suite   — collection of cases with mode (capability vs regression)
//	          and pass_threshold.
//	Run     — execution of a suite producing per-case results +
//	          aggregate pass_rate. Persisted JSON-on-disk under
//	          ~/.datawatch/evals/runs/<id>.json.
//
// llm_rubric is stubbed in v6.10.0 — returns a "manual review needed"
// placeholder result. Real LLM grading wires up in a follow-up.
package evals

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
)

// GraderType enumerates the supported grader kinds.
type GraderType string

const (
	GraderStringMatch GraderType = "string_match"
	GraderRegexMatch  GraderType = "regex_match"
	GraderLLMRubric   GraderType = "llm_rubric"
	GraderBinaryTest  GraderType = "binary_test"
)

// Mode is the suite execution mode.
type Mode string

const (
	ModeCapability Mode = "capability" // ~70% pass target for new features
	ModeRegression Mode = "regression" // ~99% pass target for existing features
)

// Grader holds the per-case grader spec.
type Grader struct {
	Type    GraderType `yaml:"type" json:"type"`
	Pattern string     `yaml:"pattern,omitempty" json:"pattern,omitempty"`         // regex_match
	Strict  bool       `yaml:"strict,omitempty" json:"strict,omitempty"`           // string_match: exact match
	Command string     `yaml:"command,omitempty" json:"command,omitempty"`         // binary_test
	Rubric  string     `yaml:"rubric,omitempty" json:"rubric,omitempty"`           // llm_rubric
	Model   string     `yaml:"model,omitempty" json:"model,omitempty"`             // llm_rubric model override
}

// Case is one test row.
type Case struct {
	Name     string `yaml:"name" json:"name"`
	Input    string `yaml:"input,omitempty" json:"input,omitempty"`
	Expected string `yaml:"expected,omitempty" json:"expected,omitempty"`
	Grader   Grader `yaml:"grader" json:"grader"`
}

// Suite holds the YAML-defined eval definition.
type Suite struct {
	Name          string  `yaml:"name" json:"name"`
	Description   string  `yaml:"description,omitempty" json:"description,omitempty"`
	Mode          Mode    `yaml:"mode" json:"mode"`
	PassThreshold float64 `yaml:"pass_threshold" json:"pass_threshold"` // 0.0..1.0
	Cases         []Case  `yaml:"cases" json:"cases"`
}

// CaseResult is the outcome for one Case.
type CaseResult struct {
	Name     string  `json:"name"`
	Pass     bool    `json:"pass"`
	Score    float64 `json:"score"` // 1.0 for pass, 0.0 for fail (rubric may differ)
	Feedback string  `json:"feedback,omitempty"`
	Actual   string  `json:"actual,omitempty"`
}

// Run is one suite execution.
type Run struct {
	ID         string       `json:"id"`
	Suite      string       `json:"suite"`
	Mode       Mode         `json:"mode"`
	StartedAt  time.Time    `json:"started_at"`
	FinishedAt time.Time    `json:"finished_at"`
	PassRate   float64      `json:"pass_rate"`
	Pass       bool         `json:"pass"` // pass_rate >= threshold
	Threshold  float64      `json:"threshold"`
	Results    []CaseResult `json:"results"`
}

// Runner loads suites + executes them, persisting runs to disk.
type Runner struct {
	mu      sync.Mutex
	dataDir string // ~/.datawatch
}

// NewRunner constructs a Runner rooted at ~/.datawatch/evals/.
// dataDir is the parent (e.g. ~/.datawatch); the runner makes the
// evals/ + evals/runs/ subdirs as needed.
func NewRunner(dataDir string) *Runner {
	return &Runner{dataDir: dataDir}
}

// SuitesDir returns the on-disk path holding suite YAML files.
func (r *Runner) SuitesDir() string { return filepath.Join(r.dataDir, "evals") }

// RunsDir returns the on-disk path holding persisted Run JSON files.
func (r *Runner) RunsDir() string { return filepath.Join(r.dataDir, "evals", "runs") }

// LoadSuite reads ~/.datawatch/evals/<name>.yaml.
func (r *Runner) LoadSuite(name string) (*Suite, error) {
	path := filepath.Join(r.SuitesDir(), name+".yaml")
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s Suite
	if err := yaml.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("yaml: %w", err)
	}
	if s.Name == "" {
		s.Name = name
	}
	if s.PassThreshold == 0 {
		if s.Mode == ModeRegression {
			s.PassThreshold = 0.99
		} else {
			s.PassThreshold = 0.7
		}
	}
	return &s, nil
}

// ListSuites returns the basenames of all *.yaml files under SuitesDir.
func (r *Runner) ListSuites() ([]string, error) {
	dir := r.SuitesDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if strings.HasSuffix(n, ".yaml") || strings.HasSuffix(n, ".yml") {
			out = append(out, strings.TrimSuffix(strings.TrimSuffix(n, ".yaml"), ".yml"))
		}
	}
	sort.Strings(out)
	return out, nil
}

// Execute runs the suite, persisting the Run JSON. Returns the Run.
func (r *Runner) Execute(s *Suite) (*Run, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	run := &Run{
		ID:        uuid.NewString(),
		Suite:     s.Name,
		Mode:      s.Mode,
		StartedAt: time.Now().UTC(),
		Threshold: s.PassThreshold,
		Results:   make([]CaseResult, 0, len(s.Cases)),
	}
	passes := 0
	for _, c := range s.Cases {
		res := Grade(c)
		if res.Pass {
			passes++
		}
		run.Results = append(run.Results, res)
	}
	run.FinishedAt = time.Now().UTC()
	if len(s.Cases) > 0 {
		run.PassRate = float64(passes) / float64(len(s.Cases))
	}
	run.Pass = run.PassRate >= run.Threshold
	if err := r.persistRun(run); err != nil {
		return run, err
	}
	return run, nil
}

func (r *Runner) persistRun(run *Run) error {
	dir := r.RunsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(run, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, run.ID+".json"), b, 0o644)
}

// LoadRun reads a persisted run by ID.
func (r *Runner) LoadRun(id string) (*Run, error) {
	path := filepath.Join(r.RunsDir(), id+".json")
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var run Run
	if err := json.Unmarshal(b, &run); err != nil {
		return nil, err
	}
	return &run, nil
}

// ListRuns returns persisted runs, optionally filtered by suite. Most
// recent first.
func (r *Runner) ListRuns(suite string, limit int) ([]*Run, error) {
	dir := r.RunsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := []*Run{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".json")
		run, err := r.LoadRun(id)
		if err != nil {
			continue
		}
		if suite != "" && run.Suite != suite {
			continue
		}
		out = append(out, run)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// Grade applies the case's grader to the case's input/expected and
// returns the case result. The "actual" output for v6.10.0 is the
// case's Input — i.e. the eval treats Input as the LLM-emitted answer
// to grade. For binary_test, Input is the cmdline arg (string).
//
// llm_rubric is stubbed: returns Pass=false with a "manual review
// needed" feedback message so operators see the case but don't
// silently get a false-positive pass. Real LLM grading is filed for
// a v6.10 follow-up.
func Grade(c Case) CaseResult {
	switch c.Grader.Type {
	case GraderStringMatch:
		return gradeStringMatch(c)
	case GraderRegexMatch:
		return gradeRegexMatch(c)
	case GraderBinaryTest:
		return gradeBinaryTest(c)
	case GraderLLMRubric:
		return CaseResult{
			Name:     c.Name,
			Pass:     false,
			Score:    0,
			Feedback: "llm_rubric: manual review needed (LLM grading shipped in v6.10 follow-up)",
			Actual:   c.Input,
		}
	default:
		return CaseResult{Name: c.Name, Pass: false, Feedback: "unknown grader type: " + string(c.Grader.Type)}
	}
}

func gradeStringMatch(c Case) CaseResult {
	pass := false
	if c.Grader.Strict {
		pass = c.Input == c.Expected
	} else {
		// case-insensitive contains
		pass = strings.Contains(strings.ToLower(c.Input), strings.ToLower(c.Expected))
	}
	score := 0.0
	fb := "expected substring not found"
	if pass {
		score = 1.0
		fb = "match"
	}
	return CaseResult{Name: c.Name, Pass: pass, Score: score, Feedback: fb, Actual: c.Input}
}

func gradeRegexMatch(c Case) CaseResult {
	pat := c.Grader.Pattern
	if pat == "" {
		pat = c.Expected
	}
	re, err := regexp.Compile(pat)
	if err != nil {
		return CaseResult{Name: c.Name, Feedback: "compile: " + err.Error(), Actual: c.Input}
	}
	pass := re.MatchString(c.Input)
	score := 0.0
	fb := "regex did not match"
	if pass {
		score = 1.0
		fb = "match"
	}
	return CaseResult{Name: c.Name, Pass: pass, Score: score, Feedback: fb, Actual: c.Input}
}

func gradeBinaryTest(c Case) CaseResult {
	cmdline := c.Grader.Command
	if cmdline == "" {
		return CaseResult{Name: c.Name, Feedback: "binary_test: grader.command is required"}
	}
	// Run via /bin/sh -c so the case can reference $INPUT.
	cmd := exec.Command("/bin/sh", "-c", cmdline)
	cmd.Env = append(os.Environ(), "INPUT="+c.Input, "EXPECTED="+c.Expected)
	out, err := cmd.CombinedOutput()
	pass := err == nil
	score := 0.0
	fb := "exit ok"
	if !pass {
		fb = "exit non-zero: " + err.Error()
	} else {
		score = 1.0
	}
	return CaseResult{Name: c.Name, Pass: pass, Score: score, Feedback: fb, Actual: string(out)}
}
