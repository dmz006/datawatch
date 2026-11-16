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

// pollPRDTerminal waits until the PRD is completed or failed. On timeout it
// reports the last observed status, so a CI failure shows exactly where the
// PRD got stuck (e.g. "stuck at PRDBlocked — the boot-resume scan parked it")
// instead of a bare deadline-missed message.
func pollPRDTerminal(t *testing.T, m *Manager, id string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last string
	for {
		if got, ok := m.Store().SnapshotPRD(id); ok {
			last = string(got.Status)
			if got.Status == PRDCompleted || got.Status == PRDFailed {
				return
			}
		} else if last == "" {
			last = "<missing>"
		}
		if time.Now().After(deadline) {
			t.Fatalf("pollPRDTerminal: PRD %s not terminal within %s (last observed status: %s)", id, timeout, last)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// settledCount waits until count() reaches want (bounded by a 10s deadline,
// so a slow dispatcher under -race cannot fail the test early), then waits
// a short settle window so an extra, unwanted delivery still shows up, and
// returns the final count. With want == 0 it only waits the settle window:
// an absence check cannot be proven by a signal, so the window bounds it.
func settledCount(t *testing.T, want int, count func() int) int {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for count() < want && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	return count()
}
