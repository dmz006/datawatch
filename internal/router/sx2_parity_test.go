// Sprint Sx2 — comm-channel parity parser tests.

package router

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSx2_Parse_Cost_Empty(t *testing.T) {
	c := Parse("cost")
	if c.Type != CmdCost || c.Text != "" {
		t.Errorf("got %+v", c)
	}
}

func TestSx2_Parse_Cost_WithSession(t *testing.T) {
	c := Parse("cost host-aa01")
	if c.Type != CmdCost || c.Text != "host-aa01" {
		t.Errorf("got %+v", c)
	}
}

func TestSx2_Parse_Stale_Default(t *testing.T) {
	c := Parse("stale")
	if c.Type != CmdStale || c.Text != "" {
		t.Errorf("got %+v", c)
	}
}

func TestSx2_Parse_Stale_WithSeconds(t *testing.T) {
	c := Parse("stale 600")
	if c.Type != CmdStale || c.Text != "600" {
		t.Errorf("got %+v", c)
	}
}

func TestSx2_Parse_Audit_Empty(t *testing.T) {
	c := Parse("audit")
	if c.Type != CmdAudit || c.Text != "" {
		t.Errorf("got %+v", c)
	}
}

func TestSx2_Parse_Audit_WithFilters(t *testing.T) {
	c := Parse("audit actor=operator action=start limit=20")
	if c.Type != CmdAudit {
		t.Fatalf("type=%v", c.Type)
	}
	if c.Text != "actor=operator action=start limit=20" {
		t.Errorf("text=%q", c.Text)
	}
}

func TestSx2_Parse_Cooldown_Status(t *testing.T) {
	c := Parse("cooldown")
	if c.Type != CmdCooldown || c.CooldownVerb != "status" {
		t.Errorf("got %+v", c)
	}
	c2 := Parse("cooldown status")
	if c2.Type != CmdCooldown || c2.CooldownVerb != "status" {
		t.Errorf("got %+v", c2)
	}
}

func TestSx2_Parse_Cooldown_Set(t *testing.T) {
	c := Parse("cooldown set 60 anthropic 429")
	if c.Type != CmdCooldown {
		t.Fatalf("type=%v", c.Type)
	}
	if c.CooldownVerb != "set" {
		t.Errorf("verb=%q", c.CooldownVerb)
	}
	if c.CooldownSeconds != 60 {
		t.Errorf("seconds=%d", c.CooldownSeconds)
	}
	if c.CooldownReason != "anthropic 429" {
		t.Errorf("reason=%q", c.CooldownReason)
	}
}

func TestSx2_Parse_Cooldown_Clear(t *testing.T) {
	c := Parse("cooldown clear")
	if c.Type != CmdCooldown || c.CooldownVerb != "clear" {
		t.Errorf("got %+v", c)
	}
}

func TestSx2_Parse_Rest_GET(t *testing.T) {
	c := Parse("rest GET /api/cost")
	if c.Type != CmdRest {
		t.Fatalf("type=%v", c.Type)
	}
	if c.RestMethod != "GET" {
		t.Errorf("method=%q", c.RestMethod)
	}
	if c.RestPath != "/api/cost" {
		t.Errorf("path=%q", c.RestPath)
	}
	if c.RestBody != "" {
		t.Errorf("body should be empty: %q", c.RestBody)
	}
}

func TestSx2_Parse_Rest_POST_WithBody(t *testing.T) {
	c := Parse(`rest POST /api/projects {"name":"foo","dir":"/tmp"}`)
	if c.Type != CmdRest {
		t.Fatalf("type=%v", c.Type)
	}
	if c.RestMethod != "POST" || c.RestPath != "/api/projects" {
		t.Errorf("got %+v", c)
	}
	if c.RestBody != `{"name":"foo","dir":"/tmp"}` {
		t.Errorf("body=%q", c.RestBody)
	}
}

func TestSx2_Parse_Rest_Malformed(t *testing.T) {
	if c := Parse("rest"); c.Type != CmdUnknown {
		t.Errorf("missing args should be unknown: %+v", c)
	}
	if c := Parse("rest GET"); c.Type != CmdUnknown {
		t.Errorf("missing path should be unknown: %+v", c)
	}
}

