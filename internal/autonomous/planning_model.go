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
//     carry a "<other>/<model>" prefix naming a different backend.
//  3. prdModel — only when the PRD's own backend equals backend, so the model
//     belongs to that backend.
//  4. "" — let the backend pick its default.
//
// Passing a model that belongs to another backend (e.g. an Ollama tag
// "ollama/qwen3.8:27b" handed to claude-code) makes the session stall on an
// "unknown model" prompt, which is why each tier is checked against backend.
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

// carriesForeignBackendPrefix reports whether model is written as
// "<prefix>/<model>" with a prefix other than backend. Such a model is
// addressed to a different backend or provider, so it does not pair with
// backend.
func carriesForeignBackendPrefix(model, backend string) bool {
	prefix, _, found := strings.Cut(model, "/")
	return found && prefix != backend
}
