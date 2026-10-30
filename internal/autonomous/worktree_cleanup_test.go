// Tests for the finished-Automaton worktree cleanup: the core
// CleanupWorktrees pass (mode handling, safety criteria, removal stages,
// per-pass limit, "never force") and the manager-level entry points
// against real git repositories whose default branch is "trunk", proving
// no branch name is assumed.
package autonomous

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

const (
	testRepo     = "/repo"
	testBranch   = "automaton/x"
	testWorktree = "/repo/wt"
)

// ---------------------------------------------------------------- fake audit / record

type recordedEvent struct {
	action  string
	details map[string]any
}

type recordedAudit struct {
	mu      sync.Mutex
	items   []recordedEvent
	failure string
}

func (r *recordedAudit) fn() WorktreeCleanupAudit {
	if r == nil {
		return func(string, map[string]any) error { return nil }
	}
	return func(action string, details map[string]any) error {
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.failure != "" {
			return errors.New(r.failure)
		}
		r.items = append(r.items, recordedEvent{action: action, details: details})
		return nil
	}
}

// events returns the recorded events for action.
func (r *recordedAudit) events(action string) []map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []map[string]any
	for _, e := range r.items {
		if e.action == action {
			out = append(out, e.details)
		}
	}
	return out
}

func (r *recordedAudit) anyFailed() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.failure != ""
}

type recordNote struct{ branch, text string }

type recordedRecords struct {
	mu    sync.Mutex
	notes []recordNote
}

func (rr *recordedRecords) fn() func(branch, text string) error {
	if rr == nil {
		return func(string, string) error { return nil }
	}
	n := rr
	return func(branch, text string) error {
		n.mu.Lock()
		defer n.mu.Unlock()
		n.notes = append(n.notes, recordNote{branch: branch, text: text})
		return nil
	}
}

func (rr *recordedRecords) failing() func(branch, text string) error {
	return func(branch, text string) error { return errors.New("record sink unavailable") }
}

// ------------------------------------------------------------- fake Probe

type fakeProbe struct {
	finished  bool
	base      string
	inUse     bool
	inUseKind string
	age       time.Duration
	ageOK     bool
	size      int64
	sizeErr   string
}

var _ WorktreeCleanupProbe = (*fakeProbe)(nil)

func (p *fakeProbe) AutomatonFinished(string) bool { return p.finished }
func (p *fakeProbe) BaseBranch(string) string      { return p.base }
func (p *fakeProbe) WorktreeInUse(string, string) (bool, string) {
	return p.inUse, p.inUseKind
}
func (p *fakeProbe) EntryAge(string) (time.Duration, bool) { return p.age, p.ageOK }
func (p *fakeProbe) WorktreeSize(string) (int64, error) {
	if p.sizeErr != "" {
		return 0, errors.New(p.sizeErr)
	}
	return p.size, nil
}

// eligibleProbe passes the finished/base/age gates with a 4096-byte work tree.
func eligibleProbe() *fakeProbe {
	return &fakeProbe{finished: true, base: "main", age: 48 * time.Hour, ageOK: true, size: 4096}
}

// ----------------------------------------------------------- scripted git runner

type scriptedGit struct {
	mu       sync.Mutex
	out      map[string]string
	fail     map[string]string
	calledMu sync.Mutex
	called   []string
}

func newScriptedGit() *scriptedGit {
	return &scriptedGit{out: map[string]string{}, fail: map[string]string{}}
}

func keyOf(dir string, args ...string) string { return dir + "\x00" + strings.Join(args, "\x00") }

func (g *scriptedGit) Run(_ context.Context, dir string, args ...string) (string, error) {
	g.calledMu.Lock()
	g.called = append(g.called, append([]string{dir}, args...)...)
	g.calledMu.Unlock()
	g.mu.Lock()
	defer g.mu.Unlock()
	k := keyOf(dir, args...)
	if msg, ok := g.fail[k]; ok {
		return "", errors.New(msg)
	}
	if out, ok := g.out[k]; ok {
		return out, nil
	}
	return "", fmt.Errorf("scriptedGit: unscripted git %s in %s", strings.Join(args, " "), dir)
}

