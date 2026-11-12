// One test per option the Group-by picker offers, over one fixture that holds
// a peer-node session and Automaton task sessions (worker, verifier, planner),
// one of them on a peer node.

package server

import (
	"testing"

	"github.com/dmz006/datawatch/internal/session"
)

func axesFixture() []sessionGroupRow {
	return []sessionGroupRow{
		{ID: "a1", FullID: "dev-a1", State: session.StateRunning, Hostname: "dev-box", Server: "local", ProjectDir: "/p/x", Tags: []string{"stage:build"}},
		{ID: "a2", FullID: "dev-a2", State: session.StateComplete, Hostname: "dev-box", ProjectDir: "/p/x/", InheritedTags: []string{"automation:worker", "lane:l1"}},
		// peer-node session: the peer's name keys it, not its hostname
		{ID: "b1", FullID: "gpu-b1", State: session.StateRunning, Hostname: "gpu-internal", Server: "gpu-peer", ProjectDir: "/p/y"},
		// Automaton task sessions: worker, verifier, planner; the verifier runs on a peer
		{ID: "w1", FullID: "dev-w1", State: session.StateRunning, Hostname: "dev-box", ProjectDir: "/wt/x", PRDID: "prd1", StoryID: "st1", TaskID: "t1", Tags: []string{"automation:worker"}},
		{ID: "v1", FullID: "gpu-v1", State: session.StateComplete, Hostname: "gpu-internal", Server: "gpu-peer", ProjectDir: "/wt/x", PRDID: "prd1", StoryID: "st1", TaskID: "t1", Tags: []string{"automation:verifier"}},
		{ID: "p1", FullID: "dev-p1", State: session.StateFailed, Hostname: "dev-box", PRDID: "prd1", Tags: []string{"automation:planner"}},
		{ID: "n1", FullID: "dev-n1", State: session.StateWaitingInput, Hostname: ""},
	}
}

func groupIDs(g sessionGroup) map[string]bool {
	out := map[string]bool{}
	for _, m := range g.Sessions {
		out[m.ID] = true
	}
	return out
}

func assertOnce(t *testing.T, g sessionGrouped, want int) {
	t.Helper()
	if g.Total != want {
		t.Fatalf("total = %d, want %d", g.Total, want)
	}
	n := 0
	for _, grp := range g.Groups {
		n += grp.Count
	}
	if n != want {
		t.Fatalf("group counts add up to %d, want %d (each session in exactly one group)", n, want)
	}
}

func TestGroupAxisState(t *testing.T) {
	g, err := GroupSessions(axesFixture(), "state", "")
	if err != nil {
		t.Fatal(err)
	}
	assertOnce(t, g, 7)
	if groupByKey(t, g, "running").Count != 3 || groupByKey(t, g, "complete").Count != 2 ||
		groupByKey(t, g, "failed").Count != 1 || groupByKey(t, g, "waiting_input").Count != 1 {
		t.Fatalf("state groups: %+v", g.Groups)
	}
}

func TestGroupAxisNode(t *testing.T) {
	g, err := GroupSessions(axesFixture(), "node", "")
	if err != nil {
		t.Fatal(err)
	}
	assertOnce(t, g, 7)
	if ids := groupIDs(groupByKey(t, g, "gpu-peer")); !ids["b1"] || !ids["v1"] || len(ids) != 2 {
		t.Fatalf("peer node group: %v", ids)
	}
	if groupByKey(t, g, "dev-box").Count != 4 {
		t.Fatalf("dev-box: %+v", groupByKey(t, g, "dev-box"))
	}
	for _, grp := range g.Groups {
		if grp.Key == "local" || grp.Key == "gpu-internal" {
			t.Fatalf("node group %q must not exist", grp.Key)
		}
	}
	if last := g.Groups[len(g.Groups)-1]; last.Key != "ungrouped" || last.Count != 1 {
		t.Fatalf("ungrouped must be last: %+v", g.Groups)
	}
}

func TestGroupAxisProject(t *testing.T) {
	g, err := GroupSessions(axesFixture(), "project", "")
	if err != nil {
		t.Fatal(err)
	}
	assertOnce(t, g, 7)
	// "/p/x" and "/p/x/" are one folder.
	if px := groupByKey(t, g, "/p/x"); px.Count != 2 {
		t.Fatalf("/p/x: %+v", px)
	}
	if wt := groupByKey(t, g, "/wt/x"); wt.Count != 2 {
		t.Fatalf("/wt/x: %+v", wt)
	}
	if groupByKey(t, g, "ungrouped").Count != 2 {
		t.Fatalf("ungrouped: %+v", groupByKey(t, g, "ungrouped"))
	}
}

func TestGroupAxisTag(t *testing.T) {
	g, err := GroupSessions(axesFixture(), "tag", "")
	if err != nil {
		t.Fatal(err)
	}
	assertOnce(t, g, 7)
	// a2 holds inherited automation:worker + lane:l1: sorted first wins.
	if ids := groupIDs(groupByKey(t, g, "automation:worker")); !ids["a2"] || !ids["w1"] {
		t.Fatalf("automation:worker: %v", ids)
	}
	if groupByKey(t, g, "automation:verifier").Count != 1 || groupByKey(t, g, "stage:build").Count != 1 {
		t.Fatalf("tag groups: %+v", g.Groups)
	}
}

func TestGroupAxisTagPrefix(t *testing.T) {
	g, err := GroupSessions(axesFixture(), "tag", "automation:")
	if err != nil {
		t.Fatal(err)
	}
	assertOnce(t, g, 7)
	if ids := groupIDs(groupByKey(t, g, "automation:planner")); !ids["p1"] {
		t.Fatalf("planner: %v", ids)
	}
	// a1 only has stage:build, b1 has none: both ungrouped under this prefix.
	if ids := groupIDs(groupByKey(t, g, "ungrouped")); !ids["a1"] || !ids["b1"] || !ids["n1"] {
		t.Fatalf("ungrouped: %v", ids)
	}
}

func TestGroupAxisAutomaton(t *testing.T) {
	g, err := GroupSessions(axesFixture(), "automaton", "")
	if err != nil {
		t.Fatal(err)
	}
	assertOnce(t, g, 7)
	a := groupByKey(t, g, "prd1")
	if a.Count != 3 || a.Progress == nil || a.Progress.Total != 3 || a.Progress.Done != 2 {
		t.Fatalf("prd1: %+v", a)
	}
	if len(a.Stories) != 2 || a.Stories[0].Key != groupRootStoryKey || a.Stories[1].Key != "st1" {
		t.Fatalf("stories (root first): %+v", a.Stories)
	}
	st := a.Stories[1]
	if st.Count != 2 || st.Sessions[0].TaskID != "t1" || st.Sessions[1].TaskID != "t1" {
		t.Fatalf("story st1 nests the worker and the peer-node verifier: %+v", st)
	}
	if root := a.Stories[0]; root.Count != 1 || root.Sessions[0].ID != "p1" {
		t.Fatalf("planner in the root block: %+v", root)
	}
	if groupByKey(t, g, "ungrouped").Count != 4 {
		t.Fatalf("ungrouped: %+v", groupByKey(t, g, "ungrouped"))
	}
}
