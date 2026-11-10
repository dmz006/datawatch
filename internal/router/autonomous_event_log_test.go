// Comm verb tests for "autonomous event-log": argument parsing, the query
// forwarded to the REST route, and the rendered text list.

package router

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// eventLogDaemon serves GET /api/autonomous/event-log with the given body and
// records each request it receives.
func eventLogDaemon(t *testing.T, body string, reqs *[]subsDaemonRequest) (int, func()) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*reqs = append(*reqs, subsDaemonRequest{method: r.Method, path: r.URL.Path, rawquery: r.URL.RawQuery})
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/api/autonomous/event-log" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	idx := strings.LastIndex(srv.URL, ":")
	port := 0
	for i := idx + 1; i < len(srv.URL); i++ {
		port = port*10 + int(srv.URL[i]-'0')
	}
	return port, srv.Close
}

// eventLogBody builds a REST body with n events, newest first.
func eventLogBody(n int, next string, total int) string {
	type ev struct {
		Time       string `json:"time"`
		Kind       string `json:"kind"`
		Project    string `json:"project"`
		Summary    string `json:"summary"`
		NextAction string `json:"next_action"`
	}
	evs := make([]ev, 0, n)
	for i := 0; i < n; i++ {
		evs = append(evs, ev{
			Time:       fmt.Sprintf("2026-11-10T02:%02d:00Z", 59-i),
			Kind:       "watch",
			Project:    "datawatch",
			Summary:    fmt.Sprintf("summary %d", i),
			NextAction: fmt.Sprintf("act %d", i),
		})
	}
	b, _ := json.Marshal(map[string]any{"events": evs, "next_cursor": next, "total": total})
	return string(b)
}

func TestAutonomousEventLogVerbParse(t *testing.T) {
	good, err := parseCommEventLog([]string{"kind=watch,lane", "project=datawatch", "q=stuck", "limit=4", "before=abc"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if good.kind != "watch,lane" || good.project != "datawatch" || good.q != "stuck" || good.limit != 4 || good.before != "abc" {
		t.Fatalf("parsed = %+v", good)
	}
	none, err := parseCommEventLog(nil)
	if err != nil || none.limit != commEventLogDefaultLimit || none.kind != "" {
		t.Fatalf("no args = %+v, %v", none, err)
	}
	blank, err := parseCommEventLog([]string{"limit=", "kind="})
	if err != nil || blank.limit != commEventLogDefaultLimit || blank.kind != "" {
		t.Fatalf("blank values should mean unset: %+v, %v", blank, err)
	}
	for _, bad := range [][]string{
		{"limit=0"}, {"limit=11"}, {"limit=abc"}, {"limit=-1"},
		{"bogus=1"}, {"watch"}, {"=x"},
	} {
		if _, err := parseCommEventLog(bad); err == nil {
			t.Errorf("%v: want an error", bad)
		}
	}
}

func TestAutonomousEventLogVerbForwardsQuery(t *testing.T) {
	var reqs []subsDaemonRequest
	port, stop := eventLogDaemon(t, eventLogBody(0, "", 0), &reqs)
	defer stop()

	out := joinReplies(subsRouter(t, "grp1", port).HandleTestMessage(
		"autonomous event-log kind=watch project=datawatch q=stuck limit=3 before=CUR1"))
	if len(reqs) != 1 {
		t.Fatalf("daemon requests = %d, want 1 (%v)", len(reqs), reqs)
	}
	if reqs[0].path != "/api/autonomous/event-log" {
		t.Fatalf("path = %q", reqs[0].path)
	}
	q, err := url.ParseQuery(reqs[0].rawquery)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	want := map[string]string{"kind": "watch", "project": "datawatch", "q": "stuck", "limit": "3", "before": "CUR1"}
	for k, v := range want {
		if q.Get(k) != v {
			t.Errorf("query %s = %q, want %q (%s)", k, q.Get(k), v, reqs[0].rawquery)
		}
	}
	if !strings.Contains(out, "no matching Automata events") {
		t.Errorf("empty result reply:\n%s", out)
	}
}

func TestAutonomousEventLogVerbRendersTenEntries(t *testing.T) {
	var reqs []subsDaemonRequest
	// The daemon could return more than ten; the verb shows at most ten.
	port, stop := eventLogDaemon(t, eventLogBody(12, "CURNEXT", 30), &reqs)
	defer stop()

	out := joinReplies(subsRouter(t, "grp1", port).HandleTestMessage("autonomous event-log project=datawatch"))
	if got := strings.Count(out, "| next: act "); got != commEventLogMaxLimit {
		t.Errorf("entry lines = %d, want %d:\n%s", got, commEventLogMaxLimit, out)
	}
	if strings.Contains(out, "summary 10") || strings.Contains(out, "summary 11") {
		t.Errorf("entries beyond ten were rendered:\n%s", out)
	}
	for _, want := range []string{
		"10 of 30 matching events",
		"2026-11-10T02:59:00Z watch summary 0 | next: act 0",
		"more: autonomous event-log project=datawatch before=CURNEXT",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("reply lacks %q:\n%s", want, out)
		}
	}
}

func TestAutonomousEventLogVerbUsageAndNoPRDWording(t *testing.T) {
	var reqs []subsDaemonRequest
	port, stop := eventLogDaemon(t, eventLogBody(1, "", 1), &reqs)
	defer stop()
	r := subsRouter(t, "grp1", port)

	out := joinReplies(r.HandleTestMessage("autonomous event-log limit=50"))
	if !strings.Contains(out, "limit must be a whole number from 1 to 10") || !strings.Contains(out, commEventLogUsage) {
		t.Errorf("bad limit should show the usage line:\n%s", out)
	}
	if len(reqs) != 0 {
		t.Errorf("a refused argument must not reach the daemon (%v)", reqs)
	}

	out = joinReplies(r.HandleTestMessage("autonomous event-log"))
	if strings.Contains(strings.ToLower(out), "prd") {
		t.Errorf("operator text must say Automaton/Automata, not PRD:\n%s", out)
	}
	if !strings.Contains(out, "1 of 1 matching events") {
		t.Errorf("single-entry reply:\n%s", out)
	}
	if strings.Contains(out, "more:") {
		t.Errorf("no next cursor means no more line:\n%s", out)
	}
}
