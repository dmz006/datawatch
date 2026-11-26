// A one-time message to a session is typed, never sent over the channel
// bridge, and is used up only when typing worked. While the session cannot
// take it the operator is told once and it is tried again, also after a
// restart. A send that may have reached the session is never repeated.
//
// The stored delivery state is read as JSON, so these tests describe
// behaviour, not types.

package autonomous

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

const oneShotText = "Start with the setup section of the task file"

type oneShotRig struct {
	m     *Manager
	store *SubscriptionStore
	e     *LifecycleSubscriptionEngine

	mu      sync.Mutex
	errs    []error // returned in order; after the last, nil
	tried   []string
	typed   []string
	bridged []string
	alerts  []string
	missing bool // the session is not found
	timers  []*oneShotTimer
	delays  []time.Duration
}

type oneShotTimer struct{ f func() }

// engine builds and starts an engine over store, as a daemon start does.
func (r *oneShotRig) engine(t *testing.T) *LifecycleSubscriptionEngine {
	t.Helper()
	e := NewLifecycleSubscriptionEngine(r.m, r.store, SessionPushConfig{
		FindSession: func(nameOrID string) (string, int, bool) {
			r.mu.Lock()
			defer r.mu.Unlock()
			if r.missing {
				return "", 0, false
			}
			return nameOrID, 9013, true // the session has a channel bridge
		},
		PostBridge: func(_ int, id, text string) error {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.bridged = append(r.bridged, id+"|"+text)
			return nil
		},
		SendInput: func(id, text, source string) error {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.tried = append(r.tried, id+"|"+text)
			if len(r.errs) > 0 {
				err := r.errs[0]
				r.errs = r.errs[1:]
				if err != nil {
					return err
				}
			}
			r.typed = append(r.typed, id+"|"+text)
			return nil
		},
	})
	e.afterFn = func(d time.Duration, f func()) func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		tm := &oneShotTimer{f: f}
		r.timers = append(r.timers, tm)
		r.delays = append(r.delays, d)
		return func() bool {
			r.mu.Lock()
			defer r.mu.Unlock()
			stopped := tm.f != nil
			tm.f = nil
			return stopped
		}
	}
	if err := e.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(e.Close)
	return e
}

func newOneShotRig(t *testing.T, errs ...error) *oneShotRig {
	t.Helper()
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(func() { m.lifecycleBus().Close() })
	if err := m.Store().SavePRD(&PRD{ID: "prd-1", NotifyEvents: []string{KindTaskSessionStarted}}); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	store, err := NewSubscriptionStore(t.TempDir()+"/subs.json", nil)
	if err != nil {
		t.Fatalf("NewSubscriptionStore: %v", err)
	}
	r := &oneShotRig{m: m, store: store, errs: errs}
	m.SetAlertSink(func(level, title, body string) string {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.alerts = append(r.alerts, level+"|"+title+"|"+body)
		return "a"
	})
	r.e = r.engine(t)
	return r
}

