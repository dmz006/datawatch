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

// ── helpers ────────────────────────────────────────────────────────────

func mustGit(t *testing.T, cwd string, args ...string) {
	t.Helper()
	if err := runGit(context.Background(), cwd, args...); err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
}

