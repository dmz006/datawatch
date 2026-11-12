package autonomous

import (
	"context"
	"strings"
	"testing"
	"time"
)

func countDecisionKind(prd *PRD, kind string) int {
	n := 0
	for _, d := range prd.Decisions {
		if d.Kind == kind {
			n++
		}
	}
	return n
}

// TestRejudgeReuseAddsNoDuplicateVerdict: a second rejudge on an unchanged
// head reuses the stored verdict, so the verdict list and the
// guardrail_verdict Decisions keep one entry per review run, the stored
// verdict keeps its time, commit and fingerprint, and the guardrail_rejudge
// Decision says why it was reused.
func TestRejudgeReuseAddsNoDuplicateVerdict(t *testing.T) {
	m, prd, _ := newRejudgeBlockedPRD(t, TaskCompleted)
	setRejudgeHeadCommit(t, m, prd.ID, "sha-reuse-1")
	j := newRejudgeContentBlockJudge(t, "judge-reuse-a")
	m.SetGuardrail(j.fn)

	first, err := m.RejudgeCompletionGuardrail(context.Background(), prd.ID, "operator", "")
	if err != nil {
		t.Fatalf("first rejudge: %v", err)
	}
	stored := first.Verdicts[len(first.Verdicts)-1]
	nVerdicts := len(first.Verdicts)
	nVerdictDecisions := countDecisionKind(first, "guardrail_verdict")

	got, err := m.RejudgeCompletionGuardrail(context.Background(), prd.ID, "operator", "")
	if err != nil {
		t.Fatalf("second rejudge: %v", err)
	}
	if j.calls != 1 {
		t.Fatalf("judge calls = %d, want 1 (stored verdict reused)", j.calls)
	}
	if len(got.Verdicts) != nVerdicts {
		t.Errorf("verdicts = %d, want %d (no duplicate)", len(got.Verdicts), nVerdicts)
	}
	if n := countDecisionKind(got, "guardrail_verdict"); n != nVerdictDecisions {
		t.Errorf("guardrail_verdict decisions = %d, want %d", n, nVerdictDecisions)
	}
	last := got.Verdicts[len(got.Verdicts)-1]
	if !last.VerdictAt.Equal(stored.VerdictAt) || last.JudgedCommitSHA != stored.JudgedCommitSHA || last.RulesFingerprint != stored.RulesFingerprint {
		t.Errorf("stored verdict changed: %+v, want %+v", last, stored)
	}
	rej := lastDecision(got, "guardrail_rejudge")
	if rej == nil {
		t.Fatal("no guardrail_rejudge decision")
	}
	if !rej.VerdictReused || rej.JudgedCommitSHA != "sha-reuse-1" {
		t.Errorf("decision reused=%v sha=%q, want true and sha-reuse-1", rej.VerdictReused, rej.JudgedCommitSHA)
	}
	if want := reusedVerdictNote("sha-reuse-1", stored.VerdictAt); rej.Note != want {
		t.Errorf("note = %q, want %q", rej.Note, want)
	}
	if got.Status != PRDBlocked || got.BlockedReason == "" {
		t.Errorf("status = %s reason = %q, want blocked with the verdict's reason", got.Status, got.BlockedReason)
	}
}

