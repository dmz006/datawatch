// A delivery notice must reach the operator also when it is raised before the
// daemon has wired its alert sink. The daemon starts the lifecycle engine
// first and sets the sink later, so these tests start things in that order. A
// daemon that stops before the sink exists, or before it took the notice, must
// still raise it after the next start.

package autonomous

import (
	"bytes"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type noticeRec struct {
	mu     sync.Mutex
	alerts []string
	during func() // run while the sink is taking a notice
}

func (r *noticeRec) sink(level, title, body string) string {
	r.mu.Lock()
	during := r.during
	r.alerts = append(r.alerts, level+"|"+title+"|"+body)
	r.mu.Unlock()
	if during != nil {
		during()
	}
	return "a"
}

// with returns the alerts of the level that contain every part.
func (r *noticeRec) with(level string, parts ...string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
next:
	for _, a := range r.alerts {
		if !strings.HasPrefix(a, level+"|") {
			continue
		}
		for _, p := range parts {
			if !strings.Contains(a, p) {
				continue next
			}
		}
		out = append(out, a)
	}
	return out
}

func (r *noticeRec) all() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.alerts...)
}

type logBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *logBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *logBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func noticeLog(t *testing.T) *logBuf {
	t.Helper()
	b := &logBuf{}
	prev := log.Writer()
	log.SetOutput(b)
	t.Cleanup(func() { log.SetOutput(prev) })
	return b
}

// diskCopy is what a daemon killed at this instant leaves behind.
func diskCopy(t *testing.T, dir string) string {
	t.Helper()
	out := t.TempDir()
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		to := filepath.Join(out, rel)
		if d.IsDir() {
			return os.MkdirAll(to, 0o700)
		}
		b, err := os.ReadFile(p)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		return os.WriteFile(to, b, 0o600)
	})
	if err != nil {
		t.Fatalf("copy %s: %v", dir, err)
	}
	return out
}

// daemonRun is one daemon start over a data folder: the manager and the
// lifecycle engine, started with no alert sink yet.
type daemonRun struct {
	dir string
	r   *oneShotRig
}

