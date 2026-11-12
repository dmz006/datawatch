package session

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// startMarkerSubprocess starts a subprocess session whose task waits (bounded)
// for a marker file and then exits 0. It returns the session and the marker
// path; the test creates the marker to let the task finish.
func startMarkerSubprocess(t *testing.T) (*Manager, *Session, string) {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skipf("bash not available: %v", err)
	}
	mgr, _ := newTestManagerWithFake(t)
	mgr.SetConfig(config.DefaultConfig())

	marker := filepath.Join(t.TempDir(), "release.marker")
	// The task polls for the marker for at most 30 seconds, so a test that
	// fails before creating it cannot leave the child running for long.
	task := fmt.Sprintf(`for i in $(seq 1 300); do [ -f %q ] && exit 0; sleep 0.1; done; exit 1`, marker)

	sess, err := mgr.Start(context.Background(), task, "", t.TempDir(), &StartOptions{Subprocess: true})
	if err != nil {
		t.Fatalf("Start subprocess session: %v", err)
	}
	t.Cleanup(func() {
		// Make sure the child ends even if the test failed early.
		_ = os.WriteFile(marker, nil, 0o644)
		waitForRunnerState(t, mgr, sess.FullID, false)
	})
	return mgr, sess, marker
}

// waitForRunnerState polls HasRunner until it equals want, within a bound.
func waitForRunnerState(t *testing.T, m *Manager, fullID string, want bool) bool {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if m.HasRunner(fullID) == want {
			return true
		}
		time.Sleep(25 * time.Millisecond)
	}
	return m.HasRunner(fullID) == want
}

// A subprocess session is held as a live runner for as long as its task runs.
func TestSubprocessSessionHoldsRunnerWhileRunning(t *testing.T) {
	mgr, sess, marker := startMarkerSubprocess(t)

	// Registered before Start returned, so it is visible immediately.
	if !mgr.HasRunner(sess.FullID) {
		t.Fatal("a freshly started subprocess session must be held as a live runner")
	}

	// Give the task time to be well into its wait; it must still be held.
	time.Sleep(500 * time.Millisecond)
	if !mgr.HasRunner(sess.FullID) {
		t.Fatal("runner released while the subprocess task is still waiting")
	}
	if cur, ok := mgr.store.Get(sess.FullID); !ok || cur.State != StateRunning {
		t.Fatalf("session should still be running while the task waits; got %+v", cur)
	}

	// Let it finish so the goroutine ends before the temp directories go.
	if err := os.WriteFile(marker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if !waitForRunnerState(t, mgr, sess.FullID, false) {
		t.Fatal("runner was never released after the task finished")
	}
}

// When the task ends and the session finishes, the runner is released.
func TestSubprocessSessionReleasesRunnerWhenDone(t *testing.T) {
	mgr, sess, marker := startMarkerSubprocess(t)

	if !mgr.HasRunner(sess.FullID) {
		t.Fatal("precondition: runner must be held while the task waits")
	}

	if err := os.WriteFile(marker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	waitForState(t, mgr, sess.FullID, StateComplete)

	// The runner is released when runSubprocess returns, just after the
	// session is saved as complete, so allow for that short gap.
	if !waitForRunnerState(t, mgr, sess.FullID, false) {
		t.Fatal("HasRunner must be false once the subprocess session has finished")
	}
}
