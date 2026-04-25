// S13 — Manager.Spawn / Terminate observer-peer hooks.

package agents

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/dmz006/datawatch/internal/profile"
)

// fakeObserverPeers implements ObserverPeerRegistry for tests. Tracks
// every Register / Delete call so assertions can inspect the lifecycle.
type fakeObserverPeers struct {
	mu        sync.Mutex
	registers []string // names registered, in order
	deletes   []string // names deleted, in order
	failNext  atomic.Bool
	tokenSeq  int
}

func (f *fakeObserverPeers) Register(name, shape, version string, hostInfo map[string]any) (string, error) {
	if f.failNext.CompareAndSwap(true, false) {
		return "", errors.New("synthetic registry failure")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.registers = append(f.registers, name)
	f.tokenSeq++
	return name + "-token-x", nil
}

func (f *fakeObserverPeers) Delete(name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deletes = append(f.deletes, name)
	return nil
}

func (f *fakeObserverPeers) registerCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.registers)
}
func (f *fakeObserverPeers) deleteCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.deletes)
}

// observerPeerManager wires a Manager identical to crashManager but
// with the observer-peer registry installed.
func observerPeerManager(t *testing.T) (*Manager, *fakeObserverPeers, *retryDriver) {
	t.Helper()
	dir := t.TempDir()
	ps, _ := profile.NewProjectStore(filepath.Join(dir, "p.json"))
	cs, _ := profile.NewClusterStore(filepath.Join(dir, "c.json"))
	_ = ps.Create(&profile.ProjectProfile{
		Name: "p", Git: profile.GitSpec{URL: "https://g/y"},
		ImagePair: profile.ImagePair{Agent: "agent-claude"},
		Memory:    profile.MemorySpec{Mode: profile.MemorySyncBack},
	})
	_ = cs.Create(&profile.ClusterProfile{
		Name: "c", Kind: profile.ClusterDocker, Context: "x",
	})
	m := NewManager(ps, cs)
	d := &retryDriver{kind: "docker"} // never fails by default
	m.RegisterDriver(d)
	fop := &fakeObserverPeers{}
	m.ObserverPeers = fop
	return m, fop, d
}

func TestSpawn_RegistersObserverPeer(t *testing.T) {
	m, fop, _ := observerPeerManager(t)
	a, err := m.Spawn(context.Background(), SpawnRequest{
		ProjectProfile: "p", ClusterProfile: "c", Task: "x",
	})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if fop.registerCount() != 1 {
		t.Errorf("expected 1 observer-peer register, got %d", fop.registerCount())
	}
	if got := m.GetObserverPeerTokenFor(a.ID); got == "" {
		t.Errorf("expected non-empty observer peer token for %s", a.ID)
	}
}

func TestSpawn_RegistryFailureIsWarnOnly(t *testing.T) {
	m, fop, _ := observerPeerManager(t)
	fop.failNext.Store(true)
	a, err := m.Spawn(context.Background(), SpawnRequest{
		ProjectProfile: "p", ClusterProfile: "c", Task: "x",
	})
	if err != nil {
		t.Fatalf("spawn should succeed even if observer registry fails: %v", err)
	}
	if got := m.GetObserverPeerTokenFor(a.ID); got != "" {
		t.Errorf("token should be empty when register failed, got %q", got)
	}
}

func TestSpawn_DriverFailureCleansUpPeer(t *testing.T) {
	m, fop, d := observerPeerManager(t)
	d.failFirstN = 1 // first spawn fails; no respawn (default OnCrash)
	_, err := m.Spawn(context.Background(), SpawnRequest{
		ProjectProfile: "p", ClusterProfile: "c", Task: "x",
	})
	if err == nil {
		t.Fatal("expected driver-spawn failure")
	}
	// Registry should have seen one Register + one Delete (cleanup).
	if fop.registerCount() != 1 {
		t.Errorf("expected 1 register, got %d", fop.registerCount())
	}
	if fop.deleteCount() != 1 {
		t.Errorf("expected 1 cleanup delete after spawn failure, got %d", fop.deleteCount())
	}
}

func TestTerminate_DropsObserverPeer(t *testing.T) {
	m, fop, _ := observerPeerManager(t)
	a, err := m.Spawn(context.Background(), SpawnRequest{
		ProjectProfile: "p", ClusterProfile: "c", Task: "x",
	})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if err := m.Terminate(context.Background(), a.ID); err != nil {
		t.Fatalf("terminate: %v", err)
	}
	if fop.deleteCount() != 1 {
		t.Errorf("expected 1 delete from terminate, got %d", fop.deleteCount())
	}
	if got := m.GetObserverPeerTokenFor(a.ID); got != "" {
		t.Errorf("expected token cleared after terminate, got %q", got)
	}
}

func TestSpawn_NoRegistry_NoOp(t *testing.T) {
	// Manager without ObserverPeers wired — Spawn should work and
	// not panic; GetObserverPeerTokenFor returns "".
	m, _, _ := observerPeerManager(t)
	m.ObserverPeers = nil
	a, err := m.Spawn(context.Background(), SpawnRequest{
		ProjectProfile: "p", ClusterProfile: "c", Task: "x",
	})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if got := m.GetObserverPeerTokenFor(a.ID); got != "" {
		t.Errorf("expected empty token without registry, got %q", got)
	}
}
