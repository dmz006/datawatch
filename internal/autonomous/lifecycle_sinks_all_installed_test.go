package autonomous

import (
	"slices"
	"strings"
	"testing"
	"time"
)

// The daemon installs the alert sink, the topic sink and the comm
// fan-out together. These tests use that wiring. The comm window is an
// hour, so nothing can flush on a timer: anything the alert or topic
// recorder holds was delivered immediately, and the comm text is only
// released by cancel(), which flushes what the bucket accumulated.

func TestSinksAllInstalledOneEventReachesAll(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer m.lifecycleBus().Close()

	alerts := &alertRecorder{}
	topicRec := &topicRecorder{}
	comm := &textCapture{}
	cancel := InstallLifecycleSinks(m, SinkConfig{
		RecordAlert:   alerts.Record,
		PublishTopics: topicRec.Publish,
		CommSink:      comm.record,
		CommWindow:    time.Hour,
	})
	defer cancel()

	const prdID = "prd-sink-all"
	kind := PRDStatusKind(PRDBlocked)
	m.emit(LifecycleEvent{Kind: kind, Level: LevelPRD, PRDID: prdID, NewStatus: string(PRDBlocked)})

	waitFor(t, "alert and topic delivery with the comm fan-out installed", func() bool {
		return alerts.count() == 1 && topicRec.count() == 1
	})
	level, title, body := alerts.get(0)
	if level != "error" || title != kind || !strings.Contains(body, prdID) {
		t.Errorf("alert = %q|%q|%q, want level error, title %q, body naming %q", level, title, body, kind, prdID)
	}
	topics, pubTitle, message := topicRec.snapshot()
	if !slices.Contains(topics, "alerts") || !slices.Contains(topics, "automaton-"+prdID) {
		t.Errorf("topics = %v, want alerts and automaton-%s", topics, prdID)
	}
	if pubTitle != kind || message != body {
		t.Errorf("publish = %q|%q, want title %q and the alert body %q", pubTitle, message, kind, body)
	}
	if got := comm.snapshot(); len(got) != 0 {
		t.Fatalf("comm text sent before the quiet window ended: %q", got)
	}

	cancel() // flushes the comm bucket
	got := comm.snapshot()
	if len(got) != 1 {
		t.Fatalf("comm sends after flush = %d, want 1: %q", len(got), got)
	}
	mustContain(t, got[0], "Lifecycle updates (1)", body)
}

// A burst: coalescing applies to the comm text only. Every event is its
// own alert and its own topic publish; comm gets one message.
func TestSinksAllInstalledOnlyCommTextCoalesces(t *testing.T) {
	m := newCommFanoutManager(t, "prd-burst")
	alerts := &alertRecorder{}
	topicRec := &topicRecorder{}
	comm := &textCapture{}
	cancel := InstallLifecycleSinks(m, SinkConfig{
		RecordAlert:   alerts.Record,
		PublishTopics: topicRec.Publish,
		CommSink:      comm.record,
		CommWindow:    time.Hour,
	})
	defer cancel()

	taskIDs := []string{"burst-1", "burst-2", "burst-3"}
	for _, id := range taskIDs {
		commFanoutFire(m, "prd-burst", id)
	}
	waitFor(t, "one alert and one publish per event", func() bool {
		return alerts.count() == len(taskIDs) && topicRec.count() == len(taskIDs)
	})
	if got := comm.snapshot(); len(got) != 0 {
		t.Fatalf("comm text sent before the quiet window ended: %q", got)
	}
	cancel()
	got := comm.snapshot()
	if len(got) != 1 {
		t.Fatalf("comm sends after flush = %d, want 1: %q", len(got), got)
	}
	mustContain(t, got[0], append([]string{"Lifecycle updates (3)"}, taskIDs...)...)
}

// The notify gate applies to all three sinks alike.
func TestSinksAllInstalledGatedEventReachesNone(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer m.lifecycleBus().Close()

	alerts := &alertRecorder{}
	topicRec := &topicRecorder{}
	comm := &textCapture{}
	cancel := InstallLifecycleSinks(m, SinkConfig{
		RecordAlert:   alerts.Record,
		PublishTopics: topicRec.Publish,
		CommSink:      comm.record,
		CommWindow:    time.Hour,
	})
	defer cancel()

	// prd.rejected is not one of the shipped default kinds. The allowed
	// event behind it marks that the subscriber has processed both.
	m.emit(LifecycleEvent{Kind: PRDStatusKind(PRDRejected), Level: LevelPRD, PRDID: "prd-gated", NewStatus: string(PRDRejected)})
	m.emit(LifecycleEvent{Kind: PRDStatusKind(PRDCompleted), Level: LevelPRD, PRDID: "prd-allowed", NewStatus: string(PRDCompleted)})
	waitFor(t, "the allowed event", func() bool { return alerts.count() >= 1 && topicRec.count() >= 1 })
	cancel()

	if alerts.count() != 1 || topicRec.count() != 1 {
		t.Errorf("alerts=%d publishes=%d, want 1 and 1", alerts.count(), topicRec.count())
	}
	got := comm.snapshot()
	if len(got) != 1 {
		t.Fatalf("comm sends = %d, want 1: %q", len(got), got)
	}
	mustNotContain(t, "comm", got[0], "prd-gated")
}
