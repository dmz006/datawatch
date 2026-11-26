// A wake is not done until its line was typed into the designated supervisor
// session: a send that typed nothing is kept and tried again by itself, and one
// that cannot be told apart from a delivered line is shown, never retyped.

package autonomous

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/session"
	"github.com/dmz006/datawatch/internal/supervisorloop"
)

type wakeTimerClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*wakeClockTimer
}

type wakeClockTimer struct {
	at time.Time
	f  func()
}

func (c *wakeTimerClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *wakeTimerClock) AfterFunc(d time.Duration, f func()) func() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &wakeClockTimer{at: c.now.Add(d), f: f}
	c.timers = append(c.timers, t)
	return func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		stopped := t.f != nil
		t.f = nil
		return stopped
	}
}

// pass moves the clock forward one minute at a time, running due timers.
func (c *wakeTimerClock) pass(d time.Duration) {
	for ; d > 0; d -= time.Minute {
		c.mu.Lock()
		c.now = c.now.Add(time.Minute)
		var due []func()
		for _, t := range c.timers {
			if t.f != nil && !t.at.After(c.now) {
				due = append(due, t.f)
				t.f = nil
			}
		}
		c.mu.Unlock()
		for _, f := range due {
			f()
		}
	}
}

type wakeDeliveryRig struct {
	*wakeTestSubscriber
	clock *wakeTimerClock

	mu     sync.Mutex
	target string
	errs   []error // returned in order; after the last, nil
	tried  []string
	typed  []string
	alerts []string
	runs   int
	record *supervisorloop.WakeRecord
}

// AlertSink lets the set raise operator alerts, as the Manager does.
func (r *wakeDeliveryRig) AlertSink() func(level, title, body string) string {
	return func(level, title, body string) string {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.alerts = append(r.alerts, level+"|"+title+"|"+body)
		return "a"
	}
}

func newWakeDeliveryRig(target string, errs ...error) *wakeDeliveryRig {
	return &wakeDeliveryRig{wakeTestSubscriber: &wakeTestSubscriber{}, clock: &wakeTimerClock{now: time.Unix(50000, 0)}, target: target, errs: errs}
}

func (r *wakeDeliveryRig) register(t *testing.T, memory *supervisorloop.WakeSendMemory) *SupervisorWakeSet {
	t.Helper()
	st := supervisorloop.DefaultWakeSettings()
	st.Enabled, st.Kinds, st.CoalesceSeconds = true, []string{"task.failed"}, 0
	set := RegisterSupervisorWake(SupervisorWakeConfig{
		Subscriber: r, Settings: st, Clock: r.clock, Memory: memory, Record: r.record,
		Run: func(supervisorloop.WakeBatch) { r.mu.Lock(); r.runs++; r.mu.Unlock() },
		Session: func() string {
			r.mu.Lock()
			defer r.mu.Unlock()
			return r.target
		},
		SendInput: func(id, text string) error {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.tried = append(r.tried, id+":"+text)
			if len(r.errs) > 0 {
				err := r.errs[0]
				r.errs = r.errs[1:]
				if err != nil {
					return err
				}
			}
			r.typed = append(r.typed, id+":"+text)
			return nil
		},
	})
	if set == nil {
		t.Fatal("set not registered")
	}
	t.Cleanup(set.Close)
	return set
}

func (r *wakeDeliveryRig) failed(set *SupervisorWakeSet, task string) {
	r.emit(LifecycleEvent{Kind: "task.failed", PRDID: "a1", TaskID: task})
	set.Wait()
}

func (r *wakeDeliveryRig) state() (tried, typed, alerts []string, runs int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.tried...), append([]string(nil), r.typed...), append([]string(nil), r.alerts...), r.runs
}

func (r *wakeDeliveryRig) retarget(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.target = id
}

func TestWakeRefusedBeforeTypingIsRetriedByItselfAndTypedOnce(t *testing.T) {
	r := newWakeDeliveryRig("sup", errRefusedBeforeTyping, errRefusedBeforeTyping, errRefusedBeforeTyping)
	set := r.register(t, nil)
	r.failed(set, "t1")
	r.clock.pass(time.Hour)
	tried, typed, alerts, runs := r.state()
	if len(typed) != 1 || !strings.HasPrefix(typed[0], "sup:") || !strings.Contains(typed[0], "task=t1") {
		t.Fatalf("typed = %q after an hour, want the wake once into sup (tried %d)", typed, len(tried))
	}
	if len(tried) != 4 {
		t.Fatalf("%d attempts, want 3 refused and 1 that worked, then none", len(tried))
	}
	if runs != 1 {
		t.Fatalf("the pass ran %d times: a retry only types", runs)
	}
	if len(alerts) != 1 || !strings.HasPrefix(alerts[0], "warn|") {
		t.Fatalf("alerts = %q, want one warning for the whole wait", alerts)
	}
}

