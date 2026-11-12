// ci-watch completion guardrail — runPRDCompletionCIWatch and its
// executor rollup wiring. See ci_watch.go's header for the design.
//
// Every test injects the Manager's ci-watch seams (ciGitFn, ciRunsFn,
// ciRerunFn, 1ms poll interval) and overrides the package-level
// ghAvailable, so nothing here shells out to the real git/gh binaries
// or touches the network.
//
// main.go's verdict-category handling (judgedGuardrailCategory:
// "policy" survives, anything else becomes content) is a pure function
// in package main and is already unit-tested in
// cmd/datawatch/bl410_failure_classification_test.go; it is not
// re-tested from here since this package cannot import package main.
package autonomous

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const ciTestSHA = "0123456789abcdef0123456789abcdef01234567"
const ciTestOrigin = "git@github.com:acme/widgets.git"

// ciFakes records every call made through the ci-watch seams.
type ciFakes struct {
	gitCalls   atomic.Int32
	runsCalls  atomic.Int32
	rerunCalls atomic.Int32

	mu       sync.Mutex
	rerunIDs []int64
}

// wireCIWatch installs fake seams on m. pushed controls ciGitFn's
// answer; runs is consulted on every ciRunsFn call; rerun (optional)
// runs inside ciRerunFn after it is recorded. ghAvailable is forced to
// true (restored on cleanup) so no test depends on the host's PATH.
func wireCIWatch(t *testing.T, m *Manager, pushed bool, runs func(ctx context.Context) []CIWatchRun, rerun func(id int64)) *ciFakes {
	t.Helper()
	f := &ciFakes{}
	m.mu.Lock()
	m.ciGitFn = func(_ context.Context, _ string) (string, string, bool, error) {
		f.gitCalls.Add(1)
		return ciTestSHA, ciTestOrigin, pushed, nil
	}
	m.ciRunsFn = func(ctx context.Context, ownerRepo, sha string) ([]CIWatchRun, error) {
		f.runsCalls.Add(1)
		if ownerRepo != "acme/widgets" || sha != ciTestSHA {
			t.Errorf("ciRunsFn(%q, %q), want acme/widgets @ %s", ownerRepo, sha, ciTestSHA)
		}
		return runs(ctx), nil
	}
	m.ciRerunFn = func(_ context.Context, _ string, id int64) error {
		f.rerunCalls.Add(1)
		f.mu.Lock()
		f.rerunIDs = append(f.rerunIDs, id)
		f.mu.Unlock()
		if rerun != nil {
			rerun(id)
		}
		return nil
	}
	m.ciPollInterval = time.Millisecond
	m.ciFollowUpDelay = time.Millisecond
	m.mu.Unlock()

	prev := ghAvailable
	ghAvailable = func() bool { return true }
	t.Cleanup(func() { ghAvailable = prev })
	return f
}

func ciRun(status, conclusion string) []CIWatchRun {
	return []CIWatchRun{{ID: 4242, Name: "ci", Status: status, Conclusion: conclusion, URL: "https://github.com/acme/widgets/actions/runs/4242"}}
}

// ciProjectDir is a project dir with the default rules file present, so
// the judge prompt has something to point at.
func ciProjectDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte("rules"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// ciApprovedPRD stores a one-story/one-task approved PRD in dir with
// the given PRD-level guardrails, ready for Run.
func ciApprovedPRD(t *testing.T, m *Manager, dir string, guardrails []string) *PRD {
	t.Helper()
	prd, err := m.CreatePRD("ship the thing", dir, "claude", "", EffortNormal)
	if err != nil {
		t.Fatal(err)
	}
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "T", Spec: "do"}}}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.PerPRDGuardrails = guardrails
	prd.Status = PRDApproved
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatal(err)
	}
	return prd
}

func ciDecisions(prd *PRD) []Decision {
	var out []Decision
	for _, d := range prd.Decisions {
		if d.Kind == "ci_watch" {
			out = append(out, d)
		}
	}
	return out
}

