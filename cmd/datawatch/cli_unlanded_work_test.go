package main

import (
	"net/http"
	"strings"
	"testing"
)

const unlandedCLISample = `{"branches":[{"automaton_id":"a1","branch":"automaton/a1","title":"Docs refresh","commits":2,"last_activity":"2026-11-01T09:00:00Z","recommendation":"merge","reason":"nothing conflicts"}]}`

func TestUnlandedCLIListsTable(t *testing.T) {
	d := newLaneFillCLIDaemon(t, http.StatusOK, unlandedCLISample)
	out, err := runLaneFillCmd(t, newAutonomousUnlandedWorkCmd(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if d.path != "/api/autonomous/unlanded-work" {
		t.Errorf("path=%s", d.path)
	}
	for _, want := range []string{"Docs refresh (automaton/a1)", "2 commits", "recommended: merge"} {
		if !strings.Contains(out, want) {
			t.Errorf("list lacks %q:\n%s", want, out)
		}
	}
	out, err = runLaneFillCmd(t, newAutonomousUnlandedWorkCmd(), []string{"--json"})
	if err != nil || !strings.Contains(out, `"branches"`) {
		t.Errorf("json out=%q err=%v", out, err)
	}
}

func TestUnlandedCLIActions(t *testing.T) {
	d := newLaneFillCLIDaemon(t, http.StatusOK, `{"branch":"automaton/a1","kept":true}`)
	out, err := runLaneFillCmd(t, newAutonomousUnlandedWorkCmd(), []string{"keep", "automaton/a1", "--note", "later"})
	if err != nil {
		t.Fatal(err)
	}
	if d.path != "/api/autonomous/unlanded-work/keep" || !strings.Contains(d.body, `"note":"later"`) || !strings.Contains(out, "Kept automaton/a1") {
		t.Errorf("path=%s body=%s out=%s", d.path, d.body, out)
	}

	d = newLaneFillCLIDaemon(t, http.StatusOK, `{"branch":"automaton/a1","archive_ref":"refs/archive/a1"}`)
	out, err = runLaneFillCmd(t, newAutonomousUnlandedWorkCmd(), []string{"archive-branch", "automaton/a1"})
	if err != nil || d.path != "/api/autonomous/unlanded-work/archive-branch" || !strings.Contains(out, "refs/archive/a1") {
		t.Errorf("path=%s out=%s err=%v", d.path, out, err)
	}

	d = newLaneFillCLIDaemon(t, http.StatusOK, `{"branch":"automaton/a1","bundle_path":"/b/a1.bundle","worktree_path":"/w/a1"}`)
	if _, err = runLaneFillCmd(t, newAutonomousUnlandedWorkCmd(), []string{"delete-worktree", "automaton/a1"}); err == nil {
		t.Error("delete-worktree without --confirm must fail")
	}
	if d.calls != 0 {
		t.Error("an unconfirmed delete must not reach the daemon")
	}
	out, err = runLaneFillCmd(t, newAutonomousUnlandedWorkCmd(), []string{"delete-worktree", "automaton/a1", "--confirm"})
	if err != nil || !strings.Contains(d.body, `"confirm":true`) || !strings.Contains(out, "/b/a1.bundle") || !strings.Contains(out, "/w/a1") {
		t.Errorf("body=%s out=%s err=%v", d.body, out, err)
	}
}

func TestUnlandedCLIDaemonError(t *testing.T) {
	newLaneFillCLIDaemon(t, http.StatusNotFound, "branch is not listed")
	if _, err := runLaneFillCmd(t, newAutonomousUnlandedWorkCmd(), []string{"keep", "x"}); err == nil {
		t.Error("a daemon refusal must be an error")
	}
	if _, err := runLaneFillCmd(t, newAutonomousUnlandedWorkCmd(), nil); err == nil {
		t.Error("a list refusal must be an error")
	}
}
