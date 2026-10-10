// BL407 Phase 2 — PRD-completion push + PR (worktree mode) tests.
package autonomous

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/git"
)

func TestRepoFromGitURL(t *testing.T) {
	cases := map[string]string{
		"https://github.com/dmz006/datawatch":     "dmz006/datawatch",
		"https://github.com/dmz006/datawatch.git": "dmz006/datawatch",
		"git@github.com:dmz006/datawatch.git":     "dmz006/datawatch",
		"git@github.com:dmz006/datawatch":         "dmz006/datawatch",
	}
	for in, want := range cases {
		if got := repoFromGitURL(in); got != want {
			t.Errorf("repoFromGitURL(%q) = %q, want %q", in, got, want)
		}
	}
}

// fakeOpenPRProvider mirrors fakeIssueProvider's pattern (used for
// BL406 Phase 4's upstream-issue tests): embeds the GitLab stub for
// every method this test doesn't care about, overrides only OpenPR.
type fakeOpenPRProvider struct {
	git.GitLab
	calls []git.PROptions
	err   error
	url   string
}

func (f *fakeOpenPRProvider) OpenPR(_ context.Context, opts git.PROptions) (string, error) {
	f.calls = append(f.calls, opts)
	if f.err != nil {
		return "", f.err
	}
	if f.url != "" {
		return f.url, nil
	}
	return "https://github.com/" + opts.Repo + "/pull/1", nil
}

// gitCompletionFixture creates a bare "remote" repo, a base repo
// pointed at it as origin, and a real worktree off the base repo —
// everything handleWorktreeCompletion needs to actually push against.
func gitCompletionFixture(t *testing.T) (worktreePath, branch string, bareRemote string) {
	t.Helper()
	bareRemote = filepath.Join(t.TempDir(), "remote.git")
	if out, err := exec.Command("git", "init", "--bare", "-q", "-b", "main", bareRemote).CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, out)
	}

	baseRepo := scratchGitRepo(t)
	run := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = baseRepo
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	run("remote", "add", "origin", bareRemote)
	run("push", "-u", "origin", "main")

	prd := &PRD{ID: "abc12345", Title: "Add widgets"}
	path, br, err := EnsureWorktree(prd, baseRepo, t.TempDir())
	if err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}
	return path, br, bareRemote
}

func TestHandleWorktreeCompletion_NoOpWhenNotWorktreeMode(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeOpenPRProvider{}
	m.gitProviderFn = func(string) git.Provider { return fake }

	prd := &PRD{ID: "abc12345", Git: GitWorkflow{AutoPR: true}} // Branch empty
	m.handleWorktreeCompletion(prd)
	if len(fake.calls) != 0 {
		t.Error("OpenPR must not be called when Git.Branch is empty")
	}
}

func TestHandleWorktreeCompletion_NoOpWhenAutoPRFalse(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeOpenPRProvider{}
	m.gitProviderFn = func(string) git.Provider { return fake }

	prd := &PRD{ID: "abc12345", Git: GitWorkflow{Branch: "automaton/abc12345"}} // AutoPR false
	m.handleWorktreeCompletion(prd)
	if len(fake.calls) != 0 {
		t.Error("OpenPR must not be called when Git.AutoPR is false")
	}
}

