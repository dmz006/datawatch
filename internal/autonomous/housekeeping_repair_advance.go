// Advance and judge repairs for the housekeeping framework (group
// advance_judge).
//
// Three repairs are registered on the framework, building on the stuck-running
// watchdog (which re-fires the completion rollup once and then blocks) rather
// than repeating it:
//
//   - advance: an Automaton in running with every task terminal is advanced to
//     its next state once the grace period has passed;
//   - judge repair: an Automaton blocked by the rules review, with no repair
//     task yet, gets one repair task made once from the real findings (add a
//     task carrying the findings, then reset it) and the fact is recorded so it
//     is never made again;
//   - merge report: an Automaton waiting to merge or ready to merge is only
//     reported. Nothing here can merge.
//
// Each repair refuses, as an unsafe finding (recorded as skipped with one
// alert), when a task is not terminal, when the repair was already made once,
// or when the findings are empty. Everything a repair can do goes through
// AdvanceOps: advance one Automaton, make the one repair task. There is no way
// to merge, ship, end a session, change a model or backend, or touch a
// security workflow.
package autonomous

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
)

// Merge states an AdvanceAutomaton may report.
const (
	MergeStateNone  = ""
	MergeStateWait  = "wait"
	MergeStateReady = "ready"
)

// judgeRepairDecisionKind is the decision recorded on an Automaton when its
// one judge repair task is made, so the repair is never repeated.
const judgeRepairDecisionKind = "housekeeping_judge_repair"

// AdvanceAutomaton is the view of one Automaton the advance and judge
// repairs need.
type AdvanceAutomaton struct {
	ID    string
	Title string
	// Running is true when the Automaton's status is running.
	Running bool
	// Blocked is true when the Automaton is blocked by the rules review.
	JudgeBlocked bool
	// TaskStatuses holds the status of every task.
	TaskStatuses []TaskStatus
	// Findings are the real findings of the blocked rules review.
	Findings []string
	// RepairMade is true when a judge repair task was already made once.
	RepairMade bool
	// HasRepairTask is true when a repair task for the review already exists.
	HasRepairTask bool
	// MergeState is MergeStateWait, MergeStateReady or empty.
	MergeState string
	// LiveExecutor is true when an executor still holds the run.
	LiveExecutor bool
}

func (a AdvanceAutomaton) allTerminal() bool {
	for _, s := range a.TaskStatuses {
		if !isTaskTerminal(s) {
			return false
		}
	}
	return true
}

// AdvanceOps is everything the advance and judge repairs may do or read.
type AdvanceOps interface {
	// Automata lists the Automata that are not finished.
	Automata(ctx context.Context) []AdvanceAutomaton
	// Advance moves one running Automaton with every task terminal to its
	// next state (the completion rollup). It never merges.
	Advance(ctx context.Context, automatonID string) error
	// RepairJudge makes the one repair task from the findings (add a task
	// carrying them, then reset it) and records that it was made.
	RepairJudge(ctx context.Context, automatonID string, findings []string) error
}

// NewAdvanceJudgeRepairs returns the advance and judge repairs over ops. Nil
// ops gives none.
func NewAdvanceJudgeRepairs(ops AdvanceOps) []Repair {
	if ops == nil {
		return nil
	}
	return []Repair{
		&advanceRepair{ops: ops},
		&judgeRepair{ops: ops, done: map[string]bool{}},
		&mergeReport{ops: ops},
	}
}

func advanceSubject(a AdvanceAutomaton) string { return "Automaton " + a.ID }

func findAutomaton(ctx context.Context, ops AdvanceOps, subject string) (AdvanceAutomaton, bool) {
	id := strings.TrimPrefix(subject, "Automaton ")
	for _, a := range ops.Automata(ctx) {
		if a.ID == id {
			return a, true
		}
	}
	return AdvanceAutomaton{}, false
}

func nonTerminalCount(a AdvanceAutomaton) int {
	n := 0
	for _, s := range a.TaskStatuses {
		if !isTaskTerminal(s) {
			n++
		}
	}
	return n
}

// ---- (a) advance a finished-but-running Automaton ----

type advanceRepair struct{ ops AdvanceOps }

func (*advanceRepair) Group() string { return RepairGroupAdvanceJudge }

