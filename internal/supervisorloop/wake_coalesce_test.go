package supervisorloop

import (
	"testing"
	"time"
)

type wakeTimer struct {
	at time.Time
	f  func()
}

type fakeWakeClock struct {
	now    time.Time
	timers []*wakeTimer
}

func (c *fakeWakeClock) Now() time.Time { return c.now }

func (c *fakeWakeClock) AfterFunc(d time.Duration, f func()) func() bool {
	t := &wakeTimer{at: c.now.Add(d), f: f}
	c.timers = append(c.timers, t)
	return func() bool { t.f = nil; return true }
}

func (c *fakeWakeClock) Advance(d time.Duration) {
	c.now = c.now.Add(d)
	for _, t := range c.timers {
		if t.f != nil && !t.at.After(c.now) {
			f := t.f
			t.f = nil
			f()
		}
	}
}

func newWakeFixture(window time.Duration) (*Coalescer, *fakeWakeClock, *[]WakeBatch) {
	clk := &fakeWakeClock{now: time.Unix(1000, 0)}
	var got []WakeBatch
	return NewCoalescer(clk, window, func(b WakeBatch) { got = append(got, b) }), clk, &got
}

func TestWakeCoalesceBurst(t *testing.T) {
	c, clk, got := newWakeFixture(30 * time.Second)
	c.Offer(WakeEvent{Kind: "task.failed", AutomatonID: "a1", TaskID: "t1", Fingerprint: "x"})
	clk.Advance(10 * time.Second)
	c.Offer(WakeEvent{Kind: "task.failed", AutomatonID: "a1", TaskID: "t2", Fingerprint: "x"})
	c.Offer(WakeEvent{Kind: "prd.blocked", AutomatonID: "a2", Fingerprint: "y"})
	if len(*got) != 0 {
		t.Fatalf("delivered before window closed: %d", len(*got))
	}
	clk.Advance(20 * time.Second)
	if len(*got) != 1 || len((*got)[0].Events) != 3 {
		t.Fatalf("want one batch of 3, got %+v", *got)
	}
}

func TestWakeDedupUnchangedCondition(t *testing.T) {
	c, clk, got := newWakeFixture(5 * time.Second)
	ev := WakeEvent{Kind: "task.failed", AutomatonID: "a1", TaskID: "t1", Fingerprint: "x"}
	c.Offer(ev)
	if c.Offer(ev) {
		t.Fatal("duplicate inside the window accepted")
	}
	clk.Advance(5 * time.Second)
	if c.Offer(ev) {
		t.Fatal("unchanged condition redelivered")
	}
	clk.Advance(time.Minute)
	if len(*got) != 1 || len((*got)[0].Events) != 1 {
		t.Fatalf("want one wake for one condition, got %+v", *got)
	}
}

func TestWakeDedupAfterClear(t *testing.T) {
	c, clk, got := newWakeFixture(5 * time.Second)
	ev := WakeEvent{Kind: "task.failed", AutomatonID: "a1", TaskID: "t1", Fingerprint: "x"}
	c.Offer(ev)
	clk.Advance(5 * time.Second)
	c.Clear(ev.Key())
	if !c.Offer(ev) {
		t.Fatal("condition not accepted after Clear")
	}
	clk.Advance(5 * time.Second)
	if len(*got) != 2 {
		t.Fatalf("want two wakes, got %d", len(*got))
	}
}
