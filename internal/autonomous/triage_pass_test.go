package autonomous

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// The triage pass shares the coordinator harness from
// automation_coordinator_test.go; this file adds a fixed clock, a pinned
// triage threshold and a few record/alert readers on top.

var triageBase = time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)

// triageHarness pins the coordinator clock and the triage alert
// threshold so a triage pass is fully deterministic.
type triageHarness struct {
	h     *coordHarness
	clock time.Time
}

func newTriageHarness(t *testing.T, nPRDs, thresholdMinutes int) *triageHarness {
	t.Helper()
	h := newCoordHarness(t, nPRDs)
	th := &triageHarness{h: h, clock: triageBase}
	h.c.now = func() time.Time { return th.clock }
	g := h.m.Config()
	g.Automation.TriageAlertMinutes = &thresholdMinutes
	g.RetryPolicy = map[string]int{"infra": 2, "content": 1, "scope": 1, "policy": 1}
	g.RetryCeilingTotal = 7
	h.m.SetConfig(g)
	return th
}

func (h *triageHarness) advance(d time.Duration) { h.clock = h.clock.Add(d) }

func (h *triageHarness) assertNoAlerts(t *testing.T, prefix string) {
	t.Helper()
	h.assertAlerts(t, 0, prefix)
}

func (h *triageHarness) assertAlerts(t *testing.T, n int, prefix string) {
	t.Helper()
	if got := h.h.alertCount(prefix); got != n {
		h.h.mu.Lock()
		alerts := append([]string(nil), h.h.alerts...)
		h.h.mu.Unlock()
		t.Fatalf("alerts matching %q = %d, want %d; got %v", prefix, got, n, alerts)
	}
}

func (h *triageHarness) failPRD(t *testing.T, idx int, taskErr, endReason string) {
	t.Helper()
	p, ok := h.h.m.store.GetPRD(h.h.prds[idx].ID)
	if !ok {
		t.Fatalf("prd %d not found", idx)
	}
	p.Status = PRDFailed
	p.Story = []Story{{Tasks: []Task{{
		ID: fmt.Sprintf("task-%d", idx), Title: "the task", Status: TaskFailed,
		Error: taskErr, SessionEndReason: endReason, UpdatedAt: h.clock,
	}}}}
	if err := h.h.m.store.SavePRD(p); err != nil {
		t.Fatalf("save prd: %v", err)
	}
}

func (h *triageHarness) blockedPRD(t *testing.T, idx int, reason string) {
	t.Helper()
	p, ok := h.h.m.store.GetPRD(h.h.prds[idx].ID)
	if !ok {
		t.Fatalf("prd %d not found", idx)
	}
	p.Status = PRDBlocked
	p.BlockedReason = reason
	if err := h.h.m.store.SavePRD(p); err != nil {
		t.Fatalf("save prd: %v", err)
	}
}

func (h *triageHarness) setStatus(t *testing.T, idx int, s PRDStatus) {
	t.Helper()
	p, ok := h.h.m.store.GetPRD(h.h.prds[idx].ID)
	if !ok {
		t.Fatalf("prd %d not found", idx)
	}
	p.Status = s
	if err := h.h.m.store.SavePRD(p); err != nil {
		t.Fatalf("save prd: %v", err)
	}
}

func (h *triageHarness) record(t *testing.T, idx int) *TriageRecord {
	t.Helper()
	p, ok := h.h.m.store.GetPRD(h.h.prds[idx].ID)
	if !ok {
		t.Fatalf("prd %d not found", idx)
	}
	return p.Triage
}

func (h *triageHarness) alertEntry(t *testing.T, prefix string) string {
	t.Helper()
	h.h.mu.Lock()
	defer h.h.mu.Unlock()
	out := ""
	for _, e := range h.h.alerts {
		if strings.HasPrefix(e, prefix) {
			out = e
		}
	}
	if out == "" {
		t.Fatalf("no alert with prefix %q; got %v", prefix, h.h.alerts)
	}
	return out
}

