package autonomous

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path"
	"sort"
	"strings"
	"time"
)

// lintFindingsCap bounds the runner output kept in LintGateResult.Findings.
const lintFindingsCap = 8 * 1024

// LintRunner runs the linter from dir over the given package patterns and
// returns its combined output. A non-zero exit must surface as an error that
// wraps *exec.ExitError so the lint gate can tell findings apart from a
// runner that could not start at all.
type LintRunner func(ctx context.Context, dir string, pkgs []string) (string, error)

// DefaultLintRunner is the production LintRunner. It resolves golangci-lint
// on PATH and runs `golangci-lint run <pkgs...>` from dir.
func DefaultLintRunner(ctx context.Context, dir string, pkgs []string) (string, error) {
	bin, err := exec.LookPath("golangci-lint")
	if err != nil {
		return "", fmt.Errorf("lint gate: golangci-lint not found: %w", err)
	}
	args := append([]string{"run"}, pkgs...)
	cmd := exec.CommandContext(ctx, bin, args...) // #nosec G204 -- fixed binary, package patterns from git
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// LintGateResult is the outcome of LintGateCheck. Passed is true when no
// touched Go package has lint findings (or no Go package was touched).
// Packages lists the ./<dir> patterns that were linted, and Findings holds
// the trimmed runner output, capped at 8 KiB.
type LintGateResult struct {
	Passed   bool
	Packages []string
	Findings string
}

// LintGateCheck is the verification lint gate. It collects the files changed
// on the current branch relative to its merge base with main (plus untracked
// files), maps the Go files to their package directories, and runs lint over
// only those packages. A nil lint uses DefaultLintRunner. When no Go package
// is touched the gate passes without running the linter. Lint findings (a
// non-zero runner exit) fail the gate; a runner that cannot start returns an
// error and never counts as a pass.
func LintGateCheck(ctx context.Context, projectDir string, lint LintRunner) (LintGateResult, error) {
	if lint == nil {
		lint = DefaultLintRunner
	}
	files, err := lintGateChangedFiles(ctx, projectDir)
	if err != nil {
		return LintGateResult{}, err
	}
	pkgs := lintGatePackages(files)
	res := LintGateResult{Packages: pkgs}
	if len(pkgs) == 0 {
		res.Passed = true
		return res, nil
	}
	out, err := lint(ctx, projectDir, pkgs)
	res.Findings = capLintFindings(out)
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			res.Passed = false
			return res, nil
		}
		return LintGateResult{Packages: pkgs}, fmt.Errorf("lint gate: runner failed to start: %w", err)
	}
	res.Passed = true
	return res, nil
}

func lintGateGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("lint gate: git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}

func lintGateChangedFiles(ctx context.Context, dir string) ([]string, error) {
	base, err := lintGateGit(ctx, dir, "merge-base", "HEAD", "main")
	if err != nil {
		return nil, err
	}
	diff, err := lintGateGit(ctx, dir, "diff", "--name-only", strings.TrimSpace(base))
	if err != nil {
		return nil, err
	}
	untracked, err := lintGateGit(ctx, dir, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	var files []string
	for _, l := range strings.Split(diff+"\n"+untracked, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			files = append(files, l)
		}
	}
	return files, nil
}

func lintGatePackages(files []string) []string {
	seen := map[string]bool{}
	var pkgs []string
	for _, f := range files {
		if !strings.HasSuffix(f, ".go") {
			continue
		}
		d := path.Dir(f)
		p := "./" + d
		if d == "." {
			p = "."
		}
		if !seen[p] {
			seen[p] = true
			pkgs = append(pkgs, p)
		}
	}
	sort.Strings(pkgs)
	return pkgs
}

func capLintFindings(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > lintFindingsCap {
		s = s[:lintFindingsCap]
	}
	return s
}

// VerifyLintRunner is the LintRunner the verification lint gate uses after
// a passing verification script. Tests replace it with a fake.
var VerifyLintRunner LintRunner = DefaultLintRunner

// applyLintGate runs the lint gate over inv.ProjectDir after a passing
// verification script. msg is "" when the gate passes, no Go package was
// touched, or the directory is not a git work tree; otherwise it is the
// failure message (starting "lint gate failed"), also appended to
// inv.ScriptOutput so the operator sees it. passed is true only when the
// gate ran and passed: a directory it could not check passes nothing.
func applyLintGate(ctx context.Context, inv *GuardrailInvocation) (msg string, passed bool) {
	if inv.ProjectDir == "" {
		return "", false
	}
	if out, err := lintGateGit(ctx, inv.ProjectDir, "rev-parse", "--is-inside-work-tree"); err != nil || strings.TrimSpace(out) != "true" {
		return "", false
	}
	res, err := LintGateCheck(ctx, inv.ProjectDir, VerifyLintRunner)
	switch {
	case err != nil:
		msg = "lint gate failed: " + err.Error()
	case !res.Passed:
		msg = "lint gate failed: findings in " + strings.Join(res.Packages, " ") + ":\n" + res.Findings
	default:
		return "", true
	}
	if inv.ScriptOutput != "" {
		inv.ScriptOutput += "\n"
	}
	inv.ScriptOutput += msg
	return msg, false
}

// lintGateBlockVerdict is the deterministic block verdict recorded when
// the lint gate fails a unit whose verification script passed.
func lintGateBlockVerdict(msg string) GuardrailVerdict {
	return GuardrailVerdict{
		Guardrail: lintGateName,
		Outcome:   "block",
		Severity:  "high",
		Summary:   msg,
		VerdictAt: time.Now(),
	}
}

// lintGateName is the guardrail name on the lint gate's own verdicts.
const lintGateName = "lint-gate"

// DecisionLintGateLifted is the decision kind written when a passing lint gate
// lifts a unit's earlier lint-gate block.
const DecisionLintGateLifted = "lint_gate_lifted"

// lintGateBlockOpen reports whether one unit's newest lint-gate judgment is a
// block.
func lintGateBlockOpen(vs []GuardrailVerdict) bool {
	var own []GuardrailVerdict
	for _, v := range vs {
		if v.Guardrail == lintGateName {
			own = append(own, v)
		}
	}
	return unitReviewBlocked(own)
}

// liftLintGateBlock records, on a unit whose lint-gate block is still open,
// that the gate has now passed it. A unit with no open lint-gate block gets
// nothing. The record is not a review verdict: it is logged under its own
// decision kind, sends no judge event, and ReviewStateOf never counts it as a
// pass (see reviewStateUnit).
func (m *Manager) liftLintGateBlock(prd *PRD, unitID string, vs *[]GuardrailVerdict) error {
	lifted := false
	m.store.WithLock(func() {
		if !lintGateBlockOpen(*vs) {
			return
		}
		lifted = true
		now := time.Now()
		*vs = append(*vs, GuardrailVerdict{Guardrail: lintGateName, Outcome: "pass", Severity: "info",
			Summary: "lint gate passed; the earlier lint-gate block no longer stands", VerdictAt: now})
		prd.Decisions = append(prd.Decisions, Decision{At: now, Kind: DecisionLintGateLifted, Actor: "autonomous",
			Note: fmt.Sprintf("unit %s passed the lint gate: its earlier lint-gate block no longer stands (this is not a review pass)", unitID)})
	})
	if !lifted {
		return nil
	}
	return m.savePRDOwn(prd)
}

// reviewStateUnit is one unit's verdicts as the review state reads them: a
// lint-gate pass is never a review pass, so it is left out, and so is a
// lint-gate block it has lifted.
func reviewStateUnit(vs []GuardrailVerdict) []GuardrailVerdict {
	open := lintGateBlockOpen(vs)
	hasPass := false
	for _, v := range vs {
		hasPass = hasPass || (v.Guardrail == lintGateName && v.Outcome == "pass")
	}
	if !hasPass {
		return vs
	}
	var out []GuardrailVerdict
	for _, v := range vs {
		if v.Guardrail == lintGateName && (v.Outcome == "pass" || !open) {
			continue
		}
		out = append(out, v)
	}
	return out
}
