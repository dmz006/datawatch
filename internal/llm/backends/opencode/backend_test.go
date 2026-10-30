package opencode

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/llm"
	"github.com/dmz006/datawatch/internal/testhome"
)

// 2026-10-15 — operator-reported: the PWA session view for an opencode
// task showed screen after screen of the task spec's own text with no
// visible sign of real work, because the whole task was embedded inline
// in the typed command and echoed by the pty. writeTaskFile moves the
// content to a file so the typed line stays short.
func TestWriteTaskFile(t *testing.T) {
	testhome.PrivateTmp(t) // the scratch file is removed with the test's temp dir
	task := "line one\nline two with 'quotes' and \"stuff\"\nCHECKPOINT PROTOCOL etc."
	path, err := writeTaskFile("johnnyjohnny-a400", task)
	if err != nil {
		t.Fatalf("writeTaskFile: %v", err)
	}
	defer os.Remove(path) //nolint:errcheck

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not created: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != task {
		t.Errorf("file content mismatch: got %q, want %q", got, task)
	}
	if !strings.Contains(path, "johnnyjohnny-a400") {
		t.Errorf("path %q should carry the tmux session name for traceability", path)
	}
}

// buildRunCmd must produce a short typed line (not the whole task spec
// inline) and must still reference the real content via command
// substitution, so opencode receives the exact same text it always did.
func TestBuildRunCmd_KeepsTypedLineShort(t *testing.T) {
	testhome.PrivateTmp(t) // the scratch file is removed with the test's temp dir
	// supportsStandaloneFlag shells out to the real binary; pre-seed the
	// cache with a synthetic name so this test stays hermetic regardless
	// of whether opencode is installed, or which version, on the machine
	// running it (2026-10-15 — caught live: this test previously hardcoded
	// "opencode run --standalone" unconditionally and would behave
	// differently in CI, with no opencode on PATH, than on a dev machine
	// with it installed).
	standaloneFlagCache.Store("opencode-test-standalone", true)
	b := &Backend{binary: "opencode-test-standalone"}
	bigTask := strings.Repeat("this is a long task spec line.\n", 500) // ~16KB
	cmd := b.buildRunCmd("/tmp/proj", bigTask, "johnnyjohnny-xyz1")

	if len(cmd) > 2000 {
		t.Errorf("typed command is %d bytes — expected the big task text to live in a file, not be embedded inline (len should stay small regardless of task size)", len(cmd))
	}
	if !strings.Contains(cmd, "run --standalone \"$(cat '") {
		t.Errorf("expected --standalone + command substitution form, got: %s", cmd)
	}
	if !strings.Contains(cmd, "rm -f '") {
		t.Errorf("expected scratch file cleanup, got: %s", cmd)
	}
	if strings.Contains(cmd, "this is a long task spec line") {
		t.Errorf("task text leaked into the typed command — it must only live in the scratch file")
	}
}

func TestBuildRunCmd_EmptyTaskUnchanged(t *testing.T) {
	b := &Backend{binary: "opencode"}
	cmd := b.buildRunCmd("/tmp/proj", "", "johnnyjohnny-xyz1")
	want := "cd '/tmp/proj' && opencode"
	if cmd != want {
		t.Errorf("buildRunCmd empty task = %q, want %q", cmd, want)
	}
}

// 2026-10-15 — an opencode version predating --standalone must not get the
// flag injected: older `run` was already private by default (no shared
// background service existed yet), and passing an unknown flag would be a
// hard, loud failure instead of the graceful no-op this should be.
func TestBuildRunCmd_NoStandaloneFlagWhenUnsupported(t *testing.T) {
	testhome.PrivateTmp(t) // the scratch file is removed with the test's temp dir
	standaloneFlagCache.Store("opencode-test-old", false)
	b := &Backend{binary: "opencode-test-old"}
	cmd := b.buildRunCmd("/tmp/proj", "a task", "johnnyjohnny-xyz2")
	if strings.Contains(cmd, "--standalone") {
		t.Errorf("binary without --standalone support must not get the flag, got: %s", cmd)
	}
	if !strings.Contains(cmd, "run \"$(cat '") {
		t.Errorf("expected plain run + command substitution form, got: %s", cmd)
	}
}

