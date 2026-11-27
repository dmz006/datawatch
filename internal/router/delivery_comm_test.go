package router

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/dmz006/datawatch/internal/delivery"
	"github.com/dmz006/datawatch/internal/federation"
)

func TestParseDelivery(t *testing.T) {
	cases := map[string]struct{ verb, rest string }{
		"delivery":                {"status", ""},
		"Delivery":                {"status", ""},
		"delivery status":         {"status", ""},
		"DELIVERY CHECK":          {"check", ""},
		"delivery ack backlog":    {"ack", "backlog"},
		"delivery Ack Task:T1:Up": {"ack", "Task:T1:Up"},
		"delivery accept kind=bug reason=parked until=2026-12-01": {"accept", "kind=bug reason=parked until=2026-12-01"},
		"delivery drop task:t1:failed reason=obsolete":            {"drop", "task:t1:failed reason=obsolete"},
		"delivery test":  {"test", ""},
		"delivery chek":  {"chek", ""},
		"delivery  test": {"test", ""},
	}
	for in, want := range cases {
		c := Parse(in)
		if c.Type != CmdDelivery || c.DeliveryVerb != want.verb || c.Text != want.rest {
			t.Errorf("Parse(%q) = type %s verb %q rest %q", in, c.Type, c.DeliveryVerb, c.Text)
		}
	}
	for _, in := range []string{"deliveryx", "delivery_check", "deliveries"} {
		if c := Parse(in); c.Type == CmdDelivery {
			t.Errorf("Parse(%q) captured", in)
		}
	}
}

type deliveryCommHit struct {
	method, path, caller, auth string
	body                       map[string]any
}

type deliveryCommREST struct {
	mu     sync.Mutex
	hits   []deliveryCommHit
	status int
	reply  map[string]string
}

func (f *deliveryCommREST) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	hit := deliveryCommHit{method: r.Method, path: r.URL.RequestURI(),
		caller: r.Header.Get(federation.HeaderDeliveryCaller), auth: r.Header.Get("Authorization")}
	if data, _ := io.ReadAll(r.Body); len(data) > 0 {
		_ = json.Unmarshal(data, &hit.body)
	}
	f.hits = append(f.hits, hit)
	if f.status != 0 {
		http.Error(w, f.reply[""], f.status)
		return
	}
	_, _ = io.WriteString(w, f.reply[r.URL.Path])
}

func newDeliveryRouter(t *testing.T) (*Router, *deliveryCommREST) {
	t.Helper()
	report, err := json.Marshal(delivery.Report{ReportOnly: true, Result: delivery.ResultRed, Pass: 4,
		Red:   []string{delivery.RedItems},
		Items: []delivery.Entry{{Key: "task:t1:failed", Kind: delivery.KindTask, ID: "t1", State: "failed", Invariant: "I3", Reason: "failed"}}})
	if err != nil {
		t.Fatal(err)
	}
	f := &deliveryCommREST{reply: map[string]string{
		"/api/delivery":              string(report),
		"/api/delivery/check":        string(report),
		"/api/delivery/ack":          `{"changed":2}`,
		"/api/delivery/accept":       `{"changed":1}`,
		"/api/delivery/drop":         `{"changed":3}`,
		"/api/delivery/test-message": `{"results":[{"channel":"signal","ok":true},{"channel":"ntfy","ok":false,"code":"send_failed"}]}`,
		"/api/config":                `{"delivery":{"enabled":true,"notify":false,"interval_seconds":600}}`,
	}}
	ts := httptest.NewServer(f)
	t.Cleanup(ts.Close)
	u, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	r := &Router{hostname: "h", webPort: port, webToken: "admin-tok"}
	return r, f
}

func deliverySay(r *Router, text, caller string) string {
	return strings.Join(r.HandleAttributedTestMessage(text, ConfigureAttribution{Caller: caller}), "\n")
}

func TestDeliveryChatCommands(t *testing.T) {
	cases := []struct {
		say, method, path string
		body              map[string]any
		replyHas          []string
	}{
		{"delivery", "GET", "/api/delivery", nil, []string{"Delivery check: red", "task:t1:failed", "report only"}},
		{"delivery check", "POST", "/api/delivery/check", nil, []string{"Pass 4", "task:t1:failed"}},
		{"delivery ack task:t1:failed bug:b1:open", "POST", "/api/delivery/ack",
			map[string]any{"keys": []any{"task:t1:failed", "bug:b1:open"}, "actor": "channel"}, []string{"2", "seen"}},
		{"delivery ack BACKLOG", "POST", "/api/delivery/ack",
			map[string]any{"backlog": true, "actor": "channel"}, []string{"2"}},
		{"delivery accept kind=automaton state=held match_reason=no_date reason=parked until=2026-12-01T10:00:00Z", "POST", "/api/delivery/accept",
			map[string]any{"kind": "automaton", "state": "held", "match_reason": "no_date", "reason": "parked", "until": "2026-12-01T10:00:00Z", "actor": "channel"},
			[]string{"1", "accepted", "2026-12-01T10:00:00Z"}},
		{"delivery drop alarm:flood:open reason=obsolete", "POST", "/api/delivery/drop",
			map[string]any{"keys": []any{"alarm:flood:open"}, "reason": "obsolete", "actor": "channel"}, []string{"3", "dropped"}},
		{"delivery test", "POST", "/api/delivery/test-message", nil, []string{"signal: sent", "ntfy: failed (send_failed)"}},
	}
	for _, c := range cases {
		r, f := newDeliveryRouter(t)
		reply := deliverySay(r, c.say, "")
		if len(f.hits) != 1 {
			t.Errorf("%q: %d calls, reply %q", c.say, len(f.hits), reply)
			continue
		}
		hit := f.hits[0]
		if hit.method != c.method || hit.path != c.path || hit.auth != "Bearer admin-tok" || hit.caller != "" {
			t.Errorf("%q: %s %s auth=%q caller=%q", c.say, hit.method, hit.path, hit.auth, hit.caller)
		}
		if !reflect.DeepEqual(hit.body, c.body) {
			t.Errorf("%q: body = %v, want %v", c.say, hit.body, c.body)
		}
		for _, want := range c.replyHas {
			if !strings.Contains(reply, want) {
				t.Errorf("%q: reply lacks %q: %q", c.say, want, reply)
			}
		}
	}
}

