// Tests for the session-list grouping read (session_group.go): the pure
// grouping over every axis, the single placement of a multi-tag session, the
// ungrouped bucket, the state mix, the Automaton nesting with progress, a
// generated 300-session fixture, the 400 for an unknown axis, and the
// unchanged legacy response without group_by.

package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/session"
)

// TestSessionGroupTagAxisWithPrefix covers the tag axis: matching tags group
// their sessions, non-matching ones go to ungrouped, and chips keep the
// other tags.
func TestSessionGroupTagAxisWithPrefix(t *testing.T) {
	rows := []sessionGroupRow{
		{ID: "s1", FullID: "h/s1", State: session.StateRunning, Tags: []string{"automation:worker", "backend:claude"}},
		{ID: "s2", FullID: "h/s2", State: session.StateComplete, Tags: []string{"automation:verifier"}},
		{ID: "s3", FullID: "h/s3", State: session.StateFailed, Tags: []string{"lane:l-1"}},
	}
	g, err := GroupSessions(rows, "tag", "automation:")
	if err != nil {
		t.Fatalf("GroupSessions: %v", err)
	}
	if len(g.Groups) != 3 {
		t.Fatalf("groups = %d, want 3: %+v", len(g.Groups), g.Groups)
	}
	worker := groupByKey(t, g, "automation:worker")
	if worker.Count != 1 || worker.StateMix[string(session.StateRunning)] != 1 {
		t.Fatalf("automation:worker group = %+v", worker)
	}
	verifier := groupByKey(t, g, "automation:verifier")
	if verifier.Count != 1 || verifier.StateMix[string(session.StateComplete)] != 1 {
		t.Fatalf("automation:verifier group = %+v", verifier)
	}
	ungrouped := groupByKey(t, g, "ungrouped")
	if ungrouped.Count != 1 || len(ungrouped.Sessions) != 1 || ungrouped.Sessions[0].ID != "s3" {
		t.Fatalf("ungrouped group = %+v", ungrouped)
	}
	chips := worker.Sessions[0].Chips
	if len(chips) != 1 || chips[0] != "backend:claude" {
		t.Fatalf("s1 chips = %v, want [backend:claude]", chips)
	}
}

// TestSessionGroupTagAxisWholeTag covers the tag axis without a prefix: the
// group is the (first, sorted) whole tag of the session.
func TestSessionGroupTagAxisWholeTag(t *testing.T) {
	rows := []sessionGroupRow{
		{ID: "s1", FullID: "h/s1", State: session.StateRunning, Tags: []string{"zeta", "alpha", "beta"}},
		{ID: "s2", FullID: "h/s2", State: session.StateComplete},
	}
	g, err := GroupSessions(rows, "tag", "")
	if err != nil {
		t.Fatalf("GroupSessions: %v", err)
	}
	alpha := groupByKey(t, g, "alpha")
	if alpha.Count != 1 || alpha.Sessions[0].ID != "s1" {
		t.Fatalf("alpha group = %+v", alpha)
	}
	// The other tags stay as chips.
	chips := alpha.Sessions[0].Chips
	if len(chips) != 2 || chips[0] != "beta" || chips[1] != "zeta" {
		t.Fatalf("s1 chips = %v, want [beta zeta]", chips)
	}
	if g.TagPrefix != "" {
		t.Fatalf("TagPrefix = %q, want empty", g.TagPrefix)
	}
}