func TestCIWatch_DisabledByDefault(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), fakeDecompose)
	f := wireCIWatch(t, m, true, func(context.Context) []CIWatchRun { return ciRun("completed", "failure") }, nil)
	judged := false
	m.SetGuardrail(func(_ context.Context, req GuardrailInvocation) (GuardrailVerdict, error) {
		if req.Guardrail == "ci-watch" {
			judged = true
		}
		return GuardrailVerdict{Outcome: "pass"}, nil
	})

	prd := ciApprovedPRD(t, m, ciProjectDir(t), nil)
	if err := m.Run(context.Background(), prd.ID, fakeSpawn, fakeVerify); err != nil {
		t.Fatalf("Run: %v", err)
	}
	prd, _ = m.Store().GetPRD(prd.ID)
	if prd.Status != PRDCompleted {
		t.Fatalf("status = %s, want completed", prd.Status)
	}
	if n := f.gitCalls.Load() + f.runsCalls.Load() + f.rerunCalls.Load(); n != 0 || judged {
		t.Fatalf("ci-watch seams called %d time(s), judged=%v; want nothing without an opt-in", n, judged)
	}
	if prd.CIWatch != nil {
		t.Fatalf("CIWatch = %+v, want nil when ci-watch never ran", prd.CIWatch)
	}
	if d := ciDecisions(prd); len(d) != 0 {
		t.Fatalf("want no ci_watch decisions, got %+v", d)
	}
}

func TestCIWatch_NotPushedSkips(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), fakeDecompose)
	f := wireCIWatch(t, m, false, func(context.Context) []CIWatchRun { return ciRun("completed", "failure") }, nil)

	prd := &PRD{ID: "p1", ProjectDir: ciProjectDir(t), PerPRDGuardrails: []string{"ci-watch"}}
	blocked, fix, err := m.runPRDCompletionCIWatch(context.Background(), prd)
	if err != nil || blocked || fix != nil {
		t.Fatalf("blocked=%v fix=%v err=%v, want false/nil/nil for an unpushed HEAD", blocked, fix, err)
	}
	if f.runsCalls.Load() != 0 {
		t.Fatalf("ciRunsFn called %d time(s), want 0 when HEAD isn't pushed", f.runsCalls.Load())
	}
	if prd.CIWatch == nil || prd.CIWatch.Status != "skipped" || prd.CIWatch.SHA != ciTestSHA {
		t.Fatalf("CIWatch = %+v, want skipped with the HEAD sha", prd.CIWatch)
	}
	if d := ciDecisions(prd); len(d) != 1 {
		t.Fatalf("want exactly one ci_watch decision, got %+v", prd.Decisions)
	}
}

func TestCIWatch_NoGHSkips(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), fakeDecompose)
	f := wireCIWatch(t, m, true, func(context.Context) []CIWatchRun { return ciRun("completed", "failure") }, nil)
	ghAvailable = func() bool { return false } // restored by wireCIWatch's cleanup

	prd := &PRD{ID: "p1", ProjectDir: ciProjectDir(t), PerPRDGuardrails: []string{"ci-watch"}}
	blocked, fix, err := m.runPRDCompletionCIWatch(context.Background(), prd)
	if err != nil || blocked || fix != nil {
		t.Fatalf("blocked=%v fix=%v err=%v, want a silent skip when gh is missing", blocked, fix, err)
	}
	if f.runsCalls.Load() != 0 {
		t.Fatalf("ciRunsFn called %d time(s), want 0 without gh", f.runsCalls.Load())
	}
	if prd.CIWatch == nil || prd.CIWatch.Status != "skipped" || prd.CIWatch.Repo != "acme/widgets" {
		t.Fatalf("CIWatch = %+v, want skipped with Repo=acme/widgets", prd.CIWatch)
	}
	if !containsStr(prd.CIWatch.Summary, "gh") {
		t.Errorf("skip summary should name the missing gh CLI: %q", prd.CIWatch.Summary)
	}
	if d := ciDecisions(prd); len(d) != 1 || d[0].VerdictOutcome != "n/a" {
		t.Fatalf("want one n/a ci_watch decision, got %+v", d)
	}
}

func TestCIWatch_AllGreenPasses(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), fakeDecompose)
	f := wireCIWatch(t, m, true, func(context.Context) []CIWatchRun { return ciRun("completed", "success") }, nil)
	var judged atomic.Int32
	m.SetGuardrail(func(_ context.Context, req GuardrailInvocation) (GuardrailVerdict, error) {
		if req.Guardrail == "ci-watch" {
			judged.Add(1)
		}
		return GuardrailVerdict{Outcome: "pass"}, nil
	})

	prd := ciApprovedPRD(t, m, ciProjectDir(t), []string{"ci-watch"})
	if err := m.Run(context.Background(), prd.ID, fakeSpawn, fakeVerify); err != nil {
		t.Fatalf("Run: %v", err)
	}
	prd, _ = m.Store().GetPRD(prd.ID)
	if prd.Status != PRDCompleted {
		t.Fatalf("status = %s, want completed", prd.Status)
	}
	if prd.CIWatch == nil || prd.CIWatch.Status != "pass" || len(prd.CIWatch.Runs) != 1 {
		t.Fatalf("CIWatch = %+v, want pass with the one run recorded", prd.CIWatch)
	}
	if judged.Load() != 0 {
		t.Fatalf("judge called %d time(s), want 0 when every run passed", judged.Load())
	}
	if f.rerunCalls.Load() != 0 {
		t.Fatalf("ciRerunFn called %d time(s), want 0", f.rerunCalls.Load())
	}
	if d := ciDecisions(prd); len(d) != 1 || d[0].VerdictOutcome != "pass" {
		t.Fatalf("want one pass ci_watch decision, got %+v", d)
	}
}

