// Proves the real autonomous API wrapper — the exact value
// cmd/datawatch passes to the server — passes the housekeeping gate: every
// one of the seven housekeeping routes is served (any non-503 status)
// instead of 503 "Housekeeping actions unavailable".

package server

import (
	"net/http"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

func TestRealAPIWrapperServesHousekeepingRoutes(t *testing.T) {
	m, err := autonomous.NewManager(t.TempDir(), autonomous.DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("autonomous.NewManager: %v", err)
	}
	api := autonomous.NewAPI(m)
	s := &Server{autonomousMgr: api} // the same wiring main.go does

	// With no base repository everything is off or empty: the read route
	// answers 200 with an empty list and the mutating actions answer
	// 2xx/4xx (for example ErrNoBaseRepo). None of them may answer 503 with
	// the gate body — that would mean the *API wrapper stopped satisfying
	// unlandedWorkOps.
	routes := []struct {
		name   string
		method string
		target string
		body   string
		serve  http.HandlerFunc
	}{
		{"unlanded-work", http.MethodGet, "/api/autonomous/unlanded-work", "", s.handleUnlandedWork},
		{"keep", http.MethodPost, "/api/autonomous/unlanded-work/keep", `{"branch":"automaton/x","note":"n"}`, s.handleUnlandedAction("keep")},
		{"archive-branch", http.MethodPost, "/api/autonomous/unlanded-work/archive-branch", `{"branch":"automaton/x"}`, s.handleUnlandedAction("archive")},
		{"delete-worktree", http.MethodPost, "/api/autonomous/unlanded-work/delete-worktree", `{"branch":"automaton/x","confirm":true}`, s.handleUnlandedAction("delete")},
		{"uncommitted-diff", http.MethodGet, "/api/autonomous/uncommitted-work/diff?worktree=/x", "", s.handleUncommittedDiff},
		{"uncommitted-commit", http.MethodPost, "/api/autonomous/uncommitted-work/commit", `{"worktree":"/x","message":"m"}`, s.handleUncommittedCommit},
		{"fix-permissions", http.MethodPost, "/api/autonomous/cleanup/fix-permissions", `{"paths":["/x"]}`, s.handleFixPermissions},
	}
	for _, rt := range routes {
		t.Run(rt.name, func(t *testing.T) {
			rec := hkDo(s, rt.serve, rt.method, rt.target, rt.body)
			if rec.Code == http.StatusServiceUnavailable {
				t.Fatalf("%s answered 503: %s", rt.name, rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), "Housekeeping actions unavailable") {
				t.Fatalf("%s: gate body leaked: %s", rt.name, rec.Body.String())
			}
		})
	}

	// The read route's off-state contract: 200 with an empty branch list.
	rec := hkDo(s, s.handleUnlandedWork, http.MethodGet, "/api/autonomous/unlanded-work", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"branches":[]`) {
		t.Fatalf("unlanded-work off state: got %d %s", rec.Code, rec.Body.String())
	}
}