func containsAll(t *testing.T, body string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(body, w) {
			t.Fatalf("alert body missing %q; body:\n%s", w, body)
		}
	}
}

// A standalone failed Automaton gets its record filled in on the first
// pass, refreshed without duplication on later passes, alerted exactly
// once after the threshold, and never re-alerted.
func TestTriagePass_SandaloneFailed_FillsRefreshesAlertsOnce(t *testing.T) {
	h := newTriageHarness(t, 1, 10)
	h.failPRD(t, 0, "boom", "operator ended it")

	// First pass: record appears, no alert yet (not stood long enough).
	h.h.c.triagePass()
	rec := h.record(t, 0)
	if rec == nil {
		t.Fatal("no triage record written on first pass")
	}
	if rec.TaskID != "task-0" || rec.TaskTitle != "the task" {
		t.Fatalf("record task = %q %q", rec.TaskID, rec.TaskTitle)
	}
	if rec.Error != "boom" || rec.SessionEndReason != "operator ended it" {
		t.Fatalf("record = err=%q reason=%q", rec.Error, rec.SessionEndReason)
	}
	if got := rec.RetryBudget.Infra; got != (TriageRetryCategory{Limit: 2, Used: 0, Left: 2}) {
		t.Fatalf("infra budget = %+v, want {Limit:2 Used:0 Left:2}", got)
	}
	if got := rec.RetryBudget; got.CeilingTotal != 7 || got.CeilingLeft != 7 {
		t.Fatalf("ceiling = %+v, want 7 left of 7 total", got)
	}
	if rec.Status != string(PRDFailed) {
		t.Fatalf("rec.Status = %q", rec.Status)
	}
	if rec.AlertedAt != nil {
		t.Fatal("AlertedAt set before the threshold stood")
	}
	h.assertNoAlerts(t, "warn|Triage: ")

	// Within the threshold: refreshed in place, still no alert.
	h.advance(9 * time.Minute)
	h.h.c.triagePass()
	rec2 := h.record(t, 0)
	if rec2.FirstSeenAt != rec.FirstSeenAt || rec2.UpdatedAt != triageBase.Add(9*time.Minute) {
		t.Fatalf("refresh did not keep FirstSeenAt=%v and move UpdatedAt to %v (got %v, %v)",
			rec.FirstSeenAt, triageBase.Add(9*time.Minute), rec2.FirstSeenAt, rec2.UpdatedAt)
	}
	h.assertNoAlerts(t, "warn|Triage: ")

	// Past the threshold: exactly one alert, record stamped AlertedAt.
	h.advance(2 * time.Minute)
	h.h.c.triagePass()
	h.assertAlerts(t, 1, "warn|Triage: ")
	entry := h.alertEntry(t, "warn|Triage: ")
	body := entry[strings.LastIndex(entry, "|")+1:]
	title := entry[strings.Index(entry, "|")+1 : strings.LastIndex(entry, "|")]
	wantTitle := fmt.Sprintf("Triage: Automaton %q failed", h.h.prds[0].Title)
	if title != wantTitle {
		t.Fatalf("alert title = %q, want %q", title, wantTitle)
	}
	containsAll(t, body,
		"failed", "Failed task: the task (id task-0)",
		"Task error: boom", "Session end reason: operator ended it",
		"Retry budget:", "Stuck since",
	)
	if strings.Contains(body, "Member of Automation") {
		t.Fatalf("standalone alert body must not name a member: %s", body)
	}
	if !strings.Contains(body, "Standalone Automaton") {
		t.Fatalf("standalone alert body should say standalone: %s", body)
	}
	if got := h.record(t, 0).AlertedAt; got == nil || !got.Equal(triageBase.Add(11*time.Minute)) {
		t.Fatalf("AlertedAt = %v, want %v", got, triageBase.Add(11*time.Minute))
	}

	// Further passes: no second alert, record not duplicated.
	h.advance(30 * time.Minute)
	h.h.c.triagePass()
	h.advance(time.Minute)
	h.h.c.triagePass()
	h.assertAlerts(t, 1, "warn|Triage: ")
	if got := len(h.record(t, 0).Notes); got != 0 {
		t.Fatalf("record Notes = %d entries, want 0", got)
	}
}

