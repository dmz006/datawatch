package mcp

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func automatonCostServer(t *testing.T, s *Server, h http.HandlerFunc) {
	t.Helper()
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	u, _ := url.Parse(ts.URL)
	port, _ := strconv.Atoi(u.Port())
	s.webPort = port
}

func TestAutomatonCostReportProxiesREST(t *testing.T) {
	s := bl91Server(t)
	var gotPath, gotGroup string
	automatonCostServer(t, s, func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotGroup = r.URL.Path, r.URL.Query().Get("group_by")
		_, _ = w.Write([]byte(`{"rows":[{"key":"a1","sessions":2}],"window_note":"n"}`))
	})
	body := costReportBody(t, costReportRes(t, s, map[string]any{"group_by": "automaton"}))
	if gotPath != "/api/cost/report" || gotGroup != "automaton" {
		t.Fatalf("proxied %q group_by=%q", gotPath, gotGroup)
	}
	if !strings.Contains(body, `"window_note":"n"`) || !strings.Contains(body, `"sessions":2`) {
		t.Fatalf("REST body not returned unchanged: %s", body)
	}
}

func TestAutomatonCostToolDescriptions(t *testing.T) {
	s := bl91Server(t)
	if !strings.Contains(s.toolAutonomousPRDGet().Description, "cost") {
		t.Fatal("autonomous_prd_get description does not mention cost")
	}
	if !strings.Contains(s.toolCostReport().Description, "window_note") {
		t.Fatal("cost_report description does not mention the new notes")
	}
}
