package adapters

import (
	"sort"
	"sync"

	"github.com/dmz006/datawatch/internal/trace"
)

// Registry holds adapters by backend name.
type Registry struct {
	mu       sync.RWMutex
	adapters map[string]Adapter
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry { return &Registry{adapters: map[string]Adapter{}} }

// DefaultRegistry returns a registry with the built-in adapters.
func DefaultRegistry() *Registry {
	r := NewRegistry()
	r.Register(NewClaudeCode())
	r.Register(NewPi("pi"))
	r.Register(NewPi("pi-prompt"))
	for _, n := range []string{"opencode", "opencode-acp", "opencode-prompt"} {
		r.Register(NewOpenCode(n))
	}
	r.Register(NewGoose("goose"))
	r.Register(NewGoose("goose-prompt"))
	r.Register(NewAider())
	r.Register(NewGemini())
	r.Register(NewOllama())
	r.Register(NewOpenWebUI())
	r.Register(NewShell())
	return r
}

// Register adds or replaces an adapter under its Name.
func (r *Registry) Register(a Adapter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.adapters[a.Name()] = a
}

// Get returns the adapter for a backend regardless of settings.
func (r *Registry) Get(name string) (Adapter, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.adapters[name]
	return a, ok
}

// For returns the adapter for a backend only when tracing is enabled for it
// by the settings (tracing.mode and tracing.backends).
func (r *Registry) For(name string, s trace.Settings) (Adapter, bool) {
	if !s.BackendEnabled(name) {
		return nil, false
	}
	return r.Get(name)
}

// Enabled returns the adapters tracing.backends leaves on, sorted by name.
func (r *Registry) Enabled(s trace.Settings) []Adapter {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []Adapter
	for name, a := range r.adapters {
		if s.BackendEnabled(name) {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}
