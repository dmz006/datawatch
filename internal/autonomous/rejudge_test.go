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
	calls        int
	v            GuardrailVerdict
	err          error
	lastBackend  string
	lastPinned   bool
	lastRejudge  bool // the invocation's Rejudge mark (launch-kind tag for the judge one-shot)
}

func (r *rejudgeJudge) fn(_ context.Context, inv GuardrailInvocation) (GuardrailVerdict, error) {
	r.calls++
	r.lastBackend = inv.GuardrailBackend
	r.lastPinned = inv.GuardrailPinned
	r.lastRejudge = inv.Rejudge
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
	cfg := DefaultConfig()
	// v9.10.18 — the rejudge judge model must resolve to a configured
	// tier; the global verification model (tier 4) keeps the fake
	// judge reachable, as in an operator's real config.
	cfg.VerificationModel = "ollama/qwen3.8:27b"
	m, err := NewManager(t.TempDir(), cfg, fakeDecompose)
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

	got, err := m.RejudgeCompletionGuardrail(context.Background(), prd.ID, "operator", "")
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

	got, err := m.RejudgeCompletionGuardrail(context.Background(), prd.ID, "operator", "")
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

		got, err := m.RejudgeCompletionGuardrail(context.Background(), prd.ID, "operator", "")
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

			got, err := m.RejudgeCompletionGuardrail(context.Background(), prd.ID, "operator", "")
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
		_, err := m.RejudgeCompletionGuardrail(context.Background(), prd.ID, "operator", "")
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

// TestRejudgeRerouteBackendWins — the reroute of the review state whose
// action is "reroute": the operator names the judge backend (typically
// the PRD's stronger guardrail backend) and the re-judgment runs on
// exactly that backend, pinned over whatever tier resolves, with the
// reroute named in the audit trail.
func TestRejudgeRerouteBackendWins(t *testing.T) {
	m, prd, _ := newRejudgeBlockedPRD(t, TaskCompleted)
	rj := newRejudgePassJudge(t)
	m.SetGuardrail(rj.fn)

	got, err := m.RejudgeCompletionGuardrail(context.Background(), prd.ID, "operator", " claude-code-strong ")
	if err != nil {
		t.Fatalf("RejudgeCompletionGuardrail: %v", err)
	}
	if rj.calls != 1 {
		t.Fatalf("judge calls = %d, want 1", rj.calls)
	}
	if rj.lastBackend != "claude-code-strong" {
		t.Errorf("guardrail backend = %q, want claude-code-strong (the reroute wins over the resolved tier)", rj.lastBackend)
	}
	if !rj.lastPinned {
		t.Error("rerouted invocation is not pinned; the runner could silently fall back behind it")
	}
	if got.Status != PRDCompleted {
		t.Errorf("status = %s, want completed after the rerouted pass", got.Status)
	}
	noted := false
	var notes []string
	for _, d := range got.Decisions {
		if d.Kind != "guardrail_rejudge" {
			continue
		}
		notes = append(notes, d.Note)
		if strings.Contains(d.Note, "rerouted to claude-code-strong") {
			noted = true
		}
	}
	if !noted {
		t.Errorf("no guardrail_rejudge decision names the reroute; notes=%v", notes)
	}
}

// TestRejudgeRerouteEnablesRunWhenNoTierResolves — the other half of
// the reroute contract: when no judge tier resolves (the loud
// no-model block the rejudge would otherwise record), naming a backend
// makes the run possible because the reroute names the backend directly.
func TestRejudgeRerouteEnablesRunWhenNoTierResolves(t *testing.T) {
	m := newNoModelShapeManager(t)
	prd, _, _ := newJudgeShapePRD(t, m)

	// The resolved tier is empty: an un-rerouted re-judgment would take
	// the loud no-model path — verify that first.
	m.SetGuardrail(func(_ context.Context, inv GuardrailInvocation) (GuardrailVerdict, error) {
		t.Fatalf("judge must not launch without a resolved tier or a rerouted backend")
		return GuardrailVerdict{}, nil
	})
	v, _, err := m.judgeCompletionRulesReview(context.Background(), prd, "")
	if err != nil {
		t.Fatalf("judgeCompletionRulesReview: %v", err)
	}
	assertLoudVerdict(t, v)

	// With a reroute backend named, the invocation carries it (pinned)
	// and the judge runs.
	rec := &modelJudgeRecorder{}
	m.SetGuardrail(rec.fn)
	v, _, err = m.judgeCompletionRulesReview(context.Background(), prd, "claude-code-strong")
	if err != nil || v.Outcome != "pass" {
		t.Fatalf("rerouted re-judge: verdict=%+v err=%v, want a clean pass on the named backend", v, err)
	}
	if rec.calls != 1 {
		t.Fatalf("judge calls = %d, want 1 (the reroute names the backend directly)", rec.calls)
	}
}

// TestRejudgeInvocationCarriesRejudgeMark — the re-judgment invocation
// (RejudgeCompletionGuardrail → judgeCompletionRulesReview) carries
// Rejudge=true while a first rules-review judgment does not: the launch
// side tags the one-shot judge session launch_kind rejudge versus review
// on that mark (2026-11-06-automata-pages-redesign.md §12). The mark is
// a launch-tag fact only — the judgment itself runs the identical path.
func TestRejudgeInvocationCarriesRejudgeMark(t *testing.T) {
	// First judgment — unmarked.
	mFirst, prdFirst := newRulesReviewVerdictPRD(t)
	first := newRecordedVerdictJudge(t, "pass")
	mFirst.SetGuardrail(first.fn)
	if _, _, err := mFirst.runPRDCompletionRulesReview(context.Background(), prdFirst); err != nil {
		t.Fatalf("runPRDCompletionRulesReview: %v", err)
	}
	if len(first.invs) != 1 {
		t.Fatalf("first-judgment judge calls = %d, want 1", len(first.invs))
	}
	if first.invs[0].Rejudge {
		t.Errorf("first-judgment invocation Rejudge = true, want false")
	}

	// Re-judgment of the same decision — marked.
	mRe, prdRe, _ := newRejudgeBlockedPRD(t, TaskCompleted, TaskCompleted)
	rj := newRejudgePassJudge(t)
	mRe.SetGuardrail(rj.fn)
	if _, err := mRe.RejudgeCompletionGuardrail(context.Background(), prdRe.ID, "operator", ""); err != nil {
		t.Fatalf("RejudgeCompletionGuardrail: %v", err)
	}
	if rj.calls != 1 {
		t.Fatalf("re-judgment judge calls = %d, want 1", rj.calls)
	}
	if !rj.lastRejudge {
		t.Errorf("re-judgment invocation Rejudge = false, want true")
	}
}

// newRejudgeContentBlockJudge returns a fake judge whose fixed verdict is a
// content-category block (a completed judgment, so it is cacheable) with
// the given judge session id and a real output file for cleanup.
func newRejudgeContentBlockJudge(t *testing.T, sessionID string) *rejudgeJudge {
	t.Helper()
	out := filepath.Join(t.TempDir(), "rejudge-content.out")
	if err := os.WriteFile(out, []byte("rejudge content body"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return &rejudgeJudge{v: GuardrailVerdict{
		Outcome:         "block",
		Category:        "content",
		Severity:        "medium",
		Summary:         "AGENT.md rule violated",
		Issues:          []string{"AGENT.md rule violated"},
		JudgeSessionID:  sessionID,
		JudgeOutputPath: out,
	}}
}

// setRejudgeHeadCommit records sha as the PRD's latest attempt commit on its
// first task, which is the anchor judgeVerdictCommitSHA reads for the
// PRD-level rules-review unit. It mirrors a fix landing on the branch.
func setRejudgeHeadCommit(t *testing.T, m *Manager, prdID, sha string) {
	t.Helper()
	fresh, ok := m.Store().GetPRD(prdID)
	if !ok {
		t.Fatal("GetPRD for head commit failed")
	}
	fresh.Story[0].Tasks[0].RecordAttemptCommit(sha)
	if err := m.Store().SavePRD(fresh); err != nil {
		t.Fatalf("SavePRD for head commit: %v", err)
	}
}

// runRejudgeHeadChange judges a blocked PRD once on head "sha-head-1" (judge
// session "judge-head-a"), then moves the head to "sha-head-2" and re-runs
// the rejudge with a fresh judge session "judge-head-b". It returns the
// manager, the PRD id, the counted judge and the PRD after the second run.
func runRejudgeHeadChange(t *testing.T) (*Manager, string, *rejudgeJudge, *PRD) {
	t.Helper()
	m, prd, _ := newRejudgeBlockedPRD(t, TaskCompleted)
	setRejudgeHeadCommit(t, m, prd.ID, "sha-head-1")
	j := newRejudgeContentBlockJudge(t, "judge-head-a")
	m.SetGuardrail(j.fn)
	if _, err := m.RejudgeCompletionGuardrail(context.Background(), prd.ID, "operator", ""); err != nil {
		t.Fatalf("first RejudgeCompletionGuardrail: %v", err)
	}
	if j.calls != 1 {
		t.Fatalf("first run judge calls = %d, want 1", j.calls)
	}

	setRejudgeHeadCommit(t, m, prd.ID, "sha-head-2")
	j.v = newRejudgeContentBlockJudge(t, "judge-head-b").v
	got, err := m.RejudgeCompletionGuardrail(context.Background(), prd.ID, "operator", "")
	if err != nil {
		t.Fatalf("second RejudgeCompletionGuardrail: %v", err)
	}
	return m, prd.ID, j, got
}

// TestRejudgeCompletionGuardrailReplaysWhenHeadUnchanged: with the head
// still at the commit the stored verdict was judged on, a rejudge replays
// that verdict and never invokes the judge, so the judge session id stays
// the same.
func TestRejudgeCompletionGuardrailReplaysWhenHeadUnchanged(t *testing.T) {
	m, prd, _ := newRejudgeBlockedPRD(t, TaskCompleted)
	setRejudgeHeadCommit(t, m, prd.ID, "sha-head-1")
	j := newRejudgeContentBlockJudge(t, "judge-head-a")
	m.SetGuardrail(j.fn)

	if _, err := m.RejudgeCompletionGuardrail(context.Background(), prd.ID, "operator", ""); err != nil {
		t.Fatalf("first RejudgeCompletionGuardrail: %v", err)
	}
	if j.calls != 1 {
		t.Fatalf("first run judge calls = %d, want 1", j.calls)
	}

	got, err := m.RejudgeCompletionGuardrail(context.Background(), prd.ID, "operator", "")
	if err != nil {
		t.Fatalf("second RejudgeCompletionGuardrail: %v", err)
	}
	if j.calls != 1 {
		t.Fatalf("judge calls = %d after an unchanged-head rejudge, want 1 (stored verdict must replay)", j.calls)
	}
	last := got.Verdicts[len(got.Verdicts)-1]
	if last.JudgeSessionID != "judge-head-a" {
		t.Errorf("replayed verdict judge session = %q, want the stored %q", last.JudgeSessionID, "judge-head-a")
	}
	if last.JudgedCommitSHA != "sha-head-1" {
		t.Errorf("replayed verdict judged commit = %q, want %q", last.JudgedCommitSHA, "sha-head-1")
	}
	if got.Status != PRDBlocked {
		t.Errorf("status = %s, want still blocked on a replayed block", got.Status)
	}
}

// TestRejudgeCompletionGuardrailRejudgesWhenHeadChanged: once the head moved,
// the stored verdict no longer describes the code, so the judge runs once
// more on the new head and its verdict becomes the PRD's latest verdict.
func TestRejudgeCompletionGuardrailRejudgesWhenHeadChanged(t *testing.T) {
	m, id, j, got := runRejudgeHeadChange(t)
	if j.calls != 2 {
		t.Fatalf("judge calls = %d, want 2 (one fresh judge on the new head)", j.calls)
	}
	if len(got.Verdicts) == 0 {
		t.Fatal("no verdicts on the PRD after the second rejudge")
	}
	latest := got.Verdicts[len(got.Verdicts)-1]
	if latest.JudgeSessionID != "judge-head-b" {
		t.Errorf("latest verdict judge session = %q, want the fresh %q", latest.JudgeSessionID, "judge-head-b")
	}
	if latest.JudgedCommitSHA != "sha-head-2" {
		t.Errorf("latest verdict judged commit = %q, want %q", latest.JudgedCommitSHA, "sha-head-2")
	}
	stored, ok := m.Store().GetPRD(id)
	if !ok {
		t.Fatal("GetPRD (stored) failed")
	}
	if bl := lastBlockVerdict(stored.Verdicts); bl == nil || bl.JudgeSessionID != "judge-head-b" {
		t.Errorf("stored latest block verdict = %+v, want the fresh judge-head-b verdict", bl)
	}
	if got.Status != PRDBlocked {
		t.Errorf("status = %s, want blocked on the fresh block", got.Status)
	}
}

// newRejudgeHeldPRD builds a PRDNeedsReview PRD holding a content block
// verdict judged on "sha-head-1" with the given hold decision kind.
func newRejudgeHeldPRD(t *testing.T, kind string) (*Manager, *PRD) {
	t.Helper()
	m, prd, _ := newRejudgeBlockedPRD(t, TaskCompleted)
	setRejudgeHeadCommit(t, m, prd.ID, "sha-head-1")
	fresh, _ := m.Store().GetPRD(prd.ID)
	fresh.Verdicts = append(fresh.Verdicts, GuardrailVerdict{
		Outcome: "block", Category: "content", Guardrail: "rules-review",
		Summary: "held finding", JudgedCommitSHA: "sha-head-1",
	})
	fresh.Decisions = append(fresh.Decisions, Decision{Kind: kind, Actor: "autonomous", FailureCategory: "content"})
	fresh.Status = PRDNeedsReview
	if err := m.Store().SavePRD(fresh); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	return m, got
}

func TestRejudgeHeldReviewUnchangedHeadRefused(t *testing.T) {
	m, prd := newRejudgeHeldPRD(t, "rules_review_retries_spent")
	j := newRejudgePassJudge(t)
	m.SetGuardrail(j.fn)
	nv, nd := len(prd.Verdicts), len(prd.Decisions)
	_, err := m.RejudgeCompletionGuardrail(context.Background(), prd.ID, "operator", "")
	if err == nil || !strings.Contains(err.Error(), "sha-head-1") || !strings.Contains(err.Error(), "new commits") {
		t.Fatalf("err = %v, want refusal naming head and new commits", err)
	}
	if j.calls != 0 {
		t.Errorf("judge calls = %d, want 0", j.calls)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	if got.Status != PRDNeedsReview || len(got.Verdicts) != nv || len(got.Decisions) != nd {
		t.Errorf("PRD changed: status=%s verdicts=%d decisions=%d", got.Status, len(got.Verdicts), len(got.Decisions))
	}
}

func TestRejudgeHeldReviewMovedHeadPassCompletes(t *testing.T) {
	m, prd := newRejudgeHeldPRD(t, "rules_review_auto_fix_disabled")
	setRejudgeHeadCommit(t, m, prd.ID, "sha-head-2")
	j := newRejudgePassJudge(t)
	m.SetGuardrail(j.fn)
	got, err := m.RejudgeCompletionGuardrail(context.Background(), prd.ID, "operator", "")
	if err != nil {
		t.Fatalf("rejudge: %v", err)
	}
	if j.calls != 1 {
		t.Errorf("judge calls = %d, want 1", j.calls)
	}
	if got.Status != PRDCompleted || got.BlockedReason != "" {
		t.Errorf("status=%s reason=%q, want completed", got.Status, got.BlockedReason)
	}
}

func TestRejudgeHeldReviewMovedHeadBlockLeavesBlocked(t *testing.T) {
	m, prd := newRejudgeHeldPRD(t, "rules_review_retries_spent")
	setRejudgeHeadCommit(t, m, prd.ID, "sha-head-2")
	j := newRejudgeBlockJudge(t)
	m.SetGuardrail(j.fn)
	got, err := m.RejudgeCompletionGuardrail(context.Background(), prd.ID, "operator", "")
	if err != nil {
		t.Fatalf("rejudge: %v", err)
	}
	if got.Status != PRDBlocked || !strings.Contains(got.BlockedReason, "would cause an incident") {
		t.Errorf("status=%s reason=%q, want blocked with new reason", got.Status, got.BlockedReason)
	}
}

func TestRejudgeHeldReviewDoesNotReplayAcrossHead(t *testing.T) {
	m, prd := newRejudgeHeldPRD(t, "rules_review_retries_spent")
	setRejudgeHeadCommit(t, m, prd.ID, "sha-head-2")
	j := newRejudgeContentBlockJudge(t, "judge-fresh")
	m.SetGuardrail(j.fn)
	got, err := m.RejudgeCompletionGuardrail(context.Background(), prd.ID, "operator", "")
	if err != nil {
		t.Fatalf("rejudge: %v", err)
	}
	if j.calls != 1 {
		t.Fatalf("judge calls = %d, want 1 (held verdict must not replay)", j.calls)
	}
	last := got.Verdicts[len(got.Verdicts)-1]
	if last.JudgeSessionID != "judge-fresh" {
		t.Errorf("latest verdict session = %q, want judge-fresh", last.JudgeSessionID)
	}
}

func TestRejudgeHeldReviewUnchangedHeadRefusedCacheDisabled(t *testing.T) {
	m, prd := newRejudgeHeldPRD(t, "rules_review_retries_spent")
	m.mu.Lock()
	m.judgeVerdictCacheEnabledFn = func() bool { return false }
	m.mu.Unlock()
	j := newRejudgePassJudge(t)
	m.SetGuardrail(j.fn)
	_, err := m.RejudgeCompletionGuardrail(context.Background(), prd.ID, "operator", "")
	if err == nil || !strings.Contains(err.Error(), "new commits") {
		t.Fatalf("err = %v, want unchanged-head refusal with the cache disabled", err)
	}
	if j.calls != 0 {
		t.Errorf("judge calls = %d, want 0", j.calls)
	}
}
