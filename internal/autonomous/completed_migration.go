// Cancelled-to-completed migration. Work that was merged on the project's
// default branch (or that carries a shipped record) but whose Automaton or
// Automation ended up cancelled is moved to completed, with the completion
// time taken from the shipped record's merge time or, failing that, from the
// commit time of the merge proof — never from now. Nothing here starts,
// stops, kills or signals a session, and no git command writes: the merge
// proof (MergedMemberProof) and the commit-time lookup are read-only.
//
// MarkPRDCompleted is the one write path for a PRD: the migration calls it
// for every PRD it changes, and the merge pipeline step and the merge helper
// use it later.
package autonomous

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// DecisionCompletedMigration is the Decision kind written on every record the
// migration (or MarkPRDCompleted) moves to completed.
const DecisionCompletedMigration = "completed_migration"

// ErrCompletedMigrationBusy is returned by a real MigrateCompleted run while a
// final gate is running: moving statuses under a running gate could race it.
var ErrCompletedMigrationBusy = errors.New("completed migration: a final gate is running; try again when it has finished")

// CompletedMigrationOpts configures one MigrateCompleted pass.
type CompletedMigrationOpts struct {
	// DryRun computes the same report but writes nothing.
	DryRun bool
	// Limit is the most records one run changes; 0 means no limit. Records are
	// taken oldest first so repeated limited runs make progress.
	Limit int
	// ID restricts the pass to one Automaton or Automation. In this mode the
	// source status may be cancelled, blocked or failed (Automations:
	// cancelled or blocked); in bulk mode only cancelled.
	ID string
	// Repo is the base repository. Empty uses Config.WorktreeBaseRepo.
	Repo string
	// Git runs the read-only git commands. Nil uses the production runner.
	Git MergeGitRunner
}

// CompletedMigrationItem is one record changed (or that would change).
type CompletedMigrationItem struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Kind        string    `json:"kind"` // automaton | automation
	From        string    `json:"from"`
	To          string    `json:"to"`
	CompletedAt time.Time `json:"completed_at"`
	Commit      string    `json:"commit,omitempty"`
	Reason      string    `json:"reason"`
}

// CompletedMigrationReport is the accounting of one MigrateCompleted pass.
type CompletedMigrationReport struct {
	DryRun bool `json:"dry_run"`
	// Busy is true when a final gate is running. A real run then fails with
	// ErrCompletedMigrationBusy; a dry run still reports.
	Busy bool `json:"busy"`
	// Considered is the number of records whose status made them candidates.
	Considered int `json:"considered"`
	// Changed is the number of records moved to completed (real run only).
	Changed int `json:"changed"`
	// WouldComplete lists the records a dry run would change.
	WouldComplete []CompletedMigrationItem `json:"would_complete,omitempty"`
	// Completed lists the records a real run changed.
	Completed []CompletedMigrationItem `json:"completed,omitempty"`
	// AlreadyCompleted counts records a previous run already moved.
	AlreadyCompleted int `json:"already_completed"`
	// LeftCancelled counts cancelled records left exactly as they are: no
	// shipped record and no proof that the branch is merged.
	LeftCancelled int `json:"left_cancelled"`
	// LeftOther counts blocked or failed records (single-id mode) and held
	// records left alone.
	LeftOther int `json:"left_other,omitempty"`
	// Deferred counts candidates not looked at because Limit was reached.
	Deferred int      `json:"deferred,omitempty"`
	Errors   []string `json:"errors,omitempty"`
}

// completedMigrationRecord is one candidate in the stable oldest-first order.
type completedMigrationRecord struct {
	id        string
	createdAt time.Time
	prd       *PRD
	auto      *Automation
}

// finalGateRunning reports whether any PRD has a final-gate task in flight.
func (m *Manager) finalGateRunning() bool {
	busy := false
	for _, p := range m.store.ListPRDs() {
		if p == nil {
			continue
		}
		m.readLocked(func() {
			for i := range p.Story {
				for j := range p.Story[i].Tasks {
					t := &p.Story[i].Tasks[j]
					if t.Kind != TaskKindFinalGate {
						continue
					}
					switch t.Status {
					case TaskInProgress, TaskRunningTests, TaskVerifying:
						busy = true
					}
				}
			}
		})
		if busy {
			return true
		}
	}
	return false
}

