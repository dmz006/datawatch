package autonomous

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// laneFillMainAnswer is one scripted answer of the fake probe.
type laneFillMainAnswer struct {
	carries  bool
	evidence string
	err      error
}

// laneFillMainProbeFake scripts the main-carries probe by Automaton id and
// records every question it was asked.
type laneFillMainProbeFake struct {
	mu      sync.Mutex
	head    string
	headErr error
	answers map[string]laneFillMainAnswer
	asked   []string
	branch  map[string]string
}

func (f *laneFillMainProbeFake) Carries(ctx context.Context, id, branch string) (bool, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, id)
	if f.branch == nil {
		f.branch = map[string]string{}
	}
	f.branch[id] = branch
	a := f.answers[id]
	return a.carries, a.evidence, a.err
}

func (f *laneFillMainProbeFake) MainHead(ctx context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.head, f.headErr
}

func (f *laneFillMainProbeFake) askedIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.asked...)
}

// laneFillWorktreeFSFake serves a scripted registered worktree list; the other
// methods of the view are unused by lane fill.
type laneFillWorktreeFSFake struct {
	list  []string
	err   error
	calls int
	repos []string
}

func (f *laneFillWorktreeFSFake) GoModAt(string) (bool, error) { return false, nil }
func (f *laneFillWorktreeFSFake) RegisteredWorktrees(repo string) ([]string, error) {
	f.calls++
	f.repos = append(f.repos, repo)
	return f.list, f.err
}
func (f *laneFillWorktreeFSFake) NewestNonGitChange(string) (time.Time, bool, error) {
	return time.Time{}, false, nil
}
func (f *laneFillWorktreeFSFake) Mtime(string) (time.Time, error) { return time.Time{}, nil }

// laneFillMainHarness is the coordinator harness with the proposal rule off
// (these tests are about the last two rules) and ready to plan.
func laneFillMainHarness(t *testing.T, mode string, mutate func(*LaneFillConfig)) *laneFillHarness {
	t.Helper()
	cfg := laneFillCoordConfig(mode)
	off := false
	cfg.LaneFill.RequireConfirmedProposal = &off
	if mutate != nil {
		mutate(&cfg.LaneFill)
	}
	h := newLaneFillHarnessIn(t, t.TempDir(), cfg)
	h.c.SetReady()
	return h
}

// laneFillRefusalOf returns the eligibility reason and the blocker text the
// poll recorded for an Automaton, or empty strings when it was not refused.
func laneFillRefusalOf(st LaneFillStatus, id string) (reason, text string) {
	for _, d := range st.Decisions {
		if d.PRDID == id && d.EligibilityReason != "" {
			return d.EligibilityReason, d.Blocker
		}
	}
	return "", ""
}

func laneFillNoStartOf(t *testing.T, h *laneFillHarness, id string) {
	t.Helper()
	for _, call := range h.fake.all() {
		if strings.Contains(call, id) {
			t.Errorf("a refused Automaton was started: %v", h.fake.all())
		}
	}
}

func TestLaneFillMainCarriesMergeCommitForTheIDRefuses(t *testing.T) {
	h := laneFillMainHarness(t, "on", nil)
	p := h.addPRD("merged already", PRDApproved, "", "pi-prompt")
	ev := fmt.Sprintf("commit 1a2b3c4 on main: Merge branch 'automaton/%s-merged-already'", p.ID)
	h.c.SetLaneFillMainProbe(&laneFillMainProbeFake{head: "h1", answers: map[string]laneFillMainAnswer{p.ID: {carries: true, evidence: ev}}})

	h.poll()

	reason, text := laneFillRefusalOf(h.status(), p.ID)
	if reason != LaneFillReasonMainCarriesWork {
		t.Fatalf("reason = %q, want %q (decisions %+v)", reason, LaneFillReasonMainCarriesWork, h.status().Decisions)
	}
	if !strings.Contains(text, ev) {
		t.Errorf("refusal text %q does not name the evidence %q", text, ev)
	}
	if strings.Contains(text, "PRD") {
		t.Errorf("refusal text %q says PRD", text)
	}
	laneFillNoStartOf(t, h, p.ID)
	if q := h.status().QueuedEligible; q.GPU != 0 {
		t.Errorf("queued eligible = %+v, want the refused Automaton not counted", q)
	}
}

