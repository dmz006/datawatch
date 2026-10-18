package autonomous

import "testing"

func TestResolvePlannedModel(t *testing.T) {
	tests := []struct {
		name                  string
		backend               string
		prdDecompositionModel string
		prdBackend            string
		prdModel              string
		globalPlanningBackend string
		globalPlanningModel   string
		want                  string
	}{
		{
			name:                "global ollama-prefixed model does not pair with claude-code",
			backend:             "claude-code",
			globalPlanningModel: "ollama/qwen3.8-64k:27b",
			want:                "",
		},
		{
			name:                  "global ollama-prefixed model with non-matching planning backend does not pair with claude-code",
			backend:               "claude-code",
			globalPlanningBackend: "opencode",
			globalPlanningModel:   "ollama/qwen3.8-64k:27b",
			want:                  "",
		},
		{
			name:       "PRD backend and model pair with the resolved backend",
			backend:    "claude-code",
			prdBackend: "claude-code",
			prdModel:   "claude-sonnet-5",
			want:       "claude-sonnet-5",
		},
		{
			name:       "PRD model is ignored when its backend differs from the resolved backend",
			backend:    "claude-code",
			prdBackend: "opencode",
			prdModel:   "ollama/qwen3.8:27b",
			want:       "",
		},
		{
			name:                  "global pair with matching backend uses the global model",
			backend:               "opencode",
			globalPlanningBackend: "opencode",
			globalPlanningModel:   "qwen3.8:27b",
			want:                  "qwen3.8:27b",
		},
		{
			name:                "unprefixed global model with no planning backend pairs with any backend",
			backend:             "opencode",
			globalPlanningModel: "qwen3.8:27b",
			want:                "qwen3.8:27b",
		},
		{
			name:                  "global model with a different planning backend does not pair",
			backend:               "opencode",
			globalPlanningBackend: "claude-code",
			globalPlanningModel:   "claude-sonnet-5",
			want:                  "",
		},
		{
			name:                  "explicit PRD decomposition model wins over conflicting globals",
			backend:               "claude-code",
			prdDecompositionModel: "claude-opus-5",
			globalPlanningBackend: "opencode",
			globalPlanningModel:   "ollama/qwen3.8-64k:27b",
			prdBackend:            "claude-code",
			prdModel:              "claude-sonnet-5",
			want:                  "claude-opus-5",
		},
		{
			name:                  "global model wins over PRD model when both pair",
			backend:               "opencode",
			globalPlanningBackend: "opencode",
			globalPlanningModel:   "qwen3.8:27b",
			prdBackend:            "opencode",
			prdModel:              "qwen3.5:9b",
			want:                  "qwen3.8:27b",
		},
		{
			name:    "nothing set returns empty so the backend default applies",
			backend: "claude-code",
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolvePlannedModel(tt.backend, tt.prdDecompositionModel, tt.prdBackend, tt.prdModel, tt.globalPlanningBackend, tt.globalPlanningModel)
			if got != tt.want {
				t.Errorf("resolvePlannedModel(%q, ...) = %q, want %q", tt.backend, got, tt.want)
			}
		})
	}
}

// Matrix axes for TestResolvePlannedModelMatrix. The coordinator's regression
// suite cites these; keep them in sync with plannedGlobalCases and
// plannedPRDCases below.
//
//	Axis 1 — decompose backend (the resolved backend passed in):
//	  claude-code, opencode, pi-prompt, ollama, unknown name ("unknown-backend").
//	Axis 2 — global planning settings:
//	  empty (no model, no planning_backend); or a global model of kind
//	  foreign ("ollama/qwen3.8-64k:27b", or "opencode/..." when the backend is
//	  ollama), same-backend ("<backend>/qwen3.8:27b")
//	  or unprefixed ("qwen3.8:27b"), each with planning_backend unset, equal to
//	  the decompose backend, or set to another backend.
//	Axis 3 — PRD-level settings:
//	  no overrides; a PRD decomposition model; a PRD worker model whose PRD
//	  backend equals the decompose backend; a PRD worker model on a different
//	  backend.
//
// Expected rule: a PRD decomposition model always wins; otherwise a global
// model is forwarded only when it is paired with the decompose backend (the
// planning backend is unset or equal, and the model is not foreign); otherwise
// the PRD worker model when its backend matches; otherwise "".

