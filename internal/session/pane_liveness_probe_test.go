// Tests for the pane liveness probe (BL355) that decides whether the AI
// process behind a tmux pane is alive. The probe reads the pane's
// foreground command through Manager.paneCommandFn and the pane's child
// processes through Manager.paneChildFn, so these tests inject fakes for
// both and never start a real tmux server or run pgrep.

package session

import (
	"context"
	"errors"
	"testing"
	"time"
)

// detachFromRealTmux makes sure nothing a test runs can reach the
// operator's tmux server: TMUX is cleared and the socket dir pointed at a
// scratch location. The tests still inject fake command/child fns, so no
// tmux exec happens at all — this is belt and braces.
func detachFromRealTmux(t *testing.T) {
	t.Helper()
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_TMPDIR", t.TempDir())
}

// probeTestSession saves a running one-shot session named cs-<suffix> and
// returns it; the caller injects the pane fakes on the manager.
func probeTestSession(t *testing.T, mgr *Manager, suffix string) *Session {
	t.Helper()
	now := time.Now()
	s := &Session{
		ID:            suffix,
		FullID:        "testhost-" + suffix,
		TmuxSession:   "cs-" + suffix,
		BackendFamily: "pi-prompt",
		OneShot:       true,
		State:         StateRunning,
		CreatedAt:     now,
		UpdatedAt:     now,
		Hostname:      "testhost",
	}
	if err := mgr.store.Save(s); err != nil {
		t.Fatalf("save: %v", err)
	}
	return s
}

// TestPaneLivenessProbe_PipelinePane pins the core rule: a pipeline pane
// (for example pi piped through tee) reports the shell as its foreground
// command, so the shell foreground is not proof of death — the pane is
// alive when the shell has a live child.
func TestPaneLivenessProbe_PipelinePane(t *testing.T) {
	detachFromRealTmux(t)
	mgr, _ := newTestManagerWithFake(t)
	sess := probeTestSession(t, mgr, "plp-pipeline")
	mgr.paneCommandFn = func(string) (string, error) { return "bash", nil }
	mgr.paneChildFn = func(string) bool { return true }

	if got := mgr.probeClaudeAlive(sess); !got {
		t.Fatal("bash pane with a live child (pipeline) should be alive, got dead")
	}
}

// TestPaneLivenessProbe_DeadPane pins the other half: for every shell the
// probe treats specially, a bare shell with no child process is dead.
func TestPaneLivenessProbe_DeadPane(t *testing.T) {
	detachFromRealTmux(t)
	mgr, _ := newTestManagerWithFake(t)
	sess := probeTestSession(t, mgr, "plp-dead")
	mgr.paneChildFn = func(string) bool { return false }

	for _, shell := range []string{"bash", "sh", "zsh", "fish", "dash", "ksh", "tcsh"} {
		t.Run(shell, func(t *testing.T) {
			mgr.paneCommandFn = func(string) (string, error) { return shell, nil }
			if got := mgr.probeClaudeAlive(sess); got {
				t.Errorf("%s pane with no child should be dead, got alive", shell)
			}
		})
	}
}

// TestPaneLivenessProbe_NonShellForeground pins the fast path: a
// foreground command that is not a shell means alive outright, without
// consulting the child-process check at all.
func TestPaneLivenessProbe_NonShellForeground(t *testing.T) {
	detachFromRealTmux(t)
	mgr, _ := newTestManagerWithFake(t)
	sess := probeTestSession(t, mgr, "plp-nonshell")
	mgr.paneChildFn = func(string) bool {
		t.Fatal("paneChildFn must not be consulted when the foreground command is not a shell")
		return false
	}

	for _, cmd := range []string{"claude", "pi"} {
		t.Run(cmd, func(t *testing.T) {
			mgr.paneCommandFn = func(string) (string, error) { return cmd, nil }
			if got := mgr.probeClaudeAlive(sess); !got {
				t.Errorf("%s foreground should be alive without a child check, got dead", cmd)
			}
		})
	}
}