func TestLaneFillMainCarriesTaskCommitForTheIDRefuses(t *testing.T) {
	h := laneFillMainHarness(t, "on", nil)
	p := h.addPRD("task landed", PRDNeedsReview, "", "pi-prompt")
	ev := fmt.Sprintf("commit 9f8e7d6 on main: task(%s): Add the thing", p.ID)
	h.c.SetLaneFillMainProbe(&laneFillMainProbeFake{head: "h1", answers: map[string]laneFillMainAnswer{p.ID: {carries: true, evidence: ev}}})

	h.poll()

	reason, text := laneFillRefusalOf(h.status(), p.ID)
	if reason != LaneFillReasonMainCarriesWork || !strings.Contains(text, ev) {
		t.Fatalf("reason %q text %q, want main_carries_work naming %q", reason, text, ev)
	}
	laneFillNoStartOf(t, h, p.ID)
}

func TestLaneFillMainCarriesBranchFullyOnMainByPatchIDRefuses(t *testing.T) {
	h := laneFillMainHarness(t, "on", nil)
	p := h.addPRD("branch on main", PRDDraft, "", "pi-prompt")
	p.Git.Branch = "automaton/" + p.ID + "-branch-on-main"
	if err := h.m.store.SavePRD(p); err != nil {
		t.Fatal(err)
	}
	ev := "Automaton branch " + p.Git.Branch + ": all 3 commit(s) are already on main by patch id"
	fake := &laneFillMainProbeFake{head: "h1", answers: map[string]laneFillMainAnswer{p.ID: {carries: true, evidence: ev}}}
	h.c.SetLaneFillMainProbe(fake)

	h.poll()

	reason, text := laneFillRefusalOf(h.status(), p.ID)
	if reason != LaneFillReasonMainCarriesWork || !strings.Contains(text, "by patch id") {
		t.Fatalf("reason %q text %q, want main_carries_work naming the comparison", reason, text)
	}
	if got := fake.branch[p.ID]; got != p.Git.Branch {
		t.Errorf("the probe was asked about branch %q, want the recorded %q", got, p.Git.Branch)
	}
	laneFillNoStartOf(t, h, p.ID)
}

func TestLaneFillMainCarriesBranchPartlyOnMainDoesNotRefuse(t *testing.T) {
	h := laneFillMainHarness(t, "on", nil)
	p := h.addPRD("partly merged", PRDApproved, "", "pi-prompt")
	h.c.SetLaneFillMainProbe(&laneFillMainProbeFake{head: "h1", answers: map[string]laneFillMainAnswer{p.ID: {carries: false}}})

	h.poll()

	if reason, _ := laneFillRefusalOf(h.status(), p.ID); reason != "" {
		t.Fatalf("a branch only partly on main was refused: %q", reason)
	}
	if q := h.status().QueuedEligible; q.GPU != 1 {
		t.Errorf("queued eligible = %+v, want the Automaton counted", q)
	}
	started := false
	for _, call := range h.fake.all() {
		started = started || strings.Contains(call, p.ID)
	}
	if !started {
		t.Errorf("the Automaton was not started: %v", h.fake.all())
	}
}

func TestLaneFillMainCarriesNoBranchAndNoCommitsDoesNotRefuse(t *testing.T) {
	h := laneFillMainHarness(t, "shadow", nil)
	p := h.addPRD("fresh", PRDApproved, "", "pi-prompt")
	fake := &laneFillMainProbeFake{head: "h1"} // no scripted answer: not carried
	h.c.SetLaneFillMainProbe(fake)

	h.poll()

	if reason, _ := laneFillRefusalOf(h.status(), p.ID); reason != "" {
		t.Fatalf("an Automaton with no branch and no commits was refused: %q", reason)
	}
	if got := fake.askedIDs(); len(got) != 1 || fake.branch[p.ID] != "" {
		t.Errorf("asked %v with branch %q, want one question with no branch", got, fake.branch[p.ID])
	}
}

