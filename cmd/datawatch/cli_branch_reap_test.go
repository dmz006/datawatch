package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

const reapCLISample = `{"reaped":[{"automaton_id":"prd-a","branch":"automaton-prd-a--tasks-fix-t1",
"backup_ref":"refs/datawatch/reap-backup/automaton-prd-a--tasks-fix-t1/1757220000","committed_in":"automaton/prd-a","commits":3,
"reason":"commits are all on the Automaton work branch automaton/prd-a"}],
"waiting":[{"automaton_id":"prd-b","branch":"automaton-prd-b--tasks-fix-t9","reason":"still landing (task in progress)"}]}`

const reapSurfaceCLISample = `{"reaped":[{"automaton_id":"prd-a","branch":"automaton-prd-a--tasks-fix-t1",
"committed_in":"automaton/prd-a","commits":3,"reason":"commits are all on the Automaton work branch automaton/prd-a"}],
"waiting":[{"automaton_id":"prd-b","branch":"automaton-prd-b--tasks-fix-t9","reason":"still landing (task in progress)"}],
"backups":[{"branch":"automaton-prd-a--tasks-fix-t1","ref":"refs/datawatch/reap-backup/automaton-prd-a--tasks-fix-t1/1757220000","sha":"3f2a"}],
"backup_days":30,"computed_at":"2026-01-02T09:30:00Z"}`

const reapRestoreCLISample = `{"branch":"automaton-prd-a--tasks-fix-t1",
"backup_ref":"refs/datawatch/reap-backup/automaton-prd-a--tasks-fix-t1/1757220000","sha":"3f2a1b9c0d"}`

