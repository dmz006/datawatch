package router

import (
	"net/http"
	"strings"
	"testing"
)

// triageCommSample is the triage read body for the comm tests: one failed
// member of an Automation with the full standing (task, error, end
// reason, budget, stamps, notes) and one standalone blocked Automaton.
const triageCommSample = `{"records":[{
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

func TestTriageComm_RendersRecordsAndThreshold(t *testing.T) {
	log, port := laneFillCommDaemon(t, http.StatusOK, triageCommSample)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous triage"), " ")
	if log.path != "/api/autonomous/triage" || log.query != "" {
		t.Errorf("path=%s query=%q; want the triage route with no filters", log.path, log.query)
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
		if !strings.Contains(got, want) {
			t.Errorf("reply lacks %q: %q", want, got)
		}
	}
}

func TestTriageComm_SingleAutomationFilter(t *testing.T) {
	// Filtered to one Automation the reply is that Automation's record
	// and the threshold — the record shows on the automaton itself, and
	// this is the same standing an operator would get from the alert.
	body := `{"records":[{
  "prd_id": "p1", "prd_title": "Add paging", "status": "failed",
  "automation_id": "a1", "automation_name": "Nightly build", "member_id": "m1",
  "task_id": "t1", "task_title": "the task",
  "error": "boom",
  "first_seen_at": "2026-01-02T00:00:00Z", "updated_at": "2026-01-02T00:05:00Z",
  "alerted_at": "2026-01-02T00:11:00Z",
  "notes": [{"at": "2026-01-02T00:12:00Z", "source": "alert", "text": "the triage alert went out"}]
}],"alert_minutes":30}`
	log, port := laneFillCommDaemon(t, http.StatusOK, body)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous triage a1"), " ")
	if log.query != "automation_id=a1" {
		t.Errorf("query=%q; want automation_id=a1 (the arg is an Automation id here)", log.query)
	}
	for _, want := range []string{
		`Automaton "Add paging" (id p1) is failed and waiting for operator triage.`,
		"(alert threshold: 30 min)",
		"the triage alert went out",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("reply lacks %q: %q", want, got)
		}
	}
	if strings.Contains(got, `Fix login`) {
		t.Errorf("the reply must show only the filtered Automation's automata: %q", got)
	}
}

func TestTriageComm_RefusesExtraArgs(t *testing.T) {
	log, port := laneFillCommDaemon(t, http.StatusOK, triageCommSample)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous triage a b c"), " ")
	if !strings.Contains(got, "usage: autonomous triage [automation-id]") {
		t.Errorf("extra args must be refused with the usage, got: %q", got)
	}
	if log.calls != 0 {
		t.Errorf("the refusal must not reach the daemon, got %d calls", log.calls)
	}
}

func TestTriageComm_EmptyListShowsThreshold(t *testing.T) {
	log, port := laneFillCommDaemon(t, http.StatusOK, `{"records":[],"alert_minutes":30}`)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous triage"), " ")
	if log.query != "" {
		t.Errorf("query=%q; want none", log.query)
	}
	for _, w := range []string{"No failed or blocked Automata", "(Triage alert threshold: 30 min)"} {
		if !strings.Contains(got, w) {
			t.Errorf("reply lacks %q: %q", w, got)
		}
	}

	// 0 minutes means the alert is off; shown as off, not 0 min.
	_, port = laneFillCommDaemon(t, http.StatusOK, `{"records":[],"alert_minutes":0}`)
	r = setupPeerRouter(t, port)
	got = strings.Join(r.HandleTestMessage("autonomous triage"), " ")
	if !strings.Contains(got, "off") || strings.Contains(got, "0 min") {
		t.Errorf("alert off must read as off, not a zero minutes: %q", got)
	}
}

func TestTriageComm_DaemonRefusalPassesThrough(t *testing.T) {
	log, port := laneFillCommDaemon(t, http.StatusBadRequest, "status must be failed or blocked")
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous triage"), " ")
	if !strings.Contains(got, "HTTP 400") || !strings.Contains(got, "status must be failed or blocked") {
		t.Errorf("the daemon refusal must pass through, got: %q", got)
	}
	_ = log
}
