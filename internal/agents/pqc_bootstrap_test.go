// BL95 — wiring tests for the PQC bootstrap envelope path through
// Manager.Spawn → driver env injection → Manager.ConsumeBootstrap.

package agents

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/profile"
)

func bl95Fixture(t *testing.T, pqc bool) (*Manager, *fakeDriver) {
	t.Helper()
	dir := t.TempDir()
	ps, _ := profile.NewProjectStore(filepath.Join(dir, "p.json"))
	cs, _ := profile.NewClusterStore(filepath.Join(dir, "c.json"))
	_ = ps.Create(&profile.ProjectProfile{
		Name:      "p",
		Git:       profile.GitSpec{URL: "https://github.com/x/y"},
		ImagePair: profile.ImagePair{Agent: "agent-claude", Sidecar: "lang-go"},
		Memory:    profile.MemorySpec{Mode: profile.MemorySyncBack},
	})
	_ = cs.Create(&profile.ClusterProfile{
		Name:    "docker-local",
		Kind:    profile.ClusterDocker,
		Context: "local",
	})
	m := NewManager(ps, cs)
	m.PQCBootstrap = pqc
	d := &fakeDriver{kind: "docker"}
	m.RegisterDriver(d)
	m.RegisterDriver(&fakeDriver{kind: "k8s"})
	return m, d
}

func TestSpawn_PQCDisabled_NoKeysOnAgent(t *testing.T) {
	m, _ := bl95Fixture(t, false)
	a, err := m.Spawn(context.Background(), SpawnRequest{
		ProjectProfile: "p", ClusterProfile: "docker-local", Task: "x",
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	// cloneAgent strips PQCKeys (json:"-"); inspect the in-memory record.
	m.mu.Lock()
	defer m.mu.Unlock()
	got := m.agents[a.ID]
	if got.PQCKeys != nil {
		t.Errorf("PQCKeys should be nil when PQCBootstrap=false, got %+v", got.PQCKeys)
	}
}

func TestSpawn_PQCEnabled_KeysOnAgent(t *testing.T) {
	m, _ := bl95Fixture(t, true)
	a, err := m.Spawn(context.Background(), SpawnRequest{
		ProjectProfile: "p", ClusterProfile: "docker-local", Task: "x",
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	m.mu.Lock()
	got := m.agents[a.ID]
	m.mu.Unlock()
	if got.PQCKeys == nil {
		t.Fatal("PQCKeys should be populated when PQCBootstrap=true")
	}
	if got.PQCKeys.KEMPrivateB64 == "" || got.PQCKeys.SignPrivateB64 == "" {
		t.Errorf("PQCKeys missing private material: %+v", got.PQCKeys)
	}
}

func TestConsumeBootstrap_PQCEnvelope_Accepted(t *testing.T) {
	m, _ := bl95Fixture(t, true)
	a, err := m.Spawn(context.Background(), SpawnRequest{
		ProjectProfile: "p", ClusterProfile: "docker-local", Task: "x",
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	m.mu.Lock()
	keys := m.agents[a.ID].PQCKeys
	m.mu.Unlock()

	envelope, _, err := MakePQCBootstrapToken(a.ID, keys)
	if err != nil {
		t.Fatalf("MakePQCBootstrapToken: %v", err)
	}
	got, err := m.ConsumeBootstrap(envelope, a.ID)
	if err != nil {
		t.Fatalf("ConsumeBootstrap with envelope: %v", err)
	}
	if got.State != StateReady {
		t.Errorf("state = %s, want ready", got.State)
	}
}

func TestConsumeBootstrap_PQCEnvelope_TamperedRejected(t *testing.T) {
	m, _ := bl95Fixture(t, true)
	a, _ := m.Spawn(context.Background(), SpawnRequest{
		ProjectProfile: "p", ClusterProfile: "docker-local", Task: "x",
	})
	m.mu.Lock()
	keys := m.agents[a.ID].PQCKeys
	m.mu.Unlock()
	envelope, _, _ := MakePQCBootstrapToken(a.ID, keys)
	// Flip one byte in the signature half (after the dot).
	idx := strings.Index(envelope, ".")
	tampered := envelope[:idx+1] + "A" + envelope[idx+2:]
	if _, err := m.ConsumeBootstrap(tampered, a.ID); err == nil {
		t.Error("expected error for tampered envelope")
	}
}

func TestConsumeBootstrap_LegacyUUID_StillWorksWhenPQCEnabled(t *testing.T) {
	// Partial-rollout scenario: parent has PQC on, but the worker
	// image hasn't been rebuilt yet so it still presents the UUID.
	m, _ := bl95Fixture(t, true)
	a, _ := m.Spawn(context.Background(), SpawnRequest{
		ProjectProfile: "p", ClusterProfile: "docker-local", Task: "x",
	})
	uuid := m.BootstrapTokenForTest(a.ID)
	if uuid == "" {
		t.Fatal("UUID still expected to be minted alongside PQC")
	}
	if _, err := m.ConsumeBootstrap(uuid, a.ID); err != nil {
		t.Errorf("legacy UUID rejected: %v", err)
	}
}

func TestConsumeBootstrap_PQCKeysBurnedAfterUse(t *testing.T) {
	m, _ := bl95Fixture(t, true)
	a, _ := m.Spawn(context.Background(), SpawnRequest{
		ProjectProfile: "p", ClusterProfile: "docker-local", Task: "x",
	})
	m.mu.Lock()
	keys := m.agents[a.ID].PQCKeys
	m.mu.Unlock()
	envelope, _, _ := MakePQCBootstrapToken(a.ID, keys)
	if _, err := m.ConsumeBootstrap(envelope, a.ID); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.agents[a.ID].PQCKeys != nil {
		t.Error("PQCKeys should be zeroed after consume")
	}
}
