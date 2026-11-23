package pi

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
)

// TestMain runs the package with a probe client that always answers, so
// launch tests that name a node never reach the network. Tests that exercise
// the gate install their own client.
func TestMain(m *testing.M) {
	setPreflightClient(&fakeDoer{status: 200})
	os.Exit(m.Run())
}

type fakeMarker struct {
	mu      sync.Mutex
	marked  map[string]string
	cleared []string
}

func (f *fakeMarker) MarkNodeUnhealthy(node, reason string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.marked == nil {
		f.marked = map[string]string{}
	}
	f.marked[node] = reason
}

func (f *fakeMarker) ClearNodeUnhealthy(node string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cleared = append(f.cleared, node)
}

func withGate(t *testing.T, m NodeHealthMarker, c HTTPDoer) {
	t.Helper()
	SetLaunchGate(m, func(addr string) string { return "node-x" }, func(string) bool { return true })
	setPreflightClient(c)
	t.Cleanup(func() {
		SetLaunchGate(nil, nil, nil)
		setPreflightClient(&fakeDoer{status: 200})
	})
}

func TestPreflightFailureDefersLaunch(t *testing.T) {
	mk := &fakeMarker{}
	withGate(t, mk, &fakeDoer{status: 503})
	called := false
	err := sendLaunch(context.Background(), "pi test", "cs-gate", "node-b:11434", "ollama/m", func(string) (string, error) {
		called = true
		return "true", nil
	})
	if !errors.Is(err, ErrLaunchDeferred) {
		t.Fatalf("err = %v, want ErrLaunchDeferred", err)
	}
	if called {
		t.Fatal("launch was built despite failed preflight")
	}
	if !strings.Contains(mk.marked["node-x"], "503") {
		t.Fatalf("marks = %v", mk.marked)
	}
	if len(mk.cleared) != 0 {
		t.Fatalf("cleared on failure: %v", mk.cleared)
	}
}

func TestPreflightSuccessClearsMark(t *testing.T) {
	mk := &fakeMarker{}
	withGate(t, mk, &fakeDoer{status: 200})
	if err := preflightBeforeLaunch(context.Background(), "node-b:11434", "ollama/m"); err != nil {
		t.Fatal(err)
	}
	if len(mk.cleared) != 1 || mk.cleared[0] != "node-x" || len(mk.marked) != 0 {
		t.Fatalf("cleared=%v marked=%v", mk.cleared, mk.marked)
	}
}

func TestPreflightSkippedWithoutNode(t *testing.T) {
	mk := &fakeMarker{}
	f := &fakeDoer{status: 500}
	withGate(t, mk, f)
	if err := preflightBeforeLaunch(context.Background(), "", "ollama/m"); err != nil {
		t.Fatal(err)
	}
	if f.req != nil || len(mk.marked) != 0 || len(mk.cleared) != 0 {
		t.Fatal("probe ran for a non-node launch")
	}
}

func TestPreflightSkippedForSingleNodeEntry(t *testing.T) {
	mk := &fakeMarker{}
	f := &fakeDoer{status: 500}
	SetLaunchGate(mk, func(string) string { return "node-x" }, func(string) bool { return false })
	setPreflightClient(f)
	t.Cleanup(func() {
		SetLaunchGate(nil, nil, nil)
		setPreflightClient(&fakeDoer{status: 200})
	})
	if err := preflightBeforeLaunch(context.Background(), "node-b:11434", "ollama/m"); err != nil {
		t.Fatal(err)
	}
	if f.req != nil || len(mk.marked) != 0 || len(mk.cleared) != 0 {
		t.Fatal("probe ran for a single-node entry")
	}
}

func TestPreflightRunsForPooledEntry(t *testing.T) {
	mk := &fakeMarker{}
	f := &fakeDoer{status: 500}
	withGate(t, mk, f)
	if err := preflightBeforeLaunch(context.Background(), "node-b:11434", "ollama/m"); !errors.Is(err, ErrLaunchDeferred) {
		t.Fatalf("err = %v, want ErrLaunchDeferred", err)
	}
	if f.req == nil {
		t.Fatal("probe did not run for a pooled entry")
	}
}
