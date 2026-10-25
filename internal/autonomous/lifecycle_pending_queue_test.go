package autonomous

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func pendingEv(id string) LifecycleEvent {
	return LifecycleEvent{ID: id, Kind: KindPRDUpdated, Level: LevelPRD, PRDID: "p"}
}

func TestPendingQueueCursorMonotonic(t *testing.T) {
	q := NewPendingEventQueue(10)
	var last uint64
	for i := 0; i < 5; i++ {
		// alternate queues: the cursor is shared and still strictly rising
		c := q.Append(fmt.Sprintf("q%d", i%2), pendingEv("e"))
		if c <= last {
			t.Fatalf("cursor %d not greater than %d", c, last)
		}
		last = c
	}
	if first := NewPendingEventQueue(1).Append("a", pendingEv("x")); first != 1 {
		t.Errorf("first cursor = %d, want 1", first)
	}
}

func TestPendingQueueDefaultMax(t *testing.T) {
	for _, m := range []int{0, -3} {
		if got := NewPendingEventQueue(m).Max(); got != DefaultPendingEventsMax {
			t.Errorf("max %d → %d, want %d", m, got, DefaultPendingEventsMax)
		}
	}
}

func TestPendingQueueEvictionAndDropped(t *testing.T) {
	q := NewPendingEventQueue(3)
	for i := 1; i <= 5; i++ {
		q.Append("a", pendingEv(fmt.Sprintf("e%d", i)))
	}
	evs, next, dropped := q.Pending("a", 0, 0)
	if !dropped {
		t.Error("dropped = false for a cursor older than the oldest retained entry")
	}
	if len(evs) != 3 || evs[0].ID != "e3" || evs[2].ID != "e5" || next != 5 {
		t.Errorf("got %v next=%d, want e3..e5 next=5", evs, next)
	}
	// A caller caught up to the eviction point sees no drop.
	_, _, dropped = q.Pending("a", 2, 0)
	if dropped {
		t.Error("dropped = true for a cursor exactly at the eviction boundary")
	}
	// A queue that never evicted never reports a drop, even with a gap
	// from other queues' cursors.
	q.Append("b", pendingEv("b1"))
	if _, _, d := q.Pending("b", 0, 0); d {
		t.Error("dropped = true on a queue that never evicted")
	}
}

func TestPendingQueueLimitAndSinceExclusive(t *testing.T) {
	q := NewPendingEventQueue(10)
	for i := 1; i <= 5; i++ {
		q.Append("a", pendingEv(fmt.Sprintf("e%d", i)))
	}
	evs, next, _ := q.Pending("a", 0, 2)
	if len(evs) != 2 || evs[1].ID != "e2" || next != 2 {
		t.Fatalf("limit page: %v next=%d", evs, next)
	}
	evs, next, _ = q.Pending("a", next, 2) // exclusive: resumes at e3
	if len(evs) != 2 || evs[0].ID != "e3" || next != 4 {
		t.Fatalf("second page: %v next=%d", evs, next)
	}
	evs, next, _ = q.Pending("a", 5, 10)
	if len(evs) != 0 || next != 5 {
		t.Errorf("caught up: %v next=%d, want empty next=5", evs, next)
	}
	if evs, next, _ := q.Pending("none", 7, 10); evs != nil || next != 7 {
		t.Errorf("unknown queue: %v next=%d", evs, next)
	}
}

func TestPendingQueueConcurrent(t *testing.T) {
	q := NewPendingEventQueue(50)
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				q.Append("a", pendingEv("e"))
				q.Pending("a", 0, 10)
			}
		}()
	}
	wg.Wait()
	evs, _, _ := q.Pending("a", 0, 0)
	if len(evs) != 50 {
		t.Errorf("retained %d, want 50", len(evs))
	}
}

func TestPendingQueueRelease(t *testing.T) {
	q := NewPendingEventQueue(5)
	q.Append("a", pendingEv("e"))
	q.Release("a")
	if q.Queues() != 0 {
		t.Errorf("queues = %d after release", q.Queues())
	}
}