func startDaemon(t *testing.T, dir string, errs ...error) *daemonRun {
	t.Helper()
	m, err := NewManager(dir, DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(func() { m.lifecycleBus().Close() })
	if _, ok := m.Store().GetPRD("prd-1"); !ok {
		if err := m.Store().SavePRD(&PRD{ID: "prd-1", NotifyEvents: []string{KindTaskSessionStarted}}); err != nil {
			t.Fatalf("SavePRD: %v", err)
		}
	}
	store, err := NewSubscriptionStore(filepath.Join(dir, "subs.json"), nil)
	if err != nil {
		t.Fatalf("NewSubscriptionStore: %v", err)
	}
	r := &oneShotRig{m: m, store: store, errs: errs}
	r.e = r.engine(t)
	return &daemonRun{dir: dir, r: r}
}

// wireSink is the daemon setting the alert sink, after the engine started.
func (d *daemonRun) wireSink() *noticeRec {
	rec := &noticeRec{}
	d.r.m.SetAlertSink(rec.sink)
	return rec
}

func (d *daemonRun) stop() {
	d.r.e.Close()
	d.r.m.lifecycleBus().Close()
}

// killedMidSend runs a daemon until a one-time message is saved as being sent,
// with no key sent yet, and returns what is on disk then.
func killedMidSend(t *testing.T) (disk, subID string) {
	t.Helper()
	d := startDaemon(t, t.TempDir())
	d.wireSink()
	inSend, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	d.r.e.cfg.SendInput = func(string, string, string) error {
		once.Do(func() { close(inSend) })
		<-release
		return nil
	}
	sub := d.r.add(t)
	d.r.fire()
	select {
	case <-inSend:
	case <-time.After(5 * time.Second):
		t.Fatal("the send never started")
	}
	disk = diskCopy(t, d.dir)
	close(release)
	waitFor(t, "the first daemon finishes its send", func() bool { return len(d.r.store.List("")) == 0 })
	return disk, sub.ID
}

func TestNoticeOneTimeInterruptedWithTheEngineStartedBeforeTheSink(t *testing.T) {
	disk, id := killedMidSend(t) // stopped before the first key
	logged := noticeLog(t)
	d := startDaemon(t, disk)
	if state, reason, _, ok := d.r.delivery(t, id); !ok || state != "review_needed" || reason != "interrupted" {
		t.Fatalf("after restart: state=%q reason=%q kept=%v, want review_needed/interrupted", state, reason, ok)
	}
	rec := d.wireSink()
	d.r.retries(3)
	if got := rec.with("error", "Subscription delivery: review needed", "subscription="+id, "reason=interrupted"); len(got) != 1 || len(rec.all()) != 1 {
		t.Fatalf("alerts = %q, want exactly one review alert naming %s", rec.all(), id)
	}
	if v := d.r.view(); len(v.tried) != 0 {
		t.Fatalf("an interrupted message was sent again: %q", v.tried)
	}
	if !strings.Contains(logged.String(), "lifecycle sub "+id) {
		t.Errorf("no daemon log line names %s:\n%s", id, logged.String())
	}
}

func TestNoticeSurvivesAStopBeforeTheSink(t *testing.T) {
	disk, id := killedMidSend(t)
	first := startDaemon(t, disk)
	first.stop() // stopped again before the sink was set

	second := startDaemon(t, disk)
	second.stop()

	third := startDaemon(t, disk)
	rec := third.wireSink()
	if got := rec.with("error", "subscription="+id, "reason=interrupted"); len(got) != 1 || len(rec.all()) != 1 {
		t.Fatalf("alerts = %q, want exactly one review alert", rec.all())
	}
	third.stop()

	// Once the sink took it, it is not raised again.
	fourth := startDaemon(t, disk)
	if rec := fourth.wireSink(); len(rec.all()) != 0 {
		t.Fatalf("raised again after the sink took it: %q", rec.all())
	}
}

// The daemon dies while the sink is taking the notice: it is raised again
// rather than lost.
func TestNoticeKilledWhileTheSinkTakesItIsRaisedAgain(t *testing.T) {
	disk, id := killedMidSend(t)
	d := startDaemon(t, disk)
	rec := &noticeRec{}
	var atSink string
	rec.during = func() {
		if atSink == "" {
			atSink = diskCopy(t, d.dir)
		}
	}
	d.r.m.SetAlertSink(rec.sink)
	// A daemon with the sink wired before the engine reviews inside Start, so
	// either way the sink has been called by now.
	if atSink == "" {
		t.Fatalf("the sink was never called (alerts %q)", rec.all())
	}
	next := startDaemon(t, atSink)
	if got := next.wireSink().with("error", "subscription="+id, "reason=interrupted"); len(got) != 1 {
		t.Fatalf("after dying inside the sink: %d review alerts, want 1", len(got))
	}
}

// Stopped after the message was saved and before it was marked as being sent:
// nothing can have been typed, so it is typed after the restart.
func TestNoticeStoppedBeforeTheSendBeganIsTypedAfterRestart(t *testing.T) {
	d := startDaemon(t, t.TempDir())
	d.stop()
	sub, err := d.r.store.Add(Subscription{
		Filter:  LifecycleFilter{Kinds: []string{KindTaskSessionStarted}},
		Sink:    SubscriptionSink{Type: SinkSession, Target: "ops"},
		OneShot: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.r.store.setDelivery(sub.ID, func(Subscription) (*OneTimeDelivery, bool) {
		return &OneTimeDelivery{State: DecisionWaiting, Target: "ops", Text: oneShotText, FiredAt: time.Now()}, true
	}); err != nil {
		t.Fatal(err)
	}
	next := startDaemon(t, d.dir)
	rec := next.wireSink()
	next.r.retries(3)
	if v := next.r.view(); len(v.typed) != 1 {
		t.Fatalf("typed %q, want the saved message once", v.typed)
	}
	if len(next.r.store.List("")) != 0 || len(rec.all()) != 0 {
		t.Fatalf("subs=%d alerts=%q, want it used up with no alert", len(next.r.store.List("")), rec.all())
	}
}

// An event handled between the engine start and the sink: the waiting warning
// is held, and raised once.
func TestNoticeOneTimeWaitingBeforeTheSink(t *testing.T) {
	for name, stopFirst := range map[string]bool{"same run": false, "after a stop": true} {
		t.Run(name, func(t *testing.T) {
			d := startDaemon(t, t.TempDir(), errRefusedBeforeTyping, errRefusedBeforeTyping, errRefusedBeforeTyping, errRefusedBeforeTyping, errRefusedBeforeTyping, errRefusedBeforeTyping)
			sub := d.r.add(t)
			d.r.fire()
			d.r.waitState(t, sub.ID, "not_typed")
			waitFor(t, "a retry to be armed", func() bool {
				d.r.mu.Lock()
				defer d.r.mu.Unlock()
				return len(d.r.timers) > 0
			})
			d.r.retries(2)
			if stopFirst {
				d.stop()
				d = startDaemon(t, d.dir, errRefusedBeforeTyping, errRefusedBeforeTyping, errRefusedBeforeTyping)
			}
			rec := d.wireSink()
			d.r.retries(2)
			if got := rec.with("warn", "Subscription delivery not typed yet", "subscription="+sub.ID); len(got) != 1 || len(rec.all()) != 1 {
				t.Fatalf("alerts = %q, want one waiting warning", rec.all())
			}
		})
	}
}

// A standing subscription's send that may have reached the session has no
// stored record: only the notice says so.
func TestNoticeStandingSendUnsureBeforeTheSink(t *testing.T) {
	d := startDaemon(t, t.TempDir(), errPromptAfterTyping)
	d.r.e.cfg.PostBridge = func(int, string, string) error { return os.ErrDeadlineExceeded }
	sub, err := d.r.store.Add(Subscription{
		Filter: LifecycleFilter{Kinds: []string{KindTaskSessionStarted}},
		Sink:   SubscriptionSink{Type: SinkSession, Target: "ops"},
	})
	if err != nil {
		t.Fatal(err)
	}
	d.r.fire()
	waitFor(t, "the send", func() bool { return len(d.r.view().tried) == 1 })
	time.Sleep(50 * time.Millisecond) // let the handler finish
	d.stop()
	next := startDaemon(t, d.dir)
	if got := next.wireSink().with("error", "subscription="+sub.ID); len(got) != 1 {
		t.Fatalf("%d review alerts after a stop before the sink, want 1", len(got))
	}
}

// The decision notices go the same way: the coordinator runs before the daemon
// sets the sink.
func TestNoticeDecisionBeforeTheSink(t *testing.T) {
	cases := map[string]struct {
		errs  []error
		run   func(t *testing.T, f *deliveryFixture) string // returns the question the notice names
		level string
		parts []string
	}{
		"send interrupted by a restart": {
			run: func(t *testing.T, f *deliveryFixture) string {
				f.leave(t, f.decide(t), "sending", TaskInProgress)
				f.tick()
				return f.qid
			},
			level: "error", parts: []string{"review needed", "reason=interrupted"},
		},
		"send failed after keys": {
			errs: []error{errPromptAfterTyping},
			run: func(t *testing.T, f *deliveryFixture) string {
				f.decide(t)
				f.tick()
				return f.qid
			},
			level: "error", parts: []string{"review needed", "reason=send_error"},
		},
		"refused and waiting": {
			errs: refusals(8),
			run: func(t *testing.T, f *deliveryFixture) string {
				f.decide(t)
				f.tick()
				f.tick()
				return f.qid
			},
			level: "warn", parts: []string{"reason=prompt_on_screen"},
		},
		"waiting behind another open question": {
			run: func(t *testing.T, f *deliveryFixture) string {
				second := f.ask(t, "A second decision?")
				f.answerID(t, second)
				f.tick()
				f.tick()
				return second
			},
			level: "warn", parts: []string{"reason=other_question_open"},
		},
		"worker cannot get it any more": {
			run: func(t *testing.T, f *deliveryFixture) string {
				f.decide(t)
				f.m.store.MutateTask(f.task(t).ID, func(c *Task) { c.Status = TaskFailed })
				f.tick()
				f.tick()
				return f.qid
			},
			level: "error", parts: []string{"review needed", "reason=task_moved_on"},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newDeliveryFixture(t, c.errs...)
			f.m.SetAlertSink(nil) // not wired yet
			qid := c.run(t, f)
			rec := &noticeRec{}
			f.m.SetAlertSink(rec.sink)
			f.tick()
			f.tick()
			want := append([]string{"question=" + qid}, c.parts...)
			if got := rec.with(c.level, want...); len(got) != 1 {
				t.Fatalf("alerts after the sink was set = %q, want one %s with %q", rec.all(), c.level, want)
			}
			for _, a := range rec.all() {
				if strings.Contains(a, answerQuote) {
					t.Fatalf("a held notice carries the answer text: %q", a)
				}
			}
		})
	}
}

func TestNoticeDecisionSurvivesAStopBeforeTheSink(t *testing.T) {
	f := newDeliveryFixture(t)
	f.m.SetAlertSink(nil)
	f.leave(t, f.decide(t), "sending", TaskInProgress)
	f.tick()
	if got := f.delivery(t); got != "review_needed" {
		t.Fatalf("delivery = %q, want review_needed", got)
	}
	next, err := NewManager(f.m.dataDir, DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { next.lifecycleBus().Close() })
	rec := &noticeRec{}
	next.SetAlertSink(rec.sink)
	if got := rec.with("error", "question="+f.qid, "reason=interrupted"); len(got) != 1 {
		t.Fatalf("alerts after the restart = %q, want the review alert once", rec.all())
	}
}

// The supervisor wake's own warning, with the daemon's subscriber (the
// Manager) and no sink yet.
func TestNoticeWakeBeforeTheSink(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.lifecycleBus().Close() })
	cfg := SupervisorWakeConfig{Subscriber: m}
	cfg.alert("warn", "Supervisor wake not typed yet", "session=sup reason=prompt_on_screen.")
	rec := &noticeRec{}
	m.SetAlertSink(rec.sink)
	if got := rec.with("warn", "Supervisor wake not typed yet"); len(got) != 1 {
		t.Fatalf("alerts = %q, want the held wake warning", rec.all())
	}
}

