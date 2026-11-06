package autonomous

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeBaseGit stands in for git for the base-commit tests. Commits resolve
// when they are in commits (value = the full sha rev-parse prints); they are
// ancestors of HEAD when in ancestors; merge-base answers come from
// mergeBases keyed by the ref; files are keyed "<sha>:<path>".
type fakeBaseGit struct {
	mu         sync.Mutex
	commits    map[string]string
	ancestors  map[string]bool
	mergeBases map[string]string
	symbolic   string
	files      map[string]string
	showErr    error
	calls      [][]string
}

func newFakeBaseGit() *fakeBaseGit {
	return &fakeBaseGit{
		commits:    map[string]string{},
		ancestors:  map[string]bool{},
		mergeBases: map[string]string{},
		files:      map[string]string{},
	}
}

// good registers sha as a resolvable ancestor of HEAD.
func (g *fakeBaseGit) good(sha string) {
	g.commits[sha] = sha
	g.ancestors[sha] = true
}

func (g *fakeBaseGit) run(_ context.Context, _ string, args ...string) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls = append(g.calls, append([]string(nil), args...))
	if len(args) == 0 {
		return "", nil
	}
	switch args[0] {
	case "rev-parse":
		cand := strings.TrimSuffix(args[len(args)-1], "^{commit}")
		if full, ok := g.commits[cand]; ok {
			return full + "\n", nil
		}
		return "", errors.New("exit status 128")
	case "merge-base":
		if args[1] == "--is-ancestor" {
			if g.ancestors[args[2]] {
				return "", nil
			}
			return "", errors.New("exit status 1")
		}
		if sha, ok := g.mergeBases[args[2]]; ok {
			return sha + "\n", nil
		}
		return "", errors.New("exit status 1")
	case "symbolic-ref":
		if g.symbolic != "" {
			return g.symbolic + "\n", nil
		}
		return "", errors.New("exit status 128")
	case "show":
		if g.showErr != nil {
			return "", g.showErr
		}
		if body, ok := g.files[args[1]]; ok {
			return body, nil
		}
		return "", fmt.Errorf("exit status 128: fatal: path '%s' does not exist in '%s'", args[1], args[1])
	}
	return "", nil
}

func (g *fakeBaseGit) sawCall(prefix ...string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, c := range g.calls {
		if len(c) >= len(prefix) && strings.Join(c[:len(prefix)], " ") == strings.Join(prefix, " ") {
			return true
		}
	}
	return false
}

func baseCommitManager(t *testing.T, g *fakeBaseGit) *Manager {
	t.Helper()
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	m.driftGit = g.run
	return m
}

func baseCommitTask(id, baseSHA, preSHA string, started *time.Time) Task {
	return Task{ID: id, BaseSHA: baseSHA, PreTaskSHA: preSHA, StartedAt: started}
}

func baseCommitPRD(tasks ...Task) *PRD {
	return &PRD{ID: "base-prd", ProjectDir: "proj", Story: []Story{{ID: "s1", Tasks: tasks}}}
}

func tp(t time.Time) *time.Time { return &t }

func TestBaseCommitTaskSourcesInOrder(t *testing.T) {
	cases := []struct {
		name          string
		base, pre     string
		wantSHA       string
		wantHow       string
		mergeBaseWant string
	}{
		{"task base wins", "base111", "pre2222", "base111", "task_base_sha", "mb33333"},
		{"pre sha when no base", "", "pre2222", "pre2222", "task_pre_sha", "mb33333"},
		{"merge base last", "", "", "mb33333", "merge_base", "mb33333"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := newFakeBaseGit()
			for _, s := range []string{"base111", "pre2222", "mb33333"} {
				g.good(s)
			}
			g.mergeBases["main"] = tc.mergeBaseWant
			m := baseCommitManager(t, g)
			task := baseCommitTask("t1", tc.base, tc.pre, nil)
			prd := baseCommitPRD(task)

			sha, how, err := m.resolveBaseCommit(context.Background(), prd, &prd.Story[0].Tasks[0])
			if err != nil {
				t.Fatalf("resolveBaseCommit: %v", err)
			}
			if sha != tc.wantSHA || how != tc.wantHow {
				t.Fatalf("got (%s, %s), want (%s, %s)", sha, how, tc.wantSHA, tc.wantHow)
			}
		})
	}
}

