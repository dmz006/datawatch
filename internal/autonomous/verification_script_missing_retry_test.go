// B124 retry path — a tooled (ProjectDir-bearing) PRD whose plan touches
// files but leaves one or more file-changing tasks without a
// verification script is rejected as a content failure. This file covers
// the retry side of that rejection:
//   - content retry budget exhausted (0) → Decompose fails immediately,
//     naming only the scriptless task(s), with the reason recorded on
//     LastDecomposeFailure so an operator sees WHY from any surface;
//   - content retry budget remaining (default AutoFixRetries = 1) → the
//     second attempt's prompt carries the "Your previous attempt failed"
//     hint naming the missing script's task, and a complete second plan
//     lands the PRD in needs_review with the hint cleared;
//   - a plan where every file-changing task already has a script is
//     accepted on the first call, consuming no retry budget at all.
//
// Both twin loops — Decompose() and decomposeStreamingCore() (via
// DecomposeStreaming) — are exercised for the reject and the retry, so
// the drifted-duplicate pair can't drift on this gate.

package autonomous

import (
	"strings"
	"testing"
)

// twoFileTasksBothScripted is the "fixed" version of
// planRespTwoFileTasksOneScript (verification_script_prompt_test.go):
// same two file-changing tasks, but the previously-scriptless Task 2 now
// names its own verification script.
const twoFileTasksBothScripted = `{"title":"T","stories":[{"title":"S","tasks":[{"title":"Task 1","spec":"do it","files":["a.go"],"verification_script":".datawatch/verify/t1.sh"},{"title":"Task 2","spec":"do it","files":["b.go"],"verification_script":".datawatch/verify/t2.sh"}]}]}`

// fakeScriptPlanner is a DecomposeFn whose response the test picks per
// call; it records every call count and the exact prompt (Spec) it was
// given so tests can assert on the retry hint.
type fakeScriptPlanner struct {
	calls int
	specs []string
	resp  func(call int) string
}

func (f *fakeScriptPlanner) fn(req DecomposeRequest) (string, error) {
	f.calls++
	f.specs = append(f.specs, req.Spec)
	return f.resp(f.calls), nil
}

// newManagerForMissingScript mirrors newTooledManagerForScriptCoverageTest
// (worktree-mode config so CreatePRD/Decompose yield a non-empty
// ProjectDir), but takes a call-counting planner and can zero the
// content retry budget (cfg.AutoFixRetries is the content/policy
// fallback in resolveRetryPolicy) to assert the no-budget path.
func newManagerForMissingScript(t *testing.T, fn func(DecomposeRequest) (string, error), zeroContentBudget bool) *Manager {
	t.Helper()
	baseRepo := scratchGitRepo(t)
	worktreeDir := t.TempDir()

	cfg := DefaultConfig()
	cfg.WorktreeBaseRepo = baseRepo
	cfg.WorktreeDir = worktreeDir
	if zeroContentBudget {
		cfg.AutoFixRetries = 0
	}
	m, err := NewManager(t.TempDir(), cfg, fn)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return m
}

// (1) zero content retry budget: the incomplete plan fails on the first
// call, naming only the scriptless task, with the detail preserved on
// LastDecomposeFailure.
func TestMissingScript_RejectedAndReportsTaskNames(t *testing.T) {
	plan := &fakeScriptPlanner{resp: func(call int) string { return planRespTwoFileTasksOneScript }}
	m := newManagerForMissingScript(t, plan.fn, true)
	prd, err := m.CreatePRD("spec", "", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}

	_, err = m.Decompose(prd.ID)
	if err == nil {
		t.Fatal("want an error when one of two file-changing tasks lacks a verification script and the content retry budget is 0")
	}
	if !strings.Contains(err.Error(), `task "Task 2"`) {
		t.Errorf("error = %v, want it to name the scriptless task (task \"Task 2\")", err)
	}
	if strings.Contains(err.Error(), `task "Task 1"`) {
		t.Errorf("error = %v, must name only the offending task (Task 1 has its own script)", err)
	}
	if plan.calls != 1 {
		t.Errorf("planner called %d times, want exactly 1 (content retry budget is 0)", plan.calls)
	}

	got, ok := m.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatal("PRD vanished")
	}
	if got.Status != PRDDraft {
		t.Errorf("status = %q, want rolled back to draft so the operator can retry", got.Status)
	}
	if got.LastDecomposeFailure == nil {
		t.Fatal("want LastDecomposeFailure recorded so an operator sees WHY the decompose failed")
	}
	if !strings.Contains(got.LastDecomposeFailure.Detail, `task "Task 2"`) {
		t.Errorf("LastDecomposeFailure.Detail = %q, want it to mention the missing script's task title (Task 2)", got.LastDecomposeFailure.Detail)
	}
	if strings.Contains(got.LastDecomposeFailure.Detail, `task "Task 1"`) {
		t.Errorf("LastDecomposeFailure.Detail = %q, must name only the offending task", got.LastDecomposeFailure.Detail)
	}
}

