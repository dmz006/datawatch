package router

// Tests of the release plan verbs over the chat channel. A fake daemon
// (planCommDaemon) answers canned replies by "METHOD /path" and records every
// request, so the tests check the route, the body and, above all, that a write
// without the preview id never reaches the daemon. Nothing here sleeps.

import (
	"encoding/json"
	"strings"
	"testing"
)

const commThemesReply = `{"plan":{"title":"Release 2","id":"rel-2","release":"2.0"},
"themes":[{"id":"t1","name":"Plans","order":1,"items":[{"kind":"automaton","id":"a1","title":"Plans browser"}]},
{"id":"t2","name":"Docs","order":2,"items":[]}],
"unplaced":[{"kind":"automaton","id":"a2","title":"Loose end"}]}`

func commBody(t *testing.T, r planCommReq) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(r.body), &m); err != nil {
		t.Fatalf("body %q: %v", r.body, err)
	}
	return m
}

func replyOf(r *Router, msg string) string { return strings.Join(r.HandleTestMessage(msg), "\n") }

func TestPlanReleaseComm_ListsThemes(t *testing.T) {
	seen, port := planCommDaemon(t, map[string]planCommRoute{
		"GET /api/plans/rel-2/themes": {0, commThemesReply},
	})
	r := setupPeerRouter(t, port)
	got := replyOf(r, "autonomous plan themes rel-2")
	last := (*seen)[len(*seen)-1]
	if last.method != "GET" || last.path != "/api/plans/rel-2/themes" {
		t.Fatalf("sent %+v", last)
	}
	for _, want := range []string{"Plan Release 2 [rel-2] release 2.0", "1. Plans [t1]: Plans browser [a1]", "2. Docs [t2]: empty", "unplaced: Loose end [a2]"} {
		if !strings.Contains(got, want) {
			t.Errorf("reply lacks %q:\n%s", want, got)
		}
	}
	// arcs works in place of plan, and a missing plan is the usage.
	if got := replyOf(r, "autonomous arcs themes rel-2"); !strings.Contains(got, "[t1]") {
		t.Errorf("arcs alias: %s", got)
	}
	n := len(*seen)
	if got := replyOf(r, "autonomous plan themes"); !strings.Contains(got, "usage") || len(*seen) != n {
		t.Errorf("a missing plan must answer the usage without a request: %s", got)
	}
}

func TestPlanReleaseComm_ThemeActionsSendTheRightBodies(t *testing.T) {
	seen, port := planCommDaemon(t, map[string]planCommRoute{
		"POST /api/plans/rel-2/themes": {0, commThemesReply},
	})
	r := setupPeerRouter(t, port)
	cases := []struct {
		msg  string
		want map[string]any
		said string
	}{
		{"autonomous plan theme add rel-2 Big ideas", map[string]any{"action": "create", "name": "Big ideas"}, "Added theme Big ideas"},
		{"autonomous plan theme rename rel-2 t1 New name", map[string]any{"action": "rename", "theme": "t1", "name": "New name"}, "Renamed theme t1 to New name"},
		{"autonomous plan theme remove rel-2 t1", map[string]any{"action": "delete", "theme": "t1"}, "Removed theme t1"},
		{"autonomous plan theme place rel-2 t1 Automaton a1", map[string]any{"action": "place", "theme": "t1", "kind": "automaton", "id": "a1"}, "Placed automaton a1 in theme t1"},
		{"autonomous plan theme unplace rel-2 automation au1", map[string]any{"action": "unplace", "kind": "automation", "id": "au1"}, "Unplaced automation au1"},
	}
	for _, c := range cases {
		got := replyOf(r, c.msg)
		last := (*seen)[len(*seen)-1]
		if last.method != "POST" || last.path != "/api/plans/rel-2/themes" {
			t.Errorf("%q: sent %+v", c.msg, last)
			continue
		}
		b := commBody(t, last)
		if len(b) != len(c.want) {
			t.Errorf("%q: body %v, want %v", c.msg, b, c.want)
		}
		for k, v := range c.want {
			if b[k] != v {
				t.Errorf("%q: body[%s] = %v, want %v", c.msg, k, b[k], v)
			}
		}
		if !strings.Contains(got, c.said) || !strings.Contains(got, "Plan Release 2") {
			t.Errorf("%q: reply %q must say %q and show the themes", c.msg, got, c.said)
		}
	}
	replyOf(r, "autonomous plan theme reorder rel-2 t2 t1")
	if b := commBody(t, (*seen)[len(*seen)-1]); b["action"] != "reorder" || len(b["themes"].([]any)) != 2 || b["themes"].([]any)[0] != "t2" {
		t.Errorf("reorder body %v", b)
	}
	replyOf(r, "autonomous plan theme reorder-items rel-2 t1 automaton a2 a1")
	if b := commBody(t, (*seen)[len(*seen)-1]); b["action"] != "reorder_items" || b["theme"] != "t1" || b["kind"] != "automaton" || len(b["items"].([]any)) != 2 {
		t.Errorf("reorder-items body %v", b)
	}
}