func TestSx2_Router_SetWebPort(t *testing.T) {
	r := &Router{}
	r.SetWebPort(8080)
	if r.webPort != 8080 {
		t.Errorf("got %d want 8080", r.webPort)
	}
}

func TestSx2_Router_LoopbackUnconfigured(t *testing.T) {
	r := &Router{}
	if _, err := r.commGet("/api/cost", nil); err == nil {
		t.Error("expected loopback-not-configured error")
	}
	if _, err := r.commJSON("POST", "/api/cooldown", `{}`); err == nil {
		t.Error("expected loopback-not-configured error")
	}
}

// ── BL221 Phase 4 — autonomous type-register comm verb tests ─────────────────

func TestBL221_Parse_Autonomous_TypeRegister(t *testing.T) {
	c := Parse("autonomous type-register devops DevOps")
	if c.Type != CmdAutonomous || c.Text != "type-register devops DevOps" {
		t.Errorf("got %+v", c)
	}
}

func TestBL221_Parse_Autonomous_TypeRegister_WithExtras(t *testing.T) {
	c := Parse("autonomous type-register devops DevOps description=infra color=#ff0000")
	if c.Type != CmdAutonomous || c.Text != "type-register devops DevOps description=infra color=#ff0000" {
		t.Errorf("got %+v", c)
	}
}

func TestBL221_Autonomous_TypeRegister_MissingArgs(t *testing.T) {
	r := &Router{}
	replies := r.HandleTestMessage("autonomous type-register")
	if len(replies) == 0 {
		t.Fatal("expected a reply")
	}
	combined := strings.Join(replies, " ")
	if !strings.Contains(combined, "usage") && !strings.Contains(combined, "failed") {
		t.Errorf("expected usage hint, got: %v", replies)
	}
}

func TestBL221_Autonomous_TypeRegister_MissingLabel(t *testing.T) {
	r := &Router{}
	replies := r.HandleTestMessage("autonomous type-register myid")
	combined := strings.Join(replies, " ")
	if !strings.Contains(combined, "usage") && !strings.Contains(combined, "failed") {
		t.Errorf("expected usage hint, got: %v", replies)
	}
}

func TestBL221_Autonomous_TypeRegister_Loopback(t *testing.T) {
	r := &Router{}
	replies := r.HandleTestMessage("autonomous type-register devops DevOps")
	combined := strings.Join(replies, " ")
	if !strings.Contains(combined, "failed") && !strings.Contains(combined, "loopback") {
		t.Errorf("expected loopback/failed error, got: %v", replies)
	}
}

func TestBL221_Parse_Autonomous_TypeList(t *testing.T) {
	c := Parse("autonomous types")
	if c.Type != CmdAutonomous || c.Text != "types" {
		t.Errorf("got %+v", c)
	}
}

func TestBL221_Parse_Autonomous_TypeListAlias(t *testing.T) {
	c := Parse("autonomous type-list")
	if c.Type != CmdAutonomous || c.Text != "type-list" {
		t.Errorf("got %+v", c)
	}
}

// ── notify_events comm verbs (parity) ──────────────────────────────────────

func TestNotifyEvents_Parse(t *testing.T) {
	for _, in := range []string{
		"autonomous set-notify-events p1 prd.blocked,prd.failed",
		"autonomous set_notify_events p1 inherit",
		"autonomous set-notify-events",
		"autonomous set-notify-events p1",
	} {
		c := Parse(in)
		if c.Type != CmdAutonomous {
			t.Errorf("Parse(%q) = %+v", in, c)
		}
	}
}

type notifyEventsRequest struct {
	method string
	path   string
	body   string
}

