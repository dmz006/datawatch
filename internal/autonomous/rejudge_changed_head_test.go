package autonomous

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// Commits the changed-head tests move the real head between. They are
// full-length ids so the review-head reader accepts them as commits.
const (
	changedHeadCommitA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	changedHeadCommitB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

// changedHeadGit is a fake git runner whose "rev-parse HEAD" answer a test
// can change between runs. Every other git call fails, which is safe
// because these tests read the rules text from the working tree, a source
// that never asks git for anything but the head.
type changedHeadGit struct {
	mu    sync.Mutex
	head  string
	calls [][]string
}

func (g *changedHeadGit) setHead(sha string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.head = sha
}

func (g *changedHeadGit) run(_ context.Context, _ string, args ...string) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls = append(g.calls, append([]string(nil), args...))
	if len(args) == 2 && args[0] == "rev-parse" && args[1] == "HEAD" {
		return g.head + "\n", nil
	}
	return "", fmt.Errorf("changed-head fake git: unexpected call %v", args)
}

// newChangedHeadBlockedPRD returns a blocked Automaton whose real head the
// returned fake git runner reports as commit A. The rules text is read from
// the working tree, so the fake never has to answer base-commit lookups.
func newChangedHeadBlockedPRD(t *testing.T) (*Manager, *PRD, *changedHeadGit) {
	t.Helper()
	m, prd, _ := newRejudgeBlockedPRD(t, TaskCompleted)
	cfg := m.Config()
	cfg.Automation.RulesReviewSource = config.RulesReviewSourceWorktree
	// A configured judge backend, so the tests can tell it from a reroute.
	cfg.VerificationBackend = "ollama"
	m.SetConfig(cfg)
	g := &changedHeadGit{head: changedHeadCommitA}
	m.mu.Lock()
	m.driftGit = g.run
	m.mu.Unlock()
	return m, prd, g
}

// judgeOnCommitA runs the first rejudge on head A with a content block
// verdict, which leaves a stored block verdict judged on commit A. The
// recorded task attempt commits are not touched.
func judgeOnCommitA(t *testing.T, m *Manager, prdID string) (*rejudgeJudge, GuardrailVerdict) {
	t.Helper()
	j := newRejudgeContentBlockJudge(t, "judge-changed-head-a")
	m.SetGuardrail(j.fn)
	got, err := m.RejudgeCompletionGuardrail(context.Background(), prdID, "operator", "")
	if err != nil {
		t.Fatalf("first RejudgeCompletionGuardrail: %v", err)
	}
	if j.calls != 1 {
		t.Fatalf("first run judge calls = %d, want 1", j.calls)
	}
	stored := lastBlockVerdict(got.Verdicts)
	if stored == nil {
		t.Fatal("first run left no stored block verdict")
	}
	if stored.JudgedCommitSHA != changedHeadCommitA {
		t.Fatalf("stored verdict judged commit = %q, want %q", stored.JudgedCommitSHA, changedHeadCommitA)
	}
	if got.Status != PRDBlocked {
		t.Fatalf("status after the first block = %s, want blocked", got.Status)
	}
	return j, *stored
}

// latestRejudgeDecision returns the newest guardrail_rejudge Decision.
func latestRejudgeDecision(t *testing.T, prd *PRD) Decision {
	t.Helper()
	for i := len(prd.Decisions) - 1; i >= 0; i-- {
		if prd.Decisions[i].Kind == "guardrail_rejudge" {
			return prd.Decisions[i]
		}
	}
	t.Fatalf("no guardrail_rejudge Decision among %v", decisionKinds(prd))
	return Decision{}
}

