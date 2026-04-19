// BL106 — runtime OnCrash policy enforcement tests.

package agents

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/profile"
)

// crashFixture wires a Manager whose driver fails the FIRST spawn
// only, then succeeds on every subsequent call. Used to assert the
// respawn paths actually issue a follow-up spawn.
type retryDriver struct {
	kind         string
	spawnCount   int
	failFirstN   int
	terminated   map[string]bool
}

func (r *retryDriver) Kind() string { return r.kind }
func (r *retryDriver) Spawn(_ context.Context, a *Agent) error {
	r.spawnCount++
	if r.spawnCount <= r.failFirstN {
		return errors.New("synthetic driver failure")
	}
	a.DriverInstance = "ok-" + a.ID
	a.ContainerAddr = "127.0.0.1:9999"
	return nil
}
func (r *retryDriver) Status(_ context.Context, _ *Agent) (State, error) {
	return StateReady, nil
}
func (r *retryDriver) Logs(_ context.Context, _ *Agent, _ int) (string, error) {
	return "", nil
}
func (r *retryDriver) Terminate(_ context.Context, a *Agent) error {
	if r.terminated == nil {
		r.terminated = map[string]bool{}
	}
	r.terminated[a.ID] = true
	return nil
}

func crashManager(t *testing.T, onCrash string, failN int) (*Manager, *retryDriver) {
	t.Helper()
	dir := t.TempDir()
	ps, _ := profile.NewProjectStore(filepath.Join(dir, "p.json"))
	cs, _ := profile.NewClusterStore(filepath.Join(dir, "c.json"))
	_ = ps.Create(&profile.ProjectProfile{
		Name:      "p",
		Git:       profile.GitSpec{URL: "https://g/y"},
		ImagePair: profile.ImagePair{Agent: "agent-claude"},
		Memory:    profile.MemorySpec{Mode: profile.MemorySyncBack},
		OnCrash:   onCrash,
	})
	_ = cs.Create(&profile.ClusterProfile{
		Name: "c", Kind: profile.ClusterDocker, Context: "x",
	})
	m := NewManager(ps, cs)
	d := &retryDriver{kind: "docker", failFirstN: failN}
	m.RegisterDriver(d)
	return m, d
}

func TestHandleCrash_FailParent_NoRespawn(t *testing.T) {
	m, d := crashManager(t, "", 1) // empty == fail_parent
	_, err := m.Spawn(context.Background(), SpawnRequest{
		ProjectProfile: "p", ClusterProfile: "c", Task: "x",
	})
	if err == nil {
		t.Fatal("expected error from initial spawn")
	}
	if d.spawnCount != 1 {
		t.Errorf("driver spawn count = %d, want 1 (no retry)", d.spawnCount)
	}
}

func TestHandleCrash_RespawnOnce_OneRetry(t *testing.T) {
	m, d := crashManager(t, OnCrashRespawnOnce, 1) // first call fails
	a, err := m.Spawn(context.Background(), SpawnRequest{
		ProjectProfile: "p", ClusterProfile: "c", Task: "x",
	})
	// After respawn the wrapped error still returns, but the
	// replacement agent is what the caller can use.
	if err == nil {
		t.Error("expected error wrapping the original failure")
	}
	if a == nil {
		t.Fatal("expected replacement agent on respawn")
	}
	if d.spawnCount != 2 {
		t.Errorf("driver spawn count = %d, want 2 (1 fail + 1 retry)", d.spawnCount)
	}
}

func TestHandleCrash_RespawnOnce_BudgetExhausted(t *testing.T) {
	m, d := crashManager(t, OnCrashRespawnOnce, 99) // every spawn fails
	_, _ = m.Spawn(context.Background(), SpawnRequest{
		ProjectProfile: "p", ClusterProfile: "c", Task: "x",
	})
	// Now the second top-level spawn shouldn't get its own retry —
	// the per-(project,branch,parent) counter is at 1 already.
	startCount := d.spawnCount
	_, _ = m.Spawn(context.Background(), SpawnRequest{
		ProjectProfile: "p", ClusterProfile: "c", Task: "x",
	})
	delta := d.spawnCount - startCount
	if delta != 1 {
		t.Errorf("respawn budget should have been exhausted: extra spawns = %d, want 1", delta)
	}
}