// fakeNotifyEventsDaemon stands in for the two REST ends the comm verbs
// hit: POST /api/autonomous/prds/{id}/set_notify_events and the
// guardrail-profile GET/PUT pair. Every request is recorded in order.
func fakeNotifyEventsDaemon(t *testing.T, reqs *[]notifyEventsRequest) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	record := func(r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		*reqs = append(*reqs, notifyEventsRequest{method: r.Method, path: r.URL.Path, body: string(b)})
	}
	wjson := func(w http.ResponseWriter, b string) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, b)
	}
	mux.HandleFunc("/api/autonomous/prds/p1/set_notify_events", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		wjson(w, `{"id":"p1","name":"demo","notify_events":["prd.needs_review","prd.blocked"],"notify_events_effective":{"kinds":["prd.needs_review","prd.blocked"],"source":"prd"}}`)
	})
	mux.HandleFunc("/api/autonomous/prds/p2/set_notify_events", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		wjson(w, `{"id":"p2","name":"demo2","notify_events":[]}`)
	})
	mux.HandleFunc("/api/autonomous/guardrail_profiles/pg1", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		if r.Method == http.MethodGet {
			wjson(w, `{"id":"pg1","name":"nightly","description":"keep it quiet","guardrails":["rules-review","ci-watch"],"notify_events":[]}`)
			return
		}
		wjson(w, `{"id":"pg1","name":"nightly","description":"keep it quiet","guardrails":["rules-review","ci-watch"],"notify_events":["prd.blocked","prd.failed"]}`)
	})
	mux.HandleFunc("/api/autonomous/guardrail_profiles", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		wjson(w, `{"id":"pg_new","name":"nightly","guardrails":["rules-review"],"notify_events":["prd.blocked","prd.failed"]}`)
	})
	return httptest.NewServer(mux)
}

func TestNotifyEvents_Set(t *testing.T) {
	var reqs []notifyEventsRequest
	srv := fakeNotifyEventsDaemon(t, &reqs)
	defer srv.Close()

	r := loopbackRouter(t, srv)
	out := strings.Join(r.HandleTestMessage("autonomous set-notify-events p1 prd.needs_review,prd.blocked"), "\n")

	if len(reqs) != 1 {
		t.Fatalf("daemon requests = %d want 1 (%v)", len(reqs), reqs)
	}
	if reqs[0].method != http.MethodPost || reqs[0].path != "/api/autonomous/prds/p1/set_notify_events" {
		t.Fatalf("request = %s %s", reqs[0].method, reqs[0].path)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(reqs[0].body), &body); err != nil {
		t.Fatalf("body not valid JSON: %v (%q)", err, reqs[0].body)
	}
	kinds, _ := body["notify_events"].([]any)
	if len(kinds) != 2 || kinds[0] != "prd.needs_review" || kinds[1] != "prd.blocked" {
		t.Errorf("notify_events = %v", kinds)
	}
	for _, frag := range []string{"autonomous set-notify-events", "prd.needs_review", "notify_events_effective"} {
		if !strings.Contains(out, frag) {
			t.Errorf("reply misses %q:\n%s", frag, out)
		}
	}
}

// TestNotifyEvents_SetWarningKinds pins the comm surface for the new
// opt-in warning kinds: set-notify-events accepts scope_warning /
// guardrail_warn verbatim and the reply lists them — turning warnings on
// per Automaton never implies blocking.
func TestNotifyEvents_SetWarningKinds(t *testing.T) {
	var reqs []notifyEventsRequest
	srv := fakeNotifyEventsDaemon(t, &reqs)
	defer srv.Close()

	r := loopbackRouter(t, srv)
	out := strings.Join(r.HandleTestMessage("autonomous set-notify-events p1 prd.blocked,scope_warning,guardrail_warn"), "\n")

	if len(reqs) != 1 {
		t.Fatalf("daemon requests = %d want 1 (%v)", len(reqs), reqs)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(reqs[0].body), &body); err != nil {
		t.Fatalf("body not valid JSON: %v (%q)", err, reqs[0].body)
	}
	kinds, _ := body["notify_events"].([]any)
	want := []string{"prd.blocked", "scope_warning", "guardrail_warn"}
	if len(kinds) != len(want) {
		t.Fatalf("notify_events = %v, want %v", kinds, want)
	}
	for _, w := range want {
		if i := findKind(kinds, w); i < 0 {
			t.Errorf("notify_events = %v, missing %v", kinds, w)
		}
	}
	if !strings.Contains(out, "autonomous set-notify-events") {
		t.Errorf("reply should echo the verb header:\n%s", out)
	}
}

func findKind(vals []any, want string) int {
	for i, v := range vals {
		if v == want {
			return i
		}
	}
	return -1
}

