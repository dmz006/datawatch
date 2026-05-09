// v7.0.0 S4 — SSE infrastructure tests.

package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSSEHub_PublishToSubscriber(t *testing.T) {
	h := NewSSEHub()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	ctx, cancel := newTestCtx()
	req = req.WithContext(ctx)

	done := make(chan struct{})
	go func() {
		_, _ = h.Subscribe("topic-1", rec, req)
		close(done)
	}()

	// Wait until subscriber is registered.
	for i := 0; i < 50; i++ {
		if h.SubscriberCount("topic-1") > 0 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if h.SubscriberCount("topic-1") != 1 {
		t.Fatalf("subscriber not registered")
	}

	h.Publish("topic-1", "test_event", map[string]any{"hello": "world"})
	time.Sleep(20 * time.Millisecond) // let writer flush
	cancel()
	<-done

	body := rec.Body.String()
	if !strings.Contains(body, "event: hello") {
		t.Fatalf("missing hello frame: %q", body)
	}
	if !strings.Contains(body, "event: test_event") {
		t.Fatalf("missing event: %q", body)
	}
	if !strings.Contains(body, `"hello":"world"`) {
		t.Fatalf("missing payload: %q", body)
	}
}

func TestSSEHub_MultipleSubscribers(t *testing.T) {
	h := NewSSEHub()
	const N = 5
	var wg sync.WaitGroup
	cancels := make([]func(), N)
	for i := 0; i < N; i++ {
		i := i
		wg.Add(1)
		ctx, cancel := newTestCtx()
		cancels[i] = cancel
		go func() {
			defer wg.Done()
			rec := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/test", nil).WithContext(ctx)
			_, _ = h.Subscribe("multi", rec, req)
			_ = i
		}()
	}
	// Wait for all subscribers.
	for i := 0; i < 50; i++ {
		if h.SubscriberCount("multi") == N {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if got := h.SubscriberCount("multi"); got != N {
		t.Fatalf("subscribers: got %d, want %d", got, N)
	}
	h.Publish("multi", "broadcast", "hi")
	time.Sleep(30 * time.Millisecond)
	for _, c := range cancels {
		c()
	}
	wg.Wait()
}

func TestSSEHub_CloseTopic(t *testing.T) {
	h := NewSSEHub()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	ctx, cancel := newTestCtx()
	defer cancel()
	req = req.WithContext(ctx)

	done := make(chan struct{})
	go func() {
		_, _ = h.Subscribe("close-me", rec, req)
		close(done)
	}()
	for i := 0; i < 50; i++ {
		if h.SubscriberCount("close-me") > 0 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	h.CloseTopic("close-me")
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("subscriber did not exit on CloseTopic")
	}
	if !strings.Contains(rec.Body.String(), "event: close") {
		t.Fatalf("missing close frame: %q", rec.Body.String())
	}
}

func TestSSEHub_NoSubscribersIsNoOp(t *testing.T) {
	h := NewSSEHub()
	// Publish to nothing — must not panic / block.
	h.Publish("ghost", "x", "y")
}

func TestSSEHub_RemovedAfterDisconnect(t *testing.T) {
	h := NewSSEHub()
	ctx, cancel := newTestCtx()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil).WithContext(ctx)
	done := make(chan struct{})
	go func() {
		_, _ = h.Subscribe("disco", rec, req)
		close(done)
	}()
	for i := 0; i < 50; i++ {
		if h.SubscriberCount("disco") > 0 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	cancel()
	<-done
	if got := h.SubscriberCount("disco"); got != 0 {
		t.Fatalf("subscriber not pruned after disconnect: %d", got)
	}
}

// Helper.
func newTestCtx() (ctx ctxBundle, cancel func()) {
	stop := make(chan struct{})
	return ctxBundle{stop: stop}, func() {
		select {
		case <-stop:
		default:
			close(stop)
		}
	}
}

type ctxBundle struct {
	stop chan struct{}
}

func (c ctxBundle) Deadline() (time.Time, bool) { return time.Time{}, false }
func (c ctxBundle) Done() <-chan struct{}        { return c.stop }
func (c ctxBundle) Err() error {
	select {
	case <-c.stop:
		return http.ErrAbortHandler
	default:
		return nil
	}
}
func (c ctxBundle) Value(key any) any { return nil }
