package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

const mergeCLISample = `{"mode":"shadow","alert_minutes":30,
"automata":[{"automaton_id":"p1","name":"Add paging","branch":"automaton/p1","state":"pending","commits_ahead":3,
  "pending_since":"2026-01-02T00:00:00Z","alerted_at":null,"would_alert":true}],
"shadow_decisions":[{"at":"2026-01-02T00:40:00Z","automaton_id":"p1","name":"Add paging","branch":"automaton/p1",
  "state":"pending","commits_ahead":3,"would_alert":true,"repeat_count":4,"last_seen_at":"2026-01-02T01:00:00Z"}]}`

const safeToDeleteCLISample = `{"safe":[{"branch":"automaton/p2","worktree":"/work/p2","reason":"merged into main","commits":0}],
"held":[{"branch":"automaton/p1","worktree":"","state":"pending"},
        {"branch":"automaton/p3","worktree":"/work/p3","state":"merged","dirty":true,"note":"Automaton worktree has uncommitted changes"}],
"held_dirty":1}`

func TestMergeCLI_MergeCommandPrints(t *testing.T) {
	d := newLaneFillCLIDaemon(t, http.StatusOK, mergeCLISample)
	out, err := runLaneFillCmd(t, newAutonomousMergeCmd(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if d.path != "/api/autonomous/merge" || d.query != "" {
		t.Errorf("path=%s query=%q; want the merge route with no filters", d.path, d.query)
	}
	for _, want := range []string{
		"mode shadow (alert threshold: 30 min)",
		`Automaton "Add paging" (id p1): branch automaton/p1 is pending, 3 commit(s) ahead of main`,
		"merge pending since 2026-01-02T00:00:00Z",
		"would alert",
		"Shadow decisions",
		"seen 4 times",
		"Nothing has been merged, deleted or pushed.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}

	raw, err := runLaneFillCmd(t, newAutonomousMergeCmd(), []string{"--json"})
	if err != nil || !strings.Contains(raw, `"mode":"shadow"`) {
		t.Errorf("--json should print the daemon body, err=%v:\n%s", err, raw)
	}
}

func TestMergeCLI_SafeToDeleteCommandPrints(t *testing.T) {
	d := newLaneFillCLIDaemon(t, http.StatusOK, safeToDeleteCLISample)
	out, err := runLaneFillCmd(t, newAutonomousSafeToDeleteCmd(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if d.path != "/api/autonomous/safe-to-delete" || d.query != "" {
		t.Errorf("path=%s query=%q", d.path, d.query)
	}
	for _, want := range []string{
		"safe to delete: 1, held back: 2",
		"automaton/p2 (0 commit(s)): merged into main",
		"worktree: /work/p2",
		"automaton/p1: pending",
		"automaton/p3: merged, uncommitted changes",
		"1 branch(es) would otherwise be safe",
		"nothing has been deleted",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestMergeCLI_RendererHandlesEmptyList(t *testing.T) {
	d := newLaneFillCLIDaemon(t, http.StatusOK, `{"safe":[],"held":[],"held_dirty":0}`)
	out, err := runLaneFillCmd(t, newAutonomousSafeToDeleteCmd(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if d.path != "/api/autonomous/safe-to-delete" {
		t.Errorf("path=%s", d.path)
	}
	if !strings.Contains(out, "No finished Automaton branches or worktrees to review.") {
		t.Errorf("empty report text wrong:\n%s", out)
	}

	// Null lists, an empty merge list and the off mode all render.
	if got := autonomous.RenderSafeToDelete(autonomous.SafeDeleteReport{}); !strings.Contains(got, "No finished Automaton") {
		t.Errorf("nil lists: %s", got)
	}
	got := autonomous.RenderMergeState(autonomous.MergeStatus{Mode: "on", AlertMinutes: 0})
	if !strings.Contains(got, "No finished Automata with a work branch.") || !strings.Contains(got, "threshold: off") {
		t.Errorf("empty merge list: %s", got)
	}
	got = autonomous.RenderMergeState(autonomous.MergeStatus{Mode: "off"})
	if !strings.Contains(got, "merge-pending check is off") {
		t.Errorf("off mode: %s", got)
	}
}
