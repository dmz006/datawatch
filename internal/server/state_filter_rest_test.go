package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestStateFilterREST exercises the shared comma-list state filter on the
// REST state surfaces: registry rows, findings, search and browse. A single
// state keeps the old, exact meaning; a comma list trims each part, drops
// empties, keeps each state once and refuses an unknown state with a message
// that names the valid ones.
func TestStateFilterREST(t *testing.T) {
	r := newPlanRecordsRig(t)
	r.create(t, "registry-rows", map[string]any{"title": "Open row", "state": "open"})
	r.create(t, "registry-rows", map[string]any{"title": "Blocked row", "state": "blocked"})
	r.create(t, "registry-rows", map[string]any{"title": "Shipped row", "state": "shipped"})
	r.create(t, "findings", map[string]any{"title": "Open finding"})

	get := func(path string) (*httptest.ResponseRecorder, map[string]any) {
		rec := prCall(t, r.s, http.MethodGet, path, nil)
		return rec, prMap(t, rec)
	}
	total := func(t *testing.T, m map[string]any, path string) int {
		t.Helper()
		if n, ok := m["total"].(float64); ok {
			return int(n)
		}
		if pg, ok := m["page"].(map[string]any); ok {
			if n, ok := pg["total"].(float64); ok {
				return int(n)
			}
		}
		t.Fatalf("%s: no total in %v", path, m)
		return 0
	}
	// wantStatus runs path, checks the code and the record count.
	wantStatus := func(path string, code int, wantTotal int) {
		rec, m := get(path)
		if rec.Code != code {
			t.Fatalf("%s: status=%d body=%s want %d", path, rec.Code, rec.Body.String(), code)
		}
		if code == http.StatusOK {
			if n := total(t, m, path); n != wantTotal {
				t.Fatalf("%s: total=%d want %d (%s)", path, n, wantTotal, rec.Body.String())
			}
		}
	}

	// No list at all: omitted or empty state is no filter, as before.
	wantStatus("/api/plan-records/registry-rows", 200, 3)
	wantStatus("/api/plan-records/findings?state=", 200, 1)
	wantStatus("/api/plan-records/search?q=row&state=", 200, 3)
	wantStatus("/api/plan-records/browse?state=", 200, 4)
	wantStatus("/api/plan-records/browse?kind=registry_row&state=", 200, 3)

	// One state: exact, as the route had it - a padded single state is not
	// trimmed and still 400s, exactly as it did before a list existed.
	wantStatus("/api/plan-records/registry-rows?state=open", 200, 1)
	wantStatus("/api/plan-records/registry-rows?state=%20open", 400, 0)
	wantStatus("/api/plan-records/findings?state=open", 200, 1)
	wantStatus("/api/plan-records/search?q=row&state=open", 200, 1)
	wantStatus("/api/plan-records/browse?kind=registry_row&state=open", 200, 1)
	wantStatus("/api/plan-records/registry-rows?state=%20", 400, 0)

	// Several states: every listed state in, all others out; a list trims,
	// drops empties and collapses repeats.
	wantStatus("/api/plan-records/registry-rows?state=open,blocked", 200, 2)
	wantStatus("/api/plan-records/registry-rows?state=open,%20blocked", 200, 2)
	wantStatus("/api/plan-records/registry-rows?state=open,%20%20open,blocked", 200, 2)
	wantStatus("/api/plan-records/registry-rows?state=open,shipped", 200, 2)
	wantStatus("/api/plan-records/findings?state=open,open", 200, 1)
	wantStatus("/api/plan-records/search?q=row&state=open,blocked", 200, 2)
	wantStatus("/api/plan-records/search?q=row&state=open,shipped", 200, 2)

	// Browse without a kind admits a state of either vocabulary (union),
	// single or listed, and a state neither knows still 400s.
	wantStatus("/api/plan-records/browse?state=open,shipped", 200, 3)
	wantStatus("/api/plan-records/browse?state=open", 200, 2) // both kinds at once
	wantStatus("/api/plan-records/browse?state=shipped", 200, 1)
	wantStatus("/api/plan-records/browse?state=triaged", 200, 0) // finding-only word still admitted
	wantStatus("/api/plan-records/browse?kind=finding&state=open", 200, 1)
	wantStatus("/api/plan-records/browse?state=in_progress", 200, 0)
	wantStatus("/api/plan-records/browse?kind=registry_row&state=planned", 200, 0)

	// Unknown states are 400s that name the parameter and the valid states.
	refusals := []struct {
		path string
		want []string
	}{
		{"/api/plan-records/registry-rows?state=plumbing", []string{"open", "planned", "shipped"}},
		{"/api/plan-records/findings?state=shipped", []string{"open", "triaged", "wontfix"}},
		{"/api/plan-records/findings?state=open,shipped", []string{"open", "triaged"}},
		{"/api/plan-records/search?q=row&state=open,plumbing", []string{"open", "fixed"}},
		{"/api/plan-records/browse?kind=registry_row&state=triaged", []string{"open", "cancelled"}},
		{"/api/plan-records/browse?kind=finding&state=blocked", []string{"duplicate"}},
		{"/api/plan-records/browse?state=bogus", []string{"open", "fixed", "shipped"}},
		{"/api/plan-records/registry-rows?state=%2C", []string{"open"}},
		{"/api/plan-records/browse?state=%2C", []string{"open"}},
	}
	for _, tc := range refusals {
		rec := prCall(t, r.s, http.MethodGet, tc.path, nil)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: status=%d body=%s want 400", tc.path, rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		if !strings.Contains(body, `"state"`) && !strings.Contains(body, "state") {
			t.Fatalf("%s: error does not name the parameter: %s", tc.path, body)
		}
		for _, word := range tc.want {
			if !strings.Contains(body, word) {
				t.Fatalf("%s: error omits valid state %q: %s", tc.path, word, body)
			}
		}
	}
}
