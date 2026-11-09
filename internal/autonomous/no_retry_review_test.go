package autonomous

// No-retry-to-review, end to end. Each test drives a real Manager through
// m.Run with fake spawns and a verifier that exercises one no-retry case. A
// no-retry hold is distinct from the ordinary retry-limit hold: it parks the
// task in review_needed after one attempt (third-attempt identical failures
// is the one that needs three prior), never schedules another attempt, records
// the reason on the failed attempt, and alerts the operator with it.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// holdAlertLog / runEscalationPRD / escalationTask / newFailureEscalation* all
// come from the failure-escalation test helpers and are reused here unchanged.

func TestNoRetry_NoFilesAndNothingToWrite_ToReviewAfterOneAttempt(t *testing.T) {
	m, api := newFailureEscalationFixture(t, t.TempDir())
	var alerts holdAlertLog
	m.SetAlertSink(alerts.sink)
	// The task writes nothing (no planned files, not read-only), so a verifier
	// that fails it with "no files changed" is the first no-retry case.
	prd := newFailureEscalationPRD(t, m, t.TempDir(), func(p *PRD) {
		p.Story[0].Tasks[0].ReadOnly = true
	})
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		return VerificationResult{OK: false, Summary: "no files changed"}, nil
	}

	var spawns escalationSpawnLog
	runEscalationPRD(t, m, api, prd, &spawns, verify)

	task := escalationTask(t, m, prd.ID)
	wantReason := "the task writes nothing"
	assertNoRetryReview(t, task, prd, &alerts, wantReason)

	// Parked after a single attempt — the second was never started.
	if n := len(task.Attempts); n != 1 {
		t.Fatalf("attempts = %d, want exactly 1 (no retry)", n)
	}
	if n := len(spawns.workers()); n != 1 {
		t.Fatalf("spawned = %d, want 1 (the second attempt was never run)", n)
	}
}

func TestNoRetry_MissingVerificationScript_ToReviewAfterOneAttempt(t *testing.T) {
	m, api := newFailureEscalationFixture(t, t.TempDir())
	var alerts holdAlertLog
	m.SetAlertSink(alerts.sink)
	dir := t.TempDir()
	// The task names a file to write, so the first (no-files-changed) case does
	// not match — this must be the missing-script case.
	prd := newFailureEscalationPRD(t, m, dir, func(p *PRD) {
		p.Story[0].Tasks[0].VerificationScript = "verify.sh"
		p.Story[0].Tasks[0].FilesPlanned = []string{"src/main.go"}
	})
	// The script exists past the pre-start gate, then disappears before
	// verification runs — the case a re-run cannot fix. The removal happens in
	// the verifier (after the gate and the spawn), so only the no-retry check
	// sees it missing.
	scriptPath := filepath.Join(dir, "verify.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		_ = os.Remove(scriptPath)
		return VerificationResult{OK: false, Summary: "verification failed"}, nil
	}

	var spawns escalationSpawnLog
	runEscalationPRD(t, m, api, prd, &spawns, verify)

	task := escalationTask(t, m, prd.ID)
	assertNoRetryReview(t, task, prd, &alerts, "is missing from the worktree")

	if n := len(task.Attempts); n != 1 {
		t.Fatalf("attempts = %d, want exactly 1 (no retry)", n)
	}
}

func TestNoRetry_ThirdIdenticalFailure_ToReview(t *testing.T) {
	m, api := newFailureEscalationFixture(t, t.TempDir())
	var alerts holdAlertLog
	m.SetAlertSink(alerts.sink)
	prd := newFailureEscalationPRD(t, m, t.TempDir(), func(p *PRD) {
		p.Story[0].Tasks[0].FilesPlanned = []string{"src/main.go"}
	})
	// Every attempt reports the identical failure summary — the one thing a
	// retry cannot fix. Distinct would be the ordinary retry path.
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		return VerificationResult{OK: false, Summary: "TestParse always fails"}, nil
	}

	var spawns escalationSpawnLog
	runEscalationPRD(t, m, api, prd, &spawns, verify)

	task := escalationTask(t, m, prd.ID)
	assertNoRetryReview(t, task, prd, &alerts, "a third identical attempt will not change the outcome")

	// The first two identical failures retried; the third stopped and parked.
	if n := len(task.Attempts); n != 3 {
		t.Fatalf("attempts = %d, want 3 (retried twice, then no-retry hold)", n)
	}
}