func TestBaseCommitUnverifiableCandidateFallsThrough(t *testing.T) {
	g := newFakeBaseGit()
	g.good("pre2222")
	// base111 does not resolve at all.
	m := baseCommitManager(t, g)
	prd := baseCommitPRD(baseCommitTask("t1", "base111", "pre2222", nil))

	sha, how, err := m.resolveBaseCommit(context.Background(), prd, &prd.Story[0].Tasks[0])
	if err != nil {
		t.Fatalf("resolveBaseCommit: %v", err)
	}
	if sha != "pre2222" || how != "task_pre_sha" {
		t.Fatalf("unverifiable base: got (%s, %s), want (pre2222, task_pre_sha)", sha, how)
	}

	// An unverifiable PreTaskSHA does not block a good BaseSHA, and in direct
	// mode the Automaton falls from the base to the pre-task commit.
	g2 := newFakeBaseGit()
	g2.good("base111")
	m2 := baseCommitManager(t, g2)
	prd2 := baseCommitPRD(baseCommitTask("t1", "base111", "gone999", nil))
	sha, how, err = m2.resolveBaseCommit(context.Background(), prd2, &prd2.Story[0].Tasks[0])
	if err != nil || sha != "base111" || how != "task_base_sha" {
		t.Fatalf("got (%s, %s, %v), want (base111, task_base_sha, nil)", sha, how, err)
	}

	g3 := newFakeBaseGit()
	g3.good("pre2222")
	m3 := baseCommitManager(t, g3)
	prd3 := baseCommitPRD(baseCommitTask("t1", "gone111", "pre2222", tp(time.Unix(100, 0))))
	sha, how, err = m3.resolveBaseCommit(context.Background(), prd3, nil)
	if err != nil || sha != "pre2222" || how != "first_task_pre_sha" {
		t.Fatalf("direct mode fall to pre sha: got (%s, %s, %v)", sha, how, err)
	}
}

func TestBaseCommitSkipsCandidateThatIsNotAnAncestor(t *testing.T) {
	g := newFakeBaseGit()
	g.commits["base111"] = "base111" // resolves, but is on another line of history
	g.good("pre2222")
	m := baseCommitManager(t, g)
	prd := baseCommitPRD(baseCommitTask("t1", "base111", "pre2222", nil))

	sha, how, err := m.resolveBaseCommit(context.Background(), prd, &prd.Story[0].Tasks[0])
	if err != nil {
		t.Fatalf("resolveBaseCommit: %v", err)
	}
	if sha != "pre2222" || how != "task_pre_sha" {
		t.Fatalf("got (%s, %s), want the ancestor pre2222", sha, how)
	}
	if !g.sawCall("merge-base", "--is-ancestor", "base111", "HEAD") {
		t.Fatalf("the ancestor check never ran for base111: %v", g.calls)
	}
}

func TestBaseCommitReturnsResolvedFullSHA(t *testing.T) {
	g := newFakeBaseGit()
	g.commits["abc1234"] = "abc1234ffffffffffffffffffffffffffffffff"
	g.ancestors["abc1234ffffffffffffffffffffffffffffffff"] = true
	m := baseCommitManager(t, g)
	prd := baseCommitPRD(baseCommitTask("t1", "abc1234", "", nil))
	sha, _, err := m.resolveBaseCommit(context.Background(), prd, &prd.Story[0].Tasks[0])
	if err != nil || sha != "abc1234ffffffffffffffffffffffffffffffff" {
		t.Fatalf("got (%s, %v), want the full sha", sha, err)
	}
}

func TestBaseCommitWorktreeModeUsesMergeBase(t *testing.T) {
	g := newFakeBaseGit()
	g.good("mb33333")
	g.good("base111") // a task base exists but the worktree rule ignores it
	g.mergeBases["develop"] = "mb33333"
	m := baseCommitManager(t, g)
	prd := baseCommitPRD(baseCommitTask("t1", "base111", "", tp(time.Unix(10, 0))))
	prd.Git = GitWorkflow{Branch: "automaton/base-prd", BaseBranch: "develop"}

	sha, how, err := m.resolveBaseCommit(context.Background(), prd, nil)
	if err != nil {
		t.Fatalf("resolveBaseCommit: %v", err)
	}
	if sha != "mb33333" || how != "merge_base" {
		t.Fatalf("got (%s, %s), want (mb33333, merge_base)", sha, how)
	}
	if !g.sawCall("merge-base", "HEAD", "develop") {
		t.Fatalf("merge-base was not taken against the recorded base branch: %v", g.calls)
	}
}