// (2) content retry budget remaining (default 1): the second attempt's
// prompt must carry the failure hint naming the scriptless task, the
// complete second plan must land the PRD in needs_review, and the
// single-use hint must be cleared.
func TestMissingScript_RetryPromptNamesTasks(t *testing.T) {
	plan := &fakeScriptPlanner{resp: func(call int) string {
		if call == 1 {
			return planRespTwoFileTasksOneScript
		}
		return twoFileTasksBothScripted
	}}
	m := newManagerForMissingScript(t, plan.fn, false)
	prd, err := m.CreatePRD("spec", "", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}

	if _, err := m.Decompose(prd.ID); err != nil {
		t.Fatalf("Decompose: %v (a complete second plan must be accepted)", err)
	}
	if plan.calls != 2 {
		t.Fatalf("planner called %d times, want exactly 2 (call 1 incomplete, call 2 complete; budget 1 allows exactly one retry)", plan.calls)
	}
	if len(plan.specs) < 2 {
		t.Fatal("test setup: expected two captured prompts")
	}
	if strings.Contains(plan.specs[0], "Your previous attempt failed") {
		t.Errorf("call 1's prompt must not carry a retry hint, got %q", plan.specs[0])
	}
	spec2 := plan.specs[1]
	if !strings.Contains(spec2, "Your previous attempt failed") {
		t.Errorf("call 2's prompt missing the retry hint %q", "Your previous attempt failed")
	}
	if !strings.Contains(spec2, `task "Task 2"`) {
		t.Errorf("call 2's prompt must name the missing script's task (task \"Task 2\") so the planner can fix it, got %q", spec2)
	}

	got, ok := m.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatal("PRD vanished")
	}
	if got.Status != PRDNeedsReview {
		t.Errorf("status = %q, want needs_review", got.Status)
	}
	if got.LastDecomposeFailure != nil {
		t.Errorf("LastDecomposeFailure = %+v, want the single-use hint cleared after success", got.LastDecomposeFailure)
	}
	if got.Story == nil || len(got.Story) != 1 || len(got.Story[0].Tasks) != 2 {
		t.Fatalf("want the accepted 2-task plan stored, got %+v", got.Story)
	}
}

// (3) a plan where every file-changing task has a script is accepted on
// the first call — no retry, no hint.
func TestMissingScript_AcceptedOnceScriptPresent(t *testing.T) {
	plan := &fakeScriptPlanner{resp: func(call int) string { return twoFileTasksBothScripted }}
	m := newManagerForMissingScript(t, plan.fn, false)
	prd, err := m.CreatePRD("spec", "", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}

	if _, err := m.Decompose(prd.ID); err != nil {
		t.Fatalf("Decompose: %v (a plan where every file-changing task has a script must be accepted)", err)
	}
	if plan.calls != 1 {
		t.Errorf("planner called %d times, want exactly 1 (nothing to retry)", plan.calls)
	}
	if strings.Contains(plan.specs[0], "Your previous attempt failed") {
		t.Errorf("a first-and-only attempt must not carry a retry hint, got %q", plan.specs[0])
	}
	got, ok := m.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatal("PRD vanished")
	}
	if got.Status != PRDNeedsReview {
		t.Errorf("status = %q, want needs_review", got.Status)
	}
	if got.LastDecomposeFailure != nil {
		t.Errorf("LastDecomposeFailure = %+v, want nil (no failure ever recorded)", got.LastDecomposeFailure)
	}
}

