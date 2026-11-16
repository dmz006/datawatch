package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// planRecordCLIStub stands in for the daemon on the plan-record routes:
// it records the state value each route received and refuses, with the
// daemon's plain 400, what the daemon would refuse for that collection.
func planRecordCLIStub(t *testing.T) (*httptest.Server, func(string) url.Values) {
	t.Helper()
	var mu sync.Mutex
	queries := map[string]url.Values{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		queries[r.URL.Path] = r.URL.Query()
		mu.Unlock()

		q := r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/plan-records/registry-rows":
			if strings.Contains(q.Get("state"), "plumbing") {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"state \"plumbing\" is not a valid state; use one of open, planned, in_progress, blocked, shipped, done, deferred, cancelled"}`))
				return
			}
			_, _ = w.Write([]byte(`{"total":1,"records":[{"id":"r1","title":"row one","project":"p1","state":"open"}]}`))
		case "/api/plan-records/findings":
			if strings.Contains(q.Get("state"), "plumbing") {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"state \"plumbing\" is not a valid state; use one of open, triaged, fixed, wontfix, duplicate"}`))
				return
			}
			if strings.Contains(q.Get("state"), "shipped") {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"state \"shipped\" is not a valid state; use one of open, triaged, fixed, wontfix, duplicate"}`))
				return
			}
			_, _ = w.Write([]byte(`{"total":1,"records":[{"id":"f1","title":"finding one","project":"p1","state":"open"}]}`))
		case "/api/plan-records/search":
			if strings.Contains(q.Get("state"), "plumbing") {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"state \"plumbing\" is not a valid state; use one of open, planned, in_progress, blocked, shipped, done, deferred, cancelled, triaged, fixed, wontfix, duplicate"}`))
				return
			}
			_, _ = w.Write([]byte(`{"total":0,"hits":[]}`))
		case "/api/autonomous/prds":
			if strings.Contains(q.Get("status"), "plumbing") {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"status \"plumbing\" is not a valid status; use one of draft, planning, needs_review, approved, revisions_asked, active, running, queued, waiting, waiting_input, completed, rejected, cancelled, archived, blocked, failed, shipped"}`))
				return
			}
			_, _ = w.Write([]byte(`{"prds":[],"total":0}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"no route"}`))
		}
	}))
	t.Cleanup(srv.Close)
	capture := func(path string) url.Values {
		mu.Lock()
		defer mu.Unlock()
		return queries[path]
	}
	return srv, capture
}

func pointCLIStub(t *testing.T, srvURL string) {
	t.Helper()
	prev := serverURL
	serverURL = srvURL
	t.Cleanup(func() { serverURL = prev })
}

// TestStateFilterCLIListSurface covers `plan-record list --state`: the
// flag's one-state and comma-list forms reach the daemon untouched, a
// state one kind admits is not a failure with no --kind, and a state no
// kind admits fails with the daemon's plain message.
func TestStateFilterCLIListSurface(t *testing.T) {
	srv, get := planRecordCLIStub(t)
	pointCLIStub(t, srv.URL)

	c := newPlanRecordListCmd()
	c.SetArgs([]string{"--kind", "registry-row", "--state", "open"})
	if err := runRunE(t, c); err != nil {
		t.Fatalf("list single: %v", err)
	}
	if got := get("/api/plan-records/registry-rows").Get("state"); got != "open" {
		t.Fatalf("list single: state=%q", got)
	}

	c = newPlanRecordListCmd()
	c.SetArgs([]string{"--kind", "registry-row", "--state", "open, shipped"})
	if err := runRunE(t, c); err != nil {
		t.Fatalf("list list: %v", err)
	}
	if got := get("/api/plan-records/registry-rows").Get("state"); got != "open, shipped" {
		t.Fatalf("list list: state=%q", got)
	}

	// No --kind: the state is asked of both kinds. A state only one kind
	// admits is refused by the other, which is not a failure.
	c = newPlanRecordListCmd()
	c.SetArgs([]string{"--state", "shipped"})
	if err := runRunE(t, c); err != nil {
		t.Fatalf("list mixed: %v", err)
	}
	if get("/api/plan-records/findings") == nil || get("/api/plan-records/registry-rows") == nil {
		t.Fatal("list mixed: expected both kinds to be queried")
	}

	// A state no kind admits must fail with the daemon's plain refusal.
	c = newPlanRecordListCmd()
	c.SetArgs([]string{"--state", "plumbing"})
	err := runRunE(t, c)
	if err == nil || !strings.Contains(err.Error(), `state \"plumbing\" is not a valid state`) {
		t.Fatalf("list refuse: err=%v", err)
	}

	// One kind asked: its own plain refusal is the failure.
	c = newPlanRecordListCmd()
	c.SetArgs([]string{"--kind", "finding", "--state", "shipped"})
	err = runRunE(t, c)
	if err == nil || !strings.Contains(err.Error(), `state \"shipped\" is not a valid state`) {
		t.Fatalf("list kind refuse: err=%v", err)
	}
}

// TestStateFilterCLISearchSurface covers `plan-record search --state`: the
// value travels to the search route untouched and an unknown state fails
// with the daemon's plain message.
func TestStateFilterCLISearchSurface(t *testing.T) {
	srv, get := planRecordCLIStub(t)
	pointCLIStub(t, srv.URL)

	c := newPlanRecordSearchCmd()
	c.SetArgs([]string{"row", "--state", "open, shipped"})
	if err := runRunE(t, c); err != nil {
		t.Fatalf("search: %v", err)
	}
	q := get("/api/plan-records/search")
	if q.Get("state") != "open, shipped" || q.Get("q") != "row" {
		t.Fatalf("search: state=%q q=%q", q.Get("state"), q.Get("q"))
	}

	c = newPlanRecordSearchCmd()
	c.SetArgs([]string{"row", "--state", "plumbing"})
	err := runRunE(t, c)
	if err == nil || !strings.Contains(err.Error(), `state \"plumbing\" is not a valid state`) {
		t.Fatalf("search refuse: err=%v", err)
	}
}

// TestStateFilterCLIPrdStatusSurface covers `autonomous prd-list --status`:
// the same comma-separated list travels on, and an unknown status fails
// with the daemon's plain message.
func TestStateFilterCLIPrdStatusSurface(t *testing.T) {
	srv, get := planRecordCLIStub(t)
	pointCLIStub(t, srv.URL)

	c := newAutonomousPRDListCmd()
	c.SetArgs([]string{"--status", "running, blocked"})
	if err := runRunE(t, c); err != nil {
		t.Fatalf("prd-list: %v", err)
	}
	if got := get("/api/autonomous/prds").Get("status"); got != "running, blocked" {
		t.Fatalf("prd-list: status=%q", got)
	}

	c = newAutonomousPRDListCmd()
	c.SetArgs([]string{"--status", "plumbing"})
	err := runRunE(t, c)
	if err == nil || !strings.Contains(err.Error(), `status \"plumbing\" is not a valid status`) {
		t.Fatalf("prd-list refuse: err=%v", err)
	}
}
