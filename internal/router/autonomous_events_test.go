// Comm verb tests for "autonomous events": parse table, default queue
// name, and owner-scoped isolation against a fake daemon loopback.

package router

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeEventsDaemon serves the owner-filtered subscription list and the
// events route. Subscriptions: "test:grp1" feeds queue "mine" and the
// derived default "test:grp1"; "signal:$other" feeds queue "theirs".
func fakeEventsDaemon(t *testing.T, reqs *[]subsDaemonRequest) (int, func()) {
	t.Helper()
	sub := func(owner, queue string) map[string]any {
		return map[string]any{"id": "s-" + queue, "owner": owner,
			"filter": map[string]any{"kinds": []string{"prd.blocked"}},
			"sink":   map[string]any{"type": "mcp_queue", "target": queue}}
	}
	byOwner := map[string][]map[string]any{
		"test:grp1":     {sub("test:grp1", "mine"), sub("test:grp1", "test:grp1")},
		"signal:$other": {sub("signal:$other", "theirs")},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*reqs = append(*reqs, subsDaemonRequest{method: r.Method, path: r.URL.Path, rawquery: r.URL.RawQuery})
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/autonomous/subscriptions":
			list := byOwner[r.URL.Query().Get("owner")]
			if list == nil {
				list = []map[string]any{}
			}
			_ = json.NewEncoder(w).Encode(list)
		case "/api/autonomous/events":
			_, _ = w.Write([]byte(`{"events":[{"at":"2026-01-02T03:04:05Z","kind":"prd.blocked","prd_id":"prd_a"}],"next_cursor":5,"dropped":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	idx := strings.LastIndex(srv.URL, ":")
	port := 0
	for i := idx + 1; i < len(srv.URL); i++ {
		port = port*10 + int(srv.URL[i]-'0')
	}
	return port, srv.Close
}

func TestPendingEventsCommParse(t *testing.T) {
	good, err := parseCommEvents([]string{"queue=mine", "since=12"})
	if err != nil || good.queue != "mine" || good.since != 12 {
		t.Fatalf("got %+v, %v", good, err)
	}
	if none, err := parseCommEvents(nil); err != nil || none.queue != "" || none.since != 0 {
		t.Fatalf("no args: %+v, %v", none, err)
	}
	for _, bad := range [][]string{{"queue="}, {"since=abc"}, {"since=-1"}, {"mine"}} {
		if _, err := parseCommEvents(bad); err == nil {
			t.Errorf("%v: want an error", bad)
		}
	}
}

func TestPendingEventsCommDefaultQueueName(t *testing.T) {
	for in, want := range map[string]string{
		"telegram:-1001234": "telegram:-1001234",
		"signal:$abc":       "signal:_abc",
		"matrix:!r@x":       "matrix:_r_x",
	} {
		if got := commEventsQueueName(in); got != want {
			t.Errorf("commEventsQueueName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPendingEventsCommReadsOwnQueue(t *testing.T) {
	var reqs []subsDaemonRequest
	port, stop := fakeEventsDaemon(t, &reqs)
	defer stop()

	out := joinReplies(subsRouter(t, "grp1", port).HandleTestMessage("autonomous events queue=mine since=2"))
	if len(reqs) != 2 {
		t.Fatalf("daemon requests = %d, want 2 (%v)", len(reqs), reqs)
	}
	if !strings.Contains(reqs[0].rawquery, "owner=test%3Agrp1") {
		t.Errorf("ownership query = %q", reqs[0].rawquery)
	}
	if reqs[1].path != "/api/autonomous/events" || !strings.Contains(reqs[1].rawquery, "queue=mine") || !strings.Contains(reqs[1].rawquery, "since=2") {
		t.Errorf("events request = %+v", reqs[1])
	}
	for _, want := range []string{"prd.blocked", "prd=prd_a", "dropped", "since=5"} {
		if !strings.Contains(out, want) {
			t.Errorf("reply lacks %q:\n%s", want, out)
		}
	}
}

func TestPendingEventsCommDefaultsToChannelQueue(t *testing.T) {
	var reqs []subsDaemonRequest
	port, stop := fakeEventsDaemon(t, &reqs)
	defer stop()

	subsRouter(t, "grp1", port).HandleTestMessage("autonomous events")
	if len(reqs) != 2 || !strings.Contains(reqs[1].rawquery, "queue=test%3Agrp1") {
		t.Fatalf("requests = %+v", reqs)
	}
}

func TestPendingEventsCommCannotReadAnotherChannelsQueue(t *testing.T) {
	var reqs []subsDaemonRequest
	port, stop := fakeEventsDaemon(t, &reqs)
	defer stop()

	out := joinReplies(subsRouter(t, "grp1", port).HandleTestMessage("autonomous events queue=theirs"))
	if !strings.Contains(out, "not found") {
		t.Errorf("reply = %q, want a not-found refusal", out)
	}
	for _, rq := range reqs {
		if rq.path == "/api/autonomous/events" {
			t.Fatalf("the foreign queue was read: %+v", rq)
		}
	}
}

func TestPendingEventsCommUsageError(t *testing.T) {
	out := joinReplies(bareSubsRouter().HandleTestMessage("autonomous events since=nope"))
	if !strings.Contains(out, "usage") {
		t.Errorf("reply = %q", out)
	}
}