func TestCIWatch_FailingPolicyBlocks(t *testing.T) {
	cfg := DefaultConfig()                       // ContentAutoFixEnabled defaults to true
	cfg.VerificationModel = "ollama/qwen3.8:27b" // v9.10.18 judge model for the fake fn
	m, _ := NewManager(t.TempDir(), cfg, fakeDecompose)
	wireCIWatch(t, m, true, func(context.Context) []CIWatchRun { return ciRun("completed", "failure") }, nil)
	var mu sync.Mutex
	var captured []GuardrailInvocation
	m.SetGuardrail(func(_ context.Context, req GuardrailInvocation) (GuardrailVerdict, error) {
		if req.Guardrail != "ci-watch" {
			return GuardrailVerdict{Outcome: "pass"}, nil
		}
		mu.Lock()
		captured = append(captured, req)
		mu.Unlock()
		return GuardrailVerdict{Outcome: "block", Severity: "high", Summary: "release gate needs operator sign-off", Category: "policy"}, nil
	})
	dir := ciProjectDir(t)

	// Direct call: return values and the judge's input.
	prd := &PRD{ID: "p1", Title: "Release", Spec: "cut the release", ProjectDir: dir, PerPRDGuardrails: []string{"ci-watch"}}
	if !m.resolveContentAutoFixEnabled(prd) || !m.retryBudgetRemaining(prd, "content") {
		t.Fatal("precondition: content auto-fix must be enabled with budget remaining")
	}
	blocked, fix, err := m.runPRDCompletionCIWatch(context.Background(), prd)
	if err != nil || !blocked || fix != nil {
		t.Fatalf("blocked=%v fix=%v err=%v, want true/nil/nil for a policy block", blocked, fix, err)
	}
	if len(captured) != 1 {
		t.Fatalf("judge called %d time(s), want 1", len(captured))
	}
	inv := captured[0]
	if inv.Guardrail != "ci-watch" || inv.Level != "prd" || inv.UnitID != "p1" {
		t.Fatalf("invocation = %+v, want Guardrail=ci-watch Level=prd UnitID=p1", inv)
	}
	for _, want := range []string{"https://github.com/acme/widgets/actions/runs/4242", "AGENT.md", "cut the release", ciTestSHA} {
		if !containsStr(inv.UnitSpec, want) {
			t.Errorf("UnitSpec missing %q: %q", want, inv.UnitSpec)
		}
	}
	if prd.CIWatch == nil || prd.CIWatch.Status != "failed_policy" || prd.CIWatch.Category != "policy" {
		t.Fatalf("CIWatch = %+v, want failed_policy/policy", prd.CIWatch)
	}
	if d := ciDecisions(prd); len(d) != 1 || d[0].FailureCategory != "policy" || d[0].VerdictOutcome != "block" {
		t.Fatalf("want one block/policy ci_watch decision, got %+v", d)
	}
	if prd.RetryCountsByCategory["content"] != 0 {
		t.Fatalf("content budget consumed (%d) by a policy block", prd.RetryCountsByCategory["content"])
	}

	// Executor rollup: PRDBlocked, and no corrective work synthesized.
	stored := ciApprovedPRD(t, m, dir, []string{"ci-watch"})
	if err := m.Run(context.Background(), stored.ID, fakeSpawn, fakeVerify); err != nil {
		t.Fatalf("Run: %v", err)
	}
	stored, _ = m.Store().GetPRD(stored.ID)
	if stored.Status != PRDBlocked {
		t.Fatalf("status = %s, want blocked", stored.Status)
	}
	if len(stored.Story) != 1 || len(stored.Story[0].Tasks) != 1 {
		t.Fatalf("want the original 1 story/1 task (no synthesized corrective work), got %d stories", len(stored.Story))
	}
	if stored.CIWatch == nil || stored.CIWatch.Status != "failed_policy" {
		t.Fatalf("stored CIWatch = %+v, want failed_policy", stored.CIWatch)
	}
}

