// BL427 (phase 3) — comm-channel verbs for Automaton lifecycle-event
// subscriptions. Covers the parse table, the subscribe/list/unsubscribe
// round trip against a fake daemon loopback, owner-scoped isolation, and
// the operator-facing usage strings.

package router

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// subsDaemonRequest records one loopback call the router makes.
type subsDaemonRequest struct {
	method   string
	path     string
	rawquery string
	body     string
}

// subsRecordJSON builds one subscription record in the daemon's JSON
// shape (a projection of autonomous.Subscription): id, owner, filter,
// sink, coalesce.
func subsRecordJSON(id, owner string, kinds []string, prdID, taskID string) string {
	b, _ := json.Marshal(map[string]any{
		"id":       id,
		"owner":    owner,
		"filter":   map[string]any{"kinds": kinds, "prd_id": prdID, "task_id": taskID},
		"sink":     map[string]any{"type": "comm", "target": owner},
		"coalesce": true,
	})
	return string(b)
}

type subsRecordEntry struct {
	id     string
	owner  string
	kinds  []string
	prdID  string
	taskID string
}

// fakeSubsDaemon stands in for the daemon's /api/autonomous/subscriptions
// REST end (the real surface: internal/server/autonomous_subscriptions.go).
// POST echoes the record back with a server-assigned id (201); GET honors
// the owner filter; DELETE is 204 (404 on an id the fake store lacks).
// Every request is recorded in order.
func fakeSubsDaemon(t *testing.T, reqs *[]subsDaemonRequest) (int, func()) {
	t.Helper()
	// The store: owner → records.
	store := map[string][]subsRecordEntry{
		"test:grp1": {
			{"sub_a", "test:grp1", []string{"task.session_started", "prd.blocked"}, "prd_a3f9", ""},
			{"sub_b", "test:grp1", []string{"task.completed"}, "prd_a3f9", "task_7"},
		},
		"signal:$someoneelse": {
			{"sub_xo", "signal:$someoneelse", []string{"task.failed"}, "", ""},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		*reqs = append(*reqs, subsDaemonRequest{
			method:   r.Method,
			path:     r.URL.Path,
			rawquery: r.URL.RawQuery,
			body:     string(b),
		})
		w.Header().Set("Content-Type", "application/json")

		switch r.Method {
		case http.MethodPost:
			if r.URL.Path != "/api/autonomous/subscriptions" {
				http.Error(w, "unexpected POST "+r.URL.Path, http.StatusNotFound)
				return
			}
			var in map[string]any
			if err := json.Unmarshal(b, &in); err != nil {
				http.Error(w, "bad record: "+err.Error(), http.StatusBadRequest)
				return
			}
			in["id"] = "sub_new1" // the server assigns the id
			out, _ := json.Marshal(in)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write(out)

		case http.MethodGet:
			var recs []any
			for _, e := range store[r.URL.Query().Get("owner")] {
				recs = append(recs, map[string]any{
					"id":       e.id,
					"owner":    e.owner,
					"filter":   map[string]any{"kinds": e.kinds, "prd_id": e.prdID, "task_id": e.taskID},
					"sink":     map[string]any{"type": "comm", "target": e.owner},
					"coalesce": true,
				})
			}
			if recs == nil {
				recs = []any{}
			}
			out, _ := json.Marshal(recs)
			_, _ = w.Write(out)

		case http.MethodDelete:
			id := strings.TrimPrefix(r.URL.Path, "/api/autonomous/subscriptions/")
			found := false
			for _, e := range store["test:grp1"] {
				if e.id == id {
					found = true
				}
			}
			if found {
				for i := range store["test:grp1"] {
					if store["test:grp1"][i].id == id {
						store["test:grp1"] = append(store["test:grp1"][:i], store["test:grp1"][i+1:]...)
						break
					}
				}
			}
			if !found {
				http.Error(w, "no such subscription", http.StatusNotFound)
				return
			}
			w.WriteHeader(http.StatusNoContent)

		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}))
	idx := strings.LastIndex(srv.URL, ":")
	port := 0
	for i := idx + 1; i < len(srv.URL); i++ {
		port = port*10 + int(srv.URL[i]-'0')
	}
	return port, srv.Close
}

// subsRouter builds a Router talking to the fake daemon on port, as the
// comm channel "test:<groupID>" (backend name "test").
func subsRouter(t *testing.T, groupID string, port int) *Router {
	t.Helper()
	r := &Router{
		hostname: "h",
		groupID:  groupID,
		backend:  &captureBackend{name: "test", capture: func(string) {}},
	}
	r.SetWebPort(port)
	return r
}

func joinReplies(out []string) string { return strings.Join(out, "\n") }

