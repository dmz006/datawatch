package autonomous

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// judgeDiffGit runs git in dir for a test repository, failing the test on error.
func judgeDiffGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// judgeDiffRepo creates a repository on branch main with one committed file.
// The commit identity comes from environment variables, not git config.
func judgeDiffRepo(t *testing.T) string {
	t.Helper()
	t.Setenv("GIT_AUTHOR_NAME", "Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.invalid")
	t.Setenv("GIT_COMMITTER_NAME", "Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.invalid")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	dir := t.TempDir()
	judgeDiffGit(t, dir, "init", "-q", "-b", "main")
	judgeDiffWrite(t, dir, "base.txt", "one\ntwo\nthree\n")
	judgeDiffCommit(t, dir, "base")
	return dir
}

func judgeDiffWrite(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func judgeDiffCommit(t *testing.T, dir, msg string) {
	t.Helper()
	judgeDiffGit(t, dir, "add", "-A")
	judgeDiffGit(t, dir, "commit", "-q", "-m", msg)
}

func judgeDiffPRD(base, branch string) *PRD {
	return &PRD{ID: "judgediff", Git: GitWorkflow{BaseBranch: base, Branch: branch}}
}

func TestJudgeDiffLines_SumsAddedAndDeleted(t *testing.T) {
	dir := judgeDiffRepo(t)
	judgeDiffGit(t, dir, "checkout", "-q", "-b", "work")
	// base.txt: delete "two" (1 deleted), add 2 lines (2 added) => 3
	judgeDiffWrite(t, dir, "base.txt", "one\nthree\nfour\nfive\n")
	// new file in a subdirectory: 4 added
	judgeDiffWrite(t, dir, "sub/new.txt", "a\nb\nc\nd\n")
	judgeDiffCommit(t, dir, "work")

	got, err := JudgeDiffLines(context.Background(), judgeDiffPRD("", "work"), dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != 7 {
		t.Fatalf("got %d lines, want 7", got)
	}
}

func TestJudgeDiffLines_ExcludesLockFiles(t *testing.T) {
	dir := judgeDiffRepo(t)
	judgeDiffGit(t, dir, "checkout", "-q", "-b", "work")
	judgeDiffWrite(t, dir, "code.go", "package x\n")
	judgeDiffWrite(t, dir, "go.sum", strings.Repeat("sum\n", 50))
	judgeDiffWrite(t, dir, "web/package-lock.json", strings.Repeat("{}\n", 40))
	judgeDiffWrite(t, dir, "deep/er/Cargo.lock", strings.Repeat("x\n", 30))
	judgeDiffWrite(t, dir, "uv.lock", "x\n")
	judgeDiffCommit(t, dir, "work")

	got, err := JudgeDiffLines(context.Background(), judgeDiffPRD("", "work"), dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != 1 {
		t.Fatalf("got %d lines, want 1 (lock files excluded)", got)
	}
}

func TestJudgeDiffLines_BinaryFileCountsZero(t *testing.T) {
	dir := judgeDiffRepo(t)
	judgeDiffGit(t, dir, "checkout", "-q", "-b", "work")
	if err := os.WriteFile(filepath.Join(dir, "blob.bin"), []byte{0, 1, 2, 0, 3, 0, 0, 255}, 0o644); err != nil {
		t.Fatal(err)
	}
	judgeDiffWrite(t, dir, "text.txt", "x\ny\n")
	judgeDiffCommit(t, dir, "work")

	got, err := JudgeDiffLines(context.Background(), judgeDiffPRD("", "work"), dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != 2 {
		t.Fatalf("got %d lines, want 2 (binary counts 0)", got)
	}
}

func TestJudgeDiffLines_BaseBranchOverride(t *testing.T) {
	dir := judgeDiffRepo(t)
	judgeDiffGit(t, dir, "checkout", "-q", "-b", "release")
	judgeDiffWrite(t, dir, "rel.txt", "r1\nr2\nr3\n")
	judgeDiffCommit(t, dir, "release work")
	judgeDiffGit(t, dir, "checkout", "-q", "-b", "work")
	judgeDiffWrite(t, dir, "work.txt", "w1\n")
	judgeDiffCommit(t, dir, "work")

	// Against main the release commit counts too (4); against release only 1.
	viaDefault, err := JudgeDiffLines(context.Background(), judgeDiffPRD("", "work"), dir)
	if err != nil {
		t.Fatal(err)
	}
	if viaDefault != 4 {
		t.Fatalf("default base: got %d, want 4", viaDefault)
	}
	viaOverride, err := JudgeDiffLines(context.Background(), judgeDiffPRD("release", "work"), dir)
	if err != nil {
		t.Fatal(err)
	}
	if viaOverride != 1 {
		t.Fatalf("override base: got %d, want 1", viaOverride)
	}
}

func TestJudgeDiffLines_IgnoresUncommittedEdits(t *testing.T) {
	dir := judgeDiffRepo(t)
	judgeDiffGit(t, dir, "checkout", "-q", "-b", "work")
	judgeDiffWrite(t, dir, "a.txt", "1\n")
	judgeDiffCommit(t, dir, "work")
	judgeDiffWrite(t, dir, "a.txt", "1\n2\n3\n")
	judgeDiffWrite(t, dir, "untracked.txt", "u\n")

	got, err := JudgeDiffLines(context.Background(), judgeDiffPRD("", "work"), dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != 1 {
		t.Fatalf("got %d lines, want 1 (committed only)", got)
	}
}

func TestJudgeDiffLines_EmptyBranchErrors(t *testing.T) {
	dir := judgeDiffRepo(t)
	_, err := JudgeDiffLines(context.Background(), judgeDiffPRD("", ""), dir)
	if err == nil || !strings.Contains(err.Error(), "no branch yet") {
		t.Fatalf("want a no-branch error, got %v", err)
	}
}

func TestJudgeDiffLines_NoMergeBaseErrors(t *testing.T) {
	dir := judgeDiffRepo(t)
	judgeDiffGit(t, dir, "checkout", "-q", "--orphan", "unrelated")
	judgeDiffGit(t, dir, "rm", "-rf", "-q", ".")
	judgeDiffWrite(t, dir, "other.txt", "x\n")
	judgeDiffCommit(t, dir, "unrelated root")

	_, err := JudgeDiffLines(context.Background(), judgeDiffPRD("main", "unrelated"), dir)
	if err == nil {
		t.Fatal("want an error for branches with no merge base")
	}
}