func TestSupportsStandaloneFlag_UnknownBinaryFailsClosed(t *testing.T) {
	if supportsStandaloneFlag("opencode-test-does-not-exist-anywhere") {
		t.Error("a binary that can't even be exec'd should be treated as not supporting --standalone, not panic or default true")
	}
}

// TestLaunch_TmuxStderrSurfacesInError pins the error-visibibility
// guarantee: Launch, LaunchResume and the one-shot PromptBackend.Launch all
// send the command via the shared llm.SendKeys helper, so a tmux failure
// carries tmux's own stderr text instead of a bare "exit status 1". Driven
// with a fake tmux on PATH that prints a distinct stderr message and exits
// nonzero — no real tmux server is started. Synthetic binary names (pre-
// seeded in standaloneFlagCache) keep the test hermetic: no real opencode
// is ever exec'd.
func TestLaunch_TmuxStderrSurfacesInError(t *testing.T) {
	testhome.PrivateTmp(t) // scratch task files are removed with the test's temp dir
	dir := t.TempDir()
	const want = "FAKE-TMUX-STDERR-OPENCODE"
	fakeTmux := filepath.Join(dir, "tmux")
	script := "#!/bin/sh\necho '" + want + "' >&2\nexit 3\n"
	if err := os.WriteFile(fakeTmux, []byte(script), 0o755); err != nil { //nolint:gosec // test fixture must be executable
		t.Fatalf("write fake tmux: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	standaloneFlagCache.Store("opencode-test-stderr-1", true)
	standaloneFlagCache.Store("opencode-test-stderr-2", true)
	b := &Backend{binary: "opencode-test-stderr-1"}
	p := &PromptBackend{binary: "opencode-test-stderr-2"}

	cases := []struct {
		name string
		fn   func() error
	}{
		{"Launch", func() error { return b.Launch(context.Background(), "task", "cs-err", dir, "") }},
		{"LaunchResume", func() error { return b.LaunchResume(context.Background(), "task", "cs-err", dir, "", "res-1") }},
		{"PromptLaunch", func() error { return p.Launch(context.Background(), "task", "cs-err", dir, "") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.fn()
			if err == nil {
				t.Fatal("expected the failing fake tmux to produce an error")
			}
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error must carry tmux stderr %q, got: %v", want, err)
			}
			// The "tmux send-keys" wrapper only exists in llm.SendKeys' error
			// path — this proves these call sites go through the shared
			// helper rather than a bare exec.Command("tmux", ...).
			if !strings.Contains(err.Error(), "tmux send-keys") {
				t.Errorf("error must be wrapped by the shared llm.SendKeys helper (\"tmux send-keys\"), got: %v", err)
			}
		})
	}
}

// TestLaunch_SendKeysLengthGuardApplies pins the second shared-helper
// guarantee at these call sites: commands longer than
// llm.MaxSendKeysCommand bytes are rejected up front with a clear error
// naming the limit, and nothing is exec'd at all — no fake tmux is placed
// on PATH in this test, so any stray exec (tmux or opencode) would fail
// differently. Synthetic binary names (pre-seeded in
// standaloneFlagCache) keep the test hermetic: no real opencode is ever
// exec'd.
func TestLaunch_SendKeysLengthGuardApplies(t *testing.T) {
	testhome.PrivateTmp(t) // scratch task files are removed with the test's temp dir
	// Oversize via the project dir, which all three command builders
	// inline (`cd '...'`), even though the task text itself goes through
	// a scratch file.
	hugeDir := strings.Repeat("p", llm.MaxSendKeysCommand+1000)

	standaloneFlagCache.Store("opencode-test-guard-1", true)
	standaloneFlagCache.Store("opencode-test-guard-2", true)
	b := &Backend{binary: "opencode-test-guard-1"}
	p := &PromptBackend{binary: "opencode-test-guard-2"}

	cases := []struct {
		name string
		fn   func() error
	}{
		{"Launch", func() error { return b.Launch(context.Background(), "task", "cs-guard", hugeDir, "") }},
		{"LaunchResume", func() error { return b.LaunchResume(context.Background(), "task", "cs-guard", hugeDir, "", "res-1") }},
		{"PromptLaunch", func() error { return p.Launch(context.Background(), "task", "cs-guard", hugeDir, "") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.fn()
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
