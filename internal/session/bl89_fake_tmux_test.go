// BL89 — verify the Manager drives TmuxAPI through an injectable
// fake so router / server tests can exercise it without a real tmux.

package session

import (
	"testing"
	"time"
)

func newTestManagerWithFake(t *testing.T) (*Manager, *FakeTmux) {
	t.Helper()
	mgr, err := NewManager("testhost", t.TempDir(), "/bin/echo", 0)
	if err != nil {
		t.Fatal(err)
	}
	fake := mgr.WithFakeTmux()
	return mgr, fake
}

func TestBL89_FakeTmux_SendInput_UsesSettleForSchedule(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)
	mgr.SetScheduleSettleMs(200)

	_ = mgr.SaveSession(&Session{
		ID: "aa01", FullID: "testhost-aa01", TmuxSession: "cs-aa01",
		State: StateWaitingInput, UpdatedAt: time.Now(),
	})

	if err := mgr.SendInput("testhost-aa01", "hello", "schedule"); err != nil {
		t.Fatalf("SendInput: %v", err)
	}
	if fake.Count("send-keys-settle") != 1 {
		t.Errorf("schedule path should use settle variant; got calls: %+v", fake.Calls)
	}
	if fake.Count("send-keys") != 0 {
		t.Errorf("schedule path should NOT use one-shot SendKeys")
	}
}

func TestBL89_FakeTmux_SendInput_UsesOneShotForUser(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)
	mgr.SetScheduleSettleMs(200) // settle configured, but source != schedule

	_ = mgr.SaveSession(&Session{
		ID: "bb01", FullID: "testhost-bb01", TmuxSession: "cs-bb01",
		State: StateWaitingInput, UpdatedAt: time.Now(),
	})

	if err := mgr.SendInput("testhost-bb01", "hi", "web"); err != nil {
		t.Fatalf("SendInput: %v", err)
	}
	if fake.Count("send-keys") != 1 {
		t.Errorf("user path should use one-shot SendKeys; got calls: %+v", fake.Calls)
	}
	if fake.Count("send-keys-settle") != 0 {
		t.Errorf("user path should NOT use settle variant")
	}
}

func TestBL89_FakeTmux_RecordsSessionLifecycle(t *testing.T) {
	_, fake := newTestManagerWithFake(t)
	// Simulate create + kill directly through the fake.
	_ = fake.NewSessionWithSize("cs-xx", 80, 24)
	if !fake.SessionExists("cs-xx") {
		t.Error("expected cs-xx to exist after create")
	}
	_ = fake.KillSession("cs-xx")
	if fake.SessionExists("cs-xx") {
		t.Error("expected cs-xx to be gone after kill")
	}
	if fake.Count("new") != 1 || fake.Count("kill") != 1 {
		t.Errorf("lifecycle count mismatch: %+v", fake.Calls)
	}
}

func TestBL89_FakeTmux_FailNext_PropagatesError(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)
	_ = mgr.SaveSession(&Session{
		ID: "cc01", FullID: "testhost-cc01", TmuxSession: "cs-cc01",
		State: StateWaitingInput, UpdatedAt: time.Now(),
	})
	fake.FailNext = []error{errBoom{}}
	if err := mgr.SendInput("testhost-cc01", "x", "web"); err == nil {
		t.Error("expected SendInput to surface FakeTmux error")
	}
}

type errBoom struct{}

func (errBoom) Error() string { return "boom" }