// TestSessionGroupMultiTagSinglePlacement pins the rule: a session with
// several tags that match the axis lands in exactly one group — the first
// matching tag in sorted order wins — and keeps the rest as chips.
func TestSessionGroupMultiTagSinglePlacement(t *testing.T) {
	rows := []sessionGroupRow{
		{ID: "s1", FullID: "h/s1", State: session.StateRunning, Tags: []string{"automation:verifier", "automation:worker", "stage:plan"}},
	}
	g, err := GroupSessions(rows, "tag", "automation:")
	if err != nil {
		t.Fatalf("GroupSessions: %v", err)
	}
	found := 0
	var placed sessionGroupMember
	for _, grp := range g.Groups {
		for _, m := range grp.Sessions {
			if m.ID == "s1" {
				found++
				placed = m
			}
		}
	}
	if found != 1 {
		t.Fatalf("s1 appears in %d groups, want exactly 1: %+v", found, g.Groups)
	}
	if len(g.Groups) != 1 || g.Groups[0].Key != "automation:verifier" {
		t.Fatalf("groups = %+v, want the single group automation:verifier (first match in sorted order)", g.Groups)
	}
	chips := placed.Chips
	if len(chips) != 2 || chips[0] != "automation:worker" || chips[1] != "stage:plan" {
		t.Fatalf("chips = %v, want [automation:worker stage:plan]", chips)
	}
}

// TestSessionGroupStateAxis covers the state axis.
func TestSessionGroupStateAxis(t *testing.T) {
	rows := []sessionGroupRow{
		{ID: "s1", FullID: "h/s1", State: session.StateRunning},
		{ID: "s2", FullID: "h/s2", State: session.StateRunning},
		{ID: "s3", FullID: "h/s3", State: session.StateComplete},
	}
	g, err := GroupSessions(rows, "state", "")
	if err != nil {
		t.Fatalf("GroupSessions: %v", err)
	}
	running := groupByKey(t, g, string(session.StateRunning))
	if running.Count != 2 {
		t.Fatalf("running group = %+v", running)
	}
	complete := groupByKey(t, g, string(session.StateComplete))
	if complete.Count != 1 {
		t.Fatalf("complete group = %+v", complete)
	}
}

// TestSessionGroupNodeAxis covers the node (hostname) axis.
func TestSessionGroupNodeAxis(t *testing.T) {
	rows := []sessionGroupRow{
		{ID: "s1", FullID: "h/s1", State: session.StateRunning, Hostname: "gpu-1"},
		{ID: "s2", FullID: "h/s2", State: session.StateComplete, Hostname: "gpu-1"},
		{ID: "s3", FullID: "h/s3", State: session.StateFailed, Hostname: ""},
	}
	g, err := GroupSessions(rows, "node", "")
	if err != nil {
		t.Fatalf("GroupSessions: %v", err)
	}
	if groupByKey(t, g, "gpu-1").Count != 2 {
		t.Fatalf("gpu-1 group missing: %+v", g.Groups)
	}
	if groupByKey(t, g, "ungrouped").Count != 1 {
		t.Fatalf("ungrouped should hold the session without a hostname: %+v", g.Groups)
	}
}

// TestSessionGroupProjectAxis covers the project axis.
func TestSessionGroupProjectAxis(t *testing.T) {
	rows := []sessionGroupRow{
		{ID: "s1", FullID: "h/s1", State: session.StateRunning, ProjectDir: "/repo/app"},
		{ID: "s2", FullID: "h/s2", State: session.StateComplete, ProjectDir: "/repo/app"},
		{ID: "s3", FullID: "h/s3", State: session.StateFailed, ProjectDir: "/repo/web"},
		{ID: "s4", FullID: "h/s4", State: session.StateKilled, ProjectDir: ""},
	}
	g, err := GroupSessions(rows, "project", "")
	if err != nil {
		t.Fatalf("GroupSessions: %v", err)
	}
	if groupByKey(t, g, "/repo/app").Count != 2 {
		t.Fatalf("/repo/app group = %+v", g.Groups)
	}
	if groupByKey(t, g, "/repo/web").Count != 1 {
		t.Fatalf("/repo/web group = %+v", g.Groups)
	}
	if groupByKey(t, g, "ungrouped").Count != 1 {
		t.Fatalf("ungrouped should hold the session without a project dir: %+v", g.Groups)
	}
}