// completedMigrationBranch is the work branch of a PRD.
func completedMigrationBranch(p *PRD) string {
	if b := strings.TrimSpace(p.Git.Branch); b != "" {
		return b
	}
	return "automaton/" + p.ID
}

// commitTime reads the committer time of commit, read-only.
func completedMigrationCommitTime(ctx context.Context, git MergeGitRunner, repo, commit string) (time.Time, error) {
	out, err := git.Run(ctx, repo, "show", "-s", "--format=%cI", commit)
	if err != nil {
		return time.Time{}, fmt.Errorf("read commit time of %s: %v: %s", commit, err, strings.TrimSpace(out))
	}
	t, perr := time.Parse(time.RFC3339, strings.TrimSpace(out))
	if perr != nil {
		return time.Time{}, fmt.Errorf("read commit time of %s: %v", commit, perr)
	}
	return t, nil
}

// completedEvidence is why a record may be completed and when.
type completedEvidence struct {
	ok      bool
	at      time.Time
	commit  string
	release string
	reason  string
	err     string
}

func (m *Manager) prdCompletedEvidence(ctx context.Context, git MergeGitRunner, repo string, p *PRD) completedEvidence {
	var e completedEvidence
	if p.IsShipped() {
		e.commit, e.release = p.MergeCommit, p.MergedInRelease
		e.reason = fmt.Sprintf("carries a shipped record (commit %s", e.commit)
		if e.release != "" {
			e.reason += ", release " + e.release
		}
		e.reason += ")"
		if p.MergedAt != nil && !p.MergedAt.IsZero() {
			e.at = *p.MergedAt
		} else if t, err := completedMigrationCommitTime(ctx, git, repo, e.commit); err == nil {
			e.at = t
		} else {
			e.err = err.Error()
			return e
		}
		e.ok = true
		return e
	}
	proof := MergedMemberProof(ctx, git, repo, completedMigrationBranch(p))
	if !proof.Merged {
		return e // unknown or not merged: never guess
	}
	t, err := completedMigrationCommitTime(ctx, git, repo, proof.Commit)
	if err != nil {
		e.err = err.Error()
		return e
	}
	e.ok, e.at, e.commit = true, t, proof.Commit
	e.reason = fmt.Sprintf("branch %s is merged on %s (commit %s)", proof.Branch, proof.Base, proof.Commit)
	return e
}

func (m *Manager) automationCompletedEvidence(ctx context.Context, git MergeGitRunner, repo string, a *Automation) completedEvidence {
	var e completedEvidence
	if a.IsShipped() {
		e.commit, e.release = a.MergeCommit, a.MergedInRelease
		e.reason = fmt.Sprintf("carries a shipped record (commit %s)", e.commit)
		if a.MergedAt != nil && !a.MergedAt.IsZero() {
			e.at = *a.MergedAt
		} else if t, err := completedMigrationCommitTime(ctx, git, repo, e.commit); err == nil {
			e.at = t
		} else {
			e.err = err.Error()
			return e
		}
		e.ok = true
		return e
	}
	nonSkipped := 0
	for _, mem := range a.Members {
		if mem.Status == MemberSkipped {
			continue
		}
		nonSkipped++
		if mem.Status == MemberCompleted && strings.TrimSpace(mem.PRDID) == "" {
			continue
		}
		p, found := m.store.GetPRD(mem.PRDID)
		if !found {
			return e
		}
		var pe completedEvidence
		var done bool
		m.readLocked(func() { done = p.Status == PRDCompleted })
		if done && !p.IsShipped() {
			// Completed without a shipped record: a member the migration just
			// completed lends its recorded completion time; any other adds
			// no time but does not block the Automation.
			m.readLocked(func() {
				for _, d := range p.Decisions {
					if d.Kind == DecisionCompletedMigration && d.At.After(e.at) {
						e.at = d.At
					}
				}
			})
			continue
		}
		pe = m.prdCompletedEvidence(ctx, git, repo, p)
		if !pe.ok {
			if pe.err != "" {
				e.err = pe.err
			}
			return e
		}
		if pe.at.After(e.at) {
			e.at, e.commit, e.release = pe.at, pe.commit, pe.release
		}
	}
	if nonSkipped == 0 || e.at.IsZero() {
		return e
	}
	e.ok = true
	e.reason = fmt.Sprintf("every non-skipped member is merged or completed (latest commit %s)", e.commit)
	return e
}

