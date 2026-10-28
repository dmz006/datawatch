package main

import (
	"net/http"
	"strings"
	"testing"
)

// triageCLISample is the triage read body: one failed Automaton that is a
// member of an Automation (with the full standing: task, error, end
// reason, budget, stamps, notes) and one standalone blocked Automaton.
const triageCLISample = `{"records":[{
  "prd_id": "p1", "prd_title": "Add paging", "status": "failed",
  "automation_id": "a1", "automation_name": "Nightly build", "member_id": "m1",
  "task_id": "t1", "task_title": "the task",
  "error": "boom",
  "session_end_reason": "operator ended it",
  "retry_budget": {"infra": {"limit": 2, "used": 0, "left": 2},
                   "content": {"limit": 1, "used": 1, "left": 0},
                   "scope": {"limit": 0, "used": 0, "left": 0},
                   "policy": {"limit": 1, "used": 0, "left": 1},
                   "ceiling_total": 7, "ceiling_left": 6},
  "first_seen_at": "2026-01-02T00:00:00Z", "updated_at": "2026-01-02T00:05:00Z",
  "alerted_at": "2026-01-02T00:11:00Z",
  "notes": [{"at": "2026-01-02T00:12:00Z", "source": "alert", "text": "the triage alert went out"}]
},{
  "prd_id": "p2", "prd_title": "Fix login", "status": "blocked",
  "blocked_reason": "waiting on review",
  "first_seen_at": "2026-01-02T00:02:00Z", "updated_at": "2026-01-02T00:02:00Z"
}],"alert_minutes": 30}`

func TestTriageCLI_RendersRecordsAndThreshold(t *testing.T) {
	d := newLaneFillCLIDaemon(t, http.StatusOK, triageCLISample)
	out, err := runLaneFillCmd(t, newAutonomousTriageCmd(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if d.path != "/api/autonomous/triage" || d.query != "" {
		t.Errorf("path=%s query=%q; want the triage route with no filters", d.path, d.query)
	}
	for _, want := range []string{
		"Triage: 2 Automata waiting",
		"(alert threshold: 30 min)",
		`Automaton "Add paging" (id p1) is failed and waiting for operator triage.`,
		`Member of Automation "Nightly build" (id a1), member m1.`,
		"Failed task: the task (id t1)",
		"Task error: boom",
		"Session end reason: operator ended it",
		"Stuck since 2026-01-02T00:00:00Z.",
		"Alerted at 2026-01-02T00:11:00Z.",
		"Notes:",
		"the triage alert went out",
		`Automaton "Fix login" (id p2) is blocked and waiting for operator triage.`,
		"Blocked reason: waiting on review",
		"Standalone Automaton (not part of an Automation).",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestTriageCLI_EmptyShowsNothingWaiting(t *testing.T) {
	d := newLaneFillCLIDaemon(t, http.StatusOK, `{"records":[],"alert_minutes":30}`)
	out, err := runLaneFillCmd(t, newAutonomousTriageCmd(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if d.path != "/api/autonomous/triage" || d.query != "" {
		t.Errorf("path=%s query=%q", d.path, d.query)
	}
	if !strings.Contains(out, "No failed or blocked Automata to triage") {
		t.Errorf("empty case must say nothing is waiting:\n%s", out)
	}
	if !strings.Contains(out, "30 min") {
		t.Errorf("the threshold in effect must still show:\n%s", out)
	}
	if strings.Contains(out, `Automaton "`) {
		t.Errorf("no record may be rendered with an empty list:\n%s", out)
	}
}

func TestTriageCLI_FiltersAreSentAsQuery(t *testing.T) {
	d := newLaneFillCLIDaemon(t, http.StatusOK, `{"records":[],"alert_minutes":30}`)
	if _, err := runLaneFillCmd(t, newAutonomousTriageCmd(),
		[]string{"--automation", "a1", "--prd", "p1", "--status", "Blocked"}); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, kv := range strings.Split(d.query, "&") {
		kv2 := strings.SplitN(kv, "=", 2)
		if len(kv2) == 2 {
			got[kv2[0]] = kv2[1]
		}
	}
	if got["automation_id"] != "a1" || got["prd_id"] != "p1" || got["status"] != "blocked" {
		t.Errorf("query=%q; want automation_id=a1, prd_id=p1, status=blocked (canonical form)", d.query)
	}
}

func TestTriageCLI_StatusRejectedLocally(t *testing.T) {
	for _, bad := range []string{"stopped", "done", "gpu"} {
		d := newLaneFillCLIDaemon(t, http.StatusOK, triageCLISample)
		_, err := runLaneFillCmd(t, newAutonomousTriageCmd(), []string{"--status", bad})
		if err == nil || !strings.Contains(err.Error(), `"failed" or "blocked"`) {
			t.Errorf("--status %q: expected a refusal naming the two allowed values, got %v", bad, err)
		}
		if d.calls != 0 {
			t.Errorf("--status %q: nothing may be sent, saw %d calls", bad, d.calls)
		}
	}
}

func TestTriageCLI_JSONPassesBodyUnchanged(t *testing.T) {
	newLaneFillCLIDaemon(t, http.StatusOK, triageCLISample)
	out, err := runLaneFillCmd(t, newAutonomousTriageCmd(), []string{"--json"})
	if err != nil {
		t.Fatal(err)
	}
	if out != triageCLISample {
		t.Errorf("--json must print the body untouched\nGot: %s", out)
	}
}

func TestTriageCLI_ErrorPassthrough(t *testing.T) {
	newLaneFillCLIDaemon(t, http.StatusBadRequest, `{"error":"status must be failed or blocked"}`)
	_, err := runLaneFillCmd(t, newAutonomousTriageCmd(), nil)
	if err == nil || !strings.Contains(err.Error(), "HTTP 400") || !strings.Contains(err.Error(), "status must be failed or blocked") {
		t.Errorf("expected the daemon refusal to pass through, got %v", err)
	}
}

func TestTriageCLI_Registered(t *testing.T) {
	names := map[string]bool{}
	for _, c := range newAutonomousCmd().Commands() {
		names[c.Name()] = true
	}
	if !names["triage"] {
		t.Error("triage is not registered under autonomous")
	}
	want := "triage [--automation ID] [--prd ID] [--status failed|blocked] [--json]"
	if !strings.Contains(newAutonomousCmd().Long, want) {
		t.Errorf("the autonomous help block lacks %q", want)
	}
}
