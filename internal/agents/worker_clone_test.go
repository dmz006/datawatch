// F10 sprint 5 (S5.3) — worker clone helper + URL/repo extraction tests.

package agents

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepoFromGitURL(t *testing.T) {
	cases := map[string]string{
		"https://github.com/owner/repo":     "owner/repo",
		"https://github.com/owner/repo.git": "owner/repo",
		"git@github.com:owner/repo.git":     "owner/repo",
		"git@gitlab.com:group/proj":         "group/proj",
		"plain-noslash-noatcolon":           "plain-noslash-noatcolon",
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			if got := repoFromGitURL(in); got != want {
				t.Errorf("repoFromGitURL(%q)=%q want %q", in, got, want)
			}
		})
	}
}

func TestRepoNameFromURL(t *testing.T) {
	cases := map[string]string{
		"https://github.com/owner/repo":     "repo",
		"https://github.com/owner/repo.git": "repo",
		"git@github.com:owner/myproject.git": "myproject",
		"":   "repo",
		"/":  "repo",
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			if got := repoNameFromURL(in); got != want {
				t.Errorf("repoNameFromURL(%q)=%q want %q", in, got, want)
			}
		})
	}
}

func TestInjectTokenIntoURL(t *testing.T) {
	cases := []struct {
		name  string
		in    string
		token string
		want  string
	}{
		{"no token leaves URL unchanged", "https://github.com/x/y", "", "https://github.com/x/y"},
		{"https URL gets basic auth", "https://github.com/x/y", "tok", "https://x-access-token:tok@github.com/x/y"},
		{"http URL also accepted (CI/dev)", "http://gitea/x/y", "tok", "http://x-access-token:tok@gitea/x/y"},
		{"non-http URL untouched", "git@github.com:x/y.git", "tok", "git@github.com:x/y.git"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := injectTokenIntoURL(c.in, c.token); got != c.want {
				t.Errorf("got=%q want=%q", got, c.want)
			}
		})
	}
}

// CloneOnBootstrap is a no-op when the response has no Git URL.
func TestCloneOnBootstrap_NoGitNoOp(t *testing.T) {
	dir := t.TempDir()
	path, err := CloneOnBootstrap(context.Background(), &BootstrapResponse{}, dir)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if path != "" {
		t.Errorf("path=%q want empty", path)
	}
	// nil resp also a no-op (safety).
	if path, err := CloneOnBootstrap(context.Background(), nil, dir); err != nil || path != "" {
		t.Errorf("nil resp: path=%q err=%v want empty", path, err)
	}
}