func TestWakeWithNoTargetWaitsAndGoesOnlyToTheDesignatedSession(t *testing.T) {
	r := newWakeDeliveryRig("")
	set := r.register(t, nil)
	r.failed(set, "t1")
	r.clock.pass(30 * time.Minute)
	if tried, _, alerts, _ := r.state(); len(tried) != 0 || len(alerts) != 1 {
		t.Fatalf("with no target: tried %q, alerts %q; want nothing typed and one alert", tried, alerts)
	}
	r.retarget("designated")
	r.clock.pass(30 * time.Minute)
	_, typed, _, _ := r.state()
	if len(typed) != 1 || !strings.HasPrefix(typed[0], "designated:") || !strings.Contains(typed[0], "task=t1") {
		t.Fatalf("typed = %q, want the waiting wake once into the designated session", typed)
	}
}

func TestWakeUnknownOutcomeIsNeverRetypedAndIsShown(t *testing.T) {
	for name, sendErr := range map[string]error{
		"prompt appeared after the text was typed": errPromptAfterTyping,
		"send failed": errors.New("send input: exit status 1"),
	} {
		t.Run(name, func(t *testing.T) {
			r := newWakeDeliveryRig("sup", sendErr)
			set := r.register(t, nil)
			r.failed(set, "t1")
			r.clock.pass(time.Hour)
			r.failed(set, "t2")
			r.clock.pass(time.Hour)
			tried, typed, alerts, _ := r.state()
			if len(tried) != 2 {
				t.Fatalf("attempts = %q, want the unsure one and the next event", tried)
			}
			if len(typed) != 1 || strings.Contains(typed[0], "task=t1") {
				t.Fatalf("typed = %q: the unsure line must not be typed again", typed)
			}
			if len(alerts) != 1 || !strings.HasPrefix(alerts[0], "error|") || !strings.Contains(alerts[0], "review needed") || !strings.Contains(alerts[0], "task=t1") {
				t.Fatalf("alerts = %q, want one 'review needed' naming the wake", alerts)
			}
		})
	}
}

func TestWakeWaitingIsCarriedByTheNextEventOnce(t *testing.T) {
	r := newWakeDeliveryRig("sup", errRefusedBeforeTyping)
	set := r.register(t, nil)
	r.failed(set, "t1")
	r.failed(set, "t2")
	r.clock.pass(time.Hour)
	_, typed, _, _ := r.state()
	if len(typed) != 1 || !strings.Contains(typed[0], "task=t1") || !strings.Contains(typed[0], "task=t2") {
		t.Fatalf("typed = %q, want one line with both wakes", typed)
	}
}

// A settings change replaces the set; the waiting wake is held by the memory
// the owner keeps, and a closed set types nothing.
func TestWakeWaitingSurvivesAReplacedSet(t *testing.T) {
	r := newWakeDeliveryRig("sup", errRefusedBeforeTyping)
	memory := supervisorloop.NewWakeSendMemory()
	old := r.register(t, memory)
	r.failed(old, "t1")
	old.Close()
	r.clock.pass(time.Hour)
	if tried, _, _, _ := r.state(); len(tried) != 1 {
		t.Fatalf("a closed set kept typing: %q", tried)
	}
	r.wakeTestSubscriber = &wakeTestSubscriber{}
	r.register(t, memory)
	r.clock.pass(time.Hour)
	_, typed, _, _ := r.state()
	if len(typed) != 1 || !strings.Contains(typed[0], "task=t1") {
		t.Fatalf("typed = %q, want the waiting wake from the new set", typed)
	}
}

