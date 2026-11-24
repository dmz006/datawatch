// Landing record. Every Automaton that has a work branch carries a landing
// record saying whether that branch's work reached the project's default
// branch: pending when the branch is created, landed when the merge pipeline
// step (through MarkPRDCompleted) or a shipped record names the merge commit,
// and kept or dropped when the operator decides the branch should not be
// merged. Archive refuses an Automaton whose record is still pending, so no
// unlanded branch disappears from view.
package autonomous

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// LandingState is the state of an Automaton's landing record.
type LandingState string

const (
	LandingPending LandingState = "pending"
	LandingLanded  LandingState = "landed"
	LandingKept    LandingState = "kept"
	LandingDropped LandingState = "dropped"
)

// Decision kinds written by KeepLanding and DropLanding.
const (
	DecisionLandingKeep = "landing_keep"
	DecisionLandingDrop = "landing_drop"
)

// Landing is the landing record of an Automaton's branch.
type Landing struct {
	State  LandingState `json:"state"`
	Commit string       `json:"commit,omitempty"`
	Actor  string       `json:"actor,omitempty"`
	At     time.Time    `json:"at"`
	Reason string       `json:"reason,omitempty"`
	// Tip is the commit the branch pointed at when a keep or drop decision
	// was recorded. The unlanded list stops honouring the decision once the
	// branch moves past it. Empty when the tip could not be read.
	Tip string `json:"tip,omitempty"`
}

// markLandingPendingLocked sets a pending record when the Automaton has a
// branch and no record yet. Callers hold the store lock.
func markLandingPendingLocked(p *PRD, actor, reason string) {
	if p == nil || p.Landing != nil || strings.TrimSpace(p.Git.Branch) == "" {
		return
	}
	p.Landing = &Landing{State: LandingPending, Actor: actor, At: time.Now(), Reason: reason}
}

// markLandingLandedLocked records the merge commit. Callers hold the store
// lock. It reports whether anything changed.
func markLandingLandedLocked(p *PRD, commit, actor string, at time.Time) bool {
	if p == nil {
		return false
	}
	if p.Landing == nil && strings.TrimSpace(p.Git.Branch) == "" {
		return false
	}
	if p.Landing != nil && p.Landing.State == LandingLanded && p.Landing.Commit == commit {
		return false
	}
	if at.IsZero() {
		at = time.Now()
	}
	p.Landing = &Landing{State: LandingLanded, Commit: commit, Actor: actor, At: at, Reason: "merged on the default branch"}
	return true
}

// landingPendingBranch returns the branch of an Automaton whose landing is
// still pending, or "".
func landingPendingBranch(p *PRD) string {
	if p == nil || p.Landing == nil || p.Landing.State != LandingPending {
		return ""
	}
	return strings.TrimSpace(p.Git.Branch)
}

// KeepLanding records that the Automaton's branch is kept unmerged on purpose.
func (m *Manager) KeepLanding(id, actor, reason string) (*PRD, error) {
	return m.decideLanding(id, actor, reason, LandingKept, DecisionLandingKeep)
}

// DropLanding records that the Automaton's branch is dropped and its work
// will not be merged.
func (m *Manager) DropLanding(id, actor, reason string) (*PRD, error) {
	return m.decideLanding(id, actor, reason, LandingDropped, DecisionLandingDrop)
}

func (m *Manager) decideLanding(id, actor, reason string, state LandingState, kind string) (*PRD, error) {
	actor, reason = strings.TrimSpace(actor), strings.TrimSpace(reason)
	if actor == "" {
		return nil, fmt.Errorf("landing %s for Automaton %q: actor is required", state, id)
	}
	if reason == "" {
		return nil, fmt.Errorf("landing %s for Automaton %q: reason is required", state, id)
	}
	p, ok := m.store.GetPRD(id)
	if !ok {
		return nil, fmt.Errorf("automaton %q not found", id)
	}
	tip := m.landingBranchTip(p)
	var refuse error
	m.store.WithLock(func() {
		branch := strings.TrimSpace(p.Git.Branch)
		if branch == "" {
			refuse = fmt.Errorf("automaton %q has no branch: there is nothing to keep or drop", id)
			return
		}
		if p.Landing != nil && p.Landing.State == LandingLanded {
			refuse = fmt.Errorf("automaton %q branch %s already landed (commit %s)", id, branch, p.Landing.Commit)
			return
		}
		now := time.Now()
		p.Landing = &Landing{State: state, Actor: actor, At: now, Reason: reason, Tip: tip}
		p.UpdatedAt = now
		p.Decisions = append(p.Decisions, Decision{At: now, Kind: kind, Actor: actor,
			Note: fmt.Sprintf("landing: branch %s recorded %s: %s", branch, state, reason)})
	})
	if refuse != nil {
		return nil, refuse
	}
	if err := m.store.SavePRD(p); err != nil {
		return nil, fmt.Errorf("landing: save: %w", err)
	}
	out, _ := m.store.GetPRD(id)
	return out, nil
}

