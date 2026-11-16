package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

func TestProjectLinkCLIBodies(t *testing.T) {
	if !reflect.DeepEqual(projectLinkBody(" datawatch "), map[string]string{"project": "datawatch"}) {
		t.Error("project body")
	}
	if !reflect.DeepEqual(projectLinkBody(""), map[string]string{"project": ""}) {
		t.Error("empty project must clear")
	}
	if !reflect.DeepEqual(projectLinkBackfillBody(false), map[string]bool{"apply": false}) ||
		!reflect.DeepEqual(projectLinkBackfillBody(true), map[string]bool{"apply": true}) {
		t.Error("backfill body")
	}
}

func TestProjectLinkCLISetProject(t *testing.T) {
	path, body, err := runKindCmd(t, newAutonomousPRDSetProjectCmd(), []string{"p1", "datawatch"})
	if err != nil || path != "/api/autonomous/prds/p1/set_project" || body["project"] != "datawatch" {
		t.Fatalf("err=%v path=%s body=%v", err, path, body)
	}
	_, body, err = runKindCmd(t, newAutonomousPRDSetProjectCmd(), []string{"p1", ""})
	if err != nil || body["project"] != "" {
		t.Fatalf("clear: err=%v body=%v", err, body)
	}
	if _, _, err := runKindCmd(t, newAutonomousPRDSetProjectCmd(), []string{"p1"}); err == nil {
		t.Error("missing project argument accepted")
	}
}

func TestProjectLinkCLIBackfillSendsApplyFlag(t *testing.T) {
	path, body, err := runKindCmd(t, newAutonomousProjectLinkBackfillCmd(), nil)
	if err != nil || path != "/api/autonomous/project-link-backfill" || body["apply"] != false {
		t.Fatalf("dry run: err=%v path=%s body=%v", err, path, body)
	}
	_, body, err = runKindCmd(t, newAutonomousProjectLinkBackfillCmd(), []string{"--apply"})
	if err != nil || body["apply"] != true {
		t.Fatalf("apply: err=%v body=%v", err, body)
	}
}

func TestProjectLinkCLIReportTable(t *testing.T) {
	rep := autonomous.ProjectLinkBackfillReport{
		Project: "datawatch",
		Entries: []autonomous.ProjectLinkBackfillEntry{
			{ID: "a1", Title: "linked", Source: "directory", Project: "datawatch"},
			{ID: "b2", Title: "left", Skipped: true, Reason: "matches no datawatch source"},
		},
		Counts: autonomous.ProjectLinkBackfillCounts{Scanned: 2, Matched: 1, Skipped: 1},
	}
	out := formatProjectLinkReport(rep)
	for _, want := range []string{"dry run", "AUTOMATON", "would link", "skipped: matches no datawatch source", "scanned 2, matched 1, applied 0, skipped 1, failed 0"} {
		if !strings.Contains(out, want) {
			t.Errorf("table missing %q:\n%s", want, out)
		}
	}
	rep.Apply = true
	rep.Entries[0].Applied = true
	if out := formatProjectLinkReport(rep); !strings.Contains(out, "applied") || !strings.Contains(out, "linked") {
		t.Errorf("apply table:\n%s", out)
	}
}
