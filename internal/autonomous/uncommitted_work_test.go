package autonomous

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// uwStatusRunner returns a driftGitRunner that answers git status with out.
func uwStatusRunner(out string) driftGitRunner {
	return func(ctx context.Context, dir string, args ...string) (string, error) {
		return out, nil
	}
}

func uwPaths(files []UncommittedFile) []string {
	var p []string
	for _, f := range files {
		p = append(p, f.Path)
	}
	return p
}

func TestUncommittedDetectorParsesStatus(t *testing.T) {
	out := " M internal/a.go\x00" +
		"?? docs/new note.md\x00" +
		"D  gone.txt\x00" +
		"R  renamed/new.go\x00renamed/old.go\x00" +
		"M  internal/a.go\x00" // duplicate path collapses to one entry
	var gotArgs []string
	run := func(ctx context.Context, dir string, args ...string) (string, error) {
		gotArgs = args
		return out, nil
	}
	got, err := DetectUncommittedWork(context.Background(), t.TempDir(), run)
	if err != nil {
		t.Fatal(err)
	}
	wantArgs := []string{"status", "--porcelain=v1", "-z", "--untracked-files=all"}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("git args = %v, want %v", gotArgs, wantArgs)
	}
	want := []UncommittedFile{
		{Path: "docs/new note.md", Status: "??"},
		{Path: "gone.txt", Status: "D "},
		{Path: "internal/a.go", Status: " M"},
		{Path: "renamed/new.go", Status: "R "},
		{Path: "renamed/old.go", Status: "D "},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("files = %+v, want %+v", got, want)
	}
}

func TestUncommittedDetectorErrors(t *testing.T) {
	if _, err := DetectUncommittedWork(context.Background(), "", uwStatusRunner("")); err == nil {
		t.Fatal("empty dir must return an error")
	}
	failing := func(ctx context.Context, dir string, args ...string) (string, error) {
		return "", os.ErrPermission
	}
	if _, err := DetectUncommittedWork(context.Background(), t.TempDir(), failing); err == nil {
		t.Fatal("git error must return an error")
	}
}

