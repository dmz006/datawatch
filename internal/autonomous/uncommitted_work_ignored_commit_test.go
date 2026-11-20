package autonomous

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Task commit: git-ignored paths are skipped (never added, never
// force-added, never committed), the commit failure path unstages what it
// staged, and the operator-facing note is informational.

// uwIgnoredSetup writes a .gitignore with .datawatch/, commits it, and creates
// an ignored evidence file under .datawatch/reports.
func uwIgnoredSetup(t *testing.T) string {
	t.Helper()
	dir := uwRepo(t)
	uwWrite(t, dir, ".gitignore", ".datawatch/\n")
	uwGit(t, dir, "add", ".gitignore")
	uwGit(t, dir, "commit", "-q", "-m", "add gitignore")
	if err := os.MkdirAll(filepath.Join(dir, ".datawatch", "reports"), 0o755); err != nil {
		t.Fatal(err)
	}
	uwWrite(t, dir, ".datawatch/reports/evidence.txt", "evidence\n")
	return dir
}

// uwIgnoredVerifySetup is uwIgnoredSetup plus a worker-created verify script
// under the ignored .datawatch/verify directory.
func uwIgnoredVerifySetup(t *testing.T) string {
	t.Helper()
	dir := uwIgnoredSetup(t)
	if err := os.MkdirAll(filepath.Join(dir, ".datawatch", "verify"), 0o755); err != nil {
		t.Fatal(err)
	}
	uwWrite(t, dir, ".datawatch/verify/a.sh", "#!/bin/sh\nexit 0\n")
	return dir
}

func TestTaskCommitSkipsIgnoredPath(t *testing.T) {
	dir := uwIgnoredSetup(t)
	uwWrite(t, dir, "normal.txt", "normal change\n")
	before := uwGit(t, dir, "rev-parse", "HEAD")

	sha, err := DefaultTaskCommit(context.Background(), dir,
		[]string{"normal.txt", ".datawatch/reports/evidence.txt"}, "task commit")
	if err != nil {
		t.Fatalf("DefaultTaskCommit: %v", err)
	}
	after := uwGit(t, dir, "rev-parse", "HEAD")
	if after == before {
		t.Fatal("expected a new commit of the non-ignored path")
	}
	if sha != after {
		t.Errorf("sha %q != HEAD %q", sha, after)
	}
	names := uwGit(t, dir, "show", "--name-only", "--format=", "HEAD")
	if !strings.Contains(names, "normal.txt") {
		t.Errorf("commit missing normal.txt: %q", names)
	}
	if strings.Contains(names, ".datawatch/reports/evidence.txt") {
		t.Errorf("commit must not carry the git-ignored path: %q", names)
	}
	if staged := uwGit(t, dir, "ls-files", "--cached", "--", ".datawatch"); staged != "" {
		t.Errorf("the git-ignored path must not be staged: %q", staged)
	}

	// classify reports the ignored path as skipped, and the caller-side
	// helper names it as ignored (never force-added).
	add, pathspec, skipped := classifyTaskCommitPaths(context.Background(), dir,
		[]string{"normal.txt", ".datawatch/reports/evidence.txt"})
	if len(add) != 1 || add[0] != "normal.txt" {
		t.Errorf("add = %v, want [normal.txt]", add)
	}
	if len(pathspec) != 1 || pathspec[0] != "normal.txt" {
		t.Errorf("pathspec = %v, want [normal.txt]", pathspec)
	}
	if len(skipped) != 1 || skipped[0] != ".datawatch/reports/evidence.txt" {
		t.Fatalf("skipped = %v, want [the ignored path]", skipped)
	}
	ignored := skippedIgnoredPaths(context.Background(), dir, skipped)
	if len(ignored) != 1 || ignored[0] != ".datawatch/reports/evidence.txt" {
		t.Errorf("skippedIgnoredPaths = %v, want [the ignored path]", ignored)
	}
	note := skippedIgnoredPathsNote(ignored, false)
	if !strings.Contains(note, "skipped because it is git-ignored:") {
		t.Errorf("note %q must carry the skipped-ignored phrasing", note)
	}
	if strings.Contains(note, "nothing was committed") {
		t.Errorf("note %q must not claim nothing was committed (normal.txt was)", note)
	}
}

func TestTaskCommitAllSkippedMakesNoCommit(t *testing.T) {
	dir := uwIgnoredSetup(t)
	before := uwGit(t, dir, "rev-parse", "HEAD")

	// The only listed path is git-ignored: nothing is staged and nothing is
	// committed, the commit must not fail, and the return is the unchanged
	// HEAD with a nil error.
	sha, err := DefaultTaskCommit(context.Background(), dir,
		[]string{".datawatch/reports/evidence.txt"}, "task commit")
	if err != nil {
		t.Fatalf("DefaultTaskCommit: nothing was staged, so the commit must not fail: %v", err)
	}
	after := uwGit(t, dir, "rev-parse", "HEAD")
	if after != before {
		t.Fatalf("all listed paths are git-ignored: no commit expected, HEAD %s -> %s", before, after)
	}
	if sha != before {
		t.Errorf("sha %q must be the unchanged HEAD %q", sha, before)
	}
	if staged := uwGit(t, dir, "ls-files", "--cached", "--", ".datawatch"); staged != "" {
		t.Errorf("nothing must be left staged: %q", staged)
	}

	add, pathspec, skipped := classifyTaskCommitPaths(context.Background(), dir,
		[]string{".datawatch/reports/evidence.txt"})
	if len(add) != 0 || len(pathspec) != 0 {
		t.Errorf("add = %v, pathspec = %v, want both empty", add, pathspec)
	}
	if len(skipped) != 1 || skipped[0] != ".datawatch/reports/evidence.txt" {
		t.Fatalf("skipped = %v, want [the ignored path]", skipped)
	}
	ignored := skippedIgnoredPaths(context.Background(), dir, skipped)
	note := skippedIgnoredPathsNote(ignored, true)
	for _, want := range []string{
		"skipped because it is git-ignored:",
		"nothing was committed",
		"outside the ignored folder",
	} {
		if !strings.Contains(note, want) {
			t.Errorf("all-skipped note %q must contain %q", note, want)
		}
	}
}

func TestTaskCommitSkipsIgnoredVerifyScript(t *testing.T) {
	dir := uwIgnoredVerifySetup(t)
	uwWrite(t, dir, "normal.txt", "work change\n")
	before := uwGit(t, dir, "rev-parse", "HEAD")

	sha, err := DefaultTaskCommit(context.Background(), dir,
		[]string{"normal.txt", ".datawatch/verify/a.sh"}, "task commit")
	if err != nil {
		t.Fatalf("DefaultTaskCommit: %v", err)
	}
	after := uwGit(t, dir, "rev-parse", "HEAD")
	if after == before {
		t.Fatal("expected a new commit of the non-ignored path")
	}
	if sha != after {
		t.Errorf("sha %q != HEAD %q", sha, after)
	}
	names := uwGit(t, dir, "show", "--name-only", "--format=", "HEAD")
	if !strings.Contains(names, "normal.txt") {
		t.Errorf("commit missing normal.txt: %q", names)
	}
	if strings.Contains(names, ".datawatch/verify/a.sh") {
		t.Errorf("commit must not carry the git-ignored verify script: %q", names)
	}
	if staged := uwGit(t, dir, "ls-files", "--cached", "--", ".datawatch"); staged != "" {
		t.Errorf("the git-ignored verify script must not be staged: %q", staged)
	}
}

// The note builder itself: empty input renders nothing; the all-skipped
// variant carries the "nothing was committed" and outside-the-ignored-folder
// advice, and never a version number.
func TestSkippedIgnoredPathsNote(t *testing.T) {
	if note := skippedIgnoredPathsNote(nil, false); note != "" {
		t.Errorf("no ignored paths: note must be empty, got %q", note)
	}
	note := skippedIgnoredPathsNote([]string{".datawatch/verify/a.sh"}, true)
	for _, want := range []string{
		"skipped because it is git-ignored:",
		".datawatch/verify/a.sh",
		"nothing was committed",
		"outside the ignored folder",
	} {
		if !strings.Contains(note, want) {
			t.Errorf("note %q must contain %q", note, want)
		}
	}
	if strings.Contains(note, "v9.9.0") {
		t.Errorf("note must not carry a version number: %q", note)
	}
}

// A failed commit must unstage what this call staged and the
// operator-facing hold must carry the git failure plus the informational
// note that the ignored path was skipped (and never force-added).
func TestUncommittedCommitFailureUnstagesAndCarriesGitOutput(t *testing.T) {
	dir := uwIgnoredSetup(t)
	uwWrite(t, dir, "normal.txt", "work change\n")

	// No usable identity anywhere: `git add` needs none, so the staging of
	// the non-ignored path succeeds; `git commit` is what fails.
	t.Setenv("GIT_AUTHOR_NAME", "") // registers restore at cleanup
	os.Unsetenv("GIT_AUTHOR_NAME")
	t.Setenv("GIT_AUTHOR_EMAIL", "") // registers restore at cleanup
	os.Unsetenv("GIT_AUTHOR_EMAIL")
	t.Setenv("GIT_COMMITTER_NAME", "") // registers restore at cleanup
	os.Unsetenv("GIT_COMMITTER_NAME")
	t.Setenv("GIT_COMMITTER_EMAIL", "") // registers restore at cleanup
	os.Unsetenv("GIT_COMMITTER_EMAIL")
	t.Setenv("GIT_CONFIG", "") // registers restore at cleanup
	os.Unsetenv("GIT_CONFIG")
	t.Setenv("GIT_CONFIG_GLOBAL", "") // registers restore at cleanup
	os.Unsetenv("GIT_CONFIG_GLOBAL")
	t.Setenv("GIT_CONFIG_SYSTEM", "") // registers restore at cleanup
	os.Unsetenv("GIT_CONFIG_SYSTEM")
	t.Setenv("HOME", "") // registers restore at cleanup
	os.Unsetenv("HOME")
	t.Setenv("XDG_CONFIG_HOME", "") // registers restore at cleanup
	os.Unsetenv("XDG_CONFIG_HOME")
	t.Setenv("GIT_CONFIG_GLOBAL", "/nonexistent")
	t.Setenv("GIT_CONFIG_SYSTEM", "/nonexistent")

	ctx := context.Background()
	_, err := DefaultTaskCommit(ctx, dir,
		[]string{"normal.txt", ".datawatch/reports/evidence.txt"}, "task commit")
	if err == nil {
		t.Fatal("expected the commit to fail (identity unavailable)")
	}
	if !strings.Contains(err.Error(), "git commit") || !strings.Contains(err.Error(), "exit status") {
		t.Fatalf("error must carry the git commit failure, got: %v", err)
	}

	// The failed commit must not leave the staged path staged.
	if staged := uwGit(t, dir, "ls-files", "--cached", "--", "normal.txt"); staged != "" {
		t.Errorf("a failed commit must unstage the added path, got staged: %q", staged)
	}

	// The operator-facing hold reasons carry the git failure plus the
	// informational note that the ignored path was skipped (and never
	// force-added). The error is the commit failure, not the skip.
	r := newUncommittedHoldRig(t, true)
	cause := uncommittedCommitCause(err)
	ignored := skippedIgnoredPaths(ctx, dir, []string{".datawatch/reports/evidence.txt"})
	if len(ignored) == 0 {
		t.Fatal("the evidence path must read as git-ignored")
	}
	fullCause := cause + "; " + skippedIgnoredPathsNote(ignored, false)
	files := []UncommittedFile{
		{Path: "normal.txt", Status: " M"},
		{Path: ".datawatch/reports/evidence.txt", Status: "??"},
	}
	if herr := r.m.holdForUncommittedWork(r.prd, r.task, files, fullCause); herr != nil {
		t.Fatalf("holdForUncommittedWork: %v", herr)
	}
	tk, _ := r.m.Store().GetTask(r.task.ID)
	if tk.Status != TaskBlocked {
		t.Errorf("status = %s; want blocked", tk.Status)
	}
	if tk.UncommittedWork == nil {
		t.Fatal("hold not recorded")
	}
	for _, want := range []string{
		"git commit",
		"exit status",
		"unable to auto-detect email address",
		"skipped because it is git-ignored:",
	} {
		if !strings.Contains(tk.UncommittedWork.Reason, want) {
			t.Errorf("hold reason %q missing %q", tk.UncommittedWork.Reason, want)
		}
	}
	qs := r.store.List(QuestionFilter{})
	if len(qs) != 1 {
		t.Fatalf("questions = %d; want 1", len(qs))
	}
	for _, want := range []string{"normal.txt", ".datawatch/reports/evidence.txt", "skipped because it is git-ignored:"} {
		if !strings.Contains(qs[0].Text, want) {
			t.Errorf("question text lacks %q:\n%s", want, qs[0].Text)
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.acts) != 1 || r.acts[0] != "autonomous_uncommitted_work_held" {
		t.Fatalf("audit actions = %v", r.acts)
	}
	if got := r.audits[0]["cause"].(string); !strings.Contains(got, "skipped because it is git-ignored:") {
		t.Errorf("audit cause = %q", got)
	}
}

// uwCachedEmpty asserts 'git diff --cached --name-only' is empty: a task
// commit must never leave anything staged, whether it committed or skipped.
func uwCachedEmpty(t *testing.T, dir string) {
	t.Helper()
	if cached := uwGit(t, dir, "diff", "--cached", "--name-only"); cached != "" {
		t.Errorf("git diff --cached --name-only must be empty, got %q", cached)
	}
}

// Case 1: one git-ignored generated file under .datawatch/verify plus two
// normal files. The commit holds exactly the two normal files; the ignored
// file is absent from HEAD and from the index; the skipped note names it.
func TestTaskCommitIgnoredVerifyFileWithTwoNormalFiles(t *testing.T) {
	dir := uwIgnoredVerifySetup(t)
	uwWrite(t, dir, "one.txt", "first change\n")
	uwWrite(t, dir, "two.txt", "second change\n")
	before := uwGit(t, dir, "rev-parse", "HEAD")
	const ignored = ".datawatch/verify/a.sh"

	sha, err := DefaultTaskCommit(context.Background(), dir,
		[]string{"one.txt", ignored, "two.txt"}, "task commit")
	if err != nil {
		t.Fatalf("DefaultTaskCommit: %v", err)
	}
	after := uwGit(t, dir, "rev-parse", "HEAD")
	if after == before {
		t.Fatal("expected a new commit of the two normal files")
	}
	if sha != after {
		t.Errorf("sha %q != HEAD %q", sha, after)
	}

	names := strings.Fields(uwGit(t, dir, "show", "--name-only", "--format=", "HEAD"))
	if len(names) != 2 || names[0] != "one.txt" || names[1] != "two.txt" {
		t.Errorf("commit files = %v, want exactly [one.txt two.txt]", names)
	}
	if inHead := uwGit(t, dir, "ls-tree", "-r", "--name-only", "HEAD", "--", ".datawatch"); inHead != "" {
		t.Errorf("ignored file must be absent from HEAD, got %q", inHead)
	}
	if inIndex := uwGit(t, dir, "ls-files", "--cached", "--", ignored); inIndex != "" {
		t.Errorf("ignored file must be absent from the index, got %q", inIndex)
	}
	uwCachedEmpty(t, dir)

	_, _, skipped := classifyTaskCommitPaths(context.Background(), dir,
		[]string{"one.txt", ignored, "two.txt"})
	ignoredPaths := skippedIgnoredPaths(context.Background(), dir, skipped)
	if len(ignoredPaths) != 1 || ignoredPaths[0] != ignored {
		t.Fatalf("skippedIgnoredPaths = %v, want [%s]", ignoredPaths, ignored)
	}
	note := skippedIgnoredPathsNote(ignoredPaths, false)
	if !strings.Contains(note, ignored) {
		t.Errorf("skipped note %q must name the ignored path %s", note, ignored)
	}
}

// Case 2: only git-ignored files. No commit is made, HEAD is unchanged, the
// note says nothing was committed, the error is nil, and nothing is staged.
func TestTaskCommitOnlyIgnoredFilesNoCommit(t *testing.T) {
	dir := uwIgnoredVerifySetup(t)
	before := uwGit(t, dir, "rev-parse", "HEAD")
	const ignored = ".datawatch/verify/a.sh"

	sha, err := DefaultTaskCommit(context.Background(), dir, []string{ignored}, "task commit")
	if err != nil {
		t.Fatalf("only ignored files: error must be nil, got %v", err)
	}
	if after := uwGit(t, dir, "rev-parse", "HEAD"); after != before {
		t.Fatalf("no commit expected, HEAD %s -> %s", before, after)
	}
	if sha != before {
		t.Errorf("sha %q must be the unchanged HEAD %q", sha, before)
	}
	if inIndex := uwGit(t, dir, "ls-files", "--cached", "--", ignored); inIndex != "" {
		t.Errorf("ignored file must be absent from the index, got %q", inIndex)
	}
	uwCachedEmpty(t, dir)

	_, _, skipped := classifyTaskCommitPaths(context.Background(), dir, []string{ignored})
	note := skippedIgnoredPathsNote(skippedIgnoredPaths(context.Background(), dir, skipped), true)
	if !strings.Contains(note, "nothing was committed") {
		t.Errorf("all-skipped note %q must say nothing was committed", note)
	}
	if !strings.Contains(note, ignored) {
		t.Errorf("all-skipped note %q must name the ignored path", note)
	}
}
