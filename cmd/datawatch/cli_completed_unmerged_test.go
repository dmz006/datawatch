package main

import (
	"net/http"
	"strings"
	"testing"
)

func TestCompletedUnmergedCLIPrints(t *testing.T) {
	d := newLaneFillCLIDaemon(t, http.StatusOK, `{"completed_unmerged":[{"id":"a1","branch":"automaton/a1","detail":"not an ancestor"}],"count":1}`)
	out, err := runLaneFillCmd(t, newAutonomousCompletedUnmergedCmd(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if d.path != "/api/autonomous/completed-unmerged" || d.query != "" || d.body != "" {
		t.Errorf("path=%s query=%q body=%q", d.path, d.query, d.body)
	}
	for _, want := range []string{`Automaton a1: branch "automaton/a1" is not merged: not an ancestor`, "1 completed Automaton(s)", "Nothing has been merged"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	raw, err := runLaneFillCmd(t, newAutonomousCompletedUnmergedCmd(), []string{"--json"})
	if err != nil || !strings.Contains(raw, `"count":1`) {
		t.Errorf("--json should print the daemon body, err=%v:\n%s", err, raw)
	}
}

func TestCompletedUnmergedCLIEmpty(t *testing.T) {
	newLaneFillCLIDaemon(t, http.StatusOK, `{"completed_unmerged":[],"count":0}`)
	out, err := runLaneFillCmd(t, newAutonomousCompletedUnmergedCmd(), nil)
	if err != nil || !strings.Contains(out, "No completed Automaton has an unmerged branch.") {
		t.Errorf("err=%v out=%q", err, out)
	}
}

func TestCompletedUnmergedCLIDaemonError(t *testing.T) {
	newLaneFillCLIDaemon(t, http.StatusServiceUnavailable, "autonomous disabled")
	if _, err := runLaneFillCmd(t, newAutonomousCompletedUnmergedCmd(), nil); err == nil {
		t.Error("a refused read must return an error")
	}
}
