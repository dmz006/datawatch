package autonomous

// An Automaton's guardrail_profile and retry_profile fields hold what the
// operator assigned, which is documented as a profile name, while profiles are
// stored under a short random id. These lookups try the id first and then the
// name; when two profiles share a name the newest one wins.

// prdGuardrailProfile returns the guardrail profile that key names.
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

// prdRetryProfile returns the retry profile that key names.
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
