// Provider qualification of pi-family worker models.
//
// A pi-family worker is admitted against a compute node chosen from its
// model's provider prefix (see LocalEngineGate). A model written without that
// prefix resolves to no node, so the lease names none and the session is left
// to pi's own provider table. This file holds the one place that turns such a
// model into the provider-qualified value the rest of dispatch then shares.

package autonomous

import "strings"

// PiDefaultModelFn reports the provider-qualified model a pi-family launch of
// backend falls back to, for example "ollama/qwen3.8:27b", or "" when none is
// configured. The session manager applies the same configured value when a
// pi session names no model, so its provider is the one a launch would use.
type PiDefaultModelFn func(backend string) string

// qualifyPiFamilyModel returns the model a pi-family worker is admitted,
// spawned and started with. A model that already names a provider is returned
// as written. A bare model takes the provider of the backend's launch default;
// an empty model takes that default whole, the model the launch would run on.
// Any other backend, and a default that names no provider, leave model
// unchanged.
func (m *Manager) qualifyPiFamilyModel(backend, model string) string {
	if !servesProviderModels(backend) || strings.Contains(model, "/") {
		return model
	}
	m.mu.Lock()
	fn := m.piDefaultModel
	m.mu.Unlock()
	if fn == nil {
		return model
	}
	def := strings.TrimSpace(fn(backend))
	provider, _, found := strings.Cut(def, "/")
	if !found || provider == "" {
		return model
	}
	if model == "" {
		return def
	}
	return provider + "/" + model
}

// SetPiDefaultModel wires the lookup that supplies the provider for a
// pi-family model written without one. Nil (the default) leaves every model
// as written.
func (m *Manager) SetPiDefaultModel(fn PiDefaultModelFn) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.piDefaultModel = fn
}