func TestPlanReleaseComm_ThemeBadArgumentsSendNothing(t *testing.T) {
	seen, port := planCommDaemon(t, map[string]planCommRoute{
		"POST /api/plans/rel-2/themes": {0, commThemesReply},
	})
	r := setupPeerRouter(t, port)
	for _, msg := range []string{
		"autonomous plan theme",
		"autonomous plan theme add rel-2",
		"autonomous plan theme rename rel-2 t1",
		"autonomous plan theme remove rel-2",
		"autonomous plan theme place rel-2 t1 finding f1",
		"autonomous plan theme place rel-2 t1 automaton",
		"autonomous plan theme unplace rel-2 automaton",
		"autonomous plan theme reorder rel-2",
		"autonomous plan theme explode rel-2 t1",
	} {
		got := replyOf(r, msg)
		if !strings.Contains(got, "failed") || !strings.Contains(got, "usage") {
			t.Errorf("%q: want the usage, got %q", msg, got)
		}
	}
	if len(*seen) != 0 {
		t.Errorf("bad verbs reached the daemon: %v", *seen)
	}
}

func TestPlanReleaseComm_ArchivesAndRestoresAPlan(t *testing.T) {
	seen, port := planCommDaemon(t, map[string]planCommRoute{
		"POST /api/plans/old/archive":   {0, `{"plan":{"title":"Old plan","id":"old"},"automata_held":2,"changed":true}`},
		"POST /api/plans/older/archive": {0, `{"plan":{"title":"Older plan","id":"older"},"automata_held":0,"changed":false}`},
		"POST /api/plans/rel-2/archive": {409, `{"error":"plan rel-2 holds 3 Automata and is protected","plan_id":"rel-2","automata_held":3}`},
		"POST /api/plans/old/restore":   {0, `{"plan":{"title":"Old plan","id":"old"},"changed":true}`},
		"POST /api/plans/older/restore": {0, `{"plan":{"title":"Older plan","id":"older"},"changed":false}`},
	})
	r := setupPeerRouter(t, port)
	got := replyOf(r, "autonomous plan archive old protect=rel-2 shipped in 2.0")
	last := (*seen)[len(*seen)-1]
	b := commBody(t, last)
	if last.method != "POST" || last.path != "/api/plans/old/archive" || b["reason"] != "shipped in 2.0" || b["protected_plan"] != "rel-2" {
		t.Fatalf("archive sent %+v", last)
	}
	if !strings.Contains(got, "Archived plan Old plan [old]") || !strings.Contains(got, "nothing was deleted") {
		t.Errorf("archive reply %q", got)
	}
	if got := replyOf(r, "autonomous plan archive older"); !strings.Contains(got, "already archived; nothing changed") {
		t.Errorf("a second archive: %q", got)
	}
	if got := replyOf(r, "autonomous plan archive rel-2 protect=rel-2"); !strings.Contains(got, "protected") || !strings.Contains(got, "failed") {
		t.Errorf("a protected plan must be refused: %q", got)
	}

	got = replyOf(r, "autonomous plan restore old")
	last = (*seen)[len(*seen)-1]
	if last.method != "POST" || last.path != "/api/plans/old/restore" || !strings.Contains(got, "Restored plan Old plan [old]") {
		t.Fatalf("restore sent %+v, reply %q", last, got)
	}
	if got := replyOf(r, "autonomous plan restore older"); !strings.Contains(got, "was not archived; nothing changed") {
		t.Errorf("a restore of a plan that is not archived: %q", got)
	}
	n := len(*seen)
	for _, msg := range []string{"autonomous plan archive", "autonomous plan restore", "autonomous plan restore a b"} {
		if got := replyOf(r, msg); !strings.Contains(got, "usage") {
			t.Errorf("%q: want the usage, got %q", msg, got)
		}
	}
	if len(*seen) != n {
		t.Error("a bad archive or restore reached the daemon")
	}
}

