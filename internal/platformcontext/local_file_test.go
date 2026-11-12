package platformcontext

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func localGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func localRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	localGit(t, dir, "init", "-q")
	return dir
}

func localRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestPlatformContextLocalNonGit(t *testing.T) {
	dir := t.TempDir()
	w, err := WritePlatformContextLocalFile(dir, "body", 1)
	if err != nil || !w {
		t.Fatalf("w=%v err=%v", w, err)
	}
	if got := localRead(t, filepath.Join(dir, platformContextLocalFileName)); got != block("body", 1) {
		t.Fatalf("got %q", got)
	}
	if w, err := WritePlatformContextLocalFile(dir, "body", 1); err != nil || w {
		t.Fatalf("second w=%v err=%v", w, err)
	}
}

func TestPlatformContextLocalGitExcludeOnceAndIdempotent(t *testing.T) {
	dir := localRepo(t)
	if w, err := WritePlatformContextLocalFile(dir, "body", 1); err != nil || !w {
		t.Fatalf("w=%v err=%v", w, err)
	}
	ex := filepath.Join(dir, ".git", "info", "exclude")
	first := localRead(t, ex)
	if strings.Count(first, platformContextLocalFileName+"\n") != 1 {
		t.Fatalf("exclude: %q", first)
	}
	file := filepath.Join(dir, platformContextLocalFileName)
	content := localRead(t, file)
	if w, err := WritePlatformContextLocalFile(dir, "body", 1); err != nil || w {
		t.Fatalf("second w=%v err=%v", w, err)
	}
	if localRead(t, ex) != first || localRead(t, file) != content {
		t.Fatal("second write changed files")
	}
	out, _ := exec.Command("git", "-C", dir, "status", "--porcelain").Output()
	if strings.Contains(string(out), platformContextLocalFileName) {
		t.Fatalf("file visible in status: %s", out)
	}
}

func TestPlatformContextLocalTrackedRefused(t *testing.T) {
	dir := localRepo(t)
	file := filepath.Join(dir, platformContextLocalFileName)
	if err := os.WriteFile(file, []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	localGit(t, dir, "add", platformContextLocalFileName)
	localGit(t, dir, "commit", "-q", "-m", "x")
	w, err := WritePlatformContextLocalFile(dir, "body", 1)
	if w || err == nil || !strings.Contains(err.Error(), "tracked") {
		t.Fatalf("w=%v err=%v", w, err)
	}
	if localRead(t, file) != "mine\n" {
		t.Fatal("tracked file modified")
	}
}

func TestPlatformContextLocalKeepsOperatorText(t *testing.T) {
	dir := localRepo(t)
	file := filepath.Join(dir, platformContextLocalFileName)
	if err := os.WriteFile(file, []byte("operator notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := WritePlatformContextLocalFile(dir, "one", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := WritePlatformContextLocalFile(dir, "two", 2); err != nil {
		t.Fatal(err)
	}
	got := localRead(t, file)
	if !strings.HasPrefix(got, "operator notes\n") || !strings.Contains(got, "two") || strings.Contains(got, "one") {
		t.Fatalf("got %q", got)
	}
}

func TestPlatformContextLocalRemove(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, platformContextLocalFileName)
	if r, err := RemovePlatformContextLocalFile(dir); err != nil || r {
		t.Fatalf("missing: r=%v err=%v", r, err)
	}
	if err := os.WriteFile(file, []byte("keep me\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := WritePlatformContextLocalFile(dir, "body", 1); err != nil {
		t.Fatal(err)
	}
	if r, err := RemovePlatformContextLocalFile(dir); err != nil || !r {
		t.Fatalf("r=%v err=%v", r, err)
	}
	if got := localRead(t, file); got != "keep me\n" {
		t.Fatalf("got %q", got)
	}
	if r, err := RemovePlatformContextLocalFile(dir); err != nil || r {
		t.Fatalf("again: r=%v err=%v", r, err)
	}
	// Only the block: file is deleted.
	if err := os.WriteFile(file, []byte(block("x", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if r, err := RemovePlatformContextLocalFile(dir); err != nil || !r {
		t.Fatalf("r=%v err=%v", r, err)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("file not deleted: %v", err)
	}
}