// (4) twin-loop coverage — the reject and the retry must behave
// identically through DecomposeStreaming (decomposeStreamingCore).

func TestMissingScriptStreaming_RejectedAndReportsTaskNames(t *testing.T) {
	plan := &fakeScriptPlanner{resp: func(call int) string { return planRespTwoFileTasksOneScript }}
	m := newManagerForMissingScript(t, plan.fn, true)
	prd, err := m.CreatePRD("spec", "", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}

	_, err = m.DecomposeStreaming(prd.ID, nil)
	if err == nil {
		t.Fatal("want an error when one of two file-changing tasks lacks a verification script and the content retry budget is 0")
	}
	if !strings.Contains(err.Error(), `task "Task 2"`) {
		t.Errorf("error = %v, want it to name the scriptless task (task \"Task 2\")", err)
	}
	if strings.Contains(err.Error(), `task "Task 1"`) {
		t.Errorf("error = %v, must name only the offending task (Task 1 has its own script)", err)
	}
	if plan.calls != 1 {
		t.Errorf("planner called %d times, want exactly 1 (content retry budget is 0)", plan.calls)
	}

	got, ok := m.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatal("PRD vanished")
	}
	if got.Status != PRDDraft {
		t.Errorf("status = %q, want rolled back to draft so the operator can retry", got.Status)
	}
	if got.LastDecomposeFailure == nil {
		t.Fatal("want LastDecomposeFailure recorded so an operator sees WHY the decompose failed")
	}
	if !strings.Contains(got.LastDecomposeFailure.Detail, `task "Task 2"`) {
		t.Errorf("LastDecomposeFailure.Detail = %q, want it to mention the missing script's task title (Task 2)", got.LastDecomposeFailure.Detail)
	}
	if strings.Contains(got.LastDecomposeFailure.Detail, `task "Task 1"`) {
		t.Errorf("LastDecomposeFailure.Detail = %q, must name only the offending task", got.LastDecomposeFailure.Detail)
	}
}

func TestMissingScriptStreaming_RetryPromptNamesTasks(t *testing.T) {
	plan := &fakeScriptPlanner{resp: func(call int) string {
		if call == 1 {
			return planRespTwoFileTasksOneScript
		}
		return twoFileTasksBothScripted
	}}
	m := newManagerForMissingScript(t, plan.fn, false)
	prd, err := m.CreatePRD("spec", "", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}

	if _, err := m.DecomposeStreaming(prd.ID, nil); err != nil {
		t.Fatalf("DecomposeStreaming: %v (a complete second plan must be accepted)", err)
	}
	if plan.calls != 2 {
		t.Fatalf("planner called %d times, want exactly 2 (call 1 incomplete, call 2 complete; budget 1 allows exactly one retry)", plan.calls)
	}
	if len(plan.specs) < 2 {
		t.Fatal("test setup: expected two captured prompts")
	}
	if strings.Contains(plan.specs[0], "Your previous attempt failed") {
		t.Errorf("call 1's prompt must not carry a retry hint, got %q", plan.specs[0])
	}
	spec2 := plan.specs[1]
	if !strings.Contains(spec2, "Your previous attempt failed") {
		t.Errorf("call 2's prompt missing the retry hint %q", "Your previous attempt failed")
	}
	if !strings.Contains(spec2, `task "Task 2"`) {
		t.Errorf("call 2's prompt must name the missing script's task (task \"Task 2\") so the planner can fix it, got %q", spec2)
	}

	got, ok := m.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatal("PRD vanished")
	}
	if got.Status != PRDNeedsReview {
		t.Errorf("status = %q, want needs_review", got.Status)
	}
	if got.LastDecomposeFailure != nil {
		t.Errorf("LastDecomposeFailure = %+v, want the single-use hint cleared after success", got.LastDecomposeFailure)
	}
	if got.Story == nil || len(got.Story) != 1 || len(got.Story[0].Tasks) != 2 {
		t.Fatalf("want the accepted 2-task plan stored, got %+v", got.Story)
	}
}
