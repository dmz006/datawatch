package autonomous

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// rejudgeJudge is a counted GuardrailFn fake. It records every invocation
// and returns one fixed (verdict, error) pair, so a test can assert both
// the judgment's effect and whether the judge was asked at all.
type rejudgeJudge struct {
	calls int
	v     GuardrailVerdict
	err   error
}

func (r *rejudgeJudge) fn(_ context.Context, _ GuardrailInvocation) (GuardrailVerdict, error) {
	r.calls++
	return r.v, r.err
}

// newRejudgePassJudge / newRejudgeBlockJudge build the fixed verdict the
// fake returns, following newRecordedVerdictJudge's pattern: the verdict
// carries a judge session id and a JudgeOutputPath pointing at a real
// file inside t.TempDir() so recordGuardrailVerdict's cleanup has
// something to remove.
func newRejudgePassJudge(t *testing.T) *rejudgeJudge {
	t.Helper()
	out := filepath.Join(t.TempDir(), "rejudge-verdict.out")
	if err := os.WriteFile(out, []byte("rejudge verdict body"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return &rejudgeJudge{v: GuardrailVerdict{
		Outcome:         "pass",
		Summary:         "rules satisfied on re-review",
		JudgeSessionID:  "judge-rejudge-pass-7f3a",
		JudgeOutputPath: out,
	}}
}

func newRejudgeBlockJudge(t *testing.T) *rejudgeJudge {
	t.Helper()
	out := filepath.Join(t.TempDir(), "rejudge-verdict.out")
	if err := os.WriteFile(out, []byte("rejudge verdict body"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return &rejudgeJudge{v: GuardrailVerdict{
		Outcome:         "block",
		Category:        "policy",
		Severity:        "high",
		Summary:         "would cause an incident",
		Issues:          []string{"would cause an incident"},
		JudgeSessionID:  "judge-rejudge-block-7f3a",
		JudgeOutputPath: out,
	}}
}

// newRejudgeBlockedPRD returns a manager and a FAKE PRD (project dir in
// t.TempDir(), never a real one) that is PRDBlocked on a prior
// rules-review finding, with the given per-task statuses and rules-review
// opted in at the PRD level. The pre-existing BlockedReason is returned
// so a test can verify whether a re-judge kept or replaced it.
func newRejudgeBlockedPRD(t *testing.T, statuses ...TaskStatus) (*Manager, *PRD, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte("rules"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := NewManager(t.TempDir(), DefaultConfig(), fakeDecompose)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("spec", dir, "claude", "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if len(statuses) == 0 {
		statuses = []TaskStatus{TaskCompleted}
	}
	tasks := make([]Task, 0, len(statuses))
	for _, st := range statuses {
		tasks = append(tasks, Task{Title: "T", Spec: "do", Status: st})
	}
	if err := m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: tasks}}); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	prd, ok := m.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatal("GetPRD after SetStories failed")
	}
	prd.PerPRDGuardrails = []string{"rules-review"}
	prd.Status = PRDBlocked
	prd.BlockedReason = "guardrail rules-review returned block (judge-prior-1) - prior finding"
	// Re-apply per-task statuses through the stored PRD: SetStories may
	// normalize fields (ids, timestamps), so mutate the authoritative copy.
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			prd.Story[si].Tasks[ti].Status = statuses[ti]
		}
	}
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	prd, ok = m.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatal("GetPRD after SavePRD failed")
	}
	return m, prd, prd.BlockedReason
}

// taskStatusSummaries returns the (task id, status) pairs in story/task
// order so a test can assert that RejudgeCompletionGuardrail never moved
// a task.
func taskStatusSummaries(prd *PRD) []string {
	out := make([]string, 0)
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			out = append(out, fmt.Sprintf("%s=%s", prd.Story[si].Tasks[ti].ID, prd.Story[si].Tasks[ti].Status))
		}
	}
	return out
}

func decisionKinds(prd *PRD) []string {
	out := make([]string, 0, len(prd.Decisions))
	for _, d := range prd.Decisions {
		out = append(out, d.Kind)
	}
	return out
}

