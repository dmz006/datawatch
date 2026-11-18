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
// verification script. It returns "" when the gate passes, no Go package
// was touched, or the directory is not a git work tree; otherwise it
// returns the failure message (starting "lint gate failed") and appends it
// to inv.ScriptOutput so the operator sees it.
func applyLintGate(ctx context.Context, inv *GuardrailInvocation) string {
	if inv.ProjectDir == "" {
		return ""
	}
	if out, err := lintGateGit(ctx, inv.ProjectDir, "rev-parse", "--is-inside-work-tree"); err != nil || strings.TrimSpace(out) != "true" {
		return ""
	}
	res, err := LintGateCheck(ctx, inv.ProjectDir, VerifyLintRunner)
	var msg string
	switch {
	case err != nil:
		msg = "lint gate failed: " + err.Error()
	case !res.Passed:
		msg = "lint gate failed: findings in " + strings.Join(res.Packages, " ") + ":\n" + res.Findings
	default:
		return ""
	}
	if inv.ScriptOutput != "" {
		inv.ScriptOutput += "\n"
	}
	inv.ScriptOutput += msg
	return msg
}

// lintGateBlockVerdict is the deterministic block verdict recorded when
// the lint gate fails a unit whose verification script passed.
func lintGateBlockVerdict(msg string) GuardrailVerdict {
	return GuardrailVerdict{
		Guardrail: "lint-gate",
		Outcome:   "block",
		Severity:  "high",
		Summary:   msg,
		VerdictAt: time.Now(),
	}
}
