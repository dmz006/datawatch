package mcp

import (
	"context"
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

// The Automaton get tool is a loopback proxy; the daemon's detail response
// carries the per-Automaton cost object at every detail level, and the tool
// must hand it back untouched.
func TestAutomatonCostOnPRDGet(t *testing.T) {
	for _, detail := range []string{"summary", "tasks", "full"} {
		t.Run(detail, func(t *testing.T) {
			s := bl91Server(t)
			var gotPath, gotDetail string
			automatonCostServer(t, s, func(w http.ResponseWriter, r *http.Request) {
				gotPath, gotDetail = r.URL.Path, r.URL.Query().Get("detail")
				_, _ = w.Write([]byte(`{"id":"a1","cost":{"key":"a1","sessions":3,"tool_calls":7,"report_window_note":"n"}}`))
			})
			req := call(map[string]any{"id": "a1", "detail": detail})
			res, err := s.handleAutonomousPRDGet(context.Background(), req)
			if err != nil || res == nil || res.IsError {
				t.Fatalf("get failed: %v %+v", err, res)
			}
			body := costReportBody(t, res)
			if gotPath != "/api/autonomous/prds/a1" || gotDetail != detail {
				t.Fatalf("proxied %q detail=%q", gotPath, gotDetail)
			}
			for _, want := range []string{`"cost":{`, `"sessions":3`, `"tool_calls":7`, `"report_window_note":"n"`} {
				if !strings.Contains(body, want) {
					t.Fatalf("missing %s in %s", want, body)
				}
			}
		})
	}
}
