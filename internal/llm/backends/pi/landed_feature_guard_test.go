package pi

import (
	"context"
	"errors"
	"testing"
)

func guardGate(t *testing.T, m NodeHealthMarker, c HTTPDoer, pooled bool) {
	t.Helper()
	SetLaunchGate(m, func(string) string { return "node-x" }, func(string) bool { return pooled })
	setPreflightClient(c)
	t.Cleanup(func() {
		SetLaunchGate(nil, nil, nil)
		setPreflightClient(&fakeDoer{status: 200})
	})
}

// TestGuardPreflightRefusesUnresponsiveNode guards that a pooled node that
// fails its probe defers the launch and is marked unhealthy.
func TestGuardPreflightRefusesUnresponsiveNode(t *testing.T) {
	mk := &fakeMarker{}
	guardGate(t, mk, &fakeDoer{err: errors.New("no route")}, true)
	err := preflightBeforeLaunch(context.Background(), "node-b:11434", "ollama/m")
	if !errors.Is(err, ErrLaunchDeferred) {
		t.Fatalf("err = %v, want ErrLaunchDeferred", err)
	}
	if mk.marked["node-x"] == "" || len(mk.cleared) != 0 {
		t.Fatalf("marked=%v cleared=%v", mk.marked, mk.cleared)
	}
}

// TestGuardPreflightPassesHealthyNode guards that a healthy node launches and
// has its mark cleared.
func TestGuardPreflightPassesHealthyNode(t *testing.T) {
	mk := &fakeMarker{}
	guardGate(t, mk, &fakeDoer{status: 200}, true)
	if err := preflightBeforeLaunch(context.Background(), "node-b:11434", "ollama/m"); err != nil {
		t.Fatal(err)
	}
	if len(mk.cleared) != 1 || len(mk.marked) != 0 {
		t.Fatalf("marked=%v cleared=%v", mk.marked, mk.cleared)
	}
}

// TestGuardPreflightSkippedForSingleNodeEntry guards that a single-node entry
// makes no node call and no mark.
func TestGuardPreflightSkippedForSingleNodeEntry(t *testing.T) {
	mk := &fakeMarker{}
	f := &fakeDoer{status: 500}
	guardGate(t, mk, f, false)
	if err := preflightBeforeLaunch(context.Background(), "node-b:11434", "ollama/m"); err != nil {
		t.Fatal(err)
	}
	if f.req != nil || len(mk.marked) != 0 || len(mk.cleared) != 0 {
		t.Fatal("probe ran for a single-node entry")
	}
}
