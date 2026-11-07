package autonomous

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func newJudgePromptRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	return dir
}

func TestWriteJudgePromptFile_ContentAndRelativePath(t *testing.T) {
	wt := newJudgePromptRepo(t)
	prompt := "judge this\nlong prompt"
	rel, err := writeJudgePromptFile(wt, "task 1/../x", prompt)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.IsAbs(rel) {
		t.Fatalf("path not relative: %s", rel)
	}
	want := filepath.Join(".datawatch", "judge", "task-1----x-prompt.md")
	if rel != want {
		t.Fatalf("rel = %q, want %q", rel, want)
	}
	got, err := os.ReadFile(filepath.Join(wt, rel))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != prompt {
		t.Fatalf("content mismatch: %q", got)
	}
	if st, _ := os.Stat(filepath.Join(wt, rel)); st.Mode().Perm() != 0o600 {
		t.Fatalf("file mode %v", st.Mode().Perm())
	}
	if st, _ := os.Stat(filepath.Join(wt, ".datawatch", "judge")); st.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode %v", st.Mode().Perm())
	}
}

func TestWriteJudgePromptFile_ExcludeAddedOnce(t *testing.T) {
	wt := newJudgePromptRepo(t)
	for i := 0; i < 2; i++ {
		if _, err := writeJudgePromptFile(wt, "a1", "p"); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(filepath.Join(wt, ".git", "info", "exclude"))
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(data), ".datawatch/judge/"); n != 1 {
		t.Fatalf("exclude line count = %d\n%s", n, data)
	}
	out, _ := exec.Command("git", "-C", wt, "status", "--porcelain").Output()
	if strings.Contains(string(out), "judge") {
		t.Fatalf("judge file not excluded: %s", out)
	}
}
