package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/planrecords"
)

// rollupRig seeds three plans in the project "datawatch": one whose linked
// work is all finished (rolls up to done), one with a failed item (blocked)
// and one whose status was set by hand (kept). It returns their ids.
func rollupRig(t *testing.T) (*planRecordsRig, *http.ServeMux, map[string]string) {
	t.Helper()
	r, mux := writeRig(t)
	ids := map[string]string{}
	for i, c := range []struct{ key, state string }{{"done", "completed"}, {"blocked", "failed"}, {"hand", "completed"}} {
		p := mustPlan(t, r.store, "Plan "+c.key, i+1, "")
		ids[c.key] = p.ID
		attach(t, r.store, p.ID, "a-"+c.key, planrecords.LinkInfo{Title: "Item " + c.key, State: c.state})
	}
	st := planrecords.StateCancelled
	if _, err := r.store.Update(ids["hand"], planrecords.Patch{State: &st}); err != nil {
		t.Fatal(err)
	}
	return r, mux, ids
}

func planStateOf(t *testing.T, r *planRecordsRig, id string) string {
	t.Helper()
	rec, err := r.store.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	return string(rec.State)
}

func changeFor(t *testing.T, out map[string]any, plan string) map[string]any {
	t.Helper()
	for _, c := range out["changes"].([]any) {
		m := c.(map[string]any)
		if m["plan"] == plan {
			return m
		}
	}
	t.Fatalf("no change for plan %s in %v", plan, out["changes"])
	return nil
}

func TestPlansRollupGetPreviewsAndWritesNothing(t *testing.T) {
	r, mux, ids := rollupRig(t)
	before := map[string]string{}
	for k, id := range ids {
		before[k] = planStateOf(t, r, id)
	}
	code, out := plansGet(t, mux, "/api/plans/rollup?project=datawatch&dry_run=true")
	if code != 200 {
		t.Fatalf("rollup preview: %d %v", code, out)
	}
	if out["dry_run"] != true || out["applied"] != false || out["project"] != "datawatch" {
		t.Fatalf("preview flags = %v", out)
	}
	if out["changed"].(float64) != 2 || out["kept"].(float64) != 1 {
		t.Fatalf("counts = changed %v kept %v, want 2 and 1", out["changed"], out["kept"])
	}
	done := changeFor(t, out, ids["done"])
	if done["to"] != "done" || done["title"] != "Plan done" || done["reason"] == "" {
		t.Fatalf("done change = %v", done)
	}
	if got := changeFor(t, out, ids["blocked"]); got["to"] != "blocked" {
		t.Fatalf("blocked change = %v", got)
	}
	hand := changeFor(t, out, ids["hand"])
	if hand["set_by_hand_kept"] != true || hand["to"] != "done" || hand["from"] != "cancelled" {
		t.Fatalf("hand change = %v", hand)
	}
	for k, id := range ids {
		if got := planStateOf(t, r, id); got != before[k] {
			t.Errorf("preview changed plan %s: %s -> %s", k, before[k], got)
		}
	}
	// With no dry_run at all a GET is still only a preview.
	if code, out = plansGet(t, mux, "/api/plans/rollup"); code != 200 || out["dry_run"] != true {
		t.Fatalf("bare GET: %d %v", code, out)
	}
	if got := planStateOf(t, r, ids["done"]); got != before["done"] {
		t.Fatalf("bare GET changed a plan: %s", got)
	}
}

func TestPlansRollupGetRefusesToApply(t *testing.T) {
	r, mux, ids := rollupRig(t)
	if code, _ := plansGet(t, mux, "/api/plans/rollup?dry_run=false"); code != http.StatusBadRequest {
		t.Fatalf("GET dry_run=false: %d", code)
	}
	if code, _ := plansGet(t, mux, "/api/plans/rollup?dry_run=maybe"); code != http.StatusBadRequest {
		t.Fatalf("GET dry_run=maybe: %d", code)
	}
	if got := planStateOf(t, r, ids["done"]); got == "done" {
		t.Fatal("a refused GET still rolled the plan up")
	}
}

func TestPlansRollupPostAppliesAndKeepsHandSetStatus(t *testing.T) {
	r, mux, ids := rollupRig(t)
	code, out := plansSend(t, mux, http.MethodPost, "/api/plans/rollup", map[string]any{"project": "datawatch"})
	if code != 200 {
		t.Fatalf("rollup apply: %d %v", code, out)
	}
	if out["applied"] != true || out["dry_run"] != false || out["changed"].(float64) != 2 || out["kept"].(float64) != 1 {
		t.Fatalf("apply answer = %v", out)
	}
	if got := planStateOf(t, r, ids["done"]); got != "done" {
		t.Errorf("done plan = %s", got)
	}
	if got := planStateOf(t, r, ids["blocked"]); got != "blocked" {
		t.Errorf("blocked plan = %s", got)
	}
	if got := planStateOf(t, r, ids["hand"]); got != "cancelled" {
		t.Errorf("hand-set plan changed to %s", got)
	}
	// A second run finds only the hand-set plan.
	_, out = plansSend(t, mux, http.MethodPost, "/api/plans/rollup", nil)
	if out["changed"].(float64) != 0 || out["kept"].(float64) != 1 {
		t.Fatalf("second apply = %v", out)
	}
}

