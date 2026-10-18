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