func TestLaneFillMainCarriesProbeErrorDoesNotRefuse(t *testing.T) {
	h := laneFillMainHarness(t, "on", nil)
	p := h.addPRD("probe fails", PRDApproved, "", "pi-prompt")
	h.c.SetLaneFillMainProbe(&laneFillMainProbeFake{head: "h1", answers: map[string]laneFillMainAnswer{
		p.ID: {carries: true, evidence: "stale answer", err: errors.New("git: timed out")},
	}})

	h.poll()

	if reason, _ := laneFillRefusalOf(h.status(), p.ID); reason != "" {
		t.Fatalf("a probe error refused the Automaton: %q", reason)
	}
	started := false
	for _, call := range h.fake.all() {
		started = started || strings.Contains(call, p.ID)
	}
	if !started {
		t.Errorf("an unknown answer stopped the Automaton from starting: %v", h.fake.all())
	}
}

func TestLaneFillMainCarriesNoProbeNeverRefuses(t *testing.T) {
	h := laneFillMainHarness(t, "shadow", nil)
	p := h.addPRD("no probe", PRDApproved, "", "pi-prompt")
	h.poll()
	if reason, _ := laneFillRefusalOf(h.status(), p.ID); reason != "" {
		t.Fatalf("refused without a probe: %q", reason)
	}
}

func TestLaneFillMainCarriesOnlyQueuedAutomataAreProbed(t *testing.T) {
	h := laneFillMainHarness(t, "shadow", nil)
	queued := h.addPRD("queued", PRDApproved, "", "pi-prompt")
	done := h.addPRD("done", PRDCompleted, "", "pi-prompt")
	running := h.addPRD("running", PRDRunning, "", "claude-code")
	fake := &laneFillMainProbeFake{head: "h1", answers: map[string]laneFillMainAnswer{
		done.ID: {carries: true, evidence: "x"}, running.ID: {carries: true, evidence: "x"},
	}}
	h.c.SetLaneFillMainProbe(fake)

	h.poll()

	asked := fake.askedIDs()
	if len(asked) != 1 || asked[0] != queued.ID {
		t.Errorf("asked %v, want only the queued Automaton %s", asked, queued.ID)
	}
}

func TestLaneFillMainCarriesPerPollBoundNewestFirstAndCacheReuse(t *testing.T) {
	h := laneFillMainHarness(t, "shadow", func(c *LaneFillConfig) { c.GPULanes, c.ClaudeLanes = 0, 0 })
	total := laneFillMainProbeMaxPerPoll + 4
	var ids []string // oldest first
	for i := 0; i < total; i++ {
		ids = append(ids, h.addPRD(fmt.Sprintf("queued %02d", i), PRDApproved, "", "pi-prompt").ID)
	}
	fake := &laneFillMainProbeFake{head: "head-1", answers: map[string]laneFillMainAnswer{}}
	h.c.SetLaneFillMainProbe(fake)

	h.poll()
	first := fake.askedIDs()
	if len(first) != laneFillMainProbeMaxPerPoll {
		t.Fatalf("probed %d Automata in one poll, want the bound %d", len(first), laneFillMainProbeMaxPerPoll)
	}
	for i, id := range first { // newest first
		if want := ids[total-1-i]; id != want {
			t.Fatalf("probe order = %v, want newest first starting at %s", first, ids[total-1])
		}
	}

	// The next poll reuses the cached answers and probes the rest.
	h.poll()
	second := fake.askedIDs()[len(first):]
	if len(second) != total-laneFillMainProbeMaxPerPoll {
		t.Fatalf("second poll probed %d, want the remaining %d (cached ones skipped): %v", len(second), total-laneFillMainProbeMaxPerPoll, second)
	}
	seen := map[string]bool{}
	for _, id := range fake.askedIDs() {
		if seen[id] {
			t.Errorf("Automaton %s probed twice for the same head", id)
		}
		seen[id] = true
	}

	// A third poll with the same head probes nothing.
	n := len(fake.askedIDs())
	h.poll()
	if got := len(fake.askedIDs()); got != n {
		t.Errorf("a poll with an unchanged head probed %d more", got-n)
	}

	// A new head invalidates the cache, bounded again.
	fake.mu.Lock()
	fake.head = "head-2"
	fake.mu.Unlock()
	h.poll()
	if got := len(fake.askedIDs()) - n; got != laneFillMainProbeMaxPerPoll {
		t.Errorf("after the head moved, probed %d, want %d", got, laneFillMainProbeMaxPerPoll)
	}
}

