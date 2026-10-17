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
// matched event (kind in Kinds), with title == event kind and body
// naming the PRD; statuses NOT in Kinds deliver nothing.
func TestSinksAlertWrites(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer m.lifecycleBus().Close()

	rec := &alertRecorder{}
	kinds := []string{KindPRDUpdated, PRDStatusKind(PRDCompleted)}
	cancel := InstallLifecycleSinks(m, SinkConfig{Kinds: kinds, RecordAlert: rec.Record})
	defer cancel()

	const prdID = "prd-sink-alert"
	m.emit(LifecycleEvent{Kind: KindPRDUpdated, Level: LevelPRD, PRDID: prdID, NewStatus: string(PRDCompleted)})

	waitFor(t, "alert sink delivery", func() bool { return rec.count() == 1 })
	if rec.count() != 1 {
		t.Fatalf("RecordAlert called %d times after one matched event, want 1", rec.count())
	}
	level, title, body := rec.get(0)
	if title != KindPRDUpdated {
		t.Errorf("alert title = %q, want %q (the event kind)", title, KindPRDUpdated)
	}
	if !strings.Contains(body, prdID) {
		t.Errorf("alert body %q does not contain PRD id %q", body, prdID)
	}
	_ = level // level mapping is a daemon concern; just check it was non-empty
	if level == "" {
		t.Errorf("alert level is empty; want info/warn/error")
	}

	// Filtering the other way: a status kind NOT in Kinds must not
	// reach the sink either.
	m.emit(LifecycleEvent{Kind: PRDStatusKind(PRDCancelled), Level: LevelPRD, PRDID: "prd-other", NewStatus: string(PRDCancelled)})
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
	cancel := InstallLifecycleSinks(m, SinkConfig{Kinds: []string{KindPRDUpdated}, PublishTopics: rec.Publish})
	defer cancel()

	const prdID = "prd-sink-topic"
	m.emit(LifecycleEvent{Kind: KindPRDUpdated, Level: LevelPRD, PRDID: prdID, NewStatus: string(PRDCompleted)})

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
	if title != KindPRDUpdated {
		t.Errorf("publish title = %q, want %q (the event kind)", title, KindPRDUpdated)
	}
	if message == "" {
		t.Error("publish message is empty; want non-empty")
	}
	if !strings.Contains(message, prdID) {
		t.Errorf("publish message %q does not contain PRD id %q", message, prdID)
	}
}

// TestSinksEmptyKindsDeliverNothing — the default-config guarantee:
// an empty Kinds list subscribes NOTHING, so no lifecycle event ever
// reaches either sink. This is exactly the pre-sink behavior: empty
// default event set = zero new comm messages.
func TestSinksEmptyKindsDeliverNothing(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer m.lifecycleBus().Close()

	alerts := &alertRecorder{}
	topics := &topicRecorder{}
	cancel := InstallLifecycleSinks(m, SinkConfig{RecordAlert: alerts.Record, PublishTopics: topics.Publish})
	defer cancel()

	// Both a bare prd.updated and a couple of statuses that a fully
	// enabled sink would match — none may get through with empty Kinds.
	m.emit(LifecycleEvent{Kind: KindPRDUpdated, Level: LevelPRD, PRDID: "prd-empty", NewStatus: string(PRDCompleted)})
	m.emit(LifecycleEvent{Kind: PRDStatusKind(PRDCompleted), Level: LevelPRD, PRDID: "prd-empty", NewStatus: string(PRDCompleted)})
	time.Sleep(300 * time.Millisecond)

	if got := alerts.count(); got != 0 {
		t.Errorf("RecordAlert called %d times with empty Kinds, want 0", got)
	}
	if got := topics.count(); got != 0 {
		t.Errorf("PublishTopics called %d times with empty Kinds, want 0", got)
	}
}
