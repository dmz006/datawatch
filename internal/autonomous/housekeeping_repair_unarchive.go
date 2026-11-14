// One-time "unarchive work that never landed" repair. It finds Automata
// that were archived while their work branch still carried commits the base
// branch does not have — exactly the condition the archive guard checks —
// and moves each one back to its pre-archive terminal status: completed, or
// cancelled when it had been cancelled automatically.
//
// Like the guard itself, the repair treats the merge-state check as the
// one-time source of truth and keeps everything it cannot verify: a git
// failure is reported as unsafe and never repaired, records the operator
// archived or cancelled deliberately are never touched, and nothing here
// kills, ends or signals a session (nor merges or ships anything). It only
// changes a record's status and appends one decision, so an undo is one
// operator archive again.
package autonomous

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// unarchiveDoneMarker is the durable completion record for the one-time
// repair, kept in the data directory next to the Automaton store so it
// never runs again after it has — on restarts, idle ticks or mode changes.
const unarchiveDoneMarker = "unarchive-repair.done"

// unarchiveDecisionKind is the decision the repair appends to every record
// it moves; it is how the operator finds and undoes the move.
const unarchiveDecisionKind = "unarchive_unmerged"

// unarchiveRepairActor writes the repair's own decisions under a non-human,
// non-loop actor so the record's log shows that a repair, not a person or
// the supervisor loop, moved the status.
const unarchiveRepairActor = "housekeeping-repair"

// unarchiveOperatorAbandoned flags the reason a record the operator closed
// must not be touched or reported by the repair.
const unarchiveOperatorAbandoned = "operator-abandoned"

// UnarchiveCandidate is one archived record the repair found: restorable
// (Unsafe false) or reported-only (Unsafe true — the work cannot be verified
// as landed, or a person's call is involved).
type UnarchiveCandidate struct {
	// ID and Title name the record in the report.
	ID    string
	Title string
	// Branch is the work branch the merge-state check ran against; Commits
	// is how many commits on it the base branch does not carry.
	Branch  string
	Commits int
	// RestoreTo is the pre-archive terminal status the record goes back
	// to: completed, or cancelled when it was cancelled automatically.
	RestoreTo PRDStatus
	// Unsafe marks a reported-only candidate; UnsafeReason says why a
	// person is needed instead.
	Unsafe       bool
	UnsafeReason string
}

// UnarchiveRepairOps is everything the "unarchive unmerged work" repair may
// read and do. There is no session anywhere in this interface: the repair
// can only move a record's status and write its completion marker.
type UnarchiveRepairOps interface {
	// Done reports whether the one-time repair has already run.
	Done(ctx context.Context) bool
	// Candidates lists the archived records whose branch still carries
	// unmerged work, with the status each one should be restored to. It
	// is read-only.
	Candidates(ctx context.Context) []UnarchiveCandidate
	// Restore moves the record to c.RestoreTo and appends the
	// unarchive_unmerged decision. It is a no-op error-free success when
	// the record already sits at the restored status.
	Restore(ctx context.Context, c UnarchiveCandidate) error
	// Remember records that the one-time repair has run.
	Remember(ctx context.Context) error
}

// NewUnarchiveRepairs builds the repairs of the one-time "unarchive unmerged
// work" group.
func NewUnarchiveRepairs(ops UnarchiveRepairOps) []Repair {
	if ops == nil {
		return nil
	}
	return []Repair{&unarchiveRepair{ops: ops}}
}

// unarchiveRepair implements Repair for the unarchive group.
type unarchiveRepair struct {
	ops UnarchiveRepairOps

	// Pass accounting. Remember is written after every restorable finding
	// of one pass has been handled and none of them failed.
	mu        sync.Mutex
	safeTotal int
	safeDone  int
	failed    bool
}

func (*unarchiveRepair) Group() string { return RepairGroupUnarchive }

func (r *unarchiveRepair) resetPass() {
	r.mu.Lock()
	r.safeTotal, r.safeDone, r.failed = 0, 0, false
	r.mu.Unlock()
}

func (r *unarchiveRepair) noteSafe() {
	r.mu.Lock()
	r.safeTotal++
	r.mu.Unlock()
}

func (r *unarchiveRepair) failPass() {
	r.mu.Lock()
	r.failed = true
	r.mu.Unlock()
}

// finishPass advances the accounting; it reports whether this finding was
// the last restorable one of the pass and no restore failed.
func (r *unarchiveRepair) finishPass() bool {
	r.mu.Lock()
	r.safeDone++
	last := !r.failed && r.safeDone >= r.safeTotal
	if last {
		r.safeTotal, r.safeDone, r.failed = 0, 0, false
	}
	r.mu.Unlock()
	return last
}

func (r *unarchiveRepair) remember(ctx context.Context) {
	if err := r.ops.Remember(ctx); err != nil {
		// The restore itself landed; a marker failure only means the
		// next pass reports a no-op (the repair stays idempotent).
		log.Printf("[autonomous] unarchive repair: completion marker: %v", err)
	}
}

