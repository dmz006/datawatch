package router

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/alerts"
	"github.com/dmz006/datawatch/internal/config"
)

const pendingItemCommSample = `{
  "policy": "hold",
  "auto_resume": true,
  "source": {"configured": true, "path": "registry/work.csv"},
  "last_refresh": {"configured": true, "at": "2026-11-02T08:30:00Z", "rows": 4, "added": 2, "updated": 1, "done": 0, "dropped": 0, "kept": 0, "skipped": 1, "error": ""},
  "items": [
    {"id": "aaaa1111", "ref": "DOC-7", "title": "Write the guide", "state": "waiting", "source": "file", "automaton_id": "",
     "automations": [{"automation_id": "auto0001", "name": "Docs run", "member_id": "m2", "held": true}]},
    {"id": "bbbb2222", "ref": "", "title": "Fix login", "state": "attached", "source": "internal", "automaton_id": "feed0001", "automations": []}
  ],
  "held": [
    {"automation_id": "auto0001", "name": "Docs run", "member_id": "m2", "item_id": "aaaa1111", "title": "Write the guide", "ref": "DOC-7",
     "since": "2026-11-01T09:00:00Z", "reason": "waiting for an Automaton to be attached"}
  ]
}`

func TestPendingItemCommList(t *testing.T) {
	log, port := laneFillCommDaemon(t, http.StatusOK, pendingItemCommSample)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous pending-items"), "\n")
	if log.path != "/api/autonomous/pending-items" || log.query != "" {
		t.Errorf("path=%s query=%q, want the bare list route", log.path, log.query)
	}
	for _, want := range []string{
		"Pending items (policy: hold, auto-resume: on):",
		"aaaa1111  waiting   Write the guide [ref DOC-7] (file)",
		`held: Automation "Docs run" (auto0001) waits since 2026-11-01 09:00 UTC`,
		"bbbb2222  attached  Fix login",
		"Automaton feed0001 attached",
		"File registry: registry/work.csv",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("reply lacks %q:\n%s", want, got)
		}
	}
}

func TestPendingItemCommListFilters(t *testing.T) {
	cases := []struct {
		msg, query string
	}{
		{"autonomous pending-items waiting", "state=waiting"},
		{"autonomous pending-items DONE", "state=done"},
		{"autonomous pending-items auto0001", "automation_id=auto0001"},
		{"autonomous pending-items attached auto0001", "automation_id=auto0001&state=attached"},
		{"autonomous pending_items waiting", "state=waiting"},
	}
	for _, tc := range cases {
		log, port := laneFillCommDaemon(t, http.StatusOK, pendingItemCommSample)
		r := setupPeerRouter(t, port)
		r.HandleTestMessage(tc.msg)
		if log.path != "/api/autonomous/pending-items" || log.query != tc.query {
			t.Errorf("%q: path=%s query=%q, want query %q", tc.msg, log.path, log.query, tc.query)
		}
	}
}

func TestPendingItemCommListCapsAtTen(t *testing.T) {
	var items []string
	for i := 0; i < 13; i++ {
		items = append(items, `{"id":"i`+string(rune('a'+i))+`000000","title":"Item `+string(rune('a'+i))+`","state":"waiting","source":"internal","automations":[]}`)
	}
	body := `{"policy":"hold","auto_resume":true,"source":{"configured":false},"last_refresh":{},"items":[` + strings.Join(items, ",") + `],"held":[]}`
	_, port := laneFillCommDaemon(t, http.StatusOK, body)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous pending-items"), "\n")
	if n := strings.Count(got, "waiting   Item"); n != 10 {
		t.Errorf("want 10 item lines, got %d:\n%s", n, got)
	}
	if !strings.Contains(got, "and 3 more") {
		t.Errorf("reply must count the cut items:\n%s", got)
	}
}

func TestPendingItemCommUsage(t *testing.T) {
	log, port := laneFillCommDaemon(t, http.StatusOK, pendingItemCommSample)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous pending-items waiting a1 extra"), " ")
	if !strings.Contains(got, pendingItemsUsage) || log.calls != 0 {
		t.Errorf("too many args must print usage and send nothing; calls=%d reply=%q", log.calls, got)
	}
	got = strings.Join(r.HandleTestMessage("autonomous pending-items waiting done"), " ")
	if !strings.Contains(got, pendingItemsUsage) || log.calls != 0 {
		t.Errorf("two state words must print usage; calls=%d reply=%q", log.calls, got)
	}

	_, port = laneFillCommDaemon(t, http.StatusInternalServerError, "boom")
	r = setupPeerRouter(t, port)
	got = strings.Join(r.HandleTestMessage("autonomous pending-items"), " ")
	if !strings.Contains(got, "autonomous pending-items failed") {
		t.Errorf("a refused read must say it failed: %q", got)
	}
}

