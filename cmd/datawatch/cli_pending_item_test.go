package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// pendingItemCLISample is a registry with a waiting item held by one
// Automation, an attached item, and a configured file registry.
const pendingItemCLISample = `{
  "policy": "hold",
  "auto_resume": true,
  "source": {"configured": true, "path": "registry/work.csv"},
  "last_refresh": {"configured": true, "at": "2026-11-02T08:30:00Z", "rows": 4, "added": 2, "updated": 1, "done": 0, "dropped": 0, "kept": 0, "skipped": 1, "error": ""},
  "items": [
    {"id": "aaaa1111", "ref": "DOC-7", "title": "Write the guide", "state": "waiting", "source": "file", "automaton_id": "",
     "automations": [{"automation_id": "auto0001", "name": "Docs run", "member_id": "m2", "held": true}]},
    {"id": "bbbb2222", "ref": "", "title": "Fix login", "state": "attached", "source": "internal", "automaton_id": "feed0001",
     "automations": [{"automation_id": "auto0001", "name": "Docs run", "member_id": "m5", "held": false}]}
  ],
  "held": [
    {"automation_id": "auto0001", "name": "Docs run", "member_id": "m2", "item_id": "aaaa1111", "title": "Write the guide", "ref": "DOC-7",
     "since": "2026-11-01T09:00:00Z", "reason": "waiting for an Automaton to be attached to \"Write the guide\""}
  ]
}`

func TestPendingItemCLIList(t *testing.T) {
	d := newLaneFillCLIDaemon(t, http.StatusOK, pendingItemCLISample)
	out, err := runLaneFillCmd(t, newAutonomousPendingItemsCmd(), []string{"--state", "waiting", "--automation", "auto0001"})
	if err != nil {
		t.Fatal(err)
	}
	if d.path != "/api/autonomous/pending-items" || d.query != "automation_id=auto0001&state=waiting" {
		t.Errorf("path=%s query=%q; want the pending-items route with both filters", d.path, d.query)
	}
	for _, want := range []string{
		"Pending items (policy: hold, auto-resume: on):",
		"aaaa1111  waiting   Write the guide [ref DOC-7] (file)",
		`held: Automation "Docs run" (auto0001) waits since 2026-11-01 09:00 UTC: waiting for an Automaton to be attached to "Write the guide"`,
		"bbbb2222  attached  Fix login",
		"Automaton feed0001 attached",
		`in Automation "Docs run" (auto0001)`,
		"File registry: registry/work.csv",
		"last refresh 2026-11-02 08:30 UTC: 4 rows, 2 added, 1 updated",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("list lacks %q:\n%s", want, out)
		}
	}
	for _, bad := range []string{"PRD", "prd"} {
		if strings.Contains(out, bad) {
			t.Errorf("list text carries %q:\n%s", bad, out)
		}
	}
}

