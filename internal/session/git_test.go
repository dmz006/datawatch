// F10 sprint 5 (S5.4) — ProjectGit helpers (push, branch, token URL).

package session

import (
	"fmt"
	"os"
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

// ── CommitSessionWork ──────────────────────────────

// newSessionWorkTestRepo creates a fresh git repo in a temp dir with
// one initial commit and a ProjectGit bound to it.
func newSessionWorkTestRepo(t *testing.T) (string, *ProjectGit) {
	t.Helper()
	dir := t.TempDir()
	g := NewProjectGit(dir)
	mustGit(t, dir, "init", "-b", "main")
	mustGit(t, dir, "config", "user.email", "t@x")
	mustGit(t, dir, "config", "user.name", "T")
	mustGit(t, dir, "commit", "--allow-empty", "-m", "init")
	return dir, g
}

func headCount(t *testing.T, dir string) int {
	t.Helper()
	n, err := gitOutput(dir, "rev-list", "--count", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	var c int
	if _, err := fmt.Sscanf(strings.TrimSpace(n), "%d", &c); err != nil {
		t.Fatalf("parse count %q: %v", n, err)
	}
	return c
}

func headSubject(t *testing.T, dir string) string {
	t.Helper()
	s, err := gitOutput(dir, "log", "-1", "--format=%s")
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(s)
}

func TestCommitSessionWork_DirtyTree(t *testing.T) {
	if !haveGit(t) {
		t.Skip("git not on PATH")
	}
	dir, g := newSessionWorkTestRepo(t)
	summary := "wrapped up the session work"
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := headCount(t, dir)
	sha, err := g.CommitSessionWork("sess1", "my task", summary)
	if err != nil {
		t.Fatalf("CommitSessionWork: %v", err)
	}
	if got := headCount(t, dir); got != before+1 {
		t.Errorf("commits=%d want %d (exactly one new commit)", got, before+1)
	}
	head, err := gitOutput(dir, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if sha != strings.TrimSpace(head) {
		t.Errorf("sha=%q want HEAD %q", sha, strings.TrimSpace(head))
	}
	if got := headSubject(t, dir); got != summary {
		t.Errorf("subject=%q want %q", got, summary)
	}
}

func TestCommitSessionWork_CleanTree(t *testing.T) {
	if !haveGit(t) {
		t.Skip("git not on PATH")
	}
	dir, g := newSessionWorkTestRepo(t)
	before := headCount(t, dir)
	sha, err := g.CommitSessionWork("sess1", "my task", "summary")
	if err != nil {
		t.Fatalf("CommitSessionWork: %v", err)
	}
	if sha != "" {
		t.Errorf("sha=%q want empty for clean tree", sha)
	}
	if got := headCount(t, dir); got != before {
		t.Errorf("commits=%d want %d (no new commit)", got, before)
	}
}

func TestCommitSessionWork_EmptySummaryFallsBackToTask(t *testing.T) {
	if !haveGit(t) {
		t.Skip("git not on PATH")
	}
	dir, g := newSessionWorkTestRepo(t)
	task := "refactor the login flow"
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := g.CommitSessionWork("sess1", task, "   \n  "); err != nil {
		t.Fatalf("CommitSessionWork: %v", err)
	}
	if got := headSubject(t, dir); !strings.Contains(got, task) {
		t.Errorf("subject=%q want it to contain task text %q", got, task)
	}
}

func TestCommitSessionWork_MultilineSummaryUsesFirstLine(t *testing.T) {
	if !haveGit(t) {
		t.Skip("git not on PATH")
	}
	dir, g := newSessionWorkTestRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	firstLine := "first line only"
	if _, err := g.CommitSessionWork("sess1", "task", firstLine+"\nsecond line\nthird line"); err != nil {
		t.Fatalf("CommitSessionWork: %v", err)
	}
	if got := headSubject(t, dir); got != firstLine {
		t.Errorf("subject=%q want %q", got, firstLine)
	}
}

func TestCommitSessionWork_UntrackedFileCommitted(t *testing.T) {
	if !haveGit(t) {
		t.Skip("git not on PATH")
	}
	dir, g := newSessionWorkTestRepo(t)
	path := filepath.Join(dir, "fresh", "brand_new.go")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Confirm it really is untracked (no index entry).
	out, err := gitOutput(dir, "ls-files")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "brand_new.go") {
		t.Fatalf("setup failed: file listed as tracked: %s", out)
	}
	if _, err := g.CommitSessionWork("sess1", "task", "added fresh file"); err != nil {
		t.Fatalf("CommitSessionWork: %v", err)
	}
	out, err = gitOutput(dir, "ls-files")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "brand_new.go") {
		t.Errorf("untracked file not committed: tracked=%q", out)
	}
}

func TestBuildCommitMessage(t *testing.T) {
	// First line wins, trimmed.
	if got := buildCommitMessage("  line one\nline two\n", "task"); got != "line one" {
		t.Errorf("got %q", got)
	}
	// Capped at 200 chars.
	long := strings.Repeat("a", 500)
	if got := buildCommitMessage(long, "task"); len(got) != 200 {
		t.Errorf("len=%d want 200", len(got))
	}
	// Fallback to task.
	if got := buildCommitMessage("", "some task"); got != "task: some task" {
		t.Errorf("got %q", got)
	}
	// Task truncated to 60 chars (+ "..." per truncateStr).
	if got := buildCommitMessage("", strings.Repeat("b", 100)); len(got) != len("task: ")+63 {
		t.Errorf("got %q (len %d)", got, len(got))
	}
	// Never empty.
	if got := buildCommitMessage("", ""); got == "" {
		t.Error("must never be empty")
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
