package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// fakeSweepSessions is a TagSweepSessionStore over one session that carries
// a stale system-tag list, recording every write it receives. A session with
// no placement facts derives to an empty tag set under any settings, so the
// sweep always has work to do on it until it has written once.
type fakeSweepSessions struct {
	tags   []string
	writes map[string][]string
}

func (f *fakeSweepSessions) ListTagSweepSessions() []autonomous.TagSweepSession {
	return []autonomous.TagSweepSession{{ID: "s1", SystemTags: f.tags, Placement: autonomous.Placement{}}}
}

func (f *fakeSweepSessions) SetSystemTags(id string, tags []string) error {
	if f.writes == nil {
		f.writes = map[string][]string{}
	}
	f.writes[id] = tags
	return nil
}

func sweepCall(t *testing.T, s *Server, path string) (int, string, autonomous.SweepResult) {
	t.Helper()
	rec := tagCall(t, s, http.MethodPost, path, "")
	var res autonomous.SweepResult
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	return rec.Code, rec.Body.String(), res
}

func TestTagSweepEndpoint(t *testing.T) {
	s, mgr := newTagTestServer(t)
	fake := &fakeSweepSessions{tags: []string{"lane:gpu"}}
	mgr.SetTagSweepSessions(fake)

	// Dry run: reports the would-be change, writes nothing.
	code, body, res := sweepCall(t, s, "/api/autonomous/tags/sweep?dry_run=true")
	if code != http.StatusOK {
		t.Fatalf("dry_run: %d %s", code, body)
	}
	if res.Sessions.Examined != 1 || res.Sessions.Changed != 1 || res.Sessions.Unchanged != 0 || res.Sessions.Failed != 0 {
		t.Errorf("dry_run sessions = %+v, want examined/changed 1", res.Sessions)
	}
	if len(fake.writes) != 0 {
		t.Errorf("dry run wrote: %v", fake.writes)
	}
	// The stale tag list is still held — a dry run touches nothing.
	if len(fake.tags) != 1 {
		t.Errorf("dry run changed the held tags: %v", fake.tags)
	}

	// Real run, through the *autonomous.API that the production server's
	// SetAutonomousAPI hands this handler — the type assertion must succeed.
	code, body, res = sweepCall(t, s, "/api/autonomous/tags/sweep")
	if code != http.StatusOK {
		t.Fatalf("sweep: %d %s", code, body)
	}
	if res.Sessions.Examined != 1 || res.Sessions.Changed != 1 {
		t.Errorf("sweep sessions = %+v, want examined/changed 1", res.Sessions)
	}
	if _, ok := fake.writes["s1"]; !ok {
		t.Fatalf("sweep did not write session tags: %v", fake.writes)
	}

	// Second run: the sweep is a no-op — it examines but changes nothing.
	fake.tags = fake.writes["s1"] // the session now holds the derived set
	code, body, res = sweepCall(t, s, "/api/autonomous/tags/sweep")
	if code != http.StatusOK {
		t.Fatalf("second sweep: %d %s", code, body)
	}
	if res.Sessions.Unchanged != 1 || res.Sessions.Changed != 0 {
		t.Errorf("second sweep sessions = %+v, want unchanged 1 changed 0", res.Sessions)
	}
}

func TestTagSweepEndpointMethodNotAllowed(t *testing.T) {
	s, _ := newTagTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/autonomous/tags/sweep", nil)
	rec := httptest.NewRecorder()
	s.handleTagSweep(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET sweep: %d, want 405", rec.Code)
	}
}