// TestRejudgeChangedHeadFixLandedPassCompletes: a fix lands so the real head
// is commit B while the recorded attempt commits stay as they were. The
// rejudge asks the judge again, records the new verdict against commit B and
// completes the Automaton on a pass.
func TestRejudgeChangedHeadFixLandedPassCompletes(t *testing.T) {
	m, prd, g := newChangedHeadBlockedPRD(t)
	j, first := judgeOnCommitA(t, m, prd.ID)

	g.setHead(changedHeadCommitB)
	pass := newRejudgePassJudge(t)
	j.v = pass.v
	// Make the new verdict's own time unmistakably later than the first.
	time.Sleep(5 * time.Millisecond)

	got, err := m.RejudgeCompletionGuardrail(context.Background(), prd.ID, "operator", "")
	if err != nil {
		t.Fatalf("second RejudgeCompletionGuardrail: %v", err)
	}
	if j.calls != 2 {
		t.Fatalf("judge calls = %d, want 2 (the changed head forces one new review)", j.calls)
	}

	latest := got.Verdicts[len(got.Verdicts)-1]
	if latest.JudgeSessionID != pass.v.JudgeSessionID {
		t.Errorf("new verdict judge session = %q, want the fake's %q", latest.JudgeSessionID, pass.v.JudgeSessionID)
	}
	if latest.JudgeSessionID == first.JudgeSessionID {
		t.Errorf("new verdict reused the first judge session %q", first.JudgeSessionID)
	}
	if latest.JudgedCommitSHA != changedHeadCommitB {
		t.Errorf("new verdict JudgedCommitSHA = %q, want %q", latest.JudgedCommitSHA, changedHeadCommitB)
	}
	if latest.VerdictAt.IsZero() || !latest.VerdictAt.After(first.VerdictAt) {
		t.Errorf("new verdict VerdictAt = %v, want a time later than the first %v", latest.VerdictAt, first.VerdictAt)
	}
	if latest.Outcome != "pass" {
		t.Errorf("new verdict outcome = %q, want pass", latest.Outcome)
	}

	d := latestRejudgeDecision(t, got)
	if d.JudgedCommitSHA != changedHeadCommitB {
		t.Errorf("guardrail_rejudge JudgedCommitSHA = %q, want %q", d.JudgedCommitSHA, changedHeadCommitB)
	}
	if d.VerdictReused {
		t.Error("guardrail_rejudge VerdictReused = true, want false for a new review")
	}

	if got.Status != PRDCompleted {
		t.Errorf("status = %s, want completed after a pass on the new head", got.Status)
	}
	if got.BlockedReason != "" {
		t.Errorf("BlockedReason = %q, want cleared after a pass", got.BlockedReason)
	}
	stored, ok := m.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatal("GetPRD (stored) failed")
	}
	if stored.Status != PRDCompleted || stored.BlockedReason != "" {
		t.Errorf("stored status = %s, BlockedReason = %q, want completed and empty", stored.Status, stored.BlockedReason)
	}
}

// TestRejudgeChangedHeadFixLandedBlockStaysBlocked: the same setup with a
// block verdict on the new head leaves the Automaton blocked, and the
// blocked reason comes from the new verdict, not the stored one.
func TestRejudgeChangedHeadFixLandedBlockStaysBlocked(t *testing.T) {
	m, prd, g := newChangedHeadBlockedPRD(t)
	j, first := judgeOnCommitA(t, m, prd.ID)

	g.setHead(changedHeadCommitB)
	fresh := newRejudgeContentBlockJudge(t, "judge-changed-head-b")
	fresh.v.Summary = "a different rule is still violated on the new head"
	fresh.v.Issues = []string{fresh.v.Summary}
	j.v = fresh.v
	time.Sleep(5 * time.Millisecond)

	got, err := m.RejudgeCompletionGuardrail(context.Background(), prd.ID, "operator", "")
	if err != nil {
		t.Fatalf("second RejudgeCompletionGuardrail: %v", err)
	}
	if j.calls != 2 {
		t.Fatalf("judge calls = %d, want 2", j.calls)
	}
	if got.Status != PRDBlocked {
		t.Fatalf("status = %s, want blocked on a block verdict", got.Status)
	}

	latest := got.Verdicts[len(got.Verdicts)-1]
	if latest.JudgeSessionID != "judge-changed-head-b" {
		t.Errorf("new verdict judge session = %q, want %q", latest.JudgeSessionID, "judge-changed-head-b")
	}
	if latest.JudgedCommitSHA != changedHeadCommitB {
		t.Errorf("new verdict JudgedCommitSHA = %q, want %q", latest.JudgedCommitSHA, changedHeadCommitB)
	}
	if !latest.VerdictAt.After(first.VerdictAt) {
		t.Errorf("new verdict VerdictAt = %v, want later than %v", latest.VerdictAt, first.VerdictAt)
	}
	if !strings.Contains(got.BlockedReason, fresh.v.Summary) {
		t.Errorf("BlockedReason = %q, want it to carry the new verdict's finding %q", got.BlockedReason, fresh.v.Summary)
	}
	if strings.Contains(got.BlockedReason, first.Summary) {
		t.Errorf("BlockedReason = %q still carries the stored finding %q", got.BlockedReason, first.Summary)
	}

	d := latestRejudgeDecision(t, got)
	if d.JudgedCommitSHA != changedHeadCommitB || d.VerdictReused {
		t.Errorf("guardrail_rejudge JudgedCommitSHA = %q, VerdictReused = %v, want %q and false", d.JudgedCommitSHA, d.VerdictReused, changedHeadCommitB)
	}
}

