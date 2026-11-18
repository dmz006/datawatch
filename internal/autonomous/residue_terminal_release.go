// Residue release at a terminal transition.
//
// When an Automaton is merged, cancelled or archived, its work tree, its
// task work trees and its local branch are residue. ResidualReleasePass
// removes them as soon as nothing unmerged or hand-written would be lost,
// and records what it removed and why it held anything back:
//
//   - held_unmerged: commits on the branch are not all on the base branch
//     (or that could not be established), so the branch and its work tree
//     stay; a task branch not yet on the Automaton's branch or the base
//     branch holds its task work tree the same way;
//   - held_hand_written: the work tree has uncommitted files that are not
//     the daemon's own sections and not generated artefacts
//     (generated_artefact_exempt.go);
//   - held_in_use: a live session or lease is using the work tree;
//   - held_primary: the entry is the main checkout, never touched;
//   - held_error: git refused the removal; the refusal is recorded.
//
// Removal is never forced: generated artefacts are deleted first, then
// `git worktree remove` and `git branch -d` run without force, so git
// itself refuses anything the checks above missed.
package autonomous

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// Terminal transitions that call the pass, and the periodic cleanup pass.
const (
	ResidueTriggerMerged    = "merged"
	ResidueTriggerCancelled = "cancelled"
	ResidueTriggerArchived  = "archived"
	ResidueTriggerCleanup   = "cleanup_pass"
)

// Hold reasons recorded on an entry the pass left in place.
const (
	ResidueHeldUnmerged    = "held_unmerged"
	ResidueHeldHandWritten = "held_hand_written"
	ResidueHeldInUse       = "held_in_use"
	ResidueHeldPrimary     = "held_primary"
	ResidueHeldError       = "held_error"
)

// Entry kinds.
const (
	ResidueKindWorktree     = "worktree"
	ResidueKindTaskWorktree = "task_worktree"
	ResidueKindBranch       = "branch"
)

// residueAuditAction is the audit event recorded for every removal.
const residueAuditAction = "autonomous_residue_release"

// ResidualTaskTree names one task work tree of the Automaton.
type ResidualTaskTree struct {
	Path   string
	Branch string
}

// ResidualReleaseOptions is one ResidualReleasePass call.
type ResidualReleaseOptions struct {
	Ctx     context.Context
	Repo    string // base repository, home of the branches
	Base    string // base branch the Automaton's work must be on
	Trigger string // one of the ResidueTrigger* constants
	// Branch is the Automaton's local branch; Worktree its work tree ("" when
	// it has none).
	Branch   string
	Worktree string
	// TaskTrees are the Automaton's task work trees.
	TaskTrees []ResidualTaskTree
	// ExtraReports names gate report paths beyond the default one.
	ExtraReports []string
	// DryRun records what would be removed and removes nothing.
	DryRun bool
	Git    MergeGitRunner
	Now    func() time.Time
	// InUse reports whether a live session or lease uses the path; nil = no.
	InUse func(path string) bool
	// Audit and Record are optional sinks; a Record error never un-removes.
	Audit  WorktreeCleanupAudit
	Record func(branch, text string) error
}

// ResidualReleaseEntry is one removed or held entry.
type ResidualReleaseEntry struct {
	Kind   string `json:"kind"`
	Path   string `json:"path,omitempty"`
	Branch string `json:"branch,omitempty"`
	// Reason is a ResidueHeld* constant for a held entry, a short fact for
	// a removed one.
	Reason string `json:"reason"`
	Detail string `json:"detail,omitempty"`
	// Artefacts lists the generated artefacts deleted with the work tree.
	Artefacts []string `json:"artefacts,omitempty"`
	// BytesFreed is the work tree's size measured before removal.
	BytesFreed int64 `json:"bytes_freed,omitempty"`
}

// ResidualReleaseResult is the outcome of one pass.
type ResidualReleaseResult struct {
	Trigger string
	DryRun  bool
	Removed []ResidualReleaseEntry
	Held    []ResidualReleaseEntry
}