// TestRejudgeReuseAppendsWhenPRDLacksVerdict: when the reused verdict came
// from the persisted cache and the PRD does not hold it, it is appended
// exactly once, with the cached verdict's own commit.
func TestRejudgeReuseAppendsWhenPRDLacksVerdict(t *testing.T) {
	m, prd, _ := newRejudgeBlockedPRD(t, TaskCompleted)
	setRejudgeHeadCommit(t, m, prd.ID, "sha-reuse-2")
	j := newRejudgeContentBlockJudge(t, "judge-reuse-b")
	m.SetGuardrail(j.fn)
	if _, err := m.RejudgeCompletionGuardrail(context.Background(), prd.ID, "operator", ""); err != nil {
		t.Fatalf("first rejudge: %v", err)
	}
	fresh, _ := m.Store().GetPRD(prd.ID)
	m.Store().WithLock(func() { fresh.Verdicts = nil })
	if err := m.Store().SavePRD(fresh); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}

	got, err := m.RejudgeCompletionGuardrail(context.Background(), prd.ID, "operator", "")
	if err != nil {
		t.Fatalf("second rejudge: %v", err)
	}
	if j.calls != 1 {
		t.Fatalf("judge calls = %d, want 1", j.calls)
	}
	if len(got.Verdicts) != 1 {
		t.Fatalf("verdicts = %d, want exactly 1 appended", len(got.Verdicts))
	}
	if got.Verdicts[0].JudgedCommitSHA != "sha-reuse-2" || got.Verdicts[0].JudgeSessionID != "judge-reuse-b" {
		t.Errorf("appended verdict = %+v", got.Verdicts[0])
	}
	if rej := lastDecision(got, "guardrail_rejudge"); rej == nil || !rej.VerdictReused {
		t.Errorf("rejudge decision = %+v, want reused", rej)
	}
}

// TestRejudgeFreshVerdictNotMarkedReused: a rejudge that runs the judge
// records a new verdict and a Decision that is not marked reused.
func TestRejudgeFreshVerdictNotMarkedReused(t *testing.T) {
	m, id, j, got := runRejudgeHeadChange(t)
	_ = m
	_ = id
	if j.calls != 2 {
		t.Fatalf("judge calls = %d, want 2", j.calls)
	}
	rej := lastDecision(got, "guardrail_rejudge")
	if rej == nil || rej.VerdictReused {
		t.Fatalf("rejudge decision = %+v, want not reused", rej)
	}
	if strings.Contains(rej.Note, "stored verdict reused") {
		t.Errorf("note = %q, must not claim reuse", rej.Note)
	}
}

func TestReusedVerdictNoteWording(t *testing.T) {
	at := time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)
	want := "stored verdict reused: the head commit abc123 and the rules text are unchanged since that verdict was made at 2026-11-11T10:00:00Z; no new review ran"
	if got := reusedVerdictNote("abc123", at); got != want {
		t.Errorf("note = %q, want %q", got, want)
	}
}

func TestVerdictAlreadyRecorded(t *testing.T) {
	at := time.Now()
	v := GuardrailVerdict{VerdictAt: at, JudgedCommitSHA: "s1", JudgeSessionID: "j1"}
	prd := &PRD{Verdicts: []GuardrailVerdict{v}}
	if !verdictAlreadyRecorded(prd, v) {
		t.Error("identical verdict not found")
	}
	for name, o := range map[string]GuardrailVerdict{
		"time":    {VerdictAt: at.Add(time.Second), JudgedCommitSHA: "s1", JudgeSessionID: "j1"},
		"commit":  {VerdictAt: at, JudgedCommitSHA: "s2", JudgeSessionID: "j1"},
		"session": {VerdictAt: at, JudgedCommitSHA: "s1", JudgeSessionID: "j2"},
	} {
		if verdictAlreadyRecorded(prd, o) {
			t.Errorf("%s differs but was reported recorded", name)
		}
	}
	if verdictAlreadyRecorded(&PRD{}, v) {
		t.Error("empty PRD reported recorded")
	}
}

// rejudgeReuseTurnOffVerdictCache switches the judge verdict cache off for
// the manager, the way a runtime config change does.
func rejudgeReuseTurnOffVerdictCache(m *Manager) {
	m.mu.Lock()
	m.judgeVerdictCacheEnabledFn = func() bool { return false }
	m.mu.Unlock()
}

// rejudgeReuseRun runs one rejudge as the operator and fails the test on error.
func rejudgeReuseRun(t *testing.T, m *Manager, prdID string) *PRD {
	t.Helper()
	got, err := m.RejudgeCompletionGuardrail(context.Background(), prdID, "operator", "")
	if err != nil {
		t.Fatalf("RejudgeCompletionGuardrail: %v", err)
	}
	return got
}

