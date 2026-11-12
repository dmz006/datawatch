// Tests for timeline row status, edges and ordering.

package server

import (
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

func tlIDs(rows []timelineRow) []string {
	var ids []string
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	return ids
}

func tlEq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestTimelineRowStatusStartEndRelease(t *testing.T) {
	done := tlPRD("d", autonomous.PRDCompleted, -20, -10, "/x")
	done.MergedInRelease = "v1.2.3"
	live := tlPRD("l", autonomous.PRDRunning, -5, -1, "/x", tlTask("k", autonomous.TaskInProgress, tlTime(-4), nil))
	out := tlBuild(t, timelineInput{
		Arcs:        []config.ProjectArc{{AutomationID: "arc1"}},
		PRDs:        []*autonomous.PRD{done, live},
		Automations: []*autonomous.Automation{tlAutomation("arc1", "d", "l")},
		Level:       "automata", Parent: "arc1",
	})
	d := tlRow(t, out.Rows, "d")
	if d.Status != "completed" || d.Release != "v1.2.3" || d.Start == nil || !d.Start.Equal(*tlTime(-20)) ||
		d.End == nil || !d.End.Equal(*tlTime(-10)) || !d.Start.Equal(*d.First) {
		t.Errorf("done row = %+v", d)
	}
	l := tlRow(t, out.Rows, "l")
	if l.Status != "running" || l.End != nil || l.Start == nil {
		t.Errorf("live row = %+v", l)
	}
	arc := tlBuild(t, timelineInput{Arcs: []config.ProjectArc{{AutomationID: "arc1"}}, PRDs: []*autonomous.PRD{done, live},
		Automations: []*autonomous.Automation{tlAutomation("arc1", "d", "l")}}).Rows[0]
	if arc.Status != "running" || arc.End != nil || !arc.Start.Equal(*tlTime(-20)) {
		t.Errorf("arc row = %+v", arc)
	}
}

func TestTimelineTaskDependsOnAndOrder(t *testing.T) {
	b := tlTask("b", autonomous.TaskCompleted, tlTime(-9), tlTime(-8))
	a := tlTask("a", autonomous.TaskCompleted, tlTime(-7), tlTime(-6))
	b.DependsOn = []string{"a"}
	p := tlPRD("p", autonomous.PRDCompleted, -10, -5, "/x", b, a)
	out := tlBuild(t, timelineInput{Arcs: []config.ProjectArc{{AutomationID: "arc1"}}, PRDs: []*autonomous.PRD{p},
		Automations: []*autonomous.Automation{tlAutomation("arc1", "p")}, Level: "tasks", Automaton: "p"})
	if got := tlIDs(out.Rows); !tlEq(got, []string{"a", "b"}) {
		t.Errorf("order = %v, want dependency a above b", got)
	}
	if r := tlRow(t, out.Rows, "b"); len(r.DependsOn) != 1 || r.DependsOn[0] != "a" {
		t.Errorf("depends_on = %v", r.DependsOn)
	}
}

func TestOrderTimelineRows(t *testing.T) {
	row := func(id string, s, e *int) timelineRow {
		r := timelineRow{ID: id}
		if s != nil {
			r.Start = tlTime(*s)
		}
		if e != nil {
			r.End = tlTime(*e)
		}
		return r
	}
	i := func(n int) *int { return &n }
	rows := []timelineRow{row("run", i(-5), nil), row("late", i(-9), i(-2)), row("early", i(-9), i(-8)), row("wait", i(-3), nil)}
	if got := tlIDs(orderTimelineRows(rows, nil)); !tlEq(got, []string{"early", "late", "run", "wait"}) {
		t.Errorf("completion order = %v", got)
	}
	// late must come first: early depends on late.
	edges := []timelineEdge{{From: "late", To: "early", Kind: "chain"}}
	if got := tlIDs(orderTimelineRows(rows, edges)); !tlEq(got, []string{"late", "early", "run", "wait"}) {
		t.Errorf("topological order = %v", got)
	}
	// cycle falls back to completion order.
	cyc := []timelineEdge{{From: "late", To: "early"}, {From: "early", To: "late"}}
	if got := tlIDs(orderTimelineRows(rows, cyc)); !tlEq(got, []string{"early", "late", "run", "wait"}) {
		t.Errorf("cycle order = %v", got)
	}
	if rows[0].ID != "run" {
		t.Error("input was modified")
	}
}

func TestTimelineEdgesThroughForkJoin(t *testing.T) {
	a := &autonomous.Automation{ID: "arc1", Members: []autonomous.AutomationMember{
		{ID: "m1", PRDID: "p1", OnComplete: "f"},
		{ID: "f", Kind: autonomous.MemberKindFork, ForkMembers: []string{"m2", "m3"}},
		{ID: "m2", PRDID: "p2"},
		{ID: "m3", PRDID: "p3"},
		{ID: "j", Kind: autonomous.MemberKindJoin, JoinMembers: []string{"m2", "m3"}, OnComplete: "m4"},
		{ID: "m4", PRDID: "p4", OnFailed: "m1"},
	}}
	got := map[string]bool{}
	for _, e := range timelineAutomationEdges(a) {
		if e.Kind != "chain" {
			t.Errorf("kind = %q", e.Kind)
		}
		got[e.From+">"+e.To] = true
	}
	for _, w := range []string{"p1>p2", "p1>p3", "p2>p4", "p3>p4", "p4>p1"} {
		if !got[w] {
			t.Errorf("missing edge %s in %v", w, got)
		}
	}
	if got["p2>p3"] || len(got) != 5 {
		t.Errorf("edges = %v", got)
	}
}

func TestTimelineFocusEdgesAndOrder(t *testing.T) {
	p1 := tlPRD("p1", autonomous.PRDCompleted, -30, -5, "/x")
	p2 := tlPRD("p2", autonomous.PRDCompleted, -20, -25, "/x")
	arc := &autonomous.Automation{ID: "arc1", Name: "arc", Members: []autonomous.AutomationMember{
		{ID: "m1", PRDID: "p1", OnComplete: "m2"}, {ID: "m2", PRDID: "p2"}}}
	out := tlBuild(t, timelineInput{Arcs: []config.ProjectArc{{AutomationID: "arc1"}}, PRDs: []*autonomous.PRD{p1, p2},
		Automations: []*autonomous.Automation{arc}, Focus: "arc1"})
	if len(out.Edges) != 1 || out.Edges[0] != (timelineEdge{From: "p1", To: "p2", Kind: "chain"}) {
		t.Fatalf("edges = %+v", out.Edges)
	}
	if got := tlIDs(out.Rows); !tlEq(got, []string{"p1", "p2"}) {
		t.Errorf("order = %v, dependency p1 must sit above p2 despite finishing later", got)
	}
}
