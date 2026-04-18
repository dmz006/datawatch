package agents

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/profile"
)

// fakeDriver is a Driver stand-in that just records calls. Real
// drivers (DockerDriver, K8sDriver) have their own tests. Keeping
// the Manager orthogonal to platform behaviour here.
type fakeDriver struct {
	kind        string
	spawnCalls  int
	spawnErr    error
	terminated  map[string]bool
	lastAgentID string
}

func (f *fakeDriver) Kind() string { return f.kind }
func (f *fakeDriver) Spawn(_ context.Context, a *Agent) error {
	f.spawnCalls++
	f.lastAgentID = a.ID
	if f.spawnErr != nil {
		return f.spawnErr
	}
	a.DriverInstance = "fake-" + a.ID
	a.ContainerAddr = "127.0.0.1:9999"
	return nil
}
func (f *fakeDriver) Status(_ context.Context, _ *Agent) (State, error) { return StateReady, nil }
func (f *fakeDriver) Logs(_ context.Context, _ *Agent, _ int) (string, error) {
	return "stub log\n", nil
}
func (f *fakeDriver) Terminate(_ context.Context, a *Agent) error {
	if f.terminated == nil {
		f.terminated = map[string]bool{}
	}
	f.terminated[a.ID] = true
	return nil
}

// managerFixture wires a Manager with real profile stores (tmp-backed)
// plus a fake driver registered under docker + k8s so the store's
// cluster-kind dispatch works both ways.
// fakeMinter records mint+revoke calls and returns a deterministic
// token. Used by the Sprint 5 git-broker wiring tests below.
type fakeMinter struct {
	mintCalls   []string // workerID per call
	revokeCalls []string // workerID per call
	repos       []string // repo per mint call
	tokenSeq    int
	mintErr     error
}

func (f *fakeMinter) MintForWorker(_ context.Context, workerID, repo string, _ time.Duration) (string, error) {
	f.mintCalls = append(f.mintCalls, workerID)
	f.repos = append(f.repos, repo)
	if f.mintErr != nil {
		return "", f.mintErr
	}
	f.tokenSeq++
	return "tok-" + workerID, nil
}
func (f *fakeMinter) RevokeForWorker(_ context.Context, workerID string) error {
	f.revokeCalls = append(f.revokeCalls, workerID)
	return nil
}

func managerFixture(t *testing.T) (*Manager, *profile.ProjectStore, *profile.ClusterStore, *fakeDriver) {
	t.Helper()
	dir := t.TempDir()
	ps, err := profile.NewProjectStore(filepath.Join(dir, "p.json"))
	if err != nil {
		t.Fatal(err)
	}
	cs, err := profile.NewClusterStore(filepath.Join(dir, "c.json"))
	if err != nil {
		t.Fatal(err)
	}
	m := NewManager(ps, cs)
	// Single fakeDriver covers both docker + k8s kinds for the tests.
	d := &fakeDriver{kind: "docker"}
	m.RegisterDriver(d)
	m.RegisterDriver(&fakeDriver{kind: "k8s"})
	return m, ps, cs, d
}