func TestHandleCrash_RespawnWithBackoff_FirstFiresImmediate(t *testing.T) {
	m, d := crashManager(t, OnCrashRespawnWithBackoff, 1)
	_, _ = m.Spawn(context.Background(), SpawnRequest{
		ProjectProfile: "p", ClusterProfile: "c", Task: "x",
	})
	if d.spawnCount != 2 {
		t.Errorf("first crash should respawn immediately: spawnCount=%d want 2", d.spawnCount)
	}
}

func TestHandleCrash_RespawnWithBackoff_SecondDeferred(t *testing.T) {
	m, d := crashManager(t, OnCrashRespawnWithBackoff, 99)
	// First call: 1 fail + 1 immediate retry (also fails) = 2 spawns.
	_, _ = m.Spawn(context.Background(), SpawnRequest{
		ProjectProfile: "p", ClusterProfile: "c", Task: "x",
	})
	if d.spawnCount != 2 {
		t.Fatalf("setup expectation broken: spawnCount=%d", d.spawnCount)
	}
	// Second top-level call right away: respawn must defer (st.count=1 already, lastAt=now).
	startCount := d.spawnCount
	_, _ = m.Spawn(context.Background(), SpawnRequest{
		ProjectProfile: "p", ClusterProfile: "c", Task: "x",
	})
	delta := d.spawnCount - startCount
	if delta != 1 {
		t.Errorf("synchronous spawns after second crash = %d, want 1 (one fail; respawn deferred to goroutine)", delta)
	}
}

// HandleCrash falls through to fail_parent semantics for unknown
// OnCrash values. Profile.Validate normally rejects those at config
// time, but defence-in-depth — call HandleCrash directly to assert
// the unknown-value branch behaves.
func TestHandleCrash_UnknownPolicy_NoRespawn(t *testing.T) {
	m, d := crashManager(t, OnCrashFailParent, 99)
	_, _ = m.Spawn(context.Background(), SpawnRequest{
		ProjectProfile: "p", ClusterProfile: "c", Task: "x",
	})
	startCount := d.spawnCount
	// Synthesise an Agent with an unsupported policy and call HandleCrash.
	a := &Agent{
		ID: "fake", ProjectProfile: "p",
		project: &profile.ProjectProfile{Name: "p", OnCrash: "shrug"},
	}
	if _, retried, _ := m.HandleCrash(context.Background(), a); retried {
		t.Error("unknown OnCrash should not retry")
	}
	if d.spawnCount != startCount {
		t.Errorf("unknown OnCrash spawned an agent: delta=%d", d.spawnCount-startCount)
	}
}

func TestRespawnBackoffFor(t *testing.T) {
	cases := []struct {
		n    int
		want time.Duration
	}{
		{0, time.Minute},
		{1, 2 * time.Minute},
		{2, 4 * time.Minute},
		{3, 8 * time.Minute},
		{4, 16 * time.Minute},
		{5, 30 * time.Minute},   // capped (32m → 30m)
		{20, 30 * time.Minute},  // also capped
		{-1, time.Minute},        // negative coerced to 0
	}
	for _, tc := range cases {
		if got := respawnBackoffFor(tc.n); got != tc.want {
			t.Errorf("respawnBackoffFor(%d) = %s, want %s", tc.n, got, tc.want)
		}
	}
}

func TestResetCrashRetries_AllowsAnotherRetry(t *testing.T) {
	m, d := crashManager(t, OnCrashRespawnOnce, 99)
	_, _ = m.Spawn(context.Background(), SpawnRequest{
		ProjectProfile: "p", ClusterProfile: "c", Task: "x",
	})
	startCount := d.spawnCount
	m.ResetCrashRetries("p", "", "")
	_, _ = m.Spawn(context.Background(), SpawnRequest{
		ProjectProfile: "p", ClusterProfile: "c", Task: "x",
	})
	delta := d.spawnCount - startCount
	if delta != 2 {
		t.Errorf("after Reset, extra spawns = %d, want 2 (1 fail + 1 retry)", delta)
	}
}