func TestCIWatch_FailingContentSynthesizesOnce(t *testing.T) {
	cfg := DefaultConfig()
	cfg.VerificationModel = "ollama/qwen3.8:27b" // v9.10.18 judge model for the fake fn
	m, _ := NewManager(t.TempDir(), cfg, fakeDecompose)
	var judged atomic.Int32
	// CI is red for the original commit; once the judge has asked for a
	// fix (and the corrective task has run) it reads green.
	f := wireCIWatch(t, m, true, func(context.Context) []CIWatchRun {
		if judged.Load() == 0 {
			return ciRun("completed", "failure")
		}
		return ciRun("completed", "success")
	}, nil)
	m.SetGuardrail(func(_ context.Context, req GuardrailInvocation) (GuardrailVerdict, error) {
		if req.Guardrail != "ci-watch" {
			return GuardrailVerdict{Outcome: "pass"}, nil
		}
		judged.Add(1)
		return GuardrailVerdict{Outcome: "block", Summary: "unit test broken by this change", Issues: []string{"fix TestFoo"}}, nil
	})

	prd := ciApprovedPRD(t, m, ciProjectDir(t), []string{"ci-watch"})
	beforeID := prd.ID
	if err := m.Run(context.Background(), prd.ID, fakeSpawn, fakeVerify); err != nil {
		t.Fatalf("Run: %v", err)
	}
	prd, _ = m.Store().GetPRD(beforeID)
	if prd.ID != beforeID {
		t.Fatalf("PRD ID changed (%s -> %s) — a new PRD must never be created for a fix", beforeID, prd.ID)
	}
	if judged.Load() != 1 {
		t.Fatalf("judge called %d time(s), want exactly 1", judged.Load())
	}
	if len(prd.Story) != 2 {
		t.Fatalf("want 2 stories (original + exactly one synthesized corrective story), got %d", len(prd.Story))
	}
	if len(prd.Story[1].Tasks) != 1 || prd.Story[1].Tasks[0].Status != TaskCompleted {
		t.Fatalf("synthesized story = %+v, want one completed task", prd.Story[1])
	}
	if prd.RetryCountsByCategory["content"] != 1 {
		t.Fatalf("want 1 consumed content retry, got %d", prd.RetryCountsByCategory["content"])
	}
	if prd.Status != PRDCompleted {
		t.Fatalf("status = %s, want completed once the fix's CI is green", prd.Status)
	}
	if prd.CIWatch == nil || prd.CIWatch.Status != "pass" {
		t.Fatalf("CIWatch = %+v, want pass after the fix", prd.CIWatch)
	}
	d := ciDecisions(prd)
	if len(d) != 2 || d[0].VerdictOutcome != "block" || d[1].VerdictOutcome != "pass" {
		t.Fatalf("want [block, pass] ci_watch decisions, got %+v", d)
	}
	if f.rerunCalls.Load() != 0 {
		t.Fatalf("ciRerunFn called %d time(s), want 0 for a content failure", f.rerunCalls.Load())
	}
}