// TestSessionGroupProjectResolution checks the project axis prefers the
// datawatch project name over the working folder.
func TestSessionGroupProjectResolution(t *testing.T) {
	rows := []sessionGroupRow{
		{ID: "a1", FullID: "h/a1", State: session.StateRunning, PRDID: "prd1",
			ProjectDir: "/home/<user>/.datawatch/prd-worktrees/prd1/task",
			SystemTags: []string{"role:worker", "project:datawatch"}},
		{ID: "p1", FullID: "h/p1", State: session.StateRunning, ProjectDir: "/repo/plain"},
		{ID: "n1", FullID: "h/n1", State: session.StateRunning},
	}
	g, err := GroupSessions(rows, "project", "")
	if err != nil {
		t.Fatalf("GroupSessions: %v", err)
	}
	if groupByKey(t, g, "datawatch").Count != 1 {
		t.Fatalf("Automaton task session should group under datawatch: %+v", g.Groups)
	}
	if groupByKey(t, g, "/repo/plain").Count != 1 {
		t.Fatalf("plain session should group under its folder: %+v", g.Groups)
	}
	if groupByKey(t, g, "ungrouped").Count != 1 {
		t.Fatalf("row with nothing known should be ungrouped: %+v", g.Groups)
	}
	for _, grp := range g.Groups {
		if strings.Contains(grp.Key, "prd-worktrees") {
			t.Fatalf("worktree path leaked as a group: %+v", g.Groups)
		}
	}
}

// TestSessionGroupInheritedTagsCovered covers rows whose matching tag is
// inherited from the Automaton rather than owned by the session.
func TestSessionGroupInheritedTagsCovered(t *testing.T) {
	rows := []sessionGroupRow{
		{ID: "s1", FullID: "h/s1", State: session.StateRunning, InheritedTags: []string{"stage:plan"}},
	}
	g, err := GroupSessions(rows, "tag", "stage:")
	if err != nil {
		t.Fatalf("GroupSessions: %v", err)
	}
	if groupByKey(t, g, "stage:plan").Count != 1 {
		t.Fatalf("stage:plan group missing (inherited tag): %+v", g.Groups)
	}
}

// TestSessionGroupUngroupedLastAndMixAndTotals pins the ungrouped bucket's
// order (last), the per-group state mix and the per-state totals of the
// response.
func TestSessionGroupUngroupedLastAndMixAndTotals(t *testing.T) {
	rows := []sessionGroupRow{
		{ID: "s1", FullID: "h/s1", State: session.StateRunning, Tags: []string{"automation:worker"}},
		{ID: "s2", FullID: "h/s2", State: session.StateComplete, Tags: []string{"automation:worker"}},
		{ID: "s3", FullID: "h/s3", State: session.StateRunning},
		{ID: "s4", FullID: "h/s4", State: session.StateFailed},
	}
	g, err := GroupSessions(rows, "tag", "automation:")
	if err != nil {
		t.Fatalf("GroupSessions: %v", err)
	}
	if g.Groups[len(g.Groups)-1].Key != "ungrouped" {
		t.Fatalf("ungrouped must be the last group, got %q last: %v", g.Groups[len(g.Groups)-1].Key, groupKeysOf(g))
	}
	worker := groupByKey(t, g, "automation:worker")
	if worker.StateMix[string(session.StateRunning)] != 1 || worker.StateMix[string(session.StateComplete)] != 1 {
		t.Fatalf("state_mix = %v, want running=1 complete=1", worker.StateMix)
	}
	wantTotals := map[string]int{
		string(session.StateRunning):  2,
		string(session.StateComplete): 1,
		string(session.StateFailed):   1,
	}
	if len(g.StateTotals) != len(wantTotals) {
		t.Fatalf("state_totals = %v, want %v", g.StateTotals, wantTotals)
	}
	for k, v := range wantTotals {
		if g.StateTotals[k] != v {
			t.Fatalf("state_totals[%s] = %d, want %d", k, g.StateTotals[k], v)
		}
	}
	if g.Total != len(rows) {
		t.Fatalf("total = %d, want %d", g.Total, len(rows))
	}
}