func TestPendingItemCommAdd(t *testing.T) {
	log, port := laneFillCommDaemon(t, http.StatusOK, `{"id":"cccc3333","ref":"DOC-9","title":"Write the guide now","state":"waiting","source":"internal"}`)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous pending-item-add Write the guide now ref=DOC-9"), " ")
	if log.path != "/api/autonomous/pending-items" {
		t.Errorf("path=%s", log.path)
	}
	if !strings.Contains(log.body, `"title":"Write the guide now"`) || !strings.Contains(log.body, `"ref":"DOC-9"`) {
		t.Errorf("body=%s, want the title with its words joined and the ref", log.body)
	}
	if !strings.Contains(got, "Recorded pending item cccc3333 (waiting): Write the guide now [ref DOC-9]") {
		t.Errorf("reply=%q", got)
	}

	log, port = laneFillCommDaemon(t, http.StatusOK, `{"id":"dddd4444","title":"Plain","state":"waiting","source":"internal"}`)
	r = setupPeerRouter(t, port)
	r.HandleTestMessage("autonomous pending-item-add Plain")
	if strings.Contains(log.body, "ref") {
		t.Errorf("no ref given, body=%s must not carry one", log.body)
	}
}

func TestPendingItemCommAddNeedsTitle(t *testing.T) {
	log, port := laneFillCommDaemon(t, http.StatusOK, `{}`)
	r := setupPeerRouter(t, port)
	for _, msg := range []string{"autonomous pending-item-add", "autonomous pending-item-add ref=X"} {
		got := strings.Join(r.HandleTestMessage(msg), " ")
		if !strings.Contains(got, pendingItemAddUsage) {
			t.Errorf("%q: reply=%q, want the usage", msg, got)
		}
	}
	if log.calls != 0 {
		t.Errorf("a blank title must send nothing, calls=%d", log.calls)
	}
}

func TestPendingItemCommAttach(t *testing.T) {
	body := `{"item":{"id":"aaaa1111","title":"Write the guide","state":"attached","automaton_id":"feed0001"},
	  "converted_automations":[{"automation_id":"auto0001","member_ids":["m2"],"pending_insert_ids":[]}],
	  "resumed_automations":["auto0001"],"notes":["one note"]}`
	log, port := laneFillCommDaemon(t, http.StatusOK, body)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous pending-item-attach aaaa1111 feed0001"), "\n")
	if log.path != "/api/autonomous/pending-items/aaaa1111/attach" {
		t.Errorf("path=%s", log.path)
	}
	if !strings.Contains(log.body, `"automaton_id":"feed0001"`) {
		t.Errorf("body=%s", log.body)
	}
	for _, want := range []string{
		"Attached Automaton feed0001 to pending item aaaa1111 (attached): Write the guide",
		"converted in Automation auto0001: members m2",
		"resumed Automation auto0001",
		"note: one note",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("reply lacks %q:\n%s", want, got)
		}
	}
}

func TestPendingItemCommAttachUsageAndRefusal(t *testing.T) {
	log, port := laneFillCommDaemon(t, http.StatusOK, `{}`)
	r := setupPeerRouter(t, port)
	for _, msg := range []string{
		"autonomous pending-item-attach",
		"autonomous pending-item-attach aaaa1111",
		"autonomous pending-item-attach aaaa1111 feed0001 extra",
	} {
		got := strings.Join(r.HandleTestMessage(msg), " ")
		if !strings.Contains(got, pendingItemAttachUsage) {
			t.Errorf("%q: reply=%q, want the usage", msg, got)
		}
	}
	got := strings.Join(r.HandleTestMessage("autonomous pending-item-attach a/../b feed0001"), " ")
	if !strings.Contains(got, "autonomous pending-item-attach failed") || log.calls != 0 {
		t.Errorf("an id with a path character must be refused before sending; calls=%d reply=%q", log.calls, got)
	}

	_, port = laneFillCommDaemon(t, http.StatusConflict, "409: pending item is not waiting")
	r = setupPeerRouter(t, port)
	got = strings.Join(r.HandleTestMessage("autonomous pending-item-attach aaaa1111 feed0001"), " ")
	if !strings.Contains(got, "autonomous pending-item-attach failed") || !strings.Contains(got, "not waiting") {
		t.Errorf("a refused attach must say why: %q", got)
	}
}