// When the parked question expires while another is open, the worker is told
// it expired before the task is parked on the other one.
func TestNoticeExpiredParkedQuestionWithAnotherOpen(t *testing.T) {
	for name, errs := range map[string][]error{"typed at once": nil, "refused first": refusals(2)} {
		t.Run(name, func(t *testing.T) {
			f := newDeliveryFixture(t, errs...)
			first := f.qid
			other := f.ask(t, "Other open question?")
			if _, err := f.store.Expire(first, "too old"); err != nil {
				t.Fatal(err)
			}
			// The wait loop polls every few seconds, the tick once a minute: the
			// loop gets there first.
			prd, task := f.live(t)
			if f.m.ParkForDecision(prd, task, other) {
				t.Fatalf("the loop parked on the other question before the expiry was told")
			}
			for i := 0; i < 8; i++ {
				f.tick()
				prd, task = f.live(t)
				f.m.ParkForDecision(prd, task, other)
			}
			var said []string
			for _, l := range f.lines() {
				if strings.Contains(l, "question="+first+" ") && strings.Contains(l, "expired") {
					said = append(said, l)
				}
			}
			if len(said) != len(errs)+1 {
				t.Fatalf("expired line sent %d times, want %d (lines %q, alerts %q)", len(said), len(errs)+1, f.lines(), f.raised())
			}
			if got := f.task(t); got.WaitReason != AwaitDecisionWaitReasonFor(other) {
				t.Fatalf("task=%s %q, want it then parked on the open question", got.Status, got.WaitReason)
			}
			// And that one is still answered normally.
			f.answerID(t, other)
			for i := 0; i < 4; i++ {
				f.tick()
			}
			if got := f.typedFor(other); !twoQOrder(got, []string{other}) {
				t.Fatalf("typed %v for the other question, want it once", got)
			}
		})
	}
}

