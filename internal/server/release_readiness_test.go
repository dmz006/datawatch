package server

// GET /api/autonomous/release-readiness: the release readiness of one size
// judged from the stored shadow-merge comparisons, its 400s for a missing
// or unknown size, its full-shape-with-zeros answer when nothing is stored,
// and its federation guard.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
)

func newReleaseReadinessServer(t *testing.T) (*Server, string) {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.DataDir = t.TempDir()
	return &Server{cfg: cfg, cfgPath: filepath.Join(t.TempDir(), "config.yaml"), dataDir: cfg.DataDir}, cfg.DataDir
}

// seedReleaseStore writes one stored shadow-merge comparison and, when
// planJSON is non-empty, its stored plan, in the layout the shadow-merge
// stores use.
func seedReleaseStore(t *testing.T, dataDir, id, comparison, plan string) {
	t.Helper()
	for _, dir := range []string{"merge-comparisons", "merge-plans"} {
		if err := os.MkdirAll(filepath.Join(dataDir, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if comparison != "" {
		if err := os.WriteFile(filepath.Join(dataDir, "merge-comparisons", id+".json"), []byte(comparison), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if plan != "" {
		if err := os.WriteFile(filepath.Join(dataDir, "merge-plans", id+".json"), []byte(plan), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func rrDo(t *testing.T, s *Server, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	s.handleReleaseReadiness(rec, httptest.NewRequest(http.MethodGet, target, strings.NewReader("")))
	return rec
}

func rrBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v: %s", err, rec.Body.String())
	}
	return body
}

func rrReadiness(t *testing.T, rec *httptest.ResponseRecorder) autonomous.ReleaseReadiness {
	t.Helper()
	body := rrBody(t, rec)
	raw, ok := body["readiness"].(map[string]any)
	if !ok {
		t.Fatalf("no readiness object in %s", rec.Body.String())
	}
	var res autonomous.ReleaseReadiness
	if err := json.Unmarshal([]byte(rawToString(t, raw)), &res); err != nil {
		t.Fatalf("decode readiness: %v", err)
	}
	return res
}

func rawToString(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

func TestReleaseReadinessRefusesMissingAndBadSize(t *testing.T) {
	s, _ := newReleaseReadinessServer(t)

	// No size at all: a JSON 400 naming the size it got.
	rec := rrDo(t, s, "/api/autonomous/release-readiness")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("no size: want 400, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type: want application/json, got %q", ct)
	}
	body := rrBody(t, rec)
	if body["code"] != "invalid_size" || !strings.Contains(body["error"].(string), "size") {
		t.Errorf("400 body: %v", body)
	}

	// Unknown size: a 400 naming the value it got.
	for _, bad := range []string{"mega", "Patch-", "  "} {
		rec := rrDo(t, s, "/api/autonomous/release-readiness?size="+strings.ReplaceAll(bad, " ", "+"))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("size %q: want 400, got %d", bad, rec.Code)
		}
		body := rrBody(t, rec)
		if body["code"] != "invalid_size" || !strings.Contains(body["error"].(string), bad) {
			t.Errorf("size %q 400 body: %v", bad, body)
		}
	}

	// A wrong method is refused with the allowed method.
	post := httptest.NewRecorder()
	s.handleReleaseReadiness(post, httptest.NewRequest(http.MethodPost, "/api/autonomous/release-readiness?size=patch", strings.NewReader("")))
	if post.Code != http.StatusMethodNotAllowed || post.Header().Get("Allow") != "GET" {
		t.Errorf("POST: want 405 with Allow GET, got %d %q", post.Code, post.Header().Get("Allow"))
	}
}

func TestReleaseReadinessEmptyStoreStillAnswersFull(t *testing.T) {
	s, _ := newReleaseReadinessServer(t)

	rec := rrDo(t, s, "/api/autonomous/release-readiness?size=patch")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rrBody(t, rec)
	res := rrReadiness(t, rec)

	// The full structure is there: every count present and zero, the bar the
	// default, not ready, and a sentence for each short condition.
	if res.Size != "patch" || res.Ready || res.Missing == nil {
		t.Fatalf("readiness: %+v", res)
	}
	if res.Records != 0 || res.Conflicts != 0 || res.PWAChanges != 0 || res.BlockingDefects != 0 {
		t.Errorf("counts should be zero: %+v", res)
	}
	if len(res.Missing) != 3 {
		t.Errorf("want a sentence for each short bar, got %v", res.Missing)
	}
	if res.Required != autonomous.DefaultReleaseReadinessBar() {
		t.Errorf("required bar: %+v", res.Required)
	}
	bar, ok := body["bar"].(map[string]any)
	if !ok || bar["records"].(float64) != 10 || bar["conflicts"].(float64) != 2 || bar["pwa_changes"].(float64) != 2 {
		t.Errorf("bar: %v", body["bar"])
	}
	if body["size"] != "patch" || body["project"] != "" {
		t.Errorf("echo: %v %v", body["size"], body["project"])
	}
}

// a comparison that held up, and its plan (project, conflicts, PWA signal).
const rrGoodComparison = `{"automaton_id":"a1","release_size":"Patch","matched":6,"different":0,"not_compared":0,"differences":[]}`

func TestReleaseReadinessCountsFromStoredComparisons(t *testing.T) {
	s, dataDir := newReleaseReadinessServer(t)

	// Ten patch comparisons for project demo, all holding up: a1 and a2 show
	// conflicts on their plan side, a3 and a4 touched the PWA.
	for i := 1; i <= 10; i++ {
		id := fmt.Sprintf("a%d", i)
		conflicts, parity := "[]", `false`
		switch i {
		case 1, 2:
			conflicts = `[{"path":"src/main.js","file_class":"source","resolution":"keep base","resolved_by":"planner"}]`
		case 3, 4:
			parity = `true`
		}
		seedReleaseStore(t, dataDir, id,
			strings.Replace(rrGoodComparison, `"a1"`, `"`+id+`"`, 1),
			`{"automaton_id":"`+id+`","project":"demo","parity_issue_needed":`+parity+`,"conflicts":`+conflicts+`}`,
		)
	}
	// One patch comparison whose plan did not hold up, with a major
	// difference: a blocking defect.
	seedReleaseStore(t, dataDir, "a11",
		`{"automaton_id":"a11","release_size":"patch","matched":4,"different":2,"not_compared":0,
		 "differences":[{"field":"version","planned":"v1.2.3","landed":"v1.2.2","severity":"major"},
		                 {"field":"readme","planned":"x","landed":"y","severity":"minor"}]}`,
		`{"automaton_id":"a11","project":"demo","parity_issue_needed":false,"conflicts":[]}`,
	)
	// One comparison for another project, and one for another size.
	seedReleaseStore(t, dataDir, "a12",
		`{"automaton_id":"a12","release_size":"patch","matched":6,"different":0,"not_compared":0,"differences":[]}`,
		`{"automaton_id":"a12","project":"other","parity_issue_needed":false,"conflicts":[]}`,
	)
	seedReleaseStore(t, dataDir, "b1",
		`{"automaton_id":"b1","release_size":"minor","matched":6,"different":0,"not_compared":0,"differences":[]}`,
		`{"automaton_id":"b1","project":"demo","parity_issue_needed":false,"conflicts":[]}`,
	)
	// A file that is not a comparison must be skipped, not fatal.
	if err := os.WriteFile(filepath.Join(dataDir, "merge-comparisons", "broken.json"), []byte("{oops"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Patch, no project filter: the ten demo records count (the twelfth is
	// project other, b1 is minor). The blocking one keeps the release not
	// ready.
	rec := rrDo(t, s, "/api/autonomous/release-readiness?size=patch")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	res := rrReadiness(t, rec)
	if res.Records != 11 || res.Conflicts != 2 || res.PWAChanges != 2 || res.BlockingDefects != 1 {
		t.Fatalf("counts: %+v", res)
	}
	if res.Ready {
		t.Errorf("a blocking defect must keep the release not ready")
	}
	found := false
	for _, m := range res.Missing {
		if strings.Contains(m, "blocking defect") {
			found = true
		}
	}
	if !found {
		t.Errorf("missing sentences should name the blocking defect: %v", res.Missing)
	}

	// Patch for project demo: the other project's record drops out.
	res = rrReadiness(t, rrDo(t, s, "/api/autonomous/release-readiness?size=patch&project=demo"))
	if res.Records != 10 || res.BlockingDefects != 1 {
		t.Errorf("project demo: %+v", res)
	}

	// Patch for project other: only its one record, short on every bar except
	// blocking defects, so exactly three sentences.
	res = rrReadiness(t, rrDo(t, s, "/api/autonomous/release-readiness?size=patch&project=other"))
	if res.Records != 1 || res.Ready || len(res.Missing) != 3 {
		t.Errorf("project other: %+v", res)
	}

	// Minor: only b1, and it is not ready.
	res = rrReadiness(t, rrDo(t, s, "/api/autonomous/release-readiness?size=minor"))
	if res.Records != 1 || res.Ready {
		t.Errorf("minor: %+v", res)
	}
}

func TestReleaseReadinessReadyWhenBarMet(t *testing.T) {
	s, dataDir := newReleaseReadinessServer(t)
	for i := 1; i <= 10; i++ {
		id := fmt.Sprintf("r%d", i)
		conflicts, parity := "[]", `false`
		switch i {
		case 1, 2:
			conflicts = `[{"path":"src/main.js","file_class":"source","resolution":"keep base","resolved_by":"planner"}]`
		case 3, 4:
			parity = `true`
		}
		seedReleaseStore(t, dataDir, id,
			strings.Replace(rrGoodComparison, `"a1"`, `"`+id+`"`, 1),
			`{"automaton_id":"`+id+`","project":"demo","parity_issue_needed":`+parity+`,"conflicts":`+conflicts+`}`,
		)
	}
	rec := rrDo(t, s, "/api/autonomous/release-readiness?size=patch")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	res := rrReadiness(t, rec)
	if !res.Ready || len(res.Missing) != 0 {
		t.Fatalf("want ready with no missing: %+v", res)
	}
	if res.Records != 10 || res.Conflicts != 2 || res.PWAChanges != 2 {
		t.Errorf("counts: %+v", res)
	}
}

func TestReleaseReadinessStoreErrors(t *testing.T) {
	s, dataDir := newReleaseReadinessServer(t)

	// A file where the comparison directory must be: unreadable store, 500.
	if err := os.WriteFile(filepath.Join(dataDir, "merge-comparisons"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if rec := rrDo(t, s, "/api/autonomous/release-readiness?size=patch"); rec.Code != http.StatusInternalServerError {
		t.Errorf("unreadable store: want 500, got %d: %s", rec.Code, rec.Body.String())
	}

	// No config at all: 503 before the stores are touched.
	if rec := rrDo(t, &Server{}, "/api/autonomous/release-readiness?size=patch"); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("no config: want 503, got %d", rec.Code)
	}
}

func TestReleaseReadinessFederationGuard(t *testing.T) {
	s, _ := newReleaseReadinessServer(t)
	target := "/api/autonomous/release-readiness?size=patch"

	call := func(caps ...string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, target, strings.NewReader(""))
		req = req.WithContext(context.WithValue(req.Context(), sessionCapsKey, caps))
		rec := httptest.NewRecorder()
		s.handleReleaseReadiness(rec, req)
		return rec
	}
	rec := call(federation.CapHealthRead)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), federation.CapAutonomousRead) {
		t.Errorf("without %s: want 403 naming it, got %d %q", federation.CapAutonomousRead, rec.Code, rec.Body.String())
	}
	if rec := call(federation.CapAutonomousRead); rec.Code != http.StatusOK {
		t.Errorf("with %s: want 200, got %d", federation.CapAutonomousRead, rec.Code)
	}
}
