package autonomous

import (
	"strings"
	"testing"
)

func TestRequirePinnedModel(t *testing.T) {
	tests := []struct {
		name    string
		backend string
		model   string
		wantErr bool
	}{
		{name: "pi with a model passes", backend: "pi", model: "ollama/qwen3.8:27b", wantErr: false},
		{name: "pi-prompt with a model passes", backend: "pi-prompt", model: "anthropic/claude-sonnet-5", wantErr: false},
		{name: "pi with an empty model fails", backend: "pi", model: "", wantErr: true},
		{name: "pi-prompt with an empty model fails", backend: "pi-prompt", model: "", wantErr: true},
		{name: "claude-code with an empty model is untouched", backend: "claude-code", model: "", wantErr: false},
		{name: "opencode with an empty model is untouched", backend: "opencode", model: "", wantErr: false},
		{name: "empty backend with a model passes", backend: "", model: "x", wantErr: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := RequirePinnedModel(tt.backend, tt.model, "autonomous.planning_model", "autonomous.verification_model")
			if (err != nil) != tt.wantErr {
				t.Fatalf("RequirePinnedModel(%q, %q) err = %v, wantErr %v", tt.backend, tt.model, err, tt.wantErr)
			}
			if err != nil {
				for _, key := range []string{"autonomous.planning_model", "autonomous.verification_model"} {
					if !strings.Contains(err.Error(), key) {
						t.Errorf("error should name %q, got: %v", key, err)
					}
				}
				if !strings.Contains(err.Error(), tt.backend) {
					t.Errorf("error should name the backend %q, got: %v", tt.backend, err)
				}
			}
		})
	}
}

func TestResolvePinnedModel(t *testing.T) {
	tests := []struct {
		name    string
		backend string
		tiers   []ModelTier
		want    string
		wantErr bool
	}{
		{
			name:    "first paired tier wins",
			backend: "pi",
			tiers: []ModelTier{
				{Key: "per-unit", Model: "prov/a"},
				{Key: "global", Model: "prov/b"},
			},
			want: "prov/a",
		},
		{
			name:    "an unpaired tier is skipped, a paired one after it wins",
			backend: "pi",
			tiers: []ModelTier{
				{Key: "per-unit (bare claude alias)", Model: "claude-sonnet-5"},
				{Key: "global", Model: "ollama/qwen3.8:27b"},
			},
			want: "ollama/qwen3.8:27b",
		},
		{
			name:    "a provider-qualified tier is skipped on claude-code",
			backend: "claude-code",
			tiers: []ModelTier{
				{Key: "per-unit (provider-qualified)", Model: "ollama/qwen3.8:27b"},
				{Key: "global (bare alias)", Model: "claude-sonnet-5"},
			},
			want: "claude-sonnet-5",
		},
		{
			name:    "unset tiers are skipped silently",
			backend: "pi",
			tiers: []ModelTier{
				{Key: "per-unit", Model: ""},
				{Key: "global", Model: "prov/b"},
			},
			want: "prov/b",
		},
		{
			name:    "nothing pairs: error names every tier key",
			backend: "pi",
			tiers: []ModelTier{
				{Key: "autonomous.verification_model", Model: "claude-sonnet-5"},
				{Key: "autonomous.planning_model", Model: ""},
			},
			wantErr: true,
		},
		{
			name:    "all tiers empty: error names every tier key",
			backend: "pi-prompt",
			tiers: []ModelTier{
				{Key: "autonomous.verification_model", Model: ""},
				{Key: "autonomous.planning_model", Model: ""},
				{Key: "per-unit model", Model: ""},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolvePinnedModel(tt.backend, tt.tiers...)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ResolvePinnedModel(%q, %v) err = %v, wantErr %v", tt.backend, tt.tiers, err, tt.wantErr)
			}
			if err == nil {
				if got != tt.want {
					t.Errorf("ResolvePinnedModel = %q, want %q", got, tt.want)
				}
			} else {
				if got != "" {
					t.Errorf("ResolvePinnedModel on error = %q, want empty", got)
				}
				for _, tier := range tt.tiers {
					if tier.Key != "" && !strings.Contains(err.Error(), tier.Key) {
						t.Errorf("error should name tier key %q, got: %v", tier.Key, err)
					}
				}
			}
		})
	}
}