func TestLaneFillMainCarriesCachedRefusalHoldsUntilTheHeadMoves(t *testing.T) {
	h := laneFillMainHarness(t, "shadow", nil)
	p := h.addPRD("carried", PRDApproved, "", "pi-prompt")
	fake := &laneFillMainProbeFake{head: "head-1", answers: map[string]laneFillMainAnswer{p.ID: {carries: true, evidence: "commit abc on main"}}}
	h.c.SetLaneFillMainProbe(fake)

	h.poll()
	h.poll()
	if got := fake.askedIDs(); len(got) != 1 {
		t.Fatalf("asked %v, want one question while the head is unchanged", got)
	}
	if reason, _ := laneFillRefusalOf(h.status(), p.ID); reason != LaneFillReasonMainCarriesWork {
		t.Fatalf("the cached answer lost its refusal: %q", reason)
	}

	fake.mu.Lock()
	fake.head = "head-2"
	fake.answers[p.ID] = laneFillMainAnswer{carries: false}
	fake.mu.Unlock()
	h.poll()
	if got := fake.askedIDs(); len(got) != 2 {
		t.Fatalf("asked %v, want a fresh question after the head moved", got)
	}
	if q := h.status().QueuedEligible; q.GPU != 1 {
		t.Errorf("queued eligible = %+v, want the Automaton eligible again", q)
	}
}

func TestLaneFillMainCarriesConfigKeyFalseDisablesRule(t *testing.T) {
	off := false
	h := laneFillMainHarness(t, "shadow", func(c *LaneFillConfig) { c.CheckMainCarries = &off })
	p := h.addPRD("carried", PRDApproved, "", "pi-prompt")
	fake := &laneFillMainProbeFake{head: "h1", answers: map[string]laneFillMainAnswer{p.ID: {carries: true, evidence: "commit abc"}}}
	h.c.SetLaneFillMainProbe(fake)

	h.poll()

	if reason, _ := laneFillRefusalOf(h.status(), p.ID); reason != "" {
		t.Errorf("check_main_carries false still refused: %q", reason)
	}
	if got := fake.askedIDs(); len(got) != 0 {
		t.Errorf("check_main_carries false still probed: %v", got)
	}
	// The pure evaluator agrees.
	cfg := laneFillTestConfig()
	cfg.CheckMainCarries = &off
	c := LaneFillCandidate{ID: "aaaa0001", Status: PRDApproved, MainState: LaneFillMainCarries, MainEvidence: "x"}
	if code, _ := EvaluateLaneFillEligibility(cfg, c, time.Now()); code != "" {
		t.Errorf("evaluator refused with the rule off: %q", code)
	}
}

