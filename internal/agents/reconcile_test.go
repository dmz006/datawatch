// BL112 — service-mode reconciler tests.

package agents

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/dmz006/datawatch/internal/profile"
)

// discoveryFakeDriver records ListLabelled calls + returns a canned roster.
type discoveryFakeDriver struct {
	kind   string
	roster []DiscoveredInstance
	err    error
	calls  int
}

func (d *discoveryFakeDriver) Kind() string                                          { return d.kind }
func (d *discoveryFakeDriver) Spawn(_ context.Context, _ *Agent) error               { return nil }
func (d *discoveryFakeDriver) Status(_ context.Context, _ *Agent) (State, error)     { return StateReady, nil }
func (d *discoveryFakeDriver) Logs(_ context.Context, _ *Agent, _ int) (string, error) {
	return "", nil
}
func (d *discoveryFakeDriver) Terminate(_ context.Context, _ *Agent) error { return nil }
func (d *discoveryFakeDriver) ListLabelled(_ context.Context, _ *profile.ClusterProfile, _ map[string]string) ([]DiscoveredInstance, error) {
	d.calls++
	return d.roster, d.err
}

// nonDiscoveryDriver doesn't implement Discovery. Used to assert the
// reconciler skips it cleanly.
type nonDiscoveryDriver struct{ kind string }

func (d *nonDiscoveryDriver) Kind() string                                          { return d.kind }
func (d *nonDiscoveryDriver) Spawn(_ context.Context, _ *Agent) error               { return nil }
func (d *nonDiscoveryDriver) Status(_ context.Context, _ *Agent) (State, error)     { return "", nil }
func (d *nonDiscoveryDriver) Logs(_ context.Context, _ *Agent, _ int) (string, error) {
	return "", nil
}
func (d *nonDiscoveryDriver) Terminate(_ context.Context, _ *Agent) error { return nil }

func reconcileFixture(t *testing.T, mode string) (*Manager, *discoveryFakeDriver) {
	t.Helper()
	dir := t.TempDir()
	ps, _ := profile.NewProjectStore(filepath.Join(dir, "p.json"))
	cs, _ := profile.NewClusterStore(filepath.Join(dir, "c.json"))
	_ = ps.Create(&profile.ProjectProfile{
		Name:      "p-svc",
		Git:       profile.GitSpec{URL: "https://g/y"},
		ImagePair: profile.ImagePair{Agent: "agent-claude"},
		Memory:    profile.MemorySpec{Mode: profile.MemorySyncBack},
		Mode:      mode,
	})
	_ = cs.Create(&profile.ClusterProfile{Name: "c1", Kind: profile.ClusterDocker, Context: "x"})
	m := NewManager(ps, cs)
	d := &discoveryFakeDriver{kind: "docker"}
	m.RegisterDriver(d)
	return m, d
}

func TestReconcileServiceMode_ReattachesService(t *testing.T) {
	m, d := reconcileFixture(t, "service")
	d.roster = []DiscoveredInstance{{
		DriverInstance: "fake-cid",
		AgentID:        "rea-1",
		ProjectProfile: "p-svc",
		ClusterProfile: "c1",
		Branch:         "main",
		Addr:           "10.0.0.1:8080",
	}}
	res, err := m.ReconcileServiceMode(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Reattached) != 1 || res.Reattached[0] != "rea-1" {
		t.Errorf("reattached=%v want [rea-1]", res.Reattached)
	}
	got := m.Get("rea-1")
	if got == nil {
		t.Fatal("agent not in registry")
	}
	if got.State != StateReady {
		t.Errorf("state=%s want ready", got.State)
	}
	if got.DriverInstance != "fake-cid" || got.ContainerAddr != "10.0.0.1:8080" {
		t.Errorf("driver/addr not populated: %+v", got)
	}
}

func TestReconcileServiceMode_SkipsEphemeral(t *testing.T) {
	m, d := reconcileFixture(t, "ephemeral")
	d.roster = []DiscoveredInstance{{
		AgentID: "orph-1", ProjectProfile: "p-svc", ClusterProfile: "c1",
	}}
	res, err := m.ReconcileServiceMode(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Orphans) != 1 || res.Orphans[0] != "orph-1" {
		t.Errorf("orphans=%v want [orph-1]", res.Orphans)
	}
	if m.Get("orph-1") != nil {
		t.Error("ephemeral worker should NOT be re-attached")
	}
}

func TestReconcileServiceMode_KnownAgentNotDuplicated(t *testing.T) {
	m, d := reconcileFixture(t, "service")
	// Pre-register the agent (simulates a registered worker).
	m.mu.Lock()
	m.agents["already"] = &Agent{ID: "already", State: StateReady}
	m.mu.Unlock()
	d.roster = []DiscoveredInstance{{
		AgentID: "already", ProjectProfile: "p-svc", ClusterProfile: "c1",
	}}
	res, _ := m.ReconcileServiceMode(context.Background())
	if len(res.Reattached) != 0 {
		t.Errorf("reattached=%v want empty (already known)", res.Reattached)
	}
}

func TestReconcileServiceMode_NonDiscoveryDriverSkipped(t *testing.T) {
	dir := t.TempDir()
	ps, _ := profile.NewProjectStore(filepath.Join(dir, "p.json"))
	cs, _ := profile.NewClusterStore(filepath.Join(dir, "c.json"))
	m := NewManager(ps, cs)
	m.RegisterDriver(&nonDiscoveryDriver{kind: "stubcloud"})

	res, err := m.ReconcileServiceMode(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.SkippedKinds) != 1 || res.SkippedKinds[0] != "stubcloud" {
		t.Errorf("skipped=%v want [stubcloud]", res.SkippedKinds)
	}
}

func TestReconcileServiceMode_DriverErrorNonFatal(t *testing.T) {
	m, d := reconcileFixture(t, "service")
	d.err = errors.New("transient kubectl failure")
	res, err := m.ReconcileServiceMode(context.Background())
	if err != nil {
		t.Fatalf("Reconcile must not abort on per-driver error: %v", err)
	}
	if len(res.Errors) == 0 {
		t.Error("expected the driver error to be recorded")
	}
}

func TestReconcileServiceMode_MissingProjectIsOrphan(t *testing.T) {
	m, d := reconcileFixture(t, "service")
	// Discovery returns a worker tagged with a profile we never created.
	d.roster = []DiscoveredInstance{{
		AgentID: "ghost", ProjectProfile: "deleted-profile", ClusterProfile: "c1",
	}}
	res, _ := m.ReconcileServiceMode(context.Background())
	if len(res.Orphans) != 1 || res.Orphans[0] != "ghost" {
		t.Errorf("orphans=%v want [ghost]", res.Orphans)
	}
}

func TestReconcileServiceMode_MissingAgentIDLabelIgnored(t *testing.T) {
	m, d := reconcileFixture(t, "service")
	d.roster = []DiscoveredInstance{
		{ProjectProfile: "p-svc", ClusterProfile: "c1"}, // no AgentID
		{AgentID: "real", ProjectProfile: "p-svc", ClusterProfile: "c1"},
	}
	res, _ := m.ReconcileServiceMode(context.Background())
	if len(res.Reattached) != 1 || res.Reattached[0] != "real" {
		t.Errorf("reattached=%v want [real] (no-id row should be ignored)", res.Reattached)
	}
}
