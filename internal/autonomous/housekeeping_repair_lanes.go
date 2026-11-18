// Lane fill repair for the housekeeping framework (group lane_fill).
//
// When a lane is idle and eligible work waits, the repair starts the next
// eligible Automaton through the existing run path once the grace period has
// passed. It respects capacity, the inflight caps and the usage policy, and it
// never changes a model or backend: the Automaton starts exactly as queued.
//
// The repair refuses, as an unsafe finding (recorded as skipped with one
// alert), when no eligible work exists, when capacity is full, when the lane's
// inflight cap is reached, when the usage guard is paused, or when the only
// waiting candidates need a backend the lane is forbidden to use. Everything it
// can do goes through LaneFillOps: start one Automaton. There is no way to
// merge, ship, end a session, change a model or backend, or touch a security
// workflow.
package autonomous

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// LaneFillCandidateView is one waiting Automaton as the repair sees it.
type LaneFillCandidateView struct {
	ID    string
	Title string
	// Eligible is true when the Automaton may start now (not held, approved,
	// dependencies done), judged by the same rules as the lane fill planner.
	Eligible bool
	// NeedsForbiddenBackend is true when starting it needs a backend this lane
	// may not use. It is never started by the repair.
	NeedsForbiddenBackend bool
}

// LaneFillLaneView is one lane as the repair sees it.
type LaneFillLaneView struct {
	Name string
	// Idle is true when the lane has a free slot.
	Idle bool
	// InflightCapReached is true when the lane is at its inflight cap.
	InflightCapReached bool
	// Candidates are the Automata waiting for this lane, in start order.
	Candidates []LaneFillCandidateView
}

// LaneFillOps is everything the lane fill repair may do or read.
type LaneFillOps interface {
	// Lanes lists every lane with its waiting work.
	Lanes(ctx context.Context) []LaneFillLaneView
	// CapacityFull is true when the pool has no room for another run.
	CapacityFull(ctx context.Context) bool
	// UsagePaused is true when the usage guard has paused starts.
	UsagePaused(ctx context.Context) bool
	// Start starts one waiting Automaton through the existing run path. It
	// changes no model or backend.
	Start(ctx context.Context, automatonID string) error
}

// NewLaneFillRepairs returns the lane fill repair over ops. Nil ops gives none.
func NewLaneFillRepairs(ops LaneFillOps) []Repair {
	if ops == nil {
		return nil
	}
	return []Repair{&laneFillStartRepair{ops: ops}}
}

type laneFillStartRepair struct{ ops LaneFillOps }

func (*laneFillStartRepair) Group() string { return RepairGroupLaneFill }

func laneFillSubject(l LaneFillLaneView) string { return "lane " + l.Name }

// laneFillRefusalReason returns why the lane must not be filled, or "" when
// the first eligible candidate may start.
func laneFillRefusalReason(l LaneFillLaneView, capFull, paused bool) (string, string) {
	if paused {
		return "", "the usage guard is paused, so no new Automaton may start"
	}
	if capFull {
		return "", "capacity is full, so there is no room for another run"
	}
	if l.InflightCapReached {
		return "", "the lane is at its inflight cap"
	}
	eligible, forbidden := 0, 0
	for _, c := range l.Candidates {
		if !c.Eligible {
			continue
		}
		eligible++
		if c.NeedsForbiddenBackend {
			forbidden++
			continue
		}
		return c.ID, ""
	}
	if eligible == 0 {
		return "", "no eligible Automaton is waiting for this lane"
	}
	return "", "the only eligible Automata need a backend this lane may not use"
}

func (r *laneFillStartRepair) Detect(ctx context.Context) []RepairFinding {
	capFull, paused := r.ops.CapacityFull(ctx), r.ops.UsagePaused(ctx)
	var out []RepairFinding
	for _, l := range r.ops.Lanes(ctx) {
		if !l.Idle {
			continue
		}
		id, why := laneFillRefusalReason(l, capFull, paused)
		f := RepairFinding{Subject: laneFillSubject(l)}
		if why != "" {
			f.Summary = fmt.Sprintf("the %s lane is idle and cannot be filled", l.Name)
			f.Unsafe, f.UnsafeReason = true, why
		} else {
			f.Summary = fmt.Sprintf("the %s lane is idle and eligible work waits (next: %s)", l.Name, id)
		}
		out = append(out, f)
	}
	return out
}

func (r *laneFillStartRepair) Apply(ctx context.Context, f RepairFinding) (string, error) {
	name := strings.TrimPrefix(f.Subject, "lane ")
	for _, l := range r.ops.Lanes(ctx) {
		if l.Name != name {
			continue
		}
		if !l.Idle {
			return "", errors.New("the lane is no longer idle; nothing was started")
		}
		id, why := laneFillRefusalReason(l, r.ops.CapacityFull(ctx), r.ops.UsagePaused(ctx))
		if why != "" {
			return "", errors.New(why + "; nothing was started")
		}
		if err := r.ops.Start(ctx, id); err != nil {
			return "", err
		}
		return fmt.Sprintf("started Automaton %s through the normal run path; its model and backend were not changed", id), nil
	}
	return "", errors.New("the lane is gone; nothing was started")
}

func (*laneFillStartRepair) UndoHint(RepairFinding) string {
	return "pause or cancel the started Automaton from the Automata page; nothing else was changed"
}
