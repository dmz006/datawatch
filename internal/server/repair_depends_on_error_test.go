// Repair depends_on: the REST / MCP error surface must show the exact
// bad-entry text the store reports (unresolvable entry or dependency
// cycle), not a generic "something went wrong".
//
// Why this package: internal/autonomous/api.go (~line 510) has
// API.RepairDependsOn as a thin delegate — it calls
// a.M.RepairDependsOn, only emits PRD updates when err == nil, and
// returns the error unchanged. Note for the reader: API embeds a
// concrete *Manager (no interface), so a stub-based test of that
// delegate in the autonomous package isn't practical; the seam the
// server package already exercises (the AutonomousAPI interface,
// server/api.go:596 — the same one the production daemon implements
// via the autonomous package) is the right place to assert the REST
// error propagates verbatim, which is what this test does.
//
// The MCP side (internal/mcp/autonomous.go handleAutonomousPRDRepairDependsOn)
// proxies to exactly this endpoint and surfaces non-2xx bodies
// untruncated via proxyJSON ("HTTP <status>: <full body>",
// internal/mcp/sx_parity.go), so a verbatim REST body means a verbatim
// MCP error.

package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
)

// fakeRepairError embeds the shared interface fake from
// orchestrator_enrich_test.go and overrides only RepairDependsOn to
// fail the way Store.RepairDependsOn does when the plan is bad.
type fakeRepairError struct {
	fakeOrchAutonomous
	repairErr error
}

func (f *fakeRepairError) RepairDependsOn(string, string) (any, error) {
	return nil, f.repairErr
}

func newRepairErrorServer(t *testing.T, repairErr error) *Server {
	t.Helper()
	dir := t.TempDir()
	sm, err := session.NewManager("h", dir, "echo", 30*time.Second)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	// In-memory tmux fake: these tests never need a live multiplexer.
	sm.WithFakeTmux()
	t.Cleanup(sm.WaitBackground)
	srv := NewServer(NewHub(), sm, "h", "", nil, &config.Config{}, filepath.Join(dir, "config.yaml"))
	srv.SetAutonomousAPI(&fakeRepairError{repairErr: repairErr})
	return srv
}

// TestRepairDependsOn_ErrorSurfacesExactBadEntry posts to
// /api/autonomous/prds/{id}/repair_depends_on with each store-style
// failure and asserts the HTTP client gets non-2xx plus the exact
// error text — both the "names the exact bad entry" form and the
// "names the cycle" form.
func TestRepairDependsOn_ErrorSurfacesExactBadEntry(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{
			name: "unresolvable entry names the exact raw entry",
			err:  fmt.Errorf("unresolvable depends_on entry %q on %s", "s9-t9", "story \"S2\""),
		},
		{
			name: "cycle error names the cycle",
			err:  fmt.Errorf("depends_on cycle: %s -> %s -> %s", "task-a", "task-b", "task-a"),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := newRepairErrorServer(t, tc.err)
			req := httptest.NewRequest(http.MethodPost,
				"/api/autonomous/prds/PRD-123/repair_depends_on",
				strings.NewReader(`{"actor":"operator"}`))
			w := httptest.NewRecorder()
			// Handle the route through the same dispatcher path the
			// production handler uses for this action.
			srv.handleAutonomousPRDs(w, req)

			if w.Code == http.StatusOK {
				t.Fatalf("expected an error response, got 200 with success semantics")
			}
			if w.Code/100 != 4 && w.Code/100 != 5 {
				t.Fatalf("status = %d, want a 4xx/5xx error status", w.Code)
			}
			body := w.Body.String()
			if !strings.Contains(body, tc.err.Error()) {
				t.Fatalf("error body %q does not contain the exact store error %q — the bad entry text is being swallowed or rewritten",
					body, tc.err.Error())
			}
			if tc.name == "unresolvable entry names the exact raw entry" && !strings.Contains(body, "s9-t9") {
				t.Fatal("body does not name the bad entry 's9-t9'")
			}
		})
	}
}