// newQueueEngine builds an engine with a pending queue wired in.
func newQueueEngine(t *testing.T, pq *PendingEventQueue) (*Manager, *SubscriptionStore) {
	t.Helper()
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	// startedEvent() (shared with the session-push tests) belongs to "prd-1":
	// opt that Automaton into task.session_started so the queue tests see
	// events the resolver permits.
	if err := m.Store().SavePRD(&PRD{ID: "prd-1", NotifyEvents: []string{KindTaskSessionStarted}}); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	store, err := NewSubscriptionStore(t.TempDir()+"/subs.json", nil)
	if err != nil {
		t.Fatalf("NewSubscriptionStore: %v", err)
	}
	e := NewLifecycleSubscriptionEngine(m, store, SessionPushConfig{PendingQueue: pq})
	if err := e.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		e.Close()
		m.lifecycleBus().Close()
	})
	return m, store
}

func queueSub(oneShot bool) Subscription {
	return Subscription{
		Filter:  LifecycleFilter{Kinds: []string{KindTaskSessionStarted}},
		Sink:    SubscriptionSink{Type: SinkMCPQueue, Target: "agent-1"},
		OneShot: oneShot,
	}
}

func TestMCPQueueSinkAppendsMatchingOnly(t *testing.T) {
	pq := NewPendingEventQueue(10)
	m, store := newQueueEngine(t, pq)
	if _, err := store.Add(queueSub(false)); err != nil {
		t.Fatalf("Add: %v", err)
	}
	m.emit(LifecycleEvent{Kind: KindPRDUpdated, Level: LevelPRD, PRDID: "prd-9"}) // non-matching
	m.emit(startedEvent())
	waitFor(t, "queue append", func() bool {
		evs, _, _ := pq.Pending("agent-1", 0, 0)
		return len(evs) == 1
	})
	evs, _, _ := pq.Pending("agent-1", 0, 0)
	if evs[0].Kind != KindTaskSessionStarted {
		t.Errorf("queued kind %q, want %q", evs[0].Kind, KindTaskSessionStarted)
	}
}

func TestMCPQueueSinkOneShotConsumed(t *testing.T) {
	pq := NewPendingEventQueue(10)
	m, store := newQueueEngine(t, pq)
	sub, err := store.Add(queueSub(true))
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	m.emit(startedEvent())
	waitFor(t, "one-shot consumed", func() bool { _, ok := store.Get(sub.ID); return !ok })
	if evs, _, _ := pq.Pending("agent-1", 0, 0); len(evs) != 1 {
		t.Errorf("queued %d events, want 1 (append precedes consume)", len(evs))
	}
}

func TestMCPQueueSinkReleasedOnDelete(t *testing.T) {
	pq := NewPendingEventQueue(10)
	m, store := newQueueEngine(t, pq)
	a, _ := store.Add(queueSub(false))
	b, _ := store.Add(queueSub(false)) // second sub feeding the same queue
	m.emit(startedEvent())
	waitFor(t, "append", func() bool { return pq.Queues() == 1 })
	if err := store.Delete(a.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if pq.Queues() != 1 {
		t.Error("queue released while another subscription still feeds it")
	}
	if err := store.Delete(b.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	waitFor(t, "release", func() bool { return pq.Queues() == 0 })
}

func TestMCPQueueSinkNilQueueDropsSilently(t *testing.T) {
	m, store := newQueueEngine(t, nil)
	if _, err := store.Add(queueSub(false)); err != nil {
		t.Fatalf("Add: %v", err)
	}
	m.emit(startedEvent())
	time.Sleep(50 * time.Millisecond) // nothing to observe; must not panic
}

func TestMCPQueueSinkWebhookStillRejected(t *testing.T) {
	sub := queueSub(false)
	sub.Sink = SubscriptionSink{Type: "webhook", Target: "https://x.example"}
	err := ValidateSubscription(sub, time.Now())
	if err == nil || !strings.Contains(err.Error(), "not available in this phase") {
		t.Errorf("webhook error = %v", err)
	}
}

func TestPendingQueueNilReceiverIsInert(t *testing.T) {
	var q *PendingEventQueue
	if c := q.Append("a", LifecycleEvent{}); c != 0 {
		t.Fatalf("nil Append cursor = %d, want 0", c)
	}
	evs, next, dropped := q.Pending("a", 7, 10)
	if len(evs) != 0 || next != 7 || dropped {
		t.Fatalf("nil Pending = %v,%d,%v", evs, next, dropped)
	}
	q.Release("a") // must not panic
}
