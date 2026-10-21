// Deterministic synchronisation helpers for session tests, replacing fixed
// time.Sleep waits with bounded polls on an observable condition.

package session

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"
)

// failer is the subset of testing.TB that pollUntil needs, so the timeout
// path can be asserted with a recorder.
type failer interface {
	Helper()
	Fatalf(format string, args ...any)
}

// pollUntil polls cond every ~5ms until it returns true, and fails via
// t.Fatalf(msg) if timeout elapses first.
func pollUntil(t failer, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s", msg)
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// captureLoopCount returns how many StartScreenCapture goroutines created by
// the calling goroutine are alive. StartScreenCapture exposes no done channel
// and production code must not change, so a full goroutine stack scan is the
// observable signal. Loops spawned by other tests or by Manager.Start carry a
// different creator goroutine id and are ignored.
func captureLoopCount() int {
	self := make([]byte, 64)
	self = self[:runtime.Stack(self, false)]
	id := strings.Fields(string(self))[1]
	marker := "Manager).StartScreenCapture in goroutine " + id + "\n"
	buf := make([]byte, 1<<20)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			return strings.Count(string(buf[:n])+"\n", marker)
		}
		buf = make([]byte, 2*len(buf))
	}
}

// waitCaptureLoopsExit blocks until none of the calling goroutine's capture
// loops remain.
func waitCaptureLoopsExit(t failer, timeout time.Duration) {
	t.Helper()
	pollUntil(t, timeout, func() bool { return captureLoopCount() == 0 },
		"screen-capture goroutines did not exit")
}

// mutatePane appends a newline to the fake pane so the capture loop sees a
// changed screen and re-runs detection.
func mutatePane(fake *FakeTmux, session string) {
	fake.mu.Lock()
	fake.Pane[session] += "\n"
	fake.mu.Unlock()
}

type recordFailer struct {
	helpers int
	msg     string
	failed  bool
}

func (r *recordFailer) Helper() { r.helpers++ }
func (r *recordFailer) Fatalf(format string, args ...any) {
	r.failed = true
	r.msg = format
}

func TestPollUntil_ReturnsOnceCondTrue(t *testing.T) {
	start := time.Now()
	n := 0
	rec := &recordFailer{}
	pollUntil(rec, 10*time.Second, func() bool { n++; return n >= 3 }, "never")
	if rec.failed {
		t.Fatalf("unexpected failure: %s", rec.msg)
	}
	if n != 3 {
		t.Errorf("cond called %d times, want 3", n)
	}
	if time.Since(start) > 5*time.Second {
		t.Error("pollUntil did not return promptly")
	}
}

func TestPollUntil_TimeoutFails(t *testing.T) {
	rec := &recordFailer{}
	pollUntil(rec, 30*time.Millisecond, func() bool { return false }, "cond never true")
	if !rec.failed || rec.msg != "%s" {
		t.Fatalf("expected Fatalf on timeout, got failed=%v", rec.failed)
	}
}

func TestCaptureLoop_ExitsAfterCancel(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)
	sess := newSelfUpdateSession(t, mgr, StateRunning)
	fake.Pane[sess.TmuxSession] = "hello"

	ctx, cancel := context.WithCancel(context.Background())
	mgr.StartScreenCapture(ctx, sess.FullID, 5)
	pollUntil(t, 10*time.Second, func() bool { return captureLoopCount() > 0 }, "capture loop never started")
	mutatePane(fake, sess.TmuxSession)
	cancel()
	waitCaptureLoopsExit(t, 10*time.Second)
}
