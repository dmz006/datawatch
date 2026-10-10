// BL406 Phase 5 — real GuidedMode behavior (fixes B111: the original
// bool flag had full set-side REST/MCP/CLI/comm plumbing but nothing
// ever read it). GuidedModeSource replaces it with a pluggable gate:
// "operator" (pause before each task, wait for ApproveTask — the
// original BL384 intent, now actually built), "council" (declared but
// not yet implemented — BL405 Phase 8's CouncilProfile is the
// dependency; falls back to "operator" rather than silently skipping
// the gate), "guardrail_auto" (continue automatically whenever quality
// gates + project rules already pass — behaviorally identical to
// leaving GuidedModeSource empty; a real, named choice for operators
// who want to say so on purpose, not new executor behavior).

package autonomous

import "log"

const (
	GuidedModeOperator      = "operator"
	GuidedModeCouncil       = "council"
	GuidedModeGuardrailAuto = "guardrail_auto"
)

// migrateGuidedMode is called once per PRD at store-load time (same
// place NormalizePRDStatus already runs). An existing PRD persisted
// with the old guided_mode:true and no GuidedModeSource is treated as
// "operator" — the field never actually worked, but an operator who
// set it clearly wanted *some* gate, and "operator" is the original
// documented intent (BL384). Never overwrites an explicitly-set
// GuidedModeSource.
func migrateGuidedMode(prd *PRD) {
	if prd.GuidedModeSource == "" && prd.GuidedMode {
		prd.GuidedModeSource = GuidedModeOperator
	}
}

// resolveGuidedModeSource returns the effective source, falling back
// "council" to "operator" when CouncilProfile support isn't wired yet
// (resolveCouncilFn is nil until BL405 Phase 8 lands) — logged once
// per call so the fallback is visible, never silent.
func (m *Manager) resolveGuidedModeSource(prd *PRD) string {
	src := prd.GuidedModeSource
	if src == GuidedModeCouncil && m.councilApprovalFn == nil {
		log.Printf("[autonomous] prd=%s guided_mode_source=council requested but no CouncilProfile support is wired (BL405 Phase 8 not yet shipped) — falling back to operator approval", prd.ID)
		return GuidedModeOperator
	}
	return src
}
