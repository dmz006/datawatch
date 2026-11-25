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
	"path"
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
	runChecks := func() (interrupted, signalled bool) {
		for i, c := range checks {
			gateCtx, done := m.trackGate(runCtx, prd.ID, t.ID, describeGateCheck(c))
			res := runGateCheckFn(gateCtx, c, dir, remainingGateTime(runCtx, timeout))
			done()
			if res.Interrupted {
				// The daemon is stopping, not the gate failing: the gate keeps
				// running and the next attempt re-attaches to its record.
				return true, false
			}
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
			return false, !timedOut && res.ExitCode < 0 && res.Reason == gateSignalKilledReason
		}
		return false, false
	}
	interrupted, signalled := runChecks()
	if interrupted {
		return true, ctx.Err()
	}
	if signalled && m.gateRestartRetryDue(prd, t) {
		m.recordGateRestartRetry(prd, t)
		report.WriteString("== the Automaton retries the gate once: the daemon restarted shortly before the kill ==\n\n")
		failReason, timedOut, lastExit = "", false, 0
		if interrupted, _ = runChecks(); interrupted {
			return true, ctx.Err()
		}
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
		dir := ResolveTaskProjectDir(prd, t)
		keptAt, werr := keepForeignReport(dir, reportRel)
		if keptAt != "" {
			m.auditKeptFiles("autonomous_gate_report_kept_previous", map[string]any{
				"automaton_id": prd.ID, "task_id": t.ID, "tree": dir,
				"files": []movedFile{{From: reportRel, To: keptAt}},
			})
			note += "; the file already at " + reportRel + " is kept as " + keptAt
		}
		if werr == nil {
			werr = writeGateReport(dir, reportRel, report)
		}
		if werr != nil {
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
		// The gate repair engine (report mode is the default: it records the
		// finding and raises one alert, changing nothing; on mode applies the
		// class's safe repair and resets the failed run) works the failure
		// out of band — its result never changes this gate's verdict.
		if g := m.GateRepairer(); g != nil {
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
				defer cancel()
				out := g.Run(ctx, prd.ID, t.ID)
				if out.Err != "" {
					log.Printf("[autonomous] %s/%s gate-repair: %s", prd.ID, t.ID, out.Err)
				}
			}()
		}
	}
}

// isDaemonGateReport reports whether text is a finished report of the
// daemon's own script gate.
func isDaemonGateReport(text string) bool {
	if !strings.HasPrefix(text, "== check 1 of ") {
		return false
	}
	for _, end := range []string{"passed", "failed", "failed (timeout)"} {
		if strings.HasSuffix(text, "\nfinal verdict: "+end+"\n") {
			return true
		}
	}
	return false
}

// keepForeignReport makes room for a gate report at rel under projectDir. An
// earlier report of the gate itself is left to be replaced. Anything else
// there is moved to a distinct name, returned relative to projectDir. An
// error means rel must not be written.
func keepForeignReport(projectDir, rel string) (string, error) {
	if strings.TrimSpace(projectDir) == "" {
		return "", nil // writeGateReport reports this
	}
	full := filepath.Join(projectDir, filepath.FromSlash(rel))
	st, err := os.Lstat(full)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if !st.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a regular file", rel)
	}
	old, err := os.ReadFile(full) // #nosec G304 -- a validated report path in the task's tree
	if err != nil {
		return "", err
	}
	if len(old) == 0 || isDaemonGateReport(string(old)) {
		return "", nil
	}
	to, _, err := moveKeepingBoth(full, full, func(p string) bool { return p == full })
	if err != nil {
		return "", fmt.Errorf("keep the file already at %s: %w", rel, err)
	}
	return path.Join(path.Dir(rel), filepath.Base(to)), nil
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

// gateRestartRecentWindow is how soon after the daemon started a signal-killed
// gate is taken to be a casualty of the restart rather than a real failure.
const gateRestartRecentWindow = 5 * time.Minute

// gateRestartRetryDecisionKind is the Decision.Kind of the note written when
// the Automaton retries a gate that the daemon restart killed.
const gateRestartRetryDecisionKind = infraRetryDecisionKind

// runGateCheckFn runs one gate check; tests replace it with a fake result.
var runGateCheckFn = RunScriptGate

// gateRestartRetryDue reports whether a signal kill seen now is retried: the
// daemon started within gateRestartRecentWindow before now and the task has
// not used its one restart retry.
func (m *Manager) gateRestartRetryDue(prd *PRD, t *Task) bool {
	started := m.startedAt
	if started.IsZero() {
		return false
	}
	var used bool
	m.store.WithLock(func() { used = t.GateRestartRetryUsed })
	if used {
		return false
	}
	age := m.genNow().Sub(started)
	return age >= 0 && age <= gateRestartRecentWindow
}

// recordGateRestartRetry marks the restart retry as used on the task and
// writes the decision-log line naming the daemon restart as the cause. It
// follows the infra retry route: no retry budget or attempt counter changes.
func (m *Manager) recordGateRestartRetry(prd *PRD, t *Task) {
	now := m.genNow()
	m.store.WithLock(func() { t.GateRestartRetryUsed = true })
	if err := m.store.SaveTask(t); err != nil {
		log.Printf("[autonomous] %s/%s save gate restart retry: %v", prd.ID, t.ID, err)
	}
	note := fmt.Sprintf("task %q final gate was killed by a signal %s after the daemon restarted: the Automaton retries the gate once; this retry does not count against the task's retry budget",
		t.Title, now.Sub(m.startedAt).Round(time.Second))
	m.store.WithLock(func() {
		prd.Decisions = append(prd.Decisions, Decision{
			At: now, Kind: gateRestartRetryDecisionKind, Actor: "autonomous",
			Note: note, FailureCategory: infraRetryFailureCategory,
		})
	})
	if err := m.store.SavePRD(prd); err != nil {
		log.Printf("[autonomous] %s save gate restart retry decision: %v", prd.ID, err)
	}
}

// storyOfTask returns the story that holds t, or nil.
func storyOfTask(prd *PRD, t *Task) *Story {
	for i := range prd.Story {
		for j := range prd.Story[i].Tasks {
			if prd.Story[i].Tasks[j].ID == t.ID {
				return &prd.Story[i]
			}
		}
	}
	return nil
}
