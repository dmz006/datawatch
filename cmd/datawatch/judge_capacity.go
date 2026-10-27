package main

import (
	"context"
	"strings"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/capacity"
)

// judgeSessionNamePrefix is the sessionName prefix of the guardrail judge
// one-shot session ("autonomous:guardrail:<unit id>").
const judgeSessionNamePrefix = "autonomous:guardrail"

// judgeCapacityAdmit lets the guardrail judge's own call (main.go's
// autonomousGuardrail closure — defined before amgr exists and so unable to
// close over it directly, mirroring the verifierCapacityAdmit package-level
// pattern) acquire the node:/llm: pools a session on that backend+model
// would use, under the "judge:<unit id>" holder, on both the one-shot
// session path and the /api/ask path. Set once at daemon startup by
// wireCapacity; nil until then, so callers must check. prdID is required so
// the lease/wait entry is attributed to the unit's PRD for fairness and for
// the capacity card.
//
// The second result is the compute node the judge's lease holds, "" for an
// entry with fewer than two candidate nodes; the judgment starts on it.
var judgeCapacityAdmit func(ctx context.Context, backend, model, unitID, prdID string) (release func(), node string, err error)

// judgeSessionStarted is called by the one-shot runner as soon as a session
// is started, with its name and id. wireCapacity points it at the judge
// registry so the judge's lease follows the judge session it spawned
// (lease binding + reaping); names that are not guardrail judge sessions
// are ignored.
var judgeSessionStarted func(sessionName, sessionID string)

// judgeAdmitRequest builds the judge's capacity.Request: Role
// capacity.RoleVerifier (documented as "a verifier or guardrail session")
// ranks it for dispatch, and the unit's PRD is the fairness group.
func judgeAdmitRequest(unitID, prdID string, pools []string, node, model string, priority int) capacity.Request {
	return capacity.Request{
		Holder:   autonomouspkg.JudgeHolder(unitID),
		PRDID:    prdID,
		Pools:    pools,
		Node:     node,
		Model:    model,
		Role:     capacity.RoleVerifier,
		Priority: priority,
	}
}

// judgeAdmitRefreshed is judgeAdmitRequest with Refresh set: each ledger
// dispatch pass re-resolves the pools and node through keysFn, so a node learned
// while the judge waits joins its placement.
func judgeAdmitRefreshed(keysFn func(backend, model string) ([]string, string), backend, unitID, prdID string, pools []string, node, model string) capacity.Request {
	req := judgeAdmitRequest(unitID, prdID, pools, node, model, 0)
	req.Refresh = func() capacity.Request {
		cur := req
		if p, n := keysFn(backend, model); len(p) > 0 {
			cur.Pools, cur.Node = p, n
		}
		return cur
	}
	return req
}

// judgeAdmitFailureVerdict turns an admission failure (wait expired or
// context cancelled) into the "infra" verdict the other pre-spawn failures
// use: resource contention, not a content judgment — the judge never ran —
// and so runGuardrailWithInfraRetry handles it through the same path as a
// failed session or ask call.
func judgeAdmitFailureVerdict(err error) autonomouspkg.GuardrailVerdict {
	return autonomouspkg.GuardrailVerdict{
		Outcome:   "block",
		Severity:  "medium",
		Summary:   "guardrail(capacity): " + err.Error(),
		VerdictAt: time.Now(),
		Category:  "infra",
	}
}

// judgeLeaseAdmit is judgeCapacityAdmit's shape, named so runJudgeLeased can
// be driven by a fake in tests.
type judgeLeaseAdmit func(ctx context.Context, backend, model, unitID, prdID string) (release func(), node string, err error)

