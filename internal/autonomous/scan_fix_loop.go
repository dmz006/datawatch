// scan_fix_loop.go — the scan-fix loop.
//
// The scan framework (internal/autonomous/scan) reports findings; the
// existing scan/fix surface creates a fix child PRD that the operator
// must open and run by hand. The scan-fix loop closes that gap under
// the two config keys that were already settable on every surface:
//
//   - fix_loop_enabled — master switch for the loop
//   - fix_loop_max_retries — how many fix attempts the loop may spend
//     on a failing scan (0 means no attempts at all)
//
// When the loop is enabled and a scan still has findings at or above
// the fail-on severity, the loop:
//
//  1. opens a corrective task (a fix child PRD) for the findings,
//  2. executes it the way operator-run fix PRDs are executed, and
//  3. re-scans;
//
// repeating the steps until the re-scan passes or the retry budget is
// spent. If the budget is spent and the scan still fails, the Automaton
// is put on hold and the hold reason names the findings that remain, in
// plain English. A passing scan, or a disabled loop, means the loop does
// exactly what a scan does today: nothing at all.
//
// The loop engine (RunScanFixLoop) is a plain function over injectable
// seams (FixLoopDeps) so the whole retry/hold behavior is testable with
// fakes. The Manager wiring below supplies the real seams: the same
// scan run, the same fix-child creation as the scan/fix surface, the
// same executor pair as operator-started runs, and the standard hold
// (with a blocked→held fallback for statuses a hold cannot start from).

package autonomous

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/dmz006/datawatch/internal/autonomous/scan"
)

// scanFixLoopActor names the loop itself in the decision records it
// leaves (open, approve, hold) so an operator reading an Automaton's
// record can tell what the loop did from what a person did.
const scanFixLoopActor = "scan_fix_loop"

// Fix-loop report outcomes.
const (
	FixLoopOutcomeClean = "clean" // scan did not fail; the loop did nothing
	FixLoopOutcomeFixed = "fixed" // a fix attempt cleared the failing scan
	FixLoopOutcomeHeld  = "held"  // retries spent, findings remain, Automaton is on hold
	FixLoopOutcomeError = "error" // a step could not even be recorded
)

// FixLoopDeps are the seams the loop drives. All four must be non-nil.
type FixLoopDeps struct {
	// RunScan re-scans the parent Automaton's project and returns the
	// new result (fresh findings, pass/fail verdict).
	RunScan func(prdID string) (*scan.Result, error)
	// OpenFix opens one corrective task that fixes the given findings
	// and returns the task's ID. The task is approved and unblocked so
	// RunFix can actually execute it.
	OpenFix func(parentID string, findings []scan.Finding) (childID string, err error)
	// RunFix executes a corrective task to a terminal outcome.
	RunFix func(childID string) error
	// Hold puts the parent Automaton on hold with the given reason.
	// The reason must be plain English naming the findings that remain.
	Hold func(parentID string, reason string) error
}

// FixLoopAttempt is one fix+re-scan round.
type FixLoopAttempt struct {
	FixPRDID string `json:"fix_prd_id,omitempty"`
	Error    string `json:"error,omitempty"`
}

// FixLoopReport is the loop's complete account of one run. It is
// returned even (especially) when the loop ended in a hold or an
// error, so every surface can show what happened and why.
type FixLoopReport struct {
	PRDID      string           `json:"prd_id"`
	Outcome    string           `json:"outcome"` // clean | fixed | held | error
	MaxRetries int              `json:"max_retries"`
	Attempts   []FixLoopAttempt `json:"attempts,omitempty"`
	Held       bool             `json:"held,omitempty"`
	HoldReason string           `json:"hold_reason,omitempty"`
	LastScan   *scan.Result     `json:"last_scan,omitempty"`
	Error      string           `json:"error,omitempty"`
	StartedAt  time.Time        `json:"started_at"`
	FinishedAt time.Time        `json:"finished_at"`
}

// scanFailing reports whether a scan still has findings at or above the
// fail-on threshold (FailingFindings in the scan package) or the LLM
// grader marked it a failure.
func scanFailing(r *scan.Result) bool {
	if r == nil {
		return true
	}
	return !r.Pass || r.Verdict == "fail"
}

