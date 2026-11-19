package mergeshadow

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// GateStatus is the outcome of one gate run against a trial worktree.
type GateStatus string

const (
	// GatePass means the make target exited 0.
	GatePass GateStatus = "pass"
	// GateFail means the make target exited non-zero or could not start.
	GateFail GateStatus = "fail"
	// GateTimeout means the gate did not finish within its timeout and was killed.
	GateTimeout GateStatus = "timeout"
	// GateSkipped means the project's Makefile has no such target.
	GateSkipped GateStatus = "skipped"
	// GateIntermittent means the gate failed, then passed on rerun. That is a
	// real intermittent failure (a race, an unbounded wait, a shared resource
	// or a timing assumption). It is recorded as a finding, not as a gate
	// failure, and does not block the merge. See RunGateWithRerun.
	GateIntermittent GateStatus = "intermittent"
)

// GateResult records one build, lint or test gate.
type GateResult struct {
	// Name is the gate and the make target it runs: "build", "lint" or "test".
	Name string
	// Status is pass, fail, timeout or skipped.
	Status GateStatus
	// Duration is the wall-clock time the gate took.
	Duration time.Duration
	// Output is the last lines of the gate's combined stdout and stderr.
	Output string
	// Detail is extra context: why a gate was skipped, or, for the lint gate,
	// the local golangci-lint version and the version CI pins.
	Detail string
	// Finding is the first lines of the failing output of the first run. It
	// is set only when Status is GateIntermittent.
	Finding string
	// Findings are gate-level findings, such as lint_version_mismatch from
	// the lint gate. They are empty when the gate has none.
	Findings []Finding
}

// gateNames are the gates run by RunGates, in order. Each is a make target of
// the same name in the project's Makefile.
var gateNames = []string{"build", "lint", "test"}

// gateOutputLines is how many trailing output lines a GateResult keeps.
const gateOutputLines = 40

// gateWaitDelay bounds how long a gate's output pipes are awaited after the
// make process is killed on timeout, so a child that keeps the pipes open
// cannot hang the gate.
const gateWaitDelay = 5 * time.Second

// RunGates runs the build, lint and test gates in dir, the trial worktree.
// Each gate runs `make <name>` with its own timeout, so one hung gate cannot
// consume the time of the others. The gates always run in order and every
// gate is reported, even when an earlier one failed. A failing test gate is
// rerun once (see RunGateWithRerun).
//
// A gate whose target the Makefile does not define is reported as skipped.
// The make command is resolved by name from PATH.
func RunGates(ctx context.Context, dir string, timeout time.Duration) []GateResult {
	results := make([]GateResult, 0, len(gateNames))
	for _, name := range gateNames {
		if name == rerunGateName {
			results = append(results, RunGateWithRerun(ctx, dir, name, timeout))
			continue
		}
		results = append(results, RunGate(ctx, dir, name, timeout))
	}
	return results
}

// RunGate runs one gate, the make target name, in dir with the given timeout
// and returns its GateResult. A target missing from the Makefile is skipped.
func RunGate(ctx context.Context, dir, name string, timeout time.Duration) GateResult {
	return runGate(ctx, dir, name, timeout)
}

// runGate runs one gate. RunGate and RunGateWithRerun both call it.
func runGate(ctx context.Context, dir, name string, timeout time.Duration) GateResult {
	res := GateResult{Name: name}
	if !makefileHasGateTarget(dir, name) {
		res.Status = GateSkipped
		res.Detail = "the project's Makefile has no " + name + " target"
		return res
	}

	makeBin, err := exec.LookPath("make")
	if err != nil {
		res.Status = GateFail
		res.Detail = "make not found on PATH: " + err.Error()
		return res
	}

	gateCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var out bytes.Buffer
	cmd := exec.CommandContext(gateCtx, makeBin, name)
	cmd.Dir = dir
	cmd.Stdout = &out
	cmd.Stderr = &out
	cmd.WaitDelay = gateWaitDelay
	// The gate runs in its own process group, so a timeout kills every
	// process it started, not only make (see gate_kill.go).
	configureGateProcessGroup(cmd)

	start := time.Now()
	runErr := cmd.Run()
	res.Duration = time.Since(start)
	res.Output = tailLines(out.String(), gateOutputLines)

	switch {
	case errors.Is(gateCtx.Err(), context.DeadlineExceeded):
		res.Status = GateTimeout
		res.Detail = fmt.Sprintf("killed after %s", timeout)
	case runErr != nil:
		res.Status = GateFail
		res.Detail = runErr.Error()
	default:
		res.Status = GatePass
	}

	if name == "lint" {
		localLine, localVersion := golangciLintVersion(ctx)
		res = applyLintVersionPin(res, dir, localLine, localVersion)
	}
	return res
}

// makefileHasGateTarget reports whether dir's Makefile defines target. It
// follows the same rule as the merge pipeline's requires_make_target check
// (mergepipeline makefileHasTarget): the first of GNUmakefile, makefile and
// Makefile that exists is read, and a line starting with "<target>:" counts.
func makefileHasGateTarget(dir, target string) bool {
	for _, name := range []string{"GNUmakefile", "makefile", "Makefile"} {
		data, err := os.ReadFile(filepath.Join(dir, name)) // #nosec G304 -- fixed names under dir
		if err != nil {
			continue
		}
		for _, l := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(l, target+":") {
				return true
			}
		}
		return false
	}
	return false
}

// golangciLintVersion returns a one-line description of the golangci-lint
// binary on PATH, or why it could not be determined, and the normalized
// version it reports ("" when none could be read).
func golangciLintVersion(ctx context.Context) (line, version string) {
	bin, err := exec.LookPath("golangci-lint")
	if err != nil {
		return "golangci-lint not found on PATH", ""
	}
	vctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(vctx, bin, "version").CombinedOutput()
	if err != nil {
		return "golangci-lint version unavailable: " + err.Error(), ""
	}
	first := strings.TrimSpace(firstLine(string(out)))
	return "golangci-lint: " + first, NormalizeLintVersion(first)
}

// tailLines returns the last n lines of s with trailing blank lines removed.
func tailLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func joinDetail(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	}
	return a + "; " + b
}