func TestSpawn_HappyPath(t *testing.T) {
	m, ps, cs, d := managerFixture(t)
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

	a, err := m.Spawn(context.Background(), SpawnRequest{
		ProjectProfile: "p",
		ClusterProfile: "docker-local",
		Task:           "echo hi",
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if a.ID == "" {
		t.Errorf("agent ID empty")
	}
	if a.State != StateStarting {
		t.Errorf("state=%s want starting", a.State)
	}
	if a.DriverInstance != "fake-"+a.ID {
		t.Errorf("driver didn't set DriverInstance: %q", a.DriverInstance)
	}
	if d.spawnCalls != 1 {
		t.Errorf("driver spawn called %d times, want 1", d.spawnCalls)
	}
}

func TestSpawn_UnknownProfile(t *testing.T) {
	m, _, cs, _ := managerFixture(t)
	_ = cs.Create(&profile.ClusterProfile{
		Name:    "c",
		Kind:    profile.ClusterDocker,
		Context: "x",
	})
	_, err := m.Spawn(context.Background(), SpawnRequest{ProjectProfile: "nope", ClusterProfile: "c"})
	if err == nil || !strings.Contains(err.Error(), "project profile") {
		t.Errorf("want project-not-found, got %v", err)
	}
}

func TestSpawn_UnknownClusterKindDriver(t *testing.T) {
	m, ps, cs, _ := managerFixture(t)
	_ = ps.Create(&profile.ProjectProfile{
		Name:      "p",
		Git:       profile.GitSpec{URL: "https://github.com/x/y"},
		ImagePair: profile.ImagePair{Agent: "agent-claude"},
		Memory:    profile.MemorySpec{Mode: profile.MemorySyncBack},
	})
	// Create a CF cluster but we didn't register a CF driver
	_ = cs.Create(&profile.ClusterProfile{
		Name:    "cf-future",
		Kind:    profile.ClusterCF,
		Context: "anywhere",
	})
	_, err := m.Spawn(context.Background(), SpawnRequest{ProjectProfile: "p", ClusterProfile: "cf-future"})
	if err == nil || !strings.Contains(err.Error(), "no driver registered") {
		t.Errorf("want no-driver error, got %v", err)
	}
}

func TestSpawn_DriverError_AgentMarkedFailed(t *testing.T) {
	m, ps, cs, d := managerFixture(t)
	d.spawnErr = errors.New("image pull timeout")
	_ = ps.Create(&profile.ProjectProfile{
		Name:      "p",
		Git:       profile.GitSpec{URL: "https://github.com/x/y"},
		ImagePair: profile.ImagePair{Agent: "agent-claude"},
		Memory:    profile.MemorySpec{Mode: profile.MemorySyncBack},
	})
	_ = cs.Create(&profile.ClusterProfile{Name: "c", Kind: profile.ClusterDocker, Context: "x"})

	a, err := m.Spawn(context.Background(), SpawnRequest{ProjectProfile: "p", ClusterProfile: "c"})
	if err == nil {
		t.Fatal("want error")
	}
	// Even on error the agent record survives so the UI can show the failure
	if a == nil {
		t.Fatal("want agent even on driver error")
	}
	if a.State != StateFailed {
		t.Errorf("state=%s want failed", a.State)
	}
	if a.FailureReason == "" {
		t.Errorf("expected failure reason")
	}
	if got := m.Get(a.ID); got == nil || got.State != StateFailed {
		t.Errorf("agent not in manager or state wrong: %+v", got)
	}
}

// ── Bootstrap token tests ───────────────────────────────────────────────

func TestConsumeBootstrap_HappyPath(t *testing.T) {
	m, ps, cs, _ := managerFixture(t)
	_ = ps.Create(&profile.ProjectProfile{
		Name: "p", Git: profile.GitSpec{URL: "https://g/y"},
		ImagePair: profile.ImagePair{Agent: "agent-claude"},
		Memory:    profile.MemorySpec{Mode: profile.MemorySyncBack},
	})
	_ = cs.Create(&profile.ClusterProfile{Name: "c", Kind: profile.ClusterDocker, Context: "x"})
	a, err := m.Spawn(context.Background(), SpawnRequest{ProjectProfile: "p", ClusterProfile: "c"})
	if err != nil {
		t.Fatal(err)
	}
	// Snapshot from List shouldn't leak the token...
	listed := m.List()
	if listed[0].BootstrapToken != "" {
		t.Errorf("token leaked via snapshot: %q", listed[0].BootstrapToken)
	}
	// ...but the real internal token is on the live record (we read
	// it back from the internal map via a targeted path)
	m.mu.Lock()
	token := m.agents[a.ID].BootstrapToken
	m.mu.Unlock()
	if token == "" {
		t.Fatal("internal token empty")
	}

	bootstrapped, err := m.ConsumeBootstrap(token, a.ID)
	if err != nil {
		t.Fatalf("ConsumeBootstrap: %v", err)
	}
	if bootstrapped.State != StateReady {
		t.Errorf("after bootstrap state=%s want ready", bootstrapped.State)
	}

	// Second attempt fails (token burned)
	if _, err := m.ConsumeBootstrap(token, a.ID); err == nil {
		t.Errorf("second bootstrap should have failed")
	}
}

func TestConsumeBootstrap_WrongToken(t *testing.T) {
	m, ps, cs, _ := managerFixture(t)
	_ = ps.Create(&profile.ProjectProfile{
		Name: "p", Git: profile.GitSpec{URL: "https://g/y"},
		ImagePair: profile.ImagePair{Agent: "agent-claude"},
		Memory:    profile.MemorySpec{Mode: profile.MemorySyncBack},
	})
	_ = cs.Create(&profile.ClusterProfile{Name: "c", Kind: profile.ClusterDocker, Context: "x"})
	a, _ := m.Spawn(context.Background(), SpawnRequest{ProjectProfile: "p", ClusterProfile: "c"})
	if _, err := m.ConsumeBootstrap("bad", a.ID); err == nil {
		t.Errorf("wrong token should fail")
	}
}

func TestConsumeBootstrap_UnknownAgent(t *testing.T) {
	m, _, _, _ := managerFixture(t)
	if _, err := m.ConsumeBootstrap("anything", "nope"); err == nil {
		t.Errorf("unknown agent should fail")
	}
}

// ── Session binding ────────────────────────────────────────────────────

func TestMarkSessionBound_TransitionsReadyToRunning(t *testing.T) {
	m, ps, cs, _ := managerFixture(t)
	_ = ps.Create(&profile.ProjectProfile{
		Name: "p", Git: profile.GitSpec{URL: "https://g/y"},
		ImagePair: profile.ImagePair{Agent: "agent-claude"},
		Memory:    profile.MemorySpec{Mode: profile.MemorySyncBack},
	})
	_ = cs.Create(&profile.ClusterProfile{Name: "c", Kind: profile.ClusterDocker, Context: "x"})
	a, _ := m.Spawn(context.Background(), SpawnRequest{ProjectProfile: "p", ClusterProfile: "c"})

	m.mu.Lock()
	token := m.agents[a.ID].BootstrapToken
	m.mu.Unlock()
	_, _ = m.ConsumeBootstrap(token, a.ID)

	if err := m.MarkSessionBound(a.ID, "sess-1"); err != nil {
		t.Fatalf("MarkSessionBound: %v", err)
	}
	snap := m.Get(a.ID)
	if snap.State != StateRunning {
		t.Errorf("after binding state=%s want running", snap.State)
	}
	if len(snap.SessionIDs) != 1 || snap.SessionIDs[0] != "sess-1" {
		t.Errorf("sessions=%v want [sess-1]", snap.SessionIDs)
	}

	// Duplicate bind is idempotent
	if err := m.MarkSessionBound(a.ID, "sess-1"); err != nil {
		t.Errorf("idempotent bind failed: %v", err)
	}
	if len(m.Get(a.ID).SessionIDs) != 1 {
		t.Errorf("duplicate bind added a second entry")
	}
}

// ── Terminate ──────────────────────────────────────────────────────────

func TestTerminate_CallsDriverAndMarksStopped(t *testing.T) {
	m, ps, cs, d := managerFixture(t)
	_ = ps.Create(&profile.ProjectProfile{
		Name: "p", Git: profile.GitSpec{URL: "https://g/y"},
		ImagePair: profile.ImagePair{Agent: "agent-claude"},
		Memory:    profile.MemorySpec{Mode: profile.MemorySyncBack},
	})
	_ = cs.Create(&profile.ClusterProfile{Name: "c", Kind: profile.ClusterDocker, Context: "x"})
	a, _ := m.Spawn(context.Background(), SpawnRequest{ProjectProfile: "p", ClusterProfile: "c"})

	if err := m.Terminate(context.Background(), a.ID); err != nil {
		t.Fatalf("Terminate: %v", err)
	}
	if !d.terminated[a.ID] {
		t.Errorf("driver.Terminate not called")
	}
	snap := m.Get(a.ID)
	if snap.State != StateStopped {
		t.Errorf("state=%s want stopped", snap.State)
	}
	if snap.StoppedAt.IsZero() {
		t.Errorf("stopped_at not set")
	}
}

func TestTerminate_UnknownAgent(t *testing.T) {
	m, _, _, _ := managerFixture(t)
	if err := m.Terminate(context.Background(), "nope"); err == nil {
		t.Errorf("terminate unknown should fail")
	}
}

// ── List order ────────────────────────────────────────────────────────

func TestList_CreationTimeOrder(t *testing.T) {
	m, ps, cs, _ := managerFixture(t)
	_ = ps.Create(&profile.ProjectProfile{
		Name: "p", Git: profile.GitSpec{URL: "https://g/y"},
		ImagePair: profile.ImagePair{Agent: "agent-claude"},
		Memory:    profile.MemorySpec{Mode: profile.MemorySyncBack},
	})
	_ = cs.Create(&profile.ClusterProfile{Name: "c", Kind: profile.ClusterDocker, Context: "x"})

	for i := 0; i < 3; i++ {
		if _, err := m.Spawn(context.Background(), SpawnRequest{ProjectProfile: "p", ClusterProfile: "c"}); err != nil {
			t.Fatal(err)
		}
	}
	out := m.List()
	if len(out) != 3 {
		t.Fatalf("len=%d want 3", len(out))
	}
	for i := 1; i < len(out); i++ {
		if out[i-1].CreatedAt.After(out[i].CreatedAt) {
			t.Errorf("list not sorted at [%d]/[%d]", i-1, i)
		}
	}
}

// ── F10 S5.3 — git token broker wiring ───────────────────────────────

// Spawn calls MintForWorker with the agent ID + parsed repo and
// stores the returned token on the Agent.
func TestSpawn_MintsGitToken(t *testing.T) {
	m, ps, cs, _ := managerFixture(t)
	mint := &fakeMinter{}
	m.GitTokenMinter = mint

	_ = ps.Create(&profile.ProjectProfile{
		Name:      "p",
		Git:       profile.GitSpec{URL: "https://github.com/example/repo.git"},
		ImagePair: profile.ImagePair{Agent: "agent-claude"},
		Memory:    profile.MemorySpec{Mode: profile.MemorySyncBack},
	})
	_ = cs.Create(&profile.ClusterProfile{Name: "c", Kind: profile.ClusterDocker, Context: "x"})

	a, err := m.Spawn(context.Background(), SpawnRequest{ProjectProfile: "p", ClusterProfile: "c"})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if got := m.GetGitTokenFor(a.ID); got != "tok-"+a.ID {
		t.Errorf("GitToken=%q want tok-%s", got, a.ID)
	}
	if len(mint.mintCalls) != 1 || mint.repos[0] != "example/repo" {
		t.Errorf("mint=%v repos=%v", mint.mintCalls, mint.repos)
	}
}

// Spawn skips MintForWorker when the project has no git URL.
func TestSpawn_SkipsMintWhenNoGitURL(t *testing.T) {
	m, ps, cs, _ := managerFixture(t)
	mint := &fakeMinter{}
	m.GitTokenMinter = mint

	_ = ps.Create(&profile.ProjectProfile{
		Name:      "p",
		ImagePair: profile.ImagePair{Agent: "agent-claude"},
		Memory:    profile.MemorySpec{Mode: profile.MemorySyncBack},
	})
	_ = cs.Create(&profile.ClusterProfile{Name: "c", Kind: profile.ClusterDocker, Context: "x"})

	if _, err := m.Spawn(context.Background(), SpawnRequest{ProjectProfile: "p", ClusterProfile: "c"}); err == nil {
		// project requires git URL for validation; expected error
		t.Skip("validator rejects no-git profile, can't drive this branch via Spawn")
	}
	if len(mint.mintCalls) != 0 {
		t.Errorf("mint should not be called for no-git profile: %v", mint.mintCalls)
	}
}

// Spawn records FailureReason when the minter fails — but doesn't
// abort the spawn (worker boots without git creds; could be useful
// for read-only sessions).
func TestSpawn_MintFailureRecorded(t *testing.T) {
	m, ps, cs, _ := managerFixture(t)
	mint := &fakeMinter{mintErr: errors.New("rate limited")}
	m.GitTokenMinter = mint

	_ = ps.Create(&profile.ProjectProfile{
		Name:      "p",
		Git:       profile.GitSpec{URL: "https://github.com/x/y"},
		ImagePair: profile.ImagePair{Agent: "agent-claude"},
		Memory:    profile.MemorySpec{Mode: profile.MemorySyncBack},
	})
	_ = cs.Create(&profile.ClusterProfile{Name: "c", Kind: profile.ClusterDocker, Context: "x"})

	a, err := m.Spawn(context.Background(), SpawnRequest{ProjectProfile: "p", ClusterProfile: "c"})
	if err != nil {
		t.Fatalf("Spawn should not abort on mint failure: %v", err)
	}
	if !strings.Contains(a.FailureReason, "rate limited") {
		t.Errorf("FailureReason=%q want contains 'rate limited'", a.FailureReason)
	}
	if got := m.GetGitTokenFor(a.ID); got != "" {
		t.Errorf("GitToken should be empty after mint failure: %q", got)
	}
}

// Terminate calls RevokeForWorker on the configured minter.
func TestTerminate_RevokesGitToken(t *testing.T) {
	m, ps, cs, _ := managerFixture(t)
	mint := &fakeMinter{}
	m.GitTokenMinter = mint

	_ = ps.Create(&profile.ProjectProfile{
		Name:      "p",
		Git:       profile.GitSpec{URL: "https://github.com/x/y"},
		ImagePair: profile.ImagePair{Agent: "agent-claude"},
		Memory:    profile.MemorySpec{Mode: profile.MemorySyncBack},
	})
	_ = cs.Create(&profile.ClusterProfile{Name: "c", Kind: profile.ClusterDocker, Context: "x"})

	a, err := m.Spawn(context.Background(), SpawnRequest{ProjectProfile: "p", ClusterProfile: "c"})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Terminate(context.Background(), a.ID); err != nil {
		t.Fatalf("Terminate: %v", err)
	}
	if len(mint.revokeCalls) != 1 || mint.revokeCalls[0] != a.ID {
		t.Errorf("revoke calls=%v want [%s]", mint.revokeCalls, a.ID)
	}
}

// GetProjectFor returns the resolved project profile so the server's
// bootstrap handler can populate the Git bundle without exposing
// the private profile pointer.
func TestGetProjectFor(t *testing.T) {
	m, ps, cs, _ := managerFixture(t)
	_ = ps.Create(&profile.ProjectProfile{
		Name:      "p",
		Git:       profile.GitSpec{URL: "https://github.com/x/y", Branch: "main"},
		ImagePair: profile.ImagePair{Agent: "agent-claude"},
		Memory:    profile.MemorySpec{Mode: profile.MemorySyncBack},
	})
	_ = cs.Create(&profile.ClusterProfile{Name: "c", Kind: profile.ClusterDocker, Context: "x"})

	a, err := m.Spawn(context.Background(), SpawnRequest{ProjectProfile: "p", ClusterProfile: "c"})
	if err != nil {
		t.Fatal(err)
	}
	got := m.GetProjectFor(a.ID)
	if got == nil {
		t.Fatal("GetProjectFor returned nil")
	}
	if got.Git.URL != "https://github.com/x/y" || got.Git.Branch != "main" {
		t.Errorf("project mismatch: %+v", got.Git)
	}
	if m.GetProjectFor("nonexistent") != nil {
		t.Error("unknown agent should return nil")
	}
}
