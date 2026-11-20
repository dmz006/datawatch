// Profile-resume wiring tests.
//
// Pins the contract for sessions that launched with a worker profile: on a
// restart or a start-with-resume, the manager must consult the profile-resume
// hook (set via Manager.SetProfileResumeFn) BEFORE falling back to the
// backend's plain LaunchResume — a profile-less resume would start the session
// with a different toolset than it had. When the hook is not set, is nil for
// the session (no stored profile, or the profile already fell back at first
// launch), or is unregistered, the manager keeps its previous profile-less
// resume behaviour, so non-profile sessions are byte-for-byte unchanged.
// The FakeTmux in this package replaces the real TmuxAPI, so none of these
// tests touch a tmux server.

package session

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/llm"
	"github.com/dmz006/datawatch/internal/workerprofile"
)

// profileResumeRecorder counts how many times the manager handed a launch to
// a profile resume function, and records the last arguments.
type profileResumeRecorder struct {
	mu        sync.Mutex
	calls     int
	lastTask  string
	lastResum string
}

func (r *profileResumeRecorder) fn() ResumeLaunchFunc {
	return func(_ context.Context, task, _, _, _, resumeID string) error {
		r.mu.Lock()
		r.calls++
		r.lastTask = task
		r.lastResum = resumeID
		r.mu.Unlock()
		return nil
	}
}

// hookFn is the hook the Server installs (internal/server): it hands out a
// resume function only for sessions that actually carry an applied profile.
func (r *profileResumeRecorder) hookFn() func(*Session) ResumeLaunchFunc {
	return func(s *Session) ResumeLaunchFunc {
		if s == nil || s.WorkerProfile == nil || s.ProfileFallbackReason != "" {
			return nil
		}
		return r.fn()
	}
}

func (r *profileResumeRecorder) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

// tasksAt is a mutex-safe snapshot of recordingBackend.tasks (the shared
// fake used by the package's restart tests).
func tasksAt(t *testing.T, b *recordingBackend) []string {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.tasks...)
}

func profileTestSession(t *testing.T, mgr *Manager, id string, withProfile bool, fellBack bool) *Session {
	t.Helper()
	sess := &Session{
		ID:            id,
		FullID:        "testhost-" + id,
		Hostname:      "testhost",
		TmuxSession:   "cs-" + id,
		BackendFamily: profileResumeStubName,
		State:         StateKilled,
		Task:          "original task text",
		ProjectDir:    t.TempDir(),
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
		LogFile:       t.TempDir() + "/" + id + ".log",
	}
	if withProfile {
		sess.WorkerProfile = &workerprofile.WorkerProfile{
			Name: "datawatch-tools",
			MCP:  workerprofile.MCPSelection{Mode: workerprofile.MCPDatawatch},
		}
		if fellBack {
			sess.ProfileFallbackReason = "unknown profile at launch; role default applied"
		}
	}
	if err := mgr.store.Save(sess); err != nil {
		t.Fatalf("save %s: %v", id, err)
	}
	return sess
}

const profileResumeStubName = "pr-resume-stub-v1"

// TestRestartProfileSession_TakesProfileResumePath: a restart of a session
// that stored an applied profile must run through the hook's resume function,
// not the backend's plain LaunchResume.
func TestRestartProfileSession_TakesProfileResumePath(t *testing.T) {
	stub := &recordingBackend{name: profileResumeStubName}
	llm.Register(keepsTaskStub{stub})

	mgr, _ := newTestManagerWithFake(t)
	rec := &profileResumeRecorder{}
	mgr.SetProfileResumeFn(rec.hookFn())

	profileTestSession(t, mgr, "pr01", true, false)

	if _, err := mgr.Restart(context.Background(), "testhost-pr01"); err != nil {
		t.Fatalf("Restart: %v", err)
	}
	if got := rec.callCount(); got != 1 {
		t.Errorf("profile resume calls = %d, want 1", got)
	}
	tasks := tasksAt(t, stub)
	if len(tasks) != 0 {
		t.Errorf("plain LaunchResume calls = %d, want 0 (profile resume must win)", len(tasks))
	}
}