type plannedGlobalKind int

const (
	plannedGlobalNone plannedGlobalKind = iota
	plannedGlobalForeign
	plannedGlobalSameBackend
	plannedGlobalUnprefixed
)

type plannedGlobalCase struct {
	name            string
	kind            plannedGlobalKind
	planningBackend string
	model           string
}

type plannedPRDCase struct {
	name                  string
	prdDecompositionModel string
	prdBackend            string
	prdModel              string
}

const (
	matrixOtherBackend = "elsewhere"
	matrixWorkerModel  = "worker-model"
	matrixDecompModel  = "decomp-model"
)

// plannedGlobalCases builds the axis-2 rows for one decompose backend. The
// model strings depend on the backend, so they are built per backend.
func plannedGlobalCases(backend string) []plannedGlobalCase {
	// The foreign prefix must name a backend other than the one under test,
	// otherwise the model would carry the backend's own prefix.
	foreignPrefix := "ollama"
	if backend == foreignPrefix {
		foreignPrefix = "opencode"
	}
	cases := []plannedGlobalCase{{name: "global empty"}}
	shapes := []struct {
		name  string
		kind  plannedGlobalKind
		model string
	}{
		{"foreign prefix", plannedGlobalForeign, foreignPrefix + "/qwen3.8-64k:27b"},
		{"same prefix", plannedGlobalSameBackend, backend + "/qwen3.8:27b"},
		{"unprefixed", plannedGlobalUnprefixed, "qwen3.8:27b"},
	}
	planningBackends := []struct {
		name  string
		value string
	}{
		{"no planning_backend", ""},
		{"matching planning_backend", backend},
		{"other planning_backend", matrixOtherBackend},
	}
	for _, s := range shapes {
		for _, pb := range planningBackends {
			cases = append(cases, plannedGlobalCase{
				name:            s.name + ", " + pb.name,
				kind:            s.kind,
				planningBackend: pb.value,
				model:           s.model,
			})
		}
	}
	return cases
}

// plannedPRDCases builds the axis-3 rows for one decompose backend.
func plannedPRDCases(backend string) []plannedPRDCase {
	return []plannedPRDCase{
		{name: "no PRD overrides"},
		{name: "PRD decomposition model", prdDecompositionModel: matrixDecompModel},
		{name: "PRD worker model, same backend", prdBackend: backend, prdModel: matrixWorkerModel},
		{name: "PRD worker model, different backend", prdBackend: matrixOtherBackend, prdModel: matrixWorkerModel},
	}
}

// expectedPlannedModel is the matrix oracle. It is written from the rule in
// the comment above, not from resolvePlannedModel, and it uses the axis kind
// instead of parsing the model string, so a wrong resolver fails the matrix.
func expectedPlannedModel(backend string, g plannedGlobalCase, p plannedPRDCase) string {
	if p.prdDecompositionModel != "" {
		return p.prdDecompositionModel
	}
	if g.kind != plannedGlobalNone {
		pairedBackend := g.planningBackend == "" || g.planningBackend == backend
		ownedElsewhere := g.kind == plannedGlobalForeign
		if pairedBackend && !ownedElsewhere {
			return g.model
		}
	}
	if p.prdModel != "" && p.prdBackend == backend {
		return p.prdModel
	}
	return ""
}

func TestResolvePlannedModelMatrix(t *testing.T) {
	backends := []string{"claude-code", "opencode", "pi-prompt", "ollama", "unknown-backend"}
	for _, backend := range backends {
		for _, g := range plannedGlobalCases(backend) {
			for _, p := range plannedPRDCases(backend) {
				want := expectedPlannedModel(backend, g, p)
				name := backend + " | " + g.name + " | " + p.name
				t.Run(name, func(t *testing.T) {
					got := resolvePlannedModel(backend, p.prdDecompositionModel, p.prdBackend, p.prdModel, g.planningBackend, g.model)
					if got != want {
						t.Errorf("resolvePlannedModel(%q, ...) = %q, want %q", backend, got, want)
					}
				})
			}
		}
	}
}
