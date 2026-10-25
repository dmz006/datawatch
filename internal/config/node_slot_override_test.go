package config

import (
	"strings"
	"testing"
)

// TestNodeSlotOverrideRejectsNegative checks the config load refuses a
// compute node whose session_slots is negative, so a bad value never reaches
// the registry seed. Zero is accepted and means "derive".
func TestNodeSlotOverrideRejectsNegative(t *testing.T) {
	bad := &Config{ComputeNodes: []ComputeNodeYAML{{Name: "gpubox", Kind: "ollama", SessionSlots: -1}}}
	applyDefaults(bad)
	err := validateLoaded(bad)
	if err == nil {
		t.Fatal("validateLoaded with session_slots -1: want an error, got nil")
	}
	if !strings.Contains(err.Error(), "session_slots") || !strings.Contains(err.Error(), "gpubox") {
		t.Errorf("error %q does not name the node and the field", err)
	}

	zero := &Config{ComputeNodes: []ComputeNodeYAML{{Name: "gpubox", Kind: "ollama", SessionSlots: 0}}}
	applyDefaults(zero)
	if err := validateLoaded(zero); err != nil {
		t.Errorf("validateLoaded with session_slots 0: %v, want nil", err)
	}
}
