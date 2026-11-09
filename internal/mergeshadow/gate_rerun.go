package mergeshadow

import (
	"context"
	"strings"
	"time"
)

// Gate reruns: a test gate that fails while the machine is busy can fail on
// a timing race that does not occur when the gate runs alone. RunGateWithRerun
// reruns such a gate once, alone, so a load-sensitive failure is recorded as
// a finding instead of blocking the merge.

// rerunGateName is the only gate that RunGates reruns after a failure.
const rerunGateName = "test"

// findingLines is how many leading lines of a failing run's output become the
// finding of a load_sensitive gate.
const findingLines = 5

// RerunOptions controls RunGateWithRerun.
type RerunOptions struct {
	// FirstRunEnv is extra environment for the first run only. It lets a
	// caller mark the first run as the concurrent one. The rerun inherits
	// the plain environment.
	FirstRunEnv []string
	// BeforeRerun, when set, is called once after the first run and before
	// the rerun. A caller uses it to stop background load so the rerun is
	// alone.
	BeforeRerun func()
}

// RunGateWithRerun runs the gate name in dir. If the first run fails, it
// calls BeforeRerun and reruns the gate once. If the rerun passes, the gate
// is reported as GateLoadSensitive: the first lines of the first run's output
// are kept as the Finding, and the status is not a failure. If the rerun does
// not pass, the first run's failure is returned with the rerun's status noted
// in Detail. A gate that does not fail on the first run is not rerun.
func RunGateWithRerun(ctx context.Context, dir, name string, timeout time.Duration, opts RerunOptions) GateResult {
	first := runGate(ctx, dir, name, timeout, opts.FirstRunEnv)
	if first.Status != GateFail {
		return first
	}
	if opts.BeforeRerun != nil {
		opts.BeforeRerun()
	}
	rerun := runGate(ctx, dir, name, timeout, nil)
	if rerun.Status != GatePass {
		first.Detail = joinDetail(first.Detail, "rerun alone also "+string(rerun.Status))
		return first
	}
	first.Status = GateLoadSensitive
	first.Finding = leadingLines(first.Output, findingLines)
	first.Detail = joinDetail(first.Detail, "failed in the first run, passed when rerun alone")
	return first
}

// leadingLines returns the first n lines of s with trailing blank lines
// removed.
func leadingLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}