func TestBaseCommitWorktreeModeBaseBranchFallbacks(t *testing.T) {
	// No recorded base branch and no configured base repo: the origin default
	// seen from the worktree names the branch.
	g := newFakeBaseGit()
	g.good("mb33333")
	g.symbolic = "refs/remotes/origin/trunk"
	g.mergeBases["trunk"] = "mb33333"
	m := baseCommitManager(t, g)
	prd := baseCommitPRD()
	prd.Git.Branch = "automaton/base-prd"
	sha, how, err := m.resolveBaseCommit(context.Background(), prd, nil)
	if err != nil || sha != "mb33333" || how != "merge_base" {
		t.Fatalf("origin default: got (%s, %s, %v)", sha, how, err)
	}

	// Nothing names a branch: main, then origin's copy of it.
	g2 := newFakeBaseGit()
	g2.good("mb44444")
	g2.mergeBases["origin/main"] = "mb44444"
	m2 := baseCommitManager(t, g2)
	sha, how, err = m2.resolveBaseCommit(context.Background(), prd, nil)
	if err != nil || sha != "mb44444" || how != "merge_base" {
		t.Fatalf("origin/main fallback: got (%s, %s, %v)", sha, how, err)
	}
}

func TestBaseCommitTaskMergeBaseUsesTaskDirectory(t *testing.T) {
	var dirs []string
	g := newFakeBaseGit()
	g.good("mb33333")
	g.mergeBases["main"] = "mb33333"
	m := baseCommitManager(t, g)
	inner := m.driftGit
	m.driftGit = func(ctx context.Context, dir string, args ...string) (string, error) {
		dirs = append(dirs, dir)
		return inner(ctx, dir, args...)
	}
	prd := baseCommitPRD(Task{ID: "t1", ProjectDir: "elsewhere"})
	if _, _, err := m.resolveBaseCommit(context.Background(), prd, &prd.Story[0].Tasks[0]); err != nil {
		t.Fatalf("resolveBaseCommit: %v", err)
	}
	for _, d := range dirs {
		if d != "elsewhere" {
			t.Fatalf("git ran in %q, want the task directory", d)
		}
	}
	if len(dirs) == 0 {
		t.Fatal("git never ran")
	}
}

func TestBaseCommitDirectModePicksEarliestStartedTask(t *testing.T) {
	g := newFakeBaseGit()
	g.good("early11")
	g.good("late222")
	g.good("never33")
	m := baseCommitManager(t, g)
	// Story order is the reverse of start order; a task that never started
	// comes first in the story and must not win.
	prd := baseCommitPRD(
		baseCommitTask("t0", "never33", "", nil),
		baseCommitTask("t1", "late222", "", tp(time.Unix(2000, 0))),
		baseCommitTask("t2", "early11", "", tp(time.Unix(1000, 0))),
		baseCommitTask("t3", "", "pre9999", tp(time.Unix(500, 0))), // earliest but no BaseSHA
	)
	sha, how, err := m.resolveBaseCommit(context.Background(), prd, nil)
	if err != nil {
		t.Fatalf("resolveBaseCommit: %v", err)
	}
	if sha != "early11" || how != "first_task_base_sha" {
		t.Fatalf("got (%s, %s), want (early11, first_task_base_sha)", sha, how)
	}

	// No task carries a BaseSHA: the earliest PreTaskSHA stands in.
	g.good("pre9999")
	g.good("pre8888")
	prd2 := baseCommitPRD(
		baseCommitTask("t1", "", "pre8888", tp(time.Unix(2000, 0))),
		baseCommitTask("t2", "", "pre9999", tp(time.Unix(1000, 0))),
	)
	sha, how, err = m.resolveBaseCommit(context.Background(), prd2, nil)
	if err != nil || sha != "pre9999" || how != "first_task_pre_sha" {
		t.Fatalf("pre sha: got (%s, %s, %v)", sha, how, err)
	}
}