func TestLaneFillMainCarriesOrderIsProposalThenMainThenWorktree(t *testing.T) {
	cfg := laneFillTestConfig()
	c := LaneFillCandidate{
		ID: "aaaa0001", Status: PRDApproved,
		ProposalState: LaneFillProposalMissing,
		MainState:     LaneFillMainCarries, MainEvidence: "commit abc",
		WorktreeState: LaneFillWorktreeInvalid, WorktreeProblem: "gone",
	}
	now := time.Now()
	if code, _ := EvaluateLaneFillEligibility(cfg, c, now); code != LaneFillReasonProposalMissing {
		t.Errorf("code = %q, want the proposal rule first", code)
	}
	c.ProposalState = LaneFillProposalConfirmed
	if code, _ := EvaluateLaneFillEligibility(cfg, c, now); code != LaneFillReasonMainCarriesWork {
		t.Errorf("code = %q, want main_carries_work before worktree_invalid", code)
	}
	c.MainState = LaneFillMainClear
	if code, _ := EvaluateLaneFillEligibility(cfg, c, now); code != LaneFillReasonWorktreeInvalid {
		t.Errorf("code = %q, want worktree_invalid last", code)
	}
	c.Status = PRDRunning
	if code, _ := EvaluateLaneFillEligibility(cfg, c, now); code != "" {
		t.Errorf("code = %q, running work is never refused", code)
	}
}

