package autonomous

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/config"
)

// Commits the rollup tests move the real head between. They are full-length
// ids so the review-head reader accepts them as commits.
const (
	rollupHeadCommitA = "1111111111111111111111111111111111111111"
	rollupHeadCommitB = "2222222222222222222222222222222222222222"
)

// rollupHeadGit is a fake git runner whose "rev-parse HEAD" answer a test can
// change between rollups. Every other git call fails, which is safe because
// these tests read the rules text from the working tree, a source that never
// asks git for anything but the head.
type rollupHeadGit struct {
	mu   sync.Mutex
	head string
}

func (g *rollupHeadGit) setHead(sha string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.head = sha
}

func (g *rollupHeadGit) run(_ context.Context, _ string, args ...string) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(args) == 2 && args[0] == "rev-parse" && args[1] == "HEAD" {
		return g.head + "\n", nil
	}
	return "", fmt.Errorf("rollup head fake git: unexpected call %v", args)
}

// rollupBlockVerdict is a real content judgment of a problem, which the
// rollup records as a block and holds the Automaton on.
func rollupBlockVerdict(session string) GuardrailVerdict {
	return GuardrailVerdict{
		Outcome:        "block",
		Severity:       "high",
		Summary:        "docs missing",
		Issues:         []string{"update docs/howto/x.md for the new flag"},
		JudgeSessionID: session,
	}
}

// configureRollupFreshReview prepares a Manager for the rollup tests: the
// rules text is read from the working tree, a content block holds the
// Automaton instead of creating corrective work, the judge is the counted
// fake and git is the fake runner reporting commit A as the head.
func configureRollupFreshReview(t *testing.T, m *Manager) (*rollupHeadGit, *rejudgeJudge) {
	t.Helper()
	noInfraSleep(t)
	cfg := m.Config()
	cfg.Automation.RulesReviewSource = config.RulesReviewSourceWorktree
	cfg.VerificationModel = "ollama/qwen3.8:27b"
	cfg.AutoFixRetries = 0
	cfg.RetryPolicy = nil
	m.SetConfig(cfg)
	g := &rollupHeadGit{head: rollupHeadCommitA}
	m.mu.Lock()
	m.driftGit = g.run
	m.mu.Unlock()
	j := &rejudgeJudge{v: rollupBlockVerdict("judge-rollup-a")}
	m.SetGuardrail(j.fn)
	return g, j
}

// rollupOnce runs the completion rollup of an Automaton whose tasks are all
// completed, with the real Manager.Run, and returns the stored Automaton.
func rollupOnce(t *testing.T, m *Manager, prdID string) *PRD {
	t.Helper()
	if err := m.Run(context.Background(), prdID, fakeSpawn, fakeVerify); err != nil {
		t.Fatalf("Run: %v", err)
	}
	p, ok := m.Store().GetPRD(prdID)
	if !ok {
		t.Fatal("GetPRD after Run failed")
	}
	return p
}

// resumeRunning puts a held Automaton back to running, the state a fix lands
// in before the rollup is asked again.
func resumeRunning(t *testing.T, m *Manager, prdID string) {
	t.Helper()
	p, ok := m.Store().GetPRD(prdID)
	if !ok {
		t.Fatal("GetPRD before resume failed")
	}
	m.store.WithLock(func() { p.Status = PRDRunning })
	if err := m.Store().SavePRD(p); err != nil {
		t.Fatalf("SavePRD before resume: %v", err)
	}
}

// heldOnBlockOnCommitA runs the first rollup on head A with a block verdict
// and checks it left one stored block verdict judged on commit A and the
// Automaton held, not completed.
func heldOnBlockOnCommitA(t *testing.T, m *Manager, prdID string, j *rejudgeJudge) GuardrailVerdict {
	t.Helper()
	held := rollupOnce(t, m, prdID)
	if j.calls != 1 {
		t.Fatalf("first rollup judge calls = %d, want 1", j.calls)
	}
	if held.Status != PRDNeedsReview && held.Status != PRDBlocked {
		t.Fatalf("status after the first block = %s, want the Automaton held", held.Status)
	}
	if len(held.Verdicts) != 1 {
		t.Fatalf("verdicts after the first rollup = %d, want 1", len(held.Verdicts))
	}
	stored := held.Verdicts[0]
	if stored.Outcome != "block" || stored.JudgedCommitSHA != rollupHeadCommitA {
		t.Fatalf("stored verdict = outcome %q on commit %q, want a block on %q", stored.Outcome, stored.JudgedCommitSHA, rollupHeadCommitA)
	}
	return stored
}

