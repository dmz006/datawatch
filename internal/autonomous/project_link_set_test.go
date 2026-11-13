package autonomous

import (
	"strings"
	"testing"
)

func TestSetPRDProject_SetClearAndDecision(t *testing.T) {
	e := newLinkBackfillEnv(t)
	p := e.prd(t, "to link", e.other)

	got, err := e.m.SetPRDProject(p.ID, "datawatch")
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	if got.Project != "datawatch" || e.project(t, p.ID) != "datawatch" {
		t.Fatalf("link not stored: %q", got.Project)
	}
	stored, _ := e.m.Store().GetPRD(p.ID)
	found := false
	for _, d := range stored.Decisions {
		if d.Kind == projectLinkSetDecisionKind && strings.Contains(d.Note, "datawatch") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no decision entry: %+v", stored.Decisions)
	}
	if proj, src := e.m.EffectiveProject(p.ID); proj != "datawatch" || src != ProjectSourceLink {
		t.Fatalf("effective = %q/%q, want datawatch/link", proj, src)
	}

	// Setting the same value again records nothing new.
	n := len(stored.Decisions)
	if _, err := e.m.SetPRDProject(p.ID, "datawatch"); err != nil {
		t.Fatal(err)
	}
	if again, _ := e.m.Store().GetPRD(p.ID); len(again.Decisions) != n {
		t.Fatal("an unchanged link wrote a decision")
	}

	// An empty project clears the link; the directory source applies again.
	if _, err := e.m.SetPRDProject(p.ID, ""); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if e.project(t, p.ID) != "" {
		t.Fatal("link not cleared")
	}
	if proj, src := e.m.EffectiveProject(p.ID); proj != "other" || src != ProjectSourceDirectory {
		t.Fatalf("effective after clear = %q/%q, want other/directory", proj, src)
	}
}

func TestSetPRDProject_UnknownRefusedListsNamesOnly(t *testing.T) {
	e := newLinkBackfillEnv(t)
	p := e.prd(t, "x", e.other)
	_, err := e.m.SetPRDProject(p.ID, "nope")
	if err == nil {
		t.Fatal("unknown project accepted")
	}
	msg := err.Error()
	if !strings.Contains(msg, `"nope"`) || !strings.Contains(msg, "datawatch") || !strings.Contains(msg, "other") {
		t.Fatalf("message = %q", msg)
	}
	if strings.Contains(msg, e.repo) || strings.Contains(msg, e.other) {
		t.Fatalf("message leaks a directory: %q", msg)
	}
	if e.project(t, p.ID) != "" {
		t.Fatal("refused call changed the record")
	}
	if _, err := e.m.SetPRDProject("missing", "datawatch"); err == nil {
		t.Fatal("unknown Automaton accepted")
	}
}
