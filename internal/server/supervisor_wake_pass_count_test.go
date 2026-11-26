// The wake status counts two things apart: event wakes whose line was typed,
// and supervisor passes that event wakes ran.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// wakeCounts reads the counts as GET /api/supervisor-loop/wake serves them.
func wakeCounts(t *testing.T, r *wakeRig) (events, passes float64, lastEvent string) {
	t.Helper()
	s, _ := newLoopServer(t)
	s.SetSupervisorWake(r.record)
	rr := httptest.NewRecorder()
	s.handleSupervisorLoopWake(rr, httptest.NewRequest(http.MethodGet, "/api/supervisor-loop/wake", nil))
	var v map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	events, okEvents := v["event_count"].(float64)
	passes, okPasses := v["pass_count"].(float64)
	if !okEvents || !okPasses {
		t.Fatalf("wake status lacks event_count or pass_count: %s", rr.Body.String())
	}
	lastEvent, _ = v["last_event"].(string)
	return events, passes, lastEvent
}

func TestSupervisorWakeCountsTypedLinesAndPassesApart(t *testing.T) {
	r := newWakeRig(t, wakeOn(autonomous.KindAutomatonStuckRunning))

	if e, p, _ := wakeCounts(t, r); e != 0 || p != 0 {
		t.Fatalf("before any event: events=%v passes=%v, want 0 and 0", e, p)
	}
	// No supervisor session: the pass runs and nothing is typed.
	r.src.emit(wakeEvent)
	r.ctl.Wait()
	e, p, last := wakeCounts(t, r)
	if r.runs.Load() != 1 || r.sent.Load() != 0 || e != 0 || p != 1 {
		t.Fatalf("no session: runs=%d sent=%d events=%v passes=%v, want 1, 0, 0, 1", r.runs.Load(), r.sent.Load(), e, p)
	}
	if last != "0001-01-01T00:00:00Z" {
		t.Fatalf("last_event = %q with nothing typed, want never", last)
	}
	// With a session, the next wake types one line, the waiting one included.
	r.session = "sup-1"
	r.src.emit(autonomous.LifecycleEvent{ID: "e2", Kind: autonomous.KindAutomatonStuckRunning, PRDID: "p2"})
	r.ctl.Wait()
	e, p, last = wakeCounts(t, r)
	if r.sent.Load() != 1 || e != 1 || p != 2 {
		t.Fatalf("typed: sent=%d events=%v passes=%v, want 1, 1, 2", r.sent.Load(), e, p)
	}
	if last == "0001-01-01T00:00:00Z" {
		t.Fatal("last_event not set by a typed line")
	}
}