// TestSessionGroupAutomatonNest pins the Automaton axis: sessions with an
// Automaton id nest under the Automaton and its story, one block for a whole
// Automaton's sessions, with a progress count; sessions without an id are
// ungrouped.
func TestSessionGroupAutomatonNest(t *testing.T) {
	rows := []sessionGroupRow{
		{ID: "w1", FullID: "h/w1", State: session.StateRunning, PRDID: "p1", StoryID: "st-1", TaskID: "t1", Tags: []string{"stage:build"}},
		{ID: "w2", FullID: "h/w2", State: session.StateComplete, PRDID: "p1", StoryID: "st-1", TaskID: "t2"},
		{ID: "v1", FullID: "h/v1", State: session.StateFailed, PRDID: "p1", StoryID: "st-1"},
		{ID: "pl", FullID: "h/pl", State: session.StateComplete, PRDID: "p1"},
		{ID: "free", FullID: "h/free", State: session.StateRunning},
	}
	g, err := GroupSessions(rows, "automaton", "")
	if err != nil {
		t.Fatalf("GroupSessions: %v", err)
	}
	automaton := groupByKey(t, g, "p1")
	if automaton.Count != 4 {
		t.Fatalf("p1 count = %d, want 4: %+v", automaton.Count, automaton)
	}
	// w2 (complete), v1 (failed) and pl (complete) are terminal: 3 of 4 done.
	if automaton.Progress == nil || automaton.Progress.Done != 3 || automaton.Progress.Total != 4 {
		t.Fatalf("progress = %+v, want done 3 of 4", automaton.Progress)
	}
	story := storyByKey(t, automaton, "st-1")
	if story.Count != 3 {
		t.Fatalf("story st-1 count = %d, want 3: %+v", story.Count, story)
	}
	ids := []string{}
	for _, st := range automaton.Stories {
		ids = append(ids, storyIDs(st.Sessions)...)
	}
	if len(ids) != 4 {
		t.Fatalf("p1 block sessions = %v, want the 4 sessions of the Automaton", ids)
	}
	// Workers, verifier and planner form one block: no session of p1 leaks
	// out, no other group claims them.
	if groupByKey(t, g, "ungrouped").Count != 1 {
		t.Fatalf("ungrouped should hold only the session without an id: %+v", g.Groups)
	}
	if storyByKey(t, automaton, "root").Count != 1 {
		t.Fatalf("the story-less planner session should sit in the root story: %+v", automaton.Stories)
	}
	// Story blocks carry their own state mix.
	if story.StateMix[string(session.StateRunning)] != 1 ||
		story.StateMix[string(session.StateComplete)] != 1 ||
		story.StateMix[string(session.StateFailed)] != 1 {
		t.Fatalf("story state_mix = %v", story.StateMix)
	}
}

// TestSessionGroupThreeHundredSessions runs the pure grouping over a
// generated 300-session fixture and checks the invariants hold at scale.
func TestSessionGroupThreeHundredSessions(t *testing.T) {
	const n = 300
	states := []session.State{session.StateRunning, session.StateWaitingInput, session.StateComplete, session.StateFailed}
	rows := make([]sessionGroupRow, 0, n)
	for i := 0; i < n; i++ {
		row := sessionGroupRow{
			ID:     fmt.Sprintf("s%03d", i),
			FullID: fmt.Sprintf("h/s%03d", i),
			State:  states[i%len(states)],
		}
		if i%3 == 0 {
			row.Tags = append(row.Tags, "lane:a")
		}
		if i%5 == 0 {
			row.Tags = append(row.Tags, "lane:b")
		}
		if i%6 == 0 {
			row.Tags = append(row.Tags, "host:gpu-1")
		}
		row.PRDID = fmt.Sprintf("p%02d", i%7)
		if i%2 == 0 {
			row.StoryID = fmt.Sprintf("st-%d", i%4)
		}
		rows = append(rows, row)
	}

	byTag, err := GroupSessions(rows, "tag", "lane:")
	if err != nil {
		t.Fatalf("GroupSessions(tag): %v", err)
	}
	checkInvariants(t, "tag", byTag, n)
	if membersOf(byTag) != n {
		t.Fatalf("tag groups hold %d members, want %d", membersOf(byTag), n)
	}
	// Every lane row must sit in exactly one lane group, never in both: a
	// session carrying both a- and b-lane tags goes to lane:a (first in
	// sorted order) and stays out of lane:b.
	if got := groupByKey(t, byTag, "lane:a").Count; got != n/3 { // every i%3==0 row carries lane:a
		t.Fatalf("lane:a count = %d, want %d", got, n/3)
	}
	if got := groupByKey(t, byTag, "lane:b").Count; got != n/5-n/15 { // only b rows without a
		t.Fatalf("lane:b count = %d, want %d", got, n/5-n/15)
	}
	if byTag.StateTotals[string(session.StateRunning)] != 75 { // i%4==0 → states[0]
		t.Fatalf("state_totals[running] = %d, want 75", byTag.StateTotals[string(session.StateRunning)])
	}

	byAuto, err := GroupSessions(rows, "automaton", "")
	if err != nil {
		t.Fatalf("GroupSessions(automaton): %v", err)
	}
	checkInvariants(t, "automaton", byAuto, n)
	if membersOfStories(byAuto) != n {
		t.Fatalf("automaton stories hold %d members, want %d", membersOfStories(byAuto), n)
	}
	if !checkSumOfProgress(t, byAuto, "automaton", n) {
		return
	}

	byState, err := GroupSessions(rows, "state", "")
	if err != nil {
		t.Fatalf("GroupSessions(state): %v", err)
	}
	checkInvariants(t, "state", byState, n)
}

