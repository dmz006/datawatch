package compute

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// baseNode returns a Node that passes Validate except for the fields under
// test, so the table below isolates the verifier settings.
func verifierBaseNode() *Node {
	return &Node{Name: "gpubox", Kind: KindOllama, Address: "http://gpubox:11434"}
}

// TestVerifierSettingsYAMLRoundTrip checks the pointer/plain-int shape: an
// unset field stays nil (or 0) after a YAML round trip, and an explicit 0
// is kept distinct from nil.
func TestVerifierSettingsYAMLRoundTrip(t *testing.T) {
	zero := 0

	set := verifierBaseNode()
	set.VerifierSlots = &zero
	set.VerifierMinFreeMemMB = &zero
	set.VerifierContextMB = 0

	data, err := yaml.Marshal(set)
	if err != nil {
		t.Fatalf("marshal explicit-zero node: %v", err)
	}
	var back Node
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

	unset := verifierBaseNode()
	data2, err := yaml.Marshal(unset)
	if err != nil {
		t.Fatalf("marshal unset node: %v", err)
	}
	var back2 Node
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

// TestNodeValidateVerifierSettings checks Node.Validate refuses a negative
// verifier_slots / verifier_min_free_mem_mb / verifier_context_mb and
// accepts unset and explicit 0, in the same message style as session_slots.
func TestNodeValidateVerifierSettings(t *testing.T) {
	neg := -1
	zero := 0

	cases := []struct {
		name    string
		mutate  func(n *Node)
		wantErr string
	}{
		{"all unset", func(n *Node) {}, ""},
		{"all zero", func(n *Node) {
			n.VerifierSlots = &zero
			n.VerifierMinFreeMemMB = &zero
			n.VerifierContextMB = 0
		}, ""},
		{"negative verifier_slots", func(n *Node) { n.VerifierSlots = &neg }, "verifier_slots"},
		{"negative verifier_min_free_mem_mb", func(n *Node) { n.VerifierMinFreeMemMB = &neg }, "verifier_min_free_mem_mb"},
		{"negative verifier_context_mb", func(n *Node) { n.VerifierContextMB = neg }, "verifier_context_mb"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n := verifierBaseNode()
			tc.mutate(n)
			err := n.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate: %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatal("Validate: want an error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q does not name %s", err, tc.wantErr)
			}
		})
	}
}
