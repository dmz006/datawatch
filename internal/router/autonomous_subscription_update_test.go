// Tests for the comm verbs "autonomous subscription-update" and
// "autonomous subscription-test": key=value parsing, the merge into the
// stored record, the proxied method/path, error passthrough and the
// owner scoping (a channel gets the same not-found answer for someone
// else's id as for an unknown one).

package router

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// updDaemon is a fake daemon holding owner-tagged records. It records
// every request, serves the owner-scoped list, and answers PUT / test
// with the configured status.
type updDaemon struct {
	reqs     []subsDaemonRequest
	putCode  int
	putBody  string
	testCode int
	testBody string
}

func newUpdDaemon(t *testing.T, d *updDaemon) int {
	t.Helper()
	records := []map[string]any{
		{
			"id": "sub_mine", "owner": "test:grp1",
			"filter":     map[string]any{"kinds": []string{"task.completed"}, "prd_id": "prd_a", "task_id": "task_1"},
			"sink":       map[string]any{"type": "comm", "target": "test:grp1"},
			"action":     map[string]any{"type": "send_input", "text": "keep me"},
			"coalesce":   true,
			"one_shot":   false,
			"expires_at": "2099-01-01T00:00:00Z",
		},
		{
			"id": "sub_other", "owner": "signal:$someoneelse",
			"filter": map[string]any{"kinds": []string{"task.failed"}},
			"sink":   map[string]any{"type": "comm", "target": "signal:$someoneelse"},
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		d.reqs = append(d.reqs, subsDaemonRequest{method: r.Method, path: r.URL.Path, rawquery: r.URL.RawQuery, body: string(b)})
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet:
			owner := r.URL.Query().Get("owner")
			out := []any{}
			for _, rc := range records {
				if rc["owner"] == owner {
					out = append(out, rc)
				}
			}
			j, _ := json.Marshal(out)
			_, _ = w.Write(j)
		case r.Method == http.MethodPut:
			if d.putCode >= 400 {
				http.Error(w, d.putBody, d.putCode)
				return
			}
			_, _ = w.Write([]byte(d.putBody))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/test"):
			if d.testCode >= 400 {
				http.Error(w, d.testBody, d.testCode)
				return
			}
			_, _ = w.Write([]byte(d.testBody))
		default:
			http.Error(w, "unexpected "+r.Method+" "+r.URL.Path, http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(srv.Close)
	idx := strings.LastIndex(srv.URL, ":")
	port := 0
	for i := idx + 1; i < len(srv.URL); i++ {
		port = port*10 + int(srv.URL[i]-'0')
	}
	return port
}

// updHarness drives the verbs through the router's command entry point,
// so the dispatch of the new verbs is covered along with the handlers.
type updHarness struct {
	r   *Router
	out []string
}

// updRouter returns a harness for channel "test:grp1" talking to the fake
// daemon on port (0 = no loopback configured).
func updRouter(t *testing.T, port int) *updHarness {
	t.Helper()
	r := bareSubsRouter()
	if port != 0 {
		r.SetWebPort(port)
	}
	return &updHarness{r: r}
}

func (h *updHarness) say(text string) string {
	h.out = h.r.HandleTestMessage(text)
	return h.joined()
}

func (h *updHarness) joined() string { return joinReplies(h.out) }

func (h *updHarness) update(args ...string) string {
	return h.say(strings.TrimSpace("autonomous subscription-update " + strings.Join(args, " ")))
}

func (h *updHarness) test(args ...string) string {
	return h.say(strings.TrimSpace("autonomous subscription-test " + strings.Join(args, " ")))
}

func TestSubscriptionSurface_ParseCommSubUpdate(t *testing.T) {
	good, err := parseCommSubUpdate([]string{"kinds=session_started,prd.blocked", "level=task", "prd=prd_b", "task=", "coalesce=false", "one_shot=true", "expires=60"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(good.kinds) != 2 || good.kinds[0] != "task.session_started" || good.kinds[1] != "prd.blocked" {
		t.Errorf("kinds = %v", good.kinds)
	}
	if good.level != "task" || good.prdID != "prd_b" || !good.setTask || good.taskID != "" {
		t.Errorf("parsed = %+v", good)
	}
	if !good.setCoal || good.coalesce || !good.setOneShot || !good.oneShot || good.expiresSec != 60 {
		t.Errorf("parsed = %+v", good)
	}

	bad := []struct {
		name string
		args []string
		want string
	}{
		{"bare word", []string{"prd.blocked"}, "not key=value"},
		{"unknown key", []string{"colour=blue"}, "unknown key"},
		{"sink outside the channel set", []string{"sink=topic"}, "sink must be comm, webhook or peer"},
		{"bad level", []string{"level=story"}, "level must be prd or task"},
		{"bad bool", []string{"coalesce=maybe"}, "must be true or false"},
		{"bad expires", []string{"expires=-5"}, "non-negative"},
		{"expires text", []string{"expires=soon"}, "non-negative"},
		{"empty", nil, "nothing to change"},
	}
	for _, tc := range bad {
		if _, err := parseCommSubUpdate(tc.args); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want containing %q", tc.name, err, tc.want)
		}
	}
}

func TestSubscriptionSurface_CommUpdateUsage(t *testing.T) {
	// A whitespace-only id is dropped by the router, leaving too few arguments.
	for _, args := range [][]string{nil, {"sub_mine"}, {" ", "coalesce=true"}} {
		h := updRouter(t, 0)
		if reply := h.update(args...); !strings.Contains(reply, "usage: autonomous subscription-update") {
			t.Errorf("args %q: reply = %q, want the usage line", args, reply)
		}
	}
	// A parse error is reported without touching the daemon (no loopback is configured).
	h := updRouter(t, 0)
	if reply := h.update("sub_mine", "bogus"); !strings.Contains(reply, "not key=value") {
		t.Errorf("reply = %q", reply)
	}
}

func TestSubscriptionSurface_CommUpdateMergesAndPuts(t *testing.T) {
	d := &updDaemon{putBody: subsRecordJSON("sub_mine", "test:grp1", []string{"prd.blocked"}, "prd_a", "")}
	h := updRouter(t, newUpdDaemon(t, d))
	before := time.Now().UTC()

	h.update("sub_mine", "kinds=prd.blocked", "task=", "one_shot=true", "expires=3600")

	if len(d.reqs) != 2 {
		t.Fatalf("requests = %+v, want an ownership GET then a PUT", d.reqs)
	}
	if g := d.reqs[0]; g.method != http.MethodGet || !strings.Contains(g.rawquery, "owner=test%3Agrp1") {
		t.Errorf("first request = %+v, want the owner-scoped GET", g)
	}
	put := d.reqs[1]
	if put.method != http.MethodPut || put.path != "/api/autonomous/subscriptions/sub_mine" {
		t.Fatalf("second request = %s %s, want PUT /api/autonomous/subscriptions/sub_mine", put.method, put.path)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(put.body), &body); err != nil {
		t.Fatalf("PUT body: %v", err)
	}
	filter := body["filter"].(map[string]any)
	kinds, _ := filter["kinds"].([]any)
	if len(kinds) != 1 || kinds[0] != "prd.blocked" {
		t.Errorf("filter.kinds = %v", filter["kinds"])
	}
	if filter["prd_id"] != "prd_a" || filter["task_id"] != "" {
		t.Errorf("filter = %v (prd kept, task cleared)", filter)
	}
	sink := body["sink"].(map[string]any)
	if sink["type"] != "comm" || sink["target"] != "test:grp1" || body["owner"] != "test:grp1" {
		t.Errorf("sink/owner must stay pinned to the channel: sink=%v owner=%v", sink, body["owner"])
	}
	if body["coalesce"] != true || body["one_shot"] != true {
		t.Errorf("coalesce/one_shot = %v/%v", body["coalesce"], body["one_shot"])
	}
	if a, _ := body["action"].(map[string]any); a == nil || a["text"] != "keep me" {
		t.Errorf("untouched action must be carried over, got %v", body["action"])
	}
	exp, _ := body["expires_at"].(string)
	at, err := time.Parse(time.RFC3339, exp)
	if err != nil {
		t.Fatalf("expires_at %q: %v", exp, err)
	}
	if dlt := at.Sub(before); dlt < 59*time.Minute || dlt > 61*time.Minute {
		t.Errorf("expires_at is %v from now, want about 1h", dlt)
	}
	if reply := h.joined(); !strings.Contains(reply, "updated sub_mine") {
		t.Errorf("reply = %q", reply)
	}
}

func TestSubscriptionSurface_CommUpdateExpiresZeroClears(t *testing.T) {
	d := &updDaemon{putBody: subsRecordJSON("sub_mine", "test:grp1", []string{"task.completed"}, "", "")}
	h := updRouter(t, newUpdDaemon(t, d))
	h.update("sub_mine", "expires=0")
	if len(d.reqs) != 2 {
		t.Fatalf("requests = %+v", d.reqs)
	}
	var body map[string]any
	_ = json.Unmarshal([]byte(d.reqs[1].body), &body)
	if _, ok := body["expires_at"]; ok {
		t.Errorf("expires=0 must clear expires_at, body = %s", d.reqs[1].body)
	}
}

func TestSubscriptionSurface_CommUpdateOwnerScoping(t *testing.T) {
	d := &updDaemon{putBody: `{}`}
	h := updRouter(t, newUpdDaemon(t, d))

	h.update("sub_other", "coalesce=false")
	otherReply := h.joined()
	h.update("sub_never", "coalesce=false")
	unknownReply := h.joined()

	if !strings.Contains(otherReply, `id "sub_other" not found`) {
		t.Errorf("another channel's id must read as not found, got %q", otherReply)
	}
	want := strings.ReplaceAll(otherReply, "sub_other", "sub_never")
	if unknownReply != want {
		t.Errorf("foreign and unknown ids must get the same answer:\n foreign: %q\n unknown: %q", otherReply, unknownReply)
	}
	for _, rq := range d.reqs {
		if rq.method != http.MethodGet {
			t.Errorf("no write may be sent for a foreign/unknown id, got %s %s", rq.method, rq.path)
		}
	}
}

func TestSubscriptionSurface_CommUpdateErrorPassthrough(t *testing.T) {
	for _, tc := range []struct {
		code int
		body string
	}{
		{http.StatusBadRequest, "filter kind \"x\" is not a known event kind"},
		{http.StatusNotFound, "subscription gone: subscription not found"},
	} {
		d := &updDaemon{putCode: tc.code, putBody: tc.body}
		h := updRouter(t, newUpdDaemon(t, d))
		h.update("sub_mine", "coalesce=false")
		reply := h.joined()
		if !strings.Contains(reply, "subscription-update failed") || !strings.Contains(reply, tc.body) {
			t.Errorf("code %d: reply = %q, want a failure carrying %q", tc.code, reply, tc.body)
		}
	}
}

func TestSubscriptionSurface_CommUpdateNoLoopback(t *testing.T) {
	h := updRouter(t, 0)
	if reply := h.update("sub_mine", "coalesce=false"); !strings.Contains(reply, "failed") {
		t.Errorf("reply = %q, want a failure", reply)
	}
}

func TestSubscriptionSurface_CommTestUsage(t *testing.T) {
	for _, args := range [][]string{nil, {"a", "b"}} {
		h := updRouter(t, 0)
		if reply := h.test(args...); !strings.Contains(reply, "usage: autonomous subscription-test <id>") {
			t.Errorf("args %q: reply = %q", args, reply)
		}
	}
}

func TestSubscriptionSurface_CommTestPostsAfterOwnerCheck(t *testing.T) {
	d := &updDaemon{testBody: `{"ok":true,"sink":"comm:test:grp1"}`}
	h := updRouter(t, newUpdDaemon(t, d))
	h.test("sub_mine")
	if len(d.reqs) != 2 || d.reqs[0].method != http.MethodGet {
		t.Fatalf("requests = %+v, want an ownership GET then a POST", d.reqs)
	}
	if p := d.reqs[1]; p.method != http.MethodPost || p.path != "/api/autonomous/subscriptions/sub_mine/test" {
		t.Errorf("second request = %s %s", p.method, p.path)
	}
	if reply := h.joined(); !strings.Contains(reply, "test event delivered for sub_mine") {
		t.Errorf("reply = %q", reply)
	}
}

func TestSubscriptionSurface_CommTestReportsDeliveryFailure(t *testing.T) {
	d := &updDaemon{testBody: `{"ok":false,"sink":"comm","error":"router offline"}`}
	h := updRouter(t, newUpdDaemon(t, d))
	h.test("sub_mine")
	reply := h.joined()
	if !strings.Contains(reply, "failed") || !strings.Contains(reply, "router offline") {
		t.Errorf("reply = %q, want the delivery error", reply)
	}
}

func TestSubscriptionSurface_CommTestOwnerScoping(t *testing.T) {
	d := &updDaemon{testBody: `{"ok":true}`}
	h := updRouter(t, newUpdDaemon(t, d))

	h.test("sub_other")
	otherReply := h.joined()
	h.test("sub_never")
	unknownReply := h.joined()

	if !strings.Contains(otherReply, `id "sub_other" not found`) {
		t.Errorf("another channel's id must read as not found, got %q", otherReply)
	}
	if unknownReply != strings.ReplaceAll(otherReply, "sub_other", "sub_never") {
		t.Errorf("foreign and unknown ids must get the same answer:\n foreign: %q\n unknown: %q", otherReply, unknownReply)
	}
	for _, rq := range d.reqs {
		if rq.method != http.MethodGet {
			t.Errorf("no test may be fired for a foreign/unknown id, got %s %s", rq.method, rq.path)
		}
	}
}

func TestSubscriptionSurface_CommTestErrorPassthrough(t *testing.T) {
	for _, code := range []int{http.StatusNotFound, http.StatusServiceUnavailable} {
		d := &updDaemon{testCode: code, testBody: "engine unavailable"}
		h := updRouter(t, newUpdDaemon(t, d))
		h.test("sub_mine")
		reply := h.joined()
		if !strings.Contains(reply, "subscription-test failed") || !strings.Contains(reply, "engine unavailable") {
			t.Errorf("code %d: reply = %q", code, reply)
		}
	}
}