func TestRejudgeCompletionGuardrailPass(t *testing.T) {
	m, prd, _ := newRejudgeBlockedPRD(t, TaskCompleted, TaskCompleted)
	rj := newRejudgePassJudge(t)
	m.SetGuardrail(rj.fn)
	_before := taskStatusSummaries(prd)

	got, err := m.RejudgeCompletionGuardrail(context.Background(), prd.ID, "operator")
	if err != nil {
		t.Fatalf("RejudgeCompletionGuardrail: %v", err)
	}
	if got.Status != PRDCompleted {
		t.Errorf("status = %s, want completed", got.Status)
	}
	if got.BlockedReason != "" {
		t.Errorf("BlockedReason = %q, want empty after a pass", got.BlockedReason)
	}
	if rj.calls != 1 {
		t.Errorf("judge calls = %d, want 1", rj.calls)
	}
	if want := _before; strings.Join(taskStatusSummaries(got), ",") != strings.Join(want, ",") {
		t.Errorf("task statuses changed: before %v, after %v", want, taskStatusSummaries(got))
	}
	// The durable store copy must agree as well.
	stored, ok := m.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatal("GetPRD (stored) failed")
	}
	if stored.Status != PRDCompleted || stored.BlockedReason != "" {
		t.Errorf("stored status = %s, BlockedReason = %q, want completed/empty", stored.Status, stored.BlockedReason)
	}

	kinds := decisionKinds(got)
	if !kindsContains(kinds, "guardrail_verdict") {
		t.Errorf("Decisions %v: missing a guardrail_verdict Decision", kinds)
	}
	if !kindsContains(kinds, "guardrail_rejudge") {
		t.Errorf("Decisions %v: missing a guardrail_rejudge Decision", kinds)
	}
	// The guardrail_verdict Decision must carry the pass verdict's judge session.
	foundVerdict := false
	for _, d := range got.Decisions {
		switch d.Kind {
		case "guardrail_verdict":
			foundVerdict = true
			if d.VerdictOutcome != "pass" {
				t.Errorf("guardrail_verdict VerdictOutcome = %q, want pass", d.VerdictOutcome)
			}
			if d.JudgeSessionID != rj.v.JudgeSessionID {
				t.Errorf("guardrail_verdict JudgeSessionID = %q, want %q", d.JudgeSessionID, rj.v.JudgeSessionID)
			}
		case "guardrail_rejudge":
			if d.Actor != "operator" {
				t.Errorf("guardrail_rejudge Actor = %q, want operator", d.Actor)
			}
			if d.VerdictOutcome != "pass" {
				t.Errorf("guardrail_rejudge VerdictOutcome = %q, want pass", d.VerdictOutcome)
			}
		}
	}
	if !foundVerdict {
		t.Errorf("no guardrail_verdict Decision among %v", kinds)
	}
	if _, err := os.Stat(rj.v.JudgeOutputPath); !os.IsNotExist(err) {
		t.Errorf("judge output still present after the saved verdict: err=%v", err)
	}
}

func TestRejudgeCompletionGuardrailFail(t *testing.T) {
	m, prd, _ := newRejudgeBlockedPRD(t, TaskCompleted)
	rj := newRejudgeBlockJudge(t)
	m.SetGuardrail(rj.fn)

	got, err := m.RejudgeCompletionGuardrail(context.Background(), prd.ID, "operator")
	if err != nil {
		t.Fatalf("RejudgeCompletionGuardrail: %v", err)
	}
	if got.Status != PRDBlocked {
		t.Errorf("status = %s, want blocked after a fail", got.Status)
	}
	if !strings.Contains(got.BlockedReason, "rules-review") {
		t.Errorf("BlockedReason %q does not name the rules-review guardrail", got.BlockedReason)
	}
	if rj.calls != 1 {
		t.Errorf("judge calls = %d, want 1", rj.calls)
	}

	kinds := decisionKinds(got)
	if !kindsContains(kinds, "guardrail_verdict") {
		t.Errorf("Decisions %v: missing a guardrail_verdict Decision", kinds)
	}
	if !kindsContains(kinds, "guardrail_rejudge") {
		t.Errorf("Decisions %v: missing a guardrail_rejudge Decision", kinds)
	}
	for _, d := range got.Decisions {
		switch d.Kind {
		case "guardrail_verdict":
			if d.VerdictOutcome != "block" {
				t.Errorf("guardrail_verdict VerdictOutcome = %q, want block", d.VerdictOutcome)
			}
		case "guardrail_rejudge":
			if d.Actor != "operator" {
				t.Errorf("guardrail_rejudge Actor = %q, want operator", d.Actor)
			}
			if d.VerdictOutcome != "block" {
				t.Errorf("guardrail_rejudge VerdictOutcome = %q, want block", d.VerdictOutcome)
			}
		}
	}
}

