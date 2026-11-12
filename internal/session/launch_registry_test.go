package session

import (
	"context"
	"errors"
	"testing"
	"time"
)

// startInBackground runs Start on its own goroutine so a test can drain while
// the launch is still in flight.
func startInBackground(mgr *Manager, dir string, launch LaunchFunc) <-chan error {
	done := make(chan error, 1)
	go func() {
		_, err := mgr.Start(context.Background(), "task", "", dir, &StartOptions{LaunchFn: launch})
		done <- err
	}()
	return done
}

// waitForRegisteredLaunch blocks until one launch is registered and returns its full ID.
func waitForRegisteredLaunch(t *testing.T, mgr *Manager) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		mgr.mu.Lock()
		for fullID := range mgr.launching {
			mgr.mu.Unlock()
			return fullID
		}
		mgr.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("no launch registered within 5s")
	return ""
}

func TestLaunchRegistry_FinishesInsideGraceNotReturned(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)
	dir := t.TempDir()

	// The launch completes after a short delay, well inside the grace.
	launch := func(ctx context.Context, _, _, _, _ string) error {
		time.Sleep(150 * time.Millisecond)
		return nil
	}
	done := startInBackground(mgr, dir, launch)
	waitForRegisteredLaunch(t, mgr)

	rolled := mgr.DrainLaunches(3 * time.Second)
	if len(rolled) != 0 {
		t.Fatalf("launch finished inside grace but was rolled back: %v", rolled)
	}
	if err := <-done; err != nil {
		t.Fatalf("Start: %v", err)
	}
	if n := mgr.launchCount(); n != 0 {
		t.Errorf("registry not empty after drain: %d launches", n)
	}
}

func TestLaunchRegistry_CancelsAndRollsBackAtDeadline(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)
	dir := t.TempDir()

	// The launch blocks until its context is cancelled, then reports the cancel.
	launch := func(ctx context.Context, _, _, _, _ string) error {
		<-ctx.Done()
		return ctx.Err()
	}
	done := startInBackground(mgr, dir, launch)
	fullID := waitForRegisteredLaunch(t, mgr)

	rolled := mgr.DrainLaunches(100 * time.Millisecond)
	if len(rolled) != 1 || rolled[0] != fullID {
		t.Fatalf("rolled-back ids = %v, want [%s]", rolled, fullID)
	}
	if err := <-done; err == nil {
		t.Fatal("Start returned nil error for a cancelled launch")
	}

	sess, ok := mgr.store.Get(fullID)
	if !ok {
		t.Fatalf("session %s not in store after rollback", fullID)
	}
	if sess.State != StateKilled {
		t.Errorf("session state = %q, want %q", sess.State, StateKilled)
	}
	if fake.Count("kill") == 0 {
		t.Errorf("expected the session's pane to be killed; calls: %+v", fake.Calls)
	}
	if n := mgr.launchCount(); n != 0 {
		t.Errorf("registry not empty after rollback: %d launches", n)
	}
}

func TestLaunchRegistry_StartRefusedWhileDraining(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)
	dir := t.TempDir()

	if rolled := mgr.DrainLaunches(0); len(rolled) != 0 {
		t.Fatalf("empty drain rolled back %v", rolled)
	}

	launched := false
	launch := func(ctx context.Context, _, _, _, _ string) error {
		launched = true
		return nil
	}
	_, err := mgr.Start(context.Background(), "task", "", dir, &StartOptions{LaunchFn: launch})
	if !errors.Is(err, ErrDaemonDraining) {
		t.Fatalf("Start during drain: err = %v, want ErrDaemonDraining", err)
	}
	if launched {
		t.Error("launch function ran during drain")
	}
	if n := len(mgr.store.List()); n != 0 {
		t.Errorf("drain refusal left %d sessions in the store", n)
	}
	if len(fake.Calls) != 0 {
		t.Errorf("drain refusal touched tmux: %+v", fake.Calls)
	}
}

func TestLaunchRegistry_EmptyAfterErrorPath(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)
	dir := t.TempDir()

	launch := func(ctx context.Context, _, _, _, _ string) error {
		return errors.New("backend refused to start")
	}
	if _, err := mgr.Start(context.Background(), "task", "", dir, &StartOptions{LaunchFn: launch}); err == nil {
		t.Fatal("Start returned nil error for a failing launch")
	}
	if n := mgr.launchCount(); n != 0 {
		t.Errorf("registry not empty after error-path Start: %d launches", n)
	}
	mgr.mu.Lock()
	leftover := len(mgr.launchSessions)
	mgr.mu.Unlock()
	if leftover != 0 {
		t.Errorf("launchSessions not cleared after error-path Start: %d", leftover)
	}
}