func (r *oneShotRig) add(t *testing.T) Subscription {
	t.Helper()
	s, err := r.store.Add(Subscription{
		Filter:  LifecycleFilter{Kinds: []string{KindTaskSessionStarted}},
		Sink:    SubscriptionSink{Type: SinkSession, Target: "ops"},
		Action:  &SubscriptionAction{Type: SinkActionSendInput, Text: oneShotText},
		OneShot: true,
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	return s
}

type oneShotView struct {
	tried, typed, bridged, alerts []string
}

func (r *oneShotRig) view() oneShotView {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := func(s []string) []string { return append([]string(nil), s...) }
	return oneShotView{cp(r.tried), cp(r.typed), cp(r.bridged), cp(r.alerts)}
}

// runTimers runs every armed retry timer once, and reports how many ran.
func (r *oneShotRig) runTimers() int {
	r.mu.Lock()
	var due []func()
	for _, tm := range r.timers {
		if tm.f != nil {
			due = append(due, tm.f)
			tm.f = nil
		}
	}
	r.mu.Unlock()
	for _, f := range due {
		f()
	}
	return len(due)
}

func (r *oneShotRig) retries(n int) {
	for i := 0; i < n; i++ {
		r.runTimers()
	}
}

// delivery is the subscription's delivery state as the API serves it; ok is
// false once the subscription is gone.
func (r *oneShotRig) delivery(t *testing.T, id string) (state, reason, text string, ok bool) {
	t.Helper()
	sub, found := r.store.Get(id)
	if !found {
		return "", "", "", false
	}
	b, err := json.Marshal(sub)
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		D struct {
			State  string `json:"state"`
			Reason string `json:"reason"`
			Text   string `json:"text"`
		} `json:"delivery"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out.D.State, out.D.Reason, out.D.Text, true
}

func (r *oneShotRig) fire() {
	ev := startedEvent()
	ev.SessionID = "worker-77aa"
	r.m.emit(ev)
}

func (r *oneShotRig) waitState(t *testing.T, id, want string) {
	t.Helper()
	waitFor(t, "delivery state "+want, func() bool {
		s, _, _, ok := r.delivery(t, id)
		return ok && s == want
	})
}

func (r *oneShotRig) waitGone(t *testing.T, id string) {
	t.Helper()
	waitFor(t, "one-time subscription used up", func() bool {
		_, ok := r.store.Get(id)
		return !ok
	})
}

func settle() { time.Sleep(100 * time.Millisecond) } // bounded assert-not-happens window

func TestOneShotIsTypedAndNeverSentOverTheBridge(t *testing.T) {
	r := newOneShotRig(t)
	sub := r.add(t)
	r.fire()
	waitFor(t, "a delivery", func() bool { v := r.view(); return len(v.typed)+len(v.bridged) > 0 })
	v := r.view()
	if len(v.bridged) != 0 || len(v.typed) != 1 || v.typed[0] != "worker-77aa|"+oneShotText {
		t.Fatalf("typed=%q bridged=%q, want it typed once into the new worker", v.typed, v.bridged)
	}
	r.waitGone(t, sub.ID)
}

func TestOneShotTheSessionCannotTakeIsKeptAnnouncedAndTypedOnce(t *testing.T) {
	r := newOneShotRig(t, errRefusedBeforeTyping, errRefusedBeforeTyping, errRefusedBeforeTyping)
	sub := r.add(t)
	r.fire()
	r.waitState(t, sub.ID, "not_typed")
	settle()
	_, reason, text, _ := r.delivery(t, sub.ID)
	if reason != "prompt_on_screen" || text != oneShotText {
		t.Fatalf("kept reason=%q text=%q, want the message kept with its reason", reason, text)
	}
	if v := r.view(); len(v.alerts) != 1 || !strings.HasPrefix(v.alerts[0], "warn|") ||
		!strings.Contains(v.alerts[0], sub.ID) || strings.Contains(v.alerts[0], oneShotText) {
		t.Fatalf("alerts = %q, want one warning naming the subscription, without the text", v.alerts)
	}
	r.fire() // a second matching event: the one-time message already fired
	settle()
	before := len(r.view().tried)
	r.retries(6)
	r.waitGone(t, sub.ID)
	r.retries(3)
	v := r.view()
	if len(v.typed) != 1 || len(v.bridged) != 0 {
		t.Fatalf("typed=%q bridged=%q, want it typed exactly once", v.typed, v.bridged)
	}
	if len(v.tried) != 4 || before != 2 {
		t.Fatalf("attempts: %d before the retries and %d in all, want 2 and 4", before, len(v.tried))
	}
	if len(v.alerts) != 1 {
		t.Fatalf("alerts = %q, want only the first warning", v.alerts)
	}
}

func TestOneShotRetryBacksOffAndNeverGivesUp(t *testing.T) {
	errs := make([]error, 20)
	for i := range errs {
		errs[i] = errRefusedBeforeTyping
	}
	r := newOneShotRig(t, errs...)
	sub := r.add(t)
	r.fire()
	r.waitState(t, sub.ID, "not_typed")
	r.retries(25)
	r.waitGone(t, sub.ID)
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.typed) != 1 || r.delays[0] != 30*time.Second || r.delays[1] != time.Minute || r.delays[len(r.delays)-1] != 5*time.Minute {
		t.Fatalf("typed=%d delays=%v, want 30s doubling to 5m until it is typed", len(r.typed), r.delays)
	}
}

func TestOneShotUnknownOutcomeIsListedAndNeverRetyped(t *testing.T) {
	for name, sendErr := range map[string]error{
		"prompt appeared after the text was typed": errPromptAfterTyping,
		"keys sent in part":                        errors.New("send input: exit status 1"),
		"typed but the session was not saved":      errors.New("save session: disk full"),
	} {
		t.Run(name, func(t *testing.T) {
			r := newOneShotRig(t, sendErr)
			sub := r.add(t)
			r.fire()
			r.waitState(t, sub.ID, "review_needed")
			r.fire()
			r.retries(4)
			settle()
			v := r.view()
			if len(v.tried) != 1 || len(v.bridged) != 0 {
				t.Fatalf("attempts=%q bridged=%q, want the one send and nothing more", v.tried, v.bridged)
			}
			if len(v.alerts) != 1 || !strings.HasPrefix(v.alerts[0], "error|") || !strings.Contains(v.alerts[0], "review needed") {
				t.Fatalf("alerts = %q, want one 'review needed'", v.alerts)
			}
		})
	}
}

func TestOneShotWaitingForAMissingSessionIsDeliveredWhenItAppears(t *testing.T) {
	r := newOneShotRig(t)
	r.missing = true
	sub := r.add(t)
	r.fire()
	r.waitState(t, sub.ID, "not_typed")
	settle() // past the second immediate attempt
	if _, reason, _, _ := r.delivery(t, sub.ID); reason != "session_unavailable" {
		t.Fatalf("reason = %q", reason)
	}
	r.retries(2)
	r.mu.Lock()
	r.missing = false
	r.mu.Unlock()
	r.retries(2)
	r.waitGone(t, sub.ID)
	if v := r.view(); len(v.typed) != 1 || len(v.alerts) != 1 {
		t.Fatalf("typed=%q alerts=%q, want one line and one warning", v.typed, v.alerts)
	}
}

func TestOneShotWaitingMessageSurvivesARestart(t *testing.T) {
	r := newOneShotRig(t, errRefusedBeforeTyping, errRefusedBeforeTyping)
	sub := r.add(t)
	r.fire()
	r.waitState(t, sub.ID, "not_typed")
	settle()
	r.e.Close()
	if n := r.runTimers(); n != 0 {
		t.Fatalf("%d retry timers still armed after Close", n)
	}
	reloaded, err := NewSubscriptionStore(r.store.path, nil)
	if err != nil {
		t.Fatal(err)
	}
	r.store = reloaded
	r.e = r.engine(t)
	r.retries(3)
	r.waitGone(t, sub.ID)
	if v := r.view(); len(v.typed) != 1 || len(v.alerts) != 1 {
		t.Fatalf("typed=%q alerts=%q, want it typed once after the restart with no second warning", v.typed, v.alerts)
	}
}

// putDelivery writes the stored state a daemon that stopped part-way leaves.
func putDelivery(t *testing.T, r *oneShotRig, sub Subscription, state string) {
	t.Helper()
	r.e.Close()
	b, err := json.Marshal(sub)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	raw["delivery"] = map[string]any{"state": state, "target": "worker-77aa", "text": oneShotText, "fired_at": "2026-11-26T00:00:00Z"}
	out, err := json.Marshal([]any{raw})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(r.store.path, out, 0o600); err != nil {
		t.Fatal(err)
	}
	reloaded, err := NewSubscriptionStore(r.store.path, nil)
	if err != nil {
		t.Fatal(err)
	}
	r.store = reloaded
	r.e = r.engine(t)
}

func TestOneShotInterruptedByARestart(t *testing.T) {
	t.Run("mid-send is listed and never typed", func(t *testing.T) {
		r := newOneShotRig(t)
		sub := r.add(t)
		putDelivery(t, r, sub, "sending")
		r.retries(3)
		r.waitState(t, sub.ID, "review_needed")
		v := r.view()
		if len(v.tried) != 0 || len(v.alerts) != 1 || !strings.Contains(v.alerts[0], "review needed") {
			t.Fatalf("attempts=%q alerts=%q, want no send and one 'review needed'", v.tried, v.alerts)
		}
		if _, reason, _, _ := r.delivery(t, sub.ID); reason != "interrupted" {
			t.Fatalf("reason = %q", reason)
		}
	})
	t.Run("before any key is typed once", func(t *testing.T) {
		r := newOneShotRig(t)
		sub := r.add(t)
		putDelivery(t, r, sub, "waiting")
		r.retries(3)
		r.waitGone(t, sub.ID)
		if v := r.view(); len(v.typed) != 1 {
			t.Fatalf("typed=%q, want once", v.typed)
		}
	})
}

// With no way to save the message first, nothing is typed; it is typed once a
// save works.
func TestOneShotIsNotTypedBeforeItsStateIsSaved(t *testing.T) {
	r := newOneShotRig(t)
	sub := r.add(t)
	good := r.store.path
	r.store.mu.Lock()
	r.store.path = t.TempDir() // a folder: the file cannot be written
	r.store.mu.Unlock()
	r.fire()
	waitFor(t, "an alert", func() bool { return len(r.view().alerts) > 0 })
	r.retries(2)
	if v := r.view(); len(v.tried) != 0 {
		t.Fatalf("typed %q with nothing saved", v.tried)
	}
	r.store.mu.Lock()
	r.store.path = good
	r.store.mu.Unlock()
	r.retries(3)
	r.waitGone(t, sub.ID)
	if v := r.view(); len(v.typed) != 1 || len(v.alerts) != 1 {
		t.Fatalf("typed=%q alerts=%q, want one line and one alert", v.typed, v.alerts)
	}
}

// Expiry stops a subscription firing; a message that already fired is not
// thrown away.
func TestOneShotWaitingMessageIsNotLostAtExpiry(t *testing.T) {
	r := newOneShotRig(t, errRefusedBeforeTyping, errRefusedBeforeTyping)
	sub := r.add(t)
	r.fire()
	r.waitState(t, sub.ID, "not_typed")
	settle()
	if _, err := r.store.DeleteExpired(time.Now().Add(24 * 365 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(time.Minute)
	if _, err := r.store.Update(sub.ID, Subscription{Filter: sub.Filter, Sink: sub.Sink, Action: sub.Action, OneShot: true, ExpiresAt: &past}); err != nil {
		t.Fatal(err)
	}
	removed, err := r.store.DeleteExpired(past.Add(time.Hour))
	if err != nil || len(removed) != 0 {
		t.Fatalf("removed %v (%v), want the waiting message kept", removed, err)
	}
	r.retries(3)
	r.waitGone(t, sub.ID)
	if v := r.view(); len(v.typed) != 1 {
		t.Fatalf("typed=%q, want once", v.typed)
	}
}

func TestOneShotDeletedWhileWaitingIsNotTyped(t *testing.T) {
	r := newOneShotRig(t, errRefusedBeforeTyping, errRefusedBeforeTyping)
	sub := r.add(t)
	r.fire()
	r.waitState(t, sub.ID, "not_typed")
	settle()
	if err := r.store.Delete(sub.ID); err != nil {
		t.Fatal(err)
	}
	r.retries(4)
	if v := r.view(); len(v.typed) != 0 || len(v.tried) != 2 {
		t.Fatalf("attempts=%q typed=%q, want nothing after the delete", v.tried, v.typed)
	}
}

// Editing keeps the waiting message. If the edit made it a standing
// subscription, typing the old message must not remove it.
func TestOneShotEditKeepsTheWaitingMessageAndTheEditedSubscription(t *testing.T) {
	r := newOneShotRig(t, errRefusedBeforeTyping, errRefusedBeforeTyping)
	sub := r.add(t)
	r.fire()
	r.waitState(t, sub.ID, "not_typed")
	settle()
	if _, err := r.store.Update(sub.ID, Subscription{Filter: sub.Filter, Sink: sub.Sink, Action: sub.Action}); err != nil {
		t.Fatal(err)
	}
	if s, _, text, _ := r.delivery(t, sub.ID); s != "not_typed" || text != oneShotText {
		t.Fatalf("after the edit: state=%q text=%q", s, text)
	}
	r.retries(3)
	waitFor(t, "the waiting message", func() bool { return len(r.view().typed) == 1 })
	settle()
	if s, _, _, ok := r.delivery(t, sub.ID); !ok || s != "" {
		t.Fatalf("subscription present=%v delivery=%q, want it kept with nothing waiting", ok, s)
	}
}

// A caller cannot create a subscription that already holds a message to type.
func TestOneShotCreateCannotPlantAWaitingMessage(t *testing.T) {
	r := newOneShotRig(t)
	var planted Subscription
	body := `{"filter":{"kinds":["task.session_started"]},"sink":{"type":"session","target":"ops"},"one_shot":true,` +
		`"delivery":{"state":"waiting","target":"someone-else","text":"planted"}}`
	if err := json.Unmarshal([]byte(body), &planted); err != nil {
		t.Fatal(err)
	}
	sub, err := r.store.Add(planted)
	if err != nil {
		t.Fatal(err)
	}
	r.retries(3)
	if s, _, _, _ := r.delivery(t, sub.ID); s != "" || len(r.view().tried) != 0 {
		t.Fatalf("a created subscription holds state %q and typed %q", s, r.view().tried)
	}
}

func TestOneShotTestFireTypesAndReportsWhatHappened(t *testing.T) {
	r := newOneShotRig(t, errRefusedBeforeTyping)
	sub := r.add(t)
	res, err := r.e.FireTest(sub.ID)
	if err == nil || res.OK || res.Error == "" {
		t.Fatalf("refused test fire reported res=%+v err=%v", res, err)
	}
	if s, _, _, ok := r.delivery(t, sub.ID); !ok || s != "" {
		t.Fatalf("a test fire left present=%v delivery=%q", ok, s)
	}
	res, err = r.e.FireTest(sub.ID)
	if err != nil || !res.OK {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if v := r.view(); len(v.bridged) != 0 || len(v.typed) != 1 || strings.Contains(v.typed[0], oneShotText) {
		t.Fatalf("typed=%q bridged=%q, want only the test text, typed as a real one-time message is", v.typed, v.bridged)
	}
}

// Many matching events at once still type the message once.
func TestOneShotManyEventsTypeOnce(t *testing.T) {
	r := newOneShotRig(t)
	subs := make([]Subscription, 0, 5)
	for i := 0; i < 5; i++ {
		subs = append(subs, r.add(t))
	}
	for i := 0; i < 10; i++ {
		r.fire()
	}
	for _, s := range subs {
		r.waitGone(t, s.ID)
	}
	settle()
	if v := r.view(); len(v.typed) != len(subs) {
		t.Fatalf("%d lines typed for %d one-time subscriptions: %s", len(v.typed), len(subs), fmt.Sprint(v.typed))
	}
}

// A change to one subscription must not throw away events another one has not
// handled yet: for a one-time message that event is the only one.
func TestOneShotQueuedEventSurvivesAChangeToAnotherSubscription(t *testing.T) {
	r := newOneShotRig(t)
	release := make(chan struct{})
	entered := make(chan struct{}, 8)
	r.e.cfg.SendInput = func(id, text, _ string) error {
		entered <- struct{}{}
		<-release
		r.mu.Lock()
		defer r.mu.Unlock()
		r.typed = append(r.typed, id+"|"+text)
		return nil
	}
	r.e.cfg.PostBridge = func(_ int, id, text string) error { return errors.New("no bridge") }
	if _, err := r.store.Add(Subscription{
		Filter: LifecycleFilter{Kinds: []string{KindTaskSessionStarted}},
		Sink:   SubscriptionSink{Type: SinkSession, Target: "ops"},
	}); err != nil {
		t.Fatal(err)
	}
	r.fire()
	select {
	case <-entered: // the first event is being delivered
	case <-time.After(5 * time.Second):
		t.Fatal("no delivery")
	}
	r.fire() // the second waits in the subscription's queue
	if _, err := r.store.Add(Subscription{
		Filter: LifecycleFilter{Kinds: []string{PRDStatusKind(PRDCompleted)}},
		Sink:   SubscriptionSink{Type: SinkSession, Target: "ops"},
	}); err != nil {
		t.Fatal(err)
	}
	close(release)
	waitFor(t, "both events delivered", func() bool { return len(r.view().typed) == 2 })
}

// breakSaves makes every save of the store fail until the returned function
// is called.
func breakSaves(t *testing.T, r *oneShotRig) (restore func()) {
	t.Helper()
	dir := t.TempDir()
	r.store.mu.Lock()
	good := r.store.path
	r.store.path = dir // a folder: the file cannot be written
	r.store.mu.Unlock()
	return func() {
		r.store.mu.Lock()
		r.store.path = good
		r.store.mu.Unlock()
	}
}

// sendThenBreakSaves makes saves fail while the first send runs, so its result
// cannot be saved.
func sendThenBreakSaves(t *testing.T, r *oneShotRig, result error) (restore func()) {
	t.Helper()
	inner := r.e.cfg.SendInput
	var once sync.Once
	restored := func() {}
	var mu sync.Mutex
	r.e.cfg.SendInput = func(id, text, source string) error {
		first := false
		once.Do(func() {
			first = true
			mu.Lock()
			restored = breakSaves(t, r)
			mu.Unlock()
		})
		if !first {
			return inner(id, text, source)
		}
		if result != nil {
			return result
		}
		return inner(id, text, source)
	}
	return func() {
		mu.Lock()
		defer mu.Unlock()
		restored()
	}
}

func TestOneShotRefusalThatCannotBeSavedIsStillRetried(t *testing.T) {
	r := newOneShotRig(t)
	restore := sendThenBreakSaves(t, r, errRefusedBeforeTyping)
	sub := r.add(t)
	r.fire()
	waitFor(t, "an alert", func() bool { return len(r.view().alerts) > 0 })
	settle()
	restore()
	r.retries(4)
	r.waitGone(t, sub.ID)
	if v := r.view(); len(v.typed) != 1 || len(v.alerts) != 1 {
		t.Fatalf("typed=%q alerts=%q, want it typed once with one alert", v.typed, v.alerts)
	}
}

func TestOneShotTypedButNotRemovedIsRemovedLaterAndNotRetyped(t *testing.T) {
	r := newOneShotRig(t)
	restore := sendThenBreakSaves(t, r, nil)
	sub := r.add(t)
	r.fire()
	waitFor(t, "the line", func() bool { return len(r.view().typed) == 1 })
	settle()
	r.retries(2) // still cannot be saved
	restore()
	r.retries(3)
	r.waitGone(t, sub.ID)
	if v := r.view(); len(v.tried) != 1 || len(v.typed) != 1 {
		t.Fatalf("attempts=%q, want the one send only", v.tried)
	}
}

func TestOneShotThatCannotBeSavedAsSendingIsAnnounced(t *testing.T) {
	r := newOneShotRig(t)
	sub := r.add(t)
	putDelivery(t, r, sub, "waiting")
	restore := breakSaves(t, r)
	r.retries(4)
	if v := r.view(); len(v.tried) != 0 || len(v.alerts) != 1 || !strings.HasPrefix(v.alerts[0], "warn|") {
		t.Fatalf("attempts=%q alerts=%q, want nothing typed and one warning", v.tried, v.alerts)
	}
	restore()
	r.retries(3)
	r.waitGone(t, sub.ID)
	if v := r.view(); len(v.typed) != 1 || len(v.alerts) != 1 {
		t.Fatalf("typed=%q alerts=%q, want one line and no second warning", v.typed, v.alerts)
	}
}
