// Restart long-task delivery tests.
//
// Pins the no-override restart contract: when a session is restarted WITHOUT
// a task override and the backend implements llm.ResumeKeepsTask (claude-code
// does), the relaunch block must hand LaunchResume an EMPTY task string — the
// resumed conversation already holds the original task, so resending a 40 KB
// stored task would both re-prompt the live TUI and risk exceeding tmux's
// send-keys length limit. A RestartSession WITH an override (or a backend
// that does NOT implement ResumeKeepsTask) must still receive the full task
// text. The FakeTmux in this package replaces the real TmuxAPI, so none of
// these tests touch a tmux server.

package session

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/llm"
)

// 40 KB of task text — far above tmux's practical send-keys ceiling.
func longTask40KB(prefix string) string {
	return prefix + strings.Repeat("x", 40960-len(prefix))
}

// recordingBackend implements llm.Backend + llm.Resumable and records the task
// strings it is called with. It deliberately does NOT implement
// llm.ResumeKeepsTask, so it models opencode/goose/pi/aider-style backends.
type recordingBackend struct {
	name  string
	mu    sync.Mutex
	tasks []string // task strings passed to LaunchResume
}

func (b *recordingBackend) Name() string { return b.name }

func (b *recordingBackend) Launch(_ context.Context, task, _, _, _ string) error {
	b.mu.Lock()
	b.tasks = append(b.tasks, task)
	b.mu.Unlock()
	return nil
}

func (b *recordingBackend) SupportsInteractiveInput() bool { return true }
func (b *recordingBackend) Version() string                { return "test-stub" }

func (b *recordingBackend) LaunchResume(_ context.Context, task, _, _, _, resumeID string) error {
	b.mu.Lock()
	b.tasks = append(b.tasks, task)
	b.mu.Unlock()
	return nil
}

// keepsTaskStub adds the optional llm.ResumeKeepsTask interface on top of
// recordingBackend, mirroring the claude-code backend.
type keepsTaskStub struct{ *recordingBackend }

func (b keepsTaskStub) ResumeKeepsTask() bool { return true }

func lastTask(t *testing.T, b *recordingBackend) string {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.tasks) == 0 {
		t.Fatalf("backend %q: expected at least one launch call, got none", b.name)
	}
	return b.tasks[len(b.tasks)-1]
}

// TestRestartNoOverride_LongTask_EmptyForResumeKeepsTask: a plain Restart with
// no override must deliver an EMPTY task to LaunchResume when the backend
// implements ResumeKeepsTask, even though the session's stored task is 40 KB.
func TestRestartNoOverride_LongTask_EmptyForResumeKeepsTask(t *testing.T) {
	const name = "rt-longtask-keeps-v1"
	stub := &recordingBackend{name: name}
	llm.Register(keepsTaskStub{stub})

	mgr, _ := newTestManagerWithFake(t)
	task := longTask40KB("keeps-task original task: ")
	sess := &Session{
		ID:            "k001",
		FullID:        "testhost-k001",
		Hostname:      "testhost",
		TmuxSession:   "cs-k001",
		BackendFamily: name,
		State:         StateKilled,
		Task:          task,
		ProjectDir:    t.TempDir(),
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
		LogFile:       t.TempDir() + "/k001.log",
	}
	if err := mgr.store.Save(sess); err != nil {
		t.Fatalf("save: %v", err)
	}

	updated, err := mgr.Restart(context.Background(), sess.FullID)
	if err != nil {
		t.Fatalf("Restart: %v", err)
	}
	if updated.Task != task {
		t.Errorf("stored sess.Task was modified: got %d bytes, want %d bytes", len(updated.Task), len(task))
	}

	got := lastTask(t, stub)
	if got != "" {
		t.Errorf("no-override restart with ResumeKeepsTask backend: LaunchResume got %d-byte task, want empty string", len(got))
	}
}

// TestRestartSessionOverride_LongTask_Delivered: a RestartSession with a 40 KB
// override must deliver that override text to LaunchResume even for a
// ResumeKeepsTask backend, and store it as sess.Task.
func TestRestartSessionOverride_LongTask_Delivered(t *testing.T) {
	const name = "rt-longtask-keeps-v2"
	stub := &recordingBackend{name: name}
	llm.Register(keepsTaskStub{stub})

	mgr, _ := newTestManagerWithFake(t)
	override := longTask40KB("OVERRIDE task for restart: ")
	sess := &Session{
		ID:            "k002",
		FullID:        "testhost-k002",
		Hostname:      "testhost",
		TmuxSession:   "cs-k002",
		BackendFamily: name,
		State:         StateComplete,
		Task:          longTask40KB("stale original task: "),
		ProjectDir:    t.TempDir(),
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
		LogFile:       t.TempDir() + "/k002.log",
	}
	if err := mgr.store.Save(sess); err != nil {
		t.Fatalf("save: %v", err)
	}

	updated, err := mgr.RestartSession(context.Background(), sess.FullID, override)
	if err != nil {
		t.Fatalf("RestartSession: %v", err)
	}
	if updated.Task != override {
		t.Errorf("override not stored: got %d bytes, want %d bytes", len(updated.Task), len(override))
	}

	got := lastTask(t, stub)
	if got != override {
		t.Errorf("override restart: LaunchResume got %d bytes, want override of %d bytes", len(got), len(override))
	}
}

// TestRestartNoOverride_PlainBackendStillGetsStoredTask (regression guard): a
// backend that does NOT implement llm.ResumeKeepsTask (opencode, goose, pi,
// aider) must keep receiving the full stored task on a no-override restart;
// its fresh launch path has no resume, so an empty task would be a bug.
func TestRestartNoOverride_PlainBackendStillGetsStoredTask(t *testing.T) {
	const name = "rt-longtask-plain-v1"
	stub := &recordingBackend{name: name} // no ResumeKeepsTask method at all
	if _, ok := any(stub).(llm.ResumeKeepsTask); ok {
		t.Fatal("test setup: recordingBackend must NOT implement llm.ResumeKeepsTask")
	}
	if _, ok := any(stub).(llm.Resumable); !ok {
		t.Fatal("test setup: recordingBackend must implement llm.Resumable")
	}
	llm.Register(stub)

	mgr, _ := newTestManagerWithFake(t)
	task := longTask40KB("plain backend original task: ")
	sess := &Session{
		ID:            "p001",
		FullID:        "testhost-p001",
		Hostname:      "testhost",
		TmuxSession:   "cs-p001",
		BackendFamily: name,
		State:         StateKilled,
		Task:          task,
		ProjectDir:    t.TempDir(),
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
		LogFile:       t.TempDir() + "/p001.log",
	}
	if err := mgr.store.Save(sess); err != nil {
		t.Fatalf("save: %v", err)
	}

	if _, err := mgr.Restart(context.Background(), sess.FullID); err != nil {
		t.Fatalf("Restart: %v", err)
	}

	got := lastTask(t, stub)
	if got != task {
		t.Errorf("no-override restart with plain backend: LaunchResume got %d bytes, want full stored task of %d bytes", len(got), len(task))
	}
}