// The notice is on disk before the state it reports is: a daemon killed
// between the two still has it.
func TestNoticeIsOnDiskBeforeTheStateItReports(t *testing.T) {
	onDisk := func(dir string) string {
		b, _ := os.ReadFile(filepath.Join(dir, "autonomous", "delivery_notices.json"))
		return string(b)
	}
	t.Run("one-time message", func(t *testing.T) {
		disk, id := killedMidSend(t)
		d := startDaemon(t, disk)
		d.wireSink() // the first review's notice is taken, so none is on disk
		d.r.m.SetAlertSink(nil)
		d.stop()
		if left := onDisk(disk); left != "" {
			t.Fatalf("still on disk before the check: %s", left)
		}
		var seen []string
		d.r.store.deliverySaveHook = func(next *OneTimeDelivery) {
			if next != nil && next.State == DecisionReviewNeeded {
				seen = append(seen, onDisk(disk))
			}
		}
		// Put it back to "sending" and review it again.
		if _, err := d.r.store.setDelivery(id, func(c Subscription) (*OneTimeDelivery, bool) {
			next := *c.Delivery
			next.State, next.Reason = DecisionSending, ""
			return &next, true
		}); err != nil {
			t.Fatal(err)
		}
		d.r.e.reviewOneShot(id, "ops", deliveryInterrupted)
		if len(seen) != 1 || !strings.Contains(seen[0], id) {
			t.Fatalf("on disk when the review state was saved: %q, want the notice for %s", seen, id)
		}
	})
	t.Run("decision", func(t *testing.T) {
		f := newDeliveryFixture(t)
		f.m.SetAlertSink(nil)
		f.leave(t, f.decide(t), "sending", TaskInProgress)
		var seen []string
		f.m.store.taskSaveHook = func(c *Task) {
			if c.DecisionResume != nil && c.DecisionResume.Delivery == DecisionReviewNeeded {
				seen = append(seen, onDisk(f.m.dataDir))
			}
		}
		f.tick()
		if len(seen) != 1 || !strings.Contains(seen[0], f.qid) {
			t.Fatalf("on disk when the review state was saved: %q, want the notice for %s", seen, f.qid)
		}
	})
}

// A state that could not be saved raises nothing; the notice comes once the
// state is saved.
func TestNoticeForAStateThatWasNotSavedIsDropped(t *testing.T) {
	f := newDeliveryFixture(t)
	f.leave(t, f.decide(t), "sending", TaskInProgress)
	dir := f.m.store.recordsDir()
	if err := os.Chmod(dir, 0o500); err != nil { // #nosec G302 -- the test makes saving fail
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) }) // #nosec G302 -- restore the test dir
	if probe := os.WriteFile(filepath.Join(dir, "probe"), []byte("x"), 0o600); probe == nil {
		t.Skip("this user can write a read-only folder")
	}
	f.tick()
	if al := f.raised(); len(al) != 0 {
		t.Fatalf("alerts = %q for a state that was not saved", al)
	}
	if err := os.Chmod(dir, 0o755); err != nil { // #nosec G302 -- restore the test dir
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		f.tick()
	}
	if al := f.reviewAlerts(f.qid); len(al) != 1 {
		t.Fatalf("review alerts = %q, want one", f.raised())
	}
}