func TestNoRetry_OrdinaryFailureStillRetries(t *testing.T) {
	m, api := newFailureEscalationFixture(t, t.TempDir())
	prd := newFailureEscalationPRD(t, m, t.TempDir(), func(p *PRD) {
		p.Story[0].Tasks[0].FilesPlanned = []string{"src/main.go"}
	})
	// Two ordinary, DIFFERENT failures then a pass. Nothing here is a no-retry
	// case, so the task must retry and recover instead of being parked.
	calls := 0
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		calls++
		switch calls {
		case 1:
			return VerificationResult{OK: false, Summary: "missing import"}, nil
		case 2:
			return VerificationResult{OK: false, Summary: "type mismatch in parse"}, nil
		default:
			return VerificationResult{OK: true, Summary: "passes"}, nil
		}
	}

	var spawns escalationSpawnLog
	runEscalationPRD(t, m, api, prd, &spawns, verify)

	task := escalationTask(t, m, prd.ID)
	// It retried and completed — not parked for review, with no reason logged.
	if task.Status != TaskCompleted {
		t.Fatalf("status = %s, want completed (an ordinary failure retries)", task.Status)
	}
	for i, a := range task.Attempts {
		if a.NoRetryReason != "" {
			t.Errorf("attempt %d NoRetryReason = %q, want empty for an ordinary failure", i+1, a.NoRetryReason)
		}
	}
	if task.FailureReason != nil {
		t.Fatalf("FailureReason = %+v, want nil on a recovered task", task.FailureReason)
	}
}

// assertNoRetryReview checks the shared shape of a no-retry hold: the task is
// parked in review_needed with the reason on FailureReason, mirrored on the
// failed attempt and on the review view, and the operator alert names it.
func assertNoRetryReview(t *testing.T, task *Task, prd *PRD, alerts *holdAlertLog, reasonContains string) {
	t.Helper()

	if task.Status != TaskReviewNeeded {
		t.Fatalf("status = %s, want review_needed", task.Status)
	}

	if task.FailureReason == nil {
		t.Fatal("FailureReason is nil, want a no-retry reason")
	}
	if task.FailureReason.Source != noRetryHoldSource {
		t.Fatalf("FailureReason.Source = %q, want %q", task.FailureReason.Source, noRetryHoldSource)
	}
	if task.FailureReason.Retryable {
		t.Error("FailureReason.Retryable = true, want false (a no-retry hold)")
	}
	if !strings.Contains(task.FailureReason.Detail, reasonContains) {
		t.Errorf("FailureReason.Detail = %q, want it to contain %q", task.FailureReason.Detail, reasonContains)
	}

	// The last attempt carries the reason, so the operator can read it there.
	last := lastTaskAttempt(task)
	if last == nil {
		t.Fatal("no recorded attempt, want the failed attempt with the reason")
	}
	if !strings.Contains(last.NoRetryReason, reasonContains) {
		t.Errorf("last attempt NoRetryReason = %q, want it to contain %q", last.NoRetryReason, reasonContains)
	}

	// The review report and the held-cause popup carry the reason too.
	if task.ReviewSummary == nil {
		t.Fatal("ReviewSummary is nil on a no-retry-held task")
	} else if !strings.Contains(task.ReviewSummary.Text, reasonContains) {
		t.Errorf("ReviewSummary.Text = %q, want it to contain %q", task.ReviewSummary.Text, reasonContains)
	}

	entries := reviewNeededEntries(prd)
	if len(entries) != 1 {
		t.Fatalf("review-needed entries = %d, want 1", len(entries))
	}
	if !strings.Contains(entries[0].NoRetryReason, reasonContains) {
		t.Errorf("view NoRetryReason = %q, want it to contain %q", entries[0].NoRetryReason, reasonContains)
	}

	// Exactly one alert, error level, titled as a no-retry hold, body names the reason.
	got := alerts.all()
	if len(got) != 1 {
		t.Fatalf("alerts = %d, want exactly 1: %+v", len(got), got)
	}
	a := got[0]
	if a.level != "error" {
		t.Errorf("alert level = %q, want error", a.level)
	}
	if a.title != "Automaton task needs review (no retry)" {
		t.Errorf("alert title = %q, want the no-retry title", a.title)
	}
	if !strings.Contains(a.body, "No retry — ") {
		t.Errorf("alert body = %q, want it to flag the no-retry reason", a.body)
	}
	if !strings.Contains(a.body, reasonContains) {
		t.Errorf("alert body = %q, want it to name the reason", a.body)
	}
}
