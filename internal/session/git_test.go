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
	sha, err := g.CommitSessionWork("sess1", "my task", summary, nil)
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
	sha, err := g.CommitSessionWork("sess1", "my task", "summary", nil)
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
	if _, err := g.CommitSessionWork("sess1", task, "   \n  ", nil); err != nil {
		t.Fatalf("CommitSessionWork: %v", err)
	}
	if got := headSubject(t, dir); got != "session work (sess1)" {
		t.Errorf("subject=%q want %q", got, "session work (sess1)")
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
	if _, err := g.CommitSessionWork("sess1", "task", firstLine+"\nsecond line\nthird line", nil); err != nil {
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
	if _, err := g.CommitSessionWork("sess1", "task", "added fresh file", nil); err != nil {
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
	if got := buildCommitMessage("sess1", "  line one\nline two\n", "task"); got != "line one" {
		t.Errorf("got %q", got)
	}
	// Capped at 200 chars.
	long := strings.Repeat("a", 500)
	if got := buildCommitMessage("sess1", long, "task"); len(got) != 200 {
		t.Errorf("len=%d want 200", len(got))
	}
	// Fallback is "session work" with the session ID, never the task.
	if got := buildCommitMessage("sess1", "", "some task"); got != "session work (sess1)" {
		t.Errorf("got %q", got)
	}
	// No session ID: bare fallback.
	if got := buildCommitMessage("", "", "some task"); got != "session work" {
		t.Errorf("got %q", got)
	}
	// A prompt-shaped task never leaks into the fallback subject.
	prompt := "You are running non-interactively\nFix the parser."
	if got := buildCommitMessage("sess1", "", prompt); strings.Contains(got, "non-interactively") || strings.Contains(got, "Fix the parser") {
		t.Errorf("fallback leaked prompt text: %q", got)
	}
	// A summary that is itself prompt text is replaced by the safety net.
	if got := buildCommitMessage("sess1", "You are running non-interactively now", "task"); strings.Contains(got, "non-interactively") {
		t.Errorf("prompt-shaped summary used as subject: %q", got)
	}
	// Never empty.
	if got := buildCommitMessage("", "", ""); got == "" {
		t.Error("must never be empty")
	}
}

// ── CommitTaskWork ─────────────────────────────────

// headMessage returns the full message of HEAD, trimmed.
func headMessage(t *testing.T, dir string) string {
	t.Helper()
	m, err := gitOutput(dir, "log", "-1", "--format=%B")
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(m)
}

func TestCommitTaskWork_SubjectNamedAfterTaskTitle(t *testing.T) {
	if !haveGit(t) {
		t.Skip("git not on PATH")
	}
	dir, g := newSessionWorkTestRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	label := TaskCommitLabel{
		PRDID:        "abcdef0123456789",
		TaskTitle:    "Add login form",
		Prompt:       "You are running non-interactively\nAdd the login form and wire it up.",
		Verification: "passed",
	}
	sha, err := g.CommitTaskWork("sess1", label, nil)
	if err != nil {
		t.Fatalf("CommitTaskWork: %v", err)
	}
	if got := headSubject(t, dir); got != "task(abcdef01): Add login form" {
		t.Errorf("subject=%q want %q", got, "task(abcdef01): Add login form")
	}
	body := headMessage(t, dir)
	if !strings.Contains(body, "Files changed:\n- note.txt") {
		t.Errorf("body missing staged file list:\n%s", body)
	}
	if !strings.Contains(body, "Verification: passed") {
		t.Errorf("body missing verification result:\n%s", body)
	}
	if strings.Contains(body, "non-interactively") {
		t.Errorf("prompt text leaked into the commit message:\n%s", body)
	}
	head, err := gitOutput(dir, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if sha != strings.TrimSpace(head) {
		t.Errorf("sha=%q want HEAD %q", sha, strings.TrimSpace(head))
	}
}

func TestCommitTaskWork_NoPRDUsesTaskPrefix(t *testing.T) {
	if !haveGit(t) {
		t.Skip("git not on PATH")
	}
	dir, g := newSessionWorkTestRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := g.CommitTaskWork("sess1", TaskCommitLabel{TaskTitle: "Tidy imports", Prompt: "Tidy the imports"}, nil); err != nil {
		t.Fatalf("CommitTaskWork: %v", err)
	}
	if got := headSubject(t, dir); got != "task: Tidy imports" {
		t.Errorf("subject=%q want %q", got, "task: Tidy imports")
	}
}

func TestCommitTaskWork_PromptSummaryNotAppended(t *testing.T) {
	if !haveGit(t) {
		t.Skip("git not on PATH")
	}
	dir, g := newSessionWorkTestRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	prompt := "You are running non-interactively\nAdd the login form."
	label := TaskCommitLabel{PRDID: "abcdef0123456789", TaskTitle: "Add login form", Prompt: prompt, Summary: prompt}
	if _, err := g.CommitTaskWork("sess1", label, nil); err != nil {
		t.Fatalf("CommitTaskWork: %v", err)
	}
	if body := headMessage(t, dir); strings.Contains(body, "non-interactively") {
		t.Errorf("prompt-shaped summary appended to the commit message:\n%s", body)
	}
}

func TestCommitTaskWork_SummaryAppendedAsFinalLine(t *testing.T) {
	if !haveGit(t) {
		t.Skip("git not on PATH")
	}
	dir, g := newSessionWorkTestRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	const summary = "Wrote note.txt and checked it"
	label := TaskCommitLabel{PRDID: "abcdef0123456789", TaskTitle: "Write note", Prompt: "Write a note", Summary: summary}
	if _, err := g.CommitTaskWork("sess1", label, nil); err != nil {
		t.Fatalf("CommitTaskWork: %v", err)
	}
	body := headMessage(t, dir)
	lines := strings.Split(body, "\n")
	if last := lines[len(lines)-1]; last != summary {
		t.Errorf("final line=%q want %q\n%s", last, summary, body)
	}
}

func TestCommitTaskWork_CleanTreeReturnsEmpty(t *testing.T) {
	if !haveGit(t) {
		t.Skip("git not on PATH")
	}
	dir, g := newSessionWorkTestRepo(t)
	before := headCount(t, dir)
	sha, err := g.CommitTaskWork("sess1", TaskCommitLabel{PRDID: "abcdef0123456789", TaskTitle: "Nothing"}, nil)
	if err != nil {
		t.Fatalf("CommitTaskWork: %v", err)
	}
	if sha != "" {
		t.Errorf("sha=%q want empty for clean tree", sha)
	}
	if got := headCount(t, dir); got != before {
		t.Errorf("commits=%d want %d (no new commit)", got, before)
	}
}

func TestCommitTaskWork_RemovesMessageFile(t *testing.T) {
	if !haveGit(t) {
		t.Skip("git not on PATH")
	}
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	dir, g := newSessionWorkTestRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := g.CommitTaskWork("sess1", TaskCommitLabel{PRDID: "abcdef0123456789", TaskTitle: "Write note"}, nil); err != nil {
		t.Fatalf("CommitTaskWork: %v", err)
	}
	entries, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("temporary commit message file left behind: %v", entries)
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

// ── operand validation ─────────────────────────────

func TestCheckGitOperands(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{"normal branch", "feature/x-1", false},
		{"remote name", "origin", false},
		{"https url", "https://github.com/o/r.git", false},
		{"leading dash option", "--upload-pack=touch /tmp/x", true},
		{"single dash", "-x", true},
		{"NUL byte", "main\x00--exec", true},
		{"empty", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := checkGitOperands([]string{"ok", c.in})
			if (err != nil) != c.wantErr {
				t.Errorf("checkGitOperands(%q) err=%v wantErr=%v", c.in, err, c.wantErr)
			}
		})
	}
}

func TestRunGitEnv_RefusesBadOperand(t *testing.T) {
	dir, _ := newSessionWorkTestRepo(t)
	for _, bad := range []string{"--hard", "-x", "tag\x00x"} {
		if err := runGitEnv(dir, nil, []string{"reset", "--hard"}, bad); err == nil {
			t.Errorf("runGitEnv accepted operand %q", bad)
		}
		if _, err := gitOutputOperands(dir, []string{"rev-parse", "--verify"}, bad); err == nil {
			t.Errorf("gitOutputOperands accepted operand %q", bad)
		}
	}
	if err := runGitEnv(dir, nil, []string{"tag", "-f", "--"}, "good-tag"); err != nil {
		t.Errorf("normal operand refused: %v", err)
	}
	if _, err := gitOutputOperands(dir, []string{"rev-parse", "--verify"}, "good-tag"); err != nil {
		t.Errorf("normal operand refused by gitOutputOperands: %v", err)
	}
}

func TestRunGit_RefusesNULInFixedArgs(t *testing.T) {
	dir, _ := newSessionWorkTestRepo(t)
	if err := runGit(dir, "tag", "x\x00y"); err == nil {
		t.Error("runGit accepted a NUL byte")
	}
	if _, err := gitOutput(dir, "log", "-1", "--format=a\x00b"); err == nil {
		t.Error("gitOutput accepted a NUL byte")
	}
}

func TestProjectGit_PushBranch_RefusesDashOperands(t *testing.T) {
	dir, g := newSessionWorkTestRepo(t)
	_ = dir
	if err := g.PushBranch("origin", "--delete", "", ""); err == nil {
		t.Error("expected leading-dash branch to be refused")
	}
	if err := g.PushBranch("--mirror", "main", "", ""); err == nil {
		t.Error("expected leading-dash remote to be refused")
	}
	if err := g.PushBranch("origin", "main", "--upload-pack=x", "tok"); err == nil {
		t.Error("expected leading-dash URL to be refused")
	}
}