// RunScanFixLoop runs the scan-fix loop for one Automaton:
// scan, and while it still fails open a corrective task for the
// findings, execute it, re-scan — up to maxRetries attempts (0 means
// no attempts: a failing scan is held immediately). A re-scan that
// passes ends the loop as "fixed". With the budget spent and the scan
// still failing, the Automaton is held and the reason names the
// findings that remain.
//
// The report is always the first return value and always carries an
// outcome; err is non-nil only when a step could not even be recorded
// (a scan could not run, or the final hold could not be set).
func RunScanFixLoop(deps FixLoopDeps, prdID string, maxRetries int) (*FixLoopReport, error) {
	if maxRetries < 0 {
		maxRetries = 0
	}
	rep := &FixLoopReport{PRDID: prdID, MaxRetries: maxRetries, StartedAt: time.Now()}
	end := func(outcome string, err error) (*FixLoopReport, error) {
		rep.Outcome = outcome
		if err != nil {
			rep.Error = err.Error()
		}
		rep.FinishedAt = time.Now()
		return rep, err
	}

	current, err := deps.RunScan(prdID)
	if err != nil {
		return end(FixLoopOutcomeError, fmt.Errorf("run scan: %w", err))
	}
	if !scanFailing(current) {
		rep.Outcome = FixLoopOutcomeClean
		rep.LastScan = current
		return end(FixLoopOutcomeClean, nil)
	}

	for i := 0; i < maxRetries; i++ {
		attempt := FixLoopAttempt{}
		childID, oerr := deps.OpenFix(prdID, current.Findings)
		attempt.FixPRDID = childID
		if oerr != nil {
			attempt.Error = oerr.Error()
		} else if rerr := deps.RunFix(childID); rerr != nil {
			attempt.Error = rerr.Error()
		}
		rep.Attempts = append(rep.Attempts, attempt)

		again, err := deps.RunScan(prdID)
		if err != nil {
			return end(FixLoopOutcomeError, fmt.Errorf("re-scan after fix attempt %d: %w", i+1, err))
		}
		current = again
		if !scanFailing(current) {
			rep.Outcome = FixLoopOutcomeFixed
			rep.LastScan = current
			return end(FixLoopOutcomeFixed, nil)
		}
	}

	reason := FormatFixLoopHoldReason(maxRetries, current)
	if herr := deps.Hold(prdID, reason); herr != nil {
		rep.HoldReason = reason
		rep.LastScan = current
		return end(FixLoopOutcomeError, fmt.Errorf("set hold after the fix-loop retries were spent: %w", herr))
	}
	rep.Outcome = FixLoopOutcomeHeld
	rep.Held = true
	rep.HoldReason = reason
	rep.LastScan = current
	return end(FixLoopOutcomeHeld, nil)
}

// FormatFixLoopHoldReason builds the hold reason for an exhausted
// fix loop: plain English, naming each scan finding of the final
// failing scan, and always short enough for a hold reason
// (holdReasonMaxChars characters).
func FormatFixLoopHoldReason(maxRetries int, res *scan.Result) string {
	var b strings.Builder
	if maxRetries == 0 {
		b.WriteString("the scan-fix loop is enabled but has no retries (fix_loop_max_retries is 0) and its scan left ")
	} else {
		fmt.Fprintf(&b, "the scan-fix loop spent all %d of its retries and its scan left ", maxRetries)
	}
	if res == nil || len(res.Findings) == 0 {
		b.WriteString("failing findings.")
		return b.String()
	}
	countWord := "findings"
	if len(res.Findings) == 1 {
		countWord = "finding"
	}
	fmt.Fprintf(&b, "%d %s: ", len(res.Findings), countWord)
	words := make([]string, 0, len(res.Findings))
	for _, f := range res.Findings {
		loc := "the project"
		if f.File != "" {
			loc = f.File
			if f.Line > 0 {
				loc += ":" + strconv.Itoa(f.Line)
			}
		}
		w := fmt.Sprintf("%s (%s) in %s", f.RuleID, f.Severity, loc)
		if msg := strings.TrimSpace(f.Message); msg != "" {
			w += ": " + msg
		}
		words = append(words, w)
	}
	b.WriteString(strings.Join(words, "; "))
	b.WriteString(".")
	if utf8.RuneCountInString(b.String()) > holdReasonMaxChars {
		// Too long to name every finding: keep as many finding labels
		// as fit and say there were more.
		prefix := strings.TrimSuffix(b.String(), strings.Join(words, "; ")+".")
		terms := make([]string, 0, len(res.Findings))
		for _, f := range res.Findings {
			term := f.RuleID
			if f.File != "" {
				term += " " + f.File
			}
			terms = append(terms, term)
		}
		suffix := " and more (see the scan report for the full list)."
		kept := make([]string, 0, len(terms))
		for _, term := range terms {
			candidate := prefix + strings.Join(append(kept, term), "; ") + suffix
			if utf8.RuneCountInString(candidate) > holdReasonMaxChars {
				break
			}
			kept = append(kept, term)
		}
		if len(kept) > 0 {
			return prefix + strings.Join(kept, "; ") + suffix
		}
		return "the scan-fix loop spent all " + strconv.Itoa(maxRetries) +
			" of its retries and the scan still reports findings; see the scan report for the full list."
	}
	return b.String()
}