func (g *scriptedGit) hasCall(args ...string) bool {
	g.calledMu.Lock()
	defer g.calledMu.Unlock()
	for i := 0; i+len(args) <= len(g.called); i++ {
		match := true
		for j, a := range args {
			if g.called[i+j] != a {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// scriptStandardRemoval scripts the calls of a removal of testBranch in a
// clean worktree whose commits are all on the base branch (ahead = 0), on
// a repository whose primary checkout is elsewhere.
func scriptStandardRemoval(t *testing.T, g *scriptedGit) {
	t.Helper()
	wtList := ""
	for _, wt := range []string{"/repo/primary", testWorktree} {
		wtList += "worktree " + wt + "\n\n"
	}
	wtList += "branch refs/heads/" + testBranch + "\n"
	g.out[keyOf(testRepo, "worktree", "list", "--porcelain")] = wtList
	g.out[keyOf(testWorktree, "status", "--porcelain")] = ""
	g.out[keyOf(testRepo, "rev-list", "--count", "main.."+testBranch)] = "0"
	g.out[keyOf(testRepo, "cherry", "main", testBranch)] = ""
	g.out[keyOf(testRepo, "log", "main.."+testBranch, "--format=%s")] = ""
	g.out[keyOf(testRepo, "log", "main", "--format=%s")] = ""
}

func coreWorktrees(mode string) SafeDeleteReport {
	return SafeDeleteReport{
		Safe: []SafeDeleteEntry{{Branch: testBranch, Worktree: testWorktree, Reason: "all commits on main"}},
		Held: []SafeDeleteHeld{},
	}
}

func runCoreWith(g MergeGitRunner, probe WorktreeCleanupProbe, report SafeDeleteReport, mode string, audit WorktreeCleanupAudit, record func(string, string) error) WorktreeCleanupResult {
	return CleanupWorktrees(WorktreeCleanupOptions{
		Ctx:         context.Background(),
		Repo:        testRepo,
		Report:      report,
		Mode:        mode,
		Retention:   24 * time.Hour,
		MaxRemovals: 10,
		Git:         g,
		Probe:       probe,
		Audit:       audit,
		Record:      record,
	})
}

func runCore(g MergeGitRunner, probe WorktreeCleanupProbe, report SafeDeleteReport, audit WorktreeCleanupAudit, record func(string, string) error) WorktreeCleanupResult {
	return runCoreWith(g, probe, report, config.CleanupModeOn, audit, record)
}

func skippedReasons(res WorktreeCleanupResult) []string {
	out := make([]string, 0, len(res.Skipped))
	for _, s := range res.Skipped {
		out = append(out, s.Reason)
	}
	return out
}

func skippedHas(res WorktreeCleanupResult, reason string) bool {
	for _, s := range res.Skipped {
		if s.Reason == reason {
			return true
		}
	}
	return false
}

func failedStage(res WorktreeCleanupResult) string {
	if len(res.Failed) == 0 {
		return ""
	}
	return res.Failed[0].Stage
}

// ------------------------------------------------------------- mode behavior

func TestWorktreeCleanupOffModeDoesNothing(t *testing.T) {
	g := newScriptedGit()
	res := runCoreWith(g, eligibleProbe(), coreWorktrees(config.CleanupModeOff), config.CleanupModeOff,
		func(string, map[string]any) error { return nil }, nil)
	if len(res.Removed) != 0 || len(res.WouldRemove) != 0 || len(res.Skipped) != 0 || len(res.Failed) != 0 {
		t.Fatalf("off mode must not decide anything, got %+v", res)
	}
	if len(g.called) != 0 {
		t.Fatalf("off mode must not call git, saw %v", g.called)
	}
}

func TestWorktreeCleanupReportModeRecordsAndRemovesNothing(t *testing.T) {
	g := newScriptedGit()
	scriptStandardRemoval(t, g)
	audit := &recordedAudit{}
	res := runCoreWith(g, eligibleProbe(), coreWorktrees(config.CleanupModeReport), config.CleanupModeReport,
		audit.fn(), nil)
	if len(res.Removed) != 0 {
		t.Fatalf("report mode must not remove: %+v", res.Removed)
	}
	if len(res.WouldRemove) != 1 || res.WouldRemove[0].Branch != testBranch {
		t.Fatalf("want 1 would-remove for %s, got %+v", testBranch, res.WouldRemove)
	}
	if res.WouldRemove[0].BytesFreed != 4096 {
		t.Fatalf("want bytes_freed 4096, got %d", res.WouldRemove[0].BytesFreed)
	}
	if g.hasCall("worktree", "remove") || g.hasCall("branch", "-d") {
		t.Fatalf("report mode must not call removal commands: %v", g.called)
	}
	if len(audit.events(worktreeCleanupAuditAction)) != 0 {
		t.Fatalf("report mode must not write per-removal audit events: %+v", audit.items)
	}
}

func TestWorktreeCleanupOnModeRemovesAndRecords(t *testing.T) {
	g := newScriptedGit()
	scriptStandardRemoval(t, g)
	g.out[keyOf(testRepo, "worktree", "remove", testWorktree)] = ""
	g.out[keyOf(testRepo, "branch", "-d", testBranch)] = ""
	var auditOK bool
	audit := func(action string, _ map[string]any) error {
		auditOK = action == worktreeCleanupAuditAction
		return nil
	}
	rec := &recordedRecords{}
	res := runCore(g, eligibleProbe(), coreWorktrees(config.CleanupModeOn), audit, rec.fn())
	if len(res.Removed) != 1 || res.Removed[0].Branch != testBranch || res.Removed[0].Worktree != testWorktree {
		t.Fatalf("want one removal of %s @ %s, got %+v", testBranch, testWorktree, res.Removed)
	}
	if res.Removed[0].BytesFreed != 4096 {
		t.Fatalf("want bytes_freed 4096, got %d", res.Removed[0].BytesFreed)
	}
	if !auditOK {
		t.Fatalf("per-removal audit event %q was not written", worktreeCleanupAuditAction)
	}
	if len(rec.notes) != 1 || rec.notes[0].branch != testBranch {
		t.Fatalf("want one record note for %s, got %+v", testBranch, rec.notes)
	}
	if !strings.Contains(rec.notes[0].text, testBranch) {
		t.Fatalf("record note should name the branch: %q", rec.notes[0].text)
	}
}

func TestWorktreeCleanupOnModeLimitsRemovalsToTenPerPass(t *testing.T) {
	g := newScriptedGit()
	wtList := "worktree /repo/primary\n\n"
	wtList += "worktree /repo/wt-a\n\nbranch refs/heads/automaton/a\n"
	wtList += "worktree /repo/wt-b\n\nbranch refs/heads/automaton/b\n"
	g.out[keyOf(testRepo, "worktree", "list", "--porcelain")] = wtList
	for _, b := range []struct {
		branch, wt string
	}{
		{"automaton/a", "/repo/wt-a"},
		{"automaton/b", "/repo/wt-b"},
	} {
		g.out[keyOf(b.wt, "status", "--porcelain")] = ""
		g.out[keyOf(testRepo, "rev-list", "--count", "main.."+b.branch)] = "0"
		g.out[keyOf(testRepo, "cherry", "main", b.branch)] = ""
		g.out[keyOf(testRepo, "log", "main.."+b.branch, "--format=%s")] = ""
		g.out[keyOf(testRepo, "log", "main", "--format=%s")] = ""
		g.out[keyOf(testRepo, "worktree", "remove", b.wt)] = ""
		g.out[keyOf(testRepo, "branch", "-d", b.branch)] = ""
	}
	report := SafeDeleteReport{
		Safe: []SafeDeleteEntry{
			{Branch: "automaton/a", Worktree: "/repo/wt-a", Reason: "all commits on main"},
			{Branch: "automaton/b", Worktree: "/repo/wt-b", Reason: "all commits on main"},
		},
		Held: []SafeDeleteHeld{},
	}
	res := CleanupWorktrees(WorktreeCleanupOptions{
		Ctx: context.Background(), Repo: testRepo, Report: report,
		Mode: config.CleanupModeOn, Retention: 24 * time.Hour, MaxRemovals: 1,
		Git: g, Probe: eligibleProbe(),
	})
	if len(res.Removed) != 1 {
		t.Fatalf("per-pass limit of 1: want exactly 1 removed, got %+v", res.Removed)
	}
	if len(res.Failed) != 0 {
		t.Fatalf("second entry is a skip, not a failure: %+v", res.Failed)
	}
	if !skippedHas(res, cleanupSkipOverLimit) {
		t.Fatalf("second entry should be skipped as over_limit, got %+v", res.Skipped)
	}
}

// ------------------------------------------------------------- skip reasons

func TestWorktreeCleanupSkipsNotFinishedAutomaton(t *testing.T) {
	g := newScriptedGit()
	probe := eligibleProbe()
	probe.finished = false
	res := runCore(g, probe, coreWorktrees(config.CleanupModeOn), nil, nil)
	if len(res.Removed) != 0 || !skippedHas(res, cleanupSkipNotFinished) {
		t.Fatalf("not-finished entry must be skipped with %q, got %+v", cleanupSkipNotFinished, res)
	}
}

func TestWorktreeCleanupSkipsLiveSessions(t *testing.T) {
	g := newScriptedGit()
	probe := eligibleProbe()
	probe.inUse = true
	probe.inUseKind = "session"
	res := runCore(g, probe, coreWorktrees(config.CleanupModeOn), nil, nil)
	if len(res.Removed) != 0 || !skippedHas(res, cleanupSkipLiveSession) {
		t.Fatalf("live session entry must be skipped with %q, got %+v", cleanupSkipLiveSession, res)
	}
}

func TestWorktreeCleanupSkipsLiveLeases(t *testing.T) {
	g := newScriptedGit()
	probe := eligibleProbe()
	probe.inUse = true
	probe.inUseKind = "lease"
	res := runCore(g, probe, coreWorktrees(config.CleanupModeOn), nil, nil)
	if len(res.Removed) != 0 || !skippedHas(res, cleanupSkipLiveLease) {
		t.Fatalf("live lease entry must be skipped with %q, got %+v", cleanupSkipLiveLease, res)
	}
}

func TestWorktreeCleanupSkipsRetainedWork(t *testing.T) {
	g := newScriptedGit()
	probe := eligibleProbe()
	probe.age = time.Hour // younger than the 24h retention window
	res := runCore(g, probe, coreWorktrees(config.CleanupModeOn), nil, nil)
	if len(res.Removed) != 0 || !skippedHas(res, cleanupSkipRetained) {
		t.Fatalf("young entry must be skipped with %q, got %+v", cleanupSkipRetained, res)
	}
}

func TestWorktreeCleanupSkipsRetainedWhenAgeUnknown(t *testing.T) {
	g := newScriptedGit()
	probe := eligibleProbe()
	probe.ageOK = false
	res := runCore(g, probe, coreWorktrees(config.CleanupModeOn), nil, nil)
	if len(res.Removed) != 0 || !skippedHas(res, cleanupSkipRetained) {
		t.Fatalf("unknown age must be treated as retained, got %+v", res)
	}
}

func TestWorktreeCleanupSkipsMainCheckout(t *testing.T) {
	g := newScriptedGit()
	g.out[keyOf(testRepo, "worktree", "list", "--porcelain")] = "worktree " + testWorktree + "\n\n"
	g.out[keyOf(testRepo, "status", "--porcelain")] = ""
	res := runCore(g, eligibleProbe(), coreWorktrees(config.CleanupModeOn), nil, nil)
	if len(res.Removed) != 0 || !skippedHas(res, cleanupSkipPrimary) {
		t.Fatalf("primary checkout must never be touched, got %+v", res)
	}
}

func TestWorktreeCleanupSkipsWhenMainCheckoutUndetermined(t *testing.T) {
	g := newScriptedGit()
	g.fail[keyOf(testRepo, "worktree", "list", "--porcelain")] = "no worktree list"
	res := runCore(g, eligibleProbe(), coreWorktrees(config.CleanupModeOn), nil, nil)
	if len(res.Removed) != 0 || !skippedHas(res, cleanupSkipPrimary) {
		t.Fatalf("unknown primary must conservatively hold a worktree entry, got %+v", res)
	}
}

func TestWorktreeCleanupSkipsDirtyWorktree(t *testing.T) {
	g := newScriptedGit()
	g.out[keyOf(testRepo, "worktree", "list", "--porcelain")] = "worktree /repo/primary\n\n"
	g.out[keyOf(testWorktree, "status", "--porcelain")] = " M modified.go\n?? stray.txt\n"
	res := runCore(g, eligibleProbe(), coreWorktrees(config.CleanupModeOn), nil, nil)
	if len(res.Removed) != 0 || !skippedHas(res, cleanupSkipDirty) {
		t.Fatalf("dirty worktree must be skipped with %q, got %+v", cleanupSkipDirty, res)
	}
}

func TestWorktreeCleanupSkipsWhenStatusCannotBeRead(t *testing.T) {
	g := newScriptedGit()
	g.out[keyOf(testRepo, "worktree", "list", "--porcelain")] = "worktree /repo/primary\n\n"
	g.fail[keyOf(testWorktree, "status", "--porcelain")] = "disappeared"
	res := runCore(g, eligibleProbe(), coreWorktrees(config.CleanupModeOn), nil, nil)
	if len(res.Removed) != 0 {
		t.Fatalf("unreadable status must not remove: %+v", res.Removed)
	}
	if len(res.Failed) != 0 && failedStage(res) != cleanupStageStatus {
		t.Fatalf("want failure at %q, got %+v", cleanupStageStatus, res.Failed)
	}
}

func TestWorktreeCleanupRemovesWorktreelessBranch(t *testing.T) {
	g := newScriptedGit()
	g.out[keyOf(testRepo, "worktree", "list", "--porcelain")] = "worktree /repo/primary\n\n"
	g.out[keyOf(testRepo, "rev-list", "--count", "main.."+testBranch)] = "0"
	g.out[keyOf(testRepo, "cherry", "main", testBranch)] = ""
	g.out[keyOf(testRepo, "log", "main.."+testBranch, "--format=%s")] = ""
	g.out[keyOf(testRepo, "log", "main", "--format=%s")] = ""
	g.out[keyOf(testRepo, "branch", "-d", testBranch)] = ""
	report := SafeDeleteReport{Safe: []SafeDeleteEntry{{Branch: testBranch, Worktree: "", Reason: "no commits ahead of main"}}, Held: []SafeDeleteHeld{}}
	res := runCore(g, eligibleProbe(), report, nil, nil)
	if len(res.Removed) != 1 {
		t.Fatalf("worktree-less branch should be removed, got %+v", res)
	}
	if g.hasCall("worktree", "remove") {
		t.Fatalf("no worktree: no worktree remove call expected, saw %v", g.called)
	}
	if !g.hasCall("branch", "-d", testBranch) {
		t.Fatalf("branch -d not called: %v", g.called)
	}
}

func TestWorktreeCleanupSkipsCommitsNotOnBase(t *testing.T) {
	g := newScriptedGit()
	g.out[keyOf(testRepo, "worktree", "list", "--porcelain")] = "worktree /repo/primary\n\n"
	g.out[keyOf(testWorktree, "status", "--porcelain")] = ""
	g.out[keyOf(testRepo, "rev-list", "--count", "main.."+testBranch)] = "3"
	g.out[keyOf(testRepo, "cherry", "main", testBranch)] = "+ unmerged one\n+ unmerged two\n"
	g.out[keyOf(testRepo, "log", "main.."+testBranch, "--format=%s")] = "unmerged one\nunmerged two\n"
	g.out[keyOf(testRepo, "log", "main", "--format=%s")] = "base work\n"
	res := runCore(g, eligibleProbe(), coreWorktrees(config.CleanupModeOn), nil, nil)
	if len(res.Removed) != 0 || !skippedHas(res, cleanupSkipCommits) {
		t.Fatalf("commits not on base must be skipped with %q, got %+v", cleanupSkipCommits, res)
	}
}

func TestWorktreeCleanupSkipsWhenBaseUnresolvable(t *testing.T) {
	g := newScriptedGit()
	g.out[keyOf(testRepo, "worktree", "list", "--porcelain")] = "worktree /repo/primary\n\n"
	g.out[keyOf(testWorktree, "status", "--porcelain")] = ""
	probe := eligibleProbe()
	probe.base = ""
	res := runCore(g, probe, coreWorktrees(config.CleanupModeOn), nil, nil)
	if len(res.Removed) != 0 || !skippedHas(res, cleanupSkipNoBase) {
		t.Fatalf("unresolvable base must be skipped with %q, got %+v", cleanupSkipNoBase, res)
	}
}

func TestWorktreeCleanupLeavesHeldEntriesAlone(t *testing.T) {
	g := newScriptedGit()
	report := SafeDeleteReport{
		Safe: []SafeDeleteEntry{},
		Held: []SafeDeleteHeld{
			{Branch: testBranch, Worktree: testWorktree, State: "pending"},
			{Branch: "automaton/y", Worktree: "/repo/wt-y", State: "merged"},
		},
	}
	report.Held[1].Dirty = true
	res := runCore(g, eligibleProbe(), report, nil, nil)
	if len(res.Removed) != 0 || len(res.Failed) != 0 {
		t.Fatalf("held entries are never removed: %+v", res)
	}
	if !skippedHas(res, cleanupSkipHeld) {
		t.Fatalf("held entry missing reason %q: %+v", cleanupSkipHeld, res.Skipped)
	}
	if !skippedHas(res, cleanupSkipDirty) {
		t.Fatalf("held-dirty entry should carry reason %q: %+v", cleanupSkipDirty, res.Skipped)
	}
}

// --------------------------------------------------------------- never force

type forceCatcher struct {
	inner    MergeGitRunner
	mu       sync.Mutex
	sawForce bool
}

func (c *forceCatcher) Run(ctx context.Context, dir string, args ...string) (string, error) {
	for _, a := range args {
		if a == "--force" || a == "-D" {
			c.mu.Lock()
			c.sawForce = true
			c.mu.Unlock()
			return "", errors.New("force was requested")
		}
	}
	return c.inner.Run(ctx, dir, args...)
}

func (c *forceCatcher) forced() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sawForce
}

func TestWorktreeCleanupOnModeWorksWhenForcedCommandsAreRefused(t *testing.T) {
	inner := newScriptedGit()
	scriptStandardRemoval(t, inner)
	inner.out[keyOf(testRepo, "worktree", "remove", testWorktree)] = ""
	inner.out[keyOf(testRepo, "branch", "-d", testBranch)] = ""
	catcher := &forceCatcher{inner: inner}
	res := runCore(catcher, eligibleProbe(), coreWorktrees(config.CleanupModeOn), nil, nil)
	if catcher.forced() {
		t.Fatalf("cleanup must never request --force or -D")
	}
	if len(res.Removed) != 1 {
		t.Fatalf("removal must succeed without force commands, got %+v", res)
	}
}

// --------------------------------------------------------------- failures

func TestWorktreeCleanupContinuesAfterAFailure(t *testing.T) {
	g := newScriptedGit()
	wtList := "worktree /repo/primary\n\n"
	wtList += "worktree /repo/wt-a\n\nbranch refs/heads/automaton/a\n"
	wtList += "worktree /repo/wt-b\n\nbranch refs/heads/automaton/b\n"
	g.out[keyOf(testRepo, "worktree", "list", "--porcelain")] = wtList
	for _, b := range []struct{ branch, wt string }{
		{"automaton/a", "/repo/wt-a"},
		{"automaton/b", "/repo/wt-b"},
	} {
		g.out[keyOf(b.wt, "status", "--porcelain")] = ""
		g.out[keyOf(testRepo, "rev-list", "--count", "main.."+b.branch)] = "0"
		g.out[keyOf(testRepo, "cherry", "main", b.branch)] = ""
		g.out[keyOf(testRepo, "log", "main.."+b.branch, "--format=%s")] = ""
		g.out[keyOf(testRepo, "log", "main", "--format=%s")] = ""
	}
	g.fail[keyOf(testRepo, "worktree", "remove", "/repo/wt-a")] = "refusing"
	g.out[keyOf(testRepo, "worktree", "remove", "/repo/wt-b")] = ""
	g.out[keyOf(testRepo, "branch", "-d", "automaton/b")] = ""
	report := SafeDeleteReport{Safe: []SafeDeleteEntry{
		{Branch: "automaton/a", Worktree: "/repo/wt-a", Reason: "all commits on main"},
		{Branch: "automaton/b", Worktree: "/repo/wt-b", Reason: "all commits on main"},
	}, Held: []SafeDeleteHeld{}}
	res := runCore(g, eligibleProbe(), report, nil, nil)
	if len(res.Removed) != 1 || res.Removed[0].Branch != "automaton/b" {
		t.Fatalf("the pass must continue to the next entry, got removed=%+v", res.Removed)
	}
	if len(res.Failed) != 1 || res.Failed[0].Stage != cleanupStageRemove || res.Failed[0].Branch != "automaton/a" {
		t.Fatalf("first failure at %q must be recorded, got %+v", cleanupStageRemove, res.Failed)
	}
}

func TestWorktreeCleanupFailureSizeLookup(t *testing.T) {
	g := newScriptedGit()
	scriptStandardRemoval(t, g)
	probe := eligibleProbe()
	probe.sizeErr = "work tree vanished"
	res := runCore(g, probe, coreWorktrees(config.CleanupModeOn), nil, nil)
	if len(res.Removed) != 0 || failedStage(res) != cleanupStageSize {
		t.Fatalf("size lookup failure at %q: %+v", cleanupStageSize, res.Failed)
	}
}

func TestWorktreeCleanupFailureAuditWrite(t *testing.T) {
	g := newScriptedGit()
	scriptStandardRemoval(t, g)
	g.out[keyOf(testRepo, "worktree", "remove", testWorktree)] = ""
	g.out[keyOf(testRepo, "branch", "-d", testBranch)] = ""
	audit := &recordedAudit{failure: "audit sink is down"}
	res := runCore(g, eligibleProbe(), coreWorktrees(config.CleanupModeOn), audit.fn(), nil)
	if len(res.Removed) != 0 || failedStage(res) != cleanupStageAudit {
		t.Fatalf("audit failure at %q must not remove: %+v", cleanupStageAudit, res.Failed)
	}
	if g.hasCall("worktree", "remove") || g.hasCall("branch", "-d") {
		t.Fatalf("audit failure must leave the entry untouched: %v", g.called)
	}
}

func TestWorktreeCleanupFailureWorktreeRemove(t *testing.T) {
	g := newScriptedGit()
	scriptStandardRemoval(t, g)
	g.out[keyOf(testRepo, "branch", "-d", testBranch)] = ""
	g.fail[keyOf(testRepo, "worktree", "remove", testWorktree)] = "path is dirty"
	res := runCore(g, eligibleProbe(), coreWorktrees(config.CleanupModeOn), nil, nil)
	if len(res.Removed) != 0 || failedStage(res) != cleanupStageRemove {
		t.Fatalf("worktree remove failure at %q: %+v", cleanupStageRemove, res.Failed)
	}
	if g.hasCall("branch", "-d") {
		t.Fatalf("branch must not be deleted when the work tree removal failed: %v", g.called)
	}
}

func TestWorktreeCleanupFailureBranchDelete(t *testing.T) {
	g := newScriptedGit()
	scriptStandardRemoval(t, g)
	g.out[keyOf(testRepo, "worktree", "remove", testWorktree)] = ""
	g.fail[keyOf(testRepo, "branch", "-d", testBranch)] = "branch is checked out elsewhere"
	res := runCore(g, eligibleProbe(), coreWorktrees(config.CleanupModeOn), nil, nil)
	if len(res.Removed) != 0 || failedStage(res) != cleanupStageBranch {
		t.Fatalf("branch delete failure at %q: %+v", cleanupStageBranch, res.Failed)
	}
}

func TestWorktreeCleanupFailureRecordDoesNotUndoRemoval(t *testing.T) {
	g := newScriptedGit()
	scriptStandardRemoval(t, g)
	g.out[keyOf(testRepo, "worktree", "remove", testWorktree)] = ""
	g.out[keyOf(testRepo, "branch", "-d", testBranch)] = ""
	rec := &recordedRecords{}
	res := runCore(g, eligibleProbe(), coreWorktrees(config.CleanupModeOn), nil, rec.failing())
	if len(res.Removed) != 1 {
		t.Fatalf("the removal happened and must stand even if recording fails, got %+v", res)
	}
	if len(res.Failed) != 1 || res.Failed[0].Stage != cleanupStageRecord {
		t.Fatalf("recording failure at %q must be reported: %+v", cleanupStageRecord, res.Failed)
	}
}

// ================================================== manager-level (real git)

// gitTestRepo runs git in dir with a hermetic identity and no inherited
// repository selection.
func gitCmd(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return string(out)
}

// fixtureRepo builds a base repository whose default branch is "trunk"
// (recorded on the remote) plus a clean linked worktree on an automaton/*
// branch with no commits ahead of trunk.
func fixtureRepo(t *testing.T) (baseRepo, worktree string) {
	t.Helper()
	isolateGitEnv(t)
	root := t.TempDir()
	bare := filepath.Join(root, "origin-bare")
	baseRepo = filepath.Join(root, "base")
	worktree = filepath.Join(root, "worktrees", "fixture")

	gitCmd(t, root, "init", "--bare", "--initial-branch=trunk", bare)
	gitCmd(t, root, "init", "--initial-branch=trunk", baseRepo)
	gitCmd(t, baseRepo, "commit", "--allow-empty", "-m", "first (on trunk)")
	gitCmd(t, baseRepo, "remote", "add", "origin", bare)
	gitCmd(t, baseRepo, "push", "-q", "origin", "trunk")
	gitCmd(t, baseRepo, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/trunk")
	gitCmd(t, baseRepo, "worktree", "add", "-q", "-B", "automaton/fixture", worktree, "trunk")
	return baseRepo, worktree
}

func branchExists(t *testing.T, baseRepo, branch string) bool {
	t.Helper()
	cmd := exec.Command("git", "-C", baseRepo, "rev-parse", "--verify", "--quiet", branch)
	return cmd.Run() == nil
}

func buildManager(t *testing.T, baseRepo, mode string, retentionHours int) (*Manager, *recordedAudit) {
	t.Helper()
	cfg := DefaultConfig()
	cfg.WorktreeBaseRepo = baseRepo
	cfg.Automation.CleanupMode = mode
	if retentionHours > 0 {
		cfg.Automation.CleanupRetentionHours = retentionHours
	}
	m, err := NewManager(t.TempDir(), cfg, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	audit := &recordedAudit{}
	m.auditFn = func(action string, details map[string]any) {
		audit.mu.Lock()
		audit.items = append(audit.items, recordedEvent{action: action, details: details})
		audit.mu.Unlock()
	}
	return m, audit
}

func saveFixturePRD(t *testing.T, m *Manager, branch string, status PRDStatus, branchBase string, updatedAt time.Time) *PRD {
	t.Helper()
	prd := &PRD{
		ID:        "prd-" + branch,
		Title:     "fixture automaton " + branch,
		Status:    status,
		Git:       GitWorkflow{Branch: branch},
		UpdatedAt: updatedAt,
	}
	if branchBase != "" {
		prd.Git.BaseBranch = branchBase
	}
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	// SavePRD stamps UpdatedAt=time.Now(); the store hands back the same
	// shared pointer, so the age used by the cleanup probe can be set
	// directly on it.
	prd.UpdatedAt = updatedAt
	return prd
}

func TestWorktreeCleanupManagerReleasesFinishedWork(t *testing.T) {
	baseRepo, worktree := fixtureRepo(t)
	m, audit := buildManager(t, baseRepo, config.CleanupModeOn, 0)
	prd := saveFixturePRD(t, m, "automaton/fixture", PRDCompleted, "", time.Now().Add(-48*time.Hour))

	m.ReleaseAutomatonWorktree(prd)

	if branchExists(t, baseRepo, "automaton/fixture") {
		t.Fatalf("finished clean work with all commits on base must be released")
	}
	if _, err := os.Stat(worktree); err == nil {
		t.Fatalf("the work tree directory must be gone")
	}
	details := audit.events(worktreeCleanupAuditAction)
	if len(details) != 1 {
		t.Fatalf("want one %q audit event, got %d", worktreeCleanupAuditAction, len(details))
	}
	if got := details[0]["branch"]; got != "automaton/fixture" {
		t.Fatalf("audit event branch = %v", got)
	}
	if got, ok := details[0]["bytes_freed"].(int64); !ok || got < 0 {
		t.Fatalf("audit event bytes_freed = %#v", details[0]["bytes_freed"])
	}
	foundNote := false
	for _, rec := range m.ListTriageRecords() {
		if rec == nil || rec.PRDID != prd.ID {
			continue
		}
		for _, note := range rec.Notes {
			if note.Source == "worktree-cleanup" && strings.Contains(note.Text, "automaton/fixture") {
				foundNote = true
			}
		}
	}
	if !foundNote {
		t.Fatalf("the removal must land a triage record note on %s", prd.ID)
	}
}

func TestWorktreeCleanupManagerReleasesCancelledWork(t *testing.T) {
	baseRepo, _ := fixtureRepo(t)
	m, _ := buildManager(t, baseRepo, config.CleanupModeOn, 0)
	// Per-Automaton base branch is honored (here: trunk, as remote HEAD is).
	prd := saveFixturePRD(t, m, "automaton/fixture", PRDCancelled, "trunk", time.Now().Add(-48*time.Hour))
	m.ReleaseAutomatonWorktree(prd)
	if branchExists(t, baseRepo, "automaton/fixture") {
		t.Fatalf("cancelled clean work with all commits on base must be released")
	}
}

func TestWorktreeCleanupLeavesUncommittedWorkAlone(t *testing.T) {
	baseRepo, worktree := fixtureRepo(t)
	m, audit := buildManager(t, baseRepo, config.CleanupModeOn, 0)
	prd := saveFixturePRD(t, m, "automaton/fixture", PRDCompleted, "", time.Now().Add(-48*time.Hour))
	if err := os.WriteFile(filepath.Join(worktree, "wip.txt"), []byte("not finished yet"), 0644); err != nil {
		t.Fatal(err)
	}
	m.ReleaseAutomatonWorktree(prd)
	if !branchExists(t, baseRepo, "automaton/fixture") {
		t.Fatalf("a work tree with uncommitted changes must be left alone")
	}
	if _, err := os.Stat(worktree); err != nil {
		t.Fatalf("the work tree must still exist: %v", err)
	}
	if n := len(audit.events(worktreeCleanupAuditAction)); n != 0 {
		t.Fatalf("no removal may be audited, got %d events", n)
	}
}

func TestWorktreeCleanupLeavesUnmergedWorkAlone(t *testing.T) {
	baseRepo, worktree := fixtureRepo(t)
	m, audit := buildManager(t, baseRepo, config.CleanupModeOn, 0)
	prd := saveFixturePRD(t, m, "automaton/fixture", PRDCompleted, "", time.Now().Add(-48*time.Hour))
	// One commit of work that is not on trunk.
	if err := os.WriteFile(filepath.Join(worktree, "new.go"), []byte("package fixture\n"), 0644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, worktree, "add", "new.go")
	gitCmd(t, worktree, "commit", "-m", "unmerged work")
	m.ReleaseAutomatonWorktree(prd)
	if !branchExists(t, baseRepo, "automaton/fixture") {
		t.Fatalf("work whose commits are not on base must be left alone")
	}
	if n := len(audit.events(worktreeCleanupAuditAction)); n != 0 {
		t.Fatalf("no removal may be audited, got %d events", n)
	}
}

func TestWorktreeCleanupLeavesLiveSessions(t *testing.T) {
	baseRepo, worktree := fixtureRepo(t)
	m, audit := buildManager(t, baseRepo, config.CleanupModeOn, 0)
	prd := saveFixturePRD(t, m, "automaton/fixture", PRDCompleted, "", time.Now().Add(-48*time.Hour))
	m.sessionListerFn = func() []SessionInfo {
		return []SessionInfo{{ID: "sess-1", Name: "autonomous:fixture", ProjectDir: worktree, Alive: true}}
	}
	m.ReleaseAutomatonWorktree(prd)
	if !branchExists(t, baseRepo, "automaton/fixture") {
		t.Fatalf("a work tree with a live session must be left alone")
	}
	found := false
	for _, e := range audit.events("autonomous_worktree_cleanup_release") {
		if skipped, ok := e["skipped"].(map[string]int); ok {
			if skipped[cleanupSkipLiveSession] >= 1 {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("outcome audit should show a live_session skip, got %+v", audit.events("autonomous_worktree_cleanup_release"))
	}
}

func TestWorktreeCleanupLeavesYoungEntries(t *testing.T) {
	baseRepo, _ := fixtureRepo(t)
	m, _ := buildManager(t, baseRepo, config.CleanupModeOn, 48)             // 48h retention
	saveFixturePRD(t, m, "automaton/fixture", PRDCompleted, "", time.Now()) // finished just now
	m.CleanupWorktreesPass()
	if !branchExists(t, baseRepo, "automaton/fixture") {
		t.Fatalf("an entry inside the retention window must be left alone by the periodic pass")
	}
}

func TestWorktreeCleanupReleaseAtEndIgnoresRetention(t *testing.T) {
	baseRepo, worktree := fixtureRepo(t)
	m, _ := buildManager(t, baseRepo, config.CleanupModeOn, 48)                    // 48h retention
	prd := saveFixturePRD(t, m, "automaton/fixture", PRDCompleted, "", time.Now()) // finished just now
	m.ReleaseAutomatonWorktree(prd)
	if branchExists(t, baseRepo, "automaton/fixture") {
		t.Fatalf("the owner's release at the end must not wait for the retention window")
	}
	if _, err := os.Stat(worktree); err == nil {
		t.Fatalf("the work tree directory must be gone")
	}
}

func TestWorktreeCleanupManagerOffModeDoesNothing(t *testing.T) {
	baseRepo, worktree := fixtureRepo(t)
	m, audit := buildManager(t, baseRepo, config.CleanupModeOff, 0)
	prd := saveFixturePRD(t, m, "automaton/fixture", PRDCompleted, "", time.Now().Add(-48*time.Hour))
	m.ReleaseAutomatonWorktree(prd)
	if !branchExists(t, baseRepo, "automaton/fixture") {
		t.Fatalf("off mode must not release")
	}
	if _, err := os.Stat(worktree); err != nil {
		t.Fatalf("off mode must not touch the work tree: %v", err)
	}
	if len(audit.events(worktreeCleanupAuditAction)) != 0 || len(audit.events("autonomous_worktree_cleanup_release")) != 0 {
		t.Fatalf("off mode must not audit work: %+v", audit.items)
	}
}

func TestWorktreeCleanupManagerReportModeRemovesNothing(t *testing.T) {
	baseRepo, worktree := fixtureRepo(t)
	m, audit := buildManager(t, baseRepo, config.CleanupModeReport, 0)
	prd := saveFixturePRD(t, m, "automaton/fixture", PRDCompleted, "", time.Now().Add(-48*time.Hour))
	m.ReleaseAutomatonWorktree(prd)
	if !branchExists(t, baseRepo, "automaton/fixture") {
		t.Fatalf("report mode must not release the branch")
	}
	if _, err := os.Stat(worktree); err != nil {
		t.Fatalf("report mode must not touch the work tree: %v", err)
	}
	if len(audit.events(worktreeCleanupAuditAction)) != 0 {
		t.Fatalf("report mode must not audit a completed removal")
	}
	outcome := audit.events("autonomous_worktree_cleanup_release")
	if len(outcome) != 1 {
		t.Fatalf("want one release outcome audit event, got %d", len(outcome))
	}
	if wr, ok := outcome[0]["would_remove"].(int); !ok || wr != 1 {
		t.Fatalf("outcome should show would_remove=1, got %#v", outcome[0])
	}
}
