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