func (r *advanceRepair) Detect(ctx context.Context) []RepairFinding {
	var out []RepairFinding
	for _, a := range r.ops.Automata(ctx) {
		if !a.Running || a.MergeState != MergeStateNone {
			continue
		}
		if len(a.TaskStatuses) == 0 {
			continue
		}
		f := RepairFinding{Subject: advanceSubject(a),
			Summary: fmt.Sprintf("Automaton %q is running but every task is finished", a.Title)}
		if n := nonTerminalCount(a); n > 0 {
			f.Unsafe, f.UnsafeReason = true, fmt.Sprintf("%d task(s) are not finished, so advancing could lose work", n)
		} else if a.LiveExecutor {
			continue // a live run still holds it; not stuck
		}
		out = append(out, f)
	}
	return out
}

func (r *advanceRepair) Apply(ctx context.Context, f RepairFinding) (string, error) {
	a, ok := findAutomaton(ctx, r.ops, f.Subject)
	if !ok || !a.Running {
		return "", errors.New("the Automaton is gone or no longer running; nothing was advanced")
	}
	if !a.allTerminal() {
		return "", errors.New("a task is not finished; nothing was advanced")
	}
	if err := r.ops.Advance(ctx, a.ID); err != nil {
		return "", err
	}
	return "advanced the Automaton to its next state", nil
}

func (*advanceRepair) UndoHint(RepairFinding) string {
	return "the Automaton moved on by the normal completion step; pause or cancel it from the Automata page to stop it"
}

// ---- (b) judge-blocked Automaton: one repair task from the real findings ----

type judgeRepair struct {
	ops  AdvanceOps
	mu   sync.Mutex
	done map[string]bool
}

func (*judgeRepair) Group() string { return RepairGroupAdvanceJudge }

func (r *judgeRepair) madeOnce(subject string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.done[subject]
}

func (r *judgeRepair) Detect(ctx context.Context) []RepairFinding {
	var out []RepairFinding
	for _, a := range r.ops.Automata(ctx) {
		if !a.JudgeBlocked || a.MergeState != MergeStateNone {
			continue
		}
		subject := advanceSubject(a)
		if a.HasRepairTask && !a.RepairMade && !r.madeOnce(subject) {
			continue // someone already made a repair task; it is theirs to finish
		}
		f := RepairFinding{Subject: subject,
			Summary: fmt.Sprintf("Automaton %q is blocked by the rules review and has no repair task", a.Title)}
		switch {
		case a.RepairMade || r.madeOnce(subject):
			f.Unsafe, f.UnsafeReason = true, "the repair task was already made once for this review, so a second one would be needed"
		case nonTerminalCount(a) > 0:
			f.Unsafe, f.UnsafeReason = true, fmt.Sprintf("%d task(s) are not finished, so a repair could lose work", nonTerminalCount(a))
		case !hasRealFindings(a.Findings):
			f.Unsafe, f.UnsafeReason = true, "the review recorded no findings, so there is nothing real to repair from"
		}
		out = append(out, f)
	}
	return out
}

func hasRealFindings(fs []string) bool {
	for _, f := range fs {
		if strings.TrimSpace(f) != "" {
			return true
		}
	}
	return false
}

func (r *judgeRepair) Apply(ctx context.Context, f RepairFinding) (string, error) {
	a, ok := findAutomaton(ctx, r.ops, f.Subject)
	if !ok || !a.JudgeBlocked {
		return "", errors.New("the Automaton is gone or no longer blocked by the review; nothing was repaired")
	}
	if a.RepairMade || r.madeOnce(f.Subject) {
		return "", errors.New("the repair was already made once; nothing was repaired")
	}
	if nonTerminalCount(a) > 0 || !hasRealFindings(a.Findings) {
		return "", errors.New("a task is not finished or the findings are empty; nothing was repaired")
	}
	r.mu.Lock()
	r.done[f.Subject] = true // marked before acting, so a failure is not retried either
	r.mu.Unlock()
	if err := r.ops.RepairJudge(ctx, a.ID, a.Findings); err != nil {
		return "", err
	}
	return fmt.Sprintf("added one repair task carrying the review's %d finding(s), reset it, and recorded that this was done once", len(a.Findings)), nil
}

func (*judgeRepair) UndoHint(RepairFinding) string {
	return "cancel the repair task from the Automaton's task list; the repair is recorded and will not be made again"
}

// ---- (c) merge wait / merge ready: reported only ----