func TestPlanReleaseComm_UnattributedReportsAnAutomatonHeldElsewhere(t *testing.T) {
	seen, port := planCommDaemon(t, map[string]planCommRoute{
		"GET /api/plans/unattributed": {0, `{"automata":[
{"title":"Free one","id":"a1","state":"draft","project":"datawatch","suggested_release":"2.0","suggested_theme":"Plans"},
{"title":"Held one","id":"a2","state":"running","project":"datawatch","holding_plan":"Old plan","holding_plan_id":"old"}],
"page":{"total":12}}`},
	})
	r := setupPeerRouter(t, port)
	got := replyOf(r, "autonomous plan unattributed project=datawatch current=2.0 next=2.1")
	last := (*seen)[len(*seen)-1]
	if last.method != "GET" || last.path != "/api/plans/unattributed" {
		t.Fatalf("sent %+v", last)
	}
	for _, want := range []string{"project=datawatch", "current_release=2.0", "next_release=2.1", "limit=10"} {
		if !strings.Contains(last.query, want) {
			t.Errorf("query %q lacks %q", last.query, want)
		}
	}
	for _, want := range []string{"Free one [a1] draft datawatch → release 2.0, theme Plans",
		"Held one [a2] running datawatch — held by plan Old plan [old]; detach it first", "and 10 more"} {
		if !strings.Contains(got, want) {
			t.Errorf("reply lacks %q:\n%s", want, got)
		}
	}
	if got := replyOf(r, "autonomous plan unattributed stray"); !strings.Contains(got, "usage") {
		t.Errorf("a stray word: %q", got)
	}
}

const commReleasePreview = `{"mode":"release","applied":false,"preview_id":"pv-123","current_release":"2.0","next_release":"2.1",
"created":[{"kind":"plan","title":"Release 2.0"}],"attached":[{"kind":"automaton","id":"a1","title":"One"}],
"unplaced":[],"conflicts":[{"kind":"automaton","id":"a2","title":"Held one","holding_plan":"Old plan","holding_plan_id":"old","reason":"attached to another plan"}],
"unchanged":[],"skipped":[]}`