func TestBaseCommitNothingResolvableReturnsErrNoBaseCommit(t *testing.T) {
	g := newFakeBaseGit()
	m := baseCommitManager(t, g)
	prd := baseCommitPRD(baseCommitTask("t1", "gone111", "gone222", nil))

	_, _, err := m.resolveBaseCommit(context.Background(), prd, &prd.Story[0].Tasks[0])
	if !errors.Is(err, ErrNoBaseCommit) {
		t.Fatalf("err = %v, want ErrNoBaseCommit", err)
	}
	for _, want := range []string{"task_base_sha", "task_pre_sha", "merge_base"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not name %s", err.Error(), want)
		}
	}

	_, _, err = m.resolveBaseCommit(context.Background(), baseCommitPRD(), nil)
	if !errors.Is(err, ErrNoBaseCommit) {
		t.Fatalf("empty direct Automaton: err = %v, want ErrNoBaseCommit", err)
	}

	noDir := baseCommitPRD()
	noDir.ProjectDir = ""
	if _, _, err = m.resolveBaseCommit(context.Background(), noDir, nil); !errors.Is(err, ErrNoBaseCommit) {
		t.Fatalf("no directory: err = %v, want ErrNoBaseCommit", err)
	}
	if _, _, err = m.resolveBaseCommit(context.Background(), nil, nil); !errors.Is(err, ErrNoBaseCommit) {
		t.Fatalf("nil Automaton: err = %v, want ErrNoBaseCommit", err)
	}
	if strings.Contains(err.Error(), "PRD") {
		t.Fatalf("operator text says PRD: %q", err.Error())
	}
}

func TestBaseCommitCancelledContextStops(t *testing.T) {
	g := newFakeBaseGit()
	g.good("base111")
	m := baseCommitManager(t, g)
	prd := baseCommitPRD(baseCommitTask("t1", "base111", "", nil))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := m.resolveBaseCommit(ctx, prd, &prd.Story[0].Tasks[0]); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestBaseCommitReadFileAtCommit(t *testing.T) {
	g := newFakeBaseGit()
	g.files["abc1234:docs/rules.md"] = "the rules\n"

	got, trunc, err := readFileAtCommit(context.Background(), "d", "abc1234", "./docs/../docs/rules.md", g.run)
	if err != nil || trunc || got != "the rules\n" {
		t.Fatalf("got (%q, %v, %v)", got, trunc, err)
	}
	if !g.sawCall("show", "abc1234:docs/rules.md") {
		t.Fatalf("path was not normalised into the show argument: %v", g.calls)
	}
}

func TestBaseCommitReadFileAtCommitMissingPath(t *testing.T) {
	g := newFakeBaseGit()
	_, _, err := readFileAtCommit(context.Background(), "d", "abc1234", "AGENT.md", g.run)
	if !errors.Is(err, ErrPathMissingAtCommit) {
		t.Fatalf("err = %v, want ErrPathMissingAtCommit", err)
	}

	// Any other git failure is wrapped, and is not reported as a missing path.
	boom := errors.New("fatal: bad object")
	g.showErr = boom
	_, _, err = readFileAtCommit(context.Background(), "d", "abc1234", "AGENT.md", g.run)
	if err == nil || errors.Is(err, ErrPathMissingAtCommit) || !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the wrapped git failure", err)
	}
}

func TestBaseCommitReadFileAtCommitTruncation(t *testing.T) {
	g := newFakeBaseGit()
	exact := strings.Repeat("a", baseCommitFileCap)
	g.files["abc1234:exact.md"] = exact
	g.files["abc1234:big.md"] = exact + "overflow"
	// A multi-byte character straddles the cap: the cut backs off to a
	// character boundary instead of splitting it.
	g.files["abc1234:utf.md"] = strings.Repeat("a", baseCommitFileCap-1) + "é" + "tail"

	got, trunc, err := readFileAtCommit(context.Background(), "d", "abc1234", "exact.md", g.run)
	if err != nil || trunc || len(got) != baseCommitFileCap {
		t.Fatalf("exact cap: len=%d trunc=%v err=%v", len(got), trunc, err)
	}
	got, trunc, err = readFileAtCommit(context.Background(), "d", "abc1234", "big.md", g.run)
	if err != nil || !trunc || len(got) != baseCommitFileCap {
		t.Fatalf("over cap: len=%d trunc=%v err=%v", len(got), trunc, err)
	}
	got, trunc, err = readFileAtCommit(context.Background(), "d", "abc1234", "utf.md", g.run)
	if err != nil || !trunc || len(got) != baseCommitFileCap-1 || strings.ContainsRune(got, 'é') {
		t.Fatalf("utf boundary: len=%d trunc=%v err=%v", len(got), trunc, err)
	}
}