func TestWakeRecordCountsAWakeWithNoPassOnlyOnceTyped(t *testing.T) {
	clock := &wakeTimerClock{now: time.Unix(50000, 0)}
	sub := &wakeTestSubscriber{}
	rec := supervisorloop.NewWakeRecord(clock, 0)
	var mu sync.Mutex
	fail := true
	st := supervisorloop.DefaultWakeSettings()
	st.Enabled, st.Kinds, st.CoalesceSeconds = true, []string{"task.failed"}, 0
	set := RegisterSupervisorWake(SupervisorWakeConfig{
		Subscriber: sub, Settings: st, Clock: clock, Record: rec,
		Session: func() string { return "sup" },
		SendInput: func(string, string) error {
			mu.Lock()
			defer mu.Unlock()
			if fail {
				return fmt.Errorf("%w (the pane could not be read: x)", session.ErrPromptOnScreen)
			}
			return nil
		},
	})
	defer set.Close()
	sub.emit(LifecycleEvent{Kind: "task.failed", PRDID: "a1", TaskID: "t1"})
	set.Wait()
	if n := rec.Status().EventCount; n != 0 {
		t.Fatalf("an untyped wake counted as delivered (%d)", n)
	}
	mu.Lock()
	fail = false
	mu.Unlock()
	clock.pass(time.Hour)
	if n := rec.Status().EventCount; n != 1 {
		t.Fatalf("event count = %d after the retry typed it, want 1", n)
	}
}

// The wake status counts a wake only once its line was typed, also when a
// supervisor pass ran for it.
func TestWakeStatusDoesNotCountAnUntypedLineWhenThePassRan(t *testing.T) {
	for name, tc := range map[string]struct {
		err      error
		afterAll int // the count once an hour of retries is over
	}{
		"refused, typed by a retry": {errRefusedBeforeTyping, 1},
		"unsure, never typed again": {errors.New("send input: exit status 1"), 0},
	} {
		t.Run(name, func(t *testing.T) {
			r := newWakeDeliveryRig("sup", tc.err)
			rec := supervisorloop.NewWakeRecord(r.clock, 0)
			r.record = rec
			set := r.register(t, nil)
			r.failed(set, "t1")
			if _, _, _, runs := r.state(); runs != 1 {
				t.Fatalf("the pass ran %d times, want 1", runs)
			}
			if n := rec.Status().EventCount; n != 0 {
				t.Fatalf("event count = %d with no line typed, want 0", n)
			}
			r.clock.pass(time.Hour)
			if n := rec.Status().EventCount; n != tc.afterAll {
				t.Fatalf("event count = %d after the retries, want %d", n, tc.afterAll)
			}
		})
	}
}

// With no supervisor session designated, a wake that ran the pass stays
// counted, as the controller's existing expectation requires; typing it later
// does not count it again.
func TestWakeStatusWithNoDesignatedSessionCountsThePassOnce(t *testing.T) {
	r := newWakeDeliveryRig("")
	rec := supervisorloop.NewWakeRecord(r.clock, 0)
	r.record = rec
	set := r.register(t, nil)
	r.failed(set, "t1")
	r.retarget("designated")
	r.clock.pass(time.Hour)
	if _, typed, _, _ := r.state(); len(typed) != 1 {
		t.Fatalf("typed = %q, want the waiting wake once", typed)
	}
	if n := rec.Status().EventCount; n != 1 {
		t.Fatalf("event count = %d, want 1 for the one wake", n)
	}
}

// A wake that adds nothing the session has not been told types no line.
func TestWakeStatusDoesNotCountARepeatThatTypesNothing(t *testing.T) {
	r := newWakeDeliveryRig("sup")
	rec := supervisorloop.NewWakeRecord(r.clock, 0)
	r.record = rec
	set := r.register(t, nil)
	r.failed(set, "t1")
	r.emit(LifecycleEvent{Kind: "task.failed", PRDID: "a1", TaskID: "t1", Details: "again"})
	set.Wait()
	if _, typed, _, runs := r.state(); len(typed) != 1 || runs != 2 {
		t.Fatalf("typed %q in %d runs, want one line in two runs", typed, runs)
	}
	if n := rec.Status().EventCount; n != 1 {
		t.Fatalf("event count = %d, want only the wake whose line was typed", n)
	}
}

// A batch that reaches a set just closed by a settings change is typed by the
// next set, and counted then.
func TestWakeHandedToAClosedSetIsCountedWhenTheNextSetTypesIt(t *testing.T) {
	r := newWakeDeliveryRig("sup")
	rec := supervisorloop.NewWakeRecord(r.clock, 0)
	r.record = rec
	memory := supervisorloop.NewWakeSendMemory()
	old := r.register(t, memory)
	old.Close()
	old.send([]supervisorloop.WakeEvent{{Kind: "task.failed", AutomatonID: "a1", TaskID: "t1"}}, true)
	r.wakeTestSubscriber = &wakeTestSubscriber{}
	r.register(t, memory)
	r.clock.pass(time.Hour)
	if _, typed, _, _ := r.state(); len(typed) != 1 {
		t.Fatalf("typed = %q, want the wake once from the new set", typed)
	}
	if n := rec.Status().EventCount; n != 1 {
		t.Fatalf("event count = %d, want 1", n)
	}
}
