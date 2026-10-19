// BL416 Phase 3 — runVerificationScript / truncateScriptOutput tests.
package autonomous

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeScript(t *testing.T, dir, rel, body string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRunVerificationScript_ExitZeroCapturesStdout(t *testing.T) {
	dir := t.TempDir()
	writeScript(t, dir, ".datawatch/verify/ok.sh", "echo all good\n")
	out, code, err := runVerificationScript(".datawatch/verify/ok.sh", dir)
	if err != nil || code != 0 {
		t.Fatalf("code=%d err=%v", code, err)
	}
	if !strings.Contains(out, "all good") {
		t.Fatalf("stdout not captured: %q", out)
	}
}

func TestRunVerificationScript_NonZeroExitIsNotError(t *testing.T) {
	dir := t.TempDir()
	writeScript(t, dir, "fail.sh", "echo boom >&2; exit 3\n")
	out, code, err := runVerificationScript("fail.sh", dir)
	if err != nil {
		t.Fatalf("script's own non-zero exit must not be an error: %v", err)
	}
	if code != 3 {
		t.Fatalf("exitCode=%d, want 3", code)
	}
	if !strings.Contains(out, "boom") {
		t.Fatalf("stderr not captured: %q", out)
	}
}

// A script the unit names but that is absent from the worktree is a
// runner failure: RunVerificationScript returns
// ErrVerificationScriptMissing (wrapped) instead of exec'ing bash.
func TestRunVerificationScript_MissingFileIsMissingScriptError(t *testing.T) {
	dir := t.TempDir()
	out, ok, err := RunVerificationScript("nope.sh", dir)
	if !errors.Is(err, ErrVerificationScriptMissing) {
		t.Fatalf("err=%v, want errors.Is(err, ErrVerificationScriptMissing)", err)
	}
	if ok {
		t.Fatalf("ok=true, want false")
	}
	if !strings.Contains(err.Error(), "nope.sh") {
		t.Fatalf("error message %q does not name the script %q", err.Error(), "nope.sh")
	}
	if out != "" {
		t.Fatalf("output=%q, want empty (nothing was exec'd)", out)
	}
}

func TestRunVerificationScript_RejectsUnsafePaths(t *testing.T) {
	root := t.TempDir()
	proj := filepath.Join(root, "proj")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(root, "ran.marker")
	body := "touch " + marker + "\n"
	writeScript(t, root, "escape.sh", body)

	for _, p := range []string{"", filepath.Join(root, "escape.sh"), "../escape.sh", "sub/../../escape.sh"} {
		_, code, err := runVerificationScript(p, proj)
		if err == nil {
			t.Errorf("path %q: want error, got nil", p)
		}
		if code != -1 {
			t.Errorf("path %q: exitCode=%d, want -1", p, code)
		}
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("unsafe script was executed (marker exists, stat err=%v)", err)
	}
}

func TestRunVerificationScript_TruncatesLargeOutput(t *testing.T) {
	dir := t.TempDir()
	// ~20KB of output ending in a recognizable tail.
	writeScript(t, dir, "big.sh", "for i in $(seq 1 2000); do echo 0123456789; done; echo THE-END\n")
	out, code, err := runVerificationScript("big.sh", dir)
	if err != nil || code != 0 {
		t.Fatalf("code=%d err=%v", code, err)
	}
	if !strings.HasPrefix(out, "[truncated: showing last 8192 of ") {
		t.Fatalf("missing truncation note: %q", out[:80])
	}
	note := out[:strings.Index(out, "\n")+1]
	if len(out)-len(note) != maxVerificationScriptOutput {
		t.Fatalf("body len=%d, want %d", len(out)-len(note), maxVerificationScriptOutput)
	}
	if !strings.HasSuffix(out, "THE-END\n") {
		t.Fatal("tail (last bytes) not kept")
	}
}

func TestTruncateScriptOutput_SmallUnchanged(t *testing.T) {
	in := []byte(strings.Repeat("x", maxVerificationScriptOutput))
	if got := truncateScriptOutput(in); got != string(in) {
		t.Fatal("output at the cap must be returned unchanged")
	}
}

func TestRunVerificationScript_RunsInProjectDir(t *testing.T) {
	dir := t.TempDir()
	writeScript(t, dir, "v/pwd.sh", "pwd\n")
	out, _, err := runVerificationScript("v/pwd.sh", dir)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.EvalSymlinks(dir)
	got, _ := filepath.EvalSymlinks(strings.TrimSpace(out))
	if got != want {
		t.Fatalf("pwd=%q, want %q", got, want)
	}
}