func TestUncommittedDetectorDropsBookkeeping(t *testing.T) {
	out := "?? CHECKPOINT.md\x00?? .decompose-output-abc.json\x00?? .datawatch/tree.lock\x00?? node_modules/x/index.js\x00 M real.go\x00"
	dir := t.TempDir()
	// The staging policy judges files on disk, so the ignored-directory file must exist.
	if err := os.MkdirAll(filepath.Join(dir, "node_modules", "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "node_modules", "x", "index.js"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := DetectUncommittedWork(context.Background(), dir, uwStatusRunner(out))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"real.go"}; !reflect.DeepEqual(uwPaths(got), want) {
		t.Fatalf("paths = %v, want %v", uwPaths(got), want)
	}
}

func uwSample() []UncommittedFile {
	return []UncommittedFile{
		{Path: "a.go", Status: " M"},
		{Path: "b.go", Status: " M"},
		{Path: "c.go", Status: "??"},
	}
}

func TestUncommittedDetectorTaskFilesScope(t *testing.T) {
	task := &Task{PreTaskDirtyFiles: []string{"a.go"}, PreTaskUntrackedFiles: []string{"./c.go"}}
	got := ScopeUncommittedFiles(uwSample(), task, nil, "", config.UncommittedWorkScopeTaskFiles)
	if want := []string{"b.go"}; !reflect.DeepEqual(uwPaths(got), want) {
		t.Fatalf("paths = %v, want %v", uwPaths(got), want)
	}
}

func TestUncommittedDetectorWorktreeScope(t *testing.T) {
	task := &Task{PreTaskDirtyFiles: []string{"a.go"}, PreTaskUntrackedFiles: []string{"c.go"}}
	got := ScopeUncommittedFiles(uwSample(), task, nil, "", config.UncommittedWorkScopeWorktree)
	if want := []string{"a.go", "b.go", "c.go"}; !reflect.DeepEqual(uwPaths(got), want) {
		t.Fatalf("paths = %v, want %v", uwPaths(got), want)
	}
}

func TestUncommittedDetectorPreDirtyExcluded(t *testing.T) {
	task := &Task{PreTaskDirtyFiles: []string{"a.go", "b.go"}, FilesPlanned: []string{"a.go"}}
	got := ScopeUncommittedFiles(uwSample(), task, nil, "", config.UncommittedWorkScopeTaskFiles)
	if want := []string{"c.go"}; !reflect.DeepEqual(uwPaths(got), want) {
		t.Fatalf("paths = %v, want %v (a planned file that was already dirty is still not the task's)", uwPaths(got), want)
	}
	// The worktree scope removes nothing, even for files dirty before the task.
	if n := len(ScopeUncommittedFiles(uwSample(), task, nil, "", config.UncommittedWorkScopeWorktree)); n != 3 {
		t.Fatalf("worktree scope kept %d files, want 3", n)
	}
}

func TestUncommittedDetectorOutsideDeclaredScope(t *testing.T) {
	dir := t.TempDir()
	files := []UncommittedFile{
		{Path: "planned.go", Status: " M"},
		{Path: "generated.sh", Status: "??"},
		{Path: "src/inside.go", Status: "??"},
		{Path: "stray.txt", Status: "??"},
	}
	task := &Task{FilesPlanned: []string{"./planned.go"}, FilesGenerated: []string{"generated.sh"}}
	got := ScopeUncommittedFiles(files, task, []string{filepath.Join(dir, "src")}, dir, config.UncommittedWorkScopeWorktree)
	if len(got) != 4 {
		t.Fatalf("every file must still be reported, got %+v", got)
	}
	flagged := map[string]bool{}
	for _, f := range got {
		flagged[f.Path] = f.OutsideScope
	}
	want := map[string]bool{"planned.go": false, "generated.sh": false, "src/inside.go": false, "stray.txt": true}
	if !reflect.DeepEqual(flagged, want) {
		t.Fatalf("outside_scope = %v, want %v", flagged, want)
	}
	if want := []string{"generated.sh", "planned.go", "src/inside.go"}; !sameSet(InScopeUncommitted(got), want) {
		t.Fatalf("in scope = %v, want %v", InScopeUncommitted(got), want)
	}
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	m := map[string]int{}
	for _, s := range a {
		m[s]++
	}
	for _, s := range b {
		m[s]--
	}
	for _, n := range m {
		if n != 0 {
			return false
		}
	}
	return true
}

// uwGit runs git in dir with a fixed identity and no inherited repository
// environment, failing the test on error.
func uwGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	env := []string{}
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "GIT_") {
			continue
		}
		env = append(env, e)
	}
	cmd.Env = append(env,
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// uwRepo makes a repository with one committed file and applies the same git
// environment DefaultTaskCommit will see through the process environment.
func uwRepo(t *testing.T) string {
	t.Helper()
	t.Setenv("GIT_DIR", "")
	os.Unsetenv("GIT_DIR")
	t.Setenv("GIT_AUTHOR_NAME", "Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")
	dir := t.TempDir()
	uwGit(t, dir, "init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "base.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	uwGit(t, dir, "add", "base.txt")
	uwGit(t, dir, "commit", "-q", "-m", "base")
	return dir
}

func TestUncommittedCommitStagesOnlyListedFiles(t *testing.T) {
	dir := uwRepo(t)
	for _, n := range []string{"one.txt", "two.txt", "three.txt"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte(n+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	before := uwGit(t, dir, "rev-parse", "HEAD")
	count := func() string { return uwGit(t, dir, "rev-list", "--count", "HEAD") }
	if count() != "1" {
		t.Fatalf("setup: want 1 commit, got %s", count())
	}

	sha, err := DefaultTaskCommit(context.Background(), dir, []string{"one.txt", "two.txt"}, "task work")
	if err != nil {
		t.Fatal(err)
	}
	if after := uwGit(t, dir, "rev-parse", "HEAD"); sha != after || sha == before {
		t.Fatalf("returned sha %s, HEAD %s, before %s", sha, after, before)
	}
	if count() != "2" {
		t.Fatalf("HEAD must advance by exactly one commit, count = %s", count())
	}
	if got := uwGit(t, dir, "show", "--name-only", "--format=", "HEAD"); !sameSet(strings.Fields(got), []string{"one.txt", "two.txt"}) {
		t.Fatalf("commit holds %q, want one.txt and two.txt", got)
	}
	if st := uwGit(t, dir, "status", "--porcelain"); st != "?? three.txt" {
		t.Fatalf("status = %q, want only three.txt still untracked", st)
	}

	// Nothing to commit: no new commit, current HEAD returned.
	again, err := DefaultTaskCommit(context.Background(), dir, []string{"one.txt"}, "again")
	if err != nil {
		t.Fatal(err)
	}
	if again != sha || count() != "2" {
		t.Fatalf("no-op commit returned %s (count %s), want %s", again, count(), sha)
	}
}

func TestUncommittedCommitFailureReported(t *testing.T) {
	dir := uwRepo(t)
	hook := filepath.Join(dir, ".git", "hooks", "pre-commit")
	if err := os.MkdirAll(filepath.Dir(hook), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hook, []byte("#!/bin/sh\necho blocked by hook >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "work.txt"), []byte("work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := uwGit(t, dir, "rev-parse", "HEAD")

	sha, err := DefaultTaskCommit(context.Background(), dir, []string{"work.txt"}, "task work")
	if err == nil {
		t.Fatalf("a failing pre-commit hook must fail the commit, got sha %s", sha)
	}
	if !strings.Contains(err.Error(), "blocked by hook") {
		t.Errorf("error should carry the hook output, got %v", err)
	}
	if after := uwGit(t, dir, "rev-parse", "HEAD"); after != before {
		t.Fatalf("HEAD moved to %s despite the failed commit", after)
	}
	files, derr := DetectUncommittedWork(context.Background(), dir, nil)
	if derr != nil {
		t.Fatal(derr)
	}
	if want := []string{"work.txt"}; !reflect.DeepEqual(uwPaths(files), want) {
		t.Fatalf("files still dirty = %v, want %v", uwPaths(files), want)
	}
}
