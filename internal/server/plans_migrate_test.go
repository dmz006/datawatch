// plans_migrate_test.go — POST /api/plans/migrate over the real store.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
)

func migrateCall(t *testing.T, r *planRecordsRig, method string, body any, caps ...string) *httptest.ResponseRecorder {
	t.Helper()
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, plansMigratePath, rd)
	if len(caps) > 0 {
		req = asPeer(req, caps...)
	}
	rec := httptest.NewRecorder()
	r.s.handlePlansMigrate(rec, req)
	return rec
}

func migrateCounts(t *testing.T, rec *httptest.ResponseRecorder) map[string]float64 {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d %s", rec.Code, rec.Body.String())
	}
	out := map[string]float64{}
	for k, v := range prMap(t, rec)["counts"].(map[string]any) {
		out[k] = v.(float64)
	}
	return out
}

func TestPlansMigrateDryRunApplyAndIdempotent(t *testing.T) {
	r := newPlanRecordsRig(t)
	r.s.cfg.Projects["datawatch"] = config.ProjectConfigEntry{
		Dir:  r.dir,
		Arcs: []config.ProjectArc{{AutomationID: "aaaa1111", Priority: 1, State: "active"}},
	}
	r.create(t, "registry-rows", map[string]any{"title": "Backlog item", "legacy_number": 7})

	// Default (no body) is a dry run.
	c := migrateCounts(t, migrateCall(t, r, http.MethodPost, nil))
	if c["would_create"] != 2 || r.store.Len() != 1 {
		t.Fatalf("dry run: counts %v, store len %d", c, r.store.Len())
	}
	if len(prAuditActions(t, r.audit)) != 1 { // only the row create
		t.Fatalf("a dry run must not audit: %v", prAuditActions(t, r.audit))
	}

	c = migrateCounts(t, migrateCall(t, r, http.MethodPost, map[string]any{"apply": true}))
	if c["would_create"] != 2 || c["would_link"] != 1 || r.store.Len() != 3 {
		t.Fatalf("apply: counts %v, store len %d", c, r.store.Len())
	}
	imported := 0
	for _, e := range prAuditActions(t, r.audit) {
		if e.Action == "plan_record_imported" && e.Subject == "plans migrate" {
			imported++
		}
	}
	if imported != 1 {
		t.Fatalf("apply must write one migrate audit entry, got %d", imported)
	}

	c = migrateCounts(t, migrateCall(t, r, http.MethodPost, map[string]any{"apply": true}))
	if c["would_create"] != 0 || c["would_link"] != 0 || r.store.Len() != 3 {
		t.Fatalf("second apply changed things: %v, len %d", c, r.store.Len())
	}
	if len(r.s.cfg.Projects["datawatch"].Arcs) != 1 {
		t.Fatal("the arcs must stay in the config")
	}
}

func TestPlansMigrateGuards(t *testing.T) {
	r := newPlanRecordsRig(t)
	if rec := migrateCall(t, r, http.MethodGet, nil); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET: want 405, got %d", rec.Code)
	}
	if rec := migrateCall(t, r, http.MethodPost, map[string]any{"apply": "yes"}); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad body: want 400, got %d", rec.Code)
	}
	if rec := migrateCall(t, r, http.MethodPost, map[string]any{"apply": true}, federation.CapAutonomousRead); rec.Code != http.StatusForbidden {
		t.Fatalf("read-only peer: want 403, got %d", rec.Code)
	}
	r.s.planRecords = nil
	if rec := migrateCall(t, r, http.MethodPost, nil); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("no store: want 503, got %d", rec.Code)
	}
	off := false
	r.s.cfg.Autonomous.PlanRecords.Enabled = &off
	if rec := migrateCall(t, r, http.MethodPost, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("turned off: want 404, got %d", rec.Code)
	}
}
