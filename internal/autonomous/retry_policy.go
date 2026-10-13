// BL410 Phase 1 — retry-policy resolution.
//
// Mirrors resolveGuardrails' exact precedence shape (guardrail_registry.go):
// explicit PRD field > named profile > global Config default. Every
// decision here (3-tier precedence, a profile controlling behavior as
// well as numbers, the content/policy→AutoFixRetries fallback, the
// infra/scope-only defaults) was asked of and answered by the operator
// directly during BL410's design — see docs/plans/README.md's BL410
// entry and the plan file it references.
package autonomous

import (
	"context"
	"fmt"
	"time"
)

// resolveRetryPolicy returns the effective retry budget for prd at the
// given category ("infra" | "content" | "scope" | "policy"). Priority:
// explicit PRD.RetryPolicy > named RetryProfile > global Config. When
// nothing sets "content"/"policy" anywhere, falls back to the existing
// Config.AutoFixRetries — the backward-compat anchor, so a PRD that
// never touches this feature gets byte-for-byte today's retry count.
func (m *Manager) resolveRetryPolicy(prd *PRD, category string) int {
	if v, ok := prd.RetryPolicy[category]; ok {
		return v
	}
	if prd.RetryProfile != "" {
		if p, ok := m.store.GetRetryProfile(prd.RetryProfile); ok {
			if v, ok := p.Budgets[category]; ok {
				return v
			}
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if v, ok := m.cfg.RetryPolicy[category]; ok {
		return v
	}
	if category == "content" || category == "policy" {
		return m.cfg.AutoFixRetries
	}
	return 0
}

// resolveRetryCeiling returns the total auto-retry budget (summed
// across every category and unit) for prd's whole lifecycle. Priority:
// explicit PRD.RetryCeilingTotal > named RetryProfile > global Config
// (default 5, seeded by DefaultConfig).
func (m *Manager) resolveRetryCeiling(prd *PRD) int {
	if prd.RetryCeilingTotal > 0 {
		return prd.RetryCeilingTotal
	}
	if prd.RetryProfile != "" {
		if p, ok := m.store.GetRetryProfile(prd.RetryProfile); ok && p.CeilingTotal > 0 {
			return p.CeilingTotal
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cfg.RetryCeilingTotal > 0 {
		return m.cfg.RetryCeilingTotal
	}
	return 5
}

// resolveInfraEscalateAfter returns how many retries keep the exact
// original backend/model config before an infra-category retry falls
// back to the global default. Same 3-tier precedence as above (default
// 2, seeded by DefaultConfig). PRD has no explicit per-PRD field for
// this (it is a RetryProfile-level behavior knob per the operator's
// own decision that a profile, not a loose PRD field, controls
// escalation behavior) — only the profile and global tiers apply.
func (m *Manager) resolveInfraEscalateAfter(prd *PRD) int {
	if prd.RetryProfile != "" {
		if p, ok := m.store.GetRetryProfile(prd.RetryProfile); ok && p.InfraRetryEscalateAfter > 0 {
			return p.InfraRetryEscalateAfter
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cfg.InfraRetryEscalateAfter > 0 {
		return m.cfg.InfraRetryEscalateAfter
	}
	return 2
}

// resolveContentAutoFixEnabled reports whether a content-category
// guardrail block should get auto-corrective retry (Phase 2). Priority:
// explicit PRD override > named RetryProfile > global Config (default
// true, seeded by DefaultConfig — the operator's "work auto the
// default" decision).
func (m *Manager) resolveContentAutoFixEnabled(prd *PRD) bool {
	if prd.ContentAutoFixEnabled != nil {
		return *prd.ContentAutoFixEnabled
	}
	if prd.RetryProfile != "" {
		if p, ok := m.store.GetRetryProfile(prd.RetryProfile); ok && p.ContentAutoFixEnabled != nil {
			return *p.ContentAutoFixEnabled
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg.ContentAutoFixEnabled
}

// resolveAutoFixSkipApproval reports whether a synthesized corrective
// task/story (Phase 2) bypasses PRD.PerStoryApproval. Same 3-tier
// precedence and default-true as resolveContentAutoFixEnabled.
func (m *Manager) resolveAutoFixSkipApproval(prd *PRD) bool {
	if prd.AutoFixSkipApproval != nil {
		return *prd.AutoFixSkipApproval
	}
	if prd.RetryProfile != "" {
		if p, ok := m.store.GetRetryProfile(prd.RetryProfile); ok && p.AutoFixSkipApproval != nil {
			return *p.AutoFixSkipApproval
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg.AutoFixSkipApproval
}

// retryBudgetRemaining reports whether prd has any budget left — both
// the per-category budget for this specific category, and the PRD-wide
// ceiling summed across every category — before a further auto-retry
// of any kind is attempted.
func (m *Manager) retryBudgetRemaining(prd *PRD, category string) bool {
	budget := m.resolveRetryPolicy(prd, category)
	spent := prd.RetryCountsByCategory[category]
	if spent >= budget {
		return false
	}
	ceiling := m.resolveRetryCeiling(prd)
	total := 0
	for _, n := range prd.RetryCountsByCategory {
		total += n
	}
	return total < ceiling
}

// consumeRetryBudget records one more auto-retry attempt against prd
// for the given category, persisting the PRD so the consumption
// survives a daemon restart.
func (m *Manager) consumeRetryBudget(prd *PRD, category string) error {
	if prd.RetryCountsByCategory == nil {
		prd.RetryCountsByCategory = map[string]int{}
	}
	prd.RetryCountsByCategory[category]++
	return m.store.SavePRD(prd)
}

// recordAutoRetryDecision appends the audit trail for one unattended
// auto-retry attempt. The executor loop has no HTTP request/actor
// context to route through internal/audit's CEF system (that's the
// server layer's own audit trail for operator-triggered actions) — the
// autonomous package's own already-universally-exposed audit trail is
// Decision (REST/MCP/PWA all already surface prd.Decisions), so an
// auto-retry gets one of those, not a new mechanism.
func (m *Manager) recordAutoRetryDecision(prd *PRD, unitID, category string, attempt int, escalated bool) {
	note := fmt.Sprintf("unit=%s category=%s attempt=%d", unitID, category, attempt)
	if escalated {
		note += " escalated-to-global-default"
	}
	prd.Decisions = append(prd.Decisions, Decision{
		At: time.Now(), Kind: "auto_retry", Actor: "autonomous",
		Note: note, FailureCategory: category,
	})
}

// runGuardrailWithInfraRetry (BL410 Phase 1) wraps a single guardrail
// invocation with bounded, escalating infra-category auto-retry — the
// direct fix for the motivating incident (a guardrail session timeout
// requiring manual operator intervention indistinguishable from a real
// compliance block). A content/scope/policy block, an error, or an
// infra block once the retry budget is exhausted, returns as-is for
// the caller to treat exactly as it does today (TaskBlocked/story
// block/PRDBlocked — Phase 1 does not change that terminal behavior,
// only how many chances a purely mechanical failure gets first).
func (m *Manager) runGuardrailWithInfraRetry(ctx context.Context, prd *PRD, inv GuardrailInvocation, fn GuardrailFn) (GuardrailVerdict, error) {
	attempt := 0
	for {
		v, err := fn(ctx, inv)
		if err != nil || v.Outcome != "block" || v.Category != "infra" {
			return v, err
		}
		if !m.retryBudgetRemaining(prd, "infra") {
			return v, nil
		}
		attempt++
		_ = m.consumeRetryBudget(prd, "infra")
		escalated := attempt > m.resolveInfraEscalateAfter(prd)
		if escalated {
			// Decision 6 — drop the per-unit override that just failed;
			// fall back to the global default, mirroring the exact
			// discipline resolveGuardrailBackendModel/
			// resolveVerifierBackendModel already apply to a FIRST
			// attempt, now applied to a RETRY.
			inv.GuardrailBackend = ""
			inv.GuardrailModel = ""
		}
		m.recordAutoRetryDecision(prd, inv.UnitID, "infra", attempt, escalated)
		_ = m.store.SavePRD(prd)
	}
}
