package autonomous

import "strings"

// resolvePlannedModel picks the model string passed to a decompose/planning
// session for the resolved planning backend. It returns "" when no model
// pairs with that backend, so the session backend uses its own default.
//
// Precedence, most specific wins:
//  1. prdDecompositionModel — an explicit PRD-level planning pairing always wins.
//  2. globalPlanningModel — only when it is paired with backend: the global
//     planning backend is empty or equal to backend, and the model does not
//     carry a "<prefix>/<model>" prefix that backend cannot serve.
//  3. prdModel — only when the PRD's own backend equals backend, so the model
//     belongs to that backend.
//  4. "" — let the backend pick its default.
//
// The pairing rule is decided by the backend the PRD or config names: the
// model must be one that backend can serve. pi-family backends serve
// provider-tagged models (e.g. "ollama/qwen3.8:27b"), so a foreign-looking
// prefix is fine for them; for other backends (e.g. an Ollama tag handed to
// claude-code) it stalls the session on an "unknown model" prompt, which is
// why each tier is checked against backend.
func resolvePlannedModel(backend, prdDecompositionModel, prdBackend, prdModel, globalPlanningBackend, globalPlanningModel string) string {
	if prdDecompositionModel != "" {
		return prdDecompositionModel
	}
	if globalPlanningModel != "" && (globalPlanningBackend == "" || globalPlanningBackend == backend) && !carriesForeignBackendPrefix(globalPlanningModel, backend) {
		return globalPlanningModel
	}
	if prdModel != "" && prdBackend == backend {
		return prdModel
	}
	return ""
}

// servesProviderModels reports whether backend accepts arbitrary model names
// written in pi's own "provider/model" form. The pi and pi-prompt backends
// forward any such model string to their configured provider (for example
// "ollama/qwen3.8:27b"), so the prefix is a provider tag, not a different
// datawatch backend.
func servesProviderModels(backend string) bool {
	return backend == "pi" || backend == "pi-prompt"
}

// carriesForeignBackendPrefix reports whether model is written as
// "<prefix>/<model>" with a prefix backend cannot serve. pi-family backends
// serve provider-tagged models, so they never carry a foreign prefix. For
// other backends the prefix names a backend the PRD or config did not
// select, so the model does not pair with backend.
func carriesForeignBackendPrefix(model, backend string) bool {
	if servesProviderModels(backend) {
		return false
	}
	_, foreign := foreignModelPrefix(model, backend)
	return foreign
}

// foreignModelPrefix returns the "<prefix>" of a model written as
// "<prefix>/<model>" when that prefix differs from backend. The second
// result is false when there is no prefix or the prefix names backend.
func foreignModelPrefix(model, backend string) (string, bool) {
	prefix, _, found := strings.Cut(model, "/")
	return prefix, found && prefix != backend
}
