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
