package autonomous

import (
	"strings"
	"sync"
	"testing"
	"time"
)

// alertRecorder captures RecordAlert calls for assertions.
type alertRecorder struct {
	mu    sync.Mutex
	calls []string // "level|title|body"
}

func (a *alertRecorder) Record(level, title, body string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls = append(a.calls, level+"|"+title+"|"+body)
}

func (a *alertRecorder) count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.calls)
}

func (a *alertRecorder) get(i int) (level, title, body string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	parts := strings.SplitN(a.calls[i], "|", 3)
	if len(parts) == 3 {
		return parts[0], parts[1], parts[2]
	}
	return parts[0], "", ""
}

// topicRecorder captures PublishTopics calls for assertions.
type topicRecorder struct {
	mu      sync.Mutex
	calls   int
	topics  []string
	title   string
	message string
}

func (r *topicRecorder) Publish(topics []string, title, message string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	r.topics = append([]string(nil), topics...)
	r.title = title
	r.message = message
}

func (r *topicRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

func (r *topicRecorder) snapshot() (topics []string, title, message string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.topics...), r.title, r.message
}

// TestSinksAlertWrites — the alert sink records exactly one alert per
// event whose kind is in the resolved set (no Automaton here, so the
// global tier — the shipped five-kind baseline), with title == event
// kind and body naming the Automaton; a status kind OUTSIDE the
// resolved set (prd.rejected is no default) delivers nothing.
func TestSinksAlertWrites(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer m.lifecycleBus().Close()

	rec := &alertRecorder{}
	cancel := InstallLifecycleSinks(m, SinkConfig{RecordAlert: rec.Record})
	defer cancel()

	const prdID = "prd-sink-alert"
	m.emit(LifecycleEvent{Kind: PRDStatusKind(PRDCompleted), Level: LevelPRD, PRDID: prdID, NewStatus: string(PRDCompleted)})

	waitFor(t, "alert sink delivery", func() bool { return rec.count() == 1 })
	if rec.count() != 1 {
		t.Fatalf("RecordAlert called %d times after one matched event, want 1", rec.count())
	}
	level, title, body := rec.get(0)
	if title != PRDStatusKind(PRDCompleted) {
		t.Errorf("alert title = %q, want %q (the event kind)", title, PRDStatusKind(PRDCompleted))
	}
	if !strings.Contains(body, prdID) {
		t.Errorf("alert body %q does not contain Automaton ID %q", body, prdID)
	}
	_ = level // level mapping is a daemon concern; just check it was non-empty
	if level == "" {
		t.Errorf("alert level is empty; want info/warn/error")
	}

	// Filtering the other way: a status kind NOT in the resolved set
	// (prd.rejected is not one of the five shipped defaults) must not
	// reach the sink either.
	m.emit(LifecycleEvent{Kind: PRDStatusKind(PRDRejected), Level: LevelPRD, PRDID: "prd-other", NewStatus: string(PRDRejected)})
	time.Sleep(300 * time.Millisecond)
	if got := rec.count(); got != 1 {
		t.Errorf("RecordAlert called %d times after an unmatched status event, want still 1 (filtering must work both ways)", got)
	}
}

// TestSinksTopicPublish — the topic sink publishes each matched event
// to "alerts" plus the per-PRD feed, with title == kind and a
// non-empty message carrying the PRD id.
func TestSinksTopicPublish(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer m.lifecycleBus().Close()

	rec := &topicRecorder{}
	cancel := InstallLifecycleSinks(m, SinkConfig{PublishTopics: rec.Publish})
	defer cancel()

	const prdID = "prd-sink-topic"
	m.emit(LifecycleEvent{Kind: PRDStatusKind(PRDCompleted), Level: LevelPRD, PRDID: prdID, NewStatus: string(PRDCompleted)})

	waitFor(t, "topic sink delivery", func() bool { return rec.count() >= 1 })
	topics, title, message := rec.snapshot()
	if rec.count() != 1 {
		t.Fatalf("PublishTopics called %d times after one matched event, want 1", rec.count())
	}

	hasAlerts, hasPRD := false, false
	for _, topic := range topics {
		switch topic {
		case "alerts":
			hasAlerts = true
		case "automaton-" + prdID:
			hasPRD = true
		}
	}
	if !hasAlerts {
		t.Errorf("topics %v do not contain %q", topics, "alerts")
	}
	if !hasPRD {
		t.Errorf("topics %v do not contain %q (per-PRD feed)", topics, "automaton-"+prdID)
	}
	if title != PRDStatusKind(PRDCompleted) {
		t.Errorf("publish title = %q, want %q (the event kind)", title, PRDStatusKind(PRDCompleted))
	}
	if message == "" {
		t.Error("publish message is empty; want non-empty")
	}
	if !strings.Contains(message, prdID) {
		t.Errorf("publish message %q does not contain Automaton ID %q", message, prdID)
	}
}

// TestSinksGlobalOffDeliversNothing — the `default_events: []` opt-out:
// with the global tier turned OFF (non-nil, empty — distinct from the
// unset/nil case) and no Automaton persisted, a status event that the
// shipped default WOULD deliver (prd.completed) is gated out — yet a
// SYNTHETIC test event (Test: true, i.e. a subscription test-fire) is
// never gated and still delivers.
func TestSinksGlobalOffDeliversNothing(t *testing.T) {
	cfg := DefaultConfig()
	cfg.NotifyDefaultEvents = []string{} // explicit global-tier opt-out
	m, err := NewManager(t.TempDir(), cfg, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer m.lifecycleBus().Close()

	alerts := &alertRecorder{}
	cancel := InstallLifecycleSinks(m, SinkConfig{RecordAlert: alerts.Record})
	defer cancel()

	// Neither event may get through with the global tier off.
	m.emit(LifecycleEvent{Kind: PRDStatusKind(PRDCompleted), Level: LevelPRD, PRDID: "prd-off", NewStatus: string(PRDCompleted)})
	m.emit(LifecycleEvent{Kind: KindPRDUpdated, Level: LevelPRD, PRDID: "prd-off", NewStatus: string(PRDCompleted)})
	time.Sleep(300 * time.Millisecond)

	if got := alerts.count(); got != 0 {
		t.Errorf("RecordAlert called %d times with the global tier off, want 0", got)
	}

	// A synthetic test event is never gated — delivery still works.
	m.emit(LifecycleEvent{Kind: PRDStatusKind(PRDCompleted), Level: LevelPRD, PRDID: "prd-off", NewStatus: string(PRDCompleted), Test: true})
	waitFor(t, "test-event delivery with global tier off", func() bool { return alerts.count() == 1 })
	if got := alerts.count(); got != 1 {
		t.Errorf("RecordAlert called %d times after the synthetic test event, want 1", got)
	}
}
