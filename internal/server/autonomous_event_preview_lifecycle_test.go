package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// previewLifecycleFake serves recorded lifecycle events to the preview route.
type previewLifecycleFake struct {
	eventLogFake
	events []autonomous.LifecycleEvent
	since  time.Time
}

func (f *previewLifecycleFake) RecentLifecycleEvents(since time.Time) ([]autonomous.LifecycleEvent, time.Time) {
	var out []autonomous.LifecycleEvent
	for _, e := range f.events {
		if !e.At.Before(since) {
			out = append(out, e)
		}
	}
	return out, f.since
}

func postPreview(t *testing.T, s *Server, body string) (int, eventPreviewResponse) {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleEventPreview(rr, httptest.NewRequest(http.MethodPost, "/api/autonomous/subscriptions/preview", strings.NewReader(body)))
	var resp eventPreviewResponse
	if rr.Code == http.StatusOK {
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v; raw %q", err, rr.Body.String())
		}
	}
	if rr.Code != http.StatusOK {
		t.Logf("status %d body %q", rr.Code, strings.TrimSpace(rr.Body.String()))
	}
	return rr.Code, resp
}

// The picker lists lifecycle kinds; the preview must count them, and must keep
// counting Events log kinds, and must still refuse a kind that is neither.
func TestEventPreviewAcceptsLifecycleAndLogKinds(t *testing.T) {
	now := time.Now().UTC()
	fake := &previewLifecycleFake{
		since: now.Add(-3 * time.Hour),
		events: []autonomous.LifecycleEvent{
			{ID: "a", At: now.Add(-10 * time.Minute), Kind: autonomous.KindTaskLooping, PRDID: "p1", NewStatus: "failed"},
			{ID: "b", At: now.Add(-20 * time.Minute), Kind: autonomous.KindTaskLooping, PRDID: "p2"},
			{ID: "c", At: now.Add(-30 * time.Minute), Kind: autonomous.KindBurnGuardRungChanged},
			{ID: "d", At: now.Add(-40 * time.Minute), Kind: autonomous.KindAutomatonStuckRunning, PRDID: "p3"},
			{ID: "old", At: now.Add(-30 * time.Hour), Kind: autonomous.KindTaskLooping, PRDID: "p1"},
		},
	}
	s := eventLogServer(fake)

	code, resp := postPreview(t, s, `{"kinds":["task.looping"]}`)
	if code != http.StatusOK || resp.Count != 2 || len(resp.Sample) != 2 {
		t.Fatalf("lifecycle kind: code=%d count=%d sample=%d, want 200/2/2 (old event excluded)", code, resp.Count, len(resp.Sample))
	}
	if resp.LifecycleSince == nil {
		t.Errorf("lifecycle_since missing: a lifecycle count must say when recording began")
	}
	if resp.Sample[0].ID != "a" {
		t.Errorf("sample is not newest first: %+v", resp.Sample)
	}

	if code, resp = postPreview(t, s, `{"kinds":["task.looping"],"owner":"p1"}`); code != http.StatusOK || resp.Count != 1 {
		t.Errorf("owner filter: code=%d count=%d, want 200/1", code, resp.Count)
	}

	code, resp = postPreview(t, s, `{"kinds":["retry"]}`)
	if code != http.StatusOK || resp.LifecycleSince != nil {
		t.Errorf("log kind: code=%d lifecycle_since=%v, want 200 and none", code, resp.LifecycleSince)
	}

	code, resp = postPreview(t, s, `{"kinds":["retry","task.looping","burn_guard.rung_changed","automaton.stuck_running"]}`)
	if code != http.StatusOK || resp.Count != 4 {
		t.Errorf("mixed kinds: code=%d count=%d, want 200/4 (4 lifecycle, 0 log)", code, resp.Count)
	}

	if code, _ = postPreview(t, s, `{"kinds":["no.such.kind"]}`); code != http.StatusBadRequest {
		t.Errorf("unknown kind: code=%d, want 400", code)
	}
}

// A daemon whose manager cannot supply lifecycle history says so rather than
// reporting a false zero.
func TestEventPreviewLifecycleKindWithoutHistory(t *testing.T) {
	s := eventLogServer(&eventLogFake{})
	if code, _ := postPreview(t, s, `{"kinds":["task.looping"]}`); code != http.StatusServiceUnavailable {
		t.Errorf("code=%d, want 503", code)
	}
}