type mergeReport struct{ ops AdvanceOps }

func (*mergeReport) Group() string { return RepairGroupAdvanceJudge }

func (r *mergeReport) Detect(ctx context.Context) []RepairFinding {
	var out []RepairFinding
	for _, a := range r.ops.Automata(ctx) {
		switch a.MergeState {
		case MergeStateWait:
			out = append(out, RepairFinding{Subject: advanceSubject(a) + " merge",
				Summary: fmt.Sprintf("Automaton %q is waiting to merge", a.Title)})
		case MergeStateReady:
			out = append(out, RepairFinding{Subject: advanceSubject(a) + " merge",
				Summary: fmt.Sprintf("Automaton %q is ready to merge", a.Title)})
		}
	}
	return out
}

// Apply never merges: it only says that the state is reported.
func (*mergeReport) Apply(context.Context, RepairFinding) (string, error) {
	return "reported only; the merge is left to the operator or the merge pipeline", nil
}

func (*mergeReport) UndoHint(RepairFinding) string {
	return "nothing was changed, so there is nothing to undo"
}

// ---- Manager-backed ops ----

// ManagerAdvanceOps is the AdvanceOps backed by the Automaton store.
type ManagerAdvanceOps struct {
	API *API
}

// Automata lists Automata that are not finished.
func (o ManagerAdvanceOps) Automata(_ context.Context) []AdvanceAutomaton {
	if o.API == nil || o.API.M == nil {
		return nil
	}
	st := o.API.M.Store()
	var out []AdvanceAutomaton
	for _, p := range st.ListPRDs() {
		prd, ok := st.SnapshotPRD(p.ID)
		if !ok {
			continue
		}
		switch prd.Status {
		case PRDCompleted, PRDRejected, PRDCancelled, PRDArchived:
			continue
		}
		a := AdvanceAutomaton{ID: prd.ID, Title: prd.Title, Running: prd.Status == PRDRunning,
			LiveExecutor: o.API.executorLive(prd.ID)}
		for _, s := range prd.Story {
			for _, t := range s.Tasks {
				a.TaskStatuses = append(a.TaskStatuses, t.Status)
			}
		}
		for _, d := range prd.Decisions {
			if d.Kind == judgeRepairDecisionKind {
				a.RepairMade = true
			}
		}
		if prd.Status == PRDBlocked && prd.RulesReviewOutcome != nil {
			ro := prd.RulesReviewOutcome
			a.JudgeBlocked = true
			a.Findings = ro.Findings
			a.HasRepairTask = ro.Action == RulesReviewActionFixTaskCreated
		}
		out = append(out, a)
	}
	return out
}

// Advance re-fires the completion rollup, the normal way a finished run
// moves on.
func (o ManagerAdvanceOps) Advance(_ context.Context, id string) error {
	if o.API == nil {
		return errors.New("Automaton API not wired")
	}
	_, err := o.API.refireRollup(id)
	return err
}

// RepairJudge adds one task carrying the findings to the last story, resets
// it with a plain reset, and records the decision.
func (o ManagerAdvanceOps) RepairJudge(_ context.Context, id string, findings []string) error {
	if o.API == nil {
		return errors.New("Automaton API not wired")
	}
	prd, ok := o.API.M.Store().SnapshotPRD(id)
	if !ok || len(prd.Story) == 0 {
		return errors.New("Automaton or its stories not found")
	}
	story := prd.Story[len(prd.Story)-1]
	spec := "Fix every finding from the completion rules review, then finish:\n- " + strings.Join(findings, "\n- ")
	before := map[string]bool{}
	for _, t := range story.Tasks {
		before[t.ID] = true
	}
	if _, err := o.API.AddTask(id, story.ID, "Fix rules review findings", spec, "housekeeping-repair", nil); err != nil {
		return err
	}
	o.API.M.recordStuckDecision(id, judgeRepairDecisionKind,
		"housekeeping made the one repair task from the review findings; it will not be made again")
	after, ok := o.API.M.Store().SnapshotPRD(id)
	if !ok {
		return nil
	}
	for _, s := range after.Story {
		if s.ID != story.ID {
			continue
		}
		for _, t := range s.Tasks {
			if !before[t.ID] {
				_, err := o.API.ResetTaskWithOptions(id, t.ID, "housekeeping-repair", false, false)
				return err
			}
		}
	}
	return nil
}