// checkInvariants asserts the per-state totals and per-group counts cover the
// whole fixture exactly once.
func checkInvariants(t *testing.T, axis string, g sessionGrouped, total int) {
	t.Helper()
	sum := 0
	for _, v := range g.StateTotals {
		sum += v
	}
	if sum != total {
		t.Fatalf("%s: state totals sum to %d, want %d", axis, sum, total)
	}
	counts := 0
	for _, grp := range g.Groups {
		counts += grp.Count
	}
	if counts != total {
		t.Fatalf("%s: group counts sum to %d, want %d", axis, counts, total)
	}
	if g.Total != total {
		t.Fatalf("%s: total = %d, want %d", axis, g.Total, total)
	}
}

// checkSumOfProgress asserts the automaton progress counts cover the fixture.
func checkSumOfProgress(t *testing.T, g sessionGrouped, axis string, total int) bool {
	t.Helper()
	done, sum := 0, 0
	states := []session.State{session.StateRunning, session.StateWaitingInput, session.StateComplete, session.StateFailed}
	expectDone := 0
	for i := 0; i < total; i++ {
		if states[i%4].IsTerminal() {
			expectDone++
		}
	}
	for _, grp := range g.Groups {
		if grp.Progress == nil {
			continue
		}
		done += grp.Progress.Done
		sum += grp.Progress.Total
	}
	if sum != total || done != expectDone {
		t.Fatalf("%s: progress sums to %d done of %d, want %d of %d", axis, done, sum, expectDone, total)
	}
	return true
}

func TestSessionGroupRejectsUnknownAxis(t *testing.T) {
	if _, err := GroupSessions(nil, "bogus", ""); err == nil {
		t.Fatal("GroupSessions(bogus) must be refused")
	}
	q := url.Values{"group_by": []string{"bogus"}}
	if _, err := parseSessionGroupOptions(q); err == nil {
		t.Fatal("parseSessionGroupOptions(bogus) must be refused")
	}
	for _, axis := range []string{"tag", "state", "node", "project", "automaton"} {
		o, err := parseSessionGroupOptions(url.Values{"group_by": []string{axis}})
		if err != nil || !o.active || o.axis != axis {
			t.Fatalf("axis %q: err=%v active=%v", axis, err, o.active)
		}
	}
	o, err := parseSessionGroupOptions(url.Values{})
	if err != nil || o.active {
		t.Fatalf("without group_by the options must be inactive, got active=%v err=%v", o.active, err)
	}
	prefix, err := parseSessionGroupOptions(url.Values{"group_by": []string{"tag"}, "tag_prefix": []string{" automation: "}})
	if err != nil || !prefix.active || prefix.tagPrefix != "automation:" {
		t.Fatalf("tag_prefix round trip: active=%v prefix=%q err=%v", prefix.active, prefix.tagPrefix, err)
	}
}

