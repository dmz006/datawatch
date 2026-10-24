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
var judgeCapacityAdmit func(ctx context.Context, backend, model, unitID, prdID string) (release func(), err error)

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
		if err := led.Acquire(ctx, judgeAdmitRequest(unitID, prdID, pools, node, model, 0), waitDur, func() bool { return false }, func(string) {}); err != nil {
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
			if err := led.Acquire(ctx, judgeAdmitRequest(unitID, prdID, pools, node, model, 0), 150*time.Millisecond, func() bool { return false }, func(string) {}); err != nil {
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