// assertFreshReviewCompleted checks a rollup that ran the judge again on the
// new head: one more verdict on commit B, the Automaton completed and no
// reuse Decision.
func assertFreshReviewCompleted(t *testing.T, got *PRD, j *rejudgeJudge, first GuardrailVerdict, wantOutcome, wantSession string) {
	t.Helper()
	if j.calls != 2 {
		t.Fatalf("judge calls = %d, want 2 (the changed head forces one new review)", j.calls)
	}
	if got.Status != PRDCompleted {
		t.Fatalf("status = %s (reason %q), want completed after a %s on the new head", got.Status, got.BlockedReason, wantOutcome)
	}
	if got.BlockedReason != "" {
		t.Errorf("BlockedReason = %q, want it cleared", got.BlockedReason)
	}
	if len(got.Verdicts) != 2 {
		t.Fatalf("verdicts = %d, want 2 (the stored block plus one new verdict)", len(got.Verdicts))
	}
	if !verdictsEqualForReview(got.Verdicts[0], first) {
		t.Errorf("stored verdict changed: %+v, want %+v", got.Verdicts[0], first)
	}
	latest := got.Verdicts[1]
	if latest.Outcome != wantOutcome {
		t.Errorf("new verdict outcome = %q, want %q", latest.Outcome, wantOutcome)
	}
	if latest.JudgedCommitSHA != rollupHeadCommitB {
		t.Errorf("new verdict JudgedCommitSHA = %q, want %q", latest.JudgedCommitSHA, rollupHeadCommitB)
	}
	if latest.JudgeSessionID != wantSession {
		t.Errorf("new verdict judge session = %q, want %q", latest.JudgeSessionID, wantSession)
	}
	if !latest.VerdictAt.After(first.VerdictAt) {
		t.Errorf("new verdict VerdictAt = %v, want later than %v", latest.VerdictAt, first.VerdictAt)
	}
	if n := countDecisionKind(got, "guardrail_reuse"); n != 0 {
		t.Errorf("guardrail_reuse decisions = %d, want 0 for a new review", n)
	}
	if lastDecision(got, "stuck_running_blocked") != nil {
		t.Error("a stuck_running_blocked Decision was recorded")
	}
}

// verdictsEqualForReview compares the fields that identify one review run.
func verdictsEqualForReview(a, b GuardrailVerdict) bool {
	return a.Outcome == b.Outcome && a.JudgeSessionID == b.JudgeSessionID &&
		a.JudgedCommitSHA == b.JudgedCommitSHA && a.RulesFingerprint == b.RulesFingerprint &&
		a.VerdictAt.Equal(b.VerdictAt)
}

// A block verdict was stored on commit A; a fix lands so the head is commit
// B. The rollup runs the judge again, the judge passes, the Automaton ends
// completed and exactly one new verdict on commit B is added.
func TestRollupFreshReviewFixLandedPassCompletes(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), fakeDecompose)
	g, j := configureRollupFreshReview(t, m)
	prd, _ := bl451RunningPRD(t, m, []string{"rules-review"}, TaskCompleted)
	first := heldOnBlockOnCommitA(t, m, prd.ID, j)

	g.setHead(rollupHeadCommitB)
	j.v = GuardrailVerdict{Outcome: "pass", Summary: "rules satisfied on the new head", JudgeSessionID: "judge-rollup-pass"}
	resumeRunning(t, m, prd.ID)
	time.Sleep(5 * time.Millisecond)

	got := rollupOnce(t, m, prd.ID)
	assertFreshReviewCompleted(t, got, j, first, "pass", "judge-rollup-pass")
	if got.RulesReviewOutcome != nil {
		t.Errorf("RulesReviewOutcome = %+v, want the stale block record cleared", got.RulesReviewOutcome)
	}
}