func TestPlanReleaseComm_MigrateReleasePreviewsThenApproves(t *testing.T) {
	seen, port := planCommDaemon(t, map[string]planCommRoute{
		"POST /api/plans/migrate": {0, commReleasePreview},
	})
	r := setupPeerRouter(t, port)

	got := replyOf(r, "autonomous plan migrate release project=datawatch current=2.0 next=2.1")
	last := (*seen)[len(*seen)-1]
	b := commBody(t, last)
	if last.method != "POST" || last.path != "/api/plans/migrate" || b["mode"] != "release" || b["apply"] != false ||
		b["project"] != "datawatch" || b["current_release"] != "2.0" || b["next_release"] != "2.1" {
		t.Fatalf("preview sent %+v", last)
	}
	if _, has := b["preview_id"]; has {
		t.Errorf("a preview carries no preview id: %v", b)
	}
	for _, want := range []string{"Preview (release 2.0, next 2.1): would create 1, would attach 1, no theme 0, refused 1",
		"refused: Held one — held by plan Old plan [old] — attached to another plan", "Preview id: pv-123",
		"autonomous plan migrate release approve pv-123 project=datawatch current=2.0 next=2.1"} {
		if !strings.Contains(got, want) {
			t.Errorf("preview reply lacks %q:\n%s", want, got)
		}
	}

	// Approve without the preview id: refused, nothing sent.
	n := len(*seen)
	got = replyOf(r, "autonomous plan migrate release approve project=datawatch")
	if !strings.Contains(got, "approve needs the preview id") || !strings.Contains(got, "Nothing was written") {
		t.Errorf("approve without an id: %q", got)
	}
	if len(*seen) != n {
		t.Fatalf("the refused approve reached the daemon: %+v", (*seen)[n:])
	}

	// Approve with the id writes exactly that.
	got = replyOf(r, "autonomous plan migrate release approve pv-123 project=datawatch current=2.0 next=2.1")
	last = (*seen)[len(*seen)-1]
	b = commBody(t, last)
	if b["apply"] != true || b["preview_id"] != "pv-123" || b["mode"] != "release" || b["project"] != "datawatch" {
		t.Fatalf("approve sent %v", b)
	}
	if strings.Contains(got, "failed") {
		t.Errorf("approve reply %q", got)
	}
}

func TestPlanReleaseComm_MigrateReleaseRefusalsAndTheOlderMigration(t *testing.T) {
	seen, port := planCommDaemon(t, map[string]planCommRoute{
		"POST /api/plans/migrate": {409, `{"error":"the preview is stale: the plans changed since you read it; preview again"}`},
	})
	r := setupPeerRouter(t, port)
	got := replyOf(r, "autonomous plan migrate release approve old-id project=datawatch")
	if !strings.Contains(got, "failed") || !strings.Contains(got, "stale") {
		t.Errorf("a stale id must be reported: %q", got)
	}
	n := len(*seen)
	for _, msg := range []string{
		"autonomous plan migrate release",
		"autonomous plan migrate release extra project=datawatch",
		"autonomous plan migrate release approve",
	} {
		if got := replyOf(r, msg); !strings.Contains(got, "failed") {
			t.Errorf("%q: want a refusal, got %q", msg, got)
		}
	}
	if len(*seen) != n {
		t.Errorf("a refused migrate release reached the daemon: %+v", (*seen)[n:])
	}

	// "plan migrate" without release is the older arcs migration.
	seen2, port2 := planCommDaemon(t, map[string]planCommRoute{
		"POST /api/plans/migrate": {0, `{"result":{"applied":false,"would_create":[{"title":"A"}]}}`},
	})
	r2 := setupPeerRouter(t, port2)
	got = replyOf(r2, "autonomous plan migrate")
	last := (*seen2)[len(*seen2)-1]
	if last.body != `{"apply":false}` || !strings.Contains(got, "Preview: would create 1") {
		t.Errorf("the older migration: sent %+v, reply %q", last, got)
	}
}

