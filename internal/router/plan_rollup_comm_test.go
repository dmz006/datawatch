package router

import (
	"net/url"
	"strings"
	"testing"
)

const planRollupReply = `{"project":"acme","dry_run":%DRY%,"applied":%APPLIED%,"changed":2,"kept":1,"changes":[
{"title":"Ship it","plan":"ship","from":"open","to":"done","reason":"all 2 linked items finished"},
{"title":"Broken","plan":"broken","from":"open","to":"blocked","reason":"1 of 1 linked items failed or blocked"},
{"title":"Hand set","plan":"hand","from":"cancelled","to":"done","reason":"all 1 linked items finished","set_by_hand_kept":true}]}`

func rollupReply(dry bool) string {
	r := strings.NewReplacer("%DRY%", "false", "%APPLIED%", "true")
	if dry {
		r = strings.NewReplacer("%DRY%", "true", "%APPLIED%", "false")
	}
	return r.Replace(planRollupReply)
}

func TestPlanRollupComm_DryRunUsesTheReadRoute(t *testing.T) {
	seen, port := planCommDaemon(t, map[string]planCommRoute{
		"GET /api/plans/rollup": {0, rollupReply(true)},
	})
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous plan rollup dry-run project=acme"), "\n")
	if len(*seen) != 1 {
		t.Fatalf("expected one request, saw %+v", *seen)
	}
	last := (*seen)[0]
	if last.method != "GET" || last.path != "/api/plans/rollup" {
		t.Fatalf("dry run sent %+v", last)
	}
	q, _ := url.ParseQuery(last.query)
	if q.Get("dry_run") != "true" || q.Get("project") != "acme" {
		t.Errorf("query = %q", last.query)
	}
	for _, want := range []string{"Would change 2 plan(s); 1 kept because the status was set by hand.",
		"Ship it [ship]: open -> done - all 2 linked items finished",
		"Broken [broken]: open -> blocked",
		"Hand set [hand]: cancelled -> done (kept: set by hand)"} {
		if !strings.Contains(got, want) {
			t.Errorf("reply lacks %q:\n%s", want, got)
		}
	}
}

func TestPlanRollupComm_ApplyPostsAndSaysChanged(t *testing.T) {
	seen, port := planCommDaemon(t, map[string]planCommRoute{
		"POST /api/plans/rollup": {0, rollupReply(false)},
	})
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous arcs rollup"), "\n")
	last := (*seen)[len(*seen)-1]
	if last.method != "POST" || last.path != "/api/plans/rollup" || last.body != `{"dry_run":false}` {
		t.Fatalf("apply sent %+v", last)
	}
	if !strings.Contains(got, "Changed 2 plan(s)") || strings.Contains(got, "Would change") {
		t.Errorf("reply %q", got)
	}
	r.HandleTestMessage("autonomous plan rollup project=acme")
	last = (*seen)[len(*seen)-1]
	if last.body != `{"dry_run":false,"project":"acme"}` {
		t.Errorf("project body = %q", last.body)
	}
}

func TestPlanRollupComm_NothingToChangeAndErrors(t *testing.T) {
	_, port := planCommDaemon(t, map[string]planCommRoute{
		"GET /api/plans/rollup":  {0, `{"dry_run":true,"applied":false,"changed":0,"kept":0,"changes":[]}`},
		"POST /api/plans/rollup": {400, `{"error":"unknown project"}`},
	})
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous plan rollup dry-run"), "\n")
	if !strings.Contains(got, "Every plan already has the status") {
		t.Errorf("empty reply %q", got)
	}
	got = strings.Join(r.HandleTestMessage("autonomous plan rollup project=ghost"), "\n")
	if !strings.Contains(got, "autonomous plan rollup failed") || !strings.Contains(got, "unknown project") {
		t.Errorf("error reply %q", got)
	}
	got = strings.Join(r.HandleTestMessage("autonomous plan rollup frobnicate"), "\n")
	if !strings.Contains(got, "usage:") || !strings.Contains(got, "plan rollup [dry-run]") {
		t.Errorf("usage reply %q", got)
	}
}

func TestPlanRollupComm_HelpListsTheVerb(t *testing.T) {
	if !strings.Contains(planVerbUsage, "plan rollup [dry-run] [project=P]") {
		t.Errorf("plan usage line lacks rollup: %s", planVerbUsage)
	}
	_, port := planCommDaemon(t, map[string]planCommRoute{})
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous help"), "\n")
	if !strings.Contains(got, "plan rollup [dry-run]") {
		t.Errorf("autonomous help lacks plan rollup:\n%.400s", got)
	}
}

func TestPlanVerbs_ListMarksAStatusSetByHand(t *testing.T) {
	_, port := planCommDaemon(t, map[string]planCommRoute{
		"GET /api/projects/acme/plans": {0, `{"plans":[{"title":"Held","id":"held","state":"cancelled","set_by_hand":true,"progress":{"done":0,"total":1}},` +
			`{"title":"Derived","id":"derived","state":"done","set_by_hand":false,"progress":{"done":1,"total":1}}],"backlog":{"plans":[]}}`},
	})
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous plan list acme"), "\n")
	if !strings.Contains(got, "1. Held [held] cancelled (by hand) 0/1 done") || !strings.Contains(got, "2. Derived [derived] done 1/1 done") {
		t.Errorf("list reply:\n%s", got)
	}
}
