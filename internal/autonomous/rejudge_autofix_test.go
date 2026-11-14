// Auto re-review after an auto-fix merge: the hook that runs the completion
// review once when an auto-fix task's work lands on the Automaton branch,
// and the wiring that fires it from the real merge path
// (MergeCompletedTasks). A pass on the new head clears the stale block; a
// head the review already answered is never re-judged.
package autonomous

import (
	"context"
	"strings"
	"testing"
)

// autoFixTask is a task of the auto-fix kind as recognized by the hook: the
// title the content auto-fix synthesizes carries the "Auto-fix: " prefix
// (autoFixTaskTitlePrefix, placement_tags.go).
func autoFixTask(id, title string) *Task {
	return &Task{ID: id, Title: title}
}

// TestAutoRejudgeAfterAutoFixMergePlainTaskRefused — the hook is scoped to
// auto-fix work: a plain task (whatever its title) never triggers an
// automatic re-judgment, even for a blocked record with a pending review.
func TestAutoRejudgeAfterAutoFixMergePlainTaskRefused(t *testing.T) {
	m, prd, _ := newRejudgeBlockedPRD(t, TaskCompleted)
	j := newRejudgePassJudge(t)
	m.SetGuardrail(j.fn)

	ran := m.AutoRejudgeAfterAutoFixMerge(context.Background(), prd, autoFixTask("t1", "rules work"))
	if ran {
		t.Error("hook ran for a plain task; only auto-fix work may trigger it")
	}
	if j.calls != 0 {
		t.Errorf("judge calls = %d, want 0", j.calls)
	}
	stored, _ := m.Store().GetPRD(prd.ID)
	if stored.Status != PRDBlocked {
		t.Errorf("status = %s, want blocked unchanged after a refusal", stored.Status)
	}

	// The prefix gate is exact: the title must start with "Auto-fix: ".
	cases := []struct {
		title string
		want  bool
	}{
		{"Auto-fix: apply the AGENT.md fix", true},
		{"Auto-fix: ", true},
		{"Auto-fix: no separator", true},    // still the prefix; the rest is free text
		{"Auto-fix:no separator", false},    // the space after the colon is part of the prefix
		{"autofix: apply", false},           // wrong capitalization
		{"Fix: Auto-fix: apply", false},    // prefix, not a substring
		{"", false},
	}
	for _, c := range cases {
		if got := isAutoFixTask(autoFixTask("t1", c.title)); got != c.want {
			t.Errorf("isAutoFixTask(%q) = %v, want %v", c.title, got, c.want)
		}
	}
	if got := isAutoFixTask(nil); got {
		t.Error("isAutoFixTask(nil) = true, want false")
	}
	if m.AutoRejudgeAfterAutoFixMerge(context.Background(), prd, nil) {
		t.Error("hook ran for a nil task; want a refusal")
	}
}

// TestAutoRejudgeAfterAutoFixMergeRunningRefused — the hook only runs for a
// record whose completion review is open for re-running (blocked, or
// finished-not-merged). A record mid-run is left alone.
func TestAutoRejudgeAfterAutoFixMergeRunningRefused(t *testing.T) {
	m, prd, _ := newRejudgeBlockedPRD(t, TaskCompleted)
	fresh, _ := m.Store().GetPRD(prd.ID)
	fresh.Status = PRDRunning
	if err := m.Store().SavePRD(fresh); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	prd.Status = PRDRunning
	j := newRejudgePassJudge(t)
	m.SetGuardrail(j.fn)

	if m.AutoRejudgeAfterAutoFixMerge(context.Background(), prd, autoFixTask("t1", "Auto-fix: apply")) {
		t.Error("hook ran for a running record; only blocked/finished records are eligible")
	}
	if j.calls != 0 {
		t.Errorf("judge calls = %d, want 0", j.calls)
	}
}