// bareSubsRouter needs no daemon: usage-path tests only.
func bareSubsRouter() *Router {
	return &Router{
		hostname: "h",
		groupID:  "grp1",
		backend:  &captureBackend{name: "test", capture: func(string) {}},
	}
}

// ── identity ───────────────────────────────────────────────────────────────

func TestCommSubIdentity(t *testing.T) {
	cases := []struct {
		name    string
		groupID string
		host    string
		backend bool
		want    string
	}{
		{"backend + group id", "grp1", "h", true, "test:grp1"},
		{"group id only", "grp1", "h", false, "grp1"},
		{"hostname fallback", "", "h", true, "test:h"},
		{"hostname only", "", "h", false, "h"},
	}
	for _, tc := range cases {
		var r Router
		r.hostname = tc.host
		r.groupID = tc.groupID
		if tc.backend {
			r.backend = &captureBackend{name: "test", capture: func(string) {}}
		}
		if got := r.commSubIdentity(); got != tc.want {
			t.Errorf("%s: identity = %q want %q", tc.name, got, tc.want)
		}
	}
}

// ── parse table ────────────────────────────────────────────────────────────

func TestParseCommSubscribe(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		kinds   []string
		prdID   string
		taskID  string
		wantErr string
	}{
		{
			name:  "comma-separated kinds with prd scope",
			args:  []string{"session_started,prd.blocked", "prd=prd_a3f9"},
			kinds: []string{"task.session_started", "prd.blocked"},
			prdID: "prd_a3f9",
		},
		{
			name:  "shorthand words map to task kinds",
			args:  []string{"session_started", "session_cleared"},
			kinds: []string{"task.session_started", "task.session_cleared"},
		},
		{
			name:  "dotted kinds pass through",
			args:  []string{"prd.blocked", "task.completed"},
			kinds: []string{"prd.blocked", "task.completed"},
		},
		{
			name:   "prd and task scope together",
			args:   []string{"prd_updated", "prd=prdx", "task=task_7"},
			kinds:  []string{"task.prd_updated"},
			prdID:  "prdx",
			taskID: "task_7",
		},
		{
			name:    "no kinds",
			args:    []string{"prd=prd_x"},
			wantErr: "at least one event kind",
		},
		{
			name:    "empty prd value",
			args:    []string{"prd.blocked", "prd="},
			wantErr: "prd= has an empty value",
		},
		{
			name:    "repeated prd scope",
			args:    []string{"prd.blocked", "prd=a", "prd=b"},
			wantErr: "prd=<id> is repeated",
		},
		{
			name:    "empty task value",
			args:    []string{"prd.blocked", "task="},
			wantErr: "task= has an empty value",
		},
		{
			name:    "repeated task scope",
			args:    []string{"prd.blocked", "task=a", "task=b"},
			wantErr: "task=<id> is repeated",
		},
		{
			name:    "empty kind token",
			args:    []string{","},
			wantErr: "at least one event kind",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseCommSubscribe(tc.args)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("want error containing %q, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got.kinds) != len(tc.kinds) {
				t.Fatalf("kinds = %v want %v", got.kinds, tc.kinds)
			}
			for i := range tc.kinds {
				if got.kinds[i] != tc.kinds[i] {
					t.Errorf("kinds[%d] = %q want %q", i, got.kinds[i], tc.kinds[i])
				}
			}
			if got.prdID != tc.prdID || got.taskID != tc.taskID {
				t.Errorf("scope = (%q,%q) want (%q,%q)", got.prdID, got.taskID, tc.prdID, tc.taskID)
			}
		})
	}
}

// ── record decode/render ───────────────────────────────────────────────────

func TestCommSubRecordRoundTrip(t *testing.T) {
	body := subsRecordJSON("sub_a", "test:grp1", []string{"task.session_started", "prd.blocked"}, "prd_a3f9", "")
	rec, err := decodeCommSubRecord(body)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if rec.id != "sub_a" || rec.Owner != "test:grp1" || !rec.Coalesce {
		t.Errorf("record = %+v", rec)
	}
	if rec.PRDID != "prd_a3f9" || rec.Target != "test:grp1" || rec.SinkType != "comm" {
		t.Errorf("record = %+v", rec)
	}
	line := renderCommSubLine(rec)
	for _, frag := range []string{
		"sub_a",
		"kinds=task.session_started,prd.blocked",
		"prd=prd_a3f9",
		"sink=comm -> test:grp1",
	} {
		if !strings.Contains(line, frag) {
			t.Errorf("rendered line misses %q: %q", frag, line)
		}
	}

	if !commSubListContainsID(`[{"id":"sub_a"},{"id":"sub_b"}]`, "sub_b") {
		t.Errorf("contains(sub_b) = false want true")
	}
	if commSubListContainsID(`[{"id":"sub_a"}]`, "sub_zz") {
		t.Errorf("contains(sub_zz) = true want false")
	}
	if commSubListContainsID(`not json`, "sub_a") {
		t.Errorf("contains on malformed body = true want false")
	}
	if _, err := decodeCommSubRecord(`{"owner":"x"}`); err == nil {
		t.Errorf("decode of record without id should fail")
	}
}

