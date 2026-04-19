// F10 sprint 5 (S5.4) — ProjectGit helpers (push, branch, token URL).

package session

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestInjectTokenIntoHTTPS(t *testing.T) {
	cases := []struct {
		name  string
		in    string
		token string
		want  string
	}{
		{"https + token", "https://github.com/o/r.git", "tok", "https://x-access-token:tok@github.com/o/r.git"},
		{"http + token", "http://gitea/o/r", "tok", "http://x-access-token:tok@gitea/o/r"},
		{"empty token leaves untouched", "https://github.com/o/r", "", "https://github.com/o/r"},
		{"non-http URL untouched", "git@github.com:o/r.git", "tok", "git@github.com:o/r.git"},
		{"already-credentialed URL untouched", "https://other:pw@host/o/r", "tok", "https://other:pw@host/o/r"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := injectTokenIntoHTTPS(c.in, c.token); got != c.want {
				t.Errorf("got=%q want=%q", got, c.want)
			}
		})
	}
}

// CurrentBranch round-trip against a real git repo on disk.
func TestProjectGit_CurrentBranch(t *testing.T) {
	if !haveGit(t) {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	g := NewProjectGit(dir)
	mustGit(t, dir, "init", "-b", "feat/abc")
	mustGit(t, dir, "config", "user.email", "t@x")
	mustGit(t, dir, "config", "user.name", "T")
	// rev-parse HEAD requires at least one commit.
	mustGit(t, dir, "commit", "--allow-empty", "-m", "init")
	got, err := g.CurrentBranch()
	if err != nil {
		t.Fatal(err)
	}
	if got != "feat/abc" {
		t.Errorf("branch=%q want feat/abc", got)
	}
}

// PushBranch writes the credential URL only for the duration of the
// push, then restores the canonical origin so the token doesn't
// persist in .git/config. We exercise this with a local bare repo
// as the "remote" — no network.
func TestProjectGit_PushBranch_RestoresOrigin(t *testing.T) {
	if !haveGit(t) {
		t.Skip("git not on PATH")
	}
	root := t.TempDir()
	bareRemote := filepath.Join(root, "remote.git")
	mustGit(t, root, "init", "--bare", bareRemote)

	work := filepath.Join(root, "work")
	mustGit(t, root, "init", "-b", "main", work)
	mustGit(t, work, "config", "user.email", "t@x")
	mustGit(t, work, "config", "user.name", "T")
	mustGit(t, work, "commit", "--allow-empty", "-m", "init")

	// Set origin to the bare remote (no token; it's a local file).
	mustGit(t, work, "remote", "add", "origin", bareRemote)

	g := NewProjectGit(work)
	// Empty token + originURL == ambient creds path (no rewrite).
	if err := g.PushBranch("origin", "main", "", ""); err != nil {
		t.Fatalf("PushBranch (no token): %v", err)
	}

	// Re-push with a fake token + URL — the helper should
	// temporarily set origin to authURL, push, then restore.
	if err := g.PushBranch("origin", "main", bareRemote, "fake-token"); err != nil {
		t.Fatalf("PushBranch (token): %v", err)
	}
	out, err := gitOutput(work, "remote", "get-url", "origin")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != bareRemote {
		t.Errorf("origin not restored: got %q want %q", strings.TrimSpace(out), bareRemote)
	}
	if strings.Contains(out, "fake-token") {
		t.Errorf(".git/config still contains the token: %s", out)
	}
}

// PushBranch validates required args.
func TestProjectGit_PushBranch_ArgValidation(t *testing.T) {
	g := NewProjectGit(t.TempDir())
	if err := g.PushBranch("origin", "", "https://x/y", "tok"); err == nil {
		t.Error("expected error for empty branch")
	}
}

// ── helpers ────────────────────────────────────────────────────────────

func haveGit(t *testing.T) bool {
	t.Helper()
	return runGit(t.TempDir(), "--version") == nil
}

func mustGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	if err := runGit(dir, args...); err != nil {
		t.Fatalf("git %v in %s: %v", args, dir, err)
	}
}