func TestPlansRollupPostDryRunBodyAndQuery(t *testing.T) {
	r, mux, ids := rollupRig(t)
	for _, c := range []struct {
		path string
		body any
	}{
		{"/api/plans/rollup", map[string]any{"dry_run": true}},
		{"/api/plans/rollup?dry_run=true", nil},
	} {
		code, out := plansSend(t, mux, http.MethodPost, c.path, c.body)
		if code != 200 || out["dry_run"] != true || out["applied"] != false {
			t.Fatalf("%s: %d %v", c.path, code, out)
		}
		if got := planStateOf(t, r, ids["done"]); got == "done" {
			t.Fatalf("%s wrote the plan", c.path)
		}
	}
}

func TestPlansRollupProjectIsChecked(t *testing.T) {
	_, mux, _ := rollupRig(t)
	if code, _ := plansGet(t, mux, "/api/plans/rollup?project=nope"); code != http.StatusBadRequest {
		t.Fatalf("unknown project on GET: %d", code)
	}
	if code, _ := plansSend(t, mux, http.MethodPost, "/api/plans/rollup", map[string]any{"project": "nope"}); code != http.StatusBadRequest {
		t.Fatalf("unknown project on POST: %d", code)
	}
	// Another registered project with no plans: nothing to change.
	code, out := plansGet(t, mux, "/api/plans/rollup?project=other")
	if code != 200 || out["changed"].(float64) != 0 || len(out["changes"].([]any)) != 0 {
		t.Fatalf("other project: %d %v", code, out)
	}
}

func TestPlansRollupBadBody(t *testing.T) {
	_, mux, _ := rollupRig(t)
	if code, _ := plansSend(t, mux, http.MethodPost, "/api/plans/rollup", map[string]any{"bogus": 1}); code != http.StatusBadRequest {
		t.Fatalf("unknown body field: %d", code)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/plans/rollup", strings.NewReader("{not json"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad JSON: %d", rec.Code)
	}
}

func TestPlansRollupCapabilitiesAndOff(t *testing.T) {
	r, mux, ids := rollupRig(t)
	if code, _ := plansGet(t, mux, "/api/plans/rollup"); code != 200 {
		t.Fatalf("no peer: %d", code)
	}
	req := asPeer(httptest.NewRequest(http.MethodGet, "/api/plans/rollup", nil), federation.CapAutonomousRead)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("read peer preview: %d", rec.Code)
	}
	if code, _ := plansSend(t, mux, http.MethodPost, "/api/plans/rollup", nil, federation.CapAutonomousRead); code != http.StatusForbidden {
		t.Fatalf("read-only peer apply: %d", code)
	}
	if got := planStateOf(t, r, ids["done"]); got == "done" {
		t.Fatal("a refused apply wrote the plan")
	}
	if code, _ := plansSend(t, mux, http.MethodPost, "/api/plans/rollup", nil, federation.CapAutonomousWrite); code != 200 {
		t.Fatalf("write peer apply: %d", code)
	}
	off := false
	r.s.cfg.Autonomous.PlanRecords.Enabled = &off
	if code, _ := plansGet(t, mux, "/api/plans/rollup"); code != http.StatusNotFound {
		t.Fatalf("off GET: %d", code)
	}
	if code, _ := plansSend(t, mux, http.MethodPost, "/api/plans/rollup", nil); code != http.StatusNotFound {
		t.Fatalf("off POST: %d", code)
	}
}

func TestPlansRollupClosesAFinishedPlan(t *testing.T) {
	r, mux, ids := rollupRig(t)
	if code, _ := plansSend(t, mux, http.MethodPost, "/api/plans/rollup", nil); code != 200 {
		t.Fatalf("apply: %d", code)
	}
	rec, err := r.store.Get(ids["done"])
	if err != nil || rec.State != planrecords.StateDone || rec.ClosedAt == "" {
		t.Fatalf("done plan = %+v %v", rec, err)
	}
}

// The plan list and the plan detail carry the status and whether it was set
// by hand, so a client can mark a hand-set status.
func TestPlansReadShowsSetByHand(t *testing.T) {
	r, mux, ids := rollupRig(t)
	code, body := plansGet(t, mux, "/api/projects/datawatch/plans?limit=50")
	if code != 200 {
		t.Fatalf("plans: %d", code)
	}
	byID := map[string]map[string]any{}
	for _, p := range body["plans"].([]any) {
		m := p.(map[string]any)
		byID[m["id"].(string)] = m
	}
	if byID[ids["hand"]]["set_by_hand"] != true || byID[ids["hand"]]["state"] != "cancelled" {
		t.Fatalf("hand row = %v", byID[ids["hand"]])
	}
	if v, ok := byID[ids["done"]]["set_by_hand"]; !ok || v != false {
		t.Fatalf("set_by_hand must be present and false on a derived plan: %v", byID[ids["done"]])
	}
	// Detail: the plan header of the Automata and tasks routes.
	_, det := plansGet(t, mux, "/api/plans/"+ids["hand"]+"/automata")
	if det["plan"].(map[string]any)["set_by_hand"] != true {
		t.Fatalf("automata plan header = %v", det["plan"])
	}
	_, det = plansGet(t, mux, "/api/plans/"+ids["hand"]+"/tasks")
	if det["plan"].(map[string]any)["set_by_hand"] != true {
		t.Fatalf("tasks plan header = %v", det["plan"])
	}
	// After a roll-up the derived plan still reads set_by_hand false.
	plansSend(t, mux, http.MethodPost, "/api/plans/rollup", nil)
	_, body = plansGet(t, mux, "/api/projects/datawatch/plans?limit=50")
	for _, p := range body["plans"].([]any) {
		m := p.(map[string]any)
		if m["id"] == ids["done"] && (m["state"] != "done" || m["set_by_hand"] != false) {
			t.Fatalf("rolled-up row = %v", m)
		}
	}
	_ = r
}