// TestPaneLivenessProbe_EmptyOrFailedRead pins the failure answers: a
// command read that errors, or that yields empty/blank output, means dead —
// and the child-process check is never consulted on those paths.
func TestPaneLivenessProbe_EmptyOrFailedRead(t *testing.T) {
	detachFromRealTmux(t)
	mgr, _ := newTestManagerWithFake(t)
	sess := probeTestSession(t, mgr, "plp-readfail")
	mgr.paneChildFn = func(string) bool {
		t.Fatal("paneChildFn must not be consulted when the command read fails or is empty")
		return false
	}

	t.Run("read_error", func(t *testing.T) {
		mgr.paneCommandFn = func(string) (string, error) {
			return "", errors.New("tmux unavailable")
		}
		if got := mgr.probeClaudeAlive(sess); got {
			t.Error("command read error should be dead, got alive")
		}
	})

	t.Run("empty_output", func(t *testing.T) {
		mgr.paneCommandFn = func(string) (string, error) { return "", nil }
		if got := mgr.probeClaudeAlive(sess); got {
			t.Error("empty command output should be dead, got alive")
		}
	})

	t.Run("blank_output", func(t *testing.T) {
		mgr.paneCommandFn = func(string) (string, error) { return "   \n", nil }
		if got := mgr.probeClaudeAlive(sess); got {
			t.Error("blank command output should be dead, got alive")
		}
	})
}

// TestPaneLivenessProbe_ReconcileKeepsPipelineAlive exercises the probe end
// to end through reconcileSessions: a running one-shot pi session whose
// pane is a bash pipeline must be persisted as claude_alive=true (the PWA
// only shows its dead badge when claude_alive === false), and the
// alive-change handler must stay quiet while the session is alive. When the
// pipeline's child later exits, the probe reads dead, the stored value
// flips to false, and the handler fires exactly once.
func TestPaneLivenessProbe_ReconcileKeepsPipelineAlive(t *testing.T) {
	detachFromRealTmux(t)
	mgr, fake := newTestManagerWithFake(t)
	sess := probeTestSession(t, mgr, "plp-recon")
	if err := fake.NewSessionWithSize(sess.TmuxSession, 80, 24); err != nil {
		t.Fatalf("fake.NewSessionWithSize: %v", err)
	}

	// The pane is a bash pipeline with a live child.
	mgr.paneCommandFn = func(string) (string, error) { return "bash", nil }
	mgr.paneChildFn = func(string) bool { return true }

	handlerFires := 0
	mgr.SetClaudeAliveChangeHandler(func(*Session) { handlerFires++ })

	mgr.reconcileSessions(context.Background())

	loaded, ok := mgr.store.Get(sess.FullID)
	if !ok {
		t.Fatalf("session %s not in store after reconcile", sess.FullID)
	}
	if loaded.ClaudeAlive == nil {
		t.Fatal("ClaudeAlive is nil — the session JSON would omit claude_alive")
	}
	if !*loaded.ClaudeAlive {
		t.Errorf("claude_alive = false for a live pipeline, want true (PWA would show the dead badge)")
	}
	if handlerFires != 0 {
		t.Errorf("alive-change handler fired %d times while alive, want 0", handlerFires)
	}

	// The pipeline's child exits: the probe reads dead, and the
	// alive→dead transition fires the handler exactly once.
	mgr.paneChildFn = func(string) bool { return false }
	mgr.reconcileSessions(context.Background())

	loaded, ok = mgr.store.Get(sess.FullID)
	if !ok {
		t.Fatalf("session %s not in store after second reconcile", sess.FullID)
	}
	if loaded.ClaudeAlive == nil || *loaded.ClaudeAlive {
		t.Errorf("claude_alive = %v after the child exits, want false", loaded.ClaudeAlive)
	}
	if handlerFires != 1 {
		t.Errorf("alive-change handler fired %d times, want 1", handlerFires)
	}
}
