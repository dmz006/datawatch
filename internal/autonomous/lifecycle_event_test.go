package autonomous

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// collector records delivered events for assertions.
type collector struct {
	mu  sync.Mutex
	evs []LifecycleEvent
}

func (c *collector) add(ev LifecycleEvent) {
	c.mu.Lock()
	c.evs = append(c.evs, ev)
	c.mu.Unlock()
}

func (c *collector) snapshot() []LifecycleEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]LifecycleEvent(nil), c.evs...)
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

func taskEvent(i int) LifecycleEvent {
	return LifecycleEvent{
		Kind:      TaskStatusKind(TaskInProgress),
		Level:     LevelTask,
		PRDID:     "prd-1",
		TaskID:    fmt.Sprintf("t%d", i),
		NewStatus: string(TaskInProgress),
	}
}

func TestLifecycle_FanOut(t *testing.T) {
	m := &Manager{}
	defer m.lifecycleBus().Close()

	cs := []*collector{{}, {}, {}}
	for _, c := range cs {
		m.Subscribe(LifecycleFilter{}, c.add)
	}
	const n = 50
	for i := 0; i < n; i++ {
		m.emit(taskEvent(i))
	}
	for ci, c := range cs {
		waitFor(t, fmt.Sprintf("subscriber %d", ci), func() bool { return len(c.snapshot()) == n })
		got := c.snapshot()
		for i, ev := range got {
			if want := fmt.Sprintf("t%d", i); ev.TaskID != want {
				t.Fatalf("subscriber %d event %d: got task %q, want %q (out of order)", ci, i, ev.TaskID, want)
			}
			if ev.ID == "" || ev.At.IsZero() {
				t.Fatalf("subscriber %d event %d not stamped: %+v", ci, i, ev)
			}
			if ev.ID != cs[0].snapshot()[i].ID {
				t.Fatalf("subscriber %d event %d: ID differs across subscribers", ci, i)
			}
		}
	}
}

// A slow subscriber must never hold up publish: the slow subscriber
// blocks on its delivery until we release it, emit of the whole burst
// completes in the meantime (a failure mode that would deadlock the
// test is caught by the generous timeout on the goroutine — emit is
// designed not to block, so timing out is the failure, not the slow
// subscriber), the fast subscribers finish, and the parked subscriber
// gets the events on release (per-subscriber queues give each its own
// delivery pace; the slow one's queue is larger than the burst, so it
// holds back, it never drops).
func TestLifecycle_SlowSubscriberIsolation(t *testing.T) {
	m := &Manager{}
	d := m.lifecycleBus()
	defer d.Close()

	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()

	slowStarted := make(chan struct{})
	var startedOnce sync.Once
	m.Subscribe(LifecycleFilter{}, func(LifecycleEvent) {
		startedOnce.Do(func() { close(slowStarted) })
		<-release // parks here until we release: the "slow subscriber"
	})
	fast1, fast2 := &collector{}, &collector{}
	m.Subscribe(LifecycleFilter{}, fast1.add)
	m.Subscribe(LifecycleFilter{}, fast2.add)

	const n = 200 // < queue size so the fast subscribers never drop

	// Run the whole burst in a goroutine: a hanging emit must fail the
	// test (time out), not stall it. The 5s window is generous and is
	// only a failure guard — emit never blocks, so every emit completes
	// immediately and done closes well within it.
	emitDone := make(chan struct{})
	go func() {
		for i := 0; i < n; i++ {
			m.emit(taskEvent(i))
		}
		close(emitDone)
	}()
	select {
	case <-emitDone:
	case <-time.After(5 * time.Second):
		unblock()
		t.Fatalf("%d emits did not complete while a subscriber was parked — emit blocked on a subscriber", n)
	}

	// Provable state: every fast subscriber already received the whole
	// burst while the slow subscriber was still parked. The slow one's
	// queue holds the events it has not consumed yet, so none are lost.
	for _, c := range []*collector{fast1, fast2} {
		waitFor(t, "fast subscriber", func() bool { return len(c.snapshot()) == n })
	}

	// The slow subscriber is provably parked: it already started on its
	// first event (slowStarted closed) and can only unblock via release,
	// which we still hold.
	waitFor(t, "slow subscriber parked on its first event", func() bool {
		select {
		case <-slowStarted:
			return true
		default:
			return false
		}
	})
	select {
	case <-release:
		t.Fatal("release closed before the test closed it — slow subscriber already ran on")
	default:
	}

	// Release: the subscriber continues from where it parked.
	unblock()
}

