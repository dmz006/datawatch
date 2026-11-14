package server

// TestMultiState covers the shared comma-separated state/status parsing at
// the REST surface: the plan record lists (collection, search, browse) and
// the PRD list. No state keeps listing unchanged, one state behaves exactly
// as the single-value form did, several states select each of them, and an
// unknown state is a 400 whose message names the valid ones.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// prTotal reads the "total" of a plan-record list/search response body.
func prTotal(t *testing.T, m map[string]any) int {
	t.Helper()
	total, ok := m["total"].(float64)
	if !ok {
		t.Fatalf("no numeric total in %v", m)
	}
	return int(total)
}

// prRecordsOf returns the (kind, state) pairs of a plan-record list body.
func prRecordsOf(t *testing.T, m map[string]any) []string {
	t.Helper()
	raw, _ := m["records"].([]any)
	out := make([]string, 0, len(raw))
	for _, r := range raw {
		rm := r.(map[string]any)
		out = append(out, rm["kind"].(string)+"="+rm["state"].(string))
	}
	return out
}

// TestMultiStatePlanRecords runs the state query of the plan record routes
// through the shared parser over HTTP.
func TestMultiStatePlanRecords(t *testing.T) {
	rig := newPlanRecordsRig(t)
	for _, state := range []string{"open", "blocked", "shipped"} {
		rig.create(t, "registry-rows", map[string]any{"title": "row " + state, "state": state})
	}
	rig.create(t, "findings", map[string]any{"title": "finding open", "state": "open"})

	get := func(path string) (*httptest.ResponseRecorder, map[string]any) {
		rec := prCall(t, rig.s, http.MethodGet, prBase+path, nil)
		return rec, prMap(t, rec)
	}

	t.Run("no state lists every record of the kind", func(t *testing.T) {
		rec, m := get("/registry-rows")
		if rec.Code != http.StatusOK {
			t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
		}
		want := map[string]bool{"registry_row=open": true, "registry_row=blocked": true, "registry_row=shipped": true}
		got := prRecordsOf(t, m)
		if len(got) != 3 || prTotal(t, m) != 3 {
			t.Fatalf("want 3 rows, got %v (total %v)", got, m["total"])
		}
		for _, row := range got {
			if !want[row] {
				t.Fatalf("unexpected row %q in %v", row, got)
			}
		}
	})

	t.Run("one state behaves exactly as before", func(t *testing.T) {
		rec, m := get("/registry-rows?state=blocked")
		if rec.Code != http.StatusOK {
			t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
		}
		if prTotal(t, m) != 1 || prRecordsOf(t, m)[0] != "registry_row=blocked" {
			t.Fatalf("want the single blocked row, got %v", m)
		}
	})

	t.Run("several states select each of them", func(t *testing.T) {
		rec, m := get("/registry-rows?state=open,%20blocked")
		if rec.Code != http.StatusOK {
			t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
		}
		if prTotal(t, m) != 2 || len(prRecordsOf(t, m)) != 2 {
			t.Fatalf("want the opened and blocked rows, got %v", m)
		}
	})

	t.Run("repeated states dedupe to one match", func(t *testing.T) {
		rec, m := get("/registry-rows?state=open,shipped,open")
		if rec.Code != http.StatusOK {
			t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
		}
		if prTotal(t, m) != 2 {
			t.Fatalf("want 2, got %v", m)
		}
	})

	t.Run("an unknown state is a 400 naming the valid ones", func(t *testing.T) {
		rec := prCall(t, rig.s, http.MethodGet, prBase+"/registry-rows?state=open,nope", nil)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
		}
		msg := rec.Body.String()
		for _, name := range []string{"nope", "open", "blocked", "shipped"} {
			if !strings.Contains(msg, name) {
				t.Fatalf("400 body must name %q: %s", name, msg)
			}
		}
	})

	t.Run("a state of another kind is still refused on a kind-fixed route", func(t *testing.T) {
		if rec := prCall(t, rig.s, http.MethodGet, prBase+"/registry-rows?state=triaged", nil); rec.Code != http.StatusBadRequest {
			t.Fatalf("want 400, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("search accepts a state valid for either kind", func(t *testing.T) {
		rec := prCall(t, rig.s, http.MethodGet, prBase+"/search?q=finding&state=open,triaged", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
		}
		m := prMap(t, rec)
		if m["total"] == nil {
			t.Fatalf("want a total in %v", m)
		}
	})

	t.Run("browse keeps its single-state form and takes several", func(t *testing.T) {
		rec := prCall(t, rig.s, http.MethodGet, prBase+"/browse?kind=registry_row&state=open,shipped", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
		}
		m := prMap(t, rec)
		records, _ := m["records"].([]any)
		if len(records) != 2 {
			t.Fatalf("want 2 browse rows, got %v", m)
		}
		// Without a kind, browse admits only the states both kinds know.
		if rec := prCall(t, rig.s, http.MethodGet, prBase+"/browse?state=shipped", nil); rec.Code != http.StatusBadRequest {
			t.Fatalf("kindless browse must refuse a kind-specific state, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

// TestMultiStatePRDList runs the status query of the PRD list through the
// shared parser over HTTP.
func TestMultiStatePRDList(t *testing.T) {
	// prdListServer(n) fixtures cycle: completed, running, blocked, failed,
	// draft — of 10 rows that is 2 each.
	s := prdListServer(10)

	t.Run("no status lists everything", func(t *testing.T) {
		rows, resp := prdListDecode(t, prdListGet(t, s, ""))
		if len(rows) != 10 || resp["total"].(float64) != 10 {
			t.Fatalf("want 10 rows, got %d (total %v)", len(rows), resp["total"])
		}
	})

	t.Run("one status behaves exactly as before", func(t *testing.T) {
		rows, _ := prdListDecode(t, prdListGet(t, s, "?status=running"))
		if len(rows) != 2 {
			t.Fatalf("want the 2 running PRDs, got %d", len(rows))
		}
		for _, r := range rows {
			if r["status"] != "running" {
				t.Fatalf("non-running row under status=running: %v", r)
			}
		}
	})

	t.Run("several statuses select each of them", func(t *testing.T) {
		rows, _ := prdListDecode(t, prdListGet(t, s, "?status=running,%20blocked"))
		if len(rows) != 4 {
			t.Fatalf("want 4 rows, got %d", len(rows))
		}
	})

	t.Run("uppercase statuses keep working", func(t *testing.T) {
		rows, _ := prdListDecode(t, prdListGet(t, s, "?status=RUNNING,BLOCKED"))
		if len(rows) != 4 {
			t.Fatalf("want 4 rows, got %d", len(rows))
		}
	})

	t.Run("an unknown status is a 400 naming the valid ones", func(t *testing.T) {
		rr := prdListGet(t, s, "?status=running,nope")
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d: %s", rr.Code, rr.Body.String())
		}
		for _, name := range []string{"nope", "running", "draft"} {
			if !strings.Contains(rr.Body.String(), name) {
				t.Fatalf("400 body must name %q: %s", name, rr.Body.String())
			}
		}
	})
}