// ResidualReleasePass releases the residue of one finished Automaton. See
// the file comment. It never forces a removal.
func ResidualReleasePass(opts ResidualReleaseOptions) ResidualReleaseResult {
	res := ResidualReleaseResult{Trigger: opts.Trigger, DryRun: opts.DryRun}
	if opts.Git == nil || opts.Repo == "" || opts.Branch == "" {
		return res
	}
	ctx := opts.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	now := opts.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	primary := ""
	primaryKnown := false
	if out, err := opts.Git.Run(ctx, opts.Repo, "worktree", "list", "--porcelain"); err == nil {
		primary, primaryKnown = worktreePrimaryPath(out), true
	}
	var wtBytes int64
	note := func(e ResidualReleaseEntry, removed bool) {
		if removed {
			res.Removed = append(res.Removed, e)
		} else {
			res.Held = append(res.Held, e)
		}
		if opts.DryRun {
			return
		}
		verb := "held (" + e.Reason + ")"
		if removed {
			verb = "removed"
		}
		text := fmt.Sprintf("residue release (%s): %s %s %s", opts.Trigger, e.Kind, firstNonEmpty(e.Path, e.Branch), verb)
		if e.Detail != "" {
			text += ": " + e.Detail
		}
		if opts.Record != nil {
			if err := opts.Record(opts.Branch, text); err != nil {
				log.Printf("[autonomous] residue-release: recording for %s: %v", opts.Branch, err)
			}
		}
		if removed && opts.Audit != nil {
			action := residueAuditAction
			details := map[string]any{
				"trigger": opts.Trigger, "kind": e.Kind, "path": e.Path, "branch": e.Branch,
				"reason": e.Reason, "artefacts": e.Artefacts, "at": now().Format(time.RFC3339),
			}
			if e.Kind == ResidueKindBranch {
				// The branch removal is the same event the cleanup pass audits
				// for a released work branch, so one audit trail covers both.
				action = worktreeCleanupAuditAction
				details["worktree"] = opts.Worktree
				details["bytes_freed"] = wtBytes
				details["base_branch"] = opts.Base
			}
			_ = opts.Audit(action, details)
		}
	}

	// Task work trees: each is judged alone.
	for _, tt := range opts.TaskTrees {
		if tt.Path == "" {
			continue
		}
		e := ResidualReleaseEntry{Kind: ResidueKindTaskWorktree, Path: tt.Path, Branch: tt.Branch}
		if _, err := os.Stat(tt.Path); err != nil {
			continue // already gone
		}
		if tt.Branch != "" && !residueOnBranch(ctx, opts.Git, opts.Repo, tt.Branch, opts.Branch) && !residueOnBranch(ctx, opts.Git, opts.Repo, tt.Branch, opts.Base) {
			e.Reason, e.Detail = ResidueHeldUnmerged, "the task branch has commits that are on neither the Automaton's branch nor the base branch"
			note(e, false)
			continue
		}
		if ok, entry := residueReleaseTree(ctx, opts, e, primary, primaryKnown); ok {
			note(entry, true)
		} else {
			note(entry, false)
		}
	}

	// The Automaton's own branch and work tree.
	be := ResidualReleaseEntry{Kind: ResidueKindBranch, Branch: opts.Branch}
	if _, err := opts.Git.Run(ctx, opts.Repo, "rev-parse", "--verify", "--quiet", "refs/heads/"+opts.Branch); err != nil && opts.Worktree == "" {
		return res // the branch is already gone
	}
	if opts.Base == "" {
		be.Reason, be.Detail = ResidueHeldUnmerged, "the base branch could not be resolved, so the branch's commits cannot be verified"
		note(be, false)
		return res
	}
	cmp := CompareBranchToMain(ctx, opts.Git, opts.Repo, opts.Branch, opts.Base)
	if cmp.State != MergeStateMerged && cmp.State != MergeStateEmpty {
		be.Reason = ResidueHeldUnmerged
		be.Detail = fmt.Sprintf("commits on the branch are not all on %q (state %q)", opts.Base, cmp.State)
		if cmp.Error != "" {
			be.Detail += ": " + cmp.Error
		}
		note(be, false)
		return res
	}
	if opts.Worktree != "" {
		if _, err := os.Stat(opts.Worktree); err == nil {
			we := ResidualReleaseEntry{Kind: ResidueKindWorktree, Path: opts.Worktree, Branch: opts.Branch}
			ok, entry := residueReleaseTree(ctx, opts, we, primary, primaryKnown)
			if ok {
				wtBytes = entry.BytesFreed
			}
			note(entry, ok)
			if !ok {
				return res // the branch is checked out there; it stays too
			}
		}
	}
	if opts.DryRun {
		be.Reason = "all commits on " + opts.Base
		note(be, true)
		return res
	}
	if out, err := opts.Git.Run(ctx, opts.Repo, "branch", "-d", opts.Branch); err != nil { // never -D
		if _, verr := opts.Git.Run(ctx, opts.Repo, "rev-parse", "--verify", "--quiet", "refs/heads/"+opts.Branch); verr == nil {
			be.Reason, be.Detail = ResidueHeldError, fmt.Sprintf("git branch -d: %v: %s", err, strings.TrimSpace(out))
			note(be, false)
		}
		return res
	}
	be.Reason = "all commits on " + opts.Base
	note(be, true)
	return res
}