func TestBaseCommitReadFileAtCommitRejectsPathEscape(t *testing.T) {
	g := newFakeBaseGit()
	for _, p := range []string{"../secret", "a/../../secret", "/etc/passwd", "..", "", ".", "  "} {
		_, _, err := readFileAtCommit(context.Background(), "d", "abc1234", p, g.run)
		if !errors.Is(err, ErrPathEscapesRepo) {
			t.Fatalf("path %q: err = %v, want ErrPathEscapesRepo", p, err)
		}
	}
	for _, sha := range []string{"", "-p", "a b", "abc:def"} {
		if _, _, err := readFileAtCommit(context.Background(), "d", sha, "x.md", g.run); err == nil {
			t.Fatalf("sha %q accepted", sha)
		}
	}
	if len(g.calls) != 0 {
		t.Fatalf("git ran for a rejected request: %v", g.calls)
	}
}

func TestBaseCommitReadFileAtCommitDirectoryIsNotAFile(t *testing.T) {
	g := newFakeBaseGit()
	g.files["abc1234:docs"] = "tree abc1234:docs\n\nrules.md\n"
	_, _, err := readFileAtCommit(context.Background(), "d", "abc1234", "docs", g.run)
	if err == nil || errors.Is(err, ErrPathMissingAtCommit) {
		t.Fatalf("err = %v, want a not-a-file failure", err)
	}
}

func TestBaseCommitReadRulesFilesAtBaseLeavesOutMissing(t *testing.T) {
	g := newFakeBaseGit()
	g.good("base111")
	g.files["base111:AGENT.md"] = "base rules"
	m := baseCommitManager(t, g)
	prd := baseCommitPRD(baseCommitTask("t1", "base111", "", tp(time.Unix(1, 0))))

	files, sha, how, err := m.readRulesFilesAtBase(context.Background(), prd)
	if err != nil {
		t.Fatalf("readRulesFilesAtBase: %v", err)
	}
	if sha != "base111" || how != "first_task_base_sha" {
		t.Fatalf("got (%s, %s)", sha, how)
	}
	if len(files) != 1 || files[0].Path != "AGENT.md" || files[0].Content != "base rules" || files[0].Truncated {
		t.Fatalf("files = %+v, want only AGENT.md (CONTEXT.md is missing at the base)", files)
	}

	// Both exist: both come back, rules first.
	g.files["base111:CONTEXT.md"] = "base context"
	files, _, _, err = m.readRulesFilesAtBase(context.Background(), prd)
	if err != nil || len(files) != 2 || files[0].Path != "AGENT.md" || files[1].Path != "CONTEXT.md" {
		t.Fatalf("files = %+v, err = %v", files, err)
	}

	// Neither exists: no error, an empty slice.
	delete(g.files, "base111:AGENT.md")
	delete(g.files, "base111:CONTEXT.md")
	files, _, _, err = m.readRulesFilesAtBase(context.Background(), prd)
	if err != nil || files == nil || len(files) != 0 {
		t.Fatalf("none exist: files = %#v, err = %v", files, err)
	}
}

func TestBaseCommitReadRulesFilesAtBaseHonoursConfiguredNames(t *testing.T) {
	g := newFakeBaseGit()
	g.good("base111")
	g.files["base111:docs/RULES.md"] = "custom rules"
	g.files["base111:docs/STATE.md"] = "custom state"
	m := baseCommitManager(t, g)
	m.mu.Lock()
	m.cfg.RulesFile = "docs/RULES.md"
	m.cfg.ContextFile = "docs/STATE.md"
	m.mu.Unlock()
	prd := baseCommitPRD(baseCommitTask("t1", "base111", "", tp(time.Unix(1, 0))))

	files, _, _, err := m.readRulesFilesAtBase(context.Background(), prd)
	if err != nil || len(files) != 2 || files[0].Content != "custom rules" || files[1].Content != "custom state" {
		t.Fatalf("files = %+v, err = %v", files, err)
	}
}

