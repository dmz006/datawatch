package autonomous

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// driftCleanEnv removes the terminal-multiplexer variables and every
// inherited GIT_ variable for the duration of the test, then points git's
// global configuration at an empty file inside a temp directory.
func driftCleanEnv(t *testing.T) {
	t.Helper()
	for _, kv := range os.Environ() {
		name, val, _ := strings.Cut(kv, "=")
		if name == "TMUX" || name == "TMUX_TMPDIR" || strings.HasPrefix(name, "GIT_") {
			n, v := name, val
			if err := os.Unsetenv(n); err != nil {
				t.Fatalf("unsetenv %s: %v", n, err)
			}
			t.Cleanup(func() { _ = os.Setenv(n, v) })
		}
	}
	cfg := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(cfg, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", cfg)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
}

func swapDriftGitExec(t *testing.T, fn func(ctx context.Context, argv []string) (string, error)) {
	t.Helper()
	orig := driftGitExec
	driftGitExec = fn
	t.Cleanup(func() { driftGitExec = orig })
}

func TestDriftGitHardeningArgs(t *testing.T) {
	driftCleanEnv(t)
	realExec := driftGitExec // the default seam, captured before any swap

	dir := t.TempDir()
	var got [][]string
	swapDriftGitExec(t, func(_ context.Context, argv []string) (string, error) {
		got = append(got, append([]string(nil), argv...))
		return "", nil
	})

	ctx := context.Background()
	if _, err := defaultDriftGitRunner(ctx, dir, "diff", "--name-only", "HEAD"); err != nil {
		t.Fatal(err)
	}
	if _, err := defaultDriftGitRunner(ctx, dir, "log", "abc..HEAD", "--name-only", "--format="); err != nil {
		t.Fatal(err)
	}

	prefix := []string{"-c", "core.fsmonitor=false", "-c", "core.hooksPath=", "--no-optional-locks", "-C", dir}
	want := [][]string{
		append(append([]string(nil), prefix...), "diff", "--no-ext-diff", "--name-only", "HEAD"),
		append(append([]string(nil), prefix...), "log", "abc..HEAD", "--name-only", "--format="),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("argv mismatch\n got: %q\nwant: %q", got, want)
	}
	for _, a := range got[1] {
		if a == "--no-ext-diff" {
			t.Fatalf("log argv must not carry --no-ext-diff: %q", got[1])
		}
	}

	// The real exec seam must turn terminal prompts off.
	if _, err := exec.LookPath("git"); err != nil {
		t.Log("git not found; skipping environment assertion")
		return
	}
	out, err := realExec(ctx, []string{"-c", "alias.promptenv=!printf %s \"$GIT_TERMINAL_PROMPT\"", "promptenv"})
	if err != nil {
		t.Skipf("git alias probe failed on this git: %v", err)
	}
	if strings.TrimSpace(out) != "0" {
		t.Fatalf("GIT_TERMINAL_PROMPT = %q, want 0", out)
	}
}

func TestDriftGitFsmonitorNotExecuted(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found")
	}
	driftCleanEnv(t)

	repo := t.TempDir()
	g := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = os.Environ()
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	g("init", "-q")
	g("config", "user.name", "Drift Test")
	g("config", "user.email", "drift@example.invalid")
	g("config", "commit.gpgsign", "false")
	tracked := filepath.Join(repo, "tracked.txt")
	if err := os.WriteFile(tracked, []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	g("add", "tracked.txt")
	g("commit", "-q", "-m", "initial")
	if err := os.WriteFile(tracked, []byte("two\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	marker := filepath.Join(t.TempDir(), "fsmonitor-ran")
	script := filepath.Join(t.TempDir(), "fsmon.sh")
	body := "#!/bin/sh\n: > '" + marker + "'\nexit 0\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil { // #nosec G306 -- test helper script must be executable
		t.Fatal(err)
	}
	g("config", "core.fsmonitor", script)

	// Prove the marker mechanism is live without the hardened options.
	g("diff", "--name-only", "HEAD")
	if _, err := os.Stat(marker); err != nil {
		t.Skip("this git ignores core.fsmonitor; cannot prove the hardening")
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}

	got := CapturePreTaskDirtyFiles(context.Background(), repo, nil)
	if !reflect.DeepEqual(got, []string{"tracked.txt"}) {
		t.Fatalf("dirty files = %q, want [tracked.txt]", got)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("fsmonitor script ran under hardened git (stat err: %v)", err)
	}
}

func TestDriftGitRefusesBadDir(t *testing.T) {
	calls := 0
	swapDriftGitExec(t, func(context.Context, []string) (string, error) {
		calls++
		return "", errors.New("git must not run for a bad directory")
	})

	ctx := context.Background()
	for _, dir := range []string{"relative/dir", "-C", "--version", ""} {
		if got := CapturePreTaskDirtyFiles(ctx, dir, nil); len(got) != 0 {
			t.Errorf("CapturePreTaskDirtyFiles(%q) = %q, want empty", dir, got)
		}
		files, err := CommittedFilesSince(ctx, dir, "abc1234", nil)
		if len(files) != 0 {
			t.Errorf("CommittedFilesSince(%q) files = %q, want empty", dir, files)
		}
		if err == nil || !strings.Contains(err.Error(), "invalid git directory") {
			t.Errorf("CommittedFilesSince(%q) err = %v, want invalid git directory error", dir, err)
		}
		stats, err := CommittedLineStatsSince(ctx, dir, "abc1234", nil)
		if len(stats) != 0 {
			t.Errorf("CommittedLineStatsSince(%q) stats = %v, want empty", dir, stats)
		}
		if err == nil || !strings.Contains(err.Error(), "invalid git directory") {
			t.Errorf("CommittedLineStatsSince(%q) err = %v, want invalid git directory error", dir, err)
		}
		if sha := CapturePostVerifySHA(ctx, dir, nil); sha != "" {
			t.Errorf("CapturePostVerifySHA(%q) = %q, want empty", dir, sha)
		}
	}
	if calls != 0 {
		t.Fatalf("git exec seam called %d times, want 0", calls)
	}
}