func TestNotifyEvents_SetUnderscoreAlias(t *testing.T) {
	var reqs []notifyEventsRequest
	srv := fakeNotifyEventsDaemon(t, &reqs)
	defer srv.Close()

	r := loopbackRouter(t, srv)
	out := strings.Join(r.HandleTestMessage("autonomous set_notify_events p2 inherit"), "\n")

	if len(reqs) != 1 || reqs[0].path != "/api/autonomous/prds/p2/set_notify_events" {
		t.Fatalf("requests = %v", reqs)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(reqs[0].body), &body); err != nil {
		t.Fatalf("body not valid JSON: %v (%q)", err, reqs[0].body)
	}
	kinds, _ := body["notify_events"].([]any)
	if len(kinds) != 0 {
		t.Errorf("inherit should send an empty list, got %v", kinds)
	}
	if strings.Contains(out, "failed") {
		t.Errorf("reply should not report failure:\n%s", out)
	}
}

func TestNotifyEvents_SetUsageOnMissingArgs(t *testing.T) {
	r := &Router{hostname: "h"}
	for _, msg := range []string{"autonomous set-notify-events", "autonomous set-notify-events p1"} {
		out := strings.Join(r.HandleTestMessage(msg), "\n")
		if !strings.Contains(out, "usage: autonomous set-notify-events <prd-id> <kind,kind,...|inherit>") {
			t.Errorf("%q: expected usage, got:\n%s", msg, out)
		}
		if !strings.Contains(out, "prd.needs_review, prd.blocked, prd.failed, prd.completed, prd.cancelled") {
			t.Errorf("%q: usage should show the valid default set, got:\n%s", msg, out)
		}
	}
}

func TestNotifyEvents_HelpListsVerb(t *testing.T) {
	r := &Router{hostname: "h"}
	out := strings.Join(r.HandleTestMessage("autonomous"), "\n")
	if !strings.Contains(out, "set-notify-events <prd> <kinds|inherit>") {
		t.Errorf("help text misses the new verb:\n%s", out)
	}
}

func TestNotifyEvents_ProfileCreateToken(t *testing.T) {
	var reqs []notifyEventsRequest
	srv := fakeNotifyEventsDaemon(t, &reqs)
	defer srv.Close()

	r := loopbackRouter(t, srv)
	out := strings.Join(r.HandleTestMessage("guardrail profile create nightly rules-review,ci-watch notify_events=prd.blocked,prd.failed"), "\n")

	if len(reqs) != 1 || reqs[0].method != http.MethodPost || reqs[0].path != "/api/autonomous/guardrail_profiles" {
		t.Fatalf("requests = %v", reqs)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(reqs[0].body), &body); err != nil {
		t.Fatalf("body not valid JSON: %v (%q)", err, reqs[0].body)
	}
	if body["name"] != "nightly" {
		t.Errorf("name = %v", body["name"])
	}
	guardrails, _ := body["guardrails"].([]any)
	if len(guardrails) != 2 || guardrails[0] != "rules-review" || guardrails[1] != "ci-watch" {
		t.Errorf("guardrails = %v", guardrails)
	}
	kinds, _ := body["notify_events"].([]any)
	if len(kinds) != 2 || kinds[0] != "prd.blocked" || kinds[1] != "prd.failed" {
		t.Errorf("notify_events = %v", kinds)
	}
	if strings.Contains(out, "error") {
		t.Errorf("reply should not report an error:\n%s", out)
	}
}

func TestNotifyEvents_ProfileCreateWithoutToken(t *testing.T) {
	var reqs []notifyEventsRequest
	srv := fakeNotifyEventsDaemon(t, &reqs)
	defer srv.Close()

	r := loopbackRouter(t, srv)
	_ = r.HandleTestMessage("guardrail profile create plain rules-review")

	if len(reqs) != 1 {
		t.Fatalf("requests = %v", reqs)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(reqs[0].body), &body); err != nil {
		t.Fatalf("body not valid JSON: %v (%q)", err, reqs[0].body)
	}
	if _, present := body["notify_events"]; present {
		t.Errorf("no notify_events token, body must omit the key: %v", body)
	}
}

