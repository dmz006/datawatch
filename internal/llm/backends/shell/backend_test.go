package shell

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/llm"
)

// TestLaunch_TmuxStderrSurfacesInError pins the error-visibibility
// guarantee: Launch sends the command via the shared llm.SendKeys helper,
// so a tmux failure carries tmux's own stderr text instead of a bare
// "exit status 1". Driven with a fake tmux on PATH that prints a distinct
// stderr message and exits nonzero — no real tmux server is started. Both
// the interactive-shell branch (empty scriptPath) and the script branch
// are covered.
func TestLaunch_TmuxStderrSurfacesInError(t *testing.T) {
	dir := t.TempDir()
	const want = "FAKE-TMUX-STDERR-SHELL"
	fakeTmux := filepath.Join(dir, "tmux")
	script := "#!/bin/sh\necho '" + want + "' >&2\nexit 3\n"
	if err := os.WriteFile(fakeTmux, []byte(script), 0o755); err != nil { //nolint:gosec // test fixture must be executable
		t.Fatalf("write fake tmux: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	cases := []struct {
		name string
		b    *Backend
	}{
		{"InteractiveShell", &Backend{scriptPath: ""}},
		{"Script", &Backend{scriptPath: "/nonexistent/script-xyz.sh"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.b.Launch(context.Background(), "task", "cs-err", dir, "")
			if err == nil {
				t.Fatal("expected the failing fake tmux to produce an error")
			}
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error must carry tmux stderr %q, got: %v", want, err)
			}
			// The "tmux send-keys" wrapper only exists in llm.SendKeys' error
			// path — this is what proves Launch went through the shared helper
			// rather than a bare exec.Command("tmux", ...).
			if !strings.Contains(err.Error(), "tmux send-keys") {
				t.Errorf("error must be wrapped by the shared llm.SendKeys helper (\"tmux send-keys\"), got: %v", err)
			}
		})
	}
}

// TestLaunch_SendKeysLengthGuardApplies pins the second shared-helper
// guarantee at this call site: commands longer than
// llm.MaxSendKeysCommand bytes are rejected up front with a clear error
// naming the limit, and nothing is exec'd at all — no fake tmux is placed
// on PATH in this test, so any stray exec would fail differently.
func TestLaunch_SendKeysLengthGuardApplies(t *testing.T) {
	// Oversize via the project dir, which every Launch branch inlines
	// (interactive `cd '...'` and the script form alike).
	hugeDir := strings.Repeat("p", llm.MaxSendKeysCommand+1000)
	cases := []struct {
		name string
		b    *Backend
	}{
		{"InteractiveShell", &Backend{scriptPath: ""}},
		{"Script", &Backend{scriptPath: "/nonexistent/script-xyz.sh"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.b.Launch(context.Background(), strings.Repeat("t", 100), "cs-guard", hugeDir, "")
			if err == nil {
				t.Fatal("expected the shared length guard to reject the oversize command")
			}
			if !strings.Contains(err.Error(), "command too long") {
				t.Errorf("expected a \"command too long\" guard error, got: %v", err)
			}
			if !strings.Contains(err.Error(), strconv.Itoa(llm.MaxSendKeysCommand)) {
				t.Errorf("guard error must name the %d-byte limit, got: %v", llm.MaxSendKeysCommand, err)
			}
			if strings.Contains(err.Error(), "tmux send-keys") {
				t.Errorf("guard rejection happens before any exec, so no exec error should be present, got: %v", err)
			}
		})
	}
}
