// The claude --effort flag of a launch comes from that launch's own request
// and nothing else. The claude-code backend is a shared singleton, so a value
// set for one session must never carry into the next one.

package session

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/llm"
)

// effortRecorder is a backend whose SetEffort value is recorded as it stands
// at each Launch, the way the claude-code backend reads it for --effort.
type effortRecorder struct {
	name     string
	mu       sync.Mutex
	current  string
	atLaunch []string
}

func (b *effortRecorder) Name() string                   { return b.name }
func (b *effortRecorder) SupportsInteractiveInput() bool { return true }
func (b *effortRecorder) Version() string                { return "test-stub" }
func (b *effortRecorder) SetEffort(e string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.current = e
}
func (b *effortRecorder) ClearEffort() { b.SetEffort("") }
func (b *effortRecorder) Launch(_ context.Context, _, _, _, _ string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.atLaunch = append(b.atLaunch, b.current)
	return nil
}
func (b *effortRecorder) LaunchResume(_ context.Context, _, _, _, _, _ string) error {
	return b.Launch(context.Background(), "", "", "", "")
}

func (b *effortRecorder) last(t *testing.T) string {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.atLaunch) == 0 {
		t.Fatal("backend was never launched")
	}
	return b.atLaunch[len(b.atLaunch)-1]
}

func startWithEffort(t *testing.T, b *effortRecorder, effort string) {
	t.Helper()
	mgr, _ := newTestManagerWithFake(t)
	if _, err := mgr.Start(context.Background(), "task", "", t.TempDir(), &StartOptions{
		Backend: b.name, BackendObj: b, LaunchFn: b.Launch, ClaudeEffort: effort, TaskID: "t1",
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}
}

// TestStart_ClaudeEffort_LaunchRequestIsAuthoritative: a launch with an effort
// gets exactly it; a later launch on the same backend that names none must
// not inherit the earlier one.
func TestStart_ClaudeEffort_LaunchRequestIsAuthoritative(t *testing.T) {
	b := &effortRecorder{name: "effort-singleton"}
	startWithEffort(t, b, "low")
	if got := b.last(t); got != "low" {
		t.Fatalf("effort at launch = %q, want low", got)
	}
	startWithEffort(t, b, "high")
	if got := b.last(t); got != "high" {
		t.Fatalf("effort at launch = %q, want high", got)
	}
	startWithEffort(t, b, "")
	if got := b.last(t); got != "" {
		t.Errorf("a launch that names no effort inherited %q from the previous session", got)
	}
}

// TestRestart_ClaudeEffort_DoesNotInheritPreviousLaunch: a restart names no
// effort, so it must not run at whatever the last other launch set.
func TestRestart_ClaudeEffort_DoesNotInheritPreviousLaunch(t *testing.T) {
	b := &effortRecorder{name: "effort-restart"}
	llm.Register(b)
	mgr, _ := newTestManagerWithFake(t)
	b.SetEffort("low") // left by an earlier session on the shared backend
	sess := &Session{
		ID: "e001", FullID: "testhost-e001", Hostname: "testhost", TmuxSession: "cs-e001",
		BackendFamily: b.name, State: StateKilled, Task: "task",
		ProjectDir: t.TempDir(), CreatedAt: time.Now(), UpdatedAt: time.Now(),
		LogFile: t.TempDir() + "/e001.log",
	}
	if err := mgr.store.Save(sess); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Restart(context.Background(), sess.FullID); err != nil {
		t.Fatalf("Restart: %v", err)
	}
	if got := b.last(t); got != "" {
		t.Errorf("restart ran with inherited effort %q", got)
	}
}