// A profile PUT overwrites description and guardrails with whatever the
// body carries, so a notify_events-only verb must fetch the profile and
// resend its current values. This pins that behaviour.
func TestNotifyEvents_ProfileNotifyEvents(t *testing.T) {
	var reqs []notifyEventsRequest
	srv := fakeNotifyEventsDaemon(t, &reqs)
	defer srv.Close()

	r := loopbackRouter(t, srv)
	out := strings.Join(r.HandleTestMessage("guardrail profile notify-events pg1 prd.blocked,prd.failed"), "\n")

	if len(reqs) != 2 {
		t.Fatalf("expected GET then PUT, got %d requests (%v)", len(reqs), reqs)
	}
	if reqs[0].method != http.MethodGet || reqs[1].method != http.MethodPut {
		t.Fatalf("order = %s then %s, want GET then PUT", reqs[0].method, reqs[1].method)
	}
	if reqs[1].path != "/api/autonomous/guardrail_profiles/pg1" {
		t.Fatalf("PUT path = %s", reqs[1].path)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(reqs[1].body), &body); err != nil {
		t.Fatalf("PUT body not valid JSON: %v (%q)", err, reqs[1].body)
	}
	if body["description"] != "keep it quiet" {
		t.Errorf("description = %v want 'keep it quiet' (must be preserved on the PUT)", body["description"])
	}
	guardrails, _ := body["guardrails"].([]any)
	if len(guardrails) != 2 || guardrails[0] != "rules-review" || guardrails[1] != "ci-watch" {
		t.Errorf("guardrails = %v (must be preserved on the PUT)", guardrails)
	}
	kinds, _ := body["notify_events"].([]any)
	if len(kinds) != 2 || kinds[0] != "prd.blocked" || kinds[1] != "prd.failed" {
		t.Errorf("notify_events = %v", kinds)
	}
	if strings.Contains(out, "error") {
		t.Errorf("reply should not report an error:\n%s", out)
	}
}

func TestNotifyEvents_ProfileNotifyEventsInherit(t *testing.T) {
	var reqs []notifyEventsRequest
	srv := fakeNotifyEventsDaemon(t, &reqs)
	defer srv.Close()

	r := loopbackRouter(t, srv)
	_ = r.HandleTestMessage("guardrail profile notify-events pg1 inherit")

	if len(reqs) != 2 {
		t.Fatalf("expected GET then PUT, got %d requests", len(reqs))
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(reqs[1].body), &body); err != nil {
		t.Fatalf("PUT body not valid JSON: %v (%q)", err, reqs[1].body)
	}
	kinds, ok := body["notify_events"].([]any)
	if !ok || len(kinds) != 0 {
		t.Errorf("inherit should PUT an empty list, got %v", body["notify_events"])
	}
}

func TestNotifyEvents_ProfileUsage(t *testing.T) {
	r := &Router{hostname: "h"}
	out := strings.Join(r.HandleTestMessage("guardrail profile notify-events"), "\n")
	if !strings.Contains(out, "usage: guardrail profile notify-events <id> <kind,kind,...|inherit>") {
		t.Errorf("expected usage, got:\n%s", out)
	}
	out = strings.Join(r.HandleTestMessage("guardrail profile notify-events pg1"), "\n")
	if !strings.Contains(out, "usage: guardrail profile notify-events <id> <kind,kind,...|inherit>") {
		t.Errorf("expected usage, got:\n%s", out)
	}
	out = strings.Join(r.HandleTestMessage("guardrail profile bogus"), "\n")
	if !strings.Contains(out, "notify-events") {
		t.Errorf("unknown-subcommand hint should list notify-events, got:\n%s", out)
	}
}

func TestNotifyEvents_ConfigureHelpListsKey(t *testing.T) {
	r := &Router{hostname: "h"}
	r.SetConfigureFunc(func(string, string) error { return nil })
	for _, msg := range []string{"configure help", "configure list"} {
		out := strings.Join(r.HandleTestMessage(msg), "\n")
		if !strings.Contains(out, "autonomous.notify.default_events") {
			t.Errorf("%q: help misses the key:\n%s", msg, out)
		}
	}
}

func TestNotifyEvents_ConfigureKeyReachesConfigWrite(t *testing.T) {
	var gotKey, gotValue string
	r := &Router{hostname: "h"}
	r.SetConfigureFunc(func(k, v string) error {
		gotKey, gotValue = k, v
		return nil
	})
	out := strings.Join(r.HandleTestMessage("configure autonomous.notify.default_events=prd.blocked,prd.failed"), "\n")

	if gotKey != "autonomous.notify.default_events" || gotValue != "prd.blocked,prd.failed" {
		t.Errorf("config write fn received (%q, %q)", gotKey, gotValue)
	}
	if !strings.Contains(out, "Set autonomous.notify.default_events") {
		t.Errorf("expected success reply, got:\n%s", out)
	}
}
