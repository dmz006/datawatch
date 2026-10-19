package llm

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// recordedArgs reads the NUL-separated argv the fake tmux recorded.
func recordedArgs(t *testing.T, argvFile string) []string {
	t.Helper()
	raw, err := os.ReadFile(argvFile)
	if err != nil {
		t.Fatalf("read recorded tmux argv: %v", err)
	}
	return strings.Split(strings.TrimSuffix(string(raw), "\x00"), "\x00")
}

// installFakeTmux drops an executable shell script named "tmux" into a
// fresh temp dir and prepends it to PATH for the duration of the test.
// Follows the exact pattern in
// internal/llm/backends/pi/backend_test.go
// (TestPromptBackend_LaunchCmdSendsSetModelToTmux): a shell script named
// `tmux` in a t.TempDir prepended to PATH via t.Setenv. No real tmux
// server is ever started.
func installFakeTmux(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	argvFile := filepath.Join(dir, "argv")
	fakeTmux := filepath.Join(dir, "tmux")
	// dir comes from t.TempDir (alphanumerics, '/', '_') so embedding it
	// in the script needs no shell escaping.
	script := "#!/bin/sh\n" + strings.ReplaceAll(body, "$ARGV_FILE", "'"+argvFile+"'") + "\n"
	if err := os.WriteFile(fakeTmux, []byte(script), 0o755); err != nil { //nolint:gosec // test fixture must be executable
		t.Fatalf("write fake tmux: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return argvFile
}

// TestSendKeys_PassesCommandThroughUnchanged proves the command string is
// passed verbatim as a single send-keys argument, followed by Enter.
func TestSendKeys_PassesCommandThroughUnchanged(t *testing.T) {
	argvFile := installFakeTmux(t, `printf '%s\0' "$@" > $ARGV_FILE`)

	const cmd = "echo 'hello world' && ls -la"
	if err := SendKeys(context.Background(), "cs-test", cmd); err != nil {
		t.Fatalf("SendKeys with a short command: %v", err)
	}

	got := recordedArgs(t, argvFile)
	want := []string{"send-keys", "-t", "cs-test", cmd, "Enter"}
	if len(got) != len(want) {
		t.Fatalf("tmux argv = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("tmux argv[%d] = %q, want %q (full argv: %v)", i, got[i], want[i], got)
		}
	}
}

// TestSendKeys_TmuxStderrIsSurfaced proves a tmux failure — e.g. its
// "command too long" rejection — is visible in the returned error instead
// of a bare "exit status 1".
func TestSendKeys_TmuxStderrIsSurfaced(t *testing.T) {
	installFakeTmux(t, `echo "command too long" >&2
exit 1`)

	err := SendKeys(context.Background(), "cs-test", "some short command")
	if err == nil {
		t.Fatal("SendKeys must return an error when tmux fails")
	}
	if !strings.Contains(err.Error(), "command too long") {
		t.Errorf("error must contain tmux's stderr text, got: %v", err)
	}
	// The original exec error must still be wrapped and findable.
	if !strings.Contains(err.Error(), "exit status 1") {
		t.Errorf("error must wrap the original exec error, got: %v", err)
	}
}

// TestSendKeys_OverLimitRejectedWithoutExecuting proves an over-limit
// command is rejected up front, with the allowed limit and the actual
// byte count in the error, and that tmux is never invoked.
func TestSendKeys_OverLimitRejectedWithoutExecuting(t *testing.T) {
	argvFile := installFakeTmux(t, `printf '%s\0' "$@" > $ARGV_FILE`)

	longCmd := strings.Repeat("a", MaxSendKeysCommand+1)
	err := SendKeys(context.Background(), "cs-test", longCmd)
	if err == nil {
		t.Fatal("SendKeys must reject a command over MaxSendKeysCommand")
	}
	if !strings.Contains(err.Error(), "12288") {
		t.Errorf("error must state the allowed byte limit, got: %v", err)
	}
	if !strings.Contains(err.Error(), fmt.Sprint(len(longCmd))) {
		t.Errorf("error must state the actual byte count %d, got: %v", len(longCmd), err)
	}
	if _, statErr := os.Stat(argvFile); !os.IsNotExist(statErr) {
		t.Errorf("fake tmux must not be invoked for an over-limit command, but it recorded argv: %q", argvFile)
	}
}

// TestSendKeys_ShortCommandSucceeds proves a command under the limit
// returns nil.
func TestSendKeys_ShortCommandSucceeds(t *testing.T) {
	installFakeTmux(t, `exit 0`)
	if err := SendKeys(context.Background(), "cs-test", "go build ./..."); err != nil {
		t.Fatalf("SendKeys with a short command must succeed, got: %v", err)
	}
}
