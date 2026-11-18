package router

import (
	"strings"
	"testing"
)

const planVerbList = `{"project":"acme","plans":[
{"title":"Ship it","id":"ship","state":"in_progress","order":2,"progress":{"done":1,"total":3},"next":{"automaton_id":"a2","title":"Two"}}],
"backlog":{"plans":[{"title":"Later","id":"later","state":"open","order":1,"progress":{"done":0,"total":0}}]}}`

func TestPlanVerbs_ListAndNew(t *testing.T) {
	seen, port := planCommDaemon(t, map[string]planCommRoute{
		"GET /api/projects/acme/plans": {0, planVerbList},
		"POST /api/plans":              {201, `{"plan":{"title":"Big idea","id":"big-idea"}}`},
	})
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous plan list acme"), "\n")
	for _, want := range []string{"1. Ship it [ship] in_progress 1/3 done next: Two", "backlog 1. Later [later]"} {
		if !strings.Contains(got, want) {
			t.Errorf("list lacks %q:\n%s", want, got)
		}
	}
	got = strings.Join(r.HandleTestMessage("autonomous arcs new acme Big idea"), "\n")
	last := (*seen)[len(*seen)-1]
	if last.method != "POST" || last.path != "/api/plans" || !strings.Contains(last.body, `"title":"Big idea"`) || !strings.Contains(last.body, `"project":"acme"`) {
		t.Errorf("new sent %+v", last)
	}
	if !strings.Contains(got, "Created plan Big idea [big-idea]") {
		t.Errorf("new reply %q", got)
	}
}

func TestPlanVerbs_MovePlanSendsFullOrder(t *testing.T) {
	seen, port := planCommDaemon(t, map[string]planCommRoute{
		"GET /api/projects/acme/plans": {0, planVerbList},
		"PUT /api/plans/order":         {0, `{"plans":[]}`},
	})
	r := setupPeerRouter(t, port)
	r.HandleTestMessage("autonomous plan move acme ship 1")
	last := (*seen)[len(*seen)-1]
	if last.method != "PUT" || last.body != `{"plans":["ship","later"]}` {
		t.Errorf("move sent %+v", last)
	}
	got := strings.Join(r.HandleTestMessage("autonomous plan move acme ghost 1"), "\n")
	if !strings.Contains(got, "no plan") {
		t.Errorf("unknown plan reply %q", got)
	}
}

func TestPlanVerbs_MoveAutomatonUsesPlanOrderAndReportsNext(t *testing.T) {
	seen, port := planCommDaemon(t, map[string]planCommRoute{
		// Run order differs from plan order: positions 1..3 are a1, a2, a3.
		"GET /api/plans/p1/automata":       {0, `{"automata":[{"id":"a3","position":3,"run":"blocked"},{"id":"a1","position":1,"run":"next"},{"id":"a2","position":2,"run":"next"}]}`},
		"PUT /api/plans/p1/automata/order": {0, `{"plan":{"title":"P","id":"p1"},"next":{"automaton_id":"a2","title":"Two"},"automata":[]}`},
	})
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous plan move p1 automaton a2 1"), "\n")
	last := (*seen)[len(*seen)-1]
	if last.body != `{"automata":["a2","a1","a3"]}` {
		t.Errorf("move sent %q", last.body)
	}
	if !strings.Contains(got, "Runs next: Two") || !strings.Contains(got, "blocked and held are skipped") {
		t.Errorf("reply %q", got)
	}
	got = strings.Join(r.HandleTestMessage("autonomous plan move p1 automaton ghost 1"), "\n")
	if !strings.Contains(got, "not linked") {
		t.Errorf("unknown Automaton reply %q", got)
	}
}

func TestPlanVerbs_AttachDetachAndConflict(t *testing.T) {
	seen, port := planCommDaemon(t, map[string]planCommRoute{
		"POST /api/plans/p1/attach": {0, `{"attached":"automaton"}`},
		"POST /api/plans/p2/attach": {409, `{"error":"automaton \"a1\" is already linked to p1; detach it first"}`},
		"POST /api/plans/p1/detach": {0, `{"detached":"finding"}`},
	})
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous plan attach p1 automaton a1"), "\n")
	last := (*seen)[len(*seen)-1]
	if last.body != `{"id":"a1","kind":"automaton"}` || !strings.Contains(got, "Attached automaton a1 to plan p1") {
		t.Errorf("attach: %+v %q", last, got)
	}
	got = strings.Join(r.HandleTestMessage("autonomous plan attach p2 automaton a1"), "\n")
	if !strings.Contains(got, "detach it first") {
		t.Errorf("a conflict must be surfaced: %q", got)
	}
	got = strings.Join(r.HandleTestMessage("autonomous plan detach p1 finding f1"), "\n")
	if !strings.Contains(got, "Detached finding f1 from plan p1") {
		t.Errorf("detach reply %q", got)
	}
	n := len(*seen)
	for _, bad := range []string{"autonomous plan attach p1 row x", "autonomous plan attach p1", "autonomous plan", "autonomous plan move acme ship zero", "autonomous plan frob"} {
		got = strings.Join(r.HandleTestMessage(bad), "\n")
		if !strings.Contains(got, "usage") && !strings.Contains(got, "must be") {
			t.Errorf("%q must answer usage, got %q", bad, got)
		}
	}
	if len(*seen) != n {
		t.Error("a malformed verb reached the daemon")
	}
}

func TestMoveIndex(t *testing.T) {
	cases := []struct {
		in       []string
		from, to int
		want     string
	}{
		{[]string{"a", "b", "c"}, 2, 1, "c,a,b"},
		{[]string{"a", "b", "c"}, 0, 3, "b,c,a"},
		{[]string{"a", "b", "c"}, 0, 99, "b,c,a"},
		{[]string{"a", "b", "c"}, 1, 2, "a,b,c"},
	}
	for _, c := range cases {
		if got := strings.Join(moveIndex(c.in, c.from, c.to), ","); got != c.want {
			t.Errorf("moveIndex(%v,%d,%d) = %s, want %s", c.in, c.from, c.to, got, c.want)
		}
	}
}