// ── subscribe: round trip ──────────────────────────────────────────────────

func TestAutonomousSubs_SubscribeRoundTrip(t *testing.T) {
	var reqs []subsDaemonRequest
	port, closeFn := fakeSubsDaemon(t, &reqs)
	defer closeFn()

	r := subsRouter(t, "grp1", port)
	out := r.HandleTestMessage("autonomous subscribe session_started,prd.blocked prd=prd_a3f9")

	if len(reqs) != 1 {
		t.Fatalf("daemon requests = %d want 1 (%v)", len(reqs), reqs)
	}
	q := reqs[0]
	if q.method != http.MethodPost || q.path != "/api/autonomous/subscriptions" {
		t.Fatalf("request = %s %s want POST /api/autonomous/subscriptions", q.method, q.path)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(q.body), &body); err != nil {
		t.Fatalf("body is not valid JSON: %v (body=%q)", err, q.body)
	}
	filter, _ := body["filter"].(map[string]any)
	kinds, _ := filter["kinds"].([]any)
	if len(kinds) != 2 || kinds[0] != "task.session_started" || kinds[1] != "prd.blocked" {
		t.Errorf("filter.kinds = %v want [task.session_started prd.blocked]", kinds)
	}
	if filter["prd_id"] != "prd_a3f9" {
		t.Errorf("filter.prd_id = %v want prd_a3f9", filter["prd_id"])
	}
	sink, _ := body["sink"].(map[string]any)
	if sink["type"] != "comm" || sink["target"] != "test:grp1" {
		t.Errorf("sink = %v want {comm test:grp1}", sink)
	}
	if body["owner"] != "test:grp1" {
		t.Errorf("owner = %v want test:grp1", body["owner"])
	}
	if body["coalesce"] != true {
		t.Errorf("coalesce = %v want true", body["coalesce"])
	}

	joined := joinReplies(out)
	for _, frag := range []string{"subscribed sub_new1", "kinds=task.session_started,prd.blocked", "prd=prd_a3f9", "sink=comm -> test:grp1"} {
		if !strings.Contains(joined, frag) {
			t.Errorf("reply misses %q:\n%s", frag, joined)
		}
	}
}

func TestAutonomousSubs_SubscribeUsageOnBadInput(t *testing.T) {
	r := bareSubsRouter()
	out := r.HandleTestMessage("autonomous subscribe")
	joined := joinReplies(out)
	if !strings.Contains(joined, "usage: autonomous subscribe <kinds|short-names>") {
		t.Errorf("bare subscribe should surface usage, got:\n%s", joined)
	}

	out = r.HandleTestMessage("autonomous subscribe prd=")
	joined = joinReplies(out)
	if !strings.Contains(joined, "prd= has an empty value") {
		t.Errorf("empty prd= should be refused with a hint, got:\n%s", joined)
	}
}

// ── subscriptions (list): owner-scoped ─────────────────────────────────────

func TestAutonomousSubs_ListIsOwnerScoped(t *testing.T) {
	var reqs []subsDaemonRequest
	port, closeFn := fakeSubsDaemon(t, &reqs)
	defer closeFn()

	r := subsRouter(t, "grp1", port)
	out := r.HandleTestMessage("autonomous subscriptions")

	if len(reqs) != 1 {
		t.Fatalf("daemon requests = %d want 1 (%v)", len(reqs), reqs)
	}
	if reqs[0].method != http.MethodGet {
		t.Errorf("method = %s want GET", reqs[0].method)
	}
	owner := reqs[0].rawquery
	if !strings.Contains(owner, "owner=test%3Agrp1") && !strings.Contains(owner, "owner=test:grp1") {
		t.Errorf("query = %q want owner=test:grp1", owner)
	}

	joined := joinReplies(out)
	for _, frag := range []string{"sub_a", "kinds=task.session_started,prd.blocked", "sub_b", "task=task_7"} {
		if !strings.Contains(joined, frag) {
			t.Errorf("list reply misses %q:\n%s", frag, joined)
		}
	}
}