func TestHandleWorktreeCompletion_PushesOpensPRAndRemovesWorktree(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir, DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeOpenPRProvider{url: "https://github.com/x/y/pull/42"}
	m.gitProviderFn = func(string) git.Provider { return fake }

	worktreePath, branch, bareRemote := gitCompletionFixture(t)
	prd, err := m.CreatePRD("spec text", worktreePath, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	prd.Git.Branch = branch
	prd.Git.AutoPR = true
	prd.Title = "Add widgets"
	_ = m.Store().SavePRD(prd)

	m.handleWorktreeCompletion(prd)

	if len(fake.calls) != 1 {
		t.Fatalf("expected exactly 1 OpenPR call, got %d: %+v", len(fake.calls), fake.calls)
	}
	got := fake.calls[0]
	if got.HeadBranch != branch {
		t.Errorf("HeadBranch = %q, want %q", got.HeadBranch, branch)
	}
	if got.Title != "Add widgets" {
		t.Errorf("Title = %q, want %q", got.Title, "Add widgets")
	}
	if !strings.Contains(got.Body, prd.ID) {
		t.Errorf("Body missing prd_id: %q", got.Body)
	}

	updated, _ := m.Store().GetPRD(prd.ID)
	if updated.Git.PRURL != "https://github.com/x/y/pull/42" {
		t.Errorf("Git.PRURL = %q, want the fake's URL", updated.Git.PRURL)
	}
	found := false
	for _, d := range updated.Decisions {
		if d.Kind == "git_pr_opened" {
			found = true
		}
	}
	if !found {
		t.Error("expected a git_pr_opened Decision to be recorded")
	}

	// The branch must actually exist on the remote now.
	out, err := exec.Command("git", "-C", bareRemote, "branch", "--list", branch).CombinedOutput()
	if err != nil || !strings.Contains(string(out), branch) {
		t.Errorf("branch %q not found on remote after push: %v\n%s", branch, err, out)
	}

	// The worktree directory must be gone.
	if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
		t.Errorf("worktree at %s still exists after successful push+PR", worktreePath)
	}
}

func TestHandleWorktreeCompletion_PushFailureKeepsWorktreeAndSkipsOpenPR(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir, DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeOpenPRProvider{}
	m.gitProviderFn = func(string) git.Provider { return fake }

	// A worktree whose base repo has no remote at all -- push must fail.
	baseRepo := scratchGitRepo(t)
	prdForWT := &PRD{ID: "def67890", Title: "x"}
	worktreePath, branch, err := EnsureWorktree(prdForWT, baseRepo, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	prd, err := m.CreatePRD("spec", worktreePath, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	prd.Git.Branch = branch
	prd.Git.AutoPR = true
	_ = m.Store().SavePRD(prd)

	m.handleWorktreeCompletion(prd)

	if len(fake.calls) != 0 {
		t.Error("OpenPR must not be called when push fails")
	}
	if _, serr := os.Stat(worktreePath); serr != nil {
		t.Errorf("worktree must survive a push failure, for operator inspection: %v", serr)
	}
	updated, _ := m.Store().GetPRD(prd.ID)
	found := false
	for _, d := range updated.Decisions {
		if d.Kind == "git_pr_failed" {
			found = true
		}
	}
	if !found {
		t.Error("expected a git_pr_failed Decision to be recorded")
	}
}

func TestHandleWorktreeCompletion_OpenPRFailureKeepsWorktree(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir, DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeOpenPRProvider{err: context.DeadlineExceeded}
	m.gitProviderFn = func(string) git.Provider { return fake }

	worktreePath, branch, _ := gitCompletionFixture(t)
	prd, err := m.CreatePRD("spec", worktreePath, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	prd.Git.Branch = branch
	prd.Git.AutoPR = true
	_ = m.Store().SavePRD(prd)

	m.handleWorktreeCompletion(prd)

	if len(fake.calls) != 1 {
		t.Fatalf("expected OpenPR to be attempted once, got %d", len(fake.calls))
	}
	if _, serr := os.Stat(worktreePath); serr != nil {
		t.Errorf("worktree must survive an OpenPR failure: %v", serr)
	}
	updated, _ := m.Store().GetPRD(prd.ID)
	if updated.Git.PRURL != "" {
		t.Errorf("Git.PRURL must stay empty on OpenPR failure, got %q", updated.Git.PRURL)
	}
	found := false
	for _, d := range updated.Decisions {
		if d.Kind == "git_pr_failed" {
			found = true
		}
	}
	if !found {
		t.Error("expected a git_pr_failed Decision to be recorded")
	}
}
