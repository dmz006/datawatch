package server

// Contract tests for the event-log read that the dashboard Live Events card
// polls: GET /api/autonomous/event-log?limit=15. The card relies on the
// newest-first order, the limit, and the time/kind/summary fields of each
// event, so these tests pin exactly that.

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// TestAutomataEventLogFeedLimitAndOrder checks that the card's limit=15 poll
// returns exactly the 15 newest events, newest first, each carrying the
// fields the card renders.
func TestAutomataEventLogFeedLimitAndOrder(t *testing.T) {
	base := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	var raws []automataRawError
	for i := 0; i < 20; i++ {
		raws = append(raws, automataRawError{
			Time:    base.Add(-time.Duration(i) * time.Minute),
			Source:  "merge",
			Raw:     fmt.Sprintf("dial tcp 10.0.0.%d: connection refused", i),
			Project: "datawatch",
		})
	}
	s := eventLogServer(&eventLogFake{
		rawErrs: raws,
		repoCI:  autonomous.RepoCIWatchStatus{Mode: config.RepoCIWatchModeOff},
	})
	code, body := getEventLog(t, s, "limit=15")
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	events, ok := body["events"].([]any)
	if !ok {
		t.Fatalf("events is not an array: %v", body["events"])
	}
	if len(events) != 15 {
		t.Fatalf("limit=15 returned %d events, want 15", len(events))
	}
	var prev time.Time
	for i, raw := range events {
		ev, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("event %d is not an object: %v", i, raw)
		}
		for _, key := range []string{"time", "kind", "summary"} {
			if v, _ := ev[key].(string); v == "" {
				t.Fatalf("event %d missing %q: %v", i, key, ev)
			}
		}
		at, err := time.Parse(time.RFC3339Nano, ev["time"].(string))
		if err != nil {
			t.Fatalf("event %d time %q is not RFC 3339: %v", i, ev["time"], err)
		}
		if i > 0 && at.After(prev) {
			t.Fatalf("event %d (%s) is newer than the one before it; want newest first", i, at)
		}
		prev = at
	}
}

// TestAutomataEventLogFeedEventsAlwaysArray checks that the events field is a
// JSON array even when the fake has no raw errors, so the card's
// Array.isArray guard never has to handle null. The empty-card rendering
// itself is covered by the node tests.
func TestAutomataEventLogFeedEventsAlwaysArray(t *testing.T) {
	s := eventLogServer(&eventLogFake{repoCI: autonomous.RepoCIWatchStatus{Mode: config.RepoCIWatchModeOff}})
	code, body := getEventLog(t, s, "limit=15")
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	if _, ok := body["events"].([]any); !ok {
		t.Fatalf("events must be an array, got %v", body["events"])
	}
}