func TestPendingItemCommRefresh(t *testing.T) {
	log, port := laneFillCommDaemon(t, http.StatusOK, `{"configured":true,"at":"2026-11-02T08:30:00Z","rows":4,"added":2,"updated":1,"done":0,"dropped":0,"kept":0,"skipped":1,"error":""}`)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous pending-items-refresh"), " ")
	if log.path != "/api/autonomous/pending-items/refresh" {
		t.Errorf("path=%s", log.path)
	}
	if !strings.Contains(got, "Refreshed 2026-11-02 08:30 UTC: 4 rows, 2 added, 1 updated") {
		t.Errorf("reply=%q", got)
	}

	_, port = laneFillCommDaemon(t, http.StatusOK, `{"configured":false}`)
	r = setupPeerRouter(t, port)
	got = strings.Join(r.HandleTestMessage("autonomous pending-items-refresh"), " ")
	if !strings.Contains(got, "No pending item file registry is configured") {
		t.Errorf("unconfigured reply=%q", got)
	}

	_, port = laneFillCommDaemon(t, http.StatusOK, `{"configured":true,"at":"2026-11-02T08:30:00Z","error":"read registry: no such file"}`)
	r = setupPeerRouter(t, port)
	got = strings.Join(r.HandleTestMessage("autonomous pending-items-refresh"), " ")
	if !strings.Contains(got, "autonomous pending-items-refresh failed") || !strings.Contains(got, "no such file") {
		t.Errorf("a refresh that carries an error must say it failed: %q", got)
	}

	got = strings.Join(r.HandleTestMessage("autonomous pending-items-refresh now"), " ")
	if !strings.Contains(got, pendingItemsRefreshHelp) {
		t.Errorf("extra args must print usage: %q", got)
	}
}

func TestPendingItemCommParityListHasVerbs(t *testing.T) {
	verbs := PendingItemCommVerbs()
	for _, want := range []string{"pending-items", "pending-item-add", "pending-item-attach", "pending-items-refresh"} {
		if !slices.Contains(verbs, want) {
			t.Fatalf("PendingItemCommVerbs() = %v, must contain %s", verbs, want)
		}
	}
	r := setupPeerRouter(t, 0)
	help := strings.Join(r.HandleTestMessage("autonomous"), " ")
	for _, verb := range verbs {
		if c := Parse("autonomous " + verb); c.Type != CmdAutonomous {
			t.Errorf(`Parse("autonomous %s").Type = %v, want CmdAutonomous`, verb, c.Type)
		}
		if !strings.Contains(help, "|"+verb+" ") && !strings.Contains(help, "|"+verb+"|") {
			t.Errorf("the autonomous help must list %s", verb)
		}
	}
}

// pendingItemConfigKeys are the nine pending item settings with a valid value
// for each, as the operator would type them after "configure".
var pendingItemConfigKeys = []struct{ key, value string }{
	{config.PendingItemKeyPolicy, "skip"},
	{config.PendingItemKeyAutoResume, "false"},
	{config.PendingItemKeyPath, "registry/work.csv"},
	{config.PendingItemKeyIDPattern, `^[A-Z]+-\d+$`},
	{config.PendingItemKeyIDColumn, "Item"},
	{config.PendingItemKeyTitleColumn, "3"},
	{config.PendingItemKeyStateColumn, "Status"},
	{config.PendingItemKeyDoneValues, "done,closed,shipped,released"},
	{config.PendingItemKeyOnMissingRow, "drop"},
}

func TestPendingItemCommConfigureKey(t *testing.T) {
	for _, tc := range pendingItemConfigKeys {
		var gotKey, gotValue string
		sent := runConfigureCapture(t, func(key, value string) error {
			gotKey, gotValue = key, value
			return nil
		}, tc.key+"="+tc.value)
		if gotKey != tc.key || gotValue != tc.value {
			t.Errorf("forwarded %q=%q, want %q=%q", gotKey, gotValue, tc.key, tc.value)
		}
		if len(sent) != 1 || !strings.Contains(sent[0], "Set "+tc.key) {
			t.Errorf("%s: reply = %q, want a confirmation naming the key", tc.key, sent)
		}
	}
	for _, in := range []string{"configure help", "configure list"} {
		r := &Router{hostname: "h", backend: &captureBackend{name: "test", capture: func(string) {}}}
		r.SetConfigureFunc(func(string, string) error { return nil })
		resp := strings.Join(r.HandleTestMessage(in), "\n")
		for _, tc := range pendingItemConfigKeys {
			if !strings.Contains(resp, tc.key) {
				t.Errorf("%q output lacks %s", in, tc.key)
			}
		}
	}
}

