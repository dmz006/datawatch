package shell

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
		})
	}
}