// TestRestartProfileFellBackSession_StaysProfileLess: a session whose profile
// fell back at first launch must keep its profile-less resume — the hook sees
// the recorded reason and declines.
func TestRestartProfileFellBackSession_StaysProfileLess(t *testing.T) {
	stub := &recordingBackend{name: profileResumeStubName}
	llm.Register(keepsTaskStub{stub})

	mgr, _ := newTestManagerWithFake(t)
	rec := &profileResumeRecorder{}
	mgr.SetProfileResumeFn(rec.hookFn())

	profileTestSession(t, mgr, "pr02", true, true)

	if _, err := mgr.Restart(context.Background(), "testhost-pr02"); err != nil {
		t.Fatalf("Restart: %v", err)
	}
	if got := rec.callCount(); got != 0 {
		t.Errorf("profile resume calls = %d, want 0 (fell back)", got)
	}
	if got := len(tasksAt(t, stub)); got != 1 {
		t.Errorf("plain LaunchResume calls = %d, want 1", got)
	}
}

// TestRestartNoProfileSession_Unchanged (regression guard): with no stored
// profile, the hook declines and the backend's plain LaunchResume runs — the
// manager's pre-existing behaviour.
func TestRestartNoProfileSession_Unchanged(t *testing.T) {
	stub := &recordingBackend{name: profileResumeStubName}
	llm.Register(keepsTaskStub{stub})

	mgr, _ := newTestManagerWithFake(t)
	rec := &profileResumeRecorder{}
	mgr.SetProfileResumeFn(rec.hookFn())

	profileTestSession(t, mgr, "pr03", false, false)

	if _, err := mgr.Restart(context.Background(), "testhost-pr03"); err != nil {
		t.Fatalf("Restart: %v", err)
	}
	if got := rec.callCount(); got != 0 {
		t.Errorf("profile resume calls = %d, want 0 (no profile)", got)
	}
	if got := len(tasksAt(t, stub)); got != 1 {
		t.Errorf("plain LaunchResume calls = %d, want 1", got)
	}
}

// TestStartResumeProfileSession_TakesProfileResumePath: a start that carries a
// resume id and a stored profile must resume through the profile function,
// with the resume id and resume launch task handed over.
func TestStartResumeProfileSession_TakesProfileResumePath(t *testing.T) {
	stub := &recordingBackend{name: profileResumeStubName}
	llm.Register(keepsTaskStub{stub})

	mgr, _ := newTestManagerWithFake(t)
	rec := &profileResumeRecorder{}
	mgr.SetProfileResumeFn(rec.hookFn())

	_, err := mgr.Start(context.Background(), "task text", "", t.TempDir(), &StartOptions{
		ResumeID: "pr04",
		WorkerProfile: workerprofile.WorkerProfile{
			Name: "datawatch-tools",
			MCP:  workerprofile.MCPSelection{Mode: workerprofile.MCPDatawatch},
		},
		Backend: profileResumeStubName,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got := rec.callCount(); got != 1 {
		t.Errorf("profile resume calls = %d, want 1", got)
	}
	rec.mu.Lock()
	gotTask, gotResume := rec.lastTask, rec.lastResum
	rec.mu.Unlock()
	if gotTask != "task text" {
		t.Errorf("profile resume task = %q, want %q", gotTask, "task text")
	}
	if gotResume != "pr04" {
		t.Errorf("profile resume id = %q, want %q", gotResume, "pr04")
	}
}

// TestStartResumeNoProfileSession_PlainResume (regression guard): the profile
// hook is set but the session has no profile — the Resumable backend's plain
// LaunchResume must still run.
func TestStartResumeNoProfileSession_PlainResume(t *testing.T) {
	stub := &recordingBackend{name: profileResumeStubName}
	llm.Register(keepsTaskStub{stub})

	mgr, _ := newTestManagerWithFake(t)
	rec := &profileResumeRecorder{}
	mgr.SetProfileResumeFn(rec.hookFn())

	_, err := mgr.Start(context.Background(), "task text", "", t.TempDir(), &StartOptions{
		ResumeID: "pr05",
		Backend:  profileResumeStubName,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got := rec.callCount(); got != 0 {
		t.Errorf("profile resume calls = %d, want 0 (no profile)", got)
	}
	if got := len(tasksAt(t, stub)); got != 1 {
		t.Errorf("plain LaunchResume calls = %d, want 1", got)
	}
}
