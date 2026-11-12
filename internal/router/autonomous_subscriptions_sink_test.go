// Tests for the webhook and peer sinks on the comm verbs: "autonomous
// subscribe" and "autonomous subscription-update" accept sink=, target= and
// secret_ref=, keep the owner scope, and refuse the combinations the daemon
// refuses before a request is sent. The fake daemon serves owner-tagged
// records and echoes POST and PUT bodies, so each reply shows what was sent.

package router

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// sinkDaemon is a fake daemon with the given owner-tagged records. GET serves
// the owner-scoped list; POST and PUT echo the request body back with an id.
func sinkDaemon(t *testing.T, reqs *[]subsDaemonRequest, records []map[string]any) int {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		*reqs = append(*reqs, subsDaemonRequest{method: r.Method, path: r.URL.Path, rawquery: r.URL.RawQuery, body: string(b)})
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			owner := r.URL.Query().Get("owner")
			out := []any{}
			for _, rc := range records {
				if rc["owner"] == owner {
					out = append(out, rc)
				}
			}
			j, _ := json.Marshal(out)
			_, _ = w.Write(j)
		case http.MethodPost, http.MethodPut:
			var rec map[string]any
			_ = json.Unmarshal(b, &rec)
			if r.Method == http.MethodPost {
				rec["id"] = "sub_new_hook"
			} else {
				rec["id"] = strings.TrimPrefix(r.URL.Path, commSubPath+"/")
			}
			j, _ := json.Marshal(rec)
			_, _ = w.Write(j)
		default:
			http.Error(w, "unexpected "+r.Method, http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	return port
}

// sinkRecords are the channel's own webhook subscription and a foreign one.
func sinkRecords() []map[string]any {
	return []map[string]any{
		{
			"id": "sub_hook", "owner": "test:grp1",
			"filter":   map[string]any{"kinds": []string{"task.completed"}},
			"sink":     map[string]any{"type": "webhook", "target": "https://hooks.example.net/x", "secret_ref": "${secret:lifecycle-hook}"},
			"coalesce": false,
		},
		{
			"id": "sub_foreign_hook", "owner": "signal:$someoneelse",
			"filter": map[string]any{"kinds": []string{"task.completed"}},
			"sink":   map[string]any{"type": "webhook", "target": "https://hooks.example.net/y", "secret_ref": "${secret:other}"},
		},
	}
}

// sentSink decodes the sink object of the request body.
func sentSink(t *testing.T, raw string) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		t.Fatalf("body is not JSON: %v (%q)", err, raw)
	}
	sink, ok := body["sink"].(map[string]any)
	if !ok {
		t.Fatalf("body has no sink object: %s", raw)
	}
	return sink
}

func TestCommSubscribeWebhookSendsSinkAndReference(t *testing.T) {
	var reqs []subsDaemonRequest
	port := sinkDaemon(t, &reqs, nil)
	r := subsRouter(t, "grp1", port)

	out := joinReplies(r.HandleTestMessage("autonomous subscribe task.completed sink=webhook target=https://hooks.example.net/x secret_ref=${secret:lifecycle-hook}"))
	if len(reqs) != 1 || reqs[0].method != http.MethodPost || reqs[0].path != commSubPath {
		t.Fatalf("requests = %+v, want one POST", reqs)
	}
	sink := sentSink(t, reqs[0].body)
	if sink["type"] != "webhook" || sink["target"] != "https://hooks.example.net/x" || sink["secret_ref"] != "${secret:lifecycle-hook}" {
		t.Errorf("sink = %v, want the webhook with its reference", sink)
	}
	var body map[string]any
	_ = json.Unmarshal([]byte(reqs[0].body), &body)
	if body["owner"] != "test:grp1" || body["coalesce"] != false {
		t.Errorf("owner/coalesce = %v/%v, want the channel owner and no coalescing for a webhook", body["owner"], body["coalesce"])
	}
	for _, frag := range []string{"subscribed sub_new_hook", "sink=webhook -> https://hooks.example.net/x", "secret_ref=${secret:lifecycle-hook}"} {
		if !strings.Contains(out, frag) {
			t.Errorf("reply misses %q:\n%s", frag, out)
		}
	}
}

func TestCommSubscribePeerSendsNoReference(t *testing.T) {
	var reqs []subsDaemonRequest
	port := sinkDaemon(t, &reqs, nil)
	r := subsRouter(t, "grp1", port)

	r.HandleTestMessage("autonomous subscribe prd.blocked sink=peer target=laptop")
	if len(reqs) != 1 {
		t.Fatalf("requests = %d, want 1", len(reqs))
	}
	sink := sentSink(t, reqs[0].body)
	if sink["type"] != "peer" || sink["target"] != "laptop" {
		t.Errorf("sink = %v, want peer laptop", sink)
	}
	if _, present := sink["secret_ref"]; present {
		t.Errorf("peer sink carried a secret_ref: %v", sink)
	}
}

func TestCommSubscribeRefusesBeforeSending(t *testing.T) {
	cases := []struct {
		name string
		text string
		want string
	}{
		{"session sink is not open to a channel", "autonomous subscribe prd.blocked sink=session target=ops", "sink must be comm, webhook or peer"},
		{"comm with a foreign target", "autonomous subscribe prd.blocked sink=comm target=other:chan", "always delivers to this channel"},
		{"webhook without a reference", "autonomous subscribe prd.blocked sink=webhook target=https://hooks.example.net/x", "needs secret_ref"},
		{"webhook without a target", "autonomous subscribe prd.blocked sink=webhook secret_ref=${secret:x}", "needs target"},
		{"reference on a peer", "autonomous subscribe prd.blocked sink=peer target=laptop secret_ref=${secret:x}", "only valid with sink=webhook"},
		{"peer without a name", "autonomous subscribe prd.blocked sink=peer", "needs target"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var reqs []subsDaemonRequest
			port := sinkDaemon(t, &reqs, nil)
			r := subsRouter(t, "grp1", port)
			out := joinReplies(r.HandleTestMessage(tc.text))
			if !strings.Contains(out, tc.want) {
				t.Errorf("reply = %q, want it to contain %q", out, tc.want)
			}
			if len(reqs) != 0 {
				t.Errorf("daemon was called for invalid input: %+v", reqs)
			}
		})
	}
}

