package router

import (
	"net/http"
	"slices"
	"strings"
	"testing"
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

func TestPendingItemComm_List(t *testing.T) {
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

func TestPendingItemComm_ListFilters(t *testing.T) {
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

func TestPendingItemComm_ListCapsAtTen(t *testing.T) {
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

func TestPendingItemComm_ListUsageAndErrors(t *testing.T) {
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

func TestPendingItemComm_Add(t *testing.T) {
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

func TestPendingItemComm_AddNeedsTitle(t *testing.T) {
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

func TestPendingItemComm_Attach(t *testing.T) {
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

func TestPendingItemComm_AttachUsageAndRefusal(t *testing.T) {
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

func TestPendingItemComm_Refresh(t *testing.T) {
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

func TestPendingItemComm_ParityListHasVerbs(t *testing.T) {
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
