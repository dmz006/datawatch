package autonomous

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/git"
)

// The two PRD writes below historically mutated the shared stored *PRD
// without the store lock: recordPushOutcome (git_push_state.go) and the
// applyParallelMetrics step of the completion roll-up (executor.go). Both are
// now wrapped in Store.WithLock. These tests hammer Store.SnapshotPRD — a
// deep copy that reads every PRD field under the store lock — at the same
// time each writer runs, so if either write ever regresses to an unlocked
// mutation, `go test -race` reports a data race on the PRD fields.

// TestStoreLockedWrites_PushOutcomeVsSnapshot runs recordPushOutcome on a
// shared stored *PRD in a loop while the test goroutine takes full PRD
// snapshots until the writer finishes.
func TestStoreLockedWrites_PushOutcomeVsSnapshot(t *testing.T) {
	m, _ := newFailureEscalationFixture(t, t.TempDir())
	prd := newFailureEscalationPRD(t, m, t.TempDir(), nil)
	saved, ok := m.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatal("stored PRD missing")
	}
	// Keep the not-pushed branches quiet.
	m.SetAlertSink(func(string, string, string) string { return "" })

	const iterations = 200
	pushed := git.PushResult{Pushed: true, Ref: "refs/heads/x", SHA: "abc"}
	notPushed := git.PushResult{Reason: "no remote"}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < iterations; i++ {
			if i%2 == 0 {
				m.recordPushOutcome(saved, pushed, git.PushBranch)
			} else {
				m.recordPushOutcome(saved, notPushed, git.PushBranch)
			}
		}
	}()
	for {
		if _, ok := m.Store().SnapshotPRD(prd.ID); !ok {
			t.Fatal("stored PRD missing while writer was running")
		}
		select {
		case <-done:
			// The last iteration (199) was the not-pushed result.
			final, ok := m.Store().SnapshotPRD(prd.ID)
			if !ok || final.Git.PushState != PushStateNotPushed {
				t.Fatalf("final PushState = %+v, want %q", final, PushStateNotPushed)
			}
			if final.Git.PushWarning != "not pushed: no remote" {
				t.Fatalf("final PushWarning = %q, want %q", final.Git.PushWarning, "not pushed: no remote")
			}
			return
		default:
		}
	}
}

// TestStoreLockedWrites_CompletionMetricsVsSnapshot drives a one-task PRD to
// completion with a fake spawn and a passing verify. While the executor runs
// (including the applyParallelMetrics write on the completion path) the test
// goroutine keeps taking full PRD snapshots until it observes the stored
// metrics, so an unlocked metrics write races the snapshot copy.
func TestStoreLockedWrites_CompletionMetricsVsSnapshot(t *testing.T) {
	m, api := newFailureEscalationFixture(t, t.TempDir())
	m.SetAlertSink(func(string, string, string) string { return "" })

	spawn := func(_ context.Context, _ SpawnRequest) (SpawnResult, error) {
		return SpawnResult{SessionID: "sess-1"}, nil
	}
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		return VerificationResult{OK: true, Summary: "all good"}, nil
	}

	prd := newFailureEscalationPRD(t, m, t.TempDir(), nil)
	api.SetExecutors(spawn, verify)
	if err := api.Run(prd.ID); err != nil {
		t.Fatalf("Run: %v", err)
	}

	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(done)
		for {
			sn, ok := m.Store().SnapshotPRD(prd.ID)
			if !ok {
				continue
			}
			if sn.Status == PRDCompleted && sn.TotalTasks == 1 {
				return
			}
		}
	}()

	pollPRDTerminal(t, m, prd.ID, 10*time.Second)
	wg.Wait()

	got, ok := m.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatal("stored PRD missing after completion")
	}
	if got.Status != PRDCompleted {
		t.Fatalf("Status = %q, want %q", got.Status, PRDCompleted)
	}
	if got.TotalTasks != 1 {
		t.Fatalf("TotalTasks = %d, want 1", got.TotalTasks)
	}
}
