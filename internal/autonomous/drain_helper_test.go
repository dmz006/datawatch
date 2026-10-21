package autonomous

import (
	"testing"
	"time"
)

// drainOnCleanup registers a cleanup that cancels every in-flight executor
// goroutine, waits for them to exit, then stops the Manager. Cleanups run
// LIFO, so call this AFTER t.TempDir() has been created: the drain then runs
// before the temp dir is removed and no executor writes into a deleted dir.
// Manager.Stop is safe when Start was never called (it no-ops on a nil cancel).
func drainOnCleanup(t *testing.T, m *Manager, api *API) {
	t.Helper()
	t.Cleanup(func() {
		if err := api.WaitIdle(10 * time.Second); err != nil {
			t.Errorf("drainOnCleanup: %v", err)
		}
		m.Stop()
	})
}

// pollUntil polls cond every few milliseconds until it holds, failing the
// test loudly when timeout elapses first. Use it instead of time.Sleep when
// waiting for a state change.
func pollUntil(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("pollUntil: %s not reached within %s", what, timeout)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// pollPRDTerminal waits until the PRD is completed or failed.
func pollPRDTerminal(t *testing.T, m *Manager, id string, timeout time.Duration) {
	t.Helper()
	pollUntil(t, timeout, "PRD "+id+" terminal status", func() bool {
		got, ok := m.Store().SnapshotPRD(id)
		return ok && (got.Status == PRDCompleted || got.Status == PRDFailed)
	})
}
