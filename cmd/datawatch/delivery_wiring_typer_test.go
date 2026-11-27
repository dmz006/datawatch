package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/delivery"
	"github.com/dmz006/datawatch/internal/session"
)

func TestReqTypedDaemonTyper(t *testing.T) {
	mgr, err := session.NewManager("testhost", t.TempDir(), "/bin/echo", 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mgr.Close() })
	fake := mgr.WithFakeTmux()
	s := &session.Session{ID: "aa01", FullID: "testhost-aa01", TmuxSession: "cs-testhost-aa01",
		BackendFamily: "claude-code", State: session.StateRunning, UpdatedAt: time.Now()}
	if err := mgr.SaveSession(s); err != nil {
		t.Fatal(err)
	}
	ty := newRequestTyper(deliveryDeps{sessions: mgr})
	ctx := context.Background()
	if err := ty.Notice(ctx, s.FullID, "datawatch: 1 request(s) wait for you."); err != nil {
		t.Fatal(err)
	}
	sent := len(fake.Calls)
	if sent == 0 {
		t.Fatal("no keys")
	}
	for name, err := range map[string]error{
		"unknown session": ty.Notice(ctx, "testhost-none", "x"),
		// The fake pane names no processes, so datawatch cannot see that
		// something other than a shell would read a request's text.
		"text where the shell cannot be ruled out": ty.Request(ctx, s.FullID, "r-1", "x"),
	} {
		if !errors.Is(err, delivery.ErrPromptRefused) {
			t.Errorf("%s: %v", name, err)
		}
	}
	ready, ok := ty.(interface {
		Ready(context.Context, string, bool) error
	})
	if !ok || ready.Ready(ctx, s.FullID, false) != nil || !errors.Is(ready.Ready(ctx, s.FullID, true), delivery.ErrPromptRefused) {
		t.Error("readiness must say the same as typing")
	}
	if err := newRequestTyper(deliveryDeps{}).Notice(ctx, s.FullID, "x"); !errors.Is(err, delivery.ErrPromptRefused) {
		t.Errorf("no session manager: %v", err)
	}
}
