// Placement start hook on restart.
//
// Manager.Restart (and so RestartSession) re-runs the placement start hook on
// the relaunched session, the same way Manager.Start does, so the restarted
// session's system tags are re-derived from its record. The hook must see the
// launch role, kind and stage the record holds, and must not run when the
// restart fails. The FakeTmux from newTestManagerWithFake replaces the real
// tmux server, so these tests never touch one.

package session

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/llm"
)

// placementRestartBackend is a launch-recording backend for the restart hook
// tests. With failLaunch set, both Launch and LaunchResume return an error, so
// Restart fails at the backend-relaunch step, before the hook point.
type placementRestartBackend struct {
	name       string
	failLaunch bool
}

func (b *placementRestartBackend) Name() string                   { return b.name }
func (b *placementRestartBackend) SupportsInteractiveInput() bool { return true }
func (b *placementRestartBackend) Version() string                { return "test-stub" }

func (b *placementRestartBackend) Launch(_ context.Context, _, _, _, _ string) error {
	if b.failLaunch {
		return errors.New("launch refused by test backend")
	}
	return nil
}

func (b *placementRestartBackend) LaunchResume(_ context.Context, _, _, _, _, _, _ string) error {
	if b.failLaunch {
		return errors.New("resume refused by test backend")
	}
	return nil
}

// placementRestartRecord returns a terminal-state session carrying the
// placement facts the hook must see: launch role planner, kind decompose and
// a PRD owner.
func placementRestartRecord(t *testing.T, mgr *Manager, id, backend string) *Session {
	t.Helper()
	sess := &Session{
		ID:            id,
		FullID:        "testhost-" + id,
		Hostname:      "testhost",
		TmuxSession:   "cs-" + id,
		BackendFamily: backend,
		State:         StateKilled,
		Task:          "placement restart task",
		ProjectDir:    t.TempDir(),
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
		LogFile:       t.TempDir() + "/" + id + ".log",
		PRDID:         "prd-placement-restart",
		LaunchRole:    "planner",
		LaunchKind:    "decompose",
		LaunchStage:   "decompose",
	}
	if err := mgr.store.Save(sess); err != nil {
		t.Fatalf("save: %v", err)
	}
	return sess
}

// Restart calls the hook once on the relaunched session, and the session the
// hook sees carries the record's launch role, kind, stage and PRD owner.
func TestPlacementRestart_HookCalledWithRecordFacts(t *testing.T) {
	const name = "rt-placement-restart-ok"
	llm.Register(&placementRestartBackend{name: name})

	mgr, _ := newTestManagerWithFake(t)
	sess := placementRestartRecord(t, mgr, "p001", name)

	var mu sync.Mutex
	var calls []*Session
	var seenRole, seenKind, seenStage, seenPRD string
	mgr.SetPlacementStartHook(func(s *Session) {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, s)
		seenRole, seenKind, seenStage, seenPRD = s.LaunchRole, s.LaunchKind, s.LaunchStage, s.PRDID
	})

	updated, err := mgr.Restart(context.Background(), sess.FullID)
	if err != nil {
		t.Fatalf("Restart: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 1 {
		t.Fatalf("hook calls after Restart = %d, want 1", len(calls))
	}
	if calls[0] != updated {
		t.Errorf("hook got session %p, want the session Restart returned %p", calls[0], updated)
	}
	if seenRole != "planner" {
		t.Errorf("hook LaunchRole = %q, want %q", seenRole, "planner")
	}
	if seenKind != "decompose" {
		t.Errorf("hook LaunchKind = %q, want %q", seenKind, "decompose")
	}
	if seenStage != "decompose" {
		t.Errorf("hook LaunchStage = %q, want %q", seenStage, "decompose")
	}
	if seenPRD != "prd-placement-restart" {
		t.Errorf("hook PRDID = %q, want %q", seenPRD, "prd-placement-restart")
	}
}

// RestartSession delegates to Restart, so it also re-runs the hook once.
func TestPlacementRestart_RestartSessionCallsHook(t *testing.T) {
	const name = "rt-placement-restart-session"
	llm.Register(&placementRestartBackend{name: name})

	mgr, _ := newTestManagerWithFake(t)
	sess := placementRestartRecord(t, mgr, "p002", name)

	calls := 0
	mgr.SetPlacementStartHook(func(*Session) { calls++ })

	if _, err := mgr.RestartSession(context.Background(), sess.FullID, ""); err != nil {
		t.Fatalf("RestartSession: %v", err)
	}
	if calls != 1 {
		t.Fatalf("hook calls after RestartSession = %d, want 1", calls)
	}
}

// A restart refused by the state guard returns an error and never calls the hook.
func TestPlacementRestart_HookNotCalledOnStateError(t *testing.T) {
	const name = "rt-placement-restart-state"
	llm.Register(&placementRestartBackend{name: name})

	mgr, _ := newTestManagerWithFake(t)
	sess := placementRestartRecord(t, mgr, "p003", name)
	sess.State = StateRunning
	if err := mgr.store.Save(sess); err != nil {
		t.Fatalf("save: %v", err)
	}

	calls := 0
	mgr.SetPlacementStartHook(func(*Session) { calls++ })

	if _, err := mgr.Restart(context.Background(), sess.FullID); err == nil {
		t.Fatal("Restart of a running session: want error, got nil")
	}
	if calls != 0 {
		t.Errorf("hook calls after failed Restart = %d, want 0", calls)
	}
}

// A restart whose backend relaunch fails returns an error and never calls the hook.
func TestPlacementRestart_HookNotCalledOnLaunchError(t *testing.T) {
	const name = "rt-placement-restart-launch-fail"
	llm.Register(&placementRestartBackend{name: name, failLaunch: true})

	mgr, _ := newTestManagerWithFake(t)
	sess := placementRestartRecord(t, mgr, "p004", name)

	calls := 0
	mgr.SetPlacementStartHook(func(*Session) { calls++ })

	if _, err := mgr.Restart(context.Background(), sess.FullID); err == nil {
		t.Fatal("Restart with failing backend: want error, got nil")
	}
	if calls != 0 {
		t.Errorf("hook calls after failed relaunch = %d, want 0", calls)
	}
}