func completedMigrationNote(kind, id, title string, from string, e completedEvidence) string {
	rel := ""
	if e.release != "" {
		rel = ", release " + e.release
	}
	return fmt.Sprintf("completed migration: %s %s (%q) was %s; moved to completed at %s (commit %s%s). Why: %s. To undo: set the status back to cancelled through the Automaton's cancel command.",
		kind, id, title, from, e.at.UTC().Format(time.RFC3339), e.commit, rel, e.reason)
}

// MarkPRDCompleted sets one PRD completed from cancelled, blocked or failed.
// An already completed PRD is a no-op (no second decision line); any other
// status is refused. mergedAt becomes the decision's time (the completion
// time); the PRD's shipped fields are never touched. It is the one write path
// for the cancelled-to-completed change.
func (m *Manager) MarkPRDCompleted(id, release string, mergedAt time.Time, commit, actor, reason string) (*PRD, error) {
	p, ok := m.store.GetPRD(id)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", id)
	}
	if actor == "" {
		actor = "operator"
	}
	at := mergedAt
	if at.IsZero() {
		at = time.Now() // only direct callers with no merge time; the migration always passes one
	}
	var refuse error
	changed := false
	m.store.WithLock(func() {
		switch p.Status {
		case PRDCompleted:
			return
		case PRDCancelled, PRDBlocked, PRDFailed:
		default:
			refuse = fmt.Errorf("prd %q is %s: only cancelled, blocked or failed work can be marked completed", id, p.Status)
			return
		}
		from := p.Status
		e := completedEvidence{at: at, commit: commit, release: release, reason: reason}
		p.Status = PRDCompleted
		p.Decisions = append(p.Decisions, Decision{
			At:    at,
			Kind:  DecisionCompletedMigration,
			Actor: actor,
			Note:  completedMigrationNote("Automaton", p.ID, p.Title, string(from), e),
		})
		changed = true
	})
	if refuse != nil {
		return nil, refuse
	}
	if !changed {
		return p, nil
	}
	if err := m.store.SavePRD(p); err != nil {
		return nil, fmt.Errorf("completed migration: save: %w", err)
	}
	out, ok := m.store.GetPRD(id)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", id)
	}
	return out, nil
}

func hasCompletedMigrationDecision(ds []Decision) bool {
	for _, d := range ds {
		if d.Kind == DecisionCompletedMigration {
			return true
		}
	}
	return false
}

