package main

import (
	"context"
	"log"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// judgeClaudeWindow counts the Claude judge backends the routing policy has
// picked, for its per-window session cap. A judge is a distinct role, so its
// count is kept apart from the verifier's.
var judgeClaudeWindow = autonomouspkg.NewVerifierPolicyWindow(nil)

// judgeCapacityTryAdmit is the judge's non-blocking admission probe, set by
// wireCapacity. It reports whether a judge on backend+model would be admitted
// now, through the same judge lease code as judgeCapacityAdmit, and holds no
// lease afterwards: the attempt that follows admits for real. nil (no ledger,
// e.g. in tests) means every backend is admitted.
var judgeCapacityTryAdmit func(ctx context.Context, backend, model, unitID, prdID string) bool

// judgeRouteLoad is the autonomouspkg.VerifierLoad the judge chooser reads. The
// probe decides admission; the capacity ledger supplies the relative load.
type judgeRouteLoad struct {
	admit   func(backend string) bool // nil: every backend admits
	results map[string]bool
}

// TryAdmit probes a backend once per judge call and remembers the answer.
func (l *judgeRouteLoad) TryAdmit(backend string) bool {
	if l.admit == nil {
		return true
	}
	if ok, seen := l.results[backend]; seen {
		return ok
	}
	ok := l.admit(backend)
	l.results[backend] = ok
	return ok
}

// Free reports -1 (unlimited): the admission probe decides, not a slot count.
func (l *judgeRouteLoad) Free(string) int { return -1 }

// RelativeLoad reads the backend's load from the capacity ledger, 0 when none
// is wired.
func (l *judgeRouteLoad) RelativeLoad(backend string) float64 {
	if verifierBackendLoad == nil {
		return 0
	}
	return verifierBackendLoad(backend, "")
}

// excludeBackends returns list without the backends already tried.
func excludeBackends(list, tried []string) []string {
	out := make([]string, 0, len(list))
	for _, b := range list {
		if !containsString(tried, b) {
			out = append(out, b)
		}
	}
	return out
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// judgePickFn writes one judge backend pick to the decision log: the backend,
// the model it runs on ("" for the backend's default) and why it was picked.
type judgePickFn func(backend, model, reason string)

// judgeDecisionLog returns the judgePickFn for one judge call. Each pick is
// written to the PRD's decision log tagged with the judge role, through the
// same store as the verifier's picks. A nil manager (tests) or a failed write
// is logged, never fatal: the judge still runs on the backend it picked.
func judgeDecisionLog(mgr *autonomouspkg.Manager, req autonomouspkg.GuardrailInvocation) judgePickFn {
	return func(backend, model, reason string) {
		log.Printf("[autonomous] judge: policy: %s (prd %s, unit %s)", reason, req.PRDID, req.UnitID)
		if mgr == nil {
			return
		}
		if err := mgr.RecordJudgePolicyChoice(req.PRDID, req.UnitID, backend, model, reason); err != nil {
			log.Printf("[autonomous] judge: RecordJudgePolicyChoice(%s/%s): %v", req.PRDID, req.UnitID, err)
		}
	}
}

// judgeVerifierChooser routes the judges of one call through the routing
// policy with the judge role. The judge's backend list is autonomous.judge_policy
// when set, else the verifier policy's own list; the policy's mode (prefer-free,
// fixed, balance and the presets) comes from the verifier policy either way.
// Each call goes through autonomouspkg.SelectBackendForRole, so a judge whose
// preferred backend has no free slot takes the next admitting one, exactly as a
// verifier does, and the pick is written through pick. Backends already tried
// are excluded, so each is tried at most once. A policy that does not apply
// (fixed, with no backend list and no Claude cap) and a policy with no eligible
// backend both fall back to the unit's own guardrail backend, but only on the
// first attempt.
func judgeVerifierChooser(mgr *autonomouspkg.Manager, liveCfg autonomouspkg.Config, unitBackend string, probe func(backend, model string) bool, pick judgePickFn) judgeBackendChooser {
	verifier := effectiveVerifierPolicy(liveCfg)
	listed := config.NormalizeJudgePolicy(liveCfg.JudgePolicy)
	if len(listed) == 0 {
		listed = config.NormalizeVerifierPolicyBackends(verifier.Backends)
	}
	pool, global, _ := effectiveVerifierSelection(liveCfg)
	candidates := pool
	if len(candidates) == 0 {
		candidates = []string{unitBackend}
		if global != "" {
			candidates = []string{global}
		}
	}
	return func(tried []string) (string, bool) {
		fallback := func(reason string) (string, bool) {
			if len(tried) == 0 {
				pick(unitBackend, "", reason+"; using the unit backend "+unitBackend)
				return unitBackend, true
			}
			return "", false
		}
		backends := listed
		if len(listed) > 0 {
			backends = excludeBackends(listed, tried)
			if len(backends) == 0 {
				return "", false
			}
		}
		policy := autonomouspkg.ResolvePolicyForRole(autonomouspkg.PolicyRoleJudge, verifier, backends)
		if !verifierPolicyApplies(policy, autonomouspkg.VerifierSelectFlags{}) {
			return fallback("judge policy does not apply")
		}
		left := excludeBackends(candidates, tried)
		flags := autonomouspkg.VerifierSelectFlags{}
		if mgr != nil {
			flags.ClaudeHold = mgr.HeldBackends(verifierPolicyBackends(policy, left))
		}
		window := time.Duration(config.NormalizeVerifierPolicyClaudeCapWindow(policy.ClaudeCapWindowMinutes)) * time.Minute
		flags.ClaudeSessionsInWindow = judgeClaudeWindow.Count(window)
		load := &judgeRouteLoad{results: map[string]bool{}}
		if probe != nil {
			load.admit = func(backend string) bool { return probe(backend, "") }
		}
		sel := autonomouspkg.SelectBackendForRole(autonomouspkg.PolicyRoleJudge, verifier, backends, left, load, flags)
		if sel.Backend == "" {
			return fallback(sel.Reason)
		}
		if config.IsClaudeVerifierBackend(sel.Backend) {
			judgeClaudeWindow.Record()
		}
		pick(sel.Backend, sel.Model, sel.Reason)
		return sel.Backend, true
	}
}

// judgeBackendChooserFor picks how one judge call chooses its backends. A unit
// that pins its own guardrail backend keeps that one backend, as a per-unit
// override does for the verifier, and the pick is still logged. Otherwise every
// judge call goes through the judge role's selector (judgeVerifierChooser).
func judgeBackendChooserFor(ctx context.Context, mgr *autonomouspkg.Manager, req autonomouspkg.GuardrailInvocation, gcfg autonomouspkg.Config, unitBackend string) judgeBackendChooser {
	pick := judgeDecisionLog(mgr, req)
	if req.GuardrailBackend != "" {
		choose := judgeChooserFromList([]string{unitBackend})
		return func(tried []string) (string, bool) {
			backend, ok := choose(tried)
			if ok {
				pick(backend, "", "pinned unit backend "+backend)
			}
			return backend, ok
		}
	}
	var probe func(backend, model string) bool
	if judgeCapacityTryAdmit != nil {
		probe = func(backend, model string) bool {
			return judgeCapacityTryAdmit(ctx, backend, model, req.UnitID, req.PRDID)
		}
	}
	return judgeVerifierChooser(mgr, gcfg, unitBackend, probe, pick)
}