func TestCIWatch_TimeoutPending(t *testing.T) {
	cfg := DefaultConfig()
	cfg.CIWatchTimeoutSeconds = 1 // smallest the config allows
	dataDir := t.TempDir()
	m, _ := NewManager(dataDir, cfg, fakeDecompose)

	// The main poll runs under the test's deadline-free context; the
	// background follow-up runs under its own bounded context. Hold the
	// follow-up's listing until Run has returned and the pending state
	// was asserted, so its GetPRD/SavePRD can't interleave with Run's.
	release := make(chan struct{})
	var followUps atomic.Int32
	f := wireCIWatch(t, m, true, func(ctx context.Context) []CIWatchRun {
		if _, ok := ctx.Deadline(); !ok {
			return ciRun("in_progress", "")
		}
		followUps.Add(1)
		select {
		case <-release:
		case <-ctx.Done():
		}
		return ciRun("completed", "success")
	}, nil)

	prd := ciApprovedPRD(t, m, ciProjectDir(t), []string{"ci-watch"})
	if err := m.Run(context.Background(), prd.ID, fakeSpawn, fakeVerify); err != nil {
		t.Fatalf("Run: %v", err)
	}
	prd, _ = m.Store().GetPRD(prd.ID)
	if prd.Status != PRDCompleted {
		t.Fatalf("status = %s, want completed (slow CI must not hold the automaton)", prd.Status)
	}
	if prd.CIWatch == nil || prd.CIWatch.Status != "pending" {
		t.Fatalf("CIWatch = %+v, want pending", prd.CIWatch)
	}
	if d := ciDecisions(prd); len(d) != 1 || d[0].FailureCategory != "infra" {
		t.Fatalf("want one infra ci_watch decision for the pending watch, got %+v", d)
	}
	if f.runsCalls.Load() < 2 {
		t.Fatalf("ciRunsFn called %d time(s), want repeated polling until the timeout", f.runsCalls.Load())
	}
	close(release)

	// The store hands out shared *PRD pointers and the follow-up mutates
	// one in place before SavePRD, so polling m.Store() would race with
	// it. Poll what SavePRD persisted instead (a fresh Store loaded from
	// disk). This is safe: the follow-up persists with an atomic
	// temp-file rename, and Store.loadRecords only removes temp files
	// older than staleTempFileAge, so opening a second Store on the
	// directory cannot destroy the in-flight write. A read that fails or
	// finds the PRD missing therefore really does mean "not yet", and is
	// retried below. (readBack runs on the test goroutine, so t.Logf is
	// safe.)
	var lastStoreErr error
	readBack := func() *PRD {
		st, err := NewStore(dataDir)
		if err != nil {
			if err.Error() != fmt.Sprint(lastStoreErr) {
				lastStoreErr = err
				t.Logf("read-back NewStore(%s) failed: %v (a persistent failure here would look like a silent timeout; it is not an assertion)", dataDir, err)
			}
			return nil
		}
		lastStoreErr = nil
		p, _ := st.GetPRD(prd.ID)
		return p
	}
	pollUntil(t, 30*time.Second, "follow-up recording a pass", func() bool {
		p := readBack()
		if p != nil && p.CIWatch != nil && p.CIWatch.Status == "pass" {
			prd = p
			return true
		}
		return false
	})
	if prd.Status != PRDCompleted {
		t.Fatalf("follow-up changed status to %s; it must only record", prd.Status)
	}
	if d := ciDecisions(prd); len(d) != 2 || d[1].VerdictOutcome != "pass" {
		t.Fatalf("want [pending, pass] ci_watch decisions, got %+v", d)
	}
	// Exactly one follow-up, and nothing further scheduled after it.
	time.Sleep(50 * time.Millisecond)
	if n := followUps.Load(); n != 1 {
		t.Fatalf("follow-up listings = %d, want exactly 1", n)
	}
	if p := readBack(); p == nil || len(ciDecisions(p)) != 2 {
		t.Fatalf("want still exactly 2 ci_watch decisions after the single follow-up, got %+v", p)
	}
}

func TestCIWatch_InfraRerun(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), fakeDecompose)
	var rerun atomic.Bool
	f := wireCIWatch(t, m, true, func(context.Context) []CIWatchRun {
		if rerun.Load() {
			return ciRun("completed", "success")
		}
		return ciRun("completed", "cancelled")
	}, func(int64) { rerun.Store(true) })
	var judged atomic.Int32
	m.SetGuardrail(func(_ context.Context, req GuardrailInvocation) (GuardrailVerdict, error) {
		if req.Guardrail == "ci-watch" {
			judged.Add(1)
		}
		return GuardrailVerdict{Outcome: "pass"}, nil
	})

	prd := &PRD{ID: "p1", ProjectDir: ciProjectDir(t), PerPRDGuardrails: []string{"ci-watch"}}
	if !m.retryBudgetRemaining(prd, "infra") {
		t.Fatal("precondition: infra budget must be available")
	}
	blocked, fix, err := m.runPRDCompletionCIWatch(context.Background(), prd)
	if err != nil || blocked || fix != nil {
		t.Fatalf("blocked=%v fix=%v err=%v, want a pass after the re-run", blocked, fix, err)
	}
	if f.rerunCalls.Load() != 1 {
		t.Fatalf("ciRerunFn called %d time(s), want 1", f.rerunCalls.Load())
	}
	f.mu.Lock()
	ids := append([]int64(nil), f.rerunIDs...)
	f.mu.Unlock()
	if len(ids) != 1 || ids[0] != 4242 {
		t.Fatalf("re-ran runs %v, want [4242]", ids)
	}
	if prd.RetryCountsByCategory["infra"] != 1 {
		t.Fatalf("want 1 consumed infra retry, got %d", prd.RetryCountsByCategory["infra"])
	}
	if judged.Load() != 0 {
		t.Fatalf("judge called %d time(s), want 0 — CI-machinery failures are re-run, not judged", judged.Load())
	}
	if prd.CIWatch == nil || prd.CIWatch.Status != "pass" {
		t.Fatalf("CIWatch = %+v, want pass", prd.CIWatch)
	}
}
