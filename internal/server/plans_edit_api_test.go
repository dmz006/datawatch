package server

import (
	"net/http"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/planrecords"
)

func TestPlansEditRenameAndGoal(t *testing.T) {
	r, mux := writeRig(t)
	p := mustPlan(t, r.store, "Old name", 1, "")
	code, out := plansSend(t, mux, http.MethodPatch, "/api/plans/"+p.ID, map[string]any{"title": "  New name ", "goal": " ship it "})
	if code != http.StatusOK {
		t.Fatalf("edit: %d %v", code, out)
	}
	plan := out["plan"].(map[string]any)
	if plan["title"] != "New name" || plan["id"] != p.ID || out["goal"] != "ship it" || out["title_set_by_hand"] != true {
		t.Fatalf("answer = %v", out)
	}
	got, err := r.store.Get(p.ID)
	if err != nil || got.Title != "New name" || got.Goal != "ship it" || !got.TitleSetByHand {
		t.Fatalf("stored = %+v %v", got, err)
	}
	// The old title still resolves as an alias.
	if code, _ = plansSend(t, mux, http.MethodPatch, "/api/plans/Old%20name", map[string]any{"goal": "again"}); code != http.StatusOK {
		t.Fatalf("by old title: %d", code)
	}
}

func TestPlansEditGoalOnlyAndClear(t *testing.T) {
	r, mux := writeRig(t)
	p := mustPlan(t, r.store, "Keep title", 1, "")
	if code, out := plansSend(t, mux, http.MethodPatch, "/api/plans/"+p.ID, map[string]any{"goal": "first"}); code != 200 || out["goal"] != "first" {
		t.Fatalf("goal only: %d %v", code, out)
	}
	got, _ := r.store.Get(p.ID)
	if got.Title != "Keep title" || got.TitleSetByHand {
		t.Fatalf("a goal edit renamed the plan: %+v", got)
	}
	// An empty goal clears it.
	if code, out := plansSend(t, mux, http.MethodPatch, "/api/plans/"+p.ID, map[string]any{"goal": ""}); code != 200 || out["goal"] != "" {
		t.Fatalf("clear: %d %v", code, out)
	}
	if got, _ = r.store.Get(p.ID); got.Goal != "" {
		t.Fatalf("goal not cleared: %q", got.Goal)
	}
}

func TestPlansEditValidation(t *testing.T) {
	r, mux := writeRig(t)
	p := mustPlan(t, r.store, "Stable", 1, "")
	long := strings.Repeat("x", planrecords.MaxPlanTitleRunes+1)
	bads := []map[string]any{
		{},                      // nothing to change
		{"title": ""},           // blank
		{"title": "   "},        // blank
		{"title": "two\nlines"}, // one line only
		{"title": long},         // over 100 characters
		{"goal": strings.Repeat("g", planrecords.MaxGoalRunes+1)},
		{"title": "Fine", "goal": strings.Repeat("g", planrecords.MaxGoalRunes+1)}, // no partial rename
	}
	for _, b := range bads {
		if code, out := plansSend(t, mux, http.MethodPatch, "/api/plans/"+p.ID, b); code != http.StatusBadRequest {
			t.Fatalf("%v: %d %v", b, code, out)
		}
	}
	if got, _ := r.store.Get(p.ID); got.Title != "Stable" || got.TitleSetByHand {
		t.Fatalf("a refused edit changed the plan: %+v", got)
	}
	if code, out := plansSend(t, mux, http.MethodPatch, "/api/plans/"+p.ID, map[string]any{"title": strings.Repeat("é", planrecords.MaxPlanTitleRunes)}); code != 200 {
		t.Fatalf("100 characters refused: %d %v", code, out)
	}
	// A body that is not JSON is 400.
	if code, _ := plansSend(t, mux, http.MethodPatch, "/api/plans/"+p.ID, "not an object"); code != http.StatusBadRequest {
		t.Fatalf("bad body: %d", code)
	}
}

func TestPlansEditConflictNamesOtherPlan(t *testing.T) {
	r, mux := writeRig(t)
	a := mustPlan(t, r.store, "Alpha", 1, "")
	b := mustPlan(t, r.store, "Beta", 2, "")
	code, out := plansSend(t, mux, http.MethodPatch, "/api/plans/"+b.ID, map[string]any{"title": "alpha", "goal": "should not land"})
	if code != http.StatusConflict {
		t.Fatalf("conflict: %d %v", code, out)
	}
	msg, _ := out["error"].(string)
	if !strings.Contains(msg, `"Alpha"`) || !strings.Contains(msg, a.ID) {
		t.Fatalf("message does not name the other plan: %q", msg)
	}
	if ex, _ := out["existing"].(map[string]any); ex == nil || ex["id"] != a.ID {
		t.Fatalf("existing = %v", out["existing"])
	}
	if got, _ := r.store.Get(b.ID); got.Title != "Beta" || got.Goal != "" {
		t.Fatalf("a refused rename changed the plan: %+v", got)
	}
}

func TestPlansEditUnknownAndNotAPlan(t *testing.T) {
	r, mux := writeRig(t)
	if code, _ := plansSend(t, mux, http.MethodPatch, "/api/plans/ghost", map[string]any{"goal": "x"}); code != http.StatusNotFound {
		t.Fatalf("unknown plan: %d", code)
	}
	row, err := r.store.Create(planrecords.Record{Kind: planrecords.KindRegistryRow, Project: "datawatch", Title: "A row"})
	if err != nil {
		t.Fatal(err)
	}
	if code, out := plansSend(t, mux, http.MethodPatch, "/api/plans/"+row.ID, map[string]any{"title": "Renamed"}); code != http.StatusBadRequest {
		t.Fatalf("not a plan: %d %v", code, out)
	}
}

func TestPlansEditNeedsWriteCapability(t *testing.T) {
	r, mux := writeRig(t)
	p := mustPlan(t, r.store, "Guarded", 1, "")
	body := map[string]any{"goal": "g"}
	if code, _ := plansSend(t, mux, http.MethodPatch, "/api/plans/"+p.ID, body, federation.CapAutonomousRead); code != http.StatusForbidden {
		t.Fatalf("read-only peer: %d", code)
	}
	if code, _ := plansSend(t, mux, http.MethodPatch, "/api/plans/"+p.ID, body, federation.CapAutonomousWrite); code != 200 {
		t.Fatalf("write peer: %d", code)
	}
}

func TestPlansEditOffAnswers404(t *testing.T) {
	r, mux := writeRig(t)
	p := mustPlan(t, r.store, "Off", 1, "")
	off := false
	r.s.cfg.Autonomous.PlanRecords.Enabled = &off
	if code, _ := plansSend(t, mux, http.MethodPatch, "/api/plans/"+p.ID, map[string]any{"goal": "g"}); code != http.StatusNotFound {
		t.Fatalf("plan records off: %d", code)
	}
}
