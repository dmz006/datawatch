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

// --- EnsureWorktree-level tests -----------------------------------------
//
// The tests above exercise prepareWorktree directly. These exercise the
// full EnsureWorktree path (the one Run() actually calls) against a
// scratch repo whose committed fixture has the same layout as this
// project: internal/docsindex go:embeds the git-ignored
// docs-bm25-index.json.

// fixtureRepoWithDocsIndex returns a scratch git repo (branch main) with
// a committed layout: go.mod (module example.com/fixture),
// internal/docsindex/embedded.go with //go:embed
// assets/docs-bm25-index.json, and internal/consumer/consumer.go which
// imports and uses the embedded bytes. When gitignore is true a
// .gitignore ignoring the placeholder path is committed too; when false
// the ignore line is absent so the info/exclude fallback is exercised.
func fixtureRepoWithDocsIndex(t *testing.T, gitignore bool) string {
	t.Helper()
	dir := scratchGitRepo(t)
	files := map[string]string{
		"go.mod":                         "module example.com/fixture\n\ngo 1.21\n",
		"internal/docsindex/embedded.go": "package docsindex\n\nimport _ \"embed\"\n\n//go:embed assets/docs-bm25-index.json\nvar EmbeddedBM25JSON []byte\n",
		"internal/consumer/consumer.go":  "package consumer\n\nimport \"example.com/fixture/internal/docsindex\"\n\nvar _ = len(docsindex.EmbeddedBM25JSON)\n",
	}
	if gitignore {
		files[".gitignore"] = "/internal/docsindex/assets/docs-bm25-index.json\n"
	}
	for rel, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("add", "-A")
	run("commit", "-m", "docsindex fixture")
	return dir
}

func worktreeGitOut(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// worktreeBuildEnv is os.Environ() minus TMUX/TMUX_TMPDIR (a build
// running inside a tmux pane must not inherit the client's socket
// state), plus GOFLAGS=-mod=mod.
func worktreeBuildEnv() []string {
	env := make([]string, 0, len(os.Environ())+1)
	for _, kv := range os.Environ() {
		idx := 0
		for idx < len(kv) && kv[idx] != '=' {
			idx++
		}
		k := kv[:idx]
		if k == "TMUX" || k == "TMUX_TMPDIR" || k == "GOFLAGS" {
			continue
		}
		env = append(env, kv)
	}
	env = append(env, "GOFLAGS=-mod=mod")
	return env
}

func TestWorktreePrepare_CreatesWhenMissing(t *testing.T) {
	base := fixtureRepoWithDocsIndex(t, true)
	prd := &PRD{ID: "wp100001", Title: "Worktree Prepare Fixture"}
	path, _, err := EnsureWorktree(prd, base, t.TempDir())
	if err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}
	placeholder := filepath.Join(path, docsIndexPlaceholderPath)
	data, err := os.ReadFile(placeholder)
	if err != nil {
		t.Fatalf("placeholder not created in new worktree: %v", err)
	}
	if !strings.Contains(string(data), "{}") {
		t.Fatalf("placeholder content = %q, want it to contain {}", data)
	}
}

func TestWorktreePrepare_LeavesExistingUntouched(t *testing.T) {
	base := fixtureRepoWithDocsIndex(t, true)
	prd := &PRD{ID: "wp100002", Title: "Leave Existing Untouched"}
	wtDir := t.TempDir()
	path, branch, err := EnsureWorktree(prd, base, wtDir)
	if err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}
	placeholder := filepath.Join(path, docsIndexPlaceholderPath)
	custom := []byte(`{"custom":"index"}`)
	if err := os.WriteFile(placeholder, custom, 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-24 * time.Hour)
	if err := os.Chtimes(placeholder, old, old); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(placeholder)
	if err != nil {
		t.Fatal(err)
	}
	// Resumed-run path: same PRD, same worktree dir, Git.Branch set
	// from the first return — EnsureWorktree must reuse, not recreate.
	prd.Git.Branch = branch
	reusePath, reuseBranch, err := EnsureWorktree(prd, base, wtDir)
	if err != nil {
		t.Fatalf("EnsureWorktree (reuse): %v", err)
	}
	if reusePath != path || reuseBranch != branch {
		t.Fatalf("reuse returned (%q, %q), want (%q, %q)", reusePath, reuseBranch, path, branch)
	}
	data, err := os.ReadFile(placeholder)
	if err != nil {
		t.Fatalf("placeholder missing after reuse: %v", err)
	}
	if string(data) != string(custom) {
		t.Fatalf("existing placeholder was rewritten: %q, want %q", data, custom)
	}
	after, err := os.Stat(placeholder)
	if err != nil {
		t.Fatal(err)
	}
	if !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("existing placeholder mtime changed: %s -> %s", before.ModTime(), after.ModTime())
	}
}

func TestWorktreePrepare_ReuseRecreates(t *testing.T) {
	base := fixtureRepoWithDocsIndex(t, true)
	prd := &PRD{ID: "wp100003", Title: "Reuse Recreates"}
	wtDir := t.TempDir()
	path, branch, err := EnsureWorktree(prd, base, wtDir)
	if err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}
	placeholder := filepath.Join(path, docsIndexPlaceholderPath)
	if err := os.Remove(placeholder); err != nil {
		t.Fatal(err)
	}
	if _, serr := os.Stat(placeholder); !os.IsNotExist(serr) {
		t.Fatalf("placeholder still present after remove: %v", serr)
	}
	prd.Git.Branch = branch
	if _, _, err := EnsureWorktree(prd, base, wtDir); err != nil {
		t.Fatalf("EnsureWorktree (reuse): %v", err)
	}
	data, err := os.ReadFile(placeholder)
	if err != nil {
		t.Fatalf("placeholder not recreated on reuse: %v", err)
	}
	if !strings.Contains(string(data), "{}") {
		t.Fatalf("recreated placeholder content = %q, want it to contain {}", data)
	}
}