func TestPlanReleaseComm_AttachSeveralReportsARefusedAutomaton(t *testing.T) {
	seen, port := planCommDaemon(t, map[string]planCommRoute{
		"POST /api/plans/rel-2/attach": {0, `{"attached":"automaton","plan":{"title":"Release 2","id":"rel-2"},"results":[
{"id":"a1","title":"One","status":"attached","theme":"t1"},
{"id":"a2","title":"Two","status":"refused","holder":"old","error":"automaton \"a2\" is already linked to old; detach it first"},
{"id":"a3","status":"not_found","error":"no automaton matches \"a3\""}]}`},
		"POST /api/plans/p1/attach": {0, `{"attached":"automaton"}`},
	})
	r := setupPeerRouter(t, port)
	got := replyOf(r, "autonomous plan attach rel-2 automaton a1 a2 a3 theme=Plans")
	last := (*seen)[len(*seen)-1]
	b := commBody(t, last)
	ids, _ := b["ids"].([]any)
	if last.path != "/api/plans/rel-2/attach" || b["kind"] != "automaton" || b["theme"] != "Plans" || len(ids) != 3 || ids[2] != "a3" {
		t.Fatalf("sent %+v", last)
	}
	if _, has := b["id"]; has {
		t.Errorf("a list must not send id: %v", b)
	}
	for _, want := range []string{"Attached 1 of 3 to plan Release 2 [rel-2]",
		"refused: Two [a2] — attached to plan old; detach it first", "not found: a3"} {
		if !strings.Contains(got, want) {
			t.Errorf("reply lacks %q:\n%s", want, got)
		}
	}

	// One id and no theme is still the single attach, with the older body.
	got = replyOf(r, "autonomous plan attach p1 automaton a1")
	last = (*seen)[len(*seen)-1]
	if last.body != `{"id":"a1","kind":"automaton"}` || !strings.Contains(got, "Attached automaton a1 to plan p1") {
		t.Errorf("single attach: sent %+v, reply %q", last, got)
	}
	// A theme turns even one id into the list form.
	replyOf(r, "autonomous plan attach rel-2 automation au1 theme=t1")
	if b := commBody(t, (*seen)[len(*seen)-1]); b["kind"] != "automation" || b["theme"] != "t1" || len(b["ids"].([]any)) != 1 {
		t.Errorf("themed attach body %v", b)
	}
	// A finding is never a list.
	n := len(*seen)
	if got := replyOf(r, "autonomous plan attach rel-2 finding f1 f2"); !strings.Contains(got, "usage") || len(*seen) != n {
		t.Errorf("a list of findings: %q", got)
	}
}

// The verbs are listed in the autonomous help line and in the parity list.
func TestPlanReleaseComm_VerbsAreListedInTheHelp(t *testing.T) {
	_, port := planCommDaemon(t, map[string]planCommRoute{})
	r := setupPeerRouter(t, port)
	help := replyOf(r, "autonomous")
	for _, want := range []string{"plan themes <plan>", "plan theme add|rename|remove|place|unplace|reorder|reorder-items <plan>",
		"plan archive <plan>", "plan restore <plan>", "plan unattributed", "plan migrate release project=P",
		"plan migrate release approve <preview-id>", "plan attach <plan> automaton|automation <id> <id>... [theme=T]"} {
		if !strings.Contains(help, want) {
			t.Errorf("the autonomous help lacks %q", want)
		}
	}
	for _, verb := range PlanReleaseCommVerbs() {
		if !strings.Contains(help, "plan "+verb) {
			t.Errorf("PlanReleaseCommVerbs names %q but the help has no \"plan %s\"", verb, verb)
		}
		if !strings.Contains(planReleaseVerbUsage, "plan "+verb) {
			t.Errorf("the usage line has no \"plan %s\"", verb)
		}
	}
	for _, banned := range []string{"PRD", "BL4", "GH#"} {
		if strings.Contains(planReleaseVerbUsage, banned) {
			t.Errorf("the usage line mentions %q", banned)
		}
	}
	// A verb the plan command does not know answers both usage lines.
	if got := replyOf(r, "autonomous plan nonsense"); !strings.Contains(got, "plan themes") || !strings.Contains(got, "plan list") {
		t.Errorf("an unknown plan verb must answer the full usage: %q", got)
	}
}