// TestRejudgeChangedHeadEditedRulesForcesNewReview: the head is unchanged but
// the rules file was edited, so the stored verdict no longer describes the
// rules text and the judge runs again.
func TestRejudgeChangedHeadEditedRulesForcesNewReview(t *testing.T) {
	m, prd, _ := newChangedHeadBlockedPRD(t)
	j, first := judgeOnCommitA(t, m, prd.ID)

	// Control: with the head and the rules both unchanged the stored verdict
	// replays and the judge is not asked.
	got, err := m.RejudgeCompletionGuardrail(context.Background(), prd.ID, "operator", "")
	if err != nil {
		t.Fatalf("unchanged RejudgeCompletionGuardrail: %v", err)
	}
	if j.calls != 1 {
		t.Fatalf("judge calls = %d after an unchanged rejudge, want 1 (stored verdict replays)", j.calls)
	}
	if d := latestRejudgeDecision(t, got); !d.VerdictReused {
		t.Fatal("unchanged rejudge: VerdictReused = false, want true")
	}

	if err := os.WriteFile(filepath.Join(prd.ProjectDir, "AGENT.md"), []byte("rules, edited after the first review"), 0o644); err != nil {
		t.Fatalf("edit rules file: %v", err)
	}
	j.v = newRejudgeContentBlockJudge(t, "judge-changed-head-rules").v
	time.Sleep(5 * time.Millisecond)

	got, err = m.RejudgeCompletionGuardrail(context.Background(), prd.ID, "operator", "")
	if err != nil {
		t.Fatalf("edited-rules RejudgeCompletionGuardrail: %v", err)
	}
	if j.calls != 2 {
		t.Fatalf("judge calls = %d, want 2 (an edited rules file forces a new review)", j.calls)
	}
	latest := got.Verdicts[len(got.Verdicts)-1]
	if latest.JudgeSessionID != "judge-changed-head-rules" {
		t.Errorf("latest verdict judge session = %q, want %q", latest.JudgeSessionID, "judge-changed-head-rules")
	}
	if latest.JudgedCommitSHA != changedHeadCommitA {
		t.Errorf("latest verdict JudgedCommitSHA = %q, want the unchanged head %q", latest.JudgedCommitSHA, changedHeadCommitA)
	}
	if latest.RulesFingerprint == "" || latest.RulesFingerprint == first.RulesFingerprint {
		t.Errorf("latest verdict RulesFingerprint = %q, want a new fingerprint different from %q", latest.RulesFingerprint, first.RulesFingerprint)
	}
	if d := latestRejudgeDecision(t, got); d.VerdictReused {
		t.Error("edited-rules rejudge: VerdictReused = true, want false")
	}
}

// TestRejudgeChangedHeadJudgeAskedOnConfiguredBackend: the judge that the
// changed head forces is asked on the configured backend, and on a named
// backend when the operator reroutes the rejudge.
func TestRejudgeChangedHeadJudgeAskedOnConfiguredBackend(t *testing.T) {
	m, prd, g := newChangedHeadBlockedPRD(t)
	j, _ := judgeOnCommitA(t, m, prd.ID)

	wantBackend, _, _, err := m.resolveUnitGuardrailModel(&Task{}, prd)
	if err != nil || wantBackend == "" {
		t.Fatalf("resolveUnitGuardrailModel: backend %q err %v, want a configured backend", wantBackend, err)
	}
	if j.lastBackend != wantBackend {
		t.Fatalf("first review backend = %q, want the configured %q", j.lastBackend, wantBackend)
	}

	g.setHead(changedHeadCommitB)
	j.lastBackend = ""
	j.v = newRejudgeContentBlockJudge(t, "judge-changed-head-configured").v
	if _, err := m.RejudgeCompletionGuardrail(context.Background(), prd.ID, "operator", ""); err != nil {
		t.Fatalf("changed-head RejudgeCompletionGuardrail: %v", err)
	}
	if j.calls != 2 {
		t.Fatalf("judge calls = %d, want 2", j.calls)
	}
	if j.lastBackend != wantBackend {
		t.Errorf("judge asked on backend %q, want the configured %q", j.lastBackend, wantBackend)
	}

	// A reroute names a backend directly and wins over the configured one.
	g.setHead(strings.Repeat("c", 40))
	j.v = newRejudgeContentBlockJudge(t, "judge-changed-head-rerouted").v
	if _, err := m.RejudgeCompletionGuardrail(context.Background(), prd.ID, "operator", "claude-code-strong"); err != nil {
		t.Fatalf("rerouted RejudgeCompletionGuardrail: %v", err)
	}
	if j.calls != 3 {
		t.Fatalf("judge calls = %d, want 3", j.calls)
	}
	if j.lastBackend != "claude-code-strong" {
		t.Errorf("rerouted judge backend = %q, want claude-code-strong", j.lastBackend)
	}
}
