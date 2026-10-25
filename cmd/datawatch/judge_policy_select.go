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

// judgeVerifierChooser routes the judges of one call through the verifier
// routing policy, the judge role's default when autonomous.judge_policy is
// empty. The policy's own mode (prefer-free, fixed, balance and the presets)
// picks among the backends not yet tried, so each backend is tried at most
// once. A policy that does not apply (fixed, with no backend list and no
// Claude cap) and a policy with no eligible backend both fall back to the
// unit's own guardrail backend, but only on the first attempt.
func judgeVerifierChooser(mgr *autonomouspkg.Manager, liveCfg autonomouspkg.Config, unitBackend string, probe func(backend, model string) bool) judgeBackendChooser {
	policy := effectiveVerifierPolicy(liveCfg)
	pool, global, _ := effectiveVerifierSelection(liveCfg)
	candidates := pool
	if len(candidates) == 0 {
		candidates = []string{unitBackend}
		if global != "" {
			candidates = []string{global}
		}
	}
	return func(tried []string) (string, bool) {
		fallback := func() (string, bool) {
			if len(tried) == 0 {
				return unitBackend, true
			}
			return "", false
		}
		if !verifierPolicyApplies(policy, autonomouspkg.VerifierSelectFlags{}) {
			return fallback()
		}
		p := policy
		if listed := config.NormalizeVerifierPolicyBackends(policy.Backends); len(listed) > 0 {
			p.Backends = excludeBackends(listed, tried)
			if len(p.Backends) == 0 {
				return "", false
			}
		}
		left := excludeBackends(candidates, tried)
		flags := autonomouspkg.VerifierSelectFlags{}
		if mgr != nil {
			flags.ClaudeHold = mgr.HeldBackends(verifierPolicyBackends(p, left))
		}
		window := time.Duration(config.NormalizeVerifierPolicyClaudeCapWindow(policy.ClaudeCapWindowMinutes)) * time.Minute
		flags.ClaudeSessionsInWindow = judgeClaudeWindow.Count(window)
		load := &judgeRouteLoad{results: map[string]bool{}}
		if probe != nil {
			load.admit = func(backend string) bool { return probe(backend, "") }
		}
		backend, _, reason := autonomouspkg.SelectVerifierBackend(p, left, load, flags)
		if backend == "" {
			log.Printf("[autonomous] judge: policy: %s; using the unit backend %s", reason, unitBackend)
			return fallback()
		}
		if config.IsClaudeVerifierBackend(backend) {
			judgeClaudeWindow.Record()
		}
		log.Printf("[autonomous] judge: policy: %s (unit backend %s)", reason, unitBackend)
		return backend, true
	}
}

// judgeBackendChooserFor picks how one judge call chooses its backends. A unit
// that pins its own guardrail backend keeps that one backend, as a per-unit
// override does for the verifier. Otherwise an explicit autonomous.judge_policy
// list is tried in order; an empty list follows the verifier routing policy.
func judgeBackendChooserFor(ctx context.Context, mgr *autonomouspkg.Manager, req autonomouspkg.GuardrailInvocation, gcfg autonomouspkg.Config, unitBackend string) judgeBackendChooser {
	if req.GuardrailBackend != "" {
		return judgeChooserFromList([]string{unitBackend})
	}
	if backends := config.NormalizeJudgePolicy(gcfg.JudgePolicy); len(backends) > 0 {
		return judgeChooserFromList(backends)
	}
	var probe func(backend, model string) bool
	if judgeCapacityTryAdmit != nil {
		probe = func(backend, model string) bool {
			return judgeCapacityTryAdmit(ctx, backend, model, req.UnitID, req.PRDID)
		}
	}
	return judgeVerifierChooser(mgr, gcfg, unitBackend, probe)
}
