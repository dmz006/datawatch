package git

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGenericGitProvider(t *testing.T) {
	ctx := context.Background()

	t.Run("Kind", func(t *testing.T) {
		for _, k := range []string{"git", "gitea", "forgejo"} {
			if got := NewGenericGit(k, "https://example.test").Kind(); got != k {
				t.Errorf("Kind() = %q, want %q", got, k)
			}
		}
	})

	t.Run("sentinels", func(t *testing.T) {
		g := NewGenericGit("gitea", "https://example.test")
		if _, err := g.MintToken(ctx, "o/r", time.Minute); !errors.Is(err, ErrNotImplemented) {
			t.Errorf("MintToken err = %v", err)
		}
		if err := g.RevokeToken(ctx, "t"); !errors.Is(err, ErrNotImplemented) {
			t.Errorf("RevokeToken err = %v", err)
		}
		if _, err := g.OpenPR(ctx, PROptions{}); !errors.Is(err, ErrNoForgeAPI) {
			t.Errorf("OpenPR err = %v", err)
		}
		if _, err := g.CreateIssue(ctx, IssueOptions{}); !errors.Is(err, ErrNoForgeAPI) {
			t.Errorf("CreateIssue err = %v", err)
		}
		if _, err := g.CreateRelease(ctx, ReleaseOptions{}); !errors.Is(err, ErrNoForgeAPI) {
			t.Errorf("CreateRelease err = %v", err)
		}
		if !strings.Contains(ErrNoForgeAPI.Error(), "web interface") {
			t.Errorf("message should point to the web interface: %q", ErrNoForgeAPI)
		}
	})

	t.Run("clone local bare repo", func(t *testing.T) {
		if _, err := exec.LookPath("git"); err != nil {
			t.Skip("git not installed")
		}
		root := t.TempDir()
		run := func(dir string, args ...string) {
			t.Helper()
			cmd := exec.Command("git", args...)
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v\n%s", args, err, out)
			}
		}
		bare := filepath.Join(root, "bare.git")
		seed := filepath.Join(root, "seed")
		run(root, "init", "--bare", "-b", "main", bare)
		run(root, "init", "-b", "main", seed)
		if err := os.WriteFile(filepath.Join(seed, "f.txt"), []byte("hi"), 0o600); err != nil {
			t.Fatal(err)
		}
		run(seed, "add", ".")
		run(seed, "commit", "-m", "init")

		g := NewGenericGit("git", "")
		if err := g.Push(ctx, seed, bare, "main"); err != nil {
			t.Fatalf("Push: %v", err)
		}
		dest := filepath.Join(root, "clone")
		if err := g.Clone(ctx, bare, dest); err != nil {
			t.Fatalf("Clone: %v", err)
		}
		if _, err := os.Stat(filepath.Join(dest, "f.txt")); err != nil {
			t.Errorf("cloned file missing: %v", err)
		}
		if err := g.Clone(ctx, "", dest); err == nil {
			t.Error("empty repo should error")
		}
	})
}
