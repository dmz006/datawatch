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
	"fmt"
	"log"
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
		return nil, fmt.Errorf("Automaton %q not found", id)
	}
	var refuse error
	m.store.WithLock(func() {
		branch := strings.TrimSpace(p.Git.Branch)
		if branch == "" {
			refuse = fmt.Errorf("Automaton %q has no branch: there is nothing to keep or drop", id)
			return
		}
		if p.Landing != nil && p.Landing.State == LandingLanded {
			refuse = fmt.Errorf("Automaton %q branch %s already landed (commit %s)", id, branch, p.Landing.Commit)
			return
		}
		now := time.Now()
		p.Landing = &Landing{State: state, Actor: actor, At: now, Reason: reason}
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

// LandingMigrationOpts configures MigrateLanding.
type LandingMigrationOpts struct {
	// Repo is the base repository. Empty uses Config.WorktreeBaseRepo.
	Repo string
	// Git runs the read-only git commands. Nil uses the production runner.
	Git MergeGitRunner
}

// MigrateLanding is the one-time landing migration: every archived Automaton
// with a branch and no landing record, whose branch read-only git cannot
// prove merged on the default branch, gets a pending record. Nothing else is
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
