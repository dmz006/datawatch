package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The node over-admission settings default to on, and the adoption wait to 30
// seconds, from an absent key and from DefaultConfig alike.
func TestCapacityNodeLimit_Defaults(t *testing.T) {
	for name, a := range map[string]AutonomousConfig{
		"absent key":    loadYAML(t, "autonomous:\n  enabled: true\n").Autonomous,
		"DefaultConfig": DefaultConfig().Autonomous,
	} {
		if !a.CapacityStrictNodeLimitOn() || !a.CapacityCountPoolByNodeOn() || !a.CapacityAdoptBeforeAdmitOn() {
			t.Errorf("%s: strict=%v count_by_node=%v adopt_first=%v, want all true", name,
				a.CapacityStrictNodeLimitOn(), a.CapacityCountPoolByNodeOn(), a.CapacityAdoptBeforeAdmitOn())
		}
		if got := a.CapacityAdoptionWait(); got != 30*time.Second {
			t.Errorf("%s: adoption wait = %v, want 30s", name, got)
		}
	}
}

// An explicit false and an explicit wait survive a save and load, and a
// setting left out stays on.
func TestCapacityNodeLimit_SaveLoadRoundTrip(t *testing.T) {
	off := false
	cfg := DefaultConfig()
	cfg.Autonomous.CapacityStrictNodeLimit = &off
	cfg.Autonomous.CapacityCountPoolByNode = &off
	cfg.Autonomous.CapacityAdoptBeforeAdmit = &off
	cfg.Autonomous.CapacityAdoptionWaitSeconds = 90
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := Save(cfg, p); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	a := got.Autonomous
	if a.CapacityStrictNodeLimitOn() || a.CapacityCountPoolByNodeOn() || a.CapacityAdoptBeforeAdmitOn() {
		t.Errorf("round trip lost an explicit false: strict=%v count_by_node=%v adopt_first=%v",
			a.CapacityStrictNodeLimitOn(), a.CapacityCountPoolByNodeOn(), a.CapacityAdoptBeforeAdmitOn())
	}
	if a.CapacityAdoptionWait() != 90*time.Second {
		t.Errorf("adoption wait = %v, want 90s", a.CapacityAdoptionWait())
	}

	// Turning one setting off leaves the other two on.
	one := loadYAML(t, "autonomous:\n  capacity_count_pool_by_node: false\n").Autonomous
	if !one.CapacityStrictNodeLimitOn() || one.CapacityCountPoolByNodeOn() || !one.CapacityAdoptBeforeAdmitOn() {
		t.Errorf("one setting off: strict=%v count_by_node=%v adopt_first=%v, want true/false/true",
			one.CapacityStrictNodeLimitOn(), one.CapacityCountPoolByNodeOn(), one.CapacityAdoptBeforeAdmitOn())
	}
}

// A negative adoption wait is rejected at load; 0 reads as the default.
func TestCapacityNodeLimit_AdoptionWaitValidation(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte("autonomous:\n  capacity_adoption_wait_seconds: -1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err == nil {
		t.Error("Load accepted a negative capacity_adoption_wait_seconds")
	}
	a := loadYAML(t, "autonomous:\n  capacity_adoption_wait_seconds: 0\n").Autonomous
	if a.CapacityAdoptionWait() != 30*time.Second {
		t.Errorf("0 reads as %v, want the 30s default", a.CapacityAdoptionWait())
	}
}
