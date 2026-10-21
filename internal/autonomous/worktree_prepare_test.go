package autonomous

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// makeFakeWorktree builds a minimal stand-in for a datawatch checkout:
// internal/docsindex/ exists with embedded.go-style content expectations
// (the tests only care about the directory layout).
func makeFakeWorktree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mustMkdir := func(p string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(dir, p), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mustMkdir("internal/docsindex")
	return dir
}

func prepareWorktreeGitFixture(t *testing.T, gitignoreLine string) string {
	t.Helper()
	dir := scratchGitRepo(t)
	if err := os.MkdirAll(filepath.Join(dir, "internal/docsindex"), 0o755); err != nil {
		t.Fatal(err)
	}
	if gitignoreLine != "" {
		if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(gitignoreLine+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestEnsureDocsIndexPlaceholder_CreatesEmptyIndex(t *testing.T) {
	dir := makeFakeWorktree(t)
	if err := ensureDocsIndexPlaceholder(dir); err != nil {
		t.Fatalf("ensureDocsIndexPlaceholder: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "internal/docsindex/assets/docs-bm25-index.json"))
	if err != nil {
		t.Fatalf("placeholder missing after prepare: %v", err)
	}
	if string(data) != "{}\n" {
		t.Fatalf("placeholder content = %q, want %q", data, "{}\n")
	}
}

func TestEnsureDocsIndexPlaceholder_LeavesExistingFileUntouched(t *testing.T) {
	dir := makeFakeWorktree(t)
	placeholder := filepath.Join(dir, "internal/docsindex/assets/docs-bm25-index.json")
	if err := os.MkdirAll(filepath.Dir(placeholder), 0o755); err != nil {
		t.Fatal(err)
	}
	generated := []byte(`{"docs":[{"id":"a","title":"A"}]}`)
	if err := os.WriteFile(placeholder, generated, 0o644); err != nil {
		t.Fatal(err)
	}
	// Park the mtime well below "now" so any rewrite would be visible.
	old := time.Now().Add(-24 * time.Hour)
	if err := os.Chtimes(placeholder, old, old); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(placeholder)
	if err != nil {
		t.Fatal(err)
	}
	if err := ensureDocsIndexPlaceholder(dir); err != nil {
		t.Fatalf("ensureDocsIndexPlaceholder on existing file: %v", err)
	}
	after, err := os.Stat(placeholder)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(placeholder); string(got) != string(generated) {
		t.Fatalf("existing file content changed: %q", got)
	}
	if !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("existing file mtime changed: %s -> %s", before.ModTime(), after.ModTime())
	}
}

func TestEnsureDocsIndexPlaceholder_IgnoresForeignRepos(t *testing.T) {
	dir := t.TempDir() // no internal/docsindex at all
	if err := ensureDocsIndexPlaceholder(dir); err != nil {
		t.Fatalf("ensureDocsIndexPlaceholder on foreign repo: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "internal")); !os.IsNotExist(err) {
		t.Fatalf("foreign repo was touched: internal/ exists")
	}
}

func TestPrepareWorktree_RejectsUnknownRepos(t *testing.T) {
	dir := scratchGitRepo(t) // no internal/docsindex layout at all
	excludeBefore, _ := os.ReadFile(filepath.Join(dir, ".git/info/exclude"))
	if err := prepareWorktree(dir); err != nil {
		t.Fatalf("prepareWorktree on foreign repo: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "internal")); !os.IsNotExist(err) {
		t.Fatalf("foreign repo was touched: internal/ exists")
	}
	excludeAfter, _ := os.ReadFile(filepath.Join(dir, ".git/info/exclude"))
	if string(excludeBefore) != string(excludeAfter) {
		t.Fatalf("foreign repo .git/info/exclude changed: %q", excludeAfter)
	}
}

func TestPrepareWorktree_GitIgnoredByRepoGitignore(t *testing.T) {
	dir := prepareWorktreeGitFixture(t, "/internal/docsindex/assets/docs-bm25-index.json")
	excludeBefore, _ := os.ReadFile(filepath.Join(dir, ".git/info/exclude"))
	if err := prepareWorktree(dir); err != nil {
		t.Fatalf("prepareWorktree: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "internal/docsindex/assets/docs-bm25-index.json"))
	if err != nil || string(data) != "{}\n" {
		t.Fatalf("placeholder not created (data=%q, err=%v)", data, err)
	}
	// Already covered by the repo .gitignore — info/exclude must be untouched.
	excludeAfter, _ := os.ReadFile(filepath.Join(dir, ".git/info/exclude"))
	if string(excludeBefore) != string(excludeAfter) {
		t.Fatalf("info/exclude was needlessly modified: %q", excludeAfter)
	}
	cmd := exec.Command("git", "check-ignore", "-q", "internal/docsindex/assets/docs-bm25-index.json")
	cmd.Dir = dir
	if _, cerr := cmd.CombinedOutput(); cerr != nil {
		t.Fatalf("placeholder not ignored after prepare: %v", cerr)
	}
}

func TestPrepareWorktree_NonIgnoredRepoGetsExcludeLine(t *testing.T) {
	dir := prepareWorktreeGitFixture(t, "")
	if err := prepareWorktree(dir); err != nil {
		t.Fatalf("prepareWorktree: %v", err)
	}
	exclude, err := os.ReadFile(filepath.Join(dir, ".git/info/exclude"))
	if err != nil {
		t.Fatalf("read info/exclude: %v", err)
	}
	if !gitExcludeHasLine(string(exclude), "/internal/docsindex/assets/docs-bm25-index.json") {
		t.Fatalf("info/exclude missing the pattern: %q", exclude)
	}
	cmd := exec.Command("git", "check-ignore", "-q", "internal/docsindex/assets/docs-bm25-index.json")
	cmd.Dir = dir
	if _, cerr := cmd.CombinedOutput(); cerr != nil {
		t.Fatalf("placeholder not ignored after append: %v", cerr)
	}
	// Second run: stays idempotent, no duplicate line.
	if err := prepareWorktree(dir); err != nil {
		t.Fatalf("prepareWorktree (second run): %v", err)
	}
	exclude2, err := os.ReadFile(filepath.Join(dir, ".git/info/exclude"))
	if err != nil {
		t.Fatalf("re-read info/exclude: %v", err)
	}
	if string(exclude2) != string(exclude) {
		t.Fatalf("second run changed info/exclude: %q", exclude2)
	}
	// And `git status` must not list the placeholder as untracked.
	statusCmd := exec.Command("git", "status", "--porcelain")
	statusCmd.Dir = dir
	out, _ := statusCmd.CombinedOutput()
	if strings.Contains(string(out), "docs-bm25-index.json") {
		t.Fatalf(`git status lists the placeholder: %q`, out)
	}
}

func TestAppendGitExcludeLine_IdsAndNoDuplicate(t *testing.T) {
	dir := t.TempDir()
	exclude := filepath.Join(dir, "info/exclude") // parent "info" intentionally absent at first

	line := "/internal/docsindex/assets/docs-bm25-index.json"
	if err := appendGitExcludeLine(exclude, line); err != nil {
		t.Fatalf("first append: %v", err)
	}
	data, err := os.ReadFile(exclude)
	if err != nil {
		t.Fatalf("read exclude: %v", err)
	}
	if s := string(data); s != line+"\n" {
		t.Fatalf("exclude content = %q, want %q", s, line+"\n")
	}
	// Second call: no duplicate, file unchanged.
	if err := appendGitExcludeLine(exclude, line); err != nil {
		t.Fatalf("second append: %v", err)
	}
	data2, err := os.ReadFile(exclude)
	if err != nil {
		t.Fatalf("re-read exclude: %v", err)
	}
	if string(data2) != string(data) {
		t.Fatalf("line duplicated: %q", data2)
	}
}

func TestAppendGitExcludeLine_NoTrailingNewline(t *testing.T) {
	dir := t.TempDir()
	exclude := filepath.Join(dir, "info/exclude")
	if err := os.MkdirAll(filepath.Dir(exclude), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exclude, []byte(".something-else"), 0o644); err != nil {
		t.Fatal(err)
	}
	line := "/internal/docsindex/assets/docs-bm25-index.json"
	if err := appendGitExcludeLine(exclude, line); err != nil {
		t.Fatalf("append: %v", err)
	}
	data, _ := os.ReadFile(exclude)
	if s := string(data); s != ".something-else\n"+line+"\n" {
		t.Fatalf("exclude content = %q", s)
	}
}
