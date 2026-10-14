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

func TestLifecycle_SlowSubscriberIsolation(t *testing.T) {
	m := &Manager{}
	d := m.lifecycleBus()
	defer d.Close()

	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()

	m.Subscribe(LifecycleFilter{}, func(LifecycleEvent) { <-release })
	fast1, fast2 := &collector{}, &collector{}
	m.Subscribe(LifecycleFilter{}, fast1.add)
	m.Subscribe(LifecycleFilter{}, fast2.add)

	const n = 200 // < queue size so the fast subscribers never drop
	start := time.Now()
	for i := 0; i < n; i++ {
		m.emit(taskEvent(i))
	}
	if el := time.Since(start); el > 50*time.Millisecond {
		t.Fatalf("%d emits took %v with a blocked subscriber; emit must not block", n, el)
	}
	for _, c := range []*collector{fast1, fast2} {
		waitFor(t, "fast subscriber", func() bool { return len(c.snapshot()) == n })
	}
	unblock()
}

func TestLifecycle_QueueOverflowDropsNotBlocks(t *testing.T) {
	m := &Manager{}
	d := m.lifecycleBus()
	defer d.Close()

	release := make(chan struct{})
	defer close(release)
	started := make(chan struct{}, 1)
	m.Subscribe(LifecycleFilter{}, func(LifecycleEvent) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-release
	})
	m.emit(taskEvent(-1))
	<-started // goroutine is now stuck inside fn; queue is empty

	const extra = 10
	done := make(chan struct{})
	go func() {
		for i := 0; i < lifecycleQueueSize+extra; i++ {
			m.emit(taskEvent(i))
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("emit blocked on a full subscriber queue")
	}
	if got := d.droppedTotal(); got != extra {
		t.Fatalf("dropped = %d, want %d", got, extra)
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