// runJudgeLeased runs one judge body under the judge's capacity lease. The
// lease is taken before body runs, so the wait for a slot happens before the
// judge's time limit starts (the session or ask call applies that limit once it
// starts). The release is deferred, so the lease is released on every exit
// path: success, error, cancellation and panic. A lease whose wait expired is an
// infra verdict, body never runs, and att.NoSlot is set so the judge moves on to
// the next backend. A judge cancelled while it waited is not a slot shortage: it
// returns the context error with no NoSlot, so no further backend is tried and
// no hold is raised. The Claude session window counts the pick only once its
// lease is granted. A nil admit (no ledger, e.g. in tests) runs body with no
// lease and no node.
//
// A judge whose attempt carries a Retarget does not block on one backend: the
// wait re-evaluates the routing policy and may leave for a backend with room
// (see judgeAwaitLease). It then returns an infra verdict with att.MoveTo set,
// holding no lease and setting no NoSlot, so it is not a slot shortage.
func runJudgeLeased(ctx context.Context, admit judgeLeaseAdmit, att *judgeAttempt, model, unitID, prdID string, body func(node string) (autonomouspkg.GuardrailVerdict, error)) (autonomouspkg.GuardrailVerdict, error) {
	node := ""
	if admit != nil {
		release, n, err := judgeAwaitLease(ctx, admit, att, model, unitID, prdID)
		if err != nil {
			if ctx.Err() != nil {
				return judgeAdmitFailureVerdict(err), ctx.Err()
			}
			att.NoSlot = true
			return judgeAdmitFailureVerdict(err), nil
		}
		if att.MoveTo != "" {
			// The wait left this backend for att.MoveTo. No lease is held and the
			// judge did not run; runJudgeAttempts runs it on the target.
			return judgeMovedVerdict(att), nil
		}
		defer release()
		node = n
	}
	judgeRecordClaudeSession(att.Backend)
	return body(node)
}

// judgeAwaitLease waits for the judge's lease on att.Backend. With att.Retarget
// set, the wait re-runs the routing policy on every retargetTick and every
// capacity change and, when the policy names another backend with room, gives
// up its place in the queue and records the target in att.MoveTo (move-only:
// the lease on the target is taken by the next attempt, not here). A wait with
// no Retarget is the blocking admit alone.
func judgeAwaitLease(ctx context.Context, admit judgeLeaseAdmit, att *judgeAttempt, model, unitID, prdID string) (func(), string, error) {
	if att.Retarget == nil {
		return admit(ctx, att.Backend, model, unitID, prdID)
	}
	out, err := retargetWait(ctx, retargetWaitIn{
		Backend: att.Backend, Model: model,
		Wait: func(ctx context.Context, backend, model string) (func(), string, error) {
			return admit(ctx, backend, model, unitID, prdID)
		},
		Next:    att.Retarget,
		Changed: judgeCapacityChanged,
		OnMove: func(_, to, toModel, reason string) {
			att.MoveTo, att.MoveModel, att.MoveReason = to, toModel, reason
		},
	})
	if err != nil {
		return func() {}, "", err
	}
	if out.Moved && out.Release != nil {
		// Move-only takes no lease; let go of one if a wait ever handed it over.
		out.Release()
		out.Release = nil
	}
	if out.Release == nil {
		out.Release = func() {}
	}
	return out.Release, out.Node, nil
}

// judgeMovedVerdict is the verdict of a wait that moved to another backend. It
// is "infra": the judge did not run on the backend it left, and runJudgeAttempts
// drops it and runs the target.
func judgeMovedVerdict(att *judgeAttempt) autonomouspkg.GuardrailVerdict {
	return autonomouspkg.GuardrailVerdict{
		Outcome:   "block",
		Severity:  "medium",
		Summary:   "guardrail(capacity): moved from " + att.Backend + " to " + att.MoveTo + ": " + att.MoveReason,
		VerdictAt: time.Now(),
		Category:  "infra",
	}
}

// judgeUnitFromSessionName extracts the unit id from a
// "autonomous:guardrail:<unit id>" session name; ok is false for any other
// name (including the bare prefix).
func judgeUnitFromSessionName(name string) (string, bool) {
	p := judgeSessionNamePrefix + ":"
	if !strings.HasPrefix(name, p) || len(name) == len(p) {
		return "", false
	}
	return name[len(p):], true
}

