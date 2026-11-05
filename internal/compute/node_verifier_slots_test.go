package compute

import (
	"strings"
	"testing"
)

// VerifierSlots — the verifier cap for parallel classification-verdict
// verifiers on a node (Decision 2026-11-03 in
// docs/plans/2026-10-14-concurrency-and-budget-design.md): 0 = sentinel for
// the default cap 1, negative = rejected.

func TestVerifierSlotsDefaultIsOne(t *testing.T) {
	if DefaultNodeVerifierSlots != 1 {
		t.Fatalf("DefaultNodeVerifierSlots = %d, want 1", DefaultNodeVerifierSlots)
	}
}

func TestNodeValidateVerifierSlots(t *testing.T) {
	valid := func(slots int) *Node {
		return &Node{
			Name:          "gpubox",
			Kind:          KindOllama,
			Address:       "http://gpubox:11434",
			VerifierSlots: slots,
		}
	}
	// 0 = sentinel: use the default cap; Validate accepts it.
	if err := valid(0).Validate(); err != nil {
		t.Errorf("Validate with verifier_slots 0 (default sentinel): %v, want nil", err)
	}
	if err := valid(3).Validate(); err != nil {
		t.Errorf("Validate with verifier_slots 3: %v, want nil", err)
	}
	err := valid(-1).Validate()
	if err == nil {
		t.Fatal("Validate with verifier_slots -1: want an error, got nil")
	}
	if !strings.Contains(err.Error(), "verifier_slots") {
		t.Errorf("error %q does not name the field", err)
	}
}