// A word the command does not know is refused: dropping it would widen what the
// action applies to, and nothing is sent.
func TestDeliveryChatRefusals(t *testing.T) {
	for _, say := range []string{
		"delivery chek",
		"delivery status now",
		"delivery ack",
		"delivery ack backlog reason=x",
		"delivery ack kind=task stat=failed",
		"delivery ack task t1",
		"delivery accept backlog reason=parked",
		"delivery accept backlog until=2026-12-01",
		"delivery accept backlog reason=a reason=b until=2026-12-01",
		"delivery accept backlog because it is parked reason=parked until=2026-12-01",
		"delivery accept backlog reason=parked untill=2026-12-01T10:00:00Z until=2026-12-01",
		"delivery drop backlog",
		"delivery drop reason=obsolete",
		"delivery drop backlog reason=obsolete until=2026-12-01",
		"delivery drop backlog reason=",
		"delivery test now",
	} {
		r, f := newDeliveryRouter(t)
		reply := deliverySay(r, say, "")
		if len(f.hits) != 0 {
			t.Errorf("%q reached the daemon: %+v", say, f.hits)
		}
		if !strings.Contains(reply, "usage: delivery") {
			t.Errorf("%q: reply has no usage: %q", say, reply)
		}
	}
}

func TestDeliveryChatDaemonRefusal(t *testing.T) {
	r, f := newDeliveryRouter(t)
	f.status, f.reply[""] = http.StatusConflict, "only failed or backlog entries can be dropped"
	reply := deliverySay(r, "delivery drop backlog reason=obsolete", "")
	if !strings.Contains(reply, "delivery drop failed") || !strings.Contains(reply, "only failed or backlog entries can be dropped") {
		t.Fatalf("reply = %q", reply)
	}
}

// A chat message sent with a session or peer credential is recorded under that
// credential, through the delivery commands and the raw rest command alike.
func TestDeliveryChatNamesTheSessionOrPeer(t *testing.T) {
	for _, caller := range []string{"session:host-1a2b", "peer:desk"} {
		for _, say := range []string{
			"delivery", "delivery check", "delivery ack backlog",
			"delivery accept backlog reason=parked until=2026-12-01",
			"delivery drop backlog reason=obsolete", "delivery test",
			`rest POST /api/delivery/drop {"backlog":true,"reason":"obsolete"}`,
			`rest POST /api/sessions/../delivery/ack {"backlog":true}`,
		} {
			r, f := newDeliveryRouter(t)
			deliverySay(r, say, caller)
			if len(f.hits) != 1 || f.hits[0].caller != caller {
				t.Errorf("%s %q: hits=%+v", caller, say, f.hits)
			}
		}
	}
	r, f := newDeliveryRouter(t)
	deliverySay(r, `rest POST /api/delivery/ack {"backlog":true}`, "")
	if len(f.hits) != 1 || f.hits[0].caller != "" {
		t.Errorf("operator rest: %+v", f.hits)
	}
}

func TestDeliveryChatSettings(t *testing.T) {
	r, _ := newDeliveryRouter(t)
	var set []string
	r.SetConfigureFunc(func(k, v string) error { set = append(set, k+"="+v); return nil })
	for _, key := range []string{"delivery.enabled=false", "delivery.notify=true", "delivery.interval_seconds=600"} {
		deliverySay(r, "configure "+key, "")
	}
	if !reflect.DeepEqual(set, []string{"delivery.enabled=false", "delivery.notify=true", "delivery.interval_seconds=600"}) {
		t.Fatalf("set = %v", set)
	}
	// A bare key, or "delivery", reads the effective values back.
	for key, want := range map[string]string{
		"delivery":                  "delivery.enabled = true",
		"delivery.enabled":          "delivery.enabled = true",
		"delivery.notify":           "delivery.notify = false",
		"delivery.interval_seconds": "delivery.interval_seconds = 600",
	} {
		reply := deliverySay(r, "configure "+key, "")
		if !strings.Contains(reply, want) {
			t.Errorf("configure %s: %q lacks %q", key, reply, want)
		}
	}
	if len(set) != 3 {
		t.Errorf("a read changed a setting: %v", set)
	}
	for _, text := range []string{"configure help", "configure list"} {
		reply := deliverySay(r, text, "")
		for _, key := range []string{"delivery.enabled", "delivery.notify", "delivery.interval_seconds"} {
			if !strings.Contains(reply, key) {
				t.Errorf("%s does not list %s", text, key)
			}
		}
	}
}

func TestDeliveryHelpText(t *testing.T) {
	help := HelpText("h")
	for _, want := range []string{"delivery check", "delivery ack", "delivery accept", "delivery drop", "delivery test", "reason=<code> until=<date>"} {
		if !strings.Contains(help, want) {
			t.Errorf("help lacks %q", want)
		}
	}
}
