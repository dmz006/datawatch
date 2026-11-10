package config

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestVerifierSettingsYAMLRoundTrip checks the pointer/plain-int shape on the
// config node entry: an unset field stays nil (or 0) after a YAML round trip,
// and an explicit 0 is kept distinct from nil.
func TestVerifierSettingsYAMLRoundTrip(t *testing.T) {
	zero := 0

	set := ComputeNodeYAML{
		Name:                 "gpubox",
		Kind:                 "ollama",
		VerifierSlots:        &zero,
		VerifierMinFreeMemMB: &zero,
		VerifierContextMB:    0,
	}
	data, err := yaml.Marshal(set)
	if err != nil {
		t.Fatalf("marshal explicit-zero node: %v", err)
	}
	var back ComputeNodeYAML
	if err := yaml.Unmarshal(data, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.VerifierSlots == nil || *back.VerifierSlots != 0 {
		t.Errorf("verifier_slots after round trip = %v, want explicit 0 (not nil)", back.VerifierSlots)
	}
	if back.VerifierMinFreeMemMB == nil || *back.VerifierMinFreeMemMB != 0 {
		t.Errorf("verifier_min_free_mem_mb after round trip = %v, want explicit 0 (not nil)", back.VerifierMinFreeMemMB)
	}
	if back.VerifierContextMB != 0 {
		t.Errorf("verifier_context_mb after round trip = %d, want 0", back.VerifierContextMB)
	}

	unset := ComputeNodeYAML{Name: "gpubox", Kind: "ollama"}
	data2, err := yaml.Marshal(unset)
	if err != nil {
		t.Fatalf("marshal unset node: %v", err)
	}
	var back2 ComputeNodeYAML
	if err := yaml.Unmarshal(data2, &back2); err != nil {
		t.Fatalf("unmarshal unset: %v", err)
	}
	if back2.VerifierSlots != nil {
		t.Errorf("verifier_slots after unset round trip = %v, want nil", *back2.VerifierSlots)
	}
	if back2.VerifierMinFreeMemMB != nil {
		t.Errorf("verifier_min_free_mem_mb after unset round trip = %v, want nil", *back2.VerifierMinFreeMemMB)
	}
	if back2.VerifierContextMB != 0 {
		t.Errorf("verifier_context_mb after unset round trip = %d, want 0", back2.VerifierContextMB)
	}
}

// TestVerifierSettingsConfigValidation checks the config load validation
// refuses a negative verifier_slots / verifier_min_free_mem_mb /
// verifier_context_mb (naming the node and the field, like session_slots)
// and accepts unset and explicit 0.
func TestVerifierSettingsConfigValidation(t *testing.T) {
	neg := -1
	zero := 0

	cases := []struct {
		name    string
		mutate  func(n *ComputeNodeYAML)
		wantErr string
	}{
		{"all unset", func(n *ComputeNodeYAML) {}, ""},
		{"all zero", func(n *ComputeNodeYAML) {
			n.VerifierSlots = &zero
			n.VerifierMinFreeMemMB = &zero
			n.VerifierContextMB = 0
		}, ""},
		{"negative verifier_slots", func(n *ComputeNodeYAML) { n.VerifierSlots = &neg }, "verifier_slots"},
		{"negative verifier_min_free_mem_mb", func(n *ComputeNodeYAML) { n.VerifierMinFreeMemMB = &neg }, "verifier_min_free_mem_mb"},
		{"negative verifier_context_mb", func(n *ComputeNodeYAML) { n.VerifierContextMB = neg }, "verifier_context_mb"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &Config{Git: GitConfig{ReleaseCIWindowMinutes: 60}, ComputeNodes: []ComputeNodeYAML{{Name: "gpubox", Kind: "ollama"}}}
			tc.mutate(&cfg.ComputeNodes[0])
			applyDefaults(cfg)
			err := validateLoaded(cfg)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("validateLoaded: %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatal("validateLoaded: want an error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantErr) || !strings.Contains(err.Error(), "gpubox") {
				t.Errorf("error %q does not name the node and %s", err, tc.wantErr)
			}
		})
	}
}