// judgeAdmitters builds the judge admission closures over a ledger, the
// judges' registry and a pool-keys function. The judge reuses the verifier
// registry type — same "active while admitted, session-bound" lifetime and
// the reaper treats it identically — but gets its OWN registry instance:
// verifier and judge holders are disjoint and must never see each
// other's state. wireCapacity installs these on the package-level vars;
// tests drive them directly. The release function is idempotent: it clears
// the registry entry and calls led.Release, which is itself safe on
// absence.
func judgeAdmitters(led *capacity.Ledger, reg *verifierRegistry, keysFn func(backend, model string) ([]string, string), waitDur time.Duration) (
	admit func(ctx context.Context, backend, model, unitID, prdID string) (release func(), err error),
	try func(ctx context.Context, backend, model, unitID, prdID string) (release func(), ok bool),
	started func(sessionName, sessionID string),
) {
	admit = func(ctx context.Context, backend, model, unitID, prdID string) (func(), error) {
		holder := autonomouspkg.JudgeHolder(unitID)
		pools, node := keysFn(backend, model)
		if len(pools) == 0 {
			// No pools for this backend: nothing to admit against.
			return func() {}, nil
		}
		// The ledger grants all-or-nothing across the pool list, so the
		// backend pool and the node pool are held atomically.
		if err := led.Acquire(ctx, judgeAdmitRefreshed(keysFn, backend, unitID, prdID, pools, node, model), waitDur, func() bool { return false }, func(string) {}); err != nil {
			return func() {}, err
		}
		end := reg.begin(holder)
		return func() { end(); led.Release(holder) }, nil
	}
	try = func(ctx context.Context, backend, model, unitID, prdID string) (func(), bool) {
		holder := autonomouspkg.JudgeHolder(unitID)
		if pools, node := keysFn(backend, model); len(pools) > 0 {
			// A short, real Acquire (not a snapshot check): the fast path
			// grants immediately when there is room, and a busy candidate
			// is given only the probe window before the caller moves on.
			if err := led.Acquire(ctx, judgeAdmitRefreshed(keysFn, backend, unitID, prdID, pools, node, model), 150*time.Millisecond, func() bool { return false }, func(string) {}); err != nil {
				return func() {}, false
			}
			end := reg.begin(holder)
			return func() { end(); led.Release(holder) }, true
		}
		return func() {}, true
	}
	started = func(sessionName, sessionID string) {
		if unitID, ok := judgeUnitFromSessionName(sessionName); ok {
			holder := autonomouspkg.JudgeHolder(unitID)
			reg.bindSession(holder, sessionID)
			led.Bind(holder, sessionID)
		}
	}
	return
}

// judgeNodeAdmitter is the production judge admission: the same
// "judge:<unit id>" holder and registry lifetime as judgeAdmitters' admit, but
// taken through Manager.AdmitJudge so an entry that pools two or more nodes
// places the judge on one of them (and loads the model there). The returned
// node is the one the judge's lease holds, "" for a one-node entry.
func judgeNodeAdmitter(amgr *autonomouspkg.Manager, reg *verifierRegistry) func(ctx context.Context, backend, model, unitID, prdID string) (func(), string, error) {
	return func(ctx context.Context, backend, model, unitID, prdID string) (func(), string, error) {
		holder := autonomouspkg.JudgeHolder(unitID)
		node, release, err := amgr.AdmitJudge(ctx, autonomouspkg.JudgeAdmit{
			Holder: holder, PRDID: prdID, Backend: backend, Model: model,
			Wait: amgr.CapacityWaitDuration(),
		})
		if err != nil {
			return func() {}, "", err
		}
		end := reg.begin(holder)
		return func() { end(); release() }, node, nil
	}
}