func (r *unarchiveRepair) Detect(ctx context.Context) []RepairFinding {
	if r.ops.Done(ctx) {
		return nil // It ran once; never again.
	}
	r.resetPass()
	var out []RepairFinding
	for _, c := range r.ops.Candidates(ctx) {
		name := c.Title
		if name == "" {
			name = c.ID
		}
		if c.Unsafe {
			// The work cannot be verified as landed: report it, alert once,
			// never repair it.
			out = append(out, RepairFinding{
				Subject:      "unarchive " + c.ID,
				Summary:      fmt.Sprintf("the archived Automaton %q may carry work that never reached the base branch", name),
				Unsafe:       true,
				UnsafeReason: c.UnsafeReason,
			})
			continue
		}
		if c.Commits <= 0 {
			continue // nothing unmerged: the archive was right
		}
		out = append(out, RepairFinding{
			Subject: "unarchive " + c.ID,
			Summary: fmt.Sprintf("the archived Automaton %q ended %s while %d commit(s) on branch %s had not reached the base branch",
				name, c.RestoreTo, c.Commits, c.Branch),
		})
		r.noteSafe()
	}
	return out
}

func (r *unarchiveRepair) findCandidate(ctx context.Context, id string) (UnarchiveCandidate, bool) {
	for _, c := range r.ops.Candidates(ctx) {
		if c.ID == id {
			return c, true
		}
	}
	return UnarchiveCandidate{}, false
}

func (r *unarchiveRepair) Apply(ctx context.Context, f RepairFinding) (string, error) {
	id := strings.TrimPrefix(f.Subject, "unarchive ")
	c, found := r.findCandidate(ctx, id)
	if !found || c.Commits <= 0 {
		// Since the detection the record left the state (restored by an
		// operator or merge in the meantime, or the branch emptied): the
		// repair is idempotent and there is nothing to do.
		if r.finishPass() {
			r.remember(ctx)
		}
		return "the record is no longer archived with unmerged work (already restored, or the work landed meanwhile); nothing was changed", nil
	}
	if c.Unsafe {
		// Safety is rechecked on a live re-scan: anything that now needs a
		// person is refused here too, not repaired.
		r.failPass()
		return "", fmt.Errorf("this record cannot be repaired by the daemon: %s", c.UnsafeReason)
	}
	if err := r.ops.Restore(ctx, c); err != nil {
		r.failPass()
		return "", fmt.Errorf("could not restore Automaton %s: %w", id, err)
	}
	if r.finishPass() {
		r.remember(ctx)
	}
	return fmt.Sprintf("restored %s back to %s (branch %s still had %d unlanded commit(s)); no session was killed, ended or signaled at all",
		id, c.RestoreTo, c.Branch, c.Commits), nil
}

func (*unarchiveRepair) UndoHint(RepairFinding) string {
	return "the restored status and the " + unarchiveDecisionKind +
		" decision are on the record; to undo, archive the record again as the operator (an operator archive is never gated by the merged-branch guard)"
}

// unarchiveRestoreDecision reads the decision log for the record's
// pre-archive terminal status:
//
//   - an archive or a cancel by the operator is the operator deliberately
//     closing the record: the repair leaves it alone (not even reported);
//   - the last status decision before the archive being a cancel by anyone
//     else restores the record to cancelled;
//   - a reject before the archive is a person's call: report, do not repair;
//   - work (decompose, run, verify, approve) or a reset to draft after the
//     last cancel or reject means the record finished completed;
//   - no archive decision at all means the archive was manual: the repair
//     does not guess.
func unarchiveRestoreDecision(ds []Decision) (PRDStatus, bool, string) {
	archIdx := -1
	for i := len(ds) - 1; i >= 0; i-- {
		if ds[i].Kind == "archive" {
			archIdx = i
			break
		}
	}
	if archIdx < 0 {
		return "", false, "there is no archive decision on the record's log; a manual archive is not guessed at by the repair"
	}
	if ds[archIdx].Actor == operatorActor {
		return "", false, unarchiveOperatorAbandoned
	}
	for i := archIdx - 1; i >= 0; i-- {
		switch ds[i].Kind {
		case "cancel":
			if ds[i].Actor == operatorActor {
				return "", false, unarchiveOperatorAbandoned
			}
			return PRDCancelled, true, ""
		case "reject":
			return "", false, "the Automaton was rejected before archiving; rejection is the operator's call and needs a person"
		case "reset_to_draft", "decompose", "run", "verify", "approve":
			return PRDCompleted, true, ""
		}
	}
	return PRDCompleted, true, ""
}

// ManagerUnarchiveOps is the production ops for the one-time repair: it
// reads the Automaton store, reuses the archive merged-branch check the
// guard itself uses, and restores statuses through the same store-lock
// path the manager's own status changes take.
type ManagerUnarchiveOps struct {
	// M is the manager whose store holds the records.
	M *Manager
}

