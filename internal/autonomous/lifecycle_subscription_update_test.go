// Tests for SubscriptionStore.Update and Engine.FireTest.

package autonomous

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func updSub() Subscription {
	return Subscription{
		Owner:  "alice",
		Filter: LifecycleFilter{Kinds: []string{KindTaskSessionStarted}},
		Sink:   SubscriptionSink{Type: SinkSession, Target: "ops"},
	}
}

func TestSubscriptionUpdateKeepsIDAndCreatedAt(t *testing.T) {
	store, _ := NewSubscriptionStore(t.TempDir()+"/s.json", nil)
	orig, err := store.Add(updSub())
	if err != nil {
		t.Fatal(err)
	}
	exp := time.Now().Add(time.Hour)
	got, err := store.Update(orig.ID, Subscription{
		ID:        "other",
		Filter:    LifecycleFilter{Kinds: []string{PRDStatusKind(PRDCompleted)}},
		Sink:      SubscriptionSink{Type: SinkAlert, Target: "x"},
		OneShot:   true,
		Coalesce:  true,
		ExpiresAt: &exp,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != orig.ID || !got.CreatedAt.Equal(orig.CreatedAt) {
		t.Fatalf("id/created_at changed: %+v vs %+v", got, orig)
	}
	if got.Owner != "alice" {
		t.Errorf("owner not kept: %q", got.Owner)
	}
	if got.Sink.Type != SinkAlert || !got.OneShot || !got.Coalesce || got.ExpiresAt == nil || got.Filter.Kinds[0] != PRDStatusKind(PRDCompleted) {
		t.Errorf("fields not replaced: %+v", got)
	}
	if stored, _ := store.Get(orig.ID); stored.Sink.Type != SinkAlert {
		t.Errorf("not stored")
	}
	got, _ = store.Update(orig.ID, Subscription{Owner: "bob", Filter: got.Filter, Sink: got.Sink})
	if got.Owner != "bob" {
		t.Errorf("supplied owner not applied: %q", got.Owner)
	}
}

func TestSubscriptionUpdateValidationFailureLeavesRecord(t *testing.T) {
	store, _ := NewSubscriptionStore(t.TempDir()+"/s.json", nil)
	orig, _ := store.Add(updSub())
	changed := 0
	store.OnChanged(func() { changed++ })
	_, err := store.Update(orig.ID, Subscription{Filter: LifecycleFilter{}, Sink: orig.Sink})
	if err == nil {
		t.Fatal("want validation error")
	}
	_, err = store.Update(orig.ID, Subscription{Filter: orig.Filter, Sink: SubscriptionSink{Type: "webhook", Target: "http://x"}})
	if err == nil {
		t.Fatal("webhook must stay rejected")
	}
	cur, _ := store.Get(orig.ID)
	if cur.Sink != orig.Sink || len(cur.Filter.Kinds) != 1 || cur.Filter.Kinds[0] != KindTaskSessionStarted {
		t.Errorf("record changed: %+v", cur)
	}
	if changed != 0 {
		t.Errorf("OnChanged fired %d times on failure", changed)
	}
}

func TestSubscriptionUpdateUnknownID(t *testing.T) {
	store, _ := NewSubscriptionStore(t.TempDir()+"/s.json", nil)
	_, err := store.Update("nope", updSub())
	if !errors.Is(err, ErrSubscriptionNotFound) {
		t.Fatalf("want not-found, got %v", err)
	}
}

func TestSubscriptionUpdateFiresOnChangedAndEngineSwapsFilter(t *testing.T) {
	rec := &sessionPushRecorder{}
	m, store, e := newEngineForTest(t, rec, sessionTable{"ops": 9013}, 0)
	var mu sync.Mutex
	var audited []string
	e.auditHook = func(action string, _ map[string]any) {
		mu.Lock()
		audited = append(audited, action)
		mu.Unlock()
	}
	sub, err := store.Add(updSub())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Update(sub.ID, Subscription{
		Filter: LifecycleFilter{Kinds: []string{PRDStatusKind(PRDCompleted)}},
		Sink:   sub.Sink,
	}); err != nil {
		t.Fatal(err)
	}
	m.emit(LifecycleEvent{Kind: PRDStatusKind(PRDCompleted), Level: LevelPRD, PRDID: "prd-1"})
	waitFor(t, "delivery on new filter", func() bool { return rec.bridgeCount() == 1 })
	m.emit(startedEvent()) // old filter no longer matches
	time.Sleep(100 * time.Millisecond)
	if rec.bridgeCount() != 1 {
		t.Errorf("old filter still delivering: %d", rec.bridgeCount())
	}
	mu.Lock()
	defer mu.Unlock()
	found := false
	for _, a := range audited {
		if a == auditLifecycleSubUpdate {
			found = true
		}
	}
	if !found {
		t.Errorf("no update audit in %v", audited)
	}
}

func TestSubscriptionFireTestPerSink(t *testing.T) {
	rec := &sessionPushRecorder{}
	m, store, e := newEngineForTest(t, rec, sessionTable{"ops": 9013}, 0)
	_ = m
	var alerts, topics []string
	e.cfg.RecordAlert = func(level, title, body string) { alerts = append(alerts, body) }
	e.cfg.PublishTopics = func(tp []string, title, msg string) { topics = append(topics, msg) }
	e.cfg.PendingQueue = NewPendingEventQueue(10)

	cases := []struct {
		name string
		sink SubscriptionSink
	}{
		{SinkSession, SubscriptionSink{Type: SinkSession, Target: "ops"}},
		{SinkComm, SubscriptionSink{Type: SinkComm, Target: "all"}},
		{SinkTopic, SubscriptionSink{Type: SinkTopic, Target: "t"}},
		{SinkAlert, SubscriptionSink{Type: SinkAlert, Target: "a"}},
		{SinkMCPQueue, SubscriptionSink{Type: SinkMCPQueue, Target: "q1"}},
	}
	for _, c := range cases {
		sub, err := store.Add(Subscription{Filter: LifecycleFilter{Kinds: []string{PRDStatusKind(PRDCompleted)}}, Sink: c.sink})
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		res, err := e.FireTest(sub.ID)
		if err != nil || !res.OK || res.Sink != c.name {
			t.Fatalf("%s: res=%+v err=%v", c.name, res, err)
		}
	}
	if rec.bridgeCount() != 1 || !strings.Contains(rec.bridge[0], "[test] ") {
		t.Errorf("session: %v", rec.bridge)
	}
	if len(rec.comm) != 1 || !strings.HasPrefix(rec.comm[0], "[test] ") {
		t.Errorf("comm: %v", rec.comm)
	}
	if len(alerts) != 1 || !strings.HasPrefix(alerts[0], "[test] ") {
		t.Errorf("alert: %v", alerts)
	}
	if len(topics) != 1 || !strings.HasPrefix(topics[0], "[test] ") {
		t.Errorf("topic: %v", topics)
	}
	evs, _, _ := e.cfg.PendingQueue.Pending("q1", 0, 10)
	if len(evs) != 1 || !evs[0].Test || evs[0].Kind != KindPRDUpdated {
		t.Errorf("queue: %+v", evs)
	}
}

func TestSubscriptionFireTestKeepsOneShotAndIgnoresActionText(t *testing.T) {
	rec := &sessionPushRecorder{}
	_, store, e := newEngineForTest(t, rec, sessionTable{"ops": 9013}, 0)
	sub, err := store.Add(Subscription{
		Filter:  LifecycleFilter{Kinds: []string{KindTaskSessionStarted}},
		Sink:    SubscriptionSink{Type: SinkSession, Target: "ops"},
		Action:  &SubscriptionAction{Type: SinkActionSendInput, Text: "SECRET-ACTION"},
		OneShot: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res, err := e.FireTest(sub.ID); err != nil || !res.OK {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if _, ok := store.Get(sub.ID); !ok {
		t.Fatal("one-shot consumed by test")
	}
	if strings.Contains(strings.Join(rec.bridge, "\n"), "SECRET-ACTION") {
		t.Errorf("action text used: %v", rec.bridge)
	}
}

func TestSubscriptionFireTestUnavailableSinkAndErrors(t *testing.T) {
	rec := &sessionPushRecorder{}
	_, store, e := newEngineForTest(t, rec, sessionTable{"ops": 0}, 0)
	for _, sink := range []SubscriptionSink{
		{Type: SinkAlert, Target: "a"},
		{Type: SinkTopic, Target: "t"},
		{Type: SinkMCPQueue, Target: "q"},
	} {
		sub, _ := store.Add(Subscription{Filter: LifecycleFilter{Kinds: []string{PRDStatusKind(PRDCompleted)}}, Sink: sink})
		res, err := e.FireTest(sub.ID)
		if err == nil || res.OK || !strings.Contains(err.Error(), "sink not available") || res.Error == "" {
			t.Errorf("%s: res=%+v err=%v", sink.Type, res, err)
		}
	}
	// session delivery error surfaces from the SendInput path
	rec.inputErr = errors.New("boom")
	sub, _ := store.Add(Subscription{Filter: LifecycleFilter{Kinds: []string{PRDStatusKind(PRDCompleted)}}, Sink: SubscriptionSink{Type: SinkSession, Target: "ops"}})
	res, err := e.FireTest(sub.ID)
	if err == nil || res.OK || !strings.Contains(res.Error, "boom") {
		t.Errorf("session: res=%+v err=%v", res, err)
	}
	if _, err := e.FireTest("missing"); !errors.Is(err, ErrSubscriptionNotFound) {
		t.Errorf("unknown id: %v", err)
	}
}
