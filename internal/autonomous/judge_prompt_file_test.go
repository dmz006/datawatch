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

func TestJudgeBackendHasFileTools(t *testing.T) {
	for b, want := range map[string]bool{"claude-code": true, "pi": true, "pi-prompt": true, "ollama": false, "openai": false, "": false} {
		if got := judgeBackendHasFileTools(b); got != want {
			t.Errorf("judgeBackendHasFileTools(%q)=%v want %v", b, got, want)
		}
	}
}

func TestJudgePromptForDelivery(t *testing.T) {
	wt := t.TempDir()
	if out, err := exec.Command("git", "-C", wt, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	long := strings.Repeat("line\n", 50)
	// Full prompt: threshold 0, short prompt, no file tools.
	for _, c := range []struct {
		backend string
		thr     int
		p       string
	}{{"claude-code", 0, long}, {"claude-code", 100, long}, {"ollama", 10, long}} {
		if got := judgePromptForDelivery(wt, "k", c.backend, c.p, c.thr); got != c.p {
			t.Errorf("%+v: expected full prompt", c)
		}
	}
	got := judgePromptForDelivery(wt, "k", "pi", long, 10)
	if got == long || strings.Count(got, "\n")+1 > 20 {
		t.Fatalf("expected short prompt <=20 lines, got %q", got)
	}
	for _, want := range []string{".datawatch/judge/k-prompt.md", "file tools", `"outcome": "pass|warn|block"`} {
		if !strings.Contains(got, want) {
			t.Errorf("short prompt missing %q", want)
		}
	}
	b, err := os.ReadFile(filepath.Join(wt, ".datawatch/judge/k-prompt.md"))
	if err != nil || string(b) != long {
		t.Fatalf("file not written with full prompt: %v", err)
	}
}