func TestLifecycle_QueueOverflowDropsNotBlocks(t *testing.T) {
	m := &Manager{}
	d := m.lifecycleBus()
	defer d.Close()

	release := make(chan struct{})
	started := make(chan struct{})
	// Park one dedicated subscriber inside the first delivery: while its
	// callback holds event 0 the queue is empty and nothing drains it, so
	// a burst of exactly lifecycleQueueSize+extra events leaves exactly
	// extra in THIS subscriber's dropped counter — regardless of what the
	// other live subscribers (including the Manager's built-in lifeHistory
	// recorder) do with their own queues and goroutines.
	s, cancel := d.subscribeCounted(LifecycleFilter{}, func(LifecycleEvent) {
		close(started)
		<-release
	})
	defer func() { // run after d.Close is scheduled below only in the success path
		close(release)
		cancel()
	}()
	m.emit(taskEvent(-1))
	<-started // parked callback holds event 0; queue is empty and undrained

	const extra = 10
	emitDone := make(chan struct{})
	go func() {
		for i := 0; i < lifecycleQueueSize+extra; i++ {
			m.emit(taskEvent(i))
		}
		close(emitDone)
	}()
	// Safety net only, not an assertion: publish never blocks, so
	// reaching this far is a genuine hang, not a scheduling flake.
	select {
	case <-emitDone:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("emit blocked on a full subscriber queue")
	}
	if got := s.dropped.Load(); got != extra {
		t.Fatalf("subscriber dropped %d, want %d", got, extra)
	}
}

func TestLifecycle_Cancel(t *testing.T) {
	m := &Manager{}
	defer m.lifecycleBus().Close()

	c, other := &collector{}, &collector{}
	cancel := m.Subscribe(LifecycleFilter{}, c.add)
	m.Subscribe(LifecycleFilter{}, other.add)

	m.emit(taskEvent(0))
	waitFor(t, "first delivery", func() bool { return len(c.snapshot()) == 1 })

	cancel()
	cancel() // idempotent

	for i := 1; i <= 20; i++ {
		m.emit(taskEvent(i))
	}
	waitFor(t, "other subscriber", func() bool { return len(other.snapshot()) == 21 })
	if got := len(c.snapshot()); got != 1 {
		t.Fatalf("cancelled subscriber received %d events, want 1", got)
	}

	// cancel from inside the callback (one-shot pattern) must not deadlock.
	var selfCancel func()
	oneShot := &collector{}
	selfCancel = m.Subscribe(LifecycleFilter{}, func(ev LifecycleEvent) {
		oneShot.add(ev)
		selfCancel()
	})
	m.emit(taskEvent(100))
	waitFor(t, "one-shot delivery", func() bool { return len(oneShot.snapshot()) == 1 })
	m.emit(taskEvent(101))
	waitFor(t, "other subscriber after one-shot", func() bool { return len(other.snapshot()) == 23 })
	if got := len(oneShot.snapshot()); got != 1 {
		t.Fatalf("one-shot subscriber received %d events, want 1", got)
	}
}