// residueReleaseTree checks one work tree for hand-written files and in-use
// state and removes it. The returned entry carries the outcome.
func residueReleaseTree(ctx context.Context, opts ResidualReleaseOptions, e ResidualReleaseEntry, primary string, primaryKnown bool) (bool, ResidualReleaseEntry) {
	if !primaryKnown || sameDirectory(e.Path, primary) {
		e.Reason, e.Detail = ResidueHeldPrimary, "the main checkout is never touched (or could not be ruled out)"
		return false, e
	}
	if opts.InUse != nil && opts.InUse(e.Path) {
		e.Reason, e.Detail = ResidueHeldInUse, "a live session or lease is using the work tree"
		return false, e
	}
	status, err := opts.Git.Run(ctx, e.Path, "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		e.Reason, e.Detail = ResidueHeldError, fmt.Sprintf("git status: %v: %s", err, strings.TrimSpace(status))
		return false, e
	}
	status = filterDaemonOnlyPorcelain(ctx, e.Path, status, func(ctx context.Context, dir string, args ...string) (string, error) {
		return opts.Git.Run(ctx, dir, args...)
	})
	generated, remaining := SplitGeneratedArtefacts(status, opts.ExtraReports)
	if len(remaining) > 0 {
		e.Reason, e.Detail = ResidueHeldHandWritten, "uncommitted files: "+firstLine(strings.Join(remaining, "\n"))
		return false, e
	}
	e.Artefacts = generated
	if opts.DryRun {
		e.Reason = "nothing unmerged or hand-written"
		return true, e
	}
	e.BytesFreed, _ = dirSize(e.Path)
	e.Artefacts = RemoveGeneratedArtefacts(e.Path, generated)
	if out, rerr := opts.Git.Run(ctx, opts.Repo, "worktree", "remove", e.Path); rerr != nil { // never --force
		e.Reason, e.Detail = ResidueHeldError, fmt.Sprintf("git worktree remove: %v: %s", rerr, strings.TrimSpace(out))
		return false, e
	}
	e.Reason = "nothing unmerged or hand-written"
	if e.Kind == ResidueKindTaskWorktree && e.Branch != "" {
		_, _ = opts.Git.Run(ctx, opts.Repo, "branch", "-d", e.Branch) // best effort, never -D
	}
	return true, e
}

// residueOnBranch reports whether every commit of branch is on target.
func residueOnBranch(ctx context.Context, git MergeGitRunner, repo, branch, target string) bool {
	if branch == "" || target == "" {
		return false
	}
	_, err := git.Run(ctx, repo, "merge-base", "--is-ancestor", branch, target)
	return err == nil
}

// worktreeForBranch returns the work tree path a `git worktree list
// --porcelain` listing has the branch checked out in, or "".
func worktreeForBranch(out, branch string) string {
	cur := ""
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		switch {
		case strings.HasPrefix(line, "worktree "):
			cur = strings.TrimPrefix(line, "worktree ")
		case line == "branch refs/heads/"+branch:
			return cur
		}
	}
	return ""
}

// ---------------------------------------------------------------------
// Manager wiring
// ---------------------------------------------------------------------