// A failed member of an Automation that still exists is skipped while
// the coordinator is not ready; once ready it records and alerts like a
// standalone one, and its alert body names the owning Automation and
// member.
func TestTriagePass_MemberOfAutomation_WaitsForReadyThenAlerts(t *testing.T) {
	h := newTriageHarness(t, 2, 10)
	a := h.h.force(h.h.makeAuto(nil, 1)) // h.prds[0] is the only member
	h.failPRD(t, 0, "member blew up", "rate limited")

	if h.h.c.Ready() {
		t.Fatal("harness coordinator should not be ready by default")
	}
	h.h.c.triagePass()
	if rec := h.record(t, 0); rec != nil {
		t.Fatalf("record written while coordinator not ready: %+v", rec)
	}
	h.assertNoAlerts(t, "warn|Triage: ")

	h.h.c.SetReady()
	h.h.c.triagePass()
	rec := h.record(t, 0)
	if rec == nil {
		t.Fatal("no record after recovery (ready)")
	}
	if rec.AutomationID != a.ID || rec.MemberID == "" || rec.AutomationName != a.Name {
		t.Fatalf("record context = auto %q (%s) member %q (%s), want auto %q (%s)",
			rec.AutomationName, rec.AutomationID, rec.MemberID, rec.MemberID, a.Name, a.ID)
	}
	h.advance(11 * time.Minute)
	h.h.c.triagePass()
	h.assertAlerts(t, 1, "warn|Triage: ")
	entry := h.alertEntry(t, "warn|Triage: ")
	body := entry[strings.LastIndex(entry, "|")+1:]
	if !strings.Contains(body, fmt.Sprintf("Member of Automation %q (id %s)", a.Name, a.ID)) {
		t.Fatalf("member alert body must name the owning Automation:\n%s", body)
	}
	containsAll(t, body, "Task error: member blew up", "Session end reason: rate limited")
}

// An Automaton that leaves the failed state has its record cleared, and
// completed/cancelled/rejected ones are skipped without one.
func TestTriagePass_ClearsRecordWhenLeavingFailed(t *testing.T) {
	h := newTriageHarness(t, 3, 10)
	h.failPRD(t, 0, "boom", "")
	h.h.c.triagePass()
	if h.record(t, 0) == nil {
		t.Fatal("expected a record for the failed automaton")
	}
	for _, s := range []PRDStatus{PRDCompleted, PRDCancelled, PRDRejected} {
		h.setStatus(t, 0, s)
		h.h.c.triagePass()
		if rec := h.record(t, 0); rec != nil {
			t.Fatalf("status %s: record not cleared (got %+v)", s, rec)
		}
	}
	// Already terminal from the start: never records.
	h.setStatus(t, 1, PRDCompleted)
	h.h.c.triagePass()
	if rec := h.record(t, 1); rec != nil {
		t.Fatalf("completed automaton got a record: %+v", rec)
	}
}

// TriageAlertMinutes = 0 turns the alert off: the record is still kept,
// refreshed, cleared — but never raises.
func TestTriagePass_AlertDisabledWhenThresholdZero(t *testing.T) {
	h := newTriageHarness(t, 1, 0)
	h.failPRD(t, 0, "boom", "")
	h.h.c.triagePass()
	if h.record(t, 0) == nil {
		t.Fatal("record should still be written when alerts are off")
	}
	h.advance(24 * time.Hour)
	h.h.c.triagePass()
	h.advance(24 * time.Hour)
	h.h.c.triagePass()
	h.assertNoAlerts(t, "warn|Triage: ")
	if got := h.record(t, 0).AlertedAt; got != nil {
		t.Fatalf("AlertedAt = %v with alerts off", got)
	}
}

