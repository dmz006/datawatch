package config

import "testing"

func TestValidateLoadedRefusesUnknownMergeConflictPolicy(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Autonomous.Parallel.MergeConflictPolicy = "nope"
	if err := validateLoaded(cfg); err == nil {
		t.Fatal("validateLoaded must refuse an unknown merge_conflict_policy")
	}
	cfg.Autonomous.ProjectWriteSerialization = "advisory"
	cfg.Autonomous.Parallel.MergeConflictPolicy = "triage"
	if err := validateLoaded(cfg); err != nil {
		t.Fatalf("valid policy refused: %v", err)
	}
}

func TestMergeRetrySeconds(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Autonomous.ProjectWriteSerialization = "advisory"
	// default: absent is valid
	if cfg.Autonomous.Parallel.MergeRetrySeconds != nil {
		t.Fatalf("default must be unset, got %v", *cfg.Autonomous.Parallel.MergeRetrySeconds)
	}
	if err := validateLoaded(cfg); err != nil {
		t.Fatalf("default refused: %v", err)
	}
	// set: 0 (off) and a positive value are valid
	for _, n := range []int{0, 30} {
		v := n
		cfg.Autonomous.Parallel.MergeRetrySeconds = &v
		if err := validateLoaded(cfg); err != nil {
			t.Fatalf("%d refused: %v", n, err)
		}
	}
	// invalid: negative
	neg := -1
	cfg.Autonomous.Parallel.MergeRetrySeconds = &neg
	if err := validateLoaded(cfg); err == nil {
		t.Fatal("validateLoaded must refuse a negative merge_retry_seconds")
	}
}