// ReleaseResidueAtTerminal runs ResidualReleasePass for one merged,
// cancelled or archived Automaton, honoring the cleanup mode (off does
// nothing and does not call git; report records what would be removed). The
// trigger names the transition. Task fields of removed task work trees are
// cleared. It returns the result; held task work trees are reported so the
// caller leaves them in place.
func (m *Manager) ReleaseResidueAtTerminal(prd *PRD, trigger string) ResidualReleaseResult {
	if prd == nil || prd.Git.Branch == "" {
		return ResidualReleaseResult{Trigger: trigger}
	}
	m.mu.Lock()
	repo := m.cfg.WorktreeBaseRepo
	mode := m.cfg.Automation.EffectiveCleanupMode()
	m.mu.Unlock()
	if mode == config.CleanupModeOff || repo == "" {
		return ResidualReleaseResult{Trigger: trigger}
	}
	ctx, cancel := context.WithTimeout(context.Background(), worktreeReleaseGitTimeout)
	defer cancel()
	git := NewMergeGitRunner()
	probe := &managerWorktreeProbe{m: m, repo: repo, git: git, ctx: ctx}
	base := prd.Git.BaseBranch
	if base == "" {
		base = probe.defaultBase()
	}
	var wt string
	if out, err := git.Run(ctx, repo, "worktree", "list", "--porcelain"); err == nil {
		wt = worktreeForBranch(out, prd.Git.Branch)
	}
	var trees []ResidualTaskTree
	var reports []string
	byPath := map[string]*Task{}
	m.readLocked(func() {
		for si := range prd.Story {
			for ti := range prd.Story[si].Tasks {
				t := &prd.Story[si].Tasks[ti]
				if t.ReportPath != "" {
					reports = append(reports, t.ReportPath)
				}
				if t.WorktreePath != "" {
					trees = append(trees, ResidualTaskTree{Path: t.WorktreePath, Branch: t.WorktreeBranch})
					byPath[t.WorktreePath] = t
				}
			}
		}
	})
	res := ResidualReleasePass(ResidualReleaseOptions{
		Ctx: ctx, Repo: repo, Base: base, Trigger: trigger,
		Branch: prd.Git.Branch, Worktree: wt, TaskTrees: trees, ExtraReports: reports,
		DryRun: mode == config.CleanupModeReport, Git: git,
		InUse: func(p string) bool {
			inUse, _ := probe.WorktreeInUse(prd.Git.Branch, p)
			return inUse
		},
		Audit:  m.worktreeCleanupAudit(),
		Record: m.recordWorktreeRemoval,
	})
	if !res.DryRun {
		for _, e := range res.Removed {
			if t := byPath[e.Path]; t != nil && e.Kind == ResidueKindTaskWorktree {
				m.store.WithLock(func() { t.WorktreePath, t.WorktreeBranch = "", "" })
				_ = m.store.SaveTask(t)
			}
		}
	}
	log.Printf("[autonomous] residue-release %s %s: mode=%q removed=%d held=%d", trigger, prd.ID, mode, len(res.Removed), len(res.Held))
	return res
}

// residueHeldTaskTrees reports whether the pass left a task work tree in place.
func residueHeldTaskTrees(r ResidualReleaseResult) bool {
	for _, h := range r.Held {
		if h.Kind == ResidueKindTaskWorktree {
			return true
		}
	}
	return false
}

// releaseResidueOfFinished runs the pass over every finished Automaton in the
// store (completed, cancelled or archived) with a branch, stopping when
// maxRemovals entries were removed and honoring the retention window.
// Called from the periodic cleanup pass.
func (m *Manager) releaseResidueOfFinished(maxRemovals int, retention time.Duration) (removed []WorktreeCleanupRemoval, held int) {
	if m.store == nil {
		return nil, 0
	}
	count := 0
	for _, prd := range m.store.ListPRDs() {
		if prd == nil || prd.Git.Branch == "" {
			continue
		}
		var trigger string
		switch prd.Status {
		case PRDCompleted:
			trigger = ResidueTriggerMerged
		case PRDCancelled:
			trigger = ResidueTriggerCancelled
		case PRDArchived:
			trigger = ResidueTriggerArchived
		default:
			continue
		}
		if maxRemovals > 0 && count >= maxRemovals {
			break
		}
		// The periodic pass keeps the retention window: a freshly finished
		// entry is left to the owner's own end-path release.
		if prd.UpdatedAt.IsZero() || time.Since(prd.UpdatedAt) < retention {
			continue
		}
		r := m.ReleaseResidueAtTerminal(prd, trigger)
		held += len(r.Held)
		if r.DryRun {
			continue
		}
		for _, e := range r.Removed {
			count++
			removed = append(removed, WorktreeCleanupRemoval{Branch: firstNonEmpty(e.Branch, prd.Git.Branch), Worktree: e.Path, Reason: e.Kind + ": " + e.Reason})
		}
	}
	return removed, held
}
