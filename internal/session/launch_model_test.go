// A shared backend with a model setting runs each launch and restart on that
// session's own model, else the family's configured one, never on the model
// an earlier launch left behind.

package session

import (
	"context"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/llm"
)

type modelRecorder struct {
	effortRecorder
	model string
	seen  []string
}

func (b *modelRecorder) SetModel(m string) { b.model = m }
func (b *modelRecorder) Launch(_ context.Context, _, _, _, _ string) error {
	b.seen = append(b.seen, b.model)
	return nil
}
func (b *modelRecorder) LaunchResume(_ context.Context, _, _, _, _, _ string) error {
	return b.Launch(context.Background(), "", "", "", "")
}
func (b *modelRecorder) last() string {
	if len(b.seen) == 0 {
		return "(never launched)"
	}
	return b.seen[len(b.seen)-1]
}

func TestLaunchModelIsNeverInherited(t *testing.T) {
	for _, tc := range []struct{ backend, configured string }{
		{"model-singleton", ""},
		{"pi", "pi-configured"},
		{"goose", "goose-configured"},
	} {
		t.Run(tc.backend, func(t *testing.T) {
			b := &modelRecorder{effortRecorder: effortRecorder{name: tc.backend}}
			prev, prevErr := llm.Get(tc.backend)
			llm.Register(b)
			t.Cleanup(func() {
				if prevErr == nil {
					llm.Register(prev)
				}
			})
			mgr, _ := newTestManagerWithFake(t)
			cfg := &config.Config{}
			cfg.Pi.Model = "pi-configured"
			cfg.Goose.Model = "goose-configured"
			mgr.SetConfig(cfg)
			start := func(model string) *Session {
				t.Helper()
				s, err := mgr.Start(context.Background(), "task", "", t.TempDir(), &StartOptions{
					Backend: tc.backend, BackendObj: b, LaunchFn: b.Launch, Model: model,
				})
				if err != nil {
					t.Fatalf("Start: %v", err)
				}
				return s
			}
			start("named-by-another")
			start("")
			if got := b.last(); got != tc.configured {
				t.Errorf("start naming no model ran on %q, want %q", got, tc.configured)
			}

			restart := func(model string) {
				t.Helper()
				sess := &Session{
					ID: "m001", FullID: "testhost-m001", Hostname: "testhost", TmuxSession: "cs-m001",
					BackendFamily: tc.backend, Model: model, State: StateKilled, Task: "task",
					ProjectDir: t.TempDir(), CreatedAt: time.Now(), UpdatedAt: time.Now(),
					LogFile: t.TempDir() + "/m001.log",
				}
				if err := mgr.store.Save(sess); err != nil {
					t.Fatal(err)
				}
				b.model = "left-by-another"
				if _, err := mgr.Restart(context.Background(), sess.FullID); err != nil {
					t.Fatalf("Restart: %v", err)
				}
			}
			restart("")
			if got := b.last(); got != tc.configured {
				t.Errorf("restart of a record with no model ran on %q, want %q", got, tc.configured)
			}
			restart("its-own")
			if got := b.last(); got != "its-own" {
				t.Errorf("restart ran on %q, want the session's own model", got)
			}
		})
	}
}

// Two launches at once must not swap settings: a backend with a per-session
// model or permission mode takes the launch lock.
func TestSharedBackendWithModelTakesLaunchLock(t *testing.T) {
	b := &modelRecorder{effortRecorder: effortRecorder{name: "model-lock"}}
	unlock := lockSharedBackend(b)
	defer unlock()
	got := make(chan func(), 1)
	go func() { got <- lockSharedBackend(b) }()
	select {
	case u := <-got:
		u()
		t.Fatal("a second launch configured the shared backend while the first still held it")
	case <-time.After(200 * time.Millisecond):
	}
	unlock()
	select {
	case u := <-got:
		u()
	case <-time.After(5 * time.Second):
		t.Fatal("the second launch never got the lock")
	}
}