// TestRejudgeReuseUnchangedHeadSkipsJudge: after one new review, a second
// rejudge on the same head and the same rules text does not ask the judge,
// says so on its Decision and leaves the verdict list as it was.
func TestRejudgeReuseUnchangedHeadSkipsJudge(t *testing.T) {
	m, prd, _ := newChangedHeadBlockedPRD(t)
	j, _ := judgeOnCommitA(t, m, prd.ID)
	before, _ := m.Store().GetPRD(prd.ID)
	nVerdicts := len(before.Verdicts)

	got := rejudgeReuseRun(t, m, prd.ID)
	if j.calls != 1 {
		t.Fatalf("judge calls = %d, want 1 (the second rejudge reuses the stored verdict)", j.calls)
	}
	d := latestRejudgeDecision(t, got)
	if !d.VerdictReused {
		t.Error("latest guardrail_rejudge VerdictReused = false, want true")
	}
	if !strings.Contains(d.Note, "stored verdict reused") {
		t.Errorf("note = %q, want it to contain %q", d.Note, "stored verdict reused")
	}
	if d.JudgedCommitSHA != changedHeadCommitA {
		t.Errorf("decision JudgedCommitSHA = %q, want %q", d.JudgedCommitSHA, changedHeadCommitA)
	}
	if len(got.Verdicts) != nVerdicts {
		t.Errorf("verdicts = %d, want %d (unchanged)", len(got.Verdicts), nVerdicts)
	}
	if got.Status != PRDBlocked {
		t.Errorf("status = %s, want blocked (a reused block verdict still blocks)", got.Status)
	}
}

// TestRejudgeReuseSevenRejudgesKeepOneVerdict: seven rejudges in a row on an
// unchanged head leave exactly one verdict, with its original time and
// commit, while the decision log holds one guardrail_rejudge Decision per
// request, each carrying the commit.
func TestRejudgeReuseSevenRejudgesKeepOneVerdict(t *testing.T) {
	m, prd, _ := newChangedHeadBlockedPRD(t)
	j := newRejudgeContentBlockJudge(t, "judge-reuse-seven")
	m.SetGuardrail(j.fn)

	const requests = 7
	var got *PRD
	for i := 0; i < requests; i++ {
		got = rejudgeReuseRun(t, m, prd.ID)
		if i == 0 && len(got.Verdicts) != 1 {
			t.Fatalf("after the first rejudge verdicts = %d, want 1", len(got.Verdicts))
		}
	}
	first := got.Verdicts[0]
	if j.calls != 1 {
		t.Errorf("judge calls = %d, want 1 across %d rejudges", j.calls, requests)
	}
	if len(got.Verdicts) != 1 {
		t.Fatalf("verdicts = %d, want exactly 1 after %d rejudges", len(got.Verdicts), requests)
	}
	if first.JudgedCommitSHA != changedHeadCommitA {
		t.Errorf("verdict commit = %q, want %q", first.JudgedCommitSHA, changedHeadCommitA)
	}
	if n := countDecisionKind(got, "guardrail_verdict"); n != 1 {
		t.Errorf("guardrail_verdict decisions = %d, want 1 (only the new review records one)", n)
	}
	if n := countDecisionKind(got, "guardrail_rejudge"); n != requests {
		t.Fatalf("guardrail_rejudge decisions = %d, want %d (one per request)", n, requests)
	}
	reused := 0
	for _, d := range got.Decisions {
		if d.Kind != "guardrail_rejudge" {
			continue
		}
		if d.JudgedCommitSHA != changedHeadCommitA {
			t.Errorf("guardrail_rejudge JudgedCommitSHA = %q, want %q", d.JudgedCommitSHA, changedHeadCommitA)
		}
		if d.VerdictReused {
			reused++
		}
	}
	if reused != requests-1 {
		t.Errorf("reused decisions = %d, want %d", reused, requests-1)
	}
	// The stored copy is still the one made by the first review.
	stored, _ := m.Store().GetPRD(prd.ID)
	if len(stored.Verdicts) != 1 || !stored.Verdicts[0].VerdictAt.Equal(first.VerdictAt) || stored.Verdicts[0].JudgedCommitSHA != first.JudgedCommitSHA {
		t.Errorf("stored verdicts = %+v, want only the original %+v", stored.Verdicts, first)
	}
}