func TestLifecycle_Filter(t *testing.T) {
	prdDone := LifecycleEvent{Kind: PRDStatusKind(PRDCompleted), Level: LevelPRD, PRDID: "p1"}
	prdRun := LifecycleEvent{Kind: PRDStatusKind(PRDRunning), Level: LevelPRD, PRDID: "p1"}
	task := LifecycleEvent{Kind: TaskStatusKind(TaskVerifying), Level: LevelTask, PRDID: "p2", TaskID: "t9"}
	sess := LifecycleEvent{Kind: KindTaskSessionStarted, Level: LevelTask, PRDID: "p2", TaskID: "t9"}

	cases := []struct {
		name string
		f    LifecycleFilter
		ev   LifecycleEvent
		want bool
	}{
		{"empty is wildcard", LifecycleFilter{}, task, true},
		{"level match", LifecycleFilter{Level: LevelPRD}, prdDone, true},
		{"level mismatch", LifecycleFilter{Level: LevelPRD}, task, false},
		{"prd match", LifecycleFilter{PRDID: "p2"}, task, true},
		{"prd mismatch", LifecycleFilter{PRDID: "p1"}, task, false},
		{"task match", LifecycleFilter{TaskID: "t9"}, sess, true},
		{"task mismatch", LifecycleFilter{TaskID: "t1"}, sess, false},
		{"kind match", LifecycleFilter{Kinds: []string{KindTaskSessionStarted}}, sess, true},
		{"kind mismatch", LifecycleFilter{Kinds: []string{KindTaskSessionStarted}}, task, false},
		{"terminal completed", LifecycleFilter{Kinds: TerminalPRDKinds(), Level: LevelPRD}, prdDone, true},
		{"terminal excludes running", LifecycleFilter{Kinds: TerminalPRDKinds(), Level: LevelPRD}, prdRun, false},
		{"all fields", LifecycleFilter{Kinds: []string{"task.verifying"}, Level: LevelTask, PRDID: "p2", TaskID: "t9"}, task, true},
	}
	for _, tc := range cases {
		if got := tc.f.Matches(tc.ev); got != tc.want {
			t.Errorf("%s: Matches = %v, want %v", tc.name, got, tc.want)
		}
	}

	want := map[string]bool{"prd.completed": true, "prd.failed": true, "prd.blocked": true, "prd.cancelled": true, "prd.rejected": true}
	got := TerminalPRDKinds()
	if len(got) != len(want) {
		t.Fatalf("TerminalPRDKinds = %v", got)
	}
	for _, k := range got {
		if !want[k] {
			t.Fatalf("unexpected terminal kind %q", k)
		}
	}

	// Filtering is applied by the dispatcher too.
	m := &Manager{}
	defer m.lifecycleBus().Close()
	term, all := &collector{}, &collector{}
	m.Subscribe(LifecycleFilter{Kinds: TerminalPRDKinds(), Level: LevelPRD}, term.add)
	m.Subscribe(LifecycleFilter{}, all.add)
	for _, ev := range []LifecycleEvent{prdRun, task, prdDone, sess} {
		m.emit(ev)
	}
	waitFor(t, "wildcard subscriber", func() bool { return len(all.snapshot()) == 4 })
	waitFor(t, "terminal subscriber", func() bool { return len(term.snapshot()) >= 1 })
	if g := term.snapshot(); len(g) != 1 || g[0].Kind != "prd.completed" {
		t.Fatalf("terminal subscriber got %+v", g)
	}
}

func TestLifecycle_PanicRecoveredAndNilSafe(t *testing.T) {
	var nilM *Manager
	nilM.emit(taskEvent(0))
	nilM.Subscribe(LifecycleFilter{}, func(LifecycleEvent) {})()

	m := &Manager{}
	defer m.lifecycleBus().Close()
	c := &collector{}
	m.Subscribe(LifecycleFilter{}, func(ev LifecycleEvent) {
		if ev.TaskID == "t0" {
			panic("boom")
		}
		c.add(ev)
	})
	m.emit(taskEvent(0))
	m.emit(taskEvent(1))
	waitFor(t, "delivery after panic", func() bool { return len(c.snapshot()) == 1 })
}

func TestLifecycle_NewManagerWiresDispatcher(t *testing.T) {
	m, err := NewManager(t.TempDir(), Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.lifecycleBus().Close()
	if m.lifecycle == nil {
		t.Fatal("NewManager did not create the lifecycle dispatcher")
	}
	c := &collector{}
	m.Subscribe(LifecycleFilter{Kinds: []string{KindPRDUpdated}}, c.add)
	// emit while holding m.mu must not deadlock.
	m.mu.Lock()
	m.emit(LifecycleEvent{Kind: KindPRDUpdated, Level: LevelPRD, PRDID: "p"})
	m.mu.Unlock()
	waitFor(t, "prd.updated", func() bool { return len(c.snapshot()) == 1 })
	if c.snapshot()[0].Host != m.lifecycleHost {
		t.Fatalf("Host = %q, want %q", c.snapshot()[0].Host, m.lifecycleHost)
	}
}