// TestAutoRejudgeAfterAutoFixMergePendingBlockedClears — the pending case:
// a blocked record with no recorded rules-review verdict is re-judged by the
// hook; a pass lifts the block to completed and clears the block reason. The
// same head is answered once: a second call is a no-op for the judge.
func TestAutoRejudgeAfterAutoFixMergePendingBlockedClears(t *testing.T) {
	m, prd, _ := newRejudgeBlockedPRD(t, TaskCompleted)
	task := autoFixTask("af", "Auto-fix: apply the AGENT.md fix")
	j := newRejudgePassJudge(t)
	m.SetGuardrail(j.fn)

	if !m.AutoRejudgeAfterAutoFixMerge(context.Background(), prd, task) {
		t.Fatal("hook did not run for a blocked record with a pending review")
	}
	if j.calls != 1 {
		t.Fatalf("judge calls = %d, want 1 (the review ran once)", j.calls)
	}
	stored, ok := m.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatal("GetPRD failed")
	}
	if stored.Status != PRDCompleted {
		t.Errorf("status = %s, want completed after the pass lifts the block", stored.Status)
	}
	if stored.BlockedReason != "" {
		t.Errorf("BlockedReason = %q, want cleared by the pass", stored.BlockedReason)
	}
	if len(stored.Verdicts) == 0 {
		t.Fatal("no verdict recorded after the re-judgment")
	}
	last := stored.Verdicts[len(stored.Verdicts)-1]
	if last.Guardrail != "rules-review" || last.Outcome != "pass" {
		t.Errorf("latest verdict = %+v, want the fresh pass rules-review verdict", last)
	}
	kinds := decisionKinds(stored)
	if !kindsContains(kinds, "guardrail_rejudge") {
		t.Errorf("Decisions %v: missing the guardrail_rejudge Decision", kinds)
	}
	// The re-judgment is attributed to the automatic caller, not an operator.
	for _, d := range stored.Decisions {
		if d.Kind == "guardrail_rejudge" && d.Actor != "auto-fix-merge" {
			t.Errorf("guardrail_rejudge Actor = %q, want the auto-fix-merge caller", d.Actor)
		}
	}

	// Same head, again: the review already answered it — no second judgment.
	if m.AutoRejudgeAfterAutoFixMerge(context.Background(), prd, task) {
		t.Error("hook ran again for a head the review already answered; run once per head")
	}
	if j.calls != 1 {
		t.Errorf("judge calls after the second hook pass = %d, want 1 (no re-judgment)", j.calls)
	}
}

// TestAutoRejudgeStalePassAfterAutoFixMergeReruns — the stale-pass case: a
// finished_not_merged record whose completion review already PASSED on an
// older head. The auto-fix just moved the head past that verdict, so the
// review must run once on the new tree (the pre-fix gate refused re-judging
// any record whose newest verdict was a pass). A pass on the new head
// settles it; a call for the same new head is then a no-op.
func TestAutoRejudgeStalePassAfterAutoFixMergeReruns(t *testing.T) {
	m, prd := newFinishedUnmergedPRD(t, PRDCompleted, false)
	// The stale verdict: a pass the review made on the previous head.
	fresh, _ := m.Store().GetPRD(prd.ID)
	fresh.Verdicts = append(fresh.Verdicts, GuardrailVerdict{
		Guardrail:        "rules-review",
		Outcome:          "pass",
		Summary:          "clean on the older tree",
		JudgeSessionID:   "judge-stale-pass",
		JudgedCommitSHA:  "sha-head-1",
		RulesFingerprint: "fingerprint-old",
	})
	if err := m.Store().SavePRD(fresh); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	// The auto-fix lands its work: the head moves off the pass's commit.
	setRejudgeHeadCommit(t, m, prd.ID, "sha-head-2")
	task := autoFixTask("af", "Auto-fix: apply the AGENT.md fix")
	j := newRejudgePassJudge(t)
	m.SetGuardrail(j.fn)

	if !m.AutoRejudgeAfterAutoFixMerge(context.Background(), prd, task) {
		t.Fatal("hook did not run for a stale pass on an older head; the review must re-run on the new tree")
	}
	if j.calls != 1 {
		t.Fatalf("judge calls = %d, want 1 (a fresh review ran on the new head)", j.calls)
	}
	stored, _ := m.Store().GetPRD(prd.ID)
	if stored.Status != PRDCompleted {
		t.Errorf("status = %s, want completed kept by the pass", stored.Status)
	}
	if stored.BlockedReason != "" {
		t.Errorf("BlockedReason = %q, want empty", stored.BlockedReason)
	}
	if len(stored.Verdicts) < 2 {
		t.Fatal("expected the stale verdict plus the fresh one")
	}
	last := stored.Verdicts[len(stored.Verdicts)-1]
	if last.Guardrail != "rules-review" || last.Outcome != "pass" || last.JudgedCommitSHA != "sha-head-2" {
		t.Errorf("latest verdict = %+v, want a fresh pass judged on the new head sha-head-2", last)
	}
	if kinds := decisionKinds(stored); !kindsContains(kinds, "guardrail_rejudge") {
		t.Errorf("Decisions %v: missing the guardrail_rejudge Decision", kinds)
	}

	// The new head is now answered: the hook is a no-op for it — including,
	// decisively, NOT treating the fresh pass as stale against itself.
	rerun := m.AutoRejudgeAfterAutoFixMerge(context.Background(), prd, task)
	if rerun {
		t.Error("hook re-ran for a head its own fresh pass already answered")
	}
	if j.calls != 1 {
		t.Errorf("judge calls after the re-run pass = %d, want 1 (the new head was answered once)", j.calls)
	}
}