// TestRejudgeReuseStoredPassCompletesWithoutSecondCopy: a stored pass verdict
// reused on a blocked Automaton completes it and appends no second copy.
func TestRejudgeReuseStoredPassCompletesWithoutSecondCopy(t *testing.T) {
	m, prd, _ := newChangedHeadBlockedPRD(t)
	j := newRejudgePassJudge(t)
	m.SetGuardrail(j.fn)

	first := rejudgeReuseRun(t, m, prd.ID)
	if first.Status != PRDCompleted || len(first.Verdicts) == 0 {
		t.Fatalf("first rejudge: status = %s verdicts = %d, want completed with a stored verdict", first.Status, len(first.Verdicts))
	}
	// The count the reuse must leave alone: it is whatever the new review stored.
	nVerdicts := len(first.Verdicts)
	stored := first.Verdicts[nVerdicts-1]

	// The Automaton is blocked again (say, by a later failure), with the same
	// head and rules text, so the stored pass verdict still applies.
	again, _ := m.Store().GetPRD(prd.ID)
	again.Status = PRDBlocked
	again.BlockedReason = "blocked again after the first pass"
	if err := m.Store().SavePRD(again); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}

	got := rejudgeReuseRun(t, m, prd.ID)
	if j.calls != 1 {
		t.Fatalf("judge calls = %d, want 1 (the stored pass is reused)", j.calls)
	}
	if got.Status != PRDCompleted || got.BlockedReason != "" {
		t.Errorf("status = %s reason = %q, want completed with the reason cleared", got.Status, got.BlockedReason)
	}
	if len(got.Verdicts) != nVerdicts {
		t.Fatalf("verdicts = %d, want %d (no second copy)", len(got.Verdicts), nVerdicts)
	}
	if last := got.Verdicts[len(got.Verdicts)-1]; !last.VerdictAt.Equal(stored.VerdictAt) || last.JudgedCommitSHA != stored.JudgedCommitSHA {
		t.Errorf("verdict changed: %+v, want %+v", last, stored)
	}
	if d := latestRejudgeDecision(t, got); !d.VerdictReused || !strings.Contains(d.Note, "stored verdict reused") {
		t.Errorf("decision = %+v, want a reused verdict with the reuse note", d)
	}
	persisted, _ := m.Store().GetPRD(prd.ID)
	if persisted.Status != PRDCompleted || len(persisted.Verdicts) != nVerdicts {
		t.Errorf("persisted status = %s verdicts = %d, want completed with %d", persisted.Status, len(persisted.Verdicts), nVerdicts)
	}
}

// TestRejudgeReuseCachedVerdictAppendedOnce: a verdict that exists only in the
// persisted verdict cache (the PRD holds none) is appended exactly once on
// reuse, and a further reuse does not add another copy.
func TestRejudgeReuseCachedVerdictAppendedOnce(t *testing.T) {
	m, prd, _ := newChangedHeadBlockedPRD(t)
	j, _ := judgeOnCommitA(t, m, prd.ID)

	fresh, _ := m.Store().GetPRD(prd.ID)
	m.Store().WithLock(func() { fresh.Verdicts = nil })
	if err := m.Store().SavePRD(fresh); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}

	got := rejudgeReuseRun(t, m, prd.ID)
	if j.calls != 1 {
		t.Fatalf("judge calls = %d, want 1 (the cached verdict is reused)", j.calls)
	}
	if len(got.Verdicts) != 1 {
		t.Fatalf("verdicts = %d, want exactly 1 appended", len(got.Verdicts))
	}
	if got.Verdicts[0].JudgedCommitSHA != changedHeadCommitA || got.Verdicts[0].JudgeSessionID != "judge-changed-head-a" {
		t.Errorf("appended verdict = %+v, want the cached verdict on commit %s", got.Verdicts[0], changedHeadCommitA)
	}
	if d := latestRejudgeDecision(t, got); !d.VerdictReused {
		t.Error("guardrail_rejudge VerdictReused = false, want true")
	}

	got = rejudgeReuseRun(t, m, prd.ID)
	if j.calls != 1 || len(got.Verdicts) != 1 {
		t.Errorf("after a further reuse: judge calls = %d verdicts = %d, want 1 and 1", j.calls, len(got.Verdicts))
	}
}

