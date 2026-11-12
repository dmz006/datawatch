package autonomous

import (
	"testing"
	"time"
)

func mustParseTime(s string) time.Time {
	tm, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return tm
}

const triageTextSampleList = `{
  "records": [
    {"prd_id": "p1", "prd_title": "Add paging", "status": "failed",
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
     "notes": [{"at": "2026-01-02T00:12:00Z", "source": "alert", "text": "the triage alert went out"}]},
    {"prd_id": "p2", "prd_title": "Fix login", "status": "blocked",
     "blocked_reason": "waiting on review",
     "retry_budget": {"infra": {"limit": 0, "used": 0, "left": 0},
                      "content": {"limit": 0, "used": 0, "left": 0},
                      "scope": {"limit": 0, "used": 0, "left": 0},
                      "policy": {"limit": 0, "used": 0, "left": 0},
                      "ceiling_total": 0, "ceiling_left": 0},
     "first_seen_at": "2026-01-02T00:02:00Z", "updated_at": "2026-01-02T00:02:00Z"}
  ],
  "alert_minutes": 30
}`

func TestParseTriageList(t *testing.T) {
	tl, err := ParseTriageList([]byte(triageTextSampleList))
	if err != nil {
		t.Fatalf("ParseTriageList: %v", err)
	}
	if len(tl.Records) != 2 || tl.Records[0].PRDID != "p1" || tl.Records[1].PRDID != "p2" {
		t.Errorf("records = %d (want 2, first p1)", len(tl.Records))
	}
	if tl.AlertMinutes != 30 {
		t.Errorf("alert_minutes = %d, want 30", tl.AlertMinutes)
	}
}

func TestParseTriageList_BadInputErrors(t *testing.T) {
	for _, bad := range []string{`{"records": "not a list"}`, `not json`} {
		if _, err := ParseTriageList([]byte(bad)); err == nil {
			t.Errorf("ParseTriageList(%q): expected an error", bad)
		}
	}
	// {} names no records but is a valid (empty) body
	if tl, err := ParseTriageList([]byte(`{"records":null,"alert_minutes":30}`)); err != nil || len(tl.Records) != 0 {
		t.Errorf("a null records list must be a valid empty list, got %d records, err %v", len(tl.Records), err)
	}
}

func TestRenderTriageText_EmptyList(t *testing.T) {
	tl, err := ParseTriageList([]byte(`{"records": [], "alert_minutes": 30}`))
	if err != nil {
		t.Fatal(err)
	}
	out := RenderTriageText(tl)
	for _, want := range []string{"No failed or blocked Automata", "Triage alert threshold: 30 min"} {
		if !contains(out, want) {
			t.Errorf("empty summary lacks %q:\n%s", want, out)
		}
	}
	// 0 means the alert is off, shown as "off" not "0 min".
	tl, _ = ParseTriageList([]byte(`{"records": [], "alert_minutes": 0}`))
	if !contains(RenderTriageText(tl), "Triage alert threshold: off") {
		t.Errorf("0 minutes must render as off:\n%s", RenderTriageText(tl))
	}
}

func TestRenderTriageText_ListAndThreshold(t *testing.T) {
	tl, err := ParseTriageList([]byte(triageTextSampleList))
	if err != nil {
		t.Fatal(err)
	}
	out := RenderTriageText(tl)
	for _, want := range []string{
		"Triage: 2 Automata waiting",
		"(alert threshold: 30 min)",
		`Automaton "Add paging" (id p1) is failed and waiting for operator triage.`,
		`Member of Automation "Nightly build" (id a1), member m1.`,
		"Failed task: the task (id t1)",
		"Task error: boom",
		"Session end reason: operator ended it",
		"Retry budget: infra 0/2 (2 left); content 1/1 (0 left); scope 0/0 (0 left); policy 0/1 (1 left); ceiling 6 left of 7 total",
		"Stuck since 2026-01-02T00:00:00Z.",
		"Last updated 2026-01-02T00:05:00Z.",
		"Alerted at 2026-01-02T00:11:00Z.",
		"Notes:",
		"the triage alert went out",
		`Automaton "Fix login" (id p2) is blocked and waiting for operator triage.`,
		"Blocked reason: waiting on review",
		"Standalone Automaton (not part of an Automation).",
	} {
		if !contains(out, want) {
			t.Errorf("list lacks %q:\n%s", want, out)
		}
	}
}

// TestRenderTriageRecord_MatchesTriageAlertBody pins the shared contract
// the alert sink relies on: the alert body IS the record text. Both are
// rendered by RenderTriageRecord, so a record with every field set must
// stand alone: identity, membership, the failed task with error and end
// reason, the blocked reason, the retry budget, the timestamps, the
// alert stamp and the notes — no field left out.
func TestRenderTriageRecord_MatchesTriageAlertBody(t *testing.T) {
	tl, err := ParseTriageList([]byte(triageTextSampleList))
	if err != nil {
		t.Fatal(err)
	}
	rec := tl.Records[0]
	if got, want := triageAlertBody(rec), RenderTriageRecord(rec); got != want {
		t.Errorf("triageAlertBody must delegate to the shared formatter; got:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderTriageRecord_OmitsEmptyFields(t *testing.T) {
	rec := &TriageRecord{
		PRDID:       "p9",
		PRDTitle:    "No title details",
		Status:      "failed",
		FirstSeenAt: mustParseTime("2026-01-02T09:00:00Z"),
		UpdatedAt:   mustParseTime("2026-01-02T09:00:00Z"),
	}
	out := RenderTriageRecord(rec)
	for _, notWant := range []string{"Notes:", "Alerted at", "Last updated", "Member of Automation", "Blocked reason", "Task error"} {
		if contains(out, notWant) {
			t.Errorf("empty-value record must not show %q:\n%s", notWant, out)
		}
	}
	if !contains(out, "Standalone Automaton (not part of an Automation).") {
		t.Errorf("standalone marker missing:\n%s", out)
	}
}
