package main

import (
	"net/http"
	"strings"
	"testing"
)

// decisionsCLISample is the decision line body: an answered decision, an open
// one with a task held on it, and an open one with a long question.
var decisionsCLISample = `{"decisions":[
 {"id":"q3","question":"` + strings.Repeat("x", 150) + `","label":"","state":"open","asked_by":"a","asked_at":"2026-01-03T00:00:00Z","prd_id":"p1","task_ids":[],"waiting_task_ids":[],"waiting_tasks":[],"answer":null},
 {"id":"q2","question":"Which store?","label":"","state":"open","asked_by":"a","asked_at":"2026-01-02T00:00:00Z","prd_id":"p1","task_ids":["t1"],"waiting_task_ids":["t1"],"waiting_tasks":[{"task_id":"t1","wait_reason":"waiting on decision q2"}],"answer":null},
 {"id":"q1","question":"Use sqlite?","label":"","state":"answered","asked_by":"a","asked_at":"2026-01-01T00:00:00Z","prd_id":"p1","task_ids":[],"waiting_task_ids":[],"waiting_tasks":[],
  "answer":{"id":"a1","quote":"yes, use sqlite — \"WAL\" on","quote_sha256":"x","actor":"dmz","surface":"cli","answered_at":"2026-01-01T01:00:00Z"}}
]}`

func TestDecisionsCLI(t *testing.T) {
	t.Run("plain output", func(t *testing.T) {
		d := newLaneFillCLIDaemon(t, http.StatusOK, decisionsCLISample)
		out, err := runLaneFillCmd(t, newAutonomousDecisionsCmd(), []string{"p1", "--state", "Open", "--limit", "5"})
		if err != nil {
			t.Fatal(err)
		}
		if d.path != "/api/autonomous/decisions" {
			t.Errorf("path=%s", d.path)
		}
		got := map[string]string{}
		for _, kv := range strings.Split(d.query, "&") {
			if p := strings.SplitN(kv, "=", 2); len(p) == 2 {
				got[p[0]] = p[1]
			}
		}
		if got["prd_id"] != "p1" || got["state"] != "open" || got["limit"] != "5" {
			t.Errorf("query=%q; want prd_id=p1, state=open, limit=5", d.query)
		}
		lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
		if len(lines) != 3 {
			t.Fatalf("want one line per decision, got %d:\n%s", len(lines), out)
		}
		if want := "q3 open " + strings.Repeat("x", 120); lines[0] != want {
			t.Errorf("long question must be cut to 120 characters: %q", lines[0])
		}
		if want := "q2 open Which store? | waiting: task t1 (waiting on decision q2)"; lines[1] != want {
			t.Errorf("line 2 = %q, want %q", lines[1], want)
		}
		if want := `q1 answered Use sqlite? | answer a1 by dmz: "yes, use sqlite — "WAL" on"`; lines[2] != want {
			t.Errorf("line 3 = %q, want %q", lines[2], want)
		}
	})

	t.Run("json output", func(t *testing.T) {
		newLaneFillCLIDaemon(t, http.StatusOK, decisionsCLISample)
		out, err := runLaneFillCmd(t, newAutonomousDecisionsCmd(), []string{"--json"})
		if err != nil {
			t.Fatal(err)
		}
		if out != decisionsCLISample {
			t.Errorf("--json must print the REST body unchanged:\n%s", out)
		}
	})

	t.Run("bad state refused", func(t *testing.T) {
		d := newLaneFillCLIDaemon(t, http.StatusOK, decisionsCLISample)
		_, err := runLaneFillCmd(t, newAutonomousDecisionsCmd(), []string{"--state", "closed"})
		if err == nil || !strings.Contains(err.Error(), `"open", "answered", "withdrawn" or "expired"`) {
			t.Errorf("expected a refusal naming the four states, got %v", err)
		}
		if d.calls != 0 {
			t.Errorf("nothing may be sent, saw %d calls", d.calls)
		}
	})

	t.Run("empty result", func(t *testing.T) {
		newLaneFillCLIDaemon(t, http.StatusOK, `{"decisions":[]}`)
		out, err := runLaneFillCmd(t, newAutonomousDecisionsCmd(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if out != "No decisions to show.\n" {
			t.Errorf("empty output = %q", out)
		}
	})
}
