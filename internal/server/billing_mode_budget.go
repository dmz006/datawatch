// billing_mode_budget.go — the budget, the burn guard and the usage page read the
// resolved billing mode of the LLM in use (config.ResolveBillingMode):
//
//	api        the budget compares dollars; the usage page labels figures in dollars
//	allowance  the budget and the burn guard behave as they always have; the usage
//	           page labels figures in points
//	local      no budget applies; the usage page shows no unit
//
// The burn guard keeps today's behaviour for the allowance entry (claude-code) and
// ignores an api or local entry unless that entry has a dollar budget set.
package server

import (
	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/burnguard"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/inference"
)

// Unit label keys the usage cards read; each is also a locale key (none is not).
const (
	UsageUnitDollars = "usage_unit_dollars"
	UsageUnitPoints  = "usage_unit_points"
	UsageUnitNone    = "none"
)

// UsageUnitKey maps a resolved billing mode to its unit label key.
func UsageUnitKey(mode string) string {
	switch mode {
	case config.BillingModeAPI:
		return UsageUnitDollars
	case config.BillingModeAllowance:
		return UsageUnitPoints
	}
	return UsageUnitNone
}

// llmEntry returns the registry entry for name, if there is one.
func (s *Server) llmEntry(name string) (inference.LLM, bool) {
	if s.inferenceReg == nil {
		return inference.LLM{}, false
	}
	l, err := s.inferenceReg.Get(name)
	if err != nil || l == nil {
		return inference.LLM{}, false
	}
	return *l, true
}

// llmBillingMode is the resolved billing mode of the named LLM. A name with no
// registry entry resolves as the allowance for the claude-code backend (the entry
// the burn guard and budget have always acted on) and as api otherwise.
func (s *Server) llmBillingMode(name string) string {
	if l, ok := s.llmEntry(name); ok {
		return config.ResolveBillingMode(l, s.cfg)
	}
	if name == burnguard.ClaudeBackend {
		return config.BillingModeAllowance
	}
	return config.BillingModeAPI
}

// backendUnitKeys is the unit label key per backend, for the usage page payload.
func (s *Server) backendUnitKeys() map[string]string {
	out := map[string]string{}
	if s.inferenceReg != nil {
		for _, l := range s.inferenceReg.List() {
			out[l.Name] = UsageUnitKey(config.ResolveBillingMode(*l, s.cfg))
		}
	}
	if _, ok := out[burnguard.ClaudeBackend]; !ok {
		out[burnguard.ClaudeBackend] = UsageUnitPoints
	}
	return out
}

// budgetApplies reports whether a budget is read for the LLM: not for local.
func (s *Server) budgetApplies(name string) bool {
	return s.llmBillingMode(name) != config.BillingModeLocal
}

// filterBudgetStatuses drops the statuses of local LLMs, which have no budget.
func (s *Server) filterBudgetStatuses(in []autonomous.BudgetStatus) []autonomous.BudgetStatus {
	out := make([]autonomous.BudgetStatus, 0, len(in))
	for _, st := range in {
		if s.budgetApplies(st.LLM) {
			out = append(out, st)
		}
	}
	return out
}

// burnGuardApplies reports whether the burn guard acts on the backend: always for
// an allowance entry; for an api or local entry only when it has a dollar budget.
func (s *Server) burnGuardApplies(backend string) bool {
	if s.llmBillingMode(backend) == config.BillingModeAllowance {
		return true
	}
	l, ok := s.llmEntry(backend)
	return ok && l.Budget != nil && l.Budget.CeilingUSD > 0
}