func TestPendingItemCLIListEmptyAndUnlistedHold(t *testing.T) {
	newLaneFillCLIDaemon(t, http.StatusOK, `{"policy":"skip","auto_resume":false,"source":{"configured":false},"last_refresh":{},"items":[],"held":[]}`)
	out, err := runLaneFillCmd(t, newAutonomousPendingItemsCmd(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"policy: skip, auto-resume: off", "  none", "File registry: not configured"} {
		if !strings.Contains(out, want) {
			t.Errorf("empty list lacks %q:\n%s", want, out)
		}
	}

	// A hold whose item the state filter left out is still shown.
	newLaneFillCLIDaemon(t, http.StatusOK, `{"policy":"hold","auto_resume":true,"source":{"configured":true,"path":"p.csv"},"last_refresh":{"configured":true},"items":[],
	  "held":[{"automation_id":"auto0009","name":"","item_id":"cccc3333","since":"2026-11-01T09:00:00Z","reason":""}]}`)
	out, err = runLaneFillCmd(t, newAutonomousPendingItemsCmd(), []string{"--state", "done"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Held on an item not listed above:", "held: Automation auto0009 waits since 2026-11-01 09:00 UTC", "last refresh: never"} {
		if !strings.Contains(out, want) {
			t.Errorf("list lacks %q:\n%s", want, out)
		}
	}
}

func TestPendingItemCLIListJSON(t *testing.T) {
	d := newLaneFillCLIDaemon(t, http.StatusOK, pendingItemCLISample)
	out, err := runLaneFillCmd(t, newAutonomousPendingItemsCmd(), []string{"--json"})
	if err != nil {
		t.Fatal(err)
	}
	if out != pendingItemCLISample {
		t.Errorf("--json must print the body untouched\ngot:  %q\nwant: %q", out, pendingItemCLISample)
	}
	if d.query != "" {
		t.Errorf("query=%q; want no filter without flags", d.query)
	}
	names := map[string]bool{}
	for _, c := range newAutonomousCmd().Commands() {
		names[c.Name()] = true
	}
	for _, n := range []string{"pending-items", "pending-item-add", "pending-item-attach", "pending-items-refresh"} {
		if !names[n] {
			t.Errorf("%s is not registered under autonomous", n)
		}
	}
	help := newAutonomousCmd().Long
	for _, n := range []string{"pending-items [--state S]", "pending-item-add --title", "pending-item-attach <item-id>", "pending-items-refresh"} {
		if !strings.Contains(help, n) {
			t.Errorf("autonomous help lacks %q", n)
		}
	}
}

func TestPendingItemCLIAdd(t *testing.T) {
	d := newLaneFillCLIDaemon(t, http.StatusCreated, `{"id":"dddd4444","ref":"T-9","title":"Port the importer","state":"waiting","source":"internal","automaton_id":"","automations":[]}`)
	out, err := runLaneFillCmd(t, newAutonomousPendingItemAddCmd(), []string{"--title", "Port the importer", "--ref", "T-9"})
	if err != nil {
		t.Fatal(err)
	}
	if d.path != "/api/autonomous/pending-items" {
		t.Errorf("path=%s", d.path)
	}
	var sent map[string]any
	if err := json.Unmarshal([]byte(d.body), &sent); err != nil {
		t.Fatalf("body %q: %v", d.body, err)
	}
	if sent["title"] != "Port the importer" || sent["ref"] != "T-9" {
		t.Errorf("sent %v; want the title and the ref", sent)
	}
	if want := "Recorded pending item dddd4444 (waiting): Port the importer [ref T-9]\n"; out != want {
		t.Errorf("out=%q want %q", out, want)
	}

	// Without --ref the key is not sent; --json prints the body untouched.
	d = newLaneFillCLIDaemon(t, http.StatusCreated, `{"id":"eeee5555"}`)
	out, err = runLaneFillCmd(t, newAutonomousPendingItemAddCmd(), []string{"--title", "Plain", "--json"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(d.body, "ref") || out != `{"id":"eeee5555"}` {
		t.Errorf("body=%q out=%q", d.body, out)
	}

	// The title is required.
	if _, err := runLaneFillCmd(t, newAutonomousPendingItemAddCmd(), nil); err == nil || !strings.Contains(err.Error(), "title") {
		t.Errorf("a missing --title must be refused, got %v", err)
	}
}

func TestPendingItemCLIAttach(t *testing.T) {
	d := newLaneFillCLIDaemon(t, http.StatusOK, `{
	  "item": {"id": "aaaa1111", "title": "Write the guide", "state": "attached", "automaton_id": "feed0001"},
	  "converted_automations": [{"automation_id": "auto0001", "member_ids": ["m2"], "pending_insert_ids": ["i1"]}],
	  "resumed_automations": ["auto0001"],
	  "notes": ["Automation auto0002 stays blocked: auto-resume is off"]
	}`)
	out, err := runLaneFillCmd(t, newAutonomousPendingItemAttachCmd(), []string{"aaaa1111", "--automaton", "feed0001"})
	if err != nil {
		t.Fatal(err)
	}
	if d.path != "/api/autonomous/pending-items/aaaa1111/attach" {
		t.Errorf("path=%s", d.path)
	}
	var sent map[string]any
	if err := json.Unmarshal([]byte(d.body), &sent); err != nil || sent["automaton_id"] != "feed0001" {
		t.Errorf("body=%q err=%v; want automaton_id feed0001", d.body, err)
	}
	for _, want := range []string{
		"Attached Automaton feed0001 to pending item aaaa1111 (attached): Write the guide",
		"converted in Automation auto0001: members m2; held inserts i1",
		"resumed Automation auto0001",
		"note: Automation auto0002 stays blocked: auto-resume is off",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("attach text lacks %q:\n%s", want, out)
		}
	}

	// --automaton is required.
	if _, err := runLaneFillCmd(t, newAutonomousPendingItemAttachCmd(), []string{"aaaa1111"}); err == nil || !strings.Contains(err.Error(), "automaton") {
		t.Errorf("a missing --automaton must be refused, got %v", err)
	}
}

func TestPendingItemCLIRefresh(t *testing.T) {
	d := newLaneFillCLIDaemon(t, http.StatusOK, `{"configured":true,"path":"work.csv","at":"2026-11-02T08:30:00Z","rows":5,"added":2,"updated":1,"done":1,"dropped":0,"kept":1,"skipped":0}`)
	out, err := runLaneFillCmd(t, newAutonomousPendingItemsRefreshCmd(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if d.path != "/api/autonomous/pending-items/refresh" {
		t.Errorf("path=%s", d.path)
	}
	if want := "Refreshed 2026-11-02 08:30 UTC: 5 rows, 2 added, 1 updated, 1 done, 0 dropped, 1 kept, 0 skipped\n"; out != want {
		t.Errorf("out=%q want %q", out, want)
	}

	// No registry configured: said plainly, no error.
	newLaneFillCLIDaemon(t, http.StatusOK, `{"configured":false,"at":"2026-11-02T08:30:00Z"}`)
	out, err = runLaneFillCmd(t, newAutonomousPendingItemsRefreshCmd(), nil)
	if err != nil || !strings.Contains(out, "No pending item file registry is configured") {
		t.Errorf("out=%q err=%v", out, err)
	}

	// A refresh that could not read the file fails the command, with the reason.
	newLaneFillCLIDaemon(t, http.StatusOK, `{"configured":true,"at":"2026-11-02T08:30:00Z","error":"open work.csv: no such file"}`)
	out, err = runLaneFillCmd(t, newAutonomousPendingItemsRefreshCmd(), nil)
	if err == nil || !strings.Contains(err.Error(), "open work.csv: no such file") {
		t.Errorf("err=%v; want the refresh error", err)
	}
	if !strings.Contains(out, "error: open work.csv: no such file") {
		t.Errorf("out=%q", out)
	}

	// --json prints the body untouched.
	const raw = `{"configured":false}`
	newLaneFillCLIDaemon(t, http.StatusOK, raw)
	out, err = runLaneFillCmd(t, newAutonomousPendingItemsRefreshCmd(), []string{"--json"})
	if err != nil || out != raw {
		t.Errorf("out=%q err=%v", out, err)
	}
}

func TestPendingItemCLIErrorShowsServerMessage(t *testing.T) {
	cases := []struct {
		name   string
		cmd    func() (string, error)
		status int
		reply  string
		want   string
	}{
		{"attach conflict", func() (string, error) {
			return runLaneFillCmd(t, newAutonomousPendingItemAttachCmd(), []string{"aaaa1111", "--automaton", "feed0001"})
		}, http.StatusConflict, "409: pending item aaaa1111 is not waiting (state attached)\n", "409: pending item aaaa1111 is not waiting (state attached)"},
		{"attach missing item", func() (string, error) {
			return runLaneFillCmd(t, newAutonomousPendingItemAttachCmd(), []string{"zzzz9999", "--automaton", "feed0001"})
		}, http.StatusNotFound, "404: no pending item zzzz9999\n", "404: no pending item zzzz9999"},
		{"add blank title", func() (string, error) {
			return runLaneFillCmd(t, newAutonomousPendingItemAddCmd(), []string{"--title", " "})
		}, http.StatusBadRequest, "400: title must not be blank\n", "400: title must not be blank"},
		{"list bad state", func() (string, error) {
			return runLaneFillCmd(t, newAutonomousPendingItemsCmd(), []string{"--state", "open"})
		}, http.StatusBadRequest, "400: state must be \"waiting\", \"attached\" or \"done\", got \"open\"\n", `400: state must be "waiting", "attached" or "done", got "open"`},
		{"refresh unavailable", func() (string, error) {
			return runLaneFillCmd(t, newAutonomousPendingItemsRefreshCmd(), nil)
		}, http.StatusServiceUnavailable, "Automaton pending item registry unavailable\n", "HTTP 503: Automaton pending item registry unavailable"},
		{"empty body", func() (string, error) {
			return runLaneFillCmd(t, newAutonomousPendingItemsRefreshCmd(), nil)
		}, http.StatusBadGateway, "", "HTTP 502"},
	}
	for _, c := range cases {
		newLaneFillCLIDaemon(t, c.status, c.reply)
		out, err := c.cmd()
		if err == nil {
			t.Errorf("%s: want an error, got output %q", c.name, out)
			continue
		}
		if err.Error() != c.want {
			t.Errorf("%s: error %q; want %q", c.name, err.Error(), c.want)
		}
		for _, ok := range []string{"Recorded", "Attached", "Refreshed", "Pending items ("} {
			if strings.Contains(out, ok) {
				t.Errorf("%s: a refused request must not print a success line, got %q", c.name, out)
			}
		}
	}
}

func TestPendingItemCLIInsertFlag(t *testing.T) {
	d := newLaneFillCLIDaemon(t, http.StatusOK, `{"id":"auto0001","name":"Docs run","version":4}`)
	insert := func(args ...string) (string, error) {
		return captureEscalationStdout(t, func() error {
			_, err := runLaneFillCmd(t, newAutomationInsertCmd(), args)
			return err
		})
	}

	if _, err := insert("auto0001", "--pending-item", "aaaa1111", "--reason", "held for the guide", "--expected-version", "3"); err != nil {
		t.Fatal(err)
	}
	if d.path != "/api/automations/auto0001/insert_member" {
		t.Errorf("path=%s; want the insert_member route", d.path)
	}
	var sent struct {
		Member map[string]any `json:"member"`
		Reason string         `json:"reason"`
	}
	if err := json.Unmarshal([]byte(d.body), &sent); err != nil {
		t.Fatalf("body %q: %v", d.body, err)
	}
	if sent.Member["pending_item_id"] != "aaaa1111" {
		t.Errorf("member=%v; want pending_item_id aaaa1111", sent.Member)
	}
	if _, has := sent.Member["prd_id"]; has {
		t.Errorf("member=%v; a pending item insert must not carry an Automaton", sent.Member)
	}
	if sent.Reason != "held for the guide" {
		t.Errorf("reason=%q", sent.Reason)
	}

	// The Automaton form is unchanged.
	if _, err := insert("auto0001", "feed0001", "--reason", "run it", "--expected-version", "3"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(d.body, `"prd_id":"feed0001"`) || strings.Contains(d.body, "pending_item_id") {
		t.Errorf("body=%s; want the Automaton member only", d.body)
	}

	// Both, or neither, is refused before anything is sent.
	calls := d.calls
	if _, err := insert("auto0001", "feed0001", "--pending-item", "aaaa1111", "--reason", "x"); err == nil || !strings.Contains(err.Error(), "not both") {
		t.Errorf("both targets must be refused, got %v", err)
	}
	if _, err := insert("auto0001", "--reason", "x"); err == nil || !strings.Contains(err.Error(), "--pending-item") {
		t.Errorf("no target must be refused, got %v", err)
	}
	if d.calls != calls {
		t.Errorf("a refused insert reached the daemon (%d calls, was %d)", d.calls, calls)
	}
}

// TestPendingItemCLIConfigSetKeys: config set has no key list of its own, so a
// pending item key reaches PUT /api/config as typed and the daemon's refusal of
// a bad value is shown.
func TestPendingItemCLIConfigSetKeys(t *testing.T) {
	cases := []struct {
		key, value string
		want       any
	}{
		{"autonomous.automation.pending_item_policy", "skip", "skip"},
		{"autonomous.automation.pending_item_auto_resume", "false", false},
		{"autonomous.automation.pending_item_source.path", "registry/work.csv", "registry/work.csv"},
		{"autonomous.automation.pending_item_source.id_pattern", `^[A-Z]+-\\d+$`, `^[A-Z]+-\d+$`},
		{"autonomous.automation.pending_item_source.id_column", "Key", "Key"},
		{"autonomous.automation.pending_item_source.title_column", "2", float64(2)},
		{"autonomous.automation.pending_item_source.state_column", "Status", "Status"},
		{"autonomous.automation.pending_item_source.done_values", `["done","shipped"]`, []any{"done", "shipped"}},
		{"autonomous.automation.pending_item_source.on_missing_row", "drop", "drop"},
	}
	for _, c := range cases {
		d := startInfraRetryConfigDaemon(t)
		if _, err := captureEscalationStdout(t, func() error {
			return runConfigSet(&config.Config{}, c.key, c.value)
		}); err != nil {
			t.Errorf("%s: %v", c.key, err)
			continue
		}
		if got := d.config[c.key]; !equalJSONValue(got, c.want) {
			t.Errorf("%s: daemon stored %#v, want %#v", c.key, got, c.want)
		}
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "autonomous.automation.pending_item_policy: must be \"hold\" or \"skip\", got \"pause\"", http.StatusBadRequest)
	}))
	t.Cleanup(srv.Close)
	prev := serverURL
	t.Cleanup(func() { serverURL = prev })
	serverURL = srv.URL
	_, err := captureEscalationStdout(t, func() error {
		return runConfigSet(&config.Config{}, "autonomous.automation.pending_item_policy", "pause")
	})
	if err == nil || !strings.Contains(err.Error(), `must be "hold" or "skip"`) {
		t.Errorf("expected the daemon refusal, got %v", err)
	}
}

// equalJSONValue compares two decoded JSON values by their encoding.
func equalJSONValue(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}