// landingBranchTip reads the commit the Automaton's branch points at in the
// base repository, read-only. It returns "" when there is no base repository,
// no branch, or git cannot say: the decision is then recorded without a tip
// and is honoured as before.
func (m *Manager) landingBranchTip(p *PRD) string {
	branch := strings.TrimSpace(p.Git.Branch)
	repo := strings.TrimSpace(m.archivedBaseRepo())
	if branch == "" || repo == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), archiveBranchGitTimeout)
	defer cancel()
	out, err := m.archivedUnmergedGitRunner().Run(ctx, repo, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// LandingMigrationOpts configures MigrateLanding.
type LandingMigrationOpts struct {
	// Repo is the base repository. Empty uses Config.WorktreeBaseRepo.
	Repo string
	// Git runs the read-only git commands. Nil uses the production runner.
	Git MergeGitRunner
}

// MigrateLanding is the one-time landing migration: every archived Automaton
// with a branch and no landing record, whose branch read-only git cannot
// prove merged on the default branch and which still has commits ahead of it,
// gets a pending record. Nothing else is
// changed (no status, no decision). Records that already carry a landing
// record are skipped, so running it again changes nothing. It returns and
// logs the number of Automata marked.
func (m *Manager) MigrateLanding(ctx context.Context, opts LandingMigrationOpts) (int, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	repo := strings.TrimSpace(opts.Repo)
	if repo == "" {
		repo = strings.TrimSpace(m.Config().WorktreeBaseRepo)
	}
	git := opts.Git
	if git == nil {
		git = NewMergeGitRunner()
	}
	count := 0
	var errs []string
	for _, p := range m.store.ListPRDs() {
		if p == nil {
			continue
		}
		var skip bool
		var branch string
		m.readLocked(func() {
			branch = strings.TrimSpace(p.Git.Branch)
			skip = p.Status != PRDArchived || p.Landing != nil || branch == ""
		})
		if skip {
			continue
		}
		proof := MergedMemberProof(ctx, git, repo, branch)
		if proof.Merged {
			continue
		}
		if proof.Base == "" {
			// The default branch is unknown: never guess.
			errs = append(errs, fmt.Sprintf("Automaton %s: %s", p.ID, proof.Reason))
			continue
		}
		// A branch with no commits ahead of the default branch (its tip is on
		// the default branch: fast-forward or merge-commit landing, or no work
		// at all) has nothing left to land. The merge proof reports that as
		// "empty", not merged, so check it here; the worktree cleanup treats
		// the same state as safe the same way.
		if proof.State == MergeStateEmpty {
			continue
		}
		changed := false
		m.store.WithLock(func() {
			if p.Landing != nil {
				return
			}
			p.Landing = &Landing{State: LandingPending, Actor: "landing_migration", At: time.Now(),
				Reason: "landing migration: " + proof.Reason}
			changed = true
		})
		if !changed {
			continue
		}
		if err := m.store.SavePRD(p); err != nil {
			errs = append(errs, fmt.Sprintf("Automaton %s: save: %v", p.ID, err))
			continue
		}
		count++
	}
	log.Printf("[autonomous] landing migration: marked %d archived Automata pending (branch not on the default branch)", count)
	if len(errs) > 0 {
		return count, fmt.Errorf("landing migration: %s", strings.Join(errs, "; "))
	}
	return count, nil
}

// landingMigrationDoneFile is the durable marker StartLandingMigration writes
// after a clean pass, so the one-time migration never runs again.
const landingMigrationDoneFile = "landing_migration.done"

// LandingMigrationDone reports whether the one-time landing migration has
// already finished cleanly on this daemon's data directory.
func (m *Manager) LandingMigrationDone() bool {
	_, err := os.Stat(filepath.Join(m.store.dir, landingMigrationDoneFile))
	return err == nil
}

// RunLandingMigrationOnce is the daemon-start pass of MigrateLanding. It does
// nothing once the done marker exists. Otherwise it runs the migration, logs
// the count, raises one operator alert (through the alert hook set by
// SetAlertFn) when any archived Automaton was marked pending, and writes the
// done marker when the pass finished without errors; a pass with errors (for
// example an unknown default branch) is retried at the next start. It returns
// the count and the pass error.
func (m *Manager) RunLandingMigrationOnce(ctx context.Context, opts LandingMigrationOpts) (int, error) {
	if m.LandingMigrationDone() {
		return 0, nil
	}
	n, err := m.MigrateLanding(ctx, opts)
	if err != nil {
		log.Printf("[autonomous] landing migration: %v", err)
	}
	if n > 0 {
		m.mu.Lock()
		alert := m.alertFn
		m.mu.Unlock()
		if alert != nil {
			alert("Archived Automata with unlanded branches",
				fmt.Sprintf("The landing migration marked %d archived Automata pending: their branch is not on the default branch. Merge each branch, or record keep or drop for its landing.", n))
		}
	}
	if err == nil {
		marker, _ := json.Marshal(map[string]any{"done_at": time.Now().UTC(), "marked": n})
		if werr := os.WriteFile(filepath.Join(m.store.dir, landingMigrationDoneFile), marker, 0o600); werr != nil {
			log.Printf("[autonomous] landing migration: done marker: %v", werr)
		}
	}
	return n, err
}

// landingAncestorActor is the distinct actor named on a landing record that
// the ancestor check flips from pending to landed.
const landingAncestorActor = "landing_ancestor"

// CheckLandingAncestor re-checks one Automaton whose landing record is still
// pending and which has a branch: it resolves the default branch of the base
// repository (m.archivedBaseRepo()) with defaultBranchForRepo, then asks git
// (through m.archivedUnmergedGitRunner(), so tests inject a fake) whether the
// branch is an ancestor of the default branch (merge-base --is-ancestor). An
// exit success means the branch has landed on the default branch; any other
// answer, a missing branch, an empty base repository or a git error means
// not landed, and the record is left pending, so an unknown state never
// loses a branch. When it has landed, it resolves the merge commit - the
// first merge commit on the default branch that contains the branch tip
// (rev-list --ancestry-path --merges --reverse) for a merge-commit landing,
// the branch tip itself for a fast-forward - records it with
// markLandingLandedLocked under the landingAncestorActor name and persists
// the PRD. It reports whether the record now reads landed.
func (m *Manager) CheckLandingAncestor(ctx context.Context, id string) bool {
	if ctx == nil {
		ctx = context.Background()
	}
	repo := strings.TrimSpace(m.archivedBaseRepo())
	if repo == "" {
		return false
	}
	p, ok := m.store.GetPRD(id)
	if !ok {
		return false
	}
	var branch string
	m.readLocked(func() { branch = landingPendingBranch(p) })
	if branch == "" {
		return false // no branch, or the landing is not pending
	}
	git := m.archivedUnmergedGitRunner()
	base, err := defaultBranchForRepo(ctx, git, repo)
	if err != nil {
		return false // the default branch is unknown: never guess
	}
	// merge-base --is-ancestor exits 0 only when branch is on the ancestry
	// path of base: the branch has landed. Anything else (not an ancestor,
	// missing ref, git failure) is not landed.
	if _, err := git.Run(ctx, repo, "merge-base", "--is-ancestor", branch, base); err != nil {
		return false
	}
	tipOut, err := git.Run(ctx, repo, "rev-parse", "refs/heads/"+branch)
	if err != nil {
		return false
	}
	tip := strings.TrimSpace(tipOut)
	if tip == "" {
		return false
	}
	commit := ""
	if mb, err := git.Run(ctx, repo, "merge-base", branch, base); err == nil && strings.TrimSpace(mb) == tip {
		commit = tip // fast-forward: the tip is on the default branch itself
	}
	if commit == "" {
		out, err := git.Run(ctx, repo, "rev-list", "--ancestry-path", "--merges", "--reverse", tip+".."+base)
		if err != nil {
			return false
		}
		for _, line := range strings.Split(out, "\n") {
			if line = strings.TrimSpace(line); line != "" {
				commit = line // first merge on the default branch containing the tip
				break
			}
		}
		if commit == "" {
			return false
		}
	}
	now := time.Now()
	changed := false
	m.store.WithLock(func() {
		changed = markLandingLandedLocked(p, commit, landingAncestorActor, now)
		if changed {
			p.UpdatedAt = now
		}
	})
	if !changed {
		return true // the record already reads landed
	}
	if err := m.store.SavePRD(p); err != nil {
		return false
	}
	return true
}

// StartLandingMigration runs RunLandingMigrationOnce in a goroutine at daemon
// start, so the read-only git checks never block or fail startup.
func (m *Manager) StartLandingMigration() {
	go func() {
		_, _ = m.RunLandingMigrationOnce(context.Background(), LandingMigrationOpts{})
	}()
}
