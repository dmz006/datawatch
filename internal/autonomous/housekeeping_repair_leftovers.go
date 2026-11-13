// Leftover release repairs for the housekeeping framework (group leftovers).
//
// When an Automaton has finished, three things can stay behind: its lease, its
// worktree and its tmux name. The repairs here release each one, but only when
// the safe-to-delete check passes for that Automaton: no uncommitted work, no
// unlanded commits and no live session. When any of those hold, or the check
// cannot be answered, the repair refuses; the framework records it as skipped
// and raises one alert.
//
// Everything a repair can do goes through LeftoverReleaseOps: release one
// lease, remove one worktree, release one tmux name. There is no way to kill,
// end or signal a session, merge, ship, change a model or backend, or touch a
// security workflow.
package autonomous

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Kinds of leftover a finished Automaton can hold.
const (
	LeftoverLease    = "lease"
	LeftoverWorktree = "worktree"
	LeftoverTmuxName = "tmux name"
)

// LeftoverView is one leftover of a finished Automaton as the repair sees it.
type LeftoverView struct {
	// Kind is LeftoverLease, LeftoverWorktree or LeftoverTmuxName.
	Kind string
	// Ref names the leftover: the lease id, the worktree path or the tmux name.
	Ref string
	// Automaton is the title of the finished Automaton that owned it.
	Automaton string
	// Finished is true when the Automaton has reached a final state.
	Finished bool
	// UncommittedWork is true when the worktree holds changes not committed.
	UncommittedWork bool
	// UnlandedCommits is true when commits exist that have not landed.
	UnlandedCommits bool
	// LiveSession is true when a session is still running in it.
	LiveSession bool
	// CheckError is set when the safe-to-delete check could not be answered.
	CheckError string
}

// LeftoverReleaseOps is everything the leftover release repairs may do or read.
type LeftoverReleaseOps interface {
	// Leftovers lists the leases, worktrees and tmux names that belong to
	// Automata and carry the safe-to-delete facts for each.
	Leftovers(ctx context.Context) []LeftoverView
	// Release releases one leftover. It never touches a session.
	Release(ctx context.Context, kind, ref string) error
}

// NewLeftoverReleaseRepairs returns the three release repairs (lease, worktree,
// tmux name) over ops. Nil ops gives none.
func NewLeftoverReleaseRepairs(ops LeftoverReleaseOps) []Repair {
	if ops == nil {
		return nil
	}
	return []Repair{
		&leftoverReleaseRepair{ops: ops, kind: LeftoverLease},
		&leftoverReleaseRepair{ops: ops, kind: LeftoverWorktree},
		&leftoverReleaseRepair{ops: ops, kind: LeftoverTmuxName},
	}
}

type leftoverReleaseRepair struct {
	ops  LeftoverReleaseOps
	kind string
}

func (*leftoverReleaseRepair) Group() string { return RepairGroupLeftovers }

func leftoverSubject(kind, ref string) string { return kind + " " + ref }

// leftoverUnsafeReason returns why the leftover must not be released, or "".
func leftoverUnsafeReason(v LeftoverView) string {
	switch {
	case v.CheckError != "":
		return "the safe-to-delete check could not be answered (" + v.CheckError + ")"
	case v.LiveSession:
		return "a session is still running in it"
	case v.UncommittedWork:
		return "it holds uncommitted work"
	case v.UnlandedCommits:
		return "it holds commits that have not landed"
	}
	return ""
}

func (r *leftoverReleaseRepair) Detect(ctx context.Context) []RepairFinding {
	var out []RepairFinding
	for _, v := range r.ops.Leftovers(ctx) {
		if v.Kind != r.kind || !v.Finished {
			continue
		}
		f := RepairFinding{
			Subject: leftoverSubject(v.Kind, v.Ref),
			Summary: fmt.Sprintf("the %s %s still belongs to the finished Automaton %q", v.Kind, v.Ref, v.Automaton),
		}
		if why := leftoverUnsafeReason(v); why != "" {
			f.Unsafe, f.UnsafeReason = true, why
		}
		out = append(out, f)
	}
	return out
}

func (r *leftoverReleaseRepair) Apply(ctx context.Context, f RepairFinding) (string, error) {
	ref := strings.TrimPrefix(f.Subject, r.kind+" ")
	for _, v := range r.ops.Leftovers(ctx) {
		if v.Kind != r.kind || v.Ref != ref {
			continue
		}
		if !v.Finished {
			return "", errors.New("the Automaton is no longer finished; nothing was released")
		}
		if why := leftoverUnsafeReason(v); why != "" {
			return "", errors.New(why + "; nothing was released")
		}
		if err := r.ops.Release(ctx, v.Kind, v.Ref); err != nil {
			return "", err
		}
		return fmt.Sprintf("released the %s %s of the finished Automaton %q; no session was touched", v.Kind, v.Ref, v.Automaton), nil
	}
	return "", errors.New("the leftover is gone; nothing was released")
}

func (r *leftoverReleaseRepair) UndoHint(RepairFinding) string {
	switch r.kind {
	case LeftoverLease:
		return "the lease can be taken again by starting the Automaton again from the Automata page"
	case LeftoverWorktree:
		return "the worktree held no uncommitted work and no unlanded commits, so it can be recreated by running the Automaton again"
	default:
		return "the tmux name is free to reuse; a new session picks it up the next time it starts"
	}
}