// TestSessionGroupHandlerServesGroups drives the endpoint end to end: the
// grouped envelope with groups, chips and state totals.
func TestSessionGroupHandlerServesGroups(t *testing.T) {
	sessA := &session.Session{ID: "s1", FullID: "testhost/s1", Name: "worker", State: session.StateRunning, ProjectDir: "/proj", TmuxSession: "s1", Tags: []string{"automation:worker", "backend:claude"}}
	sessB := &session.Session{ID: "s2", FullID: "testhost/s2", Name: "verifier", State: session.StateComplete, ProjectDir: "/proj", TmuxSession: "s2", Tags: []string{"automation:verifier"}}
	sessC := &session.Session{ID: "s3", FullID: "testhost/s3", Name: "solo", State: session.StateFailed, ProjectDir: "/proj", TmuxSession: "s3"}
	s := sessionViewServer(t, 0, 200, sessA, sessB, sessC)

	rr := getSessions(t, s, "group_by=tag&tag_prefix=automation:")
	if rr.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
	}
	var g sessionGrouped
	if err := json.Unmarshal(rr.Body.Bytes(), &g); err != nil {
		t.Fatalf("decode grouped body: %v (body=%s)", err, rr.Body.String())
	}
	if g.GroupBy != "tag" || g.TagPrefix != "automation:" || g.Total != 3 {
		t.Fatalf("envelope = %+v, want tag/automation: and total 3", g)
	}
	worker := groupByKey(t, g, "automation:worker")
	if worker.Count != 1 || len(worker.Sessions) != 1 || worker.Sessions[0].ID != "s1" {
		t.Fatalf("automation:worker group = %+v", worker)
	}
	if len(worker.Sessions[0].Chips) != 1 || worker.Sessions[0].Chips[0] != "backend:claude" {
		t.Fatalf("chips = %v, want [backend:claude]", worker.Sessions[0].Chips)
	}
	if groupByKey(t, g, "ungrouped").Count != 1 {
		t.Fatalf("ungrouped should hold the untagged session: %+v", g.Groups)
	}
	if g.StateTotals[string(session.StateRunning)] != 1 || g.StateTotals[string(session.StateComplete)] != 1 || g.StateTotals[string(session.StateFailed)] != 1 {
		t.Fatalf("state_totals = %v", g.StateTotals)
	}
}

// TestSessionGroupHandlerAutomatonAxis serves the automaton axis with the
// story nesting and the progress count.
func TestSessionGroupHandlerAutomatonAxis(t *testing.T) {
	w := &session.Session{ID: "w", FullID: "testhost/w", Name: "worker", State: session.StateRunning, ProjectDir: "/proj", TmuxSession: "w", PRDID: "p-1", StoryID: "st-1", TaskID: "t-1"}
	v := &session.Session{ID: "v", FullID: "testhost/v", Name: "verifier", State: session.StateComplete, ProjectDir: "/proj", TmuxSession: "v", PRDID: "p-1", StoryID: "st-1"}
	f := &session.Session{ID: "f", FullID: "testhost/f", Name: "free", State: session.StateRunning, ProjectDir: "/proj", TmuxSession: "f"}
	s := sessionViewServer(t, 0, 200, w, v, f)

	rr := getSessions(t, s, "group_by=automaton")
	if rr.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
	}
	var g sessionGrouped
	if err := json.Unmarshal(rr.Body.Bytes(), &g); err != nil {
		t.Fatalf("decode grouped body: %v", err)
	}
	automaton := groupByKey(t, g, "p-1")
	if automaton.Count != 2 || automaton.Progress == nil || automaton.Progress.Done != 1 || automaton.Progress.Total != 2 {
		t.Fatalf("p-1 block = %+v, want 2 sessions, 1 done", automaton)
	}
	story := storyByKey(t, automaton, "st-1")
	if story.Count != 2 {
		t.Fatalf("story st-1 = %+v, want 2 sessions", story)
	}
	if len(story.Sessions) != 2 || story.Sessions[0].StoryID != "st-1" {
		t.Fatalf("story sessions = %+v", story.Sessions)
	}
	if groupByKey(t, g, "ungrouped").Count != 1 {
		t.Fatalf("ungrouped should hold the session without an id: %+v", g.Groups)
	}
}