var _ UnarchiveRepairOps = ManagerUnarchiveOps{}

// Done reports the one-time completion marker.
func (o ManagerUnarchiveOps) Done(_ context.Context) bool {
	if o.M == nil || o.M.store == nil || o.M.store.dir == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(o.M.store.dir, unarchiveDoneMarker))
	return err == nil
}

// Candidates lists the archived records the repair may restore or must
// report. Nothing is changed.
func (o ManagerUnarchiveOps) Candidates(ctx context.Context) []UnarchiveCandidate {
	m := o.M
	if m == nil || m.store == nil {
		return nil
	}
	var out []UnarchiveCandidate
	for _, p := range m.store.ListPRDs() {
		prd, ok := m.store.SnapshotPRD(p.ID)
		if !ok || prd.Status != PRDArchived || prd.Git.Branch == "" {
			continue
		}
		name := prd.Title
		if name == "" {
			name = prd.ID
		}
		restored, ok, reason := unarchiveRestoreDecision(prd.Decisions)
		if !ok {
			if reason == unarchiveOperatorAbandoned {
				continue // the operator closed this record: never touched, not even reported
			}
			out = append(out, UnarchiveCandidate{
				ID: prd.ID, Title: name, Branch: prd.Git.Branch,
				Unsafe: true, UnsafeReason: reason,
			})
			continue
		}
		// The same check as the archive guard: unknown stays unknown, never
		// treated as merged.
		block, commits, known := m.archiveBranchMergeBlock(ctx, prd)
		switch {
		case !block:
			// nothing unmerged (or an empty branch, or a dropped one): the
			// record was archived correctly
		case !known:
			out = append(out, UnarchiveCandidate{
				ID: prd.ID, Title: name, Branch: prd.Git.Branch,
				Unsafe:       true,
				UnsafeReason: "the merge state of branch " + prd.Git.Branch + " could not be determined (git failed); the record must be kept and a person must look",
			})
		case commits <= 0:
			out = append(out, UnarchiveCandidate{
				ID: prd.ID, Title: name, Branch: prd.Git.Branch,
				Unsafe:       true,
				UnsafeReason: "there is no base repository to check the merge against; the record must be kept and a person must look",
			})
		default:
			out = append(out, UnarchiveCandidate{
				ID: prd.ID, Title: name, Branch: prd.Git.Branch,
				Commits: commits, RestoreTo: restored,
			})
		}
	}
	return out
}

// Restore moves the record back to its pre-archive terminal status and
// appends the unarchive_unmerged decision.
func (o ManagerUnarchiveOps) Restore(ctx context.Context, c UnarchiveCandidate) error {
	m := o.M
	if m == nil || m.store == nil {
		return fmt.Errorf("no Automaton manager is wired to the repair")
	}
	p, ok := m.store.GetPRD(c.ID)
	if !ok {
		return fmt.Errorf("Automaton %s no longer exists", c.ID)
	}
	if p.Status == c.RestoreTo {
		return nil // already restored: the repair is one-time and idempotent
	}
	if p.Status != PRDArchived {
		return fmt.Errorf("Automaton %s is at %s now, not archived; the repair changes nothing", c.ID, p.Status)
	}
	now := time.Now()
	old := p.Status
	m.store.WithLock(func() {
		p.Status = c.RestoreTo
		p.UpdatedAt = now
		p.Decisions = append(p.Decisions, Decision{
			At:    now,
			Kind:  unarchiveDecisionKind,
			Actor: unarchiveRepairActor,
			Note:  fmt.Sprintf("moved out of archived by the one-time repair: branch %s still had %d commit(s) the base branch did not carry when the record was archived", c.Branch, c.Commits),
		})
	})
	if err := m.store.SavePRD(p); err != nil {
		return fmt.Errorf("could not save the record: %w", err)
	}
	m.emitPRDStatus(p, old, lastDecision(p, unarchiveDecisionKind), nil)
	title := p.Title
	if title == "" {
		title = p.ID
	}
	if sink := m.AlertSink(); sink != nil {
		sink("info", "Housekeeping repair unarchived an Automaton",
			fmt.Sprintf("%q was archived while branch %s still carried %d unlanded commit(s); it is back at %s. To undo, archive it again as the operator.", title, c.Branch, c.Commits, c.RestoreTo))
	}
	return nil
}

// Remember records the one-time completion of the repair.
func (o ManagerUnarchiveOps) Remember(_ context.Context) error {
	m := o.M
	if m == nil || m.store == nil || m.store.dir == "" {
		return fmt.Errorf("no Automaton manager is wired to the repair, so its completion cannot be recorded")
	}
	b, err := json.Marshal(map[string]any{
		"done_at": time.Now().UTC(),
		"note":    "the one-time unarchive-unmerged-work repair has run; the records it restored carry the " + unarchiveDecisionKind + " decision",
	})
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(m.store.dir, unarchiveDoneMarker), append(b, '\n'), 0o600)
}