// CloneOnBootstrap exercises the full real-git path against a local
// bare repo on disk. Skipped when git isn't installed on the runner.
func TestCloneOnBootstrap_LocalGitRoundTrip(t *testing.T) {
	if err := runGit(context.Background(), "", "--version"); err != nil {
		t.Skip("git not installed; skipping clone roundtrip")
	}

	root := t.TempDir()
	srcRepo := filepath.Join(root, "src-repo")
	if err := os.MkdirAll(srcRepo, 0755); err != nil {
		t.Fatal(err)
	}
	// Init a tiny upstream repo with one commit on main.
	mustGit(t, srcRepo, "init", "-b", "main")
	mustGit(t, srcRepo, "config", "user.email", "smoke@test")
	mustGit(t, srcRepo, "config", "user.name", "Smoke")
	if err := os.WriteFile(filepath.Join(srcRepo, "README.md"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, srcRepo, "add", "README.md")
	mustGit(t, srcRepo, "commit", "-m", "init")

	workspace := filepath.Join(root, "workspace")
	resp := &BootstrapResponse{Git: BootstrapGit{URL: srcRepo}}
	target, err := CloneOnBootstrap(context.Background(), resp, workspace)
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	if !strings.HasPrefix(target, workspace) {
		t.Errorf("target=%q not under workspace=%q", target, workspace)
	}
	if _, err := os.Stat(filepath.Join(target, "README.md")); err != nil {
		t.Errorf("expected README.md in clone: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, ".git")); err != nil {
		t.Errorf("expected .git directory: %v", err)
	}

	// Second invocation should run `git pull` instead of re-cloning.
	target2, err := CloneOnBootstrap(context.Background(), resp, workspace)
	if err != nil {
		t.Errorf("re-invocation should succeed: %v", err)
	}
	if target2 != target {
		t.Errorf("target2=%q want %q", target2, target)
	}
}

// BL407 Phase 4 — CreateBranch=true clones the repo's own default
// branch (never --branch <not-yet-existing-name>, which would fail
// outright) then creates the new branch locally.
func TestCloneOnBootstrap_CreateBranchFresh(t *testing.T) {
	if err := runGit(context.Background(), "", "--version"); err != nil {
		t.Skip("git not installed; skipping clone roundtrip")
	}
	root := t.TempDir()
	srcRepo := filepath.Join(root, "src-repo")
	if err := os.MkdirAll(srcRepo, 0755); err != nil {
		t.Fatal(err)
	}
	mustGit(t, srcRepo, "init", "-b", "main")
	mustGit(t, srcRepo, "config", "user.email", "smoke@test")
	mustGit(t, srcRepo, "config", "user.name", "Smoke")
	if err := os.WriteFile(filepath.Join(srcRepo, "README.md"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, srcRepo, "add", "README.md")
	mustGit(t, srcRepo, "commit", "-m", "init")

	workspace := filepath.Join(root, "workspace")
	resp := &BootstrapResponse{Git: BootstrapGit{URL: srcRepo, Branch: "automaton/abc12345-fixture", CreateBranch: true}}
	target, err := CloneOnBootstrap(context.Background(), resp, workspace)
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	cur, err := gitOutputText(context.Background(), target, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}
	if cur != "automaton/abc12345-fixture" {
		t.Errorf("current branch=%q want automaton/abc12345-fixture", cur)
	}
	if _, err := os.Stat(filepath.Join(target, "README.md")); err != nil {
		t.Errorf("expected README.md in clone: %v", err)
	}

	// Simulated worker restart (same target dir, same CreateBranch
	// request): must land back on that same branch, not re-create a
	// duplicate or error out on "branch already exists".
	target2, err := CloneOnBootstrap(context.Background(), resp, workspace)
	if err != nil {
		t.Fatalf("resumed clone: %v", err)
	}
	if target2 != target {
		t.Errorf("target2=%q want %q", target2, target)
	}
	cur2, err := gitOutputText(context.Background(), target2, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		t.Fatalf("rev-parse HEAD (resumed): %v", err)
	}
	if cur2 != "automaton/abc12345-fixture" {
		t.Errorf("resumed current branch=%q want automaton/abc12345-fixture", cur2)
	}
}

// PushOnCompletion is a no-op (no error, empty branch/sha) whenever
// there's nothing to push back to — no dir, nil resp, or a resp with
// no Git.URL (worker was never git-bootstrapped).
func TestPushOnCompletion_NoGitNoOp(t *testing.T) {
	if branch, sha, err := PushOnCompletion(context.Background(), "", &BootstrapResponse{Git: BootstrapGit{URL: "https://x/y"}}); err != nil || branch != "" || sha != "" {
		t.Errorf("empty dir: branch=%q sha=%q err=%v want all empty/nil", branch, sha, err)
	}
	if branch, sha, err := PushOnCompletion(context.Background(), t.TempDir(), nil); err != nil || branch != "" || sha != "" {
		t.Errorf("nil resp: branch=%q sha=%q err=%v want all empty/nil", branch, sha, err)
	}
	if branch, sha, err := PushOnCompletion(context.Background(), t.TempDir(), &BootstrapResponse{}); err != nil || branch != "" || sha != "" {
		t.Errorf("empty Git.URL: branch=%q sha=%q err=%v want all empty/nil", branch, sha, err)
	}
}

// PushOnCompletion exercises the full real-git path: clone from a bare
// remote (CloneOnBootstrap), commit new work in the clone, push it
// back, then confirm the bare remote actually received it — not just
// that the local push command returned success.
func TestPushOnCompletion_LocalGitRoundTrip(t *testing.T) {
	if err := runGit(context.Background(), "", "--version"); err != nil {
		t.Skip("git not installed; skipping push roundtrip")
	}

	root := t.TempDir()
	bareRepo := filepath.Join(root, "bare.git")
	mustGit(t, "", "init", "--bare", "-b", "main", bareRepo)

	// Seed one commit on main via a throwaway working clone — a bare
	// repo has nothing to clone from until it has at least one commit.
	seed := filepath.Join(root, "seed")
	mustGit(t, "", "clone", bareRepo, seed)
	mustGit(t, seed, "config", "user.email", "smoke@test")
	mustGit(t, seed, "config", "user.name", "Smoke")
	if err := os.WriteFile(filepath.Join(seed, "README.md"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, seed, "add", "README.md")
	mustGit(t, seed, "commit", "-m", "init")
	mustGit(t, seed, "push", "origin", "main")

	workspace := filepath.Join(root, "workspace")
	resp := &BootstrapResponse{Git: BootstrapGit{URL: bareRepo}}
	target, err := CloneOnBootstrap(context.Background(), resp, workspace)
	if err != nil {
		t.Fatalf("clone: %v", err)
	}
	mustGit(t, target, "config", "user.email", "worker@test")
	mustGit(t, target, "config", "user.name", "Worker")
	if err := os.WriteFile(filepath.Join(target, "WORK.md"), []byte("done"), 0644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, target, "add", "WORK.md")
	mustGit(t, target, "commit", "-m", "task work")
	wantSHA, err := gitOutputText(context.Background(), target, "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}

	branch, sha, err := PushOnCompletion(context.Background(), target, resp)
	if err != nil {
		t.Fatalf("PushOnCompletion: %v", err)
	}
	if branch != "main" {
		t.Errorf("branch=%q want main", branch)
	}
	if sha != wantSHA {
		t.Errorf("sha=%q want %q", sha, wantSHA)
	}

	remoteSHA, err := gitOutputText(context.Background(), "", "--git-dir="+bareRepo, "rev-parse", "main")
	if err != nil {
		t.Fatalf("rev-parse main on bare repo: %v", err)
	}
	if remoteSHA != wantSHA {
		t.Errorf("bare repo main=%q want %q — push didn't actually land", remoteSHA, wantSHA)
	}
}

// ── helpers ────────────────────────────────────────────────────────────

func mustGit(t *testing.T, cwd string, args ...string) {
	t.Helper()
	if err := runGit(context.Background(), cwd, args...); err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
}

