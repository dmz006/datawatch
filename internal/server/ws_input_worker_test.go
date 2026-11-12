package server

import (
	"strconv"
	"testing"
	"time"
)

// TestClientInputWorker_FIFO verifies that enqueued input jobs run in the
// order they were enqueued, and that awaitInputForSession blocks a "later
// plain" input until the earlier (slow) one for the same session completes
// — i.e. a later message can never overtake an earlier image message.
func TestClientInputWorker_FIFO(t *testing.T) {
	c := &client{send: make(chan []byte, 16), subscribed: make(map[string]bool)}

	var order []string
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 3; i++ {
			i := i
			if !c.enqueueInput("sess-1", func() {
				// Simulate slow image expansion.
				time.Sleep(20 * time.Millisecond)
				order = append(order, "job-"+strconv.Itoa(i))
			}) {
				t.Errorf("enqueue %d: accepted=false", i)
				return
			}
			// A "later plain message" for the same session must wait for
			// every earlier job for that session to complete.
			if i < 2 {
				c.awaitInputForSession("sess-1")
				order = append(order, "plain-"+strconv.Itoa(i))
			}
		}
	}()

	// Stop (drains what is queued) while/after the producer finishes.
	<-done
	c.stopInputWorker()

	want := []string{"job-0", "plain-0", "job-1", "plain-1", "job-2"}
	if len(order) != len(want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
}

// TestClientInputWorker_AwaitOnlyBlocksSameSession verifies that a plain
// input for a *different* session does not wait on a pending job for
// another session (FIFO is per session, not per connection), while a plain
// input for the same session waits for the slow job to finish.
func TestClientInputWorker_AwaitOnlyBlocksSameSession(t *testing.T) {
	c := &client{send: make(chan []byte, 16), subscribed: make(map[string]bool)}

	started := make(chan struct{})
	c.enqueueInput("sess-a", func() {
		close(started)
		time.Sleep(100 * time.Millisecond)
	})
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("queued job did not start")
	}

	// Different session: must not block on sess-a's pending job.
	waited := make(chan struct{})
	go func() {
		c.awaitInputForSession("sess-b")
		close(waited)
	}()
	select {
	case <-waited:
	case <-time.After(time.Second):
		t.Fatal("await for different session blocked on another session's job")
	}

	// Same session: must wait for the slow job to finish.
	start := time.Now()
	c.awaitInputForSession("sess-a")
	if time.Since(start) < 50*time.Millisecond {
		t.Fatal("await for same session returned before pending job completed")
	}

	c.stopInputWorker()
}

// TestClientInputWorker_StopDrainsThenRejects verifies that an in-flight
// job still runs to completion after stop (accepted input is delivered),
// that a second stop is a safe no-op, and that a later enqueue is rejected
// without blocking await.
func TestClientInputWorker_StopDrainsThenRejects(t *testing.T) {
	c := &client{send: make(chan []byte, 16), subscribed: make(map[string]bool)}

	ran := make(chan struct{})
	c.enqueueInput("sess-1", func() { close(ran) })

	c.stopInputWorker()

	select {
	case <-ran:
	case <-time.After(time.Second):
		t.Fatal("accepted job did not run before worker stopped")
	}

	c.stopInputWorker() // idempotent

	if c.enqueueInput("sess-1", func() {}) {
		t.Fatal("enqueue after stop was accepted")
	}
	c.awaitInputForSession("sess-1") // must not deadlock
}
