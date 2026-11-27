package main

// The CLI's direct kill, used when the daemon does not answer, writes the end
// record before it closes the pane, like the daemon.

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
)

// tmuxSnapshot puts a tmux stub on PATH that copies the session files as they
// are when it is asked to close a pane, then exits with rc.
func tmuxSnapshot(t *testing.T, dataDir string, rc int) (snapshot string) {
	t.Helper()
	cp, err := exec.LookPath("cp")
	if err != nil {
		t.Skip("no cp")
	}
	bin, snapshot := t.TempDir(), t.TempDir()
	script := fmt.Sprintf("#!/bin/sh\n[ \"$1\" = kill-session ] && %s -R %s/. %s/\nexit %d\n", cp, dataDir, snapshot, rc)
	if err := os.WriteFile(filepath.Join(bin, "tmux"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	return snapshot
}

func stateIn(t *testing.T, dir, fullID string) session.State {
	t.Helper()
	st, err := session.NewStore(filepath.Join(dir, "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	got, ok := st.Get(fullID)
	if !ok {
		return ""
	}
	return got.State
}

func oneRunningSession(t *testing.T) *config.Config {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, port, _ := net.SplitHostPort(l.Addr().String())
	_ = l.Close() // nothing answers there: the daemon is down
	cfg := &config.Config{DataDir: t.TempDir()}
	cfg.Server.Host = "127.0.0.1"
	cfg.Server.Port, _ = strconv.Atoi(port)
	store, err := session.NewStore(filepath.Join(cfg.DataDir, "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(&session.Session{ID: "aa11", FullID: "h-aa11", TmuxSession: "cs-stub-aa11", State: session.StateRunning}); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()
	return cfg
}

func TestCLIKillsSaveTheEndRecordBeforeClosingThePane(t *testing.T) {
	kills := map[string]func(*config.Config) error{
		"session kill":     func(c *config.Config) error { return runSessionKill(c, "aa11") },
		"session stop-all": runSessionStopAll,
	}
	for label, kill := range kills {
		t.Run(label, func(t *testing.T) {
			cfg := oneRunningSession(t)
			snapshot := tmuxSnapshot(t, cfg.DataDir, 0)
			captureStdout(t, func() {
				if err := kill(cfg); err != nil {
					t.Fatal(err)
				}
			})
			if got := stateIn(t, snapshot, "h-aa11"); got != session.StateKilled {
				t.Errorf("saved state when the pane was closed = %q, want killed", got)
			}
			if got := stateIn(t, cfg.DataDir, "h-aa11"); got != session.StateKilled {
				t.Errorf("saved state afterwards = %q, want killed", got)
			}
		})
		t.Run(label+", pane not closed", func(t *testing.T) {
			cfg := oneRunningSession(t)
			tmuxSnapshot(t, cfg.DataDir, 1)
			captureStdout(t, func() { _ = kill(cfg) })
			if got := stateIn(t, cfg.DataDir, "h-aa11"); got != session.StateRunning {
				t.Errorf("state = %q, want running: nothing was ended", got)
			}
		})
	}
}
