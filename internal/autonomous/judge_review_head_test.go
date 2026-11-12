package autonomous

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// reviewHeadPRD builds a PRD whose latest recorded attempt commit is attempt.
func reviewHeadPRD(dir, attempt string) *PRD {
	prd := &PRD{ProjectDir: dir}
	if attempt != "" {
		prd.Story = []Story{{ID: "s1", Tasks: []Task{{ID: "t1", AttemptCommitSHAs: []string{"older", attempt}}}}}
	}
	return prd
}

func TestReviewHeadUsesTrimmedGitOutput(t *testing.T) {
	var gotDir string
	var gotArgs []string
	m := &Manager{driftGit: func(ctx context.Context, dir string, args ...string) (string, error) {
		gotDir, gotArgs = dir, args
		return "  0123456789abcdef0123456789abcdef01234567\n", nil
	}}
	prd := reviewHeadPRD("work-dir", "attemptsha")
	got := m.currentReviewHead(context.Background(), prd)
	if got != "0123456789abcdef0123456789abcdef01234567" {
		t.Fatalf("head = %q, want trimmed git output", got)
	}
	if gotDir != "work-dir" || strings.Join(gotArgs, " ") != "rev-parse HEAD" {
		t.Fatalf("runner called with dir=%q args=%v", gotDir, gotArgs)
	}
}

func TestReviewHeadRunnerErrorFallsBackToAttemptCommit(t *testing.T) {
	m := &Manager{driftGit: func(ctx context.Context, dir string, args ...string) (string, error) {
		return "ignored\n", errors.New("not a git repository")
	}}
	got := m.currentReviewHead(context.Background(), reviewHeadPRD("work-dir", "attemptsha"))
	if got != "attemptsha" {
		t.Fatalf("head = %q, want attempt commit", got)
	}
}

func TestReviewHeadEmptyOutputFallsBackToAttemptCommit(t *testing.T) {
	m := &Manager{driftGit: func(ctx context.Context, dir string, args ...string) (string, error) {
		return " \n", nil
	}}
	got := m.currentReviewHead(context.Background(), reviewHeadPRD("work-dir", "attemptsha"))
	if got != "attemptsha" {
		t.Fatalf("head = %q, want attempt commit", got)
	}
	if got := m.currentReviewHead(context.Background(), reviewHeadPRD("work-dir", "")); got != "" {
		t.Fatalf("head = %q, want empty when no attempt commit exists", got)
	}
}

func TestReviewHeadEmptyProjectDirFallsBackToAttemptCommit(t *testing.T) {
	called := false
	m := &Manager{driftGit: func(ctx context.Context, dir string, args ...string) (string, error) {
		called = true
		return "gitsha\n", nil
	}}
	got := m.currentReviewHead(context.Background(), reviewHeadPRD("", "attemptsha"))
	if got != "attemptsha" {
		t.Fatalf("head = %q, want attempt commit", got)
	}
	if called {
		t.Fatal("runner must not be called when ProjectDir is empty")
	}
}

func TestReviewHeadNilPRDReturnsEmpty(t *testing.T) {
	m := &Manager{driftGit: func(ctx context.Context, dir string, args ...string) (string, error) {
		t.Fatal("runner must not be called for a nil prd")
		return "", nil
	}}
	if got := m.currentReviewHead(context.Background(), nil); got != "" {
		t.Fatalf("head = %q, want empty", got)
	}
}

func TestReviewHeadDoesNotChangePRD(t *testing.T) {
	m := &Manager{driftGit: func(ctx context.Context, dir string, args ...string) (string, error) {
		return "gitsha\n", nil
	}}
	prd := reviewHeadPRD("work-dir", "attemptsha")
	m.currentReviewHead(context.Background(), prd)
	if prd.ProjectDir != "work-dir" || len(prd.Story) != 1 || len(prd.Story[0].Tasks[0].AttemptCommitSHAs) != 2 {
		t.Fatalf("prd was changed: %+v", prd)
	}
}

// unsetEnvForTest removes key for the test and restores it on cleanup.
func unsetEnvForTest(t *testing.T, key string) {
	t.Helper()
	old, had := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("unset %s: %v", key, err)
	}
	t.Cleanup(func() {
		if had {
			_ = os.Setenv(key, old)
		}
	})
}

func TestReviewHeadRealRepositoryDefaultRunner(t *testing.T) {
	unsetEnvForTest(t, "TMUX")
	unsetEnvForTest(t, "TMUX_TMPDIR")
	t.Setenv("GIT_AUTHOR_NAME", "Review Head")
	t.Setenv("GIT_AUTHOR_EMAIL", "review-head@example.invalid")
	t.Setenv("GIT_COMMITTER_NAME", "Review Head")
	t.Setenv("GIT_COMMITTER_EMAIL", "review-head@example.invalid")

	dir := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init")
	commit := func(name string) string {
		t.Helper()
		if err := os.WriteFile(dir+"/"+name, []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
		git("add", name)
		git("-c", "commit.gpgsign=false", "commit", "-m", name)
		return git("rev-parse", "HEAD")
	}

	m := &Manager{} // no injected runner: the default runner is used
	prd := reviewHeadPRD(dir, "attemptsha")

	first := commit("one.txt")
	if got := m.currentReviewHead(context.Background(), prd); got != first {
		t.Fatalf("head = %q, want first commit %q", got, first)
	}
	second := commit("two.txt")
	if second == first {
		t.Fatal("second commit has the same id as the first")
	}
	if got := m.currentReviewHead(context.Background(), prd); got != second {
		t.Fatalf("head = %q, want second commit %q", got, second)
	}
}
