package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const routerArcFixture = `{"generated_at":"2026-11-01T09:00:00Z","available":true,"arc":{"available":true,"id":"a1","name":"Release arc","stages":[{"stage":"bugs","total":4,"done":2,"running":1,"blocked":0,"percent":50},{"stage":"features","total":2,"done":0,"running":0,"blocked":1,"percent":0},{"stage":"final","total":1,"done":0,"running":0,"blocked":0,"percent":0}],"maintenance_queue":{"Total":3,"Done":1,"Running":0,"Blocked":0},"blocked_stage":{"stage":"features","reason":"Login is blocked: waiting on review"}},"lanes":{"available":true,"lanes":[{"kind":"claude","in_flight":1,"cap":2}],"queued_by_priority":[]},"usage":{"available":true,"points":{"used":12.5,"allowance":100,"remaining":87.5,"percent":12.5},"tokens":{"in":1000,"out":500,"total":1500}}}`

func TestParse_DashboardArc(t *testing.T) {
	for _, in := range []string{"dashboard arc", "DASHBOARD ARC", "dashboard-arc"} {
		if got := Parse(in).Type; got != CmdDashboardArc {
			t.Errorf("Parse(%q) = %q", in, got)
		}
	}
	for _, in := range []string{"dashboard arcx", "dashboard arc extra"} {
		if got := Parse(in).Type; got == CmdDashboardArc {
			t.Errorf("Parse(%q) must not be the arc command", in)
		}
	}
}

func TestDashboardArcOneLiner(t *testing.T) {
	line, err := dashboardArcOneLiner(routerArcFixture)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"arc Release arc", "bugs 2/4 done (50.0%)", "blocked: Login is blocked", "claude lane 1/2", "12.5 of 100.0 points", "1500 tokens"} {
		if !strings.Contains(line, want) {
			t.Errorf("line lacks %q:\n%s", want, line)
		}
	}
	if strings.Contains(line, "$") {
		t.Errorf("must not show dollars: %s", line)
	}
}

func TestDashboardArcCommRoundTrip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/dashboard/arc" {
			_, _ = w.Write([]byte(routerArcFixture))
			return
		}
		t.Errorf("unexpected path %s", r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	r := loopbackRouter(t, srv)
	out := strings.Join(r.HandleTestMessage("dashboard arc"), "\n")
	if !strings.Contains(out, "dashboard arc:") || !strings.Contains(out, "bugs 2/4 done") {
		t.Fatalf("bad reply:\n%s", out)
	}
}

func TestDashboardArcCommRefusal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "disabled", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	r := loopbackRouter(t, srv)
	out := strings.Join(r.HandleTestMessage("dashboard arc"), "\n")
	if !strings.Contains(out, "dashboard arc failed") {
		t.Fatalf("reply lacks failure label:\n%s", out)
	}
}
