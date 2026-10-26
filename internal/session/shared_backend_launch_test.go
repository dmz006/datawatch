package session

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

// nodeRecordingBackend stands in for a registry backend that takes a node host
// per launch, as the pi family does. The setter lingers so a concurrent start
// has time to overwrite the field before Launch reads it.
type nodeRecordingBackend struct {
	mu       sync.Mutex
	nodeHost string
	launched map[string]string // tmux session -> node host seen at launch
}

func (b *nodeRecordingBackend) Name() string                   { return "node-recorder" }
func (b *nodeRecordingBackend) SupportsInteractiveInput() bool { return true }
func (b *nodeRecordingBackend) Version() string                { return "test" }

func (b *nodeRecordingBackend) SetNodeHost(addr string) {
	b.mu.Lock()
	b.nodeHost = addr
	b.mu.Unlock()
	time.Sleep(20 * time.Millisecond)
}

func (b *nodeRecordingBackend) Launch(_ context.Context, _, tmuxSession, _, _ string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.launched[tmuxSession] = b.nodeHost
	return nil
}

// Sessions that start at the same time must each launch against their own
// node: the backend object is shared, so the node set for one start must not
// be replaced by another start before its launch runs.
func TestConcurrentStartsLaunchOnTheirOwnNode(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)
	backend := &nodeRecordingBackend{launched: map[string]string{}}

	const sessions = 6
	var wg sync.WaitGroup
	wanted := make(map[string]string)
	var wantedMu sync.Mutex
	errs := make(chan error, sessions)
	for i := 0; i < sessions; i++ {
		addr := fmt.Sprintf("http://127.0.0.1:%d", 19300+i)
		wg.Add(1)
		go func() {
			defer wg.Done()
			sess, err := mgr.Start(context.Background(), "task", "", t.TempDir(), &StartOptions{
				BackendObj:  backend,
				LaunchFn:    backend.Launch,
				NodeAddress: addr,
			})
			if err != nil {
				errs <- err
				return
			}
			wantedMu.Lock()
			wanted[sess.TmuxSession] = addr
			wantedMu.Unlock()
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("Start: %v", err)
	}

	backend.mu.Lock()
	defer backend.mu.Unlock()
	if len(backend.launched) != sessions {
		t.Fatalf("launched %d sessions, want %d", len(backend.launched), sessions)
	}
	for tmuxSession, want := range wanted {
		if got := backend.launched[tmuxSession]; got != want {
			t.Errorf("session %s launched on %q, want %q", tmuxSession, got, want)
		}
	}
}
