package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParse_DashboardData(t *testing.T) {
	cases := map[string]string{
		"dashboard work-map":                 "work-map",
		"Dashboard Lane-Pulse":               "lane-pulse",
		"dashboard lane-pulse 12h":           "lane-pulse 12h",
		"dashboard series release-train":     "series release-train",
		"dashboard series review-backlog 20": "series review-backlog 20",
	}
	for in, want := range cases {
		got := Parse(in)
		if got.Type != CmdDashboardData || got.Text != want {
			t.Errorf("Parse(%q) = %q/%q, want data/%q", in, got.Type, got.Text, want)
		}
	}
	for _, in := range []string{"dashboard work-map extra", "dashboard series", "dashboard lane-pulse 1h 2h"} {
		if got := Parse(in).Type; got == CmdDashboardData {
			t.Errorf("Parse(%q) must not be a dashboard data command", in)
		}
	}
	if got := Parse("dashboard console").Type; got != CmdDashboardConsole {
		t.Errorf("dashboard console regressed: %q", got)
	}
}

func dataRouter(t *testing.T, path, query *string, body string) *Router {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*path, *query = r.URL.Path, r.URL.RawQuery
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return loopbackRouter(t, srv)
}

func TestDashboardDataComm_WorkMapSameBody(t *testing.T) {
	var p, q string
	r := dataRouter(t, &p, &q, "{\n \"nodes\": [{\"id\":\"p1\"}]\n}")
	out := strings.Join(r.HandleTestMessage("dashboard work-map"), "\n")
	if p != "/api/dashboard/work-map" {
		t.Errorf("path = %q", p)
	}
	if !strings.Contains(out, `{"nodes":[{"id":"p1"}]}`) {
		t.Errorf("reply lacks the compact body:\n%s", out)
	}
}

func TestDashboardDataComm_LanePulseWindow(t *testing.T) {
	var p, q string
	r := dataRouter(t, &p, &q, `{"lanes":[]}`)
	strings.Join(r.HandleTestMessage("dashboard lane-pulse 12h"), "\n")
	if p != "/api/dashboard/lane-pulse" || q != "window=12h" {
		t.Errorf("got %s?%s", p, q)
	}
}

func TestDashboardDataComm_SeriesAllSix(t *testing.T) {
	want := map[string]string{"lane-utilization": "window", "throughput-forecast": "days", "verifier-health": "window",
		"release-train": "days", "review-backlog": "limit", "defects-per-day": "days"}
	for name, param := range want {
		var p, q string
		r := dataRouter(t, &p, &q, `{"points":[]}`)
		out := strings.Join(r.HandleTestMessage("dashboard series "+name+" 7"), "\n")
		if p != "/api/dashboard/series/"+name || q != param+"=7" {
			t.Errorf("%s: got %s?%s", name, p, q)
		}
		if !strings.Contains(out, `{"points":[]}`) {
			t.Errorf("%s: reply lacks body:\n%s", name, out)
		}
	}
}

func TestDashboardDataComm_UnknownSeriesAndRefusal(t *testing.T) {
	var p, q string
	r := dataRouter(t, &p, &q, `{}`)
	out := strings.Join(r.HandleTestMessage("dashboard series nope"), "\n")
	if !strings.Contains(out, "unknown series nope") || p != "" {
		t.Errorf("unknown series not refused locally (path %q):\n%s", p, out)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "work map unavailable", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	out = strings.Join(loopbackRouter(t, srv).HandleTestMessage("dashboard work-map"), "\n")
	if !strings.Contains(out, "dashboard work-map failed") || !strings.Contains(out, "work map unavailable") {
		t.Errorf("refusal not surfaced:\n%s", out)
	}
}
