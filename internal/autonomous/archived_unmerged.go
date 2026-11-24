// Archived-unmerged finder and carry-forward. An Automaton can be archived
// while its work branch still holds commits that never reached the base
// branch. ArchivedUnmerged lists those records; CarryForwardArchived starts a
// new draft that reuses the branch so the work is not lost.
//
// Git use is read-only: for-each-ref and the CompareBranchToMain queries.
// Nothing here deletes, merges, moves or otherwise changes a branch, and the
// archived record is never modified.
package autonomous

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Comparison states reported per archived record.
const (
	// ArchivedUnmergedAhead: the branch holds commits not on the base.
	ArchivedUnmergedAhead = "ahead"
	// ArchivedUnmergedUnknown: the comparison could not be made (git error,
	// no base repository); the record is listed so it is not silently lost.
	ArchivedUnmergedUnknown = "unknown"
)

// ArchivedUnmergedEntry is one archived record whose branch still holds work.
type ArchivedUnmergedEntry struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	FormerStatus string    `json:"former_status,omitempty"`
	Branch       string    `json:"branch"`
	BaseBranch   string    `json:"base_branch,omitempty"`
	Ahead        int       `json:"ahead"`
	ArchivedAt   time.Time `json:"archived_at"`
	// State is "ahead" or "unknown".
	State string `json:"state"`
	// Detail is the reason when State is "unknown".
	Detail string `json:"detail,omitempty"`
	// CarriedTo is the id of a draft already carried from this record.
	CarriedTo string `json:"carried_to,omitempty"`
}

// archivedUnmergedGitRunner returns the manager's git runner for this feature.
func (m *Manager) archivedUnmergedGitRunner() MergeGitRunner {
	m.mu.Lock()
	g := m.archivedUnmergedGit
	m.mu.Unlock()
	if g == nil {
		return NewMergeGitRunner()
	}
	return g
}

func (m *Manager) archivedBaseRepo() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg.WorktreeBaseRepo
}

// archivedFormerStatus infers the terminal status an archived record had. The
// record keeps no field for it, so it is read from what the record still says.
func archivedFormerStatus(p *PRD) string {
	for i := len(p.Decisions) - 1; i >= 0; i-- {
		switch p.Decisions[i].Kind {
		case "reject":
			return string(PRDRejected)
		case "cancel":
			return string(PRDCancelled)
		}
	}
	total := 0
	for _, st := range p.Story {
		for _, t := range st.Tasks {
			total++
			if t.Status != TaskCompleted {
				return string(PRDCancelled)
			}
		}
	}
	if total == 0 {
		return ""
	}
	return string(PRDCompleted)
}

func archivedAtOf(p *PRD) time.Time {
	if p.Archive != nil && !p.Archive.ArchivedAt.IsZero() {
		return p.Archive.ArchivedAt
	}
	return p.UpdatedAt
}