// The same with a warn verdict: a warning is not a block, so the Automaton
// ends completed and is not blocked.
func TestRollupFreshReviewFixLandedWarnDoesNotBlock(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), fakeDecompose)
	g, j := configureRollupFreshReview(t, m)
	prd, _ := bl451RunningPRD(t, m, []string{"rules-review"}, TaskCompleted)
	first := heldOnBlockOnCommitA(t, m, prd.ID, j)

	g.setHead(rollupHeadCommitB)
	j.v = GuardrailVerdict{Outcome: "warn", Severity: "low", Summary: "a minor rule is worth a look", JudgeSessionID: "judge-rollup-warn"}
	resumeRunning(t, m, prd.ID)
	time.Sleep(5 * time.Millisecond)

	got := rollupOnce(t, m, prd.ID)
	assertFreshReviewCompleted(t, got, j, first, "warn", "judge-rollup-warn")
	if got.Status == PRDBlocked || got.Status == PRDNeedsReview {
		t.Errorf("status = %s, want a warn never to hold the Automaton", got.Status)
	}
}

// With the head and the rules unchanged the rollup reuses the stored block
// verdict: the judge is not asked, no duplicate verdict is appended and a
// guardrail_reuse Decision says the stored verdict was reused.
func TestRollupFreshReviewUnchangedHeadReusesStoredVerdict(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), fakeDecompose)
	_, j := configureRollupFreshReview(t, m)
	prd, _ := bl451RunningPRD(t, m, []string{"rules-review"}, TaskCompleted)
	first := heldOnBlockOnCommitA(t, m, prd.ID, j)
	before, _ := m.Store().GetPRD(prd.ID)
	verdictDecisions := countDecisionKind(before, "guardrail_verdict")
	if n := countDecisionKind(before, "guardrail_reuse"); n != 0 {
		t.Fatalf("guardrail_reuse decisions after the first rollup = %d, want 0", n)
	}

	resumeRunning(t, m, prd.ID)
	got := rollupOnce(t, m, prd.ID)

	if j.calls != 1 {
		t.Fatalf("judge calls = %d, want 1 (the stored verdict is reused)", j.calls)
	}
	if len(got.Verdicts) != 1 {
		t.Fatalf("verdicts = %d, want 1 (no duplicate appended)", len(got.Verdicts))
	}
	if !verdictsEqualForReview(got.Verdicts[0], first) {
		t.Errorf("stored verdict changed: %+v, want %+v", got.Verdicts[0], first)
	}
	if n := countDecisionKind(got, "guardrail_verdict"); n != verdictDecisions {
		t.Errorf("guardrail_verdict decisions = %d, want %d (no second verdict recorded)", n, verdictDecisions)
	}
	if n := countDecisionKind(got, "guardrail_reuse"); n != 1 {
		t.Fatalf("guardrail_reuse decisions = %d, want exactly 1", n)
	}
	d := lastDecision(got, "guardrail_reuse")
	if !d.VerdictReused {
		t.Error("guardrail_reuse VerdictReused = false, want true")
	}
	if d.JudgedCommitSHA != rollupHeadCommitA {
		t.Errorf("guardrail_reuse JudgedCommitSHA = %q, want %q", d.JudgedCommitSHA, rollupHeadCommitA)
	}
	if d.VerdictOutcome != "block" {
		t.Errorf("guardrail_reuse VerdictOutcome = %q, want block", d.VerdictOutcome)
	}
	if want := reusedVerdictNote(rollupHeadCommitA, first.VerdictAt); d.Note != want {
		t.Errorf("guardrail_reuse note = %q, want %q", d.Note, want)
	}
	if !strings.Contains(d.Note, "stored verdict reused") {
		t.Errorf("guardrail_reuse note = %q, want it to say the stored verdict was reused", d.Note)
	}
	if got.Status != PRDNeedsReview && got.Status != PRDBlocked {
		t.Errorf("status = %s, want the Automaton still held on the reused block", got.Status)
	}
}

// refireRollupEnv is the stuck-running watchdog harness with the refire hook
// running the real completion rollup, and the judge and git fakes wired in.
type refireRollupEnv struct {
	*stuckWatchdogEnv
	g *rollupHeadGit
	j *rejudgeJudge
}

func newRefireRollupEnv(t *testing.T) *refireRollupEnv {
	t.Helper()
	e := newStuckWatchdogEnv(t, 60)
	g, j := configureRollupFreshReview(t, e.m)
	m := e.m
	m.SetStuckRunningHooks(func(id string) (bool, error) {
		e.mu.Lock()
		e.refires = append(e.refires, id)
		e.mu.Unlock()
		return true, m.Run(context.Background(), id, fakeSpawn, fakeVerify)
	}, func(id string) bool {
		e.mu.Lock()
		defer e.mu.Unlock()
		return e.live[id]
	})
	return &refireRollupEnv{stuckWatchdogEnv: e, g: g, j: j}
}