func TestWorktreePrepare_NeverStaged(t *testing.T) {
	base := fixtureRepoWithDocsIndex(t, true) // placeholder ignored by repo .gitignore
	prd := &PRD{ID: "wp100004", Title: "Never Staged"}
	path, _, err := EnsureWorktree(prd, base, t.TempDir())
	if err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}
	status, err := worktreeGitOut(t, path, "status", "--porcelain")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(status, "docs-bm25-index.json") {
		t.Fatalf(`git status --porcelain mentions the placeholder: %q`, strings.TrimSpace(status))
	}
	cached, err := worktreeGitOut(t, path, "diff", "--cached", "--name-only")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(cached) != "" {
		t.Fatalf("staging area is not empty: %q", strings.TrimSpace(cached))
	}
	if _, cerr := worktreeGitOut(t, path, "check-ignore", "-q", docsIndexPlaceholderPath); cerr != nil {
		t.Fatalf("git check-ignore -q reports the placeholder as not ignored: %v", cerr)
	}
}

func TestWorktreePrepare_ExcludeLineNotDuplicated(t *testing.T) {
	base := fixtureRepoWithDocsIndex(t, false) // no .gitignore — info/exclude fallback
	prd := &PRD{ID: "wp100005", Title: "Exclude Line Not Duplicated"}
	wtDir := t.TempDir()
	path, branch, err := EnsureWorktree(prd, base, wtDir)
	if err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}
	placeholder := filepath.Join(path, docsIndexPlaceholderPath)
	if _, serr := os.Stat(placeholder); serr != nil {
		t.Fatalf("placeholder missing: %v", serr)
	}
	// Second prepare for the same PRD (reused worktree): the exclude
	// line must not be appended a second time.
	prd.Git.Branch = branch
	if _, _, err := EnsureWorktree(prd, base, wtDir); err != nil {
		t.Fatalf("EnsureWorktree (reuse): %v", err)
	}
	if _, serr := os.Stat(placeholder); serr != nil {
		t.Fatalf("placeholder missing after reuse: %v", serr)
	}
	status, serr := worktreeGitOut(t, path, "status", "--porcelain")
	if serr != nil {
		t.Fatal(serr)
	}
	if strings.Contains(status, "docs-bm25-index.json") {
		t.Fatalf(`git status --porcelain lists the placeholder even though the repo .gitignore does not ignore it: %q`, strings.TrimSpace(status))
	}
	if _, cerr := worktreeGitOut(t, path, "check-ignore", "-q", docsIndexPlaceholderPath); cerr != nil {
		t.Fatalf("placeholder not ignored via info/exclude: %v", cerr)
	}
	excludePath, err := worktreeExcludePath(path)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(excludePath)
	if err != nil {
		t.Fatalf("read info/exclude: %v", err)
	}
	want := "/" + strings.TrimPrefix(docsIndexPlaceholderPath, "/")
	count := 0
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == want {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("info/exclude has %d copy(ies) of %q after two prepares, want exactly 1:\n%s", count, want, data)
	}
}

func TestWorktreePrepare_NoOpWithoutDocsIndex(t *testing.T) {
	base := scratchGitRepo(t) // plain repo: no internal/docsindex at all
	prd := &PRD{ID: "wp100006", Title: "No Op Without DocsIndex"}
	path, _, err := EnsureWorktree(prd, base, t.TempDir())
	if err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}
	assetsDir := filepath.Join(path, "internal", "docsindex", "assets")
	if _, serr := os.Stat(assetsDir); !os.IsNotExist(serr) {
		t.Fatalf("foreign worktree got an assets dir at %q (prepareWorktree must be a no-op): %v", assetsDir, serr)
	}
	if _, serr := os.Stat(filepath.Join(path, "internal")); !os.IsNotExist(serr) {
		t.Fatalf("foreign worktree got an internal/ dir: %v", serr)
	}
	status, _ := worktreeGitOut(t, path, "status", "--porcelain")
	if strings.Contains(status, "docs-bm25-index.json") {
		t.Fatalf(`git status mentions the placeholder in a repo that has no docsindex: %q`, strings.TrimSpace(status))
	}
}

func TestDocsIndex_PackageCompilesInNewWorktree(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available (exec.LookPath failed)")
	}
	base := fixtureRepoWithDocsIndex(t, true)
	prd := &PRD{ID: "wp100007", Title: "DocsIndex Package Compiles In New Worktree"}
	path, _, err := EnsureWorktree(prd, base, t.TempDir())
	if err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}
	runBuild := func() ([]byte, error) {
		cmd := exec.Command("go", "build", "./...")
		cmd.Dir = path
		cmd.Env = worktreeBuildEnv()
		out, berr := cmd.CombinedOutput()
		return out, berr
	}
	out, berr := runBuild()
	if berr != nil {
		t.Fatalf("go build ./... in prepared worktree failed (the bug this guards against):\n%s", out)
	}
	// Regression half: WITHOUT the placeholder the go:embed directive
	// has no target and the same build must fail with
	// \"no matching files\". Proves the prepare step is what makes the
	// worktree buildable.
	placeholder := filepath.Join(path, docsIndexPlaceholderPath)
	if err := os.Remove(placeholder); err != nil {
		t.Fatal(err)
	}
	out2, berr2 := runBuild()
	if berr2 == nil {
		t.Fatalf("go build still succeeded with the embedded file removed:\n%s", out2)
	}
	if !strings.Contains(string(out2), "no matching files") {
		t.Fatalf("expected 'no matching files' in build failure, got:\n%s", out2)
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
