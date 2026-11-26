package autonomous

import (
	"context"
	"testing"
)

// A cluster worker makes its own push, so the Automaton's push-on-complete
// override travels with the spawn.
func TestRun_ClusterSpawnCarriesPushOverride(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	prd := newApprovedClusterPRD(t, m, "k8s-default")
	if _, err := m.SetPRDGitPush(prd.ID, "never"); err != nil {
		t.Fatal(err)
	}
	got := "unset"
	spawn := func(_ context.Context, req SpawnRequest) (SpawnResult, error) {
		got = req.PushOnComplete
		return SpawnResult{SessionID: "s1"}, nil
	}
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		return VerificationResult{OK: true, Summary: "ok"}, nil
	}
	if err := m.Run(context.Background(), prd.ID, spawn, verify); err != nil {
		t.Fatal(err)
	}
	if got != "never" {
		t.Errorf("SpawnRequest.PushOnComplete = %q, want never", got)
	}
}