// ---------------------------------------------------------------------
// Manager wiring: the real seams behind FixLoopDeps.
// ---------------------------------------------------------------------

// FixLoopRunFn executes a corrective task (a fix child PRD) to a
// terminal outcome — the same executor pair operator-started runs use.
type FixLoopRunFn func(ctx context.Context, prdID string) error

// SetFixLoopRun wires the executor the loop's run step uses. The HTTP
// API supplies the real spawn/verify pair (SetExecutors) once session
// wiring is available; until then the run step falls back to
// Manager.Run's status-only path, in which the corrective task fails
// its single execution (no worker exists) and the loop records that as
// a spent attempt.
func (m *Manager) SetFixLoopRun(fn FixLoopRunFn) {
	m.mu.Lock()
	m.fixLoopRun = fn
	m.mu.Unlock()
}

// RunScanFixLoop runs the scan-fix loop for one PRD using the scan
// sub-config's retry budget (fix_loop_max_retries). See
// RunScanFixLoop for the full behavior contract.
func (m *Manager) RunScanFixLoop(prdID string) (*FixLoopReport, error) {
	return m.RunScanFixLoopContext(context.Background(), prdID)
}

// RunScanFixLoopContext is RunScanFixLoop with a context that ends its waits
// for an operator pause.
func (m *Manager) RunScanFixLoopContext(ctx context.Context, prdID string) (*FixLoopReport, error) {
	sc := m.ScanConfig()
	return RunScanFixLoop(m.fixLoopDepsIn(ctx), prdID, sc.FixLoopMaxRetries)
}

// fixLoopDeps builds the real seams over the Manager's own methods.
func (m *Manager) fixLoopDeps() FixLoopDeps {
	return m.fixLoopDepsIn(context.Background())
}

func (m *Manager) fixLoopDepsIn(ctx context.Context) FixLoopDeps {
	return FixLoopDeps{
		RunScan: func(prdID string) (*scan.Result, error) {
			return m.RunScan(prdID)
		},
		OpenFix: func(parentID string, findings []scan.Finding) (string, error) {
			return m.openScanFixTask(ctx, parentID, findings)
		},
		RunFix: func(childID string) error {
			return m.runScanFixTask(ctx, childID)
		},
		Hold: func(parentID string, reason string) error {
			// A loop stopped while it waited spent no real attempts.
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("the scan-fix loop was stopped while waiting: %w", err)
			}
			return m.holdForScanFixLoop(parentID, reason)
		},
	}
}

// openScanFixTask opens one corrective task for the findings: a fix
// child PRD with the same spec the scan/fix surface builds, linked to
// the scanned Automaton, decomposed and approved so the run step can
// execute it. The child ID is returned even when a later step fails, so
// the loop (and every surface) can point at the Automaton it created.
func (m *Manager) openScanFixTask(ctx context.Context, parentID string, findings []scan.Finding) (string, error) {
	if len(findings) == 0 {
		return "", fmt.Errorf("no scan findings to fix")
	}
	parent, ok := m.store.GetPRD(parentID)
	if !ok {
		return "", fmt.Errorf("prd %q not found", parentID)
	}
	// A paused project gets no new fix task and no planner until the resume.
	wait := func(dir string) error { return m.waitDirUnpaused(ctx, dir) }
	if err := wait(m.planDirOf(parentID)); err != nil {
		return "", fmt.Errorf("open fix task: %w", err)
	}
	spec := scan.BuildFixSpec(findings)
	child, err := m.store.CreatePRD(spec, parent.ProjectDir, parent.Backend, parent.Model, parent.Effort)
	if err != nil {
		return "", fmt.Errorf("create fix PRD: %w", err)
	}
	now := time.Now()
	child.ParentPRDID = parentID
	child.Decisions = append(child.Decisions, Decision{
		At:    now,
		Kind:  "scan_fix_loop_open",
		Actor: scanFixLoopActor,
		Note:  fmt.Sprintf("opened by the scan-fix loop to fix %d scan finding(s)", len(findings)),
	})
	if err := m.store.SavePRD(child); err != nil {
		return child.ID, fmt.Errorf("save fix PRD: %w", err)
	}
	// Same inheritance as the manager's CreateFixPRD: the fix task
	// starts from the setup of the Automaton it is fixing.
	m.inheritFromParent(child, parent, childExplicit{})

	if len(child.Story) == 0 {
		if derr := m.planEngineOpened(child.ID, wait); derr != nil {
			return child.ID, fmt.Errorf("decompose fix task: %w", derr)
		}
	}
	if aerr := m.approveFixTask(child.ID); aerr != nil {
		return child.ID, aerr
	}
	return child.ID, nil
}

