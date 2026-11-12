package autonomous

import (
	"fmt"
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

// recordingJudgeSession is a fake judge session: it records the one prompt it
// is sent and never starts a real backend session.
type recordingJudgeSession struct {
	prompts []string
}

func (s *recordingJudgeSession) send(p string) { s.prompts = append(s.prompts, p) }

// deliverJudge mirrors the judge launcher: it picks the delivered prompt with
// judgePromptForDelivery and hands it to the fake session.
func deliverJudge(s *recordingJudgeSession, worktree, backend, prompt string, threshold int) {
	s.send(judgePromptForDelivery(worktree, "task-3000", backend, prompt, threshold))
}

// build3000LinePrompt returns a judge prompt of exactly 3,000 lines.
func build3000LinePrompt() string {
	var b strings.Builder
	for i := 1; i <= 3000; i++ {
		fmt.Fprintf(&b, "judge prompt line %d\n", i)
	}
	return b.String()
}

func TestJudgePrompt3000Lines_FileToolBackendGetsShortPrompt(t *testing.T) {
	wt := newJudgePromptRepo(t)
	prompt := build3000LinePrompt()
	if n := judgePromptLineCount(prompt); n != 3000 {
		t.Fatalf("test prompt has %d lines, want 3000", n)
	}
	for _, backend := range []string{"claude-code", "pi", "pi-prompt"} {
		s := &recordingJudgeSession{}
		deliverJudge(s, wt, backend, prompt, 500)
		if len(s.prompts) != 1 {
			t.Fatalf("%s: session got %d prompts, want 1", backend, len(s.prompts))
		}
		got := s.prompts[0]
		// (1) the session receives a prompt under 20 lines naming the file.
		if n := judgePromptLineCount(got); n >= 20 {
			t.Errorf("%s: session prompt has %d lines, want < 20", backend, n)
		}
		rel := filepath.ToSlash(filepath.Join(judgePromptDir, "task-3000-prompt.md"))
		if !strings.Contains(got, rel) {
			t.Errorf("%s: session prompt does not name %q:\n%s", backend, rel, got)
		}
		// (2) the file exists in the worktree with all 3,000 lines.
		data, err := os.ReadFile(filepath.Join(wt, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("%s: judge file missing: %v", backend, err)
		}
		if string(data) != prompt {
			t.Errorf("%s: judge file content differs from the full prompt", backend)
		}
		if n := judgePromptLineCount(string(data)); n != 3000 {
			t.Errorf("%s: judge file has %d lines, want 3000", backend, n)
		}
	}
}

func TestJudgePrompt3000Lines_NoFileToolsBackendGetsFullPrompt(t *testing.T) {
	wt := newJudgePromptRepo(t)
	prompt := build3000LinePrompt()
	for _, backend := range []string{"ollama", "openai", ""} {
		s := &recordingJudgeSession{}
		deliverJudge(s, wt, backend, prompt, 500)
		if len(s.prompts) != 1 || s.prompts[0] != prompt {
			t.Errorf("%q: expected the full 3000-line prompt", backend)
		}
		if n := judgePromptLineCount(s.prompts[0]); n != 3000 {
			t.Errorf("%q: session prompt has %d lines, want 3000", backend, n)
		}
	}
	if _, err := os.Stat(filepath.Join(wt, filepath.FromSlash(judgePromptDir))); err == nil {
		t.Errorf("no judge file should be written for a backend without file tools")
	}
}

func TestJudgePrompt3000Lines_ThresholdZeroSendsFullPrompt(t *testing.T) {
	wt := newJudgePromptRepo(t)
	prompt := build3000LinePrompt()
	s := &recordingJudgeSession{}
	deliverJudge(s, wt, "claude-code", prompt, 0)
	if len(s.prompts) != 1 || s.prompts[0] != prompt {
		t.Fatalf("threshold 0 must send the full prompt even with file tools")
	}
	if n := judgePromptLineCount(s.prompts[0]); n != 3000 {
		t.Fatalf("session prompt has %d lines, want 3000", n)
	}
	if _, err := os.Stat(filepath.Join(wt, filepath.FromSlash(judgePromptDir))); err == nil {
		t.Errorf("threshold 0 must not write a judge file")
	}
}