// TestAutoRejudgeAfterAutoFixMergeBlockVerdictStillReruns — a newest
// verdict that is itself the stale BLOCK the auto-fix should clear: the
// hook re-judges, and the same fresh block flips the finished record back to
// blocked with the new finding (the pass is what ever clears it).
func TestAutoRejudgeAfterAutoFixMergeBlockVerdictStillReruns(t *testing.T) {
	m, prd := newFinishedUnmergedPRD(t, PRDArchived, true)
	setRejudgeHeadCommit(t, m, prd.ID, "sha-head-2")
	task := autoFixTask("af", "Auto-fix: apply the AGENT.md fix")
	j := newRejudgeBlockJudge(t)
	m.SetGuardrail(j.fn)

	if !m.AutoRejudgeAfterAutoFixMerge(context.Background(), prd, task) {
		t.Fatal("hook did not run for a stale block the auto-fix should re-judge")
	}
	if j.calls != 1 {
		t.Fatalf("judge calls = %d, want 1 (a fresh review, not the stale verdict replayed)", j.calls)
	}
	stored, _ := m.Store().GetPRD(prd.ID)
	if stored.Status != PRDBlocked {
		t.Errorf("status = %s, want blocked: a fresh block on a finished record flips it back", stored.Status)
	}
	if !strings.Contains(stored.BlockedReason, "would cause an incident") {
		t.Errorf("BlockedReason = %q, want the fresh finding", stored.BlockedReason)
	}
}

