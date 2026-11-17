package autonomous

// Named-profile resolution for PRD fields.
//
// The operator assigns a profile to an Automaton by its NAME — "named
// profile beats global config" is the documented contract (the
// per_automaton_guardrails_set and PUT …/guardrails surfaces, and the
// "named RetryProfile" tier in retry_policy.go). But profiles are
// persisted keyed by a short random ID (newProfileID), so a lookup of the
// stored name against the store's ID key always misses and every
// profile-tier resolution silently fell through to the global tier
// (source=global). These helpers restore the name: they try the ID first
// (a field that already holds an ID keeps working) and then match the
// profile's name.

// prdGuardrailProfile returns the guardrail profile a PRD's
// guardrail_profile field names, or false when no profile matches.
func (m *Manager) prdGuardrailProfile(key string) (*GuardrailProfile, bool) {
	if key == "" {
		return nil, false
	}
	if p, ok := m.store.GetGuardrailProfile(key); ok {
		return p, true
	}
	for _, p := range m.store.ListGuardrailProfiles() {
		if p.Name == key {
			return p, true
		}
	}
	return nil, false
}

// prdRetryProfile is the same for a PRD's retry_profile field.
func (m *Manager) prdRetryProfile(key string) (*RetryProfile, bool) {
	if key == "" {
		return nil, false
	}
	if p, ok := m.store.GetRetryProfile(key); ok {
		return p, true
	}
	for _, p := range m.store.ListRetryProfiles() {
		if p.Name == key {
			return p, true
		}
	}
	return nil, false
}