// approveFixTask opens any approval gates a freshly decomposed fix
// task still has, so the loop can execute it. The operator's plan
// gates (coverage, plan review, hold) stay in force: if one refuses,
// the error is recorded on the attempt — the operator sees both the
// corrective task and the gate that stopped it. The loop itself is
// never the actor that overrides an operator gate.
func (m *Manager) approveFixTask(childID string) error {
	prd, ok := m.store.GetPRD(childID)
	if !ok {
		return fmt.Errorf("fix PRD %q vanished", childID)
	}
	switch prd.Status {
	case PRDNeedsReview, PRDRevisionsAsked:
		if _, err := m.Approve(childID, scanFixLoopActor, "opened by the scan-fix loop to fix scan findings"); err != nil {
			return fmt.Errorf("approve fix task: %w", err)
		}
	default:
		// draft (decompose produced no stories) or already approved:
		// the run step will report what is runnable.
	}
	prd, _ = m.store.GetPRD(childID)
	for si := range prd.Story {
		s := &prd.Story[si]
		if s.Status == StoryAwaitingApproval {
			if _, err := m.ApproveStory(childID, s.ID, scanFixLoopActor); err != nil {
				return fmt.Errorf("approve story %s: %w", s.ID, err)
			}
		}
		for ti := range s.Tasks {
			tk := &s.Tasks[ti]
			if tk.Status == TaskAwaitingApproval {
				if _, err := m.ApproveTask(childID, tk.ID, scanFixLoopActor); err != nil {
					return fmt.Errorf("approve task %s: %w", tk.ID, err)
				}
			}
		}
	}
	return nil
}

// runScanFixTask executes a corrective task to a terminal outcome,
// using the wired executor pair when one was provided (production)
// and the manager's status-only fallback otherwise.
func (m *Manager) runScanFixTask(waitCtx context.Context, childID string) error {
	m.mu.Lock()
	fn := m.fixLoopRun
	m.mu.Unlock()
	wait := func(dir string) error { return m.waitDirUnpaused(waitCtx, dir) }
	return m.runEngineOpened(childID, wait, func() error {
		ctx := context.Background()
		if fn != nil {
			return fn(ctx, childID)
		}
		return m.Run(ctx, childID, nil, nil)
	})
}

// holdForScanFixLoop puts the scanned Automaton on hold with the
// loop's plain-English reason, via the standard hold (operator reason,
// actor and From-status bookkeeping all stay correct). When the
// current status is not one a hold may start from — e.g. the Automaton
// already finished its work, in which the loop re-scanned it — it is
// first parked as blocked with the same reason (blocked is a
// holdable status) and then held, so the findings never vanish into a
// completed-looking state.
func (m *Manager) holdForScanFixLoop(prdID, reason string) error {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return fmt.Errorf("prd %q not found", prdID)
	}
	req := HoldRequest{Reason: reason}
	if _, err := m.Hold(prdID, req, scanFixLoopActor); err == nil {
		return nil
	}
	if NormalizePRDStatus(prd.Status) == PRDHeld {
		// Already on a hold: the findings are already visible to the
		// operator through that hold; do not steal its reason.
		return nil
	}
	if NormalizePRDStatus(prd.Status) == PRDCompleted {
		// A completed status cannot be held at all (nor parked as
		// blocked first without a rewrite the store would reject), so
		// say so plainly instead of pretending the hold happened.
		return fmt.Errorf("cannot hold a completed Automaton (%q); release or re-open it first", prdID)
	}
	// Park it as blocked with the same reason, then hold it from the
	// blocked status (a legal hold start).
	now := time.Now()
	var old PRDStatus
	m.store.WithLock(func() {
		old = prd.Status
		prd.Status = PRDBlocked
		prd.BlockedReason = reason
		prd.UpdatedAt = now
		prd.Decisions = append(prd.Decisions, Decision{
			At:    now,
			Kind:  "scan_fix_loop_hold",
			Actor: scanFixLoopActor,
			Note:  reason,
		})
	})
	if err := m.store.SavePRD(prd); err != nil {
		return err
	}
	m.emitPRDStatus(prd, old, lastDecision(prd, "scan_fix_loop_hold"), nil)
	if _, err := m.Hold(prdID, req, scanFixLoopActor); err != nil {
		return fmt.Errorf("set hold after parking the Automaton as blocked: %w", err)
	}
	return nil
}
