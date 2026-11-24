package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func sentKeys(tm *FakeTmux) []string {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	var out []string
	for _, c := range tm.Calls {
		if c.Op == "send-keys" && len(c.Args) > 0 {
			out = append(out, c.Args[0])
		}
	}
	return out
}

func guardManager(t *testing.T, tm TmuxAPI, guard *bool) (*Manager, string) {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{}
	cfg.Autonomous.TmuxKillGuard = guard
	return &Manager{tmux: tm, dataDir: dir, cfg: cfg}, dir
}

// The guard is on by default: a worker pane gets the shim directory first on
// PATH, for every backend, because the scrub is the one shared launch step.
func TestScrubPaneTmuxEnv_KillGuardOnByDefault(t *testing.T) {
	tm := NewFakeTmux()
	m, dir := guardManager(t, tm, nil)

	m.scrubPaneTmuxEnv(&Session{TmuxSession: "s1", ScrubTmuxEnv: true})

	shim := filepath.Join(dir, "tmux-guard", "tmux")
	fi, err := os.Stat(shim)
	if err != nil {
		t.Fatalf("shim not installed at %s: %v", shim, err)
	}
	if fi.Mode().Perm()&0o111 == 0 {
		t.Fatalf("shim %s is not executable (%v)", shim, fi.Mode())
	}
	var sawPath bool
	for _, k := range sentKeys(tm) {
		if strings.Contains(k, "export PATH=") && strings.Contains(k, filepath.Join(dir, "tmux-guard")) {
			sawPath = true
		}
	}
	if !sawPath {
		t.Fatalf("no PATH export naming the shim dir; sent: %q", sentKeys(tm))
	}
	if got := scrubCalls(tm); got != 1 {
		t.Fatalf("env scrub must still be sent once, got %d", got)
	}
}

// An explicit autonomous.tmux_kill_guard: false installs nothing.
func TestScrubPaneTmuxEnv_KillGuardOptOut(t *testing.T) {
	tm := NewFakeTmux()
	off := false
	m, dir := guardManager(t, tm, &off)

	m.scrubPaneTmuxEnv(&Session{TmuxSession: "s1", ScrubTmuxEnv: true})

	if _, err := os.Stat(filepath.Join(dir, "tmux-guard")); err == nil {
		t.Fatal("guard disabled but the shim directory was created")
	}
	for _, k := range sentKeys(tm) {
		if strings.Contains(k, "export PATH=") {
			t.Fatalf("guard disabled but PATH was changed: %q", k)
		}
	}
	if got := scrubCalls(tm); got != 1 {
		t.Fatalf("env scrub must still be sent once, got %d", got)
	}
}

// Operator sessions are never guarded.
func TestScrubPaneTmuxEnv_KillGuardSkipsOperatorSessions(t *testing.T) {
	tm := NewFakeTmux()
	m, dir := guardManager(t, tm, nil)

	m.scrubPaneTmuxEnv(&Session{TmuxSession: "s1"})

	if _, err := os.Stat(filepath.Join(dir, "tmux-guard")); err == nil {
		t.Fatal("operator session got the shim")
	}
	if len(sentKeys(tm)) != 0 {
		t.Fatalf("operator session must send nothing, got %q", sentKeys(tm))
	}
}

// A stale or edited shim on disk is replaced by the embedded one.
func TestEnsureTmuxGuardShim_RewritesStaleCopy(t *testing.T) {
	dir := t.TempDir()
	d := filepath.Join(dir, "tmux-guard")
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "tmux"), []byte("#!/bin/sh\nexec true\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := ensureTmuxGuardShim(dir)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(got, "tmux"))
	if string(b) != tmuxGuardShimSource {
		t.Fatal("stale shim was not replaced by the embedded source")
	}
}

// Real tmux, throwaway server only. A scrubbed worker pane with the guard
// runs the incident command; the guard must refuse it before any tmux runs.
// TMUX_TMPDIR points at an empty directory so that, even if the guard failed,
// the call could only find "no server", never a live one.
func TestTmuxKillGuard_WorkerPaneRefusesBareKill(t *testing.T) {
	tt := newThrowawayTmux(t)
	m, _ := guardManager(t, tt, nil)
	m.scrubPaneTmuxEnv(&Session{ID: "guard1", TmuxSession: "w", ScrubTmuxEnv: true})

	empty := t.TempDir()
	out := t.TempDir()
	errFile := filepath.Join(out, "err")
	marker := filepath.Join(out, "rc")
	cmd := "TMUX_TMPDIR=" + empty + " tmux kill-" + "server 2> " + errFile + "; echo $? > " + marker
	if err := tt.SendKeys("w", cmd); err != nil {
		t.Fatal(err)
	}
	waitForFile(t, marker)
	b, _ := os.ReadFile(errFile)
	if !strings.Contains(string(b), "tmux kill guard") {
		t.Fatalf("the guard did not answer the bare kill; stderr: %q", b)
	}
	if !tt.alive() {
		t.Fatal("the throwaway server died")
	}
}
