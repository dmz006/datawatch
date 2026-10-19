// Package autonomous.
//
// local_engine.go decides whether a session runs its model on a local compute
// node, so the session can be admitted against that node's capacity pool. The
// decision is a pure function: callers supply the provider-to-node lookup
// (built from the inference and compute registries) and get back the node name
// and a gated flag, with no I/O of their own.
package autonomous

import "strings"

// localEngineBackends are the backends that can run a model on a local engine.
// Hosted backends such as claude-code are never gated here. opencode and
// opencode-acp are included only when the model names a local engine, which
// the provider lookup in LocalEngineGate decides.
var localEngineBackends = map[string]bool{
	"pi":           true,
	"pi-prompt":    true,
	"ollama":       true,
	"opencode":     true,
	"opencode-acp": true,
}

// LocalEngineGate reports the compute node a session's model runs on and
// whether that session is gated on the node's capacity pool.
//
// The node comes from the model's provider prefix, the text before the first
// slash, passed through lookup. A session is gated only when backend is one of
// the local-engine backends and that prefix resolves to a node. A hosted
// backend, a model with no provider prefix, or a prefix lookup cannot resolve
// all return ("", false).
func LocalEngineGate(backend, model string, lookup func(provider string) (node string, ok bool)) (string, bool) {
	if !localEngineBackends[backend] {
		return "", false
	}
	provider, _, found := strings.Cut(model, "/")
	if !found || provider == "" {
		return "", false
	}
	node, ok := lookup(provider)
	if !ok || node == "" {
		return "", false
	}
	return node, true
}
