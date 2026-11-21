package autonomous

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

const emptyDiffTestSummary = "verifier: no changes detected (diff empty and no new files); task produced no output"

// docsSetTestTasks returns a sibling and the docs set task. The docs set task
// does not depend on the sibling, so it runs even when the sibling is held.
func docsSetTestTasks(siblingStatus TaskStatus) []Task {
	return []Task{
		{ID: "sib-1", Title: "Implement it", Spec: "do it", Status: siblingStatus},
		{ID: "docs-1", Title: "Docs set " + TaskTemplateIDMarker(TemplateDocsSet), Spec: "update docs"},
	}
}

// emptyDiffVerifyFor fails the docs task with a per-attempt summary (identical
// summaries would trip the no-retry hold first) and the sibling when
// failSibling is set; everything else passes.
func emptyDiffVerifyFor(title, summary string, failSibling bool) VerifyFn {
	return func(_ context.Context, _ *PRD, task *Task) (VerificationResult, error) {
		if task.Title == title {
			return VerificationResult{OK: false, Summary: fmt.Sprintf("%s (attempt %d)", summary, len(task.Attempts)+1)}, nil
		}
		if failSibling {
			return VerificationResult{OK: false, Summary: fmt.Sprintf("tests still fail (attempt %d)", len(task.Attempts)+1)}, nil
		}
		return VerificationResult{OK: true, Summary: "tests pass"}, nil
	}
}

func runDocsSetCase(t *testing.T, siblingStatus TaskStatus, summary string, failSibling bool) (*Manager, *PRD, *Task) {
	t.Helper()
	m, api := newFailureEscalationFixture(t, t.TempDir())
	prd := newHoldPRD(t, m, docsSetTestTasks(siblingStatus), nil)
	var spawns escalationSpawnLog
	title := "Docs set " + TaskTemplateIDMarker(TemplateDocsSet)
	runEscalationPRD(t, m, api, prd, &spawns, emptyDiffVerifyFor(title, summary, failSibling))
	return m, prd, taskByTitle(t, m, prd.ID, title)
}

func TestDocsSetEmptyDiffCompletedWithNote(t *testing.T) {
	m, prd, task := runDocsSetCase(t, TaskCompleted, emptyDiffTestSummary, false)
	if task.Status != TaskCompleted {
		t.Fatalf("task status = %q, want completed", task.Status)
	}
	if want := "nothing to document: "; !strings.HasPrefix(task.Error, want) || !strings.Contains(task.Error, emptyDiffSummaryFragment) {
		t.Fatalf("note = %q, want prefix %q carrying the verifier summary", task.Error, want)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	if got.Status == PRDBlocked || m.reviewHeldTaskID(got) != "" {
		t.Fatalf("Automaton status = %q, want not blocked or held", got.Status)
	}
	if got.Status != PRDCompleted {
		t.Fatalf("Automaton status = %q, want completed", got.Status)
	}
}

func TestDocsSetEmptyDiffSiblingsIncomplete(t *testing.T) {
	_, _, task := runDocsSetCase(t, TaskPending, emptyDiffTestSummary, true)
	if task.Status == TaskCompleted {
		t.Fatalf("task completed with an incomplete sibling")
	}
	if task.Status != TaskReviewNeeded {
		t.Fatalf("task status = %q, want %q", task.Status, TaskReviewNeeded)
	}
}

func TestDocsSetOtherFailureStillHeld(t *testing.T) {
	_, _, task := runDocsSetCase(t, TaskCompleted, "tests still fail", false)
	if task.Status != TaskReviewNeeded {
		t.Fatalf("task status = %q, want %q", task.Status, TaskReviewNeeded)
	}
}