func TestCommUpdateKeepsWebhookSinkAndReference(t *testing.T) {
	var reqs []subsDaemonRequest
	port := sinkDaemon(t, &reqs, sinkRecords())
	r := subsRouter(t, "grp1", port)

	out := joinReplies(r.HandleTestMessage("autonomous subscription-update sub_hook kinds=prd.blocked"))
	if len(reqs) != 2 || reqs[1].method != http.MethodPut {
		t.Fatalf("requests = %+v, want an owner GET then a PUT", reqs)
	}
	sink := sentSink(t, reqs[1].body)
	if sink["type"] != "webhook" || sink["target"] != "https://hooks.example.net/x" || sink["secret_ref"] != "${secret:lifecycle-hook}" {
		t.Errorf("sink = %v, want the webhook and its reference carried over", sink)
	}
	if !strings.Contains(out, "updated sub_hook") {
		t.Errorf("reply = %q", out)
	}
}

func TestCommUpdateSwitchesSinkAndDropsReference(t *testing.T) {
	var reqs []subsDaemonRequest
	port := sinkDaemon(t, &reqs, sinkRecords())
	r := subsRouter(t, "grp1", port)

	r.HandleTestMessage("autonomous subscription-update sub_hook sink=peer target=laptop")
	if len(reqs) != 2 {
		t.Fatalf("requests = %+v", reqs)
	}
	sink := sentSink(t, reqs[1].body)
	if sink["type"] != "peer" || sink["target"] != "laptop" {
		t.Errorf("sink = %v, want peer laptop", sink)
	}
	if _, present := sink["secret_ref"]; present {
		t.Errorf("reference kept on a peer sink: %v", sink)
	}
}

func TestCommUpdateBackToCommPinsChannel(t *testing.T) {
	var reqs []subsDaemonRequest
	port := sinkDaemon(t, &reqs, sinkRecords())
	r := subsRouter(t, "grp1", port)

	r.HandleTestMessage("autonomous subscription-update sub_hook sink=comm")
	if len(reqs) != 2 {
		t.Fatalf("requests = %+v", reqs)
	}
	sink := sentSink(t, reqs[1].body)
	if sink["type"] != "comm" || sink["target"] != "test:grp1" {
		t.Errorf("sink = %v, want the channel's own comm sink", sink)
	}
	if _, present := sink["secret_ref"]; present {
		t.Errorf("reference kept on a comm sink: %v", sink)
	}
}

func TestCommUpdateSinkRulesRefuseBeforeSending(t *testing.T) {
	cases := []struct {
		name string
		text string
		want string
	}{
		{"new peer needs a name", "autonomous subscription-update sub_hook sink=peer", "needs target"},
		{"reference on a peer", "autonomous subscription-update sub_hook sink=peer target=laptop secret_ref=${secret:x}", "only valid with sink=webhook"},
		{"comm with a foreign target", "autonomous subscription-update sub_hook sink=comm target=other:chan", "always delivers to this channel"},
		{"sink outside the channel set", "autonomous subscription-update sub_hook sink=session", "sink must be comm, webhook or peer"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var reqs []subsDaemonRequest
			port := sinkDaemon(t, &reqs, sinkRecords())
			r := subsRouter(t, "grp1", port)
			out := joinReplies(r.HandleTestMessage(tc.text))
			if !strings.Contains(out, tc.want) {
				t.Errorf("reply = %q, want it to contain %q", out, tc.want)
			}
			for _, rq := range reqs {
				if rq.method == http.MethodPut {
					t.Errorf("a PUT was sent for invalid input: %+v", rq)
				}
			}
		})
	}
}

func TestCommUpdateRefusesAnotherChannelsWebhook(t *testing.T) {
	var reqs []subsDaemonRequest
	port := sinkDaemon(t, &reqs, sinkRecords())
	r := subsRouter(t, "grp1", port)

	out := joinReplies(r.HandleTestMessage("autonomous subscription-update sub_foreign_hook sink=peer target=laptop"))
	if !strings.Contains(out, "not found") {
		t.Errorf("reply = %q, want the same not-found answer as an unknown id", out)
	}
	for _, rq := range reqs {
		if rq.method == http.MethodPut {
			t.Errorf("a PUT reached another channel's subscription: %+v", rq)
		}
	}
}

func TestCommListShowsReferenceNotValue(t *testing.T) {
	var reqs []subsDaemonRequest
	port := sinkDaemon(t, &reqs, sinkRecords())
	r := subsRouter(t, "grp1", port)

	out := joinReplies(r.HandleTestMessage("autonomous subscriptions"))
	if !strings.Contains(out, "sink=webhook -> https://hooks.example.net/x") || !strings.Contains(out, "secret_ref=${secret:lifecycle-hook}") {
		t.Errorf("list = %q, want the webhook sink and its reference", out)
	}
	if strings.Contains(out, "hooks.example.net/y") {
		t.Errorf("list leaked another channel's record: %q", out)
	}
}
