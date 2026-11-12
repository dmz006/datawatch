// Performance budget guard for the web UI load path
// (docs/plans/2026-10-23-web-ui-load-performance.md).
//
// Each list endpoint the UI polls on load is requested in its default first
// page form against the seeded data set (perf_seed_test.go). The uncompressed
// body must stay under perfBudgetBytes and a repeat request carrying the
// ETag in If-None-Match must answer 304 with an empty body. The explicit
// full form must still return complete objects.
//
// PERF_REPORT_OUT=<path> writes the measured rows as JSON, as the
// TestEndpointMeasure* tests do.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// perfBudgetBytes is the largest uncompressed first-page body allowed: 256 KB.
const perfBudgetBytes = 262144

type perfBudgetEndpoint struct {
	name    string
	path    string
	handler func(*Server) func(http.ResponseWriter, *http.Request)
}

// perfBudgetEndpoints are the lists the web UI fetches on load, first page.
var perfBudgetEndpoints = []perfBudgetEndpoint{
	{"sessions", "/api/sessions?view=summary&limit=50", func(s *Server) func(http.ResponseWriter, *http.Request) { return s.handleSessions }},
	{"automata", "/api/autonomous/prds", func(s *Server) func(http.ResponseWriter, *http.Request) { return s.handleAutonomousPRDs }},
	{"automations", "/api/automations", func(s *Server) func(http.ResponseWriter, *http.Request) { return s.handleAutomations }},
	{"alerts", "/api/alerts?limit=50", func(s *Server) func(http.ResponseWriter, *http.Request) { return s.handleAlerts }},
}

func perfGetWith(h func(http.ResponseWriter, *http.Request), path, etag string) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	h(rr, req)
	return rr
}

func TestPerfBudgetFirstPage(t *testing.T) {
	seed := SeedPerfServer(t, t.TempDir())
	var rows []PerfRow
	for _, ep := range perfBudgetEndpoints {
		h := ep.handler(seed.Server)
		rr := perfGetWith(h, ep.path, "")
		if rr.Code != http.StatusOK {
			t.Errorf("%s %s: status %d, want 200", ep.name, ep.path, rr.Code)
			continue
		}
		body := rr.Body.Bytes()
		rows = append(rows, PerfRow{Name: ep.name, Path: ep.path, Status: rr.Code, RawBytes: len(body), GzipBytes: perfGzipLen(body)})
		rows[len(rows)-1].GzipRatioPc = 100 * float64(rows[len(rows)-1].GzipBytes) / float64(len(body))
		if len(body) > perfBudgetBytes {
			t.Errorf("%s %s: body %d bytes exceeds budget %d", ep.name, ep.path, len(body), perfBudgetBytes)
		}
	}
	perfLogTable(t, rows)
	perfWriteReport(t, seed, rows)
}

func TestPerfBudgetConditionalRepeat(t *testing.T) {
	seed := SeedPerfServer(t, t.TempDir())
	for _, ep := range perfBudgetEndpoints {
		h := ep.handler(seed.Server)
		first := perfGetWith(h, ep.path, "")
		etag := first.Header().Get("ETag")
		if etag == "" {
			t.Errorf("%s %s: no ETag header", ep.name, ep.path)
			continue
		}
		again := perfGetWith(h, ep.path, etag)
		if again.Code != http.StatusNotModified {
			t.Errorf("%s %s: repeat with If-None-Match got %d, want 304", ep.name, ep.path, again.Code)
		}
		if again.Body.Len() != 0 {
			t.Errorf("%s %s: 304 carried a %d byte body", ep.name, ep.path, again.Body.Len())
		}
	}
}

func TestPerfBudgetFullFormStillComplete(t *testing.T) {
	seed := SeedPerfServer(t, t.TempDir())

	// Automata: ?full=1 returns every Automaton with its stories and tasks.
	rr := perfGetWith(seed.Server.handleAutonomousPRDs, "/api/autonomous/prds?full=1", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("automata full: status %d", rr.Code)
	}
	var prds struct {
		PRDs []struct {
			ID    string            `json:"id"`
			Story []json.RawMessage `json:"stories"`
		} `json:"prds"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &prds); err != nil {
		t.Fatalf("automata full: %v", err)
	}
	if len(prds.PRDs) != perfSeedAutomata {
		t.Fatalf("automata full: %d objects, want %d", len(prds.PRDs), perfSeedAutomata)
	}
	withStories := 0
	for _, p := range prds.PRDs {
		if len(p.Story) > 0 {
			withStories++
		}
	}
	if withStories == 0 {
		t.Error("automata full: no object carries stories")
	}
	if rr.Body.Len() < 1<<20 {
		t.Errorf("automata full: body only %d bytes, expected the complete objects", rr.Body.Len())
	}

	// Sessions: the plain list keeps the complete records, task text included.
	rr = perfGetWith(seed.Server.handleSessions, "/api/sessions", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("sessions full: status %d", rr.Code)
	}
	var sess []map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &sess); err != nil {
		t.Fatalf("sessions full: %v", err)
	}
	if len(sess) != perfSeedSessions {
		t.Fatalf("sessions full: %d objects, want %d", len(sess), perfSeedSessions)
	}
	big := 0
	for _, s := range sess {
		if task, _ := s["task"].(string); len(task) >= perfSeedBigTaskBytes {
			big++
		}
	}
	if big != seed.BigTasks {
		t.Errorf("sessions full: %d records keep their %d byte task, want %d", big, perfSeedBigTaskBytes, seed.BigTasks)
	}
}
