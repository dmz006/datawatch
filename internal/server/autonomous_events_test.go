// REST tests for /api/autonomous/events, driven through the handler
// against the real autonomous.PendingEventQueue.

package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

type pendingEventsBody struct {
	Events     []autonomous.LifecycleEvent `json:"events"`
	NextCursor uint64                      `json:"next_cursor"`
	Dropped    bool                        `json:"dropped"`
}

func doPendingEvents(t *testing.T, s *Server, query string) (*httptest.ResponseRecorder, pendingEventsBody) {
	t.Helper()
	rec := httptest.NewRecorder()
	s.handleAutonomousEvents(rec, httptest.NewRequest(http.MethodGet, "/api/autonomous/events"+query, nil))
	var body pendingEventsBody
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode %q: %v", rec.Body.String(), err)
		}
	}
	return rec, body
}

func fillPendingQueue(q *autonomous.PendingEventQueue, queue string, n int) {
	for i := 0; i < n; i++ {
		q.Append(queue, autonomous.LifecycleEvent{ID: fmt.Sprintf("e%d", i+1), Kind: "prd.blocked", PRDID: "prd_1"})
	}
}

func TestPendingEventsRESTHappyPath(t *testing.T) {
	q := autonomous.NewPendingEventQueue(10)
	fillPendingQueue(q, "ops", 3)
	s := &Server{autonomousEvents: q}

	rec, body := doPendingEvents(t, s, "?queue=ops")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if len(body.Events) != 3 || body.NextCursor != 3 || body.Dropped {
		t.Fatalf("got %+v", body)
	}
	// Passing next_cursor back returns only newer events.
	fillPendingQueue(q, "ops", 1)
	_, body = doPendingEvents(t, s, "?queue=ops&since=3")
	if len(body.Events) != 1 || body.NextCursor != 4 {
		t.Fatalf("after since=3: %+v", body)
	}
}

func TestPendingEventsRESTEmptyIsArrayNotNull(t *testing.T) {
	s := &Server{autonomousEvents: autonomous.NewPendingEventQueue(10)}
	rec, body := doPendingEvents(t, s, "?queue=nothing&since=7")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	var raw map[string]json.RawMessage
	_ = json.Unmarshal(rec.Body.Bytes(), &raw)
	if string(raw["events"]) != "[]" {
		t.Fatalf("events = %s, want []", raw["events"])
	}
	if body.NextCursor != 7 || body.Dropped {
		t.Fatalf("got %+v", body)
	}
}

func TestPendingEventsREST400(t *testing.T) {
	s := &Server{autonomousEvents: autonomous.NewPendingEventQueue(10)}
	for _, query := range []string{"", "?queue=", "?queue=a&since=abc", "?queue=a&since=-1", "?queue=a&limit=x", "?queue=a&limit=0"} {
		if rec, _ := doPendingEvents(t, s, query); rec.Code != http.StatusBadRequest {
			t.Errorf("%q: want 400, got %d", query, rec.Code)
		}
	}
}

func TestPendingEventsREST503WhenNotWired(t *testing.T) {
	if rec, _ := doPendingEvents(t, &Server{}, "?queue=ops"); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d", rec.Code)
	}
}

func TestPendingEventsRESTMethodNotAllowed(t *testing.T) {
	s := &Server{autonomousEvents: autonomous.NewPendingEventQueue(10)}
	rec := httptest.NewRecorder()
	s.handleAutonomousEvents(rec, httptest.NewRequest(http.MethodPost, "/api/autonomous/events?queue=a", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("want 405, got %d", rec.Code)
	}
}

func TestPendingEventsRESTFederationCaps(t *testing.T) {
	q := autonomous.NewPendingEventQueue(10)
	fillPendingQueue(q, "ops", 1)
	s := &Server{autonomousEvents: q}

	rec := httptest.NewRecorder()
	s.handleAutonomousEvents(rec, asPeer(httptest.NewRequest(http.MethodGet, "/api/autonomous/events?queue=ops", nil), "sessions:read"))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("caps-less peer: want 403, got %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	s.handleAutonomousEvents(rec, asPeer(httptest.NewRequest(http.MethodGet, "/api/autonomous/events?queue=ops", nil), "autonomous:read"))
	if rec.Code != http.StatusOK {
		t.Fatalf("autonomous:read peer: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestPendingEventsRESTLimitCap(t *testing.T) {
	q := autonomous.NewPendingEventQueue(500)
	fillPendingQueue(q, "big", 300)
	s := &Server{autonomousEvents: q}

	_, body := doPendingEvents(t, s, "?queue=big")
	if len(body.Events) != pendingEventsDefaultLimit {
		t.Errorf("default limit: got %d events, want %d", len(body.Events), pendingEventsDefaultLimit)
	}
	_, body = doPendingEvents(t, s, "?queue=big&limit=10")
	if len(body.Events) != 10 || body.NextCursor != 10 {
		t.Errorf("limit=10: got %d events, next %d", len(body.Events), body.NextCursor)
	}
	_, body = doPendingEvents(t, s, "?queue=big&limit=100000")
	if len(body.Events) != pendingEventsMaxLimit {
		t.Errorf("capped limit: got %d events, want %d", len(body.Events), pendingEventsMaxLimit)
	}
}

func TestPendingEventsRESTDroppedFlagRoundTrip(t *testing.T) {
	q := autonomous.NewPendingEventQueue(3)
	fillPendingQueue(q, "tiny", 5) // cursors 1-5; 1 and 2 are evicted
	s := &Server{autonomousEvents: q}

	_, body := doPendingEvents(t, s, "?queue=tiny&since=0")
	if !body.Dropped || len(body.Events) != 3 {
		t.Fatalf("since=0: want dropped with 3 events, got %+v", body)
	}
	// A caller that kept up (cursor past the eviction point) is not told
	// it lost events.
	_, body = doPendingEvents(t, s, "?queue=tiny&since=2")
	if body.Dropped || len(body.Events) != 3 {
		t.Fatalf("since=2: want no drop, got %+v", body)
	}
}