// TestMergeCompletedTasksAutoFixMergeTriggersRejudge — the real merge path:
// MergeCompletedTasks lands an auto-fix task's branch into the PRD branch
// (a genuine git merge in a per-task worktree) and, because the fix moved
// the head past the review's block, fires the one-time automatic re-judgment
// whose pass clears the stale block — no operator action in between.
func TestMergeCompletedTasksAutoFixMergeTriggersRejudge(t *testing.T) {
	f := dsmFixture(t, dsmBaseRules)
	// The auto-fix task: its completed work sits on its own branch, and the
	// record is blocked on the pre-fix head (a stale rules-review block).
	f.task.Title = "Auto-fix: apply the AGENT.md fix"
	f.prd.Status = PRDBlocked
	f.prd.BlockedReason = "guardrail rules-review returned block (judge-prior) - prior finding"
	f.prd.PerPRDGuardrails = []string{"rules-review"}
	if err := f.m.store.SavePRD(f.prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	// Keep the fake judge reachable, as in an operator's real config.
	f.m.mu.Lock()
	f.m.cfg.VerificationModel = "ollama/qwen3.8:27b"
	f.m.mu.Unlock()
	f.commit("work.txt", "the fix\n")
	j := newRejudgePassJudge(t)
	f.m.SetGuardrail(j.fn)

	merged, err := f.m.MergeCompletedTasks(context.Background(), f.prd)
	if err != nil {
		t.Fatalf("MergeCompletedTasks: %v", err)
	}
	if merged != 1 {
		t.Fatalf("merged = %d, want 1 (the task branch landed)", merged)
	}
	if f.task.MergeState != MergeStateMerged {
		t.Fatalf("task MergeState = %q, want merged", f.task.MergeState)
	}
	if got := f.prdFile("work.txt"); got != "the fix\n" {
		t.Fatalf("PRD branch work.txt = %q, want the auto-fix work merged in", got)
	}
	if j.calls != 1 {
		t.Fatalf("judge calls = %d, want 1 (the review ran once after the auto-fix merge)", j.calls)
	}
	stored, ok := f.m.store.GetPRD(f.prd.ID)
	if !ok {
		t.Fatal("GetPRD (stored) failed")
	}
	if stored.Status != PRDCompleted {
		t.Errorf("stored status = %s, want completed after the stale block clears", stored.Status)
	}
	if stored.BlockedReason != "" {
		t.Errorf("stored BlockedReason = %q, want the stale block cleared by the pass", stored.BlockedReason)
	}
	if len(stored.Verdicts) == 0 {
		t.Fatal("no verdict recorded after the automatic re-judgment")
	}
	last := stored.Verdicts[len(stored.Verdicts)-1]
	if last.Guardrail != "rules-review" || last.Outcome != "pass" {
		t.Errorf("latest verdict = %+v, want the fresh pass rules-review verdict", last)
	}
	for _, d := range stored.Decisions {
		if d.Kind == "guardrail_rejudge" && d.Actor != "auto-fix-merge" {
			t.Errorf("guardrail_rejudge Actor = %q, want the auto-fix-merge caller", d.Actor)
		}
	}
	// Run once per head: another auto-fix merge pass for the same head is a
	// no-op for the judge.
	if f.m.AutoRejudgeAfterAutoFixMerge(context.Background(), stored, autoFixTask("af", "Auto-fix: apply")) {
		t.Error("hook re-ran for a head the automatic re-judgment already answered")
	}
	if j.calls != 1 {
		t.Errorf("judge calls after the repeat pass = %d, want 1 (no second judgment for the same head)", j.calls)
	}
}

// TestMergeCompletedTasksPlainTaskNoAutoRejudge — the control: a plain
// (non-auto-fix) merge lands its work but triggers no automatic
// re-judgment at all, even for a blocked record.
func TestMergeCompletedTasksPlainTaskNoAutoRejudge(t *testing.T) {
	f := dsmFixture(t, dsmBaseRules)
	f.prd.Status = PRDBlocked
	f.prd.BlockedReason = "guardrail rules-review returned block (judge-prior) - prior finding"
	f.prd.PerPRDGuardrails = []string{"rules-review"}
	if err := f.m.store.SavePRD(f.prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	f.m.mu.Lock()
	f.m.cfg.VerificationModel = "ollama/qwen3.8:27b"
	f.m.mu.Unlock()
	f.commit("work.txt", "plain work\n")
	j := newRejudgePassJudge(t)
	f.m.SetGuardrail(j.fn)

	merged, err := f.m.MergeCompletedTasks(context.Background(), f.prd)
	if err != nil {
		t.Fatalf("MergeCompletedTasks: %v", err)
	}
	if merged != 1 {
		t.Fatalf("merged = %d, want 1", merged)
	}
	if j.calls != 0 {
		t.Errorf("judge calls = %d, want 0 (a plain task merge never auto re-judges)", j.calls)
	}
	stored, _ := f.m.store.GetPRD(f.prd.ID)
	if stored.Status != PRDBlocked {
		t.Errorf("stored status = %s, want still blocked: nothing cleared it", stored.Status)
	}
	if kinds := decisionKinds(stored); kindsContains(kinds, "guardrail_rejudge") {
		t.Error("a guardrail_rejudge Decision exists after a plain-task merge; none may be added")
	}
}
