package mergeshadow

import (
	"context"
	"strings"
	"time"
)

// Gate reruns: a test gate that fails once and then passes on a second run
// is a real intermittent failure (a race, an unbounded wait, a shared
// resource or a timing assumption). RunGateWithRerun reruns a failing gate
// once, so the intermittent failure is recorded as a finding instead of
// blocking the merge. The failure is never written off as machine load: it is
// a defect in the test or the code, and the finding keeps its first lines.

// rerunGateName is the only gate that RunGates reruns after a failure.
const rerunGateName = "test"

// findingLines is how many leading lines of a failing run's output become the
// finding of an intermittent gate.
const findingLines = 5

// intermittentDetail is the exact Detail of an intermittent gate.
const intermittentDetail = "intermittent: passed on rerun"

// RunGateWithRerun runs the gate name in dir. If the first run fails, it
// reruns the gate once. If the rerun passes, the gate is reported as
// GateIntermittent: the first lines of the first run's output are kept as the
// Finding, and the status is not a failure, so the merge is not blocked. If
// the rerun does not pass, the first run's failure is returned with the
// rerun's status noted in Detail. A gate that does not fail on the first run
// is not rerun.
func RunGateWithRerun(ctx context.Context, dir, name string, timeout time.Duration) GateResult {
	first := runGate(ctx, dir, name, timeout)
	if first.Status != GateFail {
		return first
	}
	rerun := runGate(ctx, dir, name, timeout)
	if rerun.Status != GatePass {
		first.Detail = joinDetail(first.Detail, "rerun also "+string(rerun.Status))
		return first
	}
	first.Status = GateIntermittent
	first.Finding = leadingLines(first.Output, findingLines)
	first.Detail = intermittentDetail
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
