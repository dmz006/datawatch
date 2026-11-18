package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/planrecords"
)

func plansWriteMux(s *Server) *http.ServeMux {
	mux := plansReadMux(s)
	mux.HandleFunc("POST /api/plans", s.handlePlanCreate)
	mux.HandleFunc("PUT /api/plans/order", s.handlePlansOrder)
	mux.HandleFunc("PUT /api/plans/{ref}/automata/order", s.handlePlanAutomataOrder)
	mux.HandleFunc("POST /api/plans/{ref}/attach", s.handlePlanAttach)
	mux.HandleFunc("POST /api/plans/{ref}/detach", s.handlePlanDetach)
	return mux
}

func plansSend(t *testing.T, mux *http.ServeMux, method, path string, body any, caps ...string) (int, map[string]any) {
	t.Helper()
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rd)
	if len(caps) > 0 {
		req = asPeer(req, caps...)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func writeRig(t *testing.T) (*planRecordsRig, *http.ServeMux) {
	r := newPlanRecordsRig(t)
	known := map[string]planrecords.MigrateRef{
		"a-1":  {Kind: "automaton", Title: "One", State: "draft"},
		"a-2":  {Kind: "automaton", Title: "Two", State: "paused"},
		"a-3":  {Kind: "automaton", Title: "Three", State: "blocked"},
		"a-4":  {Kind: "automaton", Title: "Four", State: "draft"},
		"au-1": {Kind: "automation", Title: "Chain", State: "draft"},
	}
	r.s.planItemSource = func(id string) (planrecords.MigrateRef, bool) { v, ok := known[id]; return v, ok }
	return r, plansWriteMux(r.s)
}

func TestPlansWriteCreateAndReorderPlans(t *testing.T) {
	r, mux := writeRig(t)
	code, out := plansSend(t, mux, http.MethodPost, "/api/plans", map[string]any{"project": "datawatch", "title": "Alpha", "goal": "g"})
	if code != http.StatusCreated {
		t.Fatalf("create: %d %v", code, out)
	}
	alpha := out["plan"].(map[string]any)["id"].(string)
	_, out = plansSend(t, mux, http.MethodPost, "/api/plans", map[string]any{"project": "datawatch", "title": "Beta"})
	beta := out["plan"].(map[string]any)["id"].(string)

	if code, _ = plansSend(t, mux, http.MethodPost, "/api/plans", map[string]any{"project": "datawatch", "title": "alpha"}); code != http.StatusConflict {
		t.Fatalf("duplicate title: %d", code)
	}
	if code, _ = plansSend(t, mux, http.MethodPost, "/api/plans", map[string]any{"project": "nope", "title": "X"}); code != http.StatusBadRequest {
		t.Fatalf("unknown project: %d", code)
	}
	if code, _ = plansSend(t, mux, http.MethodPost, "/api/plans", map[string]any{"project": "datawatch"}); code != http.StatusBadRequest {
		t.Fatalf("no title: %d", code)
	}

	code, out = plansSend(t, mux, http.MethodPut, "/api/plans/order", map[string]any{"plans": []string{beta, alpha}})
	if code != 200 {
		t.Fatalf("reorder: %d %v", code, out)
	}
	rows := out["plans"].([]any)
	if rows[0].(map[string]any)["id"] != beta || rows[1].(map[string]any)["id"] != alpha {
		t.Fatalf("order = %v", rows)
	}
	if code, _ = plansSend(t, mux, http.MethodPut, "/api/plans/order", map[string]any{"plans": []string{beta, beta}}); code != http.StatusBadRequest {
		t.Fatalf("duplicate in order: %d", code)
	}
	if code, _ = plansSend(t, mux, http.MethodPut, "/api/plans/order", map[string]any{"plans": []string{"ghost"}}); code != http.StatusNotFound {
		t.Fatalf("unknown plan in order: %d", code)
	}
	if code, _ = plansSend(t, mux, http.MethodPut, "/api/plans/order", map[string]any{}); code != http.StatusBadRequest {
		t.Fatalf("empty order: %d", code)
	}
	_ = r
}

func TestPlansWriteAttachDetachAndAutomataOrder(t *testing.T) {
	r, mux := writeRig(t)
	p1 := mustPlan(t, r.store, "P one", 1, "")
	p2 := mustPlan(t, r.store, "P two", 2, "")
	for _, id := range []string{"a-1", "a-2", "a-3", "a-4"} {
		if code, out := plansSend(t, mux, http.MethodPost, "/api/plans/"+p1.ID+"/attach", map[string]any{"kind": "automaton", "id": id}); code != 200 {
			t.Fatalf("attach %s: %d %v", id, code, out)
		}
	}
	// Linked elsewhere: conflict, never overwritten.
	code, out := plansSend(t, mux, http.MethodPost, "/api/plans/"+p2.ID+"/attach", map[string]any{"kind": "automaton", "id": "a-1"})
	if code != http.StatusConflict || out["holder"] != p1.ID {
		t.Fatalf("conflict: %d %v", code, out)
	}
	if h, _ := r.store.PlanOf("a-1"); h.ID != p1.ID {
		t.Fatalf("a-1 moved to %s", h.ID)
	}
	if code, _ = plansSend(t, mux, http.MethodPost, "/api/plans/"+p1.ID+"/attach", map[string]any{"kind": "automaton", "id": "ghost"}); code != http.StatusNotFound {
		t.Fatalf("unknown automaton: %d", code)
	}
	if code, _ = plansSend(t, mux, http.MethodPost, "/api/plans/"+p1.ID+"/attach", map[string]any{"kind": "automaton", "id": "au-1"}); code != http.StatusNotFound {
		t.Fatalf("an Automation id is not an Automaton: %d", code)
	}
	if code, _ = plansSend(t, mux, http.MethodPost, "/api/plans/"+p1.ID+"/attach", map[string]any{"kind": "wat", "id": "a-1"}); code != http.StatusBadRequest {
		t.Fatalf("bad kind: %d", code)
	}
	if code, _ = plansSend(t, mux, http.MethodPost, "/api/plans/ghost/attach", map[string]any{"kind": "automaton", "id": "a-4"}); code != http.StatusNotFound {
		t.Fatalf("unknown plan: %d", code)
	}
	if code, out = plansSend(t, mux, http.MethodPost, "/api/plans/"+p1.ID+"/attach", map[string]any{"kind": "automation", "id": "au-1"}); code != 200 {
		t.Fatalf("attach automation: %d %v", code, out)
	}

	// Reorder: held (a-2) and blocked (a-3) are shown but never the next marker.
	code, out = plansSend(t, mux, http.MethodPut, "/api/plans/"+p1.ID+"/automata/order",
		map[string]any{"automata": []string{"a-3", "a-2", "a-4", "a-1"}})
	if code != 200 {
		t.Fatalf("automata order: %d %v", code, out)
	}
	if n := out["next"].(map[string]any)["automaton_id"]; n != "a-4" {
		t.Fatalf("next marker = %v, want a-4", n)
	}
	if code, _ = plansSend(t, mux, http.MethodPut, "/api/plans/"+p1.ID+"/automata/order", map[string]any{"automata": []string{"a-1"}}); code != http.StatusBadRequest {
		t.Fatalf("partial list: %d", code)
	}

	// Findings attach and detach.
	f, err := r.store.Create(planrecords.Record{Kind: planrecords.KindFinding, Project: "datawatch", Title: "Leaky"})
	if err != nil {
		t.Fatal(err)
	}
	if code, out = plansSend(t, mux, http.MethodPost, "/api/plans/"+p1.ID+"/attach", map[string]any{"kind": "finding", "id": f.ID}); code != 200 {
		t.Fatalf("attach finding: %d %v", code, out)
	}
	if code, _ = plansSend(t, mux, http.MethodPost, "/api/plans/"+p2.ID+"/attach", map[string]any{"kind": "finding", "id": f.ID}); code != http.StatusConflict {
		t.Fatalf("finding conflict: %d", code)
	}
	if code, _ = plansSend(t, mux, http.MethodPost, "/api/plans/"+p2.ID+"/detach", map[string]any{"kind": "finding", "id": f.ID}); code != http.StatusConflict {
		t.Fatalf("detach from the wrong plan: %d", code)
	}
	if code, _ = plansSend(t, mux, http.MethodPost, "/api/plans/"+p1.ID+"/detach", map[string]any{"kind": "finding", "id": f.ID}); code != 200 {
		t.Fatalf("detach finding: %d", code)
	}

	// Detach Automaton and Automation.
	if code, _ = plansSend(t, mux, http.MethodPost, "/api/plans/"+p2.ID+"/detach", map[string]any{"kind": "automaton", "id": "a-1"}); code != http.StatusConflict {
		t.Fatalf("automaton detach from wrong plan: %d", code)
	}
	if code, _ = plansSend(t, mux, http.MethodPost, "/api/plans/"+p1.ID+"/detach", map[string]any{"kind": "automaton", "id": "a-1"}); code != 200 {
		t.Fatalf("detach automaton: %d", code)
	}
	if code, _ = plansSend(t, mux, http.MethodPost, "/api/plans/"+p1.ID+"/detach", map[string]any{"kind": "automaton", "id": "a-1"}); code != http.StatusNotFound {
		t.Fatalf("detach again: %d", code)
	}
	if code, _ = plansSend(t, mux, http.MethodPost, "/api/plans/"+p1.ID+"/detach", map[string]any{"kind": "automation", "id": "au-1"}); code != 200 {
		t.Fatalf("detach automation: %d", code)
	}
}

func TestPlansWriteNeedsWriteCapability(t *testing.T) {
	r, mux := writeRig(t)
	p := mustPlan(t, r.store, "Guarded", 1, "")
	body := map[string]any{"kind": "automaton", "id": "a-1"}
	if code, _ := plansSend(t, mux, http.MethodPost, "/api/plans/"+p.ID+"/attach", body, federation.CapAutonomousRead); code != http.StatusForbidden {
		t.Fatalf("read-only peer attach: %d", code)
	}
	if code, _ := plansSend(t, mux, http.MethodPost, "/api/plans", map[string]any{"project": "datawatch", "title": "Z"}, federation.CapAutonomousRead); code != http.StatusForbidden {
		t.Fatalf("read-only peer create: %d", code)
	}
	if code, _ := plansSend(t, mux, http.MethodPut, "/api/plans/order", map[string]any{"plans": []string{p.ID}}, federation.CapAutonomousRead); code != http.StatusForbidden {
		t.Fatalf("read-only peer reorder: %d", code)
	}
	if code, _ := plansSend(t, mux, http.MethodPost, "/api/plans/"+p.ID+"/attach", body, federation.CapAutonomousWrite); code != 200 {
		t.Fatalf("write peer attach: %d", code)
	}
}

func TestPlansWriteOffAnswers404(t *testing.T) {
	r, mux := writeRig(t)
	off := false
	r.s.cfg.Autonomous.PlanRecords.Enabled = &off
	if code, _ := plansSend(t, mux, http.MethodPost, "/api/plans", map[string]any{"project": "datawatch", "title": "Q"}); code != http.StatusNotFound {
		t.Fatalf("off: %d", code)
	}
}
