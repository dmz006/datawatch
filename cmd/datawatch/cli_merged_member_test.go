package main

import (
	"net/http"
	"strings"
	"testing"
)

// mergedMemberCLISample is the read route body: two decisions, oldest first.
const mergedMemberCLISample = `{"automation_id":"a1","count":2,"merged_members":[
 {"automation_id":"a1","automation_name":"Ship paging","member":"m1","prd_id":"p1","branch":"automaton/x","default_branch":"main","proving_commit":"abc1234","policy":"completed","outcome":"advanced","at":"2026-01-01T00:00:00Z"},
 {"automation_id":"a1","automation_name":"Ship paging","member":"m2","prd_id":"p2","branch":"automaton/y","default_branch":"main","proving_commit":"def5678","policy":"manual","outcome":"blocked","at":"2026-01-02T00:00:00Z"}
]}`

func TestMergedMemberCLI_OneAutomaton(t *testing.T) {
	d := newLaneFillCLIDaemon(t, http.StatusOK, mergedMemberCLISample)
	out, err := runLaneFillCmd(t, newAutonomousMergedMembersCmd(), []string{"a1"})
	if err != nil {
		t.Fatal(err)
	}
	if d.path != "/api/automations/a1/merged-members" || d.query != "" {
		t.Errorf("path=%s query=%q", d.path, d.query)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("want one line per decision, got %d:\n%s", len(lines), out)
	}
	if want := "Ship paging | member m2 blocked | branch automaton/y on main | commit def5678 | policy manual | 2026-01-02T00:00:00Z"; lines[0] != want {
		t.Errorf("newest first: line 1 = %q, want %q", lines[0], want)
	}
	if want := "Ship paging | member m1 advanced | branch automaton/x on main | commit abc1234 | policy completed | 2026-01-01T00:00:00Z"; lines[1] != want {
		t.Errorf("line 2 = %q, want %q", lines[1], want)
	}
}

func TestMergedMemberCLI_AllAutomata(t *testing.T) {
	d := newLaneFillCLIDaemon(t, http.StatusOK, mergedMemberCLISample)
	if _, err := runLaneFillCmd(t, newAutonomousMergedMembersCmd(), nil); err != nil {
		t.Fatal(err)
	}
	if d.path != "/api/automations/merged-members" {
		t.Errorf("path=%s, want the list-all route", d.path)
	}
}

func TestMergedMemberCLI_JSONUnchanged(t *testing.T) {
	newLaneFillCLIDaemon(t, http.StatusOK, mergedMemberCLISample)
	out, err := runLaneFillCmd(t, newAutonomousMergedMembersCmd(), []string{"a1", "--json"})
	if err != nil {
		t.Fatal(err)
	}
	if out != mergedMemberCLISample {
		t.Errorf("--json must print the REST body unchanged:\n%s", out)
	}
}

func TestMergedMemberCLI_Empty(t *testing.T) {
	newLaneFillCLIDaemon(t, http.StatusOK, `{"automation_id":"a1","merged_members":[],"count":0}`)
	out, err := runLaneFillCmd(t, newAutonomousMergedMembersCmd(), []string{"a1"})
	if err != nil {
		t.Fatal(err)
	}
	if out != "No merged-member decisions.\n" {
		t.Errorf("out = %q", out)
	}
}

func TestMergedMemberCLI_ErrorsSurface(t *testing.T) {
	newLaneFillCLIDaemon(t, http.StatusNotFound, "404: Automaton zz not found")
	_, err := runLaneFillCmd(t, newAutonomousMergedMembersCmd(), []string{"zz"})
	if err == nil || !strings.Contains(err.Error(), "HTTP 404") {
		t.Errorf("expected the HTTP 404 to surface, got %v", err)
	}
}

func TestMergedMemberCLI_RegisteredUnderAutonomous(t *testing.T) {
	c, _, err := newAutonomousCmd().Find([]string{"merged-members"})
	if err != nil || c.Name() != "merged-members" {
		t.Fatalf("merged-members is not registered under autonomous: cmd=%v err=%v", c, err)
	}
}
