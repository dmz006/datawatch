package server

import (
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// projEvFixture: project "alpha" at /work/alpha with one arc (automation
// "auto-arc"). Automata:
//   - prdArc:  held by the arc's automation via PRD.AutomationID
//   - prdMemb: held by the arc's automation only as a member
//   - prdDir:  no arc, ProjectDir matches /work/alpha -> unassigned, in scope
//   - prdOther: another project's dir, not in an arc -> out of scope
//   - prdTmpl: template in the project's dir -> never in scope
//   - prdOrphanArc: held by a non-arc automation with another dir -> out
func projEvFixture() projectEventInput {
	sessions := map[string][]string{
		"prdArc":   {"sess-arc"},
		"prdMemb":  {"sess-memb"},
		"prdDir":   {"sess-dir"},
		"prdOther": {"sess-other"},
		"prdTmpl":  {"sess-tmpl"},
	}
	return projectEventInput{
		Dir:  "/work/alpha/",
		Arcs: []config.ProjectArc{{AutomationID: "auto-arc"}},
		PRDs: []*autonomous.PRD{
			{ID: "prdArc", AutomationID: "auto-arc", ProjectDir: "/elsewhere"},
			{ID: "prdMemb", ProjectDir: "/elsewhere"},
			{ID: "prdDir", ProjectDir: "/work/alpha"},
			{ID: "prdOther", ProjectDir: "/work/beta"},
			{ID: "prdTmpl", ProjectDir: "/work/alpha", IsTemplate: true},
			{ID: "prdOrphanArc", AutomationID: "auto-other", ProjectDir: "/work/beta"},
		},
		Automations: []*autonomous.Automation{
			{ID: "auto-arc", Members: []autonomous.AutomationMember{{PRDID: "prdMemb"}}},
			{ID: "auto-other", Members: []autonomous.AutomationMember{{PRDID: "prdOrphanArc"}}},
		},
		SessionIDs: func(id string) []string { return sessions[id] },
	}
}

func TestBuildProjectEventScope_Membership(t *testing.T) {
	sc := buildProjectEventScope(projEvFixture())
	for _, id := range []string{"prdArc", "prdMemb", "prdDir"} {
		if !sc.PRDs[id] {
			t.Errorf("PRD %s should be in project scope", id)
		}
	}
	for _, id := range []string{"prdOther", "prdTmpl", "prdOrphanArc"} {
		if sc.PRDs[id] {
			t.Errorf("PRD %s should be out of project scope", id)
		}
	}
	if !sc.Automations["auto-arc"] || sc.Automations["auto-other"] {
		t.Errorf("automations scope = %v, want only auto-arc", sc.Automations)
	}
	for _, sid := range []string{"sess-arc", "sess-memb", "sess-dir"} {
		if !sc.Sessions[sid] {
			t.Errorf("session %s should be in scope", sid)
		}
	}
	if sc.Sessions["sess-other"] || sc.Sessions["sess-tmpl"] {
		t.Error("sessions of out-of-scope Automata must not be in scope")
	}
}

func TestBuildProjectEventScope_EmptyDirAndNilSessions(t *testing.T) {
	in := projEvFixture()
	in.Dir = ""
	in.SessionIDs = nil
	sc := buildProjectEventScope(in)
	if sc.PRDs["prdDir"] {
		t.Error("empty project dir must not match by ProjectDir")
	}
	if !sc.PRDs["prdArc"] {
		t.Error("arc-held Automaton should stay in scope without a dir")
	}
	if len(sc.Sessions) != 0 {
		t.Errorf("nil SessionIDs should give no sessions, got %v", sc.Sessions)
	}
}

func TestFilterProjectEvents_MatchesEachIdentifier(t *testing.T) {
	sc := buildProjectEventScope(projEvFixture())
	events := []autonomous.LifecycleEvent{
		{ID: "e1", PRDID: "prdArc"},                            // by PRD
		{ID: "e2", PRDID: "prdOther"},                          // out
		{ID: "e3", PRDID: "prdOther", ParentPRDID: "prdDir"},   // by parent PRD
		{ID: "e4", AutomationID: "auto-arc"},                   // by automation
		{ID: "e5", AutomationID: "auto-other"},                 // out
		{ID: "e6", PRDID: "prdOther", SessionID: "sess-dir"},   // by session
		{ID: "e7", PRDID: "prdOther", SessionID: "sess-other"}, // out
		{ID: "e8"}, // no identifiers -> out
	}
	got := filterProjectEvents(sc, events)
	want := []string{"e1", "e3", "e4", "e6"}
	if len(got) != len(want) {
		t.Fatalf("kept %d events, want %d: %+v", len(got), len(want), got)
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Errorf("kept[%d] = %s, want %s (order must be preserved)", i, got[i].ID, id)
		}
	}
}

func TestFilterProjectEvents_EmptyInputNeverNil(t *testing.T) {
	sc := buildProjectEventScope(projEvFixture())
	if got := filterProjectEvents(sc, nil); got == nil || len(got) != 0 {
		t.Errorf("empty input should give a non-nil empty slice, got %#v", got)
	}
	none := filterProjectEvents(sc, []autonomous.LifecycleEvent{{ID: "x", PRDID: "nope"}})
	if none == nil || len(none) != 0 {
		t.Errorf("no matches should give a non-nil empty slice, got %#v", none)
	}
}
