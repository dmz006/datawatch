package main

import (
	"context"
	"errors"
	"time"
)

// retargetTick is how often a waiting request re-runs its routing policy. A
// var so tests shorten it; it is deliberately not a config setting.
var retargetTick = 5 * time.Second

// retargetCooldownTicks is how many ticks a target that looked free but lost
// the race on Take is left alone, so a flaky target cannot cause a requeue on
// every tick.
const retargetCooldownTicks = 3

// retargetWaitIn carries the request and every collaborator retargetWait
// needs, as functions, so the mechanism needs no ledger and no daemon.
type retargetWaitIn struct {
	Backend, Model string
	// Left lists backends the request already left on an earlier call. Callers
	// that loop (the judge path) pass it so the policy and the helper never
	// send the request back to one of them.
	Left []string
	// Wait blocks until the lease on backend is granted or ctx ends (the
	// existing blocking admit).
	Wait func(ctx context.Context, backend, model string) (release func(), node string, err error)
	// Next re-runs the routing policy for a request currently waiting on
	// `waiting`. left lists backends the waiter already left. ok is true only
	// when the policy picks a backend other than waiting that has a free slot
	// right now. A nil Next means the request is pinned: Wait is called once
	// and nothing else happens.
	Next func(waiting string, left []string) (backend, model, reason string, ok bool)
	// Take tries to get the lease on the target without waiting beyond a short
	// probe. A nil Take means move-only: no lease is taken, the caller admits
	// on the target itself.
	Take func(backend, model string) (release func(), node string, ok bool)
	// Changed returns the capacity change channel (may be nil).
	Changed func() <-chan struct{}
	// OnMove is told about every move, after the target is settled.
	OnMove func(from, to, model, reason string)
}

// retargetWaitOut says where the request ended up.
type retargetWaitOut struct {
	Release func()
	Node    string
	// Backend and Model are where the request ended up (the target when Moved).
	Backend, Model string
	Moved          bool
}

// retargetWaitResult is what one Wait goroutine reports.
type retargetWaitResult struct {
	release func()
	node    string
	err     error
}

// retargetWait waits for a lease on in.Backend while re-running the routing
// policy on every retargetTick and every capacity change. When the policy
// names a backend with a free slot it stops waiting and takes the lease there
// (or, move-only, reports the move and lets the caller admit). A grant that
// lands at the same instant as a target wins: no move. A failed Take resumes
// the original wait and leaves that target alone for a few ticks.
//
// Next, Take, OnMove and Wait's result handling all run on the caller's
// goroutine with no lock held; only Wait runs on its own goroutine, which is
// joined on every return path.
func retargetWait(ctx context.Context, in retargetWaitIn) (retargetWaitOut, error) {
	out := retargetWaitOut{Backend: in.Backend, Model: in.Model}
	if in.Next == nil {
		release, node, err := in.Wait(ctx, in.Backend, in.Model)
		if err != nil {
			return out, err
		}
		out.Release, out.Node = release, node
		return out, nil
	}

	var (
		cancelWait context.CancelFunc
		waitCh     chan retargetWaitResult
	)
	startWait := func() {
		var wctx context.Context
		wctx, cancelWait = context.WithCancel(ctx)
		ch := make(chan retargetWaitResult, 1)
		waitCh = ch
		go func() {
			release, node, err := in.Wait(wctx, in.Backend, in.Model)
			ch <- retargetWaitResult{release: release, node: node, err: err}
		}()
	}
	// stopWait cancels the running Wait and returns what it reported.
	stopWait := func() retargetWaitResult {
		cancelWait()
		return <-waitCh
	}
	startWait()
	defer func() { cancelWait() }()

	ticker := time.NewTicker(retargetTick)
	defer ticker.Stop()

	var changed <-chan struct{}
	if in.Changed != nil {
		changed = in.Changed()
	}

	left := append([]string(nil), in.Left...)
	coolUntil := map[string]int{}
	tickN := 0

	// evaluate asks the policy once. done is true when retargetWait should
	// return with res and err.
	evaluate := func() (res retargetWaitOut, done bool, err error) {
		backend, model, reason, ok := in.Next(in.Backend, append([]string(nil), left...))
		if !ok || backend == "" || backend == in.Backend || retargetContains(left, backend) {
			return res, false, nil
		}
		if until, cooling := coolUntil[backend]; cooling && tickN <= until {
			return res, false, nil
		}
		r := stopWait()
		if ctx.Err() != nil {
			if r.err == nil && r.release != nil {
				r.release()
			}
			return res, true, ctx.Err()
		}
		if r.err == nil {
			// Granted at the same instant a target appeared: keep the grant.
			out.Release, out.Node = r.release, r.node
			return out, true, nil
		}
		if !errors.Is(r.err, context.Canceled) {
			return res, true, r.err
		}
		if in.Take == nil {
			if in.OnMove != nil {
				in.OnMove(in.Backend, backend, model, reason)
			}
			out.Backend, out.Model, out.Moved = backend, model, true
			return out, true, nil
		}
		release, node, took := in.Take(backend, model)
		if took {
			if in.OnMove != nil {
				in.OnMove(in.Backend, backend, model, reason)
			}
			out.Release, out.Node = release, node
			out.Backend, out.Model, out.Moved = backend, model, true
			return out, true, nil
		}
		coolUntil[backend] = tickN + retargetCooldownTicks
		startWait()
		return res, false, nil
	}

	for {
		select {
		case <-ctx.Done():
			r := stopWait()
			if r.err == nil && r.release != nil {
				r.release()
			}
			return out, ctx.Err()
		case r := <-waitCh:
			if r.err != nil {
				return out, r.err
			}
			out.Release, out.Node = r.release, r.node
			return out, nil
		case <-ticker.C:
			tickN++
		case <-changed:
			changed = in.Changed()
		}
		res, done, err := evaluate()
		if done {
			return res, err
		}
	}
}

func retargetContains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