// A record that was cleared because the Automaton recovered, then a new
// failure on the same Automaton, is a new episode that alerts again.
func TestTriagePass_NewEpisodeAlertsAgain(t *testing.T) {
	h := newTriageHarness(t, 1, 10)
	h.failPRD(t, 0, "first", "")
	h.h.c.triagePass()
	h.advance(11 * time.Minute)
	h.h.c.triagePass()
	h.assertAlerts(t, 1, "warn|Triage: ")

	h.setStatus(t, 0, PRDCompleted)
	h.h.c.triagePass()
	if h.record(t, 0) != nil {
		t.Fatal("record not cleared on completion")
	}
	h.advance(time.Minute)
	h.failPRD(t, 0, "second", "")
	h.h.c.triagePass()
	rec := h.record(t, 0)
	if rec == nil {
		t.Fatal("no new record for the new episode")
	}
	if rec.AlertedAt != nil {
		t.Fatalf("new episode carried the old AlertedAt: %v", rec.AlertedAt)
	}
	h.advance(11 * time.Minute)
	h.h.c.triagePass()
	h.assertAlerts(t, 2, "warn|Triage: ")
}

// A blocked standalone records the block reason when no failed task
// carries error text.
func TestTriagePass_BlockedStandalone_RecordsBlockReason(t *testing.T) {
	h := newTriageHarness(t, 1, 10)
	h.blockedPRD(t, 0, "waiting on review")
	h.h.c.triagePass()
	rec := h.record(t, 0)
	if rec == nil {
		t.Fatal("no record for the blocked automaton")
	}
	if rec.Status != string(PRDBlocked) {
		t.Fatalf("rec.Status = %q, want %q", rec.Status, PRDBlocked)
	}
	if rec.Error != "waiting on review" {
		t.Fatalf("rec.Error = %q, want block reason", rec.Error)
	}
	if rec.TaskID != "" {
		t.Fatalf("rec.TaskID = %q, want empty", rec.TaskID)
	}
}

// The existing "Automation blocked" alert gains the failed task's error
// and session end reason when known.
func TestBlockedAlert_CarriesTaskErrorAndEndReason(t *testing.T) {
	h := newTriageHarness(t, 1, 10)
	a := h.h.force(h.h.makeAuto(nil, 1))
	h.failPRD(t, 0, "boom", "operator ended it")
	h.h.c.SetReady()
	h.h.event(0, PRDFailed)
	h.assertAlerts(t, 1, "warn|Automation blocked")
	entry := h.alertEntry(t, "warn|Automation blocked")
	body := entry[strings.LastIndex(entry, "|")+1:]
	containsAll(t, body,
		fmt.Sprintf("Automation %q (%s):", a.Name, a.ID),
		"Task error: boom",
		"Session end reason: operator ended it",
		"Failed task: the task (id task-0)",
	)
}

// ...and when the member carries no error or end reason the alert stays
// exactly as it was (template only, nothing appended).
func TestBlockedAlert_StaysAsIsWithoutTaskInfo(t *testing.T) {
	h := newTriageHarness(t, 1, 10)
	h.h.force(h.h.makeAuto(nil, 1))
	// failed member, but no task at all: no error, no end reason
	p, _ := h.h.m.store.GetPRD(h.h.prds[0].ID)
	p.Status = PRDFailed
	if err := h.h.m.store.SavePRD(p); err != nil {
		t.Fatal(err)
	}
	h.h.c.SetReady()
	h.h.event(0, PRDFailed)
	h.assertAlerts(t, 1, "warn|Automation blocked")
	entry := h.alertEntry(t, "warn|Automation blocked")
	body := entry[strings.LastIndex(entry, "|")+1:]
	if strings.Contains(body, "Task error:") || strings.Contains(body, "Session end reason:") {
		t.Fatalf("alert should stay as-is when the task carries no error/reason:\n%s", body)
	}
	if strings.Count(body, "\n") != 0 {
		t.Fatalf("alert body should be a single template line:\n%s", body)
	}
}
