package main

import (
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/alerts"
	"github.com/dmz006/datawatch/internal/router"
	"github.com/dmz006/datawatch/internal/server"
)

// A lifecycle alert goes into the alert store at once, but its chat text
// belongs to the coalescing comm fan-out. The daemon's alert listener
// sends every other session-less alert to the comm channels, so it must
// not send this one as well.
func TestLifecycleAlertIsNotSentToCommByTheAlertListener(t *testing.T) {
	store, err := alerts.NewStore(filepath.Join(t.TempDir(), "alerts.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	chat := newFakeAlertCapture("signal")
	routers := []*router.Router{router.NewRouter("host", "sig-1", chat, nil, 0, nil)}
	var ntfyTexts, emailTexts []string
	var listened []*alerts.Alert
	// The daemon's listener: session-less alerts go to the comm channels.
	store.AddListener(func(a *alerts.Alert) {
		listened = append(listened, a)
		if a.SessionID == "" {
			sendSystemAlertToComm(a, "host", nil, routers,
				func(text string) error { ntfyTexts = append(ntfyTexts, text); return nil },
				func(text string) error { emailTexts = append(emailTexts, text); return nil })
		}
	})

	lifecycleAlertSink(store)("error", "prd.failed", "PRD prd-1: running -> failed")

	got := store.List()
	if len(got) != 1 || got[0].Level != alerts.LevelError || got[0].Title != "prd.failed" ||
		got[0].Body != "PRD prd-1: running -> failed" || got[0].SessionID != "" {
		t.Fatalf("stored alerts = %+v, want the one lifecycle alert", got)
	}
	// The web app and the other alert consumers still hear about it.
	if len(listened) != 1 {
		t.Fatalf("alert listener calls = %d, want 1", len(listened))
	}
	time.Sleep(200 * time.Millisecond) // let a wrong router send land
	if n := chat.count(); n != 0 || len(ntfyTexts) != 0 || len(emailTexts) != 0 {
		t.Fatalf("the alert listener sent the lifecycle alert to comm: router=%d %q ntfy=%q email=%q",
			n, chat.all(), ntfyTexts, emailTexts)
	}

	// Any other session-less alert still reaches the comm channels.
	store.Add(alerts.LevelWarn, "other system alert", "body", "")
	deadline := time.Now().Add(2 * time.Second)
	for chat.count() < 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if chat.count() != 1 || len(ntfyTexts) != 1 || len(emailTexts) != 1 {
		t.Fatalf("other system alert: router=%d ntfy=%d email=%d, want 1 each",
			chat.count(), len(ntfyTexts), len(emailTexts))
	}
}

// One event is one publish call over both topics, so a registered mobile
// endpoint is dialled once and not once per topic. The publisher dials
// endpoints for the last topic in the list, which is the global feed.
func TestLifecycleTopicSinkPublishesOnce(t *testing.T) {
	var calls [][]string
	var events []server.PushEvent
	sink := lifecycleTopicSink(func(topics []string, ev server.PushEvent) {
		calls = append(calls, slices.Clone(topics))
		events = append(events, ev)
	})
	topics := []string{"alerts", "automaton-prd-1"}

	sink(topics, "prd.failed", "PRD prd-1: running -> failed")

	if len(calls) != 1 {
		t.Fatalf("publish calls = %d (%v), want 1", len(calls), calls)
	}
	if want := []string{"automaton-prd-1", "alerts"}; !slices.Equal(calls[0], want) {
		t.Errorf("published topics = %v, want %v", calls[0], want)
	}
	if !slices.Equal(topics, []string{"alerts", "automaton-prd-1"}) {
		t.Errorf("the caller's topic slice was modified: %v", topics)
	}
	if ev := events[0]; ev.Title != "prd.failed" || ev.Message != "PRD prd-1: running -> failed" || !slices.Equal(ev.Tags, []string{"lifecycle"}) {
		t.Errorf("event = %+v", ev)
	}
}