func TestRejudgeCompletionGuardrailNoVerdict(t *testing.T) {
	// (c) the judgment yields no verdict: the PRD stays blocked with its
	// pre-existing BlockedReason, a guardrail_rejudge Decision is
	// recorded, and an error is returned so the operator sees it.
	run := func(j *rejudgeJudge) {
		t.Helper()
		m, prd, oldReason := newRejudgeBlockedPRD(t, TaskCompleted)
		m.SetGuardrail(j.fn)
		_before := taskStatusSummaries(prd)
		staleDecisions := len(prd.Decisions)

		got, err := m.RejudgeCompletionGuardrail(context.Background(), prd.ID, "operator")
		if err == nil {
			t.Fatal("RejudgeCompletionGuardrail: want an error when no verdict is produced, got nil")
		}
		if got != nil {
			t.Errorf("returned PRD = %+v, want nil with an error", got)
		}
		if r := j.calls; r != 1 {
			t.Errorf("judge calls = %d, want 1", r)
		}
		stored, ok := m.Store().GetPRD(prd.ID)
		if !ok {
			t.Fatal("GetPRD (stored) failed")
		}
		if stored.Status != PRDBlocked {
			t.Errorf("status = %s, want still blocked", stored.Status)
		}
		if stored.BlockedReason != oldReason {
			t.Errorf("BlockedReason = %q, want the previous reason %q kept", stored.BlockedReason, oldReason)
		}
		if strings.Join(taskStatusSummaries(stored), ",") != strings.Join(_before, ",") {
			t.Errorf("task statuses changed: before %v, after %v", _before, taskStatusSummaries(stored))
		}
		foundRejudge := false
		for _, d := range stored.Decisions {
			if d.Kind == "guardrail_rejudge" {
				foundRejudge = true
			}
		}
		if !foundRejudge {
			t.Errorf("no guardrail_rejudge Decision in %+v", stored.Decisions)
		}
		if len(stored.Decisions) <= staleDecisions {
			t.Errorf("Decisions count = %d, want a new guardrail_rejudge Decision added beyond the prior %d", len(stored.Decisions), staleDecisions)
		}
	}

	t.Run("empty outcome", func(t *testing.T) {
		run(&rejudgeJudge{v: GuardrailVerdict{Outcome: ""}})
	})
	t.Run("judge error", func(t *testing.T) {
		run(&rejudgeJudge{err: fmt.Errorf("judge session exploded")})
	})
}

func TestRejudgeCompletionGuardrailRefuses(t *testing.T) {
	// (d) refusal: any task not at TaskCompleted blocks the re-judge with
	// an error that names the task, the judge is NEVER called, and no
	// Decision is added. A PRD that is not blocked is refused too.
	refuse := func(name, wantErrContains string, mutate func(m *Manager, prd *PRD)) {
		t.Run(name, func(t *testing.T) {
			m, prd, _ := newRejudgeBlockedPRD(t, TaskCompleted)
			if mutate != nil {
				mutate(m, prd)
			}
			rj := newRejudgePassJudge(t)
			m.SetGuardrail(rj.fn)
			staleDecisions := len(prd.Decisions)

			got, err := m.RejudgeCompletionGuardrail(context.Background(), prd.ID, "operator")
			if err == nil {
				t.Fatal("RejudgeCompletionGuardrail: want a refusal error, got nil")
			}
			if wantErrContains != "" && !strings.Contains(err.Error(), wantErrContains) {
				t.Errorf("refusal error %q does not contain %q", err, wantErrContains)
			}
			if got != nil {
				t.Errorf("returned PRD = %+v, want nil with an error", got)
			}
			if rj.calls != 0 {
				t.Errorf("judge calls = %d, want 0 (refusal happens before the judge runs)", rj.calls)
			}
			stored, ok := m.Store().GetPRD(prd.ID)
			if !ok {
				t.Fatal("GetPRD (stored) failed")
			}
			if len(stored.Decisions) != staleDecisions {
				t.Errorf("Decisions count = %d, want unchanged (%d) after a refusal", len(stored.Decisions), staleDecisions)
			}
			if kindsContains(decisionKinds(stored), "guardrail_rejudge") {
				t.Error("a guardrail_rejudge Decision exists after a refusal; none may be added")
			}
		})
	}

	setTaskStatus := func(st TaskStatus) func(m *Manager, prd *PRD) {
		return func(m *Manager, prd *PRD) {
			for si := range prd.Story {
				for ti := range prd.Story[si].Tasks {
					prd.Story[si].Tasks[ti].Status = st
				}
			}
			if err := m.Store().SavePRD(prd); err != nil {
				t.Fatalf("SavePRD: %v", err)
			}
		}
	}

	// The non-completed subtests' refusal error must name the offending task.
	refuse("pending task names the task", "not completed; every task must be completed", setTaskStatus(TaskPending))
	refuse("failed task names the task", "not completed; every task must be completed", setTaskStatus(TaskFailed))
	refuse("prd not blocked", "not blocked", func(m *Manager, prd *PRD) {
		prd.Status = PRDApproved
		if err := m.Store().SavePRD(prd); err != nil {
			t.Fatalf("SavePRD: %v", err)
		}
	})

	// Refusing a PRD with a pending task: the error must name the task id.
	t.Run("pending task error names task id", func(t *testing.T) {
		m, prd, _ := newRejudgeBlockedPRD(t, TaskCompleted)
		setTaskStatus(TaskPending)(m, prd)
		m.SetGuardrail(newRejudgePassJudge(t).fn)
		_, err := m.RejudgeCompletionGuardrail(context.Background(), prd.ID, "operator")
		if err == nil {
			t.Fatal("want refusal error for a pending task, got nil")
		}
		bad, _, status := firstNonCompletedTask(prd)
		if status != TaskPending {
			t.Fatalf("firstNonCompletedTask status = %s, want pending", status)
		}
		if !strings.Contains(err.Error(), bad) {
			t.Errorf("refusal error %q does not name task %s", err, bad)
		}
	})
}

func kindsContains(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}
