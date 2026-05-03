// BL221 (v6.2.0) Phase 3 — scan framework.
// scan.Run orchestrates SAST, secrets, and dependency scanners against a
// project directory and optionally grades findings through an LLM.
// GraderFn is injected from cmd/datawatch/main.go (same pattern as
// DecomposeFn) so this package stays free of server-layer imports.

package scan

import (
	"fmt"
	"time"
)

// Severity levels in ascending order.
type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityError    Severity = "error"
	SeverityCritical Severity = "critical"
)

// Finding is one result entry from any scanner.
type Finding struct {
	Scanner  string   `json:"scanner"`
	File     string   `json:"file,omitempty"`
	Line     int      `json:"line,omitempty"`
	Severity Severity `json:"severity"`
	RuleID   string   `json:"rule_id,omitempty"`
	Message  string   `json:"message"`
	Fixable  bool     `json:"fixable,omitempty"`
}

// Result is the output of one complete scan run.
type Result struct {
	At       time.Time `json:"at"`
	PRDID    string    `json:"prd_id,omitempty"`
	Findings []Finding `json:"findings"`
	Pass     bool      `json:"pass"`
	Verdict  string    `json:"verdict,omitempty"` // pass | warn | fail
	Notes    string    `json:"notes,omitempty"`
	Error    string    `json:"error,omitempty"`
}

// Config holds the operator-tunable scan knobs embedded in autonomous.Config.
type Config struct {
	Enabled            bool     `json:"enabled"`
	SASTEnabled        bool     `json:"sast_enabled"`
	SecretsEnabled     bool     `json:"secrets_enabled"`
	DepsEnabled        bool     `json:"deps_enabled"`
	FailOnSeverity     Severity `json:"fail_on_severity,omitempty"` // default "error"
	MaxFindings        int      `json:"max_findings,omitempty"`
	RulesGraderEnabled bool     `json:"rules_grader_enabled,omitempty"`
	FixLoopEnabled     bool     `json:"fix_loop_enabled,omitempty"`
	FixLoopMaxRetries  int      `json:"fix_loop_max_retries,omitempty"`
}

// DefaultConfig returns safe defaults (all scanners on, LLM grader off
// until operator configures an ask-compatible backend).
func DefaultConfig() Config {
	return Config{
		Enabled:        true,
		SASTEnabled:    true,
		SecretsEnabled: true,
		DepsEnabled:    true,
		FailOnSeverity: SeverityError,
	}
}

// Scanner performs one class of scan over a project directory.
type Scanner interface {
	Name() string
	Scan(dir string) ([]Finding, error)
}

// GraderFn grades a finding list using an LLM.
// Returns verdict ("pass"|"warn"|"fail") and free-form notes.
// Injected from outside; nil disables LLM grading.
type GraderFn func(findings []Finding, projectDir string) (verdict, notes string, err error)

// RuleEditorFn asks an LLM to propose AGENT.md changes based on findings.
// Returns proposed diff text. Injected from outside.
type RuleEditorFn func(findings []Finding, projectDir string) (proposedDiff string, err error)

// Run executes scanners against dir, applies config filters, then optionally
// calls grader for an LLM-based verdict.
func Run(dir string, cfg Config, scanners []Scanner, grader GraderFn) Result {
	r := Result{At: time.Now()}

	for _, sc := range scanners {
		findings, err := sc.Scan(dir)
		if err != nil {
			msg := fmt.Sprintf("%s: %v", sc.Name(), err)
			if r.Error == "" {
				r.Error = msg
			} else {
				r.Error += "; " + msg
			}
			continue
		}
		r.Findings = append(r.Findings, findings...)
	}

	if cfg.MaxFindings > 0 && len(r.Findings) > cfg.MaxFindings {
		r.Findings = r.Findings[:cfg.MaxFindings]
	}

	threshold := cfg.FailOnSeverity
	if threshold == "" {
		threshold = SeverityError
	}
	r.Pass = !hasFailingFindings(r.Findings, threshold)

	if grader != nil && cfg.RulesGraderEnabled && len(r.Findings) > 0 {
		verdict, notes, err := grader(r.Findings, dir)
		if err != nil {
			r.Notes = "grader error: " + err.Error()
		} else {
			r.Verdict = verdict
			r.Notes = notes
			if verdict == "fail" {
				r.Pass = false
			}
		}
	}

	if r.Verdict == "" {
		if r.Pass {
			r.Verdict = "pass"
		} else {
			r.Verdict = "fail"
		}
	}

	return r
}

var severityOrder = map[Severity]int{
	SeverityInfo:     0,
	SeverityWarning:  1,
	SeverityError:    2,
	SeverityCritical: 3,
}

func hasFailingFindings(fs []Finding, threshold Severity) bool {
	thresh := severityOrder[threshold]
	for _, f := range fs {
		if severityOrder[f.Severity] >= thresh {
			return true
		}
	}
	return false
}

// BuildFixSpec constructs a Claude Code task spec from scan findings
// suitable for creating a fix mini-PRD.
func BuildFixSpec(findings []Finding) string {
	if len(findings) == 0 {
		return ""
	}
	spec := "Fix the following scan violations found in the project:\n\n"
	for _, f := range findings {
		loc := f.File
		if f.Line > 0 {
			loc = fmt.Sprintf("%s:%d", f.File, f.Line)
		}
		if loc == "" {
			loc = "(project)"
		}
		spec += fmt.Sprintf("- [%s] %s — %s (%s)\n", f.Severity, loc, f.Message, f.RuleID)
	}
	spec += "\nFor each violation:\n"
	spec += "1. Locate the offending code\n"
	spec += "2. Apply the minimal safe fix\n"
	spec += "3. Verify the file still compiles / passes lint\n"
	return spec
}