// refireOnOlderHead holds a running, all-terminal Automaton whose stored
// verdict was judged on commit A, moves the head to commit B and drives the
// watchdog past its bound, so the one-time re-fire runs the rollup. It
// returns the stored verdict and the Automaton after the re-fire.
func (e *refireRollupEnv) refireOnOlderHead(t *testing.T, next GuardrailVerdict) (GuardrailVerdict, *PRD) {
	t.Helper()
	prd, _ := bl451RunningPRD(t, e.m, []string{"rules-review"}, TaskCompleted)
	first := heldOnBlockOnCommitA(t, e.m, prd.ID, e.j)

	// A fix landed on the branch, and the Automaton is running again with
	// every task terminal and nothing live, which is what the watchdog finds.
	e.g.setHead(rollupHeadCommitB)
	e.j.v = next
	resumeRunning(t, e.m, prd.ID)
	time.Sleep(5 * time.Millisecond)

	bound := 60 * time.Second
	e.m.sweepStuckRunning() // first sight
	e.advance(bound + time.Second)
	if e.refireCount() != 0 {
		t.Fatalf("refired before the bound passed")
	}
	e.m.sweepStuckRunning() // past the bound: the one re-fire
	if e.refireCount() != 1 {
		t.Fatalf("refires = %d, want 1", e.refireCount())
	}
	got, ok := e.m.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatal("GetPRD after the re-fire failed")
	}
	if !bl451HasDecision(got, "stuck_running_refire") {
		t.Fatal("no stuck_running_refire decision")
	}
	return first, got
}

// assertRefireNotBlocked checks the Automaton the re-fire's rollup completed
// and that the watchdog never blocked it, now or after a further full bound.
func (e *refireRollupEnv) assertRefireNotBlocked(t *testing.T, first GuardrailVerdict, got *PRD, wantOutcome, wantSession string) {
	t.Helper()
	assertFreshReviewCompleted(t, got, e.j, first, wantOutcome, wantSession)
	if got.Status == PRDBlocked {
		t.Fatalf("status = %s, want the re-fire not to leave the Automaton blocked", got.Status)
	}
	if bl451HasDecision(got, "stuck_running_blocked") {
		t.Fatal("a stuck_running_blocked Decision was recorded")
	}

	// Two more full bounds: a completed Automaton is no longer stuck.
	for i := 0; i < 2; i++ {
		e.advance(61 * time.Second)
		e.m.sweepStuckRunning()
	}
	final, _ := e.m.Store().GetPRD(got.ID)
	if final.Status != PRDCompleted {
		t.Errorf("status after later sweeps = %s, want completed", final.Status)
	}
	if bl451HasDecision(final, "stuck_running_blocked") {
		t.Error("a stuck_running_blocked Decision was recorded by a later sweep")
	}
	if e.refireCount() != 1 {
		t.Errorf("refires = %d, want still 1", e.refireCount())
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	// The first review's block writes its own judge-outcome audit entry; the
	// watchdog's block audit must be absent.
	for _, a := range e.audits {
		if a == audit.ActionStuckRunningBlocked {
			t.Errorf("audits = %v, want no %s record", e.audits, audit.ActionStuckRunningBlocked)
		}
	}
}

// The watchdog re-fire of a running, all-terminal Automaton whose stored
// verdict was judged on an older head runs the rollup; a pass judge ends it
// completed, not blocked, with no stuck_running_blocked Decision.
func TestRollupFreshReviewWatchdogRefirePassNotBlocked(t *testing.T) {
	e := newRefireRollupEnv(t)
	first, got := e.refireOnOlderHead(t, GuardrailVerdict{Outcome: "pass", Summary: "rules satisfied on the new head", JudgeSessionID: "judge-refire-pass"})
	e.assertRefireNotBlocked(t, first, got, "pass", "judge-refire-pass")
}

// The same with a warn judge.
func TestRollupFreshReviewWatchdogRefireWarnNotBlocked(t *testing.T) {
	e := newRefireRollupEnv(t)
	first, got := e.refireOnOlderHead(t, GuardrailVerdict{Outcome: "warn", Severity: "low", Summary: "a minor rule is worth a look", JudgeSessionID: "judge-refire-warn"})
	e.assertRefireNotBlocked(t, first, got, "warn", "judge-refire-warn")
}
