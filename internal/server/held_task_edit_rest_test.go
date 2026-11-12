// REST coverage of the held-task edit and the read-only / report-path fields on
// the review retry. They run against the real Automaton manager seeded with a
// blocked Automaton (heldRESTPRD), so the status codes and the stored task are
// both checked.

package server

import (
	"net/http"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

const heldRESTReport = ".datawatch/reports/held.md"

// TestHeldTaskEditRESTCarriesSpecReadOnlyAndReportPath: edit_task accepts a
// review_needed task of a blocked Automaton, and the task stays held.
func TestHeldTaskEditRESTCarriesSpecReadOnlyAndReportPath(t *testing.T) {
	s, _, _ := newFailureEscalationRESTServer(t, heldRESTPRD("a1"))
	rr := postAutomaton(t, s, "a1", "edit_task",
		`{"task_id":"t-held","new_spec":"report only","read_only":true,"report_path":"`+heldRESTReport+`"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("edit_task on a held task: want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	task := autoTask(t, s, "a1", "t-held")
	if task.Spec != "report only" || !task.ReadOnly || task.ReportPath != heldRESTReport {
		t.Fatalf("task after edit: spec=%q read_only=%v report_path=%q", task.Spec, task.ReadOnly, task.ReportPath)
	}
	if task.Status != autonomous.TaskReviewNeeded {
		t.Fatalf("edit changed the status to %q", task.Status)
	}
}

// TestHeldTaskEditRESTRefusals: a pending task of a blocked Automaton and a
// report path on a task that is not read-only answer 400 and change nothing.
func TestHeldTaskEditRESTRefusals(t *testing.T) {
	s, _, _ := newFailureEscalationRESTServer(t, heldRESTPRD("a1"))
	if rr := postAutomaton(t, s, "a1", "edit_task", `{"task_id":"t-pending","new_spec":"x"}`); rr.Code != http.StatusBadRequest {
		t.Fatalf("pending task of a blocked automaton: want 400, got %d", rr.Code)
	}
	rr := postAutomaton(t, s, "a1", "edit_task", `{"task_id":"t-held","new_spec":"half","report_path":"`+heldRESTReport+`"}`)
	if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "read_only") {
		t.Fatalf("report path on a writer: want 400 naming read_only, got %d: %s", rr.Code, rr.Body.String())
	}
	if task := autoTask(t, s, "a1", "t-held"); task.Spec != "make the change" || task.ReportPath != "" {
		t.Fatalf("a refused edit changed the task: spec=%q report_path=%q", task.Spec, task.ReportPath)
	}
}

// TestReviewRetryReadOnlyRESTRequeuesWithBothApplied: resolve_task_review retry
// carries read_only and report_path through to the stored task.
func TestReviewRetryReadOnlyRESTRequeuesWithBothApplied(t *testing.T) {
	s, _, auditPath := newFailureEscalationRESTServer(t, heldRESTPRD("a1"))
	rr := postAutomaton(t, s, "a1", "resolve_task_review",
		`{"task_id":"t-held","action":"retry","read_only":true,"report_path":"`+heldRESTReport+`"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("retry: want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	task := autoTask(t, s, "a1", "t-held")
	if task.Status != "" && task.Status != autonomous.TaskPending {
		t.Fatalf("status after retry = %q, want pending", task.Status)
	}
	if !task.ReadOnly || task.ReportPath != heldRESTReport {
		t.Fatalf("read_only=%v report_path=%q, want true and %q", task.ReadOnly, task.ReportPath, heldRESTReport)
	}
	if got := auditActions(t, auditPath); !hasAuditAction(got, "resolve_task_review") {
		t.Fatalf("no resolve_task_review audit entry; actions = %v", got)
	}
}

// TestReviewRetryReadOnlyRESTBadReportPathLeavesTaskHeld: a bad report path, or
// one on a task that is not read-only, answers 400 and the task stays held.
func TestReviewRetryReadOnlyRESTBadReportPathLeavesTaskHeld(t *testing.T) {
	s, _, _ := newFailureEscalationRESTServer(t, heldRESTPRD("a1"))
	for _, body := range []string{
		`{"task_id":"t-held","action":"retry","read_only":true,"report_path":"docs/x.md"}`,
		`{"task_id":"t-held","action":"retry","report_path":"` + heldRESTReport + `"}`,
	} {
		if rr := postAutomaton(t, s, "a1", "resolve_task_review", body); rr.Code != http.StatusBadRequest {
			t.Fatalf("%s: want 400, got %d: %s", body, rr.Code, rr.Body.String())
		}
		task := autoTask(t, s, "a1", "t-held")
		if task.Status != autonomous.TaskReviewNeeded || task.ReviewSummary == nil || task.ReadOnly || task.ReportPath != "" {
			t.Fatalf("%s: refused retry changed the held task: %+v", body, task)
		}
	}
}
