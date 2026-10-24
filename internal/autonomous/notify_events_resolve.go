// notify_events_resolve.go — resolve the effective notify event kinds for
// an Automaton, mirroring the resolveGuardrails precedence convention
// (per-Automaton > named profile > global):
//
//  1. the Automaton's own PRD.NotifyEvents list (non-empty),
//  2. the assigned guardrail profile's NotifyEvents list (non-empty),
//  3. the global Manager Config.NotifyDefaultEvents — nil means the built-in
//     DefaultNotifyEvents set, a non-nil empty list means notifications off.
//
// A nil or empty list never "wins" over a lower tier in tiers 1–2 (empty
// means inherit); only tier 3 can express "no events" (explicit empty list).
package autonomous

// NotifyEventSet names the set of notify event kinds produced by a
// ResolveNotifyEvents* call. It is the API surface shape: the events list
// plus which tier supplied it, so a caller (API, notification consumer, MCP
// surface) both renders and reports the value.
type NotifyEventSet struct {
	// Events — the effective notify event kinds (fresh copy on every
	// resolution; mutating it never affects the store or live config).
	Events []string `json:"events"`
	// Source — NotifySourceAutomaton, NotifySourceProfile, or
	// NotifySourceGlobal: which tier produced Events. Source stays
	// "global" both for the built-in default (nil config) and for the
	// explicit empty list (notifications off).
	Source string `json:"source"`
}

// copyStrings returns a fresh copy of in, preserving nil (nil in → nil
// out) and an explicit empty slice. The resolver hands copies out so a
// caller mutating the result can never affect the store-owned or
// config-owned slice behind it.
func copyStrings(in []string) []string {
	if in == nil {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

// ResolveNotifyEvents resolves the effective notify event kinds for the
// Automaton named by prd (a nil prd resolves to the global tier directly).
//
// Precedence mirrors resolveGuardrails: the Automaton's own PRD.NotifyEvents
// list wins over the assigned guardrail profile's NotifyEvents list, which
// wins over the global default. A missing guardrail profile falls to the
// global tier; a nil global default resolves to the built-in
// DefaultNotifyEvents set; a non-nil empty global default means no events.
// The returned Events slice is always a fresh copy.
//
// The store lookup (profile tier) runs before m.mu is held for the global
// read — the same lock ordering resolveGuardrails uses: m.mu guards the
// global Config read only, never store calls.
func (m *Manager) ResolveNotifyEvents(prd *PRD) NotifyEventSet {
	if prd != nil {
		// Tier 1 — the Automaton's own list. Non-empty wins; a nil or empty
		// list here means inherit, and on its own never turns notifications
		// off.
		if len(prd.NotifyEvents) > 0 {
			return NotifyEventSet{
				Events: copyStrings(prd.NotifyEvents),
				Source: NotifySourceAutomaton,
			}
		}
		// Tier 2 — the assigned guardrail profile. A profile that no longer
		// exists, or a profile with an empty list, falls through to the
		// global tier (empty at this tier means inherit, same rule).
		if prd.GuardrailProfile != "" {
			if p, ok := m.store.GetGuardrailProfile(prd.GuardrailProfile); ok && p != nil && len(p.NotifyEvents) > 0 {
				return NotifyEventSet{
					Events: copyStrings(p.NotifyEvents),
					Source: NotifySourceProfile,
				}
			}
		}
	}
	// Tier 3 — the global default. m.mu guards this read only; the
	// profile lookup above ran with the mutex not held.
	m.mu.Lock()
	def := m.cfg.NotifyDefaultEvents
	m.mu.Unlock()
	if def == nil {
		def = DefaultNotifyEvents()
	}
	return NotifyEventSet{
		Events: copyStrings(def),
		Source: NotifySourceGlobal,
	}
}

// ResolveNotifyEventsFor resolves the effective notify event kinds for the
// Automaton of the given ID, loading it from the store first. It reports
// false when no such Automaton exists.
func (m *Manager) ResolveNotifyEventsFor(prdID string) (NotifyEventSet, bool) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return NotifyEventSet{}, false
	}
	return m.ResolveNotifyEvents(prd), true
}