// TestSessionGroupHandlerRejectsUnknownAxis pins the 400 for a bad axis.
func TestSessionGroupHandlerRejectsUnknownAxis(t *testing.T) {
	s := sessionViewServer(t, 0, 200, &session.Session{ID: "s1", FullID: "testhost/s1", Name: "a", State: session.StateRunning, ProjectDir: "/proj", TmuxSession: "s1"})
	rr := getSessions(t, s, "group_by=bogus")
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("code=%d body=%s, want 400", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "group_by") {
		t.Fatalf("400 body must name group_by: %s", rr.Body.String())
	}
}

// TestSessionGroupLegacyResponseUnchanged pins the contract: without
// group_by the response is the same JSON array it always was.
func TestSessionGroupLegacyResponseUnchanged(t *testing.T) {
	s := sessionViewServer(t, 0, 200,
		&session.Session{ID: "s1", FullID: "testhost/s1", Name: "a", State: session.StateRunning, ProjectDir: "/proj", TmuxSession: "s1", Tags: []string{"automation:worker"}},
		&session.Session{ID: "s2", FullID: "testhost/s2", Name: "b", State: session.StateComplete, ProjectDir: "/proj", TmuxSession: "s2"})

	rr := getSessions(t, s, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.Bytes()
	if len(body) == 0 || body[0] != '[' {
		t.Fatalf("legacy response must stay a JSON array, got %s", strings.TrimSpace(string(body)))
	}
	if bytes.Contains(body, []byte(`"group_by"`)) {
		t.Fatalf("legacy response must not carry the grouping envelope: %s", string(body))
	}
	var legacy []map[string]any
	if err := json.Unmarshal(body, &legacy); err != nil {
		t.Fatalf("decode legacy array: %v", err)
	}
	if len(legacy) != 2 {
		t.Fatalf("legacy array has %d entries, want 2", len(legacy))
	}
	for i, e := range legacy {
		if _, ok := e["id"]; !ok {
			t.Fatalf("legacy entry %d has no id: %v", i, e)
		}
		if _, ok := e["full_id"]; !ok {
			t.Fatalf("legacy entry %d has no full_id: %v", i, e)
		}
	}
}

// groupByKey finds a group and fails the test if absent.
func groupByKey(t *testing.T, g sessionGrouped, key string) sessionGroup {
	t.Helper()
	for _, grp := range g.Groups {
		if grp.Key == key {
			return grp
		}
	}
	t.Fatalf("group %q not in %v", key, groupKeysOf(g))
	return sessionGroup{}
}

// storyByKey finds a story block and fails the test if absent.
func storyByKey(t *testing.T, grp sessionGroup, key string) sessionGroupStory {
	t.Helper()
	for _, st := range grp.Stories {
		if st.Key == key {
			return st
		}
	}
	t.Fatalf("story %q not in %+v", key, grp.Stories)
	return sessionGroupStory{}
}

func groupKeysOf(g sessionGrouped) []string {
	out := make([]string, 0, len(g.Groups))
	for _, grp := range g.Groups {
		out = append(out, grp.Key)
	}
	return out
}

func membersOf(g sessionGrouped) int {
	n := 0
	for _, grp := range g.Groups {
		n += len(grp.Sessions)
	}
	return n
}

func membersOfStories(g sessionGrouped) int {
	n := 0
	for _, grp := range g.Groups {
		for _, st := range grp.Stories {
			n += len(st.Sessions)
		}
	}
	return n
}

func storyIDs(s []sessionGroupMember) []string {
	out := make([]string, 0, len(s))
	for _, m := range s {
		out = append(out, m.ID)
	}
	return out
}
