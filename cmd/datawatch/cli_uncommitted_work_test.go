package main

import (
	"net/http"
	"strings"
	"testing"
)

// uncommittedCLISample is a status with one held task: two files, one of them
// outside the task's declared files, and a question waiting for an answer.
const uncommittedCLISample = `{
  "policy": "hold",
  "scope": "task_files",
  "held": [
    {
      "automaton_id": "abcd1234",
      "name": "Docs refresh",
      "task_id": "t7",
      "task_title": "Rewrite the intro",
      "kind": "task",
      "files": ["docs/intro.md", "docs/extra.md"],
      "outside_scope": ["docs/extra.md"],
      "reason": "the task left files uncommitted",
      "question_id": "q42",
      "held_at": "2026-11-01T09:00:00Z"
    }
  ]
}`

func TestUncommittedCLIPrintsTable(t *testing.T) {
	d := newLaneFillCLIDaemon(t, http.StatusOK, uncommittedCLISample)
	out, err := runLaneFillCmd(t, newAutonomousUncommittedWorkCmd(), []string{"--automaton", "abcd1234"})
	if err != nil {
		t.Fatal(err)
	}
	if d.path != "/api/autonomous/uncommitted-work" || d.query != "automaton=abcd1234" {
		t.Errorf("path=%s query=%q; want the uncommitted-work route with the automaton filter", d.path, d.query)
	}
	for _, want := range []string{
		"Automaton uncommitted work (policy: hold, scope: task_files):",
		"Rewrite the intro",
		"docs/intro.md",
		"docs/extra.md",
		"q42",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("summary lacks %q:\n%s", want, out)
		}
	}
}

func TestUncommittedCLIJSON(t *testing.T) {
	d := newLaneFillCLIDaemon(t, http.StatusOK, uncommittedCLISample)
	out, err := runLaneFillCmd(t, newAutonomousUncommittedWorkCmd(), []string{"--json"})
	if err != nil {
		t.Fatal(err)
	}
	if out != uncommittedCLISample {
		t.Errorf("--json must print the body untouched\ngot:  %q\nwant: %q", out, uncommittedCLISample)
	}
	if d.query != "" {
		t.Errorf("query=%q; want no filter without --automaton", d.query)
	}
	names := map[string]bool{}
	for _, c := range newAutonomousCmd().Commands() {
		names[c.Name()] = true
	}
	if !names["uncommitted-work"] {
		t.Error("uncommitted-work is not registered under autonomous")
	}
}
