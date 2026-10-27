// BL474 — prompt-mode backends are one-shot by default.
//
// A "<family>-prompt" backend is a headless one-run adapter: a caller that
// hand-starts one (without setting OneShot itself) still gets a one-shot
// session, because the decision is made at creation from the effective
// backend name. Interactive backends (claude-code, pi, opencode, goose, and
// the manager default) stay interactive, and an explicit OneShot option
// still wins for them. The decision also never rewrites existing stored
// records — it is created once at session Start.

package session

import (
	"context"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// newPromptFamilyTestManager builds the standard fake-tmux manager helper
// (bl89_fake_tmux_test.go) and gives the manager a zero-value config: the
// goose-family launch path in Start reads m.cfg unguarded, and a real daemon
// always has one. Without it the manager still works for other backends, but the goose launch path panics.
//
// The tests start sessions with a context that is never cancelled, so the
// helper also ends every monitor goroutine the manager kept when the test
// finishes. Otherwise the opencode usage tracker outlives the test and its
// first tick runs the real opencode program during a later test.
func newPromptFamilyTestManager(t *testing.T) (*Manager, *FakeTmux) {
	t.Helper()
	mgr, fake := newTestManagerWithFake(t)
	mgr.cfg = &config.Config{}
	t.Cleanup(func() {
		mgr.mu.Lock()
		defer mgr.mu.Unlock()
		for id, cancel := range mgr.monitors {
			cancel()
			delete(mgr.monitors, id)
		}
	})
	return mgr, fake
}

// stubPromptLaunch mimics a prompt-style backend: create the pane, wire the
// log pipe, return. No real backend binary or multiplexer server is involved
// because the multiplexer is the in-memory fake installed by the manager
// helper.
func stubPromptLaunch(t *testing.T, fake *FakeTmux) LaunchFunc {
	t.Helper()
	return func(_ context.Context, _, tmuxSession, _, _ string) error {
		_ = fake.NewSessionWithSize(tmuxSession, 80, 24)
		_ = fake.PipeOutput(tmuxSession, "/dev/null")
		return nil
	}
}

// isolateMultiplexerEnv keeps the test process pointed away from any real
// multiplexer socket, following the pattern in tmux_socket_test.go.
func isolateMultiplexerEnv(t *testing.T) {
	t.Helper()
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_TMPDIR", "")
}

func TestHandStartedPromptBackendIsOneShot(t *testing.T) {
	// The stub launch fn means the backend does not need to exist in the
	// registry; Start's lookup falls back to the manager default when the
	// name is not registered, and our LaunchFn performs the launch.
	for _, backend := range []string{"pi-prompt", "goose-prompt", "opencode-prompt"} {
		t.Run(backend, func(t *testing.T) {
			isolateMultiplexerEnv(t)
			mgr, fake := newPromptFamilyTestManager(t)

			started, err := mgr.Start(context.Background(), "do the hand-started job", "", t.TempDir(), &StartOptions{
				Backend:  backend,
				OneShot:  false, // a hand-started caller never sets OneShot itself
				LaunchFn: stubPromptLaunch(t, fake),
			})
			if err != nil {
				t.Fatalf("Start(%s): %v", backend, err)
			}

			// Read the stored session back through the manager and assert
			// the creation-time decision landed on the record.
			sess, ok := mgr.GetSession(started.FullID)
			if !ok {
				t.Fatalf("started session %q not found via GetSession", started.FullID)
			}
			if !sess.OneShot {
				t.Errorf("backend=%s: OneShot = false, want true for a prompt backend", backend)
			}
			if sess.BackendFamily != backend {
				t.Errorf("backend=%s: BackendFamily = %q", backend, sess.BackendFamily)
			}
			if sess.PRDID != "" {
				t.Errorf("backend=%s: PRDID = %q, want empty (no PRD linkage)", backend, sess.PRDID)
			}
			if sess.Git {
				t.Errorf("backend=%s: Git = true, want false", backend)
			}
		})
	}
}

func TestInteractiveBackendStaysInteractive(t *testing.T) {
	for _, backend := range []string{"claude-code", "pi", "opencode", "goose"} {
		t.Run(backend, func(t *testing.T) {
			isolateMultiplexerEnv(t)
			mgr, fake := newPromptFamilyTestManager(t)

			started, err := mgr.Start(context.Background(), "interactive job", "", t.TempDir(), &StartOptions{
				Backend:  backend, // OneShot left at its zero value
				LaunchFn: stubPromptLaunch(t, fake),
			})
			if err != nil {
				t.Fatalf("Start(%s): %v", backend, err)
			}
			sess, ok := mgr.GetSession(started.FullID)
			if !ok {
				t.Fatalf("started session %q not found via GetSession", started.FullID)
			}
			if sess.OneShot {
				t.Errorf("backend=%s: OneShot = true, want false for an interactive backend", backend)
			}
			if sess.BackendFamily != backend {
				t.Errorf("backend=%s: BackendFamily = %q", backend, sess.BackendFamily)
			}
		})
	}

	t.Run("manager-default-backend", func(t *testing.T) {
		isolateMultiplexerEnv(t)
		mgr, fake := newPromptFamilyTestManager(t)

		started, err := mgr.Start(context.Background(), "default backend job", "", t.TempDir(), &StartOptions{
			Backend:  "", // no backend override: manager default applies
			LaunchFn: stubPromptLaunch(t, fake),
		})
		if err != nil {
			t.Fatalf("Start(default): %v", err)
		}
		sess, ok := mgr.GetSession(started.FullID)
		if !ok {
			t.Fatalf("started session %q not found via GetSession", started.FullID)
		}
		if sess.OneShot {
			t.Error("manager default backend: OneShot = true, want false")
		}
	})

	t.Run("explicit-one-shot-on-interactive", func(t *testing.T) {
		isolateMultiplexerEnv(t)
		mgr, fake := newPromptFamilyTestManager(t)

		started, err := mgr.Start(context.Background(), "explicit one-shot job", "", t.TempDir(), &StartOptions{
			Backend:  "claude-code",
			OneShot:  true, // explicit callers keep working
			LaunchFn: stubPromptLaunch(t, fake),
		})
		if err != nil {
			t.Fatalf("Start(claude-code, OneShot): %v", err)
		}
		sess, ok := mgr.GetSession(started.FullID)
		if !ok {
			t.Fatalf("started session %q not found via GetSession", started.FullID)
		}
		if !sess.OneShot {
			t.Error("explicit OneShot on an interactive backend must be honored")
		}
	})
}

func TestExistingSessionRecordUnchanged(t *testing.T) {
	isolateMultiplexerEnv(t)
	mgr, fake := newPromptFamilyTestManager(t)

	// A pre-existing stored record: prompt backend family but OneShot false
	// (the way a record created before the default may legitimately look).
	updatedAt := time.UnixMilli(1700000000123)
	old := &Session{
		ID:            "dd01",
		FullID:        "testhost-dd01",
		BackendFamily: "pi-prompt",
		OneShot:       false,
		State:         StateWaitingInput,
		Task:          "legacy task text",
		CreatedAt:     updatedAt,
		UpdatedAt:     updatedAt,
	}
	if err := mgr.SaveSession(old); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}

	// Start an unrelated new prompt-style session on the same manager. This
	// runs the full creation path (which must only apply the decision to the
	// new record) and rewrites the store file with both records.
	if _, err := mgr.Start(context.Background(), "fresh prompt job", "", t.TempDir(), &StartOptions{
		Backend:  "pi-prompt",
		LaunchFn: stubPromptLaunch(t, fake),
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Reload the stored data through a second manager on the same data
	// directory — the manager's load path for stored sessions — and verify
	// the old record came back exactly as it was saved.
	m2, err := NewManager("testhost", mgr.dataDir, "/bin/echo", 0)
	if err != nil {
		t.Fatalf("second NewManager: %v", err)
	}
	allowTestTemp(m2)
	_ = m2.WithFakeTmux()

	got, ok := m2.GetSession("testhost-dd01")
	if !ok {
		t.Fatal("old session not found after reload")
	}
	if got.OneShot {
		t.Error("old record: OneShot flipped to true — stored records must never be rewritten at load")
	}
	if got.State != StateWaitingInput {
		t.Errorf("old record: State = %q, want %q", got.State, StateWaitingInput)
	}
	if got.BackendFamily != "pi-prompt" {
		t.Errorf("old record: BackendFamily = %q, want pi-prompt", got.BackendFamily)
	}
	if got.Task != "legacy task text" {
		t.Errorf("old record: Task = %q, want %q", got.Task, "legacy task text")
	}
	if !got.UpdatedAt.Equal(updatedAt) {
		t.Errorf("old record: UpdatedAt = %v, want %v", got.UpdatedAt, updatedAt)
	}
}