// laneFillWorktreeSetup makes a worktree-shaped directory on disk: with or
// without a .git entry.
func laneFillWorktreeSetup(t *testing.T, withGit bool) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "wt")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if withGit {
		if err := os.WriteFile(filepath.Join(dir, ".git"), []byte("gitdir: /elsewhere\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// laneFillWorktreePRD records a worktree on the Automaton, the way the run
// path does: the project directory and the branch.
func laneFillWorktreePRD(t *testing.T, h *laneFillHarness, title, dir string) *PRD {
	t.Helper()
	p := h.addPRD(title, PRDApproved, "", "pi-prompt")
	p.ProjectDir = dir
	p.Git.Branch = "automaton/" + p.ID
	if err := h.m.store.SavePRD(p); err != nil {
		t.Fatal(err)
	}
	return p
}

func laneFillWorktreeHarness(t *testing.T, mutate func(*LaneFillConfig)) (*laneFillHarness, *laneFillWorktreeFSFake) {
	t.Helper()
	h := laneFillMainHarness(t, "on", mutate)
	fsys := &laneFillWorktreeFSFake{}
	h.c.SetLaneFillWorktreeFS(fsys)
	cfg := h.m.Config()
	cfg.WorktreeBaseRepo = "base-repo"
	h.m.SetConfig(cfg)
	return h, fsys
}

func TestLaneFillWorktreeMissingDirectoryRefuses(t *testing.T) {
	h, fsys := laneFillWorktreeHarness(t, nil)
	dir := filepath.Join(t.TempDir(), "gone")
	p := laneFillWorktreePRD(t, h, "gone", dir)
	fsys.list = []string{dir}

	h.poll()

	reason, text := laneFillRefusalOf(h.status(), p.ID)
	if reason != LaneFillReasonWorktreeInvalid || !strings.Contains(text, "does not exist") {
		t.Fatalf("reason %q text %q, want worktree_invalid saying the directory is missing", reason, text)
	}
	laneFillNoStartOf(t, h, p.ID)
	if fsys.calls != 0 {
		t.Errorf("the registered list was read %d times for a missing directory", fsys.calls)
	}
}

func TestLaneFillWorktreeDirectoryWithoutGitEntryRefuses(t *testing.T) {
	h, fsys := laneFillWorktreeHarness(t, nil)
	dir := laneFillWorktreeSetup(t, false)
	p := laneFillWorktreePRD(t, h, "no git", dir)
	fsys.list = []string{dir}

	h.poll()

	reason, text := laneFillRefusalOf(h.status(), p.ID)
	if reason != LaneFillReasonWorktreeInvalid || !strings.Contains(text, ".git") {
		t.Fatalf("reason %q text %q, want worktree_invalid naming the missing .git entry", reason, text)
	}
}

func TestLaneFillWorktreeNotInRegisteredListRefuses(t *testing.T) {
	h, fsys := laneFillWorktreeHarness(t, nil)
	dir := laneFillWorktreeSetup(t, true)
	p := laneFillWorktreePRD(t, h, "unregistered", dir)
	fsys.list = []string{filepath.Join(t.TempDir(), "some-other-worktree")}

	h.poll()

	reason, text := laneFillRefusalOf(h.status(), p.ID)
	if reason != LaneFillReasonWorktreeInvalid || !strings.Contains(text, "registered") {
		t.Fatalf("reason %q text %q, want worktree_invalid saying it is not registered", reason, text)
	}
	if len(fsys.repos) == 0 || fsys.repos[0] != "base-repo" {
		t.Errorf("the list was read for repositories %v, want the base repository", fsys.repos)
	}
	laneFillNoStartOf(t, h, p.ID)
}

func TestLaneFillWorktreeRegisteredHealthyPasses(t *testing.T) {
	h, fsys := laneFillWorktreeHarness(t, nil)
	dir := laneFillWorktreeSetup(t, true)
	p := laneFillWorktreePRD(t, h, "healthy", dir)
	fsys.list = []string{"/not/this/one", dir}

	h.poll()

	if reason, text := laneFillRefusalOf(h.status(), p.ID); reason != "" {
		t.Fatalf("a registered healthy worktree was refused: %q %q", reason, text)
	}
	started := false
	for _, call := range h.fake.all() {
		started = started || strings.Contains(call, p.ID)
	}
	if !started {
		t.Errorf("a healthy Automaton was not started: %v", h.fake.all())
	}
}

func TestLaneFillWorktreeRegisteredThroughASymbolicLinkPasses(t *testing.T) {
	h, fsys := laneFillWorktreeHarness(t, nil)
	dir := laneFillWorktreeSetup(t, true)
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Skipf("symbolic links unavailable: %v", err)
	}
	p := laneFillWorktreePRD(t, h, "linked", link)
	fsys.list = []string{dir}

	h.poll()

	if reason, _ := laneFillRefusalOf(h.status(), p.ID); reason != "" {
		t.Fatalf("a worktree reached through a link was refused: %q", reason)
	}
}

func TestLaneFillWorktreeNoWorktreeYetPasses(t *testing.T) {
	h, fsys := laneFillWorktreeHarness(t, nil)
	p := h.addPRD("never ran", PRDApproved, "", "pi-prompt") // no project directory, no branch
	// An explicit project directory with no branch is not a worktree either.
	q := h.addPRD("explicit dir", PRDApproved, "", "pi-prompt")
	q.ProjectDir = filepath.Join(t.TempDir(), "elsewhere")
	if err := h.m.store.SavePRD(q); err != nil {
		t.Fatal(err)
	}

	h.poll()

	for _, id := range []string{p.ID, q.ID} {
		if reason, _ := laneFillRefusalOf(h.status(), id); reason != "" {
			t.Errorf("Automaton %s with no worktree yet was refused: %q", id, reason)
		}
	}
	if fsys.calls != 0 {
		t.Errorf("the registered list was read %d times with no worktree to check", fsys.calls)
	}
}

func TestLaneFillWorktreeUnreadableListPasses(t *testing.T) {
	h, fsys := laneFillWorktreeHarness(t, nil)
	dir := laneFillWorktreeSetup(t, true)
	p := laneFillWorktreePRD(t, h, "list fails", dir)
	fsys.err = errors.New("git missing")

	h.poll()

	if reason, _ := laneFillRefusalOf(h.status(), p.ID); reason != "" {
		t.Fatalf("an unreadable list refused the Automaton: %q", reason)
	}
}

func TestLaneFillWorktreeNoViewOrNoBaseRepositoryPasses(t *testing.T) {
	// No view set.
	h := laneFillMainHarness(t, "shadow", nil)
	dir := laneFillWorktreeSetup(t, true)
	p := laneFillWorktreePRD(t, h, "no view", dir)
	h.poll()
	if reason, _ := laneFillRefusalOf(h.status(), p.ID); reason != "" {
		t.Errorf("refused with no worktree view: %q", reason)
	}
	// A view but no base repository.
	h2 := laneFillMainHarness(t, "shadow", nil)
	fsys := &laneFillWorktreeFSFake{list: nil}
	h2.c.SetLaneFillWorktreeFS(fsys)
	p2 := laneFillWorktreePRD(t, h2, "no base", laneFillWorktreeSetup(t, true))
	h2.poll()
	if reason, _ := laneFillRefusalOf(h2.status(), p2.ID); reason != "" {
		t.Errorf("refused with no base repository: %q", reason)
	}
	if fsys.calls != 0 {
		t.Errorf("the list was read with no base repository")
	}
}

func TestLaneFillWorktreeListIsReadOncePerPoll(t *testing.T) {
	h, fsys := laneFillWorktreeHarness(t, nil)
	var dirs []string
	for i := 0; i < 4; i++ {
		d := laneFillWorktreeSetup(t, true)
		dirs = append(dirs, d)
		laneFillWorktreePRD(t, h, fmt.Sprintf("wt %d", i), d)
	}
	fsys.list = dirs

	h.poll()

	if fsys.calls != 1 {
		t.Errorf("the registered list was read %d times in one poll, want once", fsys.calls)
	}
}

func TestLaneFillWorktreeConfigKeyFalseDisablesRule(t *testing.T) {
	off := false
	h, fsys := laneFillWorktreeHarness(t, func(c *LaneFillConfig) { c.RequireWorktree = &off })
	p := laneFillWorktreePRD(t, h, "gone", filepath.Join(t.TempDir(), "gone"))

	h.poll()

	if reason, _ := laneFillRefusalOf(h.status(), p.ID); reason != "" {
		t.Errorf("require_worktree false still refused: %q", reason)
	}
	if fsys.calls != 0 {
		t.Errorf("require_worktree false still read the list")
	}
	cfg := laneFillTestConfig()
	cfg.RequireWorktree = &off
	c := LaneFillCandidate{ID: "aaaa0001", Status: PRDApproved, WorktreeState: LaneFillWorktreeInvalid, WorktreeProblem: "x"}
	if code, _ := EvaluateLaneFillEligibility(cfg, c, time.Now()); code != "" {
		t.Errorf("evaluator refused with the rule off: %q", code)
	}
}

func TestLaneFillWorktreeOnlyQueuedAutomataAreChecked(t *testing.T) {
	h, _ := laneFillWorktreeHarness(t, nil)
	p := laneFillWorktreePRD(t, h, "was running", filepath.Join(t.TempDir(), "gone"))
	p.Status = PRDCompleted
	if err := h.m.store.SavePRD(p); err != nil {
		t.Fatal(err)
	}
	h.poll()
	if reason, _ := laneFillRefusalOf(h.status(), p.ID); reason != "" {
		t.Errorf("finished work was refused: %q", reason)
	}
}

func TestLaneFillWorktreeRefusalNeverCountsAsEligibleWork(t *testing.T) {
	h, fsys := laneFillWorktreeHarness(t, nil)
	p := laneFillWorktreePRD(t, h, "broken", filepath.Join(t.TempDir(), "gone"))
	fsys.list = nil

	h.poll()

	if q := h.status().QueuedEligible; q.GPU != 0 || q.Claude != 0 {
		t.Errorf("queued eligible = %+v, want the refused Automaton not counted (the idle lane watch reads this)", q)
	}
	for _, l := range h.status().Lanes {
		if l.EligibleWaiting != 0 {
			t.Errorf("lane %s counts %d eligible waiting", l.ID, l.EligibleWaiting)
		}
	}
	if reason, _ := laneFillRefusalOf(h.status(), p.ID); reason != LaneFillReasonWorktreeInvalid {
		t.Errorf("reason = %q", reason)
	}
}
