package router

import (
	"io"
	"net/http"
	"testing"
	"time"
)

// TestAutomationCommFixtureDrainsLateRequests pins the fixture guard: a
// request that reached the fake daemon but is recorded later than
// HandleTestMessage's fixed 200 ms wait must not be counted in the next
// input's window. controlLog.reset has to wait for it before clearing.
func TestAutomationCommFixtureDrainsLateRequests(t *testing.T) {
	calls := newControlLog()
	reached := make(chan struct{}, 1)
	srv, port := newFakePeerParent(t, func(w http.ResponseWriter, r *http.Request) {
		calls.begin()
		_, _ = io.Copy(io.Discard, r.Body)
		if r.URL.Path == "/late" {
			reached <- struct{}{}
			time.Sleep(300 * time.Millisecond) // longer than HandleTestMessage's 200 ms wait
		}
		calls.add(controlCall{method: r.Method, path: r.URL.Path})
		_, _ = w.Write([]byte(`{"id":"ab12","status":"running","version":9}`))
	})
	r := setupRetryRouter(t, port)

	// Send the late request and wait until it is inside the handler, so it is
	// counted as in flight but not yet recorded.
	lateDone := make(chan struct{})
	go func() {
		defer close(lateDone)
		resp, err := http.Get(srv.URL + "/late")
		if err == nil {
			_ = resp.Body.Close()
		}
	}()
	select {
	case <-reached:
	case <-time.After(5 * time.Second):
		t.Fatal("late request never reached the handler")
	}

	// The next input: reset must drain the late request before clearing.
	calls.reset()
	r.HandleTestMessage("automation set-llm ab12 model=sonnet")
	select {
	case <-lateDone:
	case <-time.After(5 * time.Second):
		t.Fatal("late request never finished")
	}

	for _, c := range calls.snapshot() {
		if c.path == "/late" {
			t.Fatalf("late request leaked into the next input's window: %+v", calls.snapshot())
		}
	}
	if seen := calls.snapshot(); len(seen) != 1 {
		t.Fatalf("next input made %d calls, want 1: %+v", len(seen), seen)
	}
}
