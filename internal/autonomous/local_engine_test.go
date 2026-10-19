package autonomous

import "testing"

func TestLocalEngineGate(t *testing.T) {
	nodes := map[string]string{
		"ollama": "alpha",
		"gpu":    "",
	}
	lookup := func(provider string) (string, bool) {
		n, ok := nodes[provider]
		return n, ok
	}

	tests := []struct {
		name     string
		backend  string
		model    string
		wantNode string
		wantOK   bool
	}{
		{
			name:     "pi model with ollama prefix gated on alpha",
			backend:  "pi",
			model:    "ollama/qwen3.8:27b",
			wantNode: "alpha",
			wantOK:   true,
		},
		{
			name:     "pi-prompt model with ollama prefix gated on alpha",
			backend:  "pi-prompt",
			model:    "ollama/qwen3.8:27b",
			wantNode: "alpha",
			wantOK:   true,
		},
		{
			name:     "ollama backend gated on alpha",
			backend:  "ollama",
			model:    "ollama/qwen3.8:27b",
			wantNode: "alpha",
			wantOK:   true,
		},
		{
			name:     "opencode naming a local engine gated on alpha",
			backend:  "opencode",
			model:    "ollama/qwen3.8:27b",
			wantNode: "alpha",
			wantOK:   true,
		},
		{
			name:     "opencode-acp naming a local engine gated on alpha",
			backend:  "opencode-acp",
			model:    "ollama/qwen3.8:27b",
			wantNode: "alpha",
			wantOK:   true,
		},
		{
			name:    "claude-code session is not gated",
			backend: "claude-code",
			model:   "claude-sonnet-5-5",
			wantOK:  false,
		},
		{
			name:    "model whose node is unknown is not gated",
			backend: "pi",
			model:   "mystery/model-x",
			wantOK:  false,
		},
		{
			name:    "hosted backend with provider-style model is not gated",
			backend: "claude-code",
			model:   "ollama/qwen3.8:27b",
			wantOK:  false,
		},
		{
			name:    "local backend with no provider prefix is not gated",
			backend: "pi",
			model:   "qwen3.8:27b",
			wantOK:  false,
		},
		{
			name:    "local backend whose provider resolves to no node is not gated",
			backend: "pi",
			model:   "gpu/llama",
			wantOK:  false,
		},
		{
			name:    "empty model is not gated",
			backend: "ollama",
			model:   "",
			wantOK:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotNode, gotOK := LocalEngineGate(tc.backend, tc.model, lookup)
			if gotOK != tc.wantOK {
				t.Fatalf("LocalEngineGate(%q, %q) gated = %v, want %v", tc.backend, tc.model, gotOK, tc.wantOK)
			}
			if gotNode != tc.wantNode {
				t.Fatalf("LocalEngineGate(%q, %q) node = %q, want %q", tc.backend, tc.model, gotNode, tc.wantNode)
			}
		})
	}
}
