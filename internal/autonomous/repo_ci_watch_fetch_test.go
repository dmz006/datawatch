package autonomous

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// gitIn runs git in dir with a fixed identity and no global config, so the
// test touches nothing outside its temp directories.
func fetchGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func fetchCommitFile(t *testing.T, dir, name string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o644); err != nil {
		t.Fatal(err)
	}
	fetchGit(t, dir, "add", name)
	fetchGit(t, dir, "commit", "-q", "-m", name)
	return fetchGit(t, dir, "rev-parse", "HEAD")
}

// A local tag that differs from origin's tag of the same name must not make
// the repo CI watch fetch fail (git: "would clobber existing tag"), and the
// local tag is left alone while new tags and origin/main still arrive.
func TestDefaultRepoCIFetch_LocalTagsDoNotFail(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found")
	}
	root := t.TempDir()
	origin := filepath.Join(root, "origin")
	if err := os.MkdirAll(origin, 0o755); err != nil {
		t.Fatal(err)
	}
	fetchGit(t, origin, "init", "-q", "-b", "main")
	fetchCommitFile(t, origin, "a")
	fetchGit(t, origin, "tag", "v1.0.0")

	clone := filepath.Join(root, "clone")
	fetchGit(t, root, "clone", "-q", origin, clone)
	// The clone grows a local v1.0.0 that points somewhere else than origin's.
	localSHA := fetchCommitFile(t, clone, "local-only")
	fetchGit(t, clone, "tag", "-f", "v1.0.0", localSHA)

	// Origin moves on: a new commit on main and a new tag.
	newSHA := fetchCommitFile(t, origin, "b")
	fetchGit(t, origin, "tag", "v1.0.1")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := defaultRepoCIFetch(ctx, clone); err != nil {
		t.Fatalf("fetch failed on a local tag that differs from origin: %v", err)
	}
	if got := fetchGit(t, clone, "rev-parse", "refs/remotes/origin/main"); got != newSHA {
		t.Errorf("origin/main = %s, want the new origin head %s", got, newSHA)
	}
	if got := fetchGit(t, clone, "rev-parse", "refs/tags/v1.0.1^{commit}"); got != newSHA {
		t.Errorf("new tag v1.0.1 = %s, want %s", got, newSHA)
	}
	if got := fetchGit(t, clone, "rev-parse", "refs/tags/v1.0.0^{commit}"); got != localSHA {
		t.Errorf("local tag v1.0.0 was overwritten: %s, want %s", got, localSHA)
	}
}

// A real fetch failure (no such remote) is still an error.
func TestDefaultRepoCIFetch_RealFailureStillFails(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found")
	}
	repo := t.TempDir()
	fetchGit(t, repo, "init", "-q", "-b", "main")
	fetchGit(t, repo, "remote", "add", "origin", filepath.Join(t.TempDir(), "missing"))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := defaultRepoCIFetch(ctx, repo); err == nil {
		t.Fatal("fetch from a missing remote returned nil")
	}
}