func TestBaseCommitReadRulesFilesAtBasePropagatesErrors(t *testing.T) {
	g := newFakeBaseGit()
	m := baseCommitManager(t, g)
	if _, _, _, err := m.readRulesFilesAtBase(context.Background(), baseCommitPRD()); !errors.Is(err, ErrNoBaseCommit) {
		t.Fatalf("err = %v, want ErrNoBaseCommit", err)
	}

	g.good("base111")
	g.showErr = errors.New("fatal: bad object")
	prd := baseCommitPRD(baseCommitTask("t1", "base111", "", tp(time.Unix(1, 0))))
	files, _, _, err := m.readRulesFilesAtBase(context.Background(), prd)
	if err == nil || files != nil {
		t.Fatalf("files = %v, err = %v, want the git failure", files, err)
	}
}

// With real git: the working copy edited by a worker never changes what the
// base commit returns, a path absent at the base reports ErrPathMissingAtCommit,
// and the worktree-mode merge-base lands on the commit the branch left.
func TestBaseCommitRealGitIgnoresWorkingCopy(t *testing.T) {
	dir := driftTestRepo(t)
	driftWrite(t, dir, "AGENT.md", "rules at base\n")
	driftGit(t, dir, "add", "AGENT.md")
	driftGit(t, dir, "commit", "-q", "-m", "base")
	base := driftGit(t, dir, "rev-parse", "HEAD")
	mainBranch := driftGit(t, dir, "symbolic-ref", "--short", "HEAD")

	// Worker work: a new rules file, an edit committed, then another edit
	// left in the working copy.
	driftWrite(t, dir, "AGENT.md", "rules edited and committed\n")
	driftWrite(t, dir, "CONTEXT.md", "context added later\n")
	driftGit(t, dir, "add", "AGENT.md", "CONTEXT.md")
	driftGit(t, dir, "commit", "-q", "-m", "worker")
	driftWrite(t, dir, "AGENT.md", "rules edited in the working copy\n")

	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd := &PRD{ID: "real", ProjectDir: dir, Story: []Story{{ID: "s", Tasks: []Task{
		{ID: "t1", BaseSHA: base, StartedAt: tp(time.Unix(1, 0))},
	}}}}

	files, sha, how, err := m.readRulesFilesAtBase(context.Background(), prd)
	if err != nil {
		t.Fatalf("readRulesFilesAtBase: %v", err)
	}
	if sha != base || how != "first_task_base_sha" {
		t.Fatalf("got (%s, %s), want (%s, first_task_base_sha)", sha, how, base)
	}
	if len(files) != 1 || files[0].Path != "AGENT.md" || files[0].Content != "rules at base\n" {
		t.Fatalf("files = %+v, want only the base AGENT.md", files)
	}

	if _, _, err := readFileAtCommit(context.Background(), dir, base, "CONTEXT.md", nil); !errors.Is(err, ErrPathMissingAtCommit) {
		t.Fatalf("real git missing path: err = %v, want ErrPathMissingAtCommit", err)
	}

	// Worktree mode: a branch cut from base, with commits of its own.
	driftGit(t, dir, "checkout", "-q", "--", "AGENT.md") // drop the working-copy edit
	driftGit(t, dir, "checkout", "-q", "-b", "feature", base)
	driftWrite(t, dir, "feature.txt", "x\n")
	driftGit(t, dir, "add", "feature.txt")
	driftGit(t, dir, "commit", "-q", "-m", "feature work")
	wt := &PRD{ID: "real2", ProjectDir: dir, Git: GitWorkflow{Branch: "feature", BaseBranch: mainBranch}}
	sha, how, err = m.resolveBaseCommit(context.Background(), wt, nil)
	if err != nil || sha != base || how != "merge_base" {
		t.Fatalf("worktree merge-base: got (%s, %s, %v), want (%s, merge_base)", sha, how, err, base)
	}

	// A commit that is not an ancestor of HEAD is refused by real git too.
	driftGit(t, dir, "checkout", "-q", mainBranch)
	other := driftGit(t, dir, "rev-parse", "feature")
	prd2 := &PRD{ID: "real3", ProjectDir: dir, Story: []Story{{ID: "s", Tasks: []Task{
		{ID: "t1", BaseSHA: other, StartedAt: tp(time.Unix(1, 0))},
	}}}}
	if _, _, err := m.resolveBaseCommit(context.Background(), prd2, nil); !errors.Is(err, ErrNoBaseCommit) {
		t.Fatalf("non-ancestor: err = %v, want ErrNoBaseCommit", err)
	}
}