func TestPendingItemCommConfigureInvalid(t *testing.T) {
	cases := []struct{ key, value, allowed string }{
		{config.PendingItemKeyPolicy, "pause", `"hold" or "skip"`},
		{config.PendingItemKeyAutoResume, "maybe", `"true" or "false"`},
		{config.PendingItemKeyOnMissingRow, "forget", `"keep" or "drop"`},
		{config.PendingItemKeyIDPattern, "([", config.PendingItemKeyIDPattern},
	}
	for _, tc := range cases {
		reached := false
		sent := runConfigureCapture(t, func(string, string) error {
			reached = true
			return nil
		}, tc.key+"="+tc.value)
		if reached {
			t.Errorf("%s=%s reached the daemon", tc.key, tc.value)
		}
		if len(sent) != 1 || !strings.Contains(sent[0], "Configure failed") || !strings.Contains(sent[0], tc.allowed) {
			t.Errorf("%s=%s: reply = %q, want Configure failed naming %s", tc.key, tc.value, sent, tc.allowed)
		}
	}
	// A value the daemon refuses is still reported.
	sent := runConfigureCapture(t, func(string, string) error { return errors.New("400: refused") },
		config.PendingItemKeyPolicy+"=hold")
	if len(sent) != 1 || !strings.Contains(sent[0], "Configure failed") {
		t.Errorf("daemon refusal: reply = %q", sent)
	}
}

func TestPendingItemCommInsertPendingItem(t *testing.T) {
	type call struct {
		path string
		body map[string]any
	}
	var calls []call
	_, port := newFakePeerParent(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		c := call{path: r.URL.Path}
		if err := json.Unmarshal(raw, &c.body); err != nil {
			t.Errorf("body %q is not JSON: %v", raw, err)
		}
		calls = append(calls, c)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"ab12","status":"draft","version":3}`))
	})
	r := setupRetryRouter(t, port)

	r.HandleTestMessage("automation insert ab12 pending_item=aaaa1111 urgent doc work must land first")
	if len(calls) != 1 || calls[0].path != "/api/automations/ab12/insert_member" {
		t.Fatalf("calls = %+v, want one insert_member", calls)
	}
	member, _ := calls[0].body["member"].(map[string]any)
	if member["pending_item_id"] != "aaaa1111" {
		t.Errorf("member = %v, want pending_item_id aaaa1111", member)
	}
	if _, has := member["prd_id"]; has {
		t.Errorf("member = %v, a pending item insert carries no Automaton id", member)
	}
	if calls[0].body["urgent"] != true || calls[0].body["reason"] != "doc work must land first" {
		t.Errorf("body = %v, want urgent with the reason", calls[0].body)
	}

	calls = nil
	for _, in := range []string{"automation insert ab12 pending_item= why", "automation insert ab12 pending_item=aaaa1111"} {
		got := strings.Join(r.HandleTestMessage(in), " ")
		if !strings.Contains(got, "usage:") || !strings.Contains(got, "pending_item=<item-id>") {
			t.Errorf("%q: reply = %q, want the usage naming pending_item", in, got)
		}
	}
	if len(calls) != 0 {
		t.Errorf("usage errors reached the daemon: %+v", calls)
	}
}

// The hold alert the coordinator raises for a waiting pending item travels
// the ordinary alert fan-out. The body below has the shape the coordinator
// builds (an Automation line, then the hold reason with the item id and the
// attach words); the chat message must carry all of it, untruncated, and the
// attach it asks for must be a verb this channel accepts.
func TestPendingItemCommHoldAlertCarriesAttachHint(t *testing.T) {
	body := `Automation "Docs run" (auto0001): member "Write the guide" is a pending item waiting for an Automaton to be attached (item aaaa1111); attach one to continue`
	r := NewRouter("host", "g1", nil, nil, 0, nil)
	got := make(chan string, 4)
	r.backend = &captureBackend{name: "chat", capture: func(m string) { got <- m }}
	r.SendAlert(&alerts.Alert{Level: alerts.LevelWarn, Title: "Automation blocked", Body: body})

	var msg string
	select {
	case msg = <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("the hold alert never reached the chat backend")
	}
	for _, want := range []string{"ALERT [WARN] Automation blocked", "Write the guide", "item aaaa1111", "pending item", "attach one to continue"} {
		if !strings.Contains(msg, want) {
			t.Errorf("chat alert lacks %q:\n%s", want, msg)
		}
	}
	if strings.Contains(strings.ToLower(msg), "prd") {
		t.Errorf("hold alert must say Automaton, not the internal word: %q", msg)
	}
	if c := Parse("autonomous pending-item-attach aaaa1111 feed0001"); c.Type != CmdAutonomous {
		t.Errorf("the attach the alert asks for must parse as an autonomous verb, got %v", c.Type)
	}
}
