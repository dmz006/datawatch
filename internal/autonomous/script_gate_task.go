// script_gate_task.go — runs a final gate task's checks in the daemon.
//
// A final_gate task whose gate resolves to at least one runnable check is
// performed by the daemon itself: each check runs through RunScriptGate under
// one shared deadline, the captured output and verdict are written to the
// task's report file, and the task completes on a clean pass or fails with
// the reason. No session is spawned. A gate the daemon cannot run (nothing
// declared, or an unusable check) is failed with that reason rather than left
// waiting for a worker.
package autonomous

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// scriptGateDecisionKind is the Decision.Kind of the note recorded when the
// daemon ran a gate.
const scriptGateDecisionKind = "script_gate"

// defaultGateReportPath is the report written when the task names none.
const defaultGateReportPath = ".datawatch/reports/final-gate.txt"

// runScriptGateTask runs a final gate task in the daemon. handled is false
// unless t.Kind is final_gate and ResolveGateCheck returns at least one check,
// or a reason. When handled, the task is completed (every check exited 0) or
// failed, and a failure also returns the error.
func (m *Manager) runScriptGateTask(ctx context.Context, prd *PRD, t *Task) (handled bool, err error) {
	if m == nil || prd == nil || t == nil || t.Kind != TaskKindFinalGate {
		return false, nil
	}
	checks, reason := ResolveGateCheckForAutomaton(t, prd.Story, m.projectGateCommand(prd))
	if len(checks) == 0 && reason == "" {
		return false, nil
	}
	if err := ctx.Err(); err != nil {
		return true, err
	}
	if len(checks) == 0 {
		m.finishScriptGateTask(prd, t, "", "the daemon could not run the gate: "+reason, reason, false)
		return true, fmt.Errorf("final gate %q: %s", t.ID, reason)
	}

	cfg := m.Config()
	auto := config.AutonomousConfig{ScriptGateTimeoutSeconds: cfg.ScriptGateTimeoutSeconds}
	timeout := auto.EffectiveScriptGateTimeout()
	dir := ResolveTaskProjectDir(prd, t)

	runCtx, cancel := ctx, context.CancelFunc(func() {})
	if timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()

	var report strings.Builder
	failReason := ""
	timedOut := false
	lastExit := 0
	for i, c := range checks {
		res := RunScriptGate(runCtx, c, dir, remainingGateTime(runCtx, timeout))
		fmt.Fprintf(&report, "== check %d of %d: %s ==\n", i+1, len(checks), describeGateCheck(c))
		report.WriteString(res.Output)
		if res.Output != "" && !strings.HasSuffix(res.Output, "\n") {
			report.WriteString("\n")
		}
		if res.Passed {
			fmt.Fprintf(&report, "verdict: passed (exit 0)\n\n")
			continue
		}
		lastExit, timedOut = res.ExitCode, res.TimedOut
		failReason = res.Reason
		if timedOut {
			failReason = fmt.Sprintf("timed out: the gate exceeded its %s-second limit", formatGateSeconds(timeout))
		}
		fmt.Fprintf(&report, "verdict: failed: %s\n\n", failReason)
		break
	}

	var note string
	switch {
	case failReason == "":
		note = fmt.Sprintf("the daemon ran the gate (%d check(s)): every check exited 0", len(checks))
		report.WriteString("final verdict: passed\n")
	case timedOut:
		note = "the daemon ran the gate: " + failReason
		report.WriteString("final verdict: failed (timeout)\n")
	default:
		note = fmt.Sprintf("the daemon ran the gate: exit status %d: %s", lastExit, failReason)
		report.WriteString("final verdict: failed\n")
	}
	m.finishScriptGateTask(prd, t, report.String(), note, failReason, failReason == "")
	if failReason != "" {
		return true, fmt.Errorf("final gate %q: %s", t.ID, failReason)
	}
	return true, nil
}

// remainingGateTime is the time left on the shared deadline, so each check
// gets what the earlier ones left over. 0 means no cap.
func remainingGateTime(ctx context.Context, total time.Duration) time.Duration {
	if total <= 0 {
		return 0
	}
	dl, ok := ctx.Deadline()
	if !ok {
		return total
	}
	left := time.Until(dl)
	if left < time.Millisecond {
		left = time.Millisecond
	}
	return left
}

func describeGateCheck(c GateCheck) string {
	if c.Kind == GateCheckScript {
		return "script " + c.Script
	}
	return "project gate command"
}

// projectGateCommand is the gate command in effect for prd: the resolved
// project/Automaton value when the policy resolver is wired, else the
// manager's configured value.
func (m *Manager) projectGateCommand(prd *PRD) string {
	if pol, ok := m.effectivePolicyOf(prd); ok {
		return strings.TrimSpace(pol.ProjectGateCommand.Value)
	}
	return strings.TrimSpace(m.Config().ProjectGateCommand)
}

// finishScriptGateTask writes the report (when there is one), records the
// decision, sets the task's final status, saves it and emits its status.
func (m *Manager) finishScriptGateTask(prd *PRD, t *Task, report, note, failReason string, passed bool) {
	reportRel := t.ReportPath
	if validateReportPath(reportRel) != nil || reportRel == "" {
		reportRel = defaultGateReportPath
	}
	if report != "" {
		if werr := writeGateReport(ResolveTaskProjectDir(prd, t), reportRel, report); werr != nil {
			log.Printf("[autonomous] %s/%s write gate report: %v", prd.ID, t.ID, werr)
			note += " (report not written: " + werr.Error() + ")"
		} else {
			note += "; report " + reportRel
		}
	}
	old := t.Status
	now := m.genNow()
	m.store.WithLock(func() {
		if passed {
			t.Status = TaskCompleted
			t.CompletedAt = &now
			t.Error = ""
		} else {
			t.Status = TaskFailed
			t.Error = failReason
		}
		prd.Decisions = append(prd.Decisions, Decision{At: now, Kind: scriptGateDecisionKind, Actor: "autonomous", Note: note})
	})
	if err := m.store.SaveTask(t); err != nil {
		log.Printf("[autonomous] %s/%s save gate task: %v", prd.ID, t.ID, err)
		return
	}
	if err := m.store.SavePRD(prd); err != nil {
		log.Printf("[autonomous] %s save gate decision: %v", prd.ID, err)
	}
	m.emitTaskStatus(prd, t, old, nil, nil)
	if !passed {
		m.emitFinalGateFailed(prd, t, failReason)
	}
}

// writeGateReport writes report to rel under projectDir, creating folders.
func writeGateReport(projectDir, rel, report string) error {
	if strings.TrimSpace(projectDir) == "" {
		return fmt.Errorf("the Automaton has no project directory")
	}
	full := filepath.Join(projectDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil { // #nosec G301 -- project report folder
		return err
	}
	return os.WriteFile(full, []byte(report), 0o644) // #nosec G306 -- report is meant to be readable
}
