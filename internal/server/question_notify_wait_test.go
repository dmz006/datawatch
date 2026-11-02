package server

// Regression test for the unawaited raise-notify alert write: a
// high-priority raise fans out in its own goroutine and its last leg is
// the alert store's Add (which rewrites the alerts JSON file and invokes
// the store's listeners). Nothing else in the raise path waits for that
// goroutine, so before Server.WaitQuestionNotifies existed a caller
// that read the alert store or listened on it right after the 201 could
// race the write. This test makes the leg's completion observable from
// the outside — a listener registered with the alert store that lingers
// 300 milliseconds before signalling — and asserts that the wait bars
// it: after WaitQuestionNotifies returns, the listener has run exactly
// once and the alert has landed in the store. Stub the wait out
// (empty body) and this test has to fail, because the assertion lands
// long before the listener's 300 ms linger has finished.

import (
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/alerts"
)

func TestQuestionNotifyWaitsForAlertWrite(t *testing.T) {
	f := qstep2New(t, true)

	// The listener runs inside the raise fan-out goroutine, after the
	// alert is saved (alerts.Store invokes its listeners from Add). The
	// linger is what makes a missing wait observable: it keeps the
	// counter at zero well past the moment an unwaited assertion would
	// run.
	var seen int64
	f.ast.AddListener(func(a *alerts.Alert) {
		if a == nil {
			t.Error("listener got a nil alert")
			return
		}
		time.Sleep(300 * time.Millisecond)
		atomic.AddInt64(&seen, 1)
	})

	// Raise a high-priority question through the real handler: the 201
	// returns before the fan-out goroutine's legs (hub, router, then
	// alert store) have finished.
	rr := doQ(t, f.s, http.MethodPost, "/api/questions", qstep2Body())
	if rr.Code != http.StatusCreated {
		t.Fatalf("POST /api/questions → %d (%s), want 201", rr.Code, rr.Body.String())
	}

	// The wait under test: it must block until this raise's fan-out has
	// run to completion, including the alert write and its listeners.
	f.s.WaitQuestionNotifies()

	if got := atomic.LoadInt64(&seen); got != 1 {
		t.Fatalf("alert listener fired %d times after WaitQuestionNotifies, want 1 (wait did not bar the in-flight raise fan-out)", got)
	}
	if got := len(f.ast.List()); got != 1 {
		t.Fatalf("alerts after WaitQuestionNotifies = %d, want 1", got)
	}
}