// TestRejudgeReuseCacheOffRunsEveryReview: with the verdict cache turned off,
// every rejudge runs a new review and adds exactly one verdict per run.
func TestRejudgeReuseCacheOffRunsEveryReview(t *testing.T) {
	m, prd, _ := newChangedHeadBlockedPRD(t)
	rejudgeReuseTurnOffVerdictCache(m)
	j := newRejudgeContentBlockJudge(t, "judge-reuse-cache-off")
	m.SetGuardrail(j.fn)

	const runs = 3
	for i := 1; i <= runs; i++ {
		got := rejudgeReuseRun(t, m, prd.ID)
		if j.calls != i {
			t.Fatalf("run %d: judge calls = %d, want %d (a new review every time)", i, j.calls, i)
		}
		if len(got.Verdicts) != i {
			t.Fatalf("run %d: verdicts = %d, want %d (one per run)", i, len(got.Verdicts), i)
		}
		d := latestRejudgeDecision(t, got)
		if d.VerdictReused {
			t.Errorf("run %d: VerdictReused = true, want false with the cache off", i)
		}
		if strings.Contains(d.Note, "stored verdict reused") {
			t.Errorf("run %d: note = %q, must not claim reuse", i, d.Note)
		}
		if d.JudgedCommitSHA != changedHeadCommitA {
			t.Errorf("run %d: decision JudgedCommitSHA = %q, want %q", i, d.JudgedCommitSHA, changedHeadCommitA)
		}
	}
}

// TestRejudgeReuseInfraBlockNeverReused: an infrastructure block (the judge
// timed out) is not a completed review, so the next rejudge runs the judge
// again instead of reusing it.
func TestRejudgeReuseInfraBlockNeverReused(t *testing.T) {
	m, prd, _ := newChangedHeadBlockedPRD(t)
	j := newRejudgeBlockJudge(t)
	j.v.Category = "infra"
	j.v.Summary = "judge timed out waiting for a verdict"
	j.v.Issues = []string{j.v.Summary}
	m.SetGuardrail(j.fn)

	first := rejudgeReuseRun(t, m, prd.ID)
	callsAfterFirst := j.calls
	if callsAfterFirst < 1 {
		t.Fatalf("first rejudge: judge calls = %d, want at least 1", callsAfterFirst)
	}
	if d := latestRejudgeDecision(t, first); d.VerdictReused {
		t.Fatal("first rejudge: VerdictReused = true, want false")
	}

	got := rejudgeReuseRun(t, m, prd.ID)
	if j.calls <= callsAfterFirst {
		t.Fatalf("judge calls = %d after the second rejudge, want more than %d (an infra block is never reused)", j.calls, callsAfterFirst)
	}
	d := latestRejudgeDecision(t, got)
	if d.VerdictReused {
		t.Error("second rejudge: VerdictReused = true, want false")
	}
	if strings.Contains(d.Note, "stored verdict reused") {
		t.Errorf("second rejudge: note = %q, must not claim reuse", d.Note)
	}
	if got.Status != PRDBlocked {
		t.Errorf("status = %s, want blocked", got.Status)
	}
}
