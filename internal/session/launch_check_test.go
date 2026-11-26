package session

// A refused launch leaves nothing behind, and a refused restart changes nothing.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/llm"
)

var errTestRefusal = errors.New("the settings file cannot be read")

func wantRefusal(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, errTestRefusal) || !strings.Contains(err.Error(), "launch refused") {
		t.Errorf("err = %v, want a refused launch that says why", err)
	}
}

func TestRefusedStartLeavesNoPaneRecordOrLaunch(t *testing.T) {
	const name = "rt-launch-check-start"
	llm.Register(&placementRestartBackend{name: name})
	mgr, fake := newTestManagerWithFake(t)

	var checked *Session
	mgr.SetLaunchCheck(func(s *Session) error { checked = s; return errTestRefusal })
	preLaunch, launched := 0, 0
	mgr.SetOnPreLaunch(func(*Session) { preLaunch++ })

	dir := t.TempDir()
	start := func() error {
		_, err := mgr.Start(context.Background(), "do the work", "", dir, &StartOptions{
			Backend:  name,
			LaunchFn: func(context.Context, string, string, string, string) error { launched++; return nil },
		})
		return err
	}
	err := start()
	wantRefusal(t, err)
	if checked == nil || checked.BackendFamily != name || checked.ProjectDir == "" {
		t.Errorf("the check did not get the session being started: %+v", checked)
	}
	if n := len(mgr.ListSessions()); n != 0 {
		t.Errorf("%d session records after a refused start", n)
	}
	if n := fake.Count("new"); n != 0 {
		t.Errorf("%d panes made for a refused start: %+v", n, fake.Calls)
	}
	if preLaunch != 0 || launched != 0 {
		t.Errorf("pre-launch ran %d times and the backend launched %d times, want neither", preLaunch, launched)
	}

	// The same start goes through when the check allows it, so the counts
	// above can see what they count.
	mgr.SetLaunchCheck(func(*Session) error { return nil })
	if err := start(); err != nil {
		t.Fatalf("allowed start: %v", err)
	}
	if fake.Count("new") != 1 || preLaunch != 1 || len(mgr.ListSessions()) != 1 {
		t.Errorf("setup: panes %d, pre-launch %d, records %d, want 1 of each", fake.Count("new"), preLaunch, len(mgr.ListSessions()))
	}
}

func TestRefusedRestartChangesNothing(t *testing.T) {
	const name = "rt-launch-check-restart"
	llm.Register(&placementRestartBackend{name: name})

	for what, tc := range map[string]struct {
		state   State
		restart func(m *Manager, id string) error
	}{
		"restart of an ended session": {StateKilled, func(m *Manager, id string) error {
			_, err := m.Restart(context.Background(), id)
			return err
		}},
		"restart-session of an ended session": {StateComplete, func(m *Manager, id string) error {
			_, err := m.RestartSession(context.Background(), id, "another task")
			return err
		}},
		"restart-session of a running session": {StateRunning, func(m *Manager, id string) error {
			_, err := m.RestartSession(context.Background(), id, "another task")
			return err
		}},
	} {
		t.Run(what, func(t *testing.T) {
			mgr, fake := newTestManagerWithFake(t)
			sess := placementRestartRecord(t, mgr, "lc01", name)
			sess.State = tc.state
			if err := mgr.store.Save(sess); err != nil {
				t.Fatal(err)
			}
			mgr.SetLaunchCheck(func(*Session) error { return errTestRefusal })

			wantRefusal(t, tc.restart(mgr, sess.FullID))

			got, ok := mgr.GetSession(sess.FullID)
			if !ok {
				t.Fatal("the record is gone")
			}
			if got.State != tc.state || got.Task != "placement restart task" {
				t.Errorf("state %q task %q, want %q and the old task", got.State, got.Task, tc.state)
			}
			if n := fake.Count("kill") + fake.Count("new"); n != 0 {
				t.Errorf("%d panes closed or made: %+v", n, fake.Calls)
			}
			mgr.mu.Lock()
			defer mgr.mu.Unlock()
			if mgr.restartTaskOverride[sess.FullID] {
				t.Error("a task override is left for a later restart")
			}
		})
	}
}