// ArchivedUnmerged lists archived records whose branch still holds commits
// that are not on the base branch, newest archive first. Records with no
// branch, a dropped branch, a branch fully on the base or a hand-ported branch
// (its shipped record or file content is on the base) are left out; a
// comparison that cannot be made is listed with state "unknown".
func (m *Manager) ArchivedUnmerged(ctx context.Context) []ArchivedUnmergedEntry {
	git := m.archivedUnmergedGitRunner()
	repo := m.archivedBaseRepo()
	all := m.store.ListPRDs()
	carried := map[string]string{}
	for _, p := range all {
		if p.CarriedFromID != "" {
			carried[p.CarriedFromID] = p.ID
		}
	}
	out := []ArchivedUnmergedEntry{}
	for _, p := range all {
		if p.Status != PRDArchived || p.Git.Branch == "" {
			continue
		}
		e := ArchivedUnmergedEntry{
			ID: p.ID, Title: p.Title, FormerStatus: archivedFormerStatus(p),
			Branch: p.Git.Branch, BaseBranch: p.Git.BaseBranch,
			ArchivedAt: archivedAtOf(p), CarriedTo: carried[p.ID],
		}
		if repo == "" {
			e.State, e.Detail = ArchivedUnmergedUnknown, "no base repository to check the branch"
			out = append(out, e)
			continue
		}
		cctx, cancel := context.WithTimeout(ctx, archiveBranchGitTimeout)
		state, ahead, detail := checkBranchAheadDelivered(cctx, git, repo, p.Git.Branch, p.Git.BaseBranch, p)
		cancel()
		switch state {
		case branchCheckAhead:
			e.State, e.Ahead = ArchivedUnmergedAhead, ahead
		case branchCheckUnknown:
			e.State, e.Detail = ArchivedUnmergedUnknown, detail
		default: // dropped or merged
			continue
		}
		out = append(out, e)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].ArchivedAt.Equal(out[j].ArchivedAt) {
			return out[i].ArchivedAt.After(out[j].ArchivedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// CarryForwardArchived is CarryForwardArchivedForced without force.
func (m *Manager) CarryForwardArchived(id, actor string) (*PRD, error) {
	return m.CarryForwardArchivedForced(id, actor, false)
}

// CarryForwardArchivedForced creates a new draft that carries an archived
// record's title, spec and branch name. The draft is not approved and has run
// nothing. It refuses when the record is not archived, has no branch, or the
// branch is gone, unknown or has no commits ahead, and refuses a second carry
// of the same source unless force is true. The archived record and its branch
// are not changed.
func (m *Manager) CarryForwardArchivedForced(id, actor string, force bool) (*PRD, error) {
	if strings.TrimSpace(actor) == "" {
		return nil, errors.New("carry forward: actor is required")
	}
	src, ok := m.store.GetPRD(id)
	if !ok {
		return nil, fmt.Errorf("carry forward: automaton %q not found", id)
	}
	if src.Status != PRDArchived {
		return nil, fmt.Errorf("carry forward: automaton %q is %s, not archived", id, src.Status)
	}
	if src.Git.Branch == "" {
		return nil, fmt.Errorf("carry forward: automaton %q has no branch to carry", id)
	}
	repo := m.archivedBaseRepo()
	if repo == "" {
		return nil, errors.New("carry forward: no base repository to check the branch")
	}
	if !force {
		for _, p := range m.store.ListPRDs() {
			if p.CarriedFromID == id {
				return nil, fmt.Errorf("carry forward: automaton %q was already carried to %s; pass force to carry again", id, p.ID)
			}
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), archiveBranchGitTimeout)
	defer cancel()
	state, ahead, detail := checkBranchAhead(ctx, m.archivedUnmergedGitRunner(), repo, src.Git.Branch, src.Git.BaseBranch)
	switch state {
	case branchCheckDropped:
		return nil, fmt.Errorf("carry forward: branch %s no longer exists", src.Git.Branch)
	case branchCheckMerged:
		return nil, fmt.Errorf("carry forward: branch %s has no commits ahead of the base", src.Git.Branch)
	case branchCheckUnknown:
		return nil, fmt.Errorf("carry forward: cannot compare branch %s: %s", src.Git.Branch, detail)
	}

	prd, err := m.CreatePRD(src.Spec, src.ProjectDir, src.Backend, src.Model, src.Effort)
	if err != nil {
		return nil, fmt.Errorf("carry forward: %w", err)
	}
	now := m.holdNow()
	m.store.WithLock(func() {
		prd.Title = src.Title
		prd.Project = src.Project
		prd.Git.Branch = src.Git.Branch
		prd.Git.BaseBranch = src.Git.BaseBranch
		prd.CarriedFromID = src.ID
		prd.UpdatedAt = now
		prd.Decisions = append(prd.Decisions, Decision{
			At: now, Kind: "carry_forward", Actor: actor,
			Note: fmt.Sprintf("carried from archived %s: branch %s, %d commits ahead", src.ID, src.Git.Branch, ahead),
		})
	})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	return prd, nil
}
