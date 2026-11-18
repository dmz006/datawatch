package router

import (
	"net/http"
	"strings"
	"testing"
)

func TestProjectLinkCommParse(t *testing.T) {
	for _, tc := range []struct{ in, text string }{
		{"automaton project p1 datawatch", "project p1 datawatch"},
		{"automaton project p1 clear", "project p1 clear"},
		{"automaton project-backfill", "project-backfill"},
		{"automaton project-backfill apply", "project-backfill apply"},
	} {
		cmd := Parse(tc.in)
		if cmd.Type != CmdAutonomous || cmd.Text != tc.text {
			t.Errorf("%q -> %s %q, want autonomous %q", tc.in, cmd.Type, cmd.Text, tc.text)
		}
	}
}

func TestProjectLinkCommSetAndClear(t *testing.T) {
	r, rec := newFailureEscalationRouter(t)
	out := r.HandleTestMessage("automaton project p1 datawatch")
	if strings.Contains(strings.Join(out, "\n"), "failed") {
		t.Fatalf("reply = %v", out)
	}
	if rec.method != http.MethodPost || rec.path != "/api/autonomous/prds/p1/set_project" || rec.body["project"] != "datawatch" {
		t.Fatalf("request = %s %s %v", rec.method, rec.path, rec.body)
	}
	r.HandleTestMessage("automaton project p1 clear")
	if rec.body["project"] != "" {
		t.Fatalf("clear body = %v", rec.body)
	}
	hits := rec.hits
	out = r.HandleTestMessage("automaton project p1")
	if rec.hits != hits || !strings.Contains(strings.Join(out, "\n"), "usage: automaton project") {
		t.Fatalf("missing name must give usage and send nothing: %v", out)
	}
}

func TestProjectLinkCommBackfill(t *testing.T) {
	r, rec := newFailureEscalationRouter(t)
	r.HandleTestMessage("automaton project-backfill")
	if rec.path != "/api/autonomous/project-link-backfill" || rec.body["apply"] != false {
		t.Fatalf("dry run: %s %v", rec.path, rec.body)
	}
	r.HandleTestMessage("automaton project-backfill apply")
	if rec.body["apply"] != true {
		t.Fatalf("apply: %v", rec.body)
	}
	hits := rec.hits
	out := r.HandleTestMessage("autonomous project-backfill nonsense")
	if rec.hits != hits || !strings.Contains(strings.Join(out, "\n"), "usage: automaton project-backfill") {
		t.Fatalf("bad argument: %v", out)
	}
}

func TestProjectLinkCommHelpListsVerbs(t *testing.T) {
	r, _ := newFailureEscalationRouter(t)
	out := strings.Join(r.HandleTestMessage("help"), "\n")
	for _, want := range []string{"automaton project <id> <name|clear>", "automaton project-backfill [apply]"} {
		if !strings.Contains(out, want) {
			t.Errorf("help missing %q", want)
		}
	}
}