func TestAutonomousSubs_ListEmptyForOtherChannel(t *testing.T) {
	var reqs []subsDaemonRequest
	port, closeFn := fakeSubsDaemon(t, &reqs)
	defer closeFn()

	r := subsRouter(t, "grp0", port) // not in the fake store → []
	out := r.HandleTestMessage("autonomous subscriptions")
	joined := joinReplies(out)
	if !strings.Contains(joined, "none for this channel") {
		t.Errorf("empty list should say so, got:\n%s", joined)
	}
	owner := reqs[len(reqs)-1].rawquery
	if !strings.Contains(owner, "owner=test%3Agrp0") && !strings.Contains(owner, "owner=test:grp0") {
		t.Errorf("query = %q want owner=test:grp0 (each channel lists its own)", owner)
	}
}

// ── unsubscribe ────────────────────────────────────────────────────────────

func TestAutonomousSubs_UnsubscribeOwnSubscription(t *testing.T) {
	var reqs []subsDaemonRequest
	port, closeFn := fakeSubsDaemon(t, &reqs)
	defer closeFn()

	r := subsRouter(t, "grp1", port)
	out := r.HandleTestMessage("autonomous unsubscribe sub_b")

	if len(reqs) != 2 {
		t.Fatalf("daemon requests = %d want 2 (ownership GET then DELETE) (%v)", len(reqs), reqs)
	}
	if reqs[0].method != http.MethodGet {
		t.Errorf("first = %s %s want GET (ownership check)", reqs[0].method, reqs[0].path)
	}
	if reqs[1].method != http.MethodDelete || reqs[1].path != "/api/autonomous/subscriptions/sub_b" {
		t.Errorf("second = %s %s want DELETE /api/autonomous/subscriptions/sub_b", reqs[1].method, reqs[1].path)
	}
	joined := joinReplies(out)
	if !strings.Contains(joined, "deleted sub_b") {
		t.Errorf("reply should confirm the delete, got:\n%s", joined)
	}
}

func TestAutonomousSubs_UnsubscribeRefusesForeignSubscription(t *testing.T) {
	var reqs []subsDaemonRequest
	port, closeFn := fakeSubsDaemon(t, &reqs)
	defer closeFn()

	r := subsRouter(t, "grp1", port)
	out := r.HandleTestMessage("autonomous unsubscribe sub_xo") // another channel's sub

	for _, q := range reqs {
		if q.method == http.MethodDelete {
			t.Fatalf("DELETE went out for a foreign id:\n%v", reqs)
		}
	}
	joined := joinReplies(out)
	if !strings.Contains(joined, "not found") || !strings.Contains(joined, "only delete its own subscriptions") {
		t.Errorf("foreign id should be refused with the ownership note, got:\n%s", joined)
	}
	if len(reqs) != 1 || reqs[0].method != http.MethodGet {
		t.Errorf("expected exactly the ownership GET, got:\n%v", reqs)
	}
}

func TestAutonomousSubs_UnsubscribeUsage(t *testing.T) {
	r := bareSubsRouter()
	out := r.HandleTestMessage("autonomous unsubscribe")
	joined := joinReplies(out)
	if !strings.Contains(joined, "usage: autonomous unsubscribe <id>") {
		t.Errorf("bare unsubscribe should surface usage, got:\n%s", joined)
	}
}

// ── help text ──────────────────────────────────────────────────────────────

func TestAutonomousHelp_ListsSubscriptionVerbs(t *testing.T) {
	r := bareSubsRouter()
	out := r.HandleTestMessage("autonomous") // bare verb → usage/help
	joined := joinReplies(out)
	for _, frag := range []string{
		"subscribe <kinds|short-names> [prd=<id>] [task=<id>]",
		"unsubscribe <id>",
		"subscriptions}",
	} {
		if !strings.Contains(joined, frag) {
			t.Errorf("help text misses %q:\n%s", frag, joined)
		}
	}
}

// Parse-level guard for the new verbs (keeps parse regressions visible
// next to the round-trip tests rather than only in autonomous_test.go).
func TestParse_AutonomousSubscriptionVerbs(t *testing.T) {
	cases := []struct {
		in       string
		wantText string
	}{
		{"autonomous subscribe session_started,prd.blocked prd=prd_a3f9", "subscribe session_started,prd.blocked prd=prd_a3f9"},
		{"autonomous subscribe task=task_7 prd_updated", "subscribe task=task_7 prd_updated"},
		{"autonomous unsubscribe sub_a", "unsubscribe sub_a"},
		{"autonomous subscriptions", "subscriptions"},
		{"prd subscribe session_started", "subscribe session_started"}, // prd is an alias for autonomous
	}
	for _, tc := range cases {
		got := Parse(tc.in)
		if got.Type != CmdAutonomous {
			t.Errorf("%q: type = %v want CmdAutonomous", tc.in, got.Type)
			continue
		}
		if got.Text != tc.wantText {
			t.Errorf("%q: text = %q want %q", tc.in, got.Text, tc.wantText)
		}
	}
}