// MigrateCompleted moves cancelled Automata and Automations whose work is
// merged (or shipped) to completed. See CompletedMigrationOpts.
func (m *Manager) MigrateCompleted(ctx context.Context, opts CompletedMigrationOpts) (CompletedMigrationReport, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	rep := CompletedMigrationReport{DryRun: opts.DryRun}
	rep.Busy = m.finalGateRunning()
	if rep.Busy && !opts.DryRun {
		return rep, ErrCompletedMigrationBusy
	}
	repo := strings.TrimSpace(opts.Repo)
	if repo == "" {
		repo = strings.TrimSpace(m.Config().WorktreeBaseRepo)
	}
	git := opts.Git
	if git == nil {
		git = NewMergeGitRunner()
	}
	single := strings.TrimSpace(opts.ID)

	var recs []completedMigrationRecord
	for _, p := range m.store.ListPRDs() {
		if p != nil && (single == "" || p.ID == single) {
			recs = append(recs, completedMigrationRecord{id: p.ID, createdAt: p.CreatedAt, prd: p})
		}
	}
	if m.automations != nil {
		for _, a := range m.automations.List() {
			if a != nil && (single == "" || a.ID == single) {
				recs = append(recs, completedMigrationRecord{id: a.ID, createdAt: a.CreatedAt, auto: a})
			}
		}
	}
	if single != "" && len(recs) == 0 {
		return rep, fmt.Errorf("completed migration: %q is not an Automaton or Automation", single)
	}
	sort.SliceStable(recs, func(i, j int) bool {
		if !recs[i].createdAt.Equal(recs[j].createdAt) {
			return recs[i].createdAt.Before(recs[j].createdAt)
		}
		return recs[i].id < recs[j].id
	})

	done := 0
	for idx, r := range recs {
		if opts.Limit > 0 && done >= opts.Limit {
			for _, rest := range recs[idx:] {
				if m.completedCandidate(rest, single != "") {
					rep.Deferred++
				}
			}
			break
		}
		var status string
		if r.prd != nil {
			m.readLocked(func() { status = string(r.prd.Status) })
		} else {
			status = string(r.auto.Status)
		}
		if r.prd != nil && PRDStatus(status) == PRDHeld {
			// A held record waits by operator decision: it is never moved to
			// completed, in a full pass or when named by id.
			rep.LeftOther++
			continue
		}
		if status == string(PRDCompleted) {
			var hist bool
			if r.prd != nil {
				m.readLocked(func() { hist = hasCompletedMigrationDecision(r.prd.Decisions) })
			} else {
				hist = hasCompletedMigrationDecision(r.auto.Decisions)
			}
			if hist || single != "" {
				rep.AlreadyCompleted++
			}
			continue
		}
		if !m.completedCandidate(r, single != "") {
			if single != "" {
				return rep, fmt.Errorf("completed migration: %s is %s: only cancelled, blocked or failed work can be moved to completed", r.id, status)
			}
			continue
		}
		rep.Considered++

		var e completedEvidence
		title, kind := "", "automaton"
		if r.prd != nil {
			title = r.prd.Title
			e = m.prdCompletedEvidence(ctx, git, repo, r.prd)
		} else {
			title, kind = r.auto.Name, "automation"
			e = m.automationCompletedEvidence(ctx, git, repo, r.auto)
		}
		if !e.ok {
			if e.err != "" {
				rep.Errors = append(rep.Errors, fmt.Sprintf("%s %s: %s", kind, r.id, e.err))
			}
			if status == string(PRDCancelled) {
				rep.LeftCancelled++
			} else {
				rep.LeftOther++
			}
			continue
		}
		item := CompletedMigrationItem{ID: r.id, Title: title, Kind: kind, From: status, To: string(PRDCompleted),
			CompletedAt: e.at, Commit: e.commit, Reason: e.reason}
		if opts.DryRun {
			rep.WouldComplete = append(rep.WouldComplete, item)
			done++
			continue
		}
		var err error
		if r.prd != nil {
			_, err = m.MarkPRDCompleted(r.id, e.release, e.at, e.commit, "operator", e.reason)
		} else {
			err = m.markAutomationCompleted(r.id, e)
		}
		if err != nil {
			rep.Errors = append(rep.Errors, fmt.Sprintf("%s %s: %v", kind, r.id, err))
			continue
		}
		rep.Completed = append(rep.Completed, item)
		rep.Changed++
		done++
	}
	return rep, nil
}

// completedCandidate reports whether the record's status is a source status.
func (m *Manager) completedCandidate(r completedMigrationRecord, single bool) bool {
	var status string
	if r.prd != nil {
		m.readLocked(func() { status = string(r.prd.Status) })
		switch PRDStatus(status) {
		case PRDCancelled:
			return true
		case PRDBlocked, PRDFailed:
			return single
		}
		return false
	}
	switch r.auto.Status {
	case AutomationCancelled:
		return true
	case AutomationBlocked:
		return single
	}
	return false
}

// markAutomationCompleted completes one Automation through the store's Update
// path so version and ownership rules still apply.
func (m *Manager) markAutomationCompleted(id string, e completedEvidence) error {
	if m.automations == nil {
		return fmt.Errorf("automations store not configured")
	}
	_, err := m.automations.Update(id, 0, func(a *Automation) error {
		switch a.Status {
		case AutomationCompleted:
			return nil
		case AutomationCancelled, AutomationBlocked:
		default:
			return fmt.Errorf("automation %q is %s: only cancelled or blocked work can be marked completed", id, a.Status)
		}
		from := a.Status
		a.Status = AutomationCompleted
		at := e.at
		a.FinishedAt = &at
		a.Decisions = append(a.Decisions, Decision{
			At: at, Kind: DecisionCompletedMigration, Actor: "operator",
			Note: completedMigrationNote("Automation", a.ID, a.Name, string(from), e),
		})
		return nil
	})
	return err
}