func TestBranchReapCLI_ReportPrints(t *testing.T) {
	d := newLaneFillCLIDaemon(t, http.StatusOK, reapSurfaceCLISample)
	out, err := runLaneFillCmd(t, newAutonomousBranchReapCmd(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if d.path != "/api/autonomous/branch-reap" || d.query != "" {
		t.Errorf("path=%s query=%q; want the report route, no filters", d.path, d.query)
	}
	for _, want := range []string{
		"Branch reaper preview (the report deletes nothing yet)",
		"Would be reaped:",
		`automaton-prd-a--tasks-fix-t1  (Automaton prd-a)`,
		"Held (branch kept):",
		`automaton-prd-b--tasks-fix-t9  (Automaton prd-b)`,
		"still landing (task in progress)",
		"Backup refs (retention 30 whole days):",
		"refs/datawatch/reap-backup/automaton-prd-a--tasks-fix-t1/1757220000 -> 3f2a",
		"(branch automaton-prd-a--tasks-fix-t1)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}

	raw, err := runLaneFillCmd(t, newAutonomousBranchReapCmd(), []string{"--json"})
	if err != nil || !strings.Contains(raw, `"backup_days":30`) {
		t.Errorf("--json should print the daemon body, err=%v:\n%s", err, raw)
	}
}

func TestBranchReapCLI_RunPrintsThePass(t *testing.T) {
	d := newLaneFillCLIDaemon(t, http.StatusOK, reapCLISample)
	out, err := runLaneFillCmd(t, newAutonomousBranchReapRunCmd(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if d.path != "/api/autonomous/branch-reap/run" || d.query != "" {
		t.Errorf("path=%s query=%q; want the run route", d.path, d.query)
	}
	if d.body != "" {
		t.Errorf("run must send no body, got %q", d.body)
	}
	for _, want := range []string{
		"Branch reap pass done",
		"Reaped (each backup ref was written before the delete):",
		`automaton-prd-a--tasks-fix-t1  (Automaton prd-a)`,
		"[backup refs/datawatch/reap-backup/automaton-prd-a--tasks-fix-t1/1757220000]",
		"Held (branch kept):",
		"still landing (task in progress)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}

	raw, err := runLaneFillCmd(t, newAutonomousBranchReapRunCmd(), []string{"--json"})
	if err != nil || !strings.Contains(raw, `"reaped"`) {
		t.Errorf("--json should print the daemon body, err=%v:\n%s", err, raw)
	}
}

func TestBranchReapCLI_RunConflictIsAnError(t *testing.T) {
	d := newLaneFillCLIDaemon(t, http.StatusConflict, "branch reap: another pass is already running")
	if _, err := runLaneFillCmd(t, newAutonomousBranchReapRunCmd(), nil); err == nil {
		t.Error("a 409 from the daemon must surface as an error")
	}
	if d.path != "/api/autonomous/branch-reap/run" {
		t.Errorf("path=%s", d.path)
	}
}

func TestBranchReapCLI_RestoreSendsTheBranch(t *testing.T) {
	d := newLaneFillCLIDaemon(t, http.StatusOK, reapRestoreCLISample)
	out, err := runLaneFillCmd(t, newAutonomousBranchReapRestoreCmd(), []string{"automaton-prd-a--tasks-fix-t1"})
	if err != nil {
		t.Fatal(err)
	}
	if d.path != "/api/autonomous/branch-reap/restore" {
		t.Errorf("path=%s", d.path)
	}
	if !strings.Contains(d.body, `"branch":"automaton-prd-a--tasks-fix-t1"`) {
		t.Errorf("body must carry the branch, got %q", d.body)
	}
	for _, want := range []string{
		"Branch restored from its newest backup ref",
		"branch: automaton-prd-a--tasks-fix-t1",
		"backup ref: refs/datawatch/reap-backup/automaton-prd-a--tasks-fix-t1/1757220000",
		"commit: 3f2a1b9c0d",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}

	raw, err := runLaneFillCmd(t, newAutonomousBranchReapRestoreCmd(), []string{"automaton-prd-a--tasks-fix-t1", "--json"})
	if err != nil || !strings.Contains(raw, `"sha":"3f2a1b9c0d"`) {
		t.Errorf("--json should print the daemon body, err=%v:\n%s", err, raw)
	}
}

func TestBranchReapCLI_RestoreNeverReapedIsAnError(t *testing.T) {
	newLaneFillCLIDaemon(t, http.StatusNotFound, "no backup ref for branch")
	if _, err := runLaneFillCmd(t, newAutonomousBranchReapRestoreCmd(), []string{"never-reaped"}); err == nil {
		t.Error("a 404 from the daemon must surface as an error")
	}
}

func TestBranchReapCLI_RestoreRequiresExactlyOneArg(t *testing.T) {
	cmd := newAutonomousBranchReapRestoreCmd()
	if err := cmd.Execute(); err == nil {
		t.Error("restore without a branch must be refused before anything is sent")
	}
}

// The renderers handle the empty lists, and a zero retention renders the
// default-30 wording.
func TestBranchReapCLI_RenderersHandleEmpty(t *testing.T) {
	got := autonomous.RenderReapReport(autonomous.ReapSurfaceReport{ReapReport: autonomous.ReapReport{}})
	for _, want := range []string{"No branch qualified and nothing was held", "none yet", "retention 30 whole days when unset"} {
		if !strings.Contains(got, want) {
			t.Errorf("empty surface report lacks %q:\n%s", want, got)
		}
	}
	if out := autonomous.RenderReapRun(autonomous.ReapReport{}); !strings.Contains(out, "No branch qualified and nothing was held") {
		t.Errorf("empty run: %s", out)
	}
}

func TestBranchReapCommandRequiresNoArgs(t *testing.T) {
	if _, err := runLaneFillCmd(t, newAutonomousBranchReapCmd(), []string{"surplus"}); err == nil {
		t.Error("the report command must refuse extra arguments")
	}
	if _, err := runLaneFillCmd(t, newAutonomousBranchReapRunCmd(), []string{"surplus"}); err == nil {
		t.Error("the run command must refuse extra arguments")
	}
}
