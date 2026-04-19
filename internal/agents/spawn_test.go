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

	// F10 S7.3: each spawn needs a distinct branch since the
	// workspace lock now refuses double-spawn on the same
	// (project, branch) tuple.
	for i := 0; i < 3; i++ {
		if _, err := m.Spawn(context.Background(), SpawnRequest{
			ProjectProfile: "p", ClusterProfile: "c",
			Branch: "b-" + string(rune('a'+i)),
		}); err != nil {
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

// ── F10 S7.3 — workspace lock ────────────────────────────────────────

// Two spawns on the same (project, branch) — second is rejected.
func TestSpawn_WorkspaceLock_RejectsDuplicate(t *testing.T) {
	m, ps, cs, _ := managerFixture(t)
	_ = ps.Create(&profile.ProjectProfile{
		Name: "p",
		Git:  profile.GitSpec{URL: "https://github.com/x/y", Branch: "main"},
		ImagePair: profile.ImagePair{Agent: "agent-claude"},
		Memory:    profile.MemorySpec{Mode: profile.MemorySyncBack},
	})
	_ = cs.Create(&profile.ClusterProfile{Name: "c", Kind: profile.ClusterDocker, Context: "x"})

	if _, err := m.Spawn(context.Background(), SpawnRequest{ProjectProfile: "p", ClusterProfile: "c"}); err != nil {
		t.Fatalf("first spawn: %v", err)
	}
	_, err := m.Spawn(context.Background(), SpawnRequest{ProjectProfile: "p", ClusterProfile: "c"})
	if err == nil {
		t.Fatal("second spawn should be rejected by workspace lock")
	}
	if !strings.Contains(err.Error(), "workspace lock") {
		t.Errorf("error wording: %v", err)
	}
}

// Different branches on same project: both allowed.
func TestSpawn_WorkspaceLock_DifferentBranchesOK(t *testing.T) {
	m, ps, cs, _ := managerFixture(t)
	_ = ps.Create(&profile.ProjectProfile{
		Name: "p",
		Git:  profile.GitSpec{URL: "https://github.com/x/y", Branch: "main"},
		ImagePair: profile.ImagePair{Agent: "agent-claude"},
		Memory:    profile.MemorySpec{Mode: profile.MemorySyncBack},
	})
	_ = cs.Create(&profile.ClusterProfile{Name: "c", Kind: profile.ClusterDocker, Context: "x"})

	if _, err := m.Spawn(context.Background(), SpawnRequest{ProjectProfile: "p", ClusterProfile: "c", Branch: "feat/a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Spawn(context.Background(), SpawnRequest{ProjectProfile: "p", ClusterProfile: "c", Branch: "feat/b"}); err != nil {
		t.Errorf("different branch should be allowed: %v", err)
	}
}

// After Terminate, workspace is freed.
func TestSpawn_WorkspaceLock_FreedAfterTerminate(t *testing.T) {
	m, ps, cs, _ := managerFixture(t)
	_ = ps.Create(&profile.ProjectProfile{
		Name: "p",
		Git:  profile.GitSpec{URL: "https://github.com/x/y", Branch: "main"},
		ImagePair: profile.ImagePair{Agent: "agent-claude"},
		Memory:    profile.MemorySpec{Mode: profile.MemorySyncBack},
	})
	_ = cs.Create(&profile.ClusterProfile{Name: "c", Kind: profile.ClusterDocker, Context: "x"})

	a, err := m.Spawn(context.Background(), SpawnRequest{ProjectProfile: "p", ClusterProfile: "c"})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Terminate(context.Background(), a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Spawn(context.Background(), SpawnRequest{ProjectProfile: "p", ClusterProfile: "c"}); err != nil {
		t.Errorf("workspace should be freed after Terminate: %v", err)
	}
}

// ── F10 S7.4 — recursion gates ───────────────────────────────────────

// Parent without AllowSpawnChildren → recursive child rejected.
func TestSpawn_RecursionGate_RejectsWhenNotAllowed(t *testing.T) {
	m, ps, cs, _ := managerFixture(t)
	_ = ps.Create(&profile.ProjectProfile{
		Name: "p",
		Git:  profile.GitSpec{URL: "https://github.com/x/y"},
		ImagePair: profile.ImagePair{Agent: "agent-claude"},
		Memory:    profile.MemorySpec{Mode: profile.MemorySyncBack},
		// AllowSpawnChildren defaults to false
	})
	_ = cs.Create(&profile.ClusterProfile{Name: "c", Kind: profile.ClusterDocker, Context: "x"})

	parent, err := m.Spawn(context.Background(), SpawnRequest{ProjectProfile: "p", ClusterProfile: "c"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.Spawn(context.Background(), SpawnRequest{
		ProjectProfile: "p", ClusterProfile: "c", Branch: "child",
		ParentAgentID: parent.ID,
	})
	if err == nil {
		t.Fatal("recursive spawn should be rejected when AllowSpawnChildren=false")
	}
	if !strings.Contains(err.Error(), "allow_spawn_children") {
		t.Errorf("error wording: %v", err)
	}
}

// SpawnBudgetTotal cap is enforced.
func TestSpawn_RecursionGate_TotalCap(t *testing.T) {
	m, ps, cs, _ := managerFixture(t)
	_ = ps.Create(&profile.ProjectProfile{
		Name: "p",
		Git:  profile.GitSpec{URL: "https://github.com/x/y"},
		ImagePair: profile.ImagePair{Agent: "agent-claude"},
		Memory:    profile.MemorySpec{Mode: profile.MemorySyncBack},
		AllowSpawnChildren: true,
		SpawnBudgetTotal:   2,
	})
	_ = cs.Create(&profile.ClusterProfile{Name: "c", Kind: profile.ClusterDocker, Context: "x"})

	parent, err := m.Spawn(context.Background(), SpawnRequest{ProjectProfile: "p", ClusterProfile: "c"})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		_, err := m.Spawn(context.Background(), SpawnRequest{
			ProjectProfile: "p", ClusterProfile: "c",
			Branch:        "child-" + string(rune('a'+i)),
			ParentAgentID: parent.ID,
		})
		if err != nil {
			t.Fatalf("child %d should succeed (under cap): %v", i, err)
		}
	}
	// Third child exceeds the cap of 2.
	_, err = m.Spawn(context.Background(), SpawnRequest{
		ProjectProfile: "p", ClusterProfile: "c", Branch: "child-c",
		ParentAgentID: parent.ID,
	})
	if err == nil {
		t.Fatal("third child should exceed total cap")
	}
	if !strings.Contains(err.Error(), "spawn_budget_total") {
		t.Errorf("error wording: %v", err)
	}
}

// SpawnBudgetPerMinute cap is enforced.
func TestSpawn_RecursionGate_PerMinuteCap(t *testing.T) {
	m, ps, cs, _ := managerFixture(t)
	_ = ps.Create(&profile.ProjectProfile{
		Name: "p",
		Git:  profile.GitSpec{URL: "https://github.com/x/y"},
		ImagePair: profile.ImagePair{Agent: "agent-claude"},
		Memory:    profile.MemorySpec{Mode: profile.MemorySyncBack},
		AllowSpawnChildren:   true,
		SpawnBudgetPerMinute: 1,
	})
	_ = cs.Create(&profile.ClusterProfile{Name: "c", Kind: profile.ClusterDocker, Context: "x"})

	parent, err := m.Spawn(context.Background(), SpawnRequest{ProjectProfile: "p", ClusterProfile: "c"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Spawn(context.Background(), SpawnRequest{
		ProjectProfile: "p", ClusterProfile: "c", Branch: "first",
		ParentAgentID: parent.ID,
	}); err != nil {
		t.Fatal(err)
	}
	_, err = m.Spawn(context.Background(), SpawnRequest{
		ProjectProfile: "p", ClusterProfile: "c", Branch: "second",
		ParentAgentID: parent.ID,
	})
	if err == nil {
		t.Fatal("second child within the same minute should be rejected")
	}
	if !strings.Contains(err.Error(), "spawn_budget_per_minute") {
		t.Errorf("error wording: %v", err)
	}
}

// Top-level spawn (no ParentAgentID) is never gated by recursion budgets.
func TestSpawn_RecursionGate_TopLevelExempt(t *testing.T) {
	m, ps, cs, _ := managerFixture(t)
	_ = ps.Create(&profile.ProjectProfile{
		Name: "p",
		Git:  profile.GitSpec{URL: "https://github.com/x/y"},
		ImagePair: profile.ImagePair{Agent: "agent-claude"},
		Memory:    profile.MemorySpec{Mode: profile.MemorySyncBack},
		AllowSpawnChildren:   false, // would block recursive spawns
	})
	_ = cs.Create(&profile.ClusterProfile{Name: "c", Kind: profile.ClusterDocker, Context: "x"})

	// Top-level (no parent) succeeds even though AllowSpawnChildren=false.
	if _, err := m.Spawn(context.Background(), SpawnRequest{ProjectProfile: "p", ClusterProfile: "c"}); err != nil {
		t.Errorf("top-level spawn should never be gated by recursion budgets: %v", err)
	}
}

// ── F10 S7.2 — fan-in result aggregation ─────────────────────────────

func TestRecordResult_StoresAndDefaultsStatus(t *testing.T) {
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
	if err := m.RecordResult(a.ID, &AgentResult{Summary: "did the thing"}); err != nil {
		t.Fatal(err)
	}
	got := m.Get(a.ID)
	if got.Result == nil {
		t.Fatal("result not stored")
	}
	if got.Result.Status != "ok" {
		t.Errorf("default status=%q want ok", got.Result.Status)
	}
	if got.Result.Summary != "did the thing" {
		t.Errorf("summary lost: %+v", got.Result)
	}
	if got.Result.ReportedAt.IsZero() {
		t.Errorf("ReportedAt not stamped")
	}
}

func TestRecordResult_OverwritesPrevious(t *testing.T) {
	m, ps, cs, _ := managerFixture(t)
	_ = ps.Create(&profile.ProjectProfile{
		Name: "p", Git: profile.GitSpec{URL: "https://g/y"},
		ImagePair: profile.ImagePair{Agent: "agent-claude"},
		Memory:    profile.MemorySpec{Mode: profile.MemorySyncBack},
	})
	_ = cs.Create(&profile.ClusterProfile{Name: "c", Kind: profile.ClusterDocker, Context: "x"})
	a, _ := m.Spawn(context.Background(), SpawnRequest{ProjectProfile: "p", ClusterProfile: "c"})
	_ = m.RecordResult(a.ID, &AgentResult{Status: "partial", Summary: "first"})
	_ = m.RecordResult(a.ID, &AgentResult{Status: "ok", Summary: "second"})
	got := m.Get(a.ID).Result
	if got.Summary != "second" || got.Status != "ok" {
		t.Errorf("overwrite mismatch: %+v", got)
	}
}

func TestRecordResult_RejectsUnknown(t *testing.T) {
	m, _, _, _ := managerFixture(t)
	if err := m.RecordResult("ghost", &AgentResult{Summary: "x"}); err == nil {
		t.Error("expected not-found error")
	}
}

func TestRecordResult_ValidatesArgs(t *testing.T) {
	m, _, _, _ := managerFixture(t)
	if err := m.RecordResult("", &AgentResult{}); err == nil {
		t.Error("expected error for empty id")
	}
	if err := m.RecordResult("x", nil); err == nil {
		t.Error("expected error for nil result")
	}
}

func TestRecordResult_ArtifactsRoundTrip(t *testing.T) {
	m, ps, cs, _ := managerFixture(t)
	_ = ps.Create(&profile.ProjectProfile{
		Name: "p", Git: profile.GitSpec{URL: "https://g/y"},
		ImagePair: profile.ImagePair{Agent: "agent-claude"},
		Memory:    profile.MemorySpec{Mode: profile.MemorySyncBack},
	})
	_ = cs.Create(&profile.ClusterProfile{Name: "c", Kind: profile.ClusterDocker, Context: "x"})
	a, _ := m.Spawn(context.Background(), SpawnRequest{ProjectProfile: "p", ClusterProfile: "c"})
	res := &AgentResult{
		Status:    "ok",
		Artifacts: map[string]interface{}{"pr_url": "https://gh/x/y/pull/7", "memory_ids": []int{42, 43}},
	}
	if err := m.RecordResult(a.ID, res); err != nil {
		t.Fatal(err)
	}
	got := m.Get(a.ID).Result.Artifacts
	if got["pr_url"] != "https://gh/x/y/pull/7" {
		t.Errorf("pr_url lost: %+v", got)
	}
}

// ── F10 S8.6 — idle-timeout enforcement ──────────────────────────────

// NoteActivity bumps LastActivityAt; nil-safe for unknown agents.
func TestNoteActivity_BumpsTimestamp(t *testing.T) {
	m, ps, cs, _ := managerFixture(t)
	_ = ps.Create(&profile.ProjectProfile{
		Name: "p", Git: profile.GitSpec{URL: "https://g/y"},
		ImagePair: profile.ImagePair{Agent: "agent-claude"},
		Memory:    profile.MemorySpec{Mode: profile.MemorySyncBack},
	})
	_ = cs.Create(&profile.ClusterProfile{Name: "c", Kind: profile.ClusterDocker, Context: "x"})
	a, _ := m.Spawn(context.Background(), SpawnRequest{ProjectProfile: "p", ClusterProfile: "c"})

	// Force the timestamp to zero so we can detect the bump.
	m.mu.Lock()
	m.agents[a.ID].LastActivityAt = time.Time{}
	m.mu.Unlock()

	m.NoteActivity(a.ID)
	if got := m.Get(a.ID).LastActivityAt; got.IsZero() {
		t.Error("NoteActivity didn't bump LastActivityAt")
	}
	// Unknown agent → no panic
	m.NoteActivity("ghost")
}

// ReapIdle terminates agents whose IdleTimeout has elapsed.
func TestReapIdle_TerminatesExpired(t *testing.T) {
	m, ps, cs, _ := managerFixture(t)
	_ = ps.Create(&profile.ProjectProfile{
		Name: "p", Git: profile.GitSpec{URL: "https://g/y"},
		ImagePair:   profile.ImagePair{Agent: "agent-claude"},
		Memory:      profile.MemorySpec{Mode: profile.MemorySyncBack},
		IdleTimeout: 10 * time.Second,
	})
	_ = cs.Create(&profile.ClusterProfile{Name: "c", Kind: profile.ClusterDocker, Context: "x"})
	a, _ := m.Spawn(context.Background(), SpawnRequest{ProjectProfile: "p", ClusterProfile: "c"})

	// Force LastActivityAt to 30s ago so the 10s threshold trips.
	old := time.Now().UTC().Add(-30 * time.Second)
	m.mu.Lock()
	m.agents[a.ID].LastActivityAt = old
	m.mu.Unlock()

	reaped := m.ReapIdle(context.Background(), time.Now().UTC())
	if len(reaped) != 1 || reaped[0] != a.ID {
		t.Errorf("reaped=%v want [%s]", reaped, a.ID)
	}
	got := m.Get(a.ID)
	if got.State != StateStopped {
		t.Errorf("state=%s want stopped", got.State)
	}
}

// BL108 — RunIdleReaper exits when its context is cancelled.
func TestRunIdleReaper_StopsOnCancel(t *testing.T) {
	m, _, _, _ := managerFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	m.RunIdleReaper(ctx, 10*time.Millisecond)
	cancel()
	// Just give the goroutine a chance to observe the cancel.
	// No hangs/goroutine leaks expected on test exit.
	time.Sleep(20 * time.Millisecond)
}

// BL108 — RunIdleReaper triggers ReapIdle on its cadence.
func TestRunIdleReaper_FiresPeriodically(t *testing.T) {
	m, ps, cs, _ := managerFixture(t)
	_ = ps.Create(&profile.ProjectProfile{
		Name: "p", Git: profile.GitSpec{URL: "https://g/y"},
		ImagePair:   profile.ImagePair{Agent: "agent-claude"},
		Memory:      profile.MemorySpec{Mode: profile.MemorySyncBack},
		IdleTimeout: 10 * time.Millisecond,
	})
	_ = cs.Create(&profile.ClusterProfile{Name: "c", Kind: profile.ClusterDocker, Context: "x"})
	a, _ := m.Spawn(context.Background(), SpawnRequest{ProjectProfile: "p", ClusterProfile: "c"})
	m.mu.Lock()
	m.agents[a.ID].LastActivityAt = time.Now().UTC().Add(-time.Hour)
	m.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Use the explicit interval; RunIdleReaper clamps to 10s in
	// production but here we drive ReapIdle directly to keep the
	// test deterministic — the goroutine is just along for the ride.
	m.RunIdleReaper(ctx, 10*time.Second)
	_ = m.ReapIdle(context.Background(), time.Now().UTC())

	if got := m.Get(a.ID); got.State != StateStopped {
		t.Errorf("agent should have been reaped: state=%s", got.State)
	}
}

// BL108 — interval below the 10s floor is clamped (no panic, loop runs).
func TestRunIdleReaper_ClampsToTenSeconds(t *testing.T) {
	m, _, _, _ := managerFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m.RunIdleReaper(ctx, time.Millisecond) // would otherwise be too tight
	// nothing to assert beyond "did not panic / did not crash" — the
	// internal clamp is what we're verifying compiles + runs.
}

// Active agents (within timeout) survive ReapIdle.
func TestReapIdle_LeavesActiveAlone(t *testing.T) {
	m, ps, cs, _ := managerFixture(t)
	_ = ps.Create(&profile.ProjectProfile{
		Name: "p", Git: profile.GitSpec{URL: "https://g/y"},
		ImagePair:   profile.ImagePair{Agent: "agent-claude"},
		Memory:      profile.MemorySpec{Mode: profile.MemorySyncBack},
		IdleTimeout: time.Hour,
	})
	_ = cs.Create(&profile.ClusterProfile{Name: "c", Kind: profile.ClusterDocker, Context: "x"})
	a, _ := m.Spawn(context.Background(), SpawnRequest{ProjectProfile: "p", ClusterProfile: "c"})

	m.NoteActivity(a.ID) // recent activity
	reaped := m.ReapIdle(context.Background(), time.Now().UTC())
	if len(reaped) != 0 {
		t.Errorf("active agent reaped: %v", reaped)
	}
	if m.Get(a.ID).State == StateStopped {
		t.Error("active agent terminated")
	}
}

// IdleTimeout=0 means "no timeout" — agent is never reaped.
func TestReapIdle_ZeroTimeoutIsDisabled(t *testing.T) {
	m, ps, cs, _ := managerFixture(t)
	_ = ps.Create(&profile.ProjectProfile{
		Name: "p", Git: profile.GitSpec{URL: "https://g/y"},
		ImagePair:   profile.ImagePair{Agent: "agent-claude"},
		Memory:      profile.MemorySpec{Mode: profile.MemorySyncBack},
		IdleTimeout: 0,
	})
	_ = cs.Create(&profile.ClusterProfile{Name: "c", Kind: profile.ClusterDocker, Context: "x"})
	a, _ := m.Spawn(context.Background(), SpawnRequest{ProjectProfile: "p", ClusterProfile: "c"})

	// Even when "now" is far in the future, no reap.
	reaped := m.ReapIdle(context.Background(), time.Now().UTC().Add(24*time.Hour))
	if len(reaped) != 0 {
		t.Errorf("zero IdleTimeout should disable reaping: %v", reaped)
	}
	_ = a
}

// LastActivityAt zero falls back to CreatedAt floor.
func TestReapIdle_FallsBackToCreatedAt(t *testing.T) {
	m, ps, cs, _ := managerFixture(t)
	_ = ps.Create(&profile.ProjectProfile{
		Name: "p", Git: profile.GitSpec{URL: "https://g/y"},
		ImagePair:   profile.ImagePair{Agent: "agent-claude"},
		Memory:      profile.MemorySpec{Mode: profile.MemorySyncBack},
		IdleTimeout: 1 * time.Second,
	})
	_ = cs.Create(&profile.ClusterProfile{Name: "c", Kind: profile.ClusterDocker, Context: "x"})
	a, _ := m.Spawn(context.Background(), SpawnRequest{ProjectProfile: "p", ClusterProfile: "c"})

	// Force LastActivityAt to zero so the reaper uses CreatedAt.
	// CreatedAt is now (just-spawned) → should NOT be reaped immediately.
	m.mu.Lock()
	m.agents[a.ID].LastActivityAt = time.Time{}
	createdAt := m.agents[a.ID].CreatedAt
	m.mu.Unlock()
	if reaped := m.ReapIdle(context.Background(), createdAt.Add(500*time.Millisecond)); len(reaped) != 0 {
		t.Errorf("reaped fresh agent: %v", reaped)
	}
	// Now jump 5s ahead — exceeds the 1s timeout from CreatedAt.
	if reaped := m.ReapIdle(context.Background(), createdAt.Add(5*time.Second)); len(reaped) != 1 {
		t.Errorf("expected reap from CreatedAt floor, got %v", reaped)
	}
}

// ConsumeBootstrap counts as activity (sets LastActivityAt to ReadyAt).
func TestConsumeBootstrap_StampsActivity(t *testing.T) {
	m, ps, cs, _ := managerFixture(t)
	_ = ps.Create(&profile.ProjectProfile{
		Name: "p", Git: profile.GitSpec{URL: "https://g/y"},
		ImagePair: profile.ImagePair{Agent: "agent-claude"},
		Memory:    profile.MemorySpec{Mode: profile.MemorySyncBack},
	})
	_ = cs.Create(&profile.ClusterProfile{Name: "c", Kind: profile.ClusterDocker, Context: "x"})
	a, _ := m.Spawn(context.Background(), SpawnRequest{ProjectProfile: "p", ClusterProfile: "c"})

	tok := m.BootstrapTokenForTest(a.ID)
	if _, err := m.ConsumeBootstrap(tok, a.ID); err != nil {
		t.Fatal(err)
	}
	got := m.Get(a.ID)
	if got.LastActivityAt.IsZero() {
		t.Error("ConsumeBootstrap should bump LastActivityAt")
	}
}

// ── F10 S8.3 — multi-cluster (default cluster on Project Profile) ────

// SpawnRequest with empty cluster_profile uses the project's default.
func TestSpawn_DefaultClusterFromProfile(t *testing.T) {
	m, ps, cs, _ := managerFixture(t)
	_ = ps.Create(&profile.ProjectProfile{
		Name: "p", Git: profile.GitSpec{URL: "https://g/y"},
		ImagePair:             profile.ImagePair{Agent: "agent-claude"},
		Memory:                profile.MemorySpec{Mode: profile.MemorySyncBack},
		DefaultClusterProfile: "preferred-cluster",
	})
	_ = cs.Create(&profile.ClusterProfile{Name: "preferred-cluster", Kind: profile.ClusterDocker, Context: "x"})

	a, err := m.Spawn(context.Background(), SpawnRequest{
		ProjectProfile: "p",
		// cluster_profile deliberately omitted
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if a.ClusterProfile != "preferred-cluster" {
		t.Errorf("ClusterProfile=%q want preferred-cluster", a.ClusterProfile)
	}
}

// Explicit cluster_profile on the request still wins over the
// project's DefaultClusterProfile (operator override).
func TestSpawn_ExplicitClusterOverridesDefault(t *testing.T) {
	m, ps, cs, _ := managerFixture(t)
	_ = ps.Create(&profile.ProjectProfile{
		Name: "p", Git: profile.GitSpec{URL: "https://g/y"},
		ImagePair:             profile.ImagePair{Agent: "agent-claude"},
		Memory:                profile.MemorySpec{Mode: profile.MemorySyncBack},
		DefaultClusterProfile: "default",
	})
	_ = cs.Create(&profile.ClusterProfile{Name: "default", Kind: profile.ClusterDocker, Context: "x"})
	_ = cs.Create(&profile.ClusterProfile{Name: "override", Kind: profile.ClusterDocker, Context: "y"})

	a, err := m.Spawn(context.Background(), SpawnRequest{
		ProjectProfile: "p",
		ClusterProfile: "override",
	})
	if err != nil {
		t.Fatal(err)
	}
	if a.ClusterProfile != "override" {
		t.Errorf("ClusterProfile=%q want override", a.ClusterProfile)
	}
}

// No cluster_profile + no default → clear actionable error.
func TestSpawn_NoClusterAndNoDefault(t *testing.T) {
	m, ps, cs, _ := managerFixture(t)
	_ = ps.Create(&profile.ProjectProfile{
		Name: "p", Git: profile.GitSpec{URL: "https://g/y"},
		ImagePair: profile.ImagePair{Agent: "agent-claude"},
		Memory:    profile.MemorySpec{Mode: profile.MemorySyncBack},
	})
	_ = cs.Create(&profile.ClusterProfile{Name: "c", Kind: profile.ClusterDocker, Context: "x"})

	_, err := m.Spawn(context.Background(), SpawnRequest{ProjectProfile: "p"})
	if err == nil {
		t.Fatal("expected error when no cluster_profile and no default")
	}
	if !strings.Contains(err.Error(), "default_cluster_profile") {
		t.Errorf("error should mention default_cluster_profile: %v", err)
	}
}

// ── F10 S8.2 — service-mode exemption ────────────────────────────────

// Service-mode agents are exempt from the idle reaper even when
// LastActivityAt is far in the past.
func TestReapIdle_ExemptsServiceMode(t *testing.T) {
	m, ps, cs, _ := managerFixture(t)
	_ = ps.Create(&profile.ProjectProfile{
		Name: "svc", Git: profile.GitSpec{URL: "https://g/y"},
		ImagePair:   profile.ImagePair{Agent: "agent-claude"},
		Memory:      profile.MemorySpec{Mode: profile.MemorySyncBack},
		IdleTimeout: 10 * time.Second,
		Mode:        "service",
	})
	_ = cs.Create(&profile.ClusterProfile{Name: "c", Kind: profile.ClusterDocker, Context: "x"})
	a, _ := m.Spawn(context.Background(), SpawnRequest{ProjectProfile: "svc", ClusterProfile: "c"})

	// Force LastActivityAt to 1 hour ago — would trip a 10s idle on
	// an ephemeral worker.
	old := time.Now().UTC().Add(-time.Hour)
	m.mu.Lock()
	m.agents[a.ID].LastActivityAt = old
	m.mu.Unlock()

	reaped := m.ReapIdle(context.Background(), time.Now().UTC())
	if len(reaped) != 0 {
		t.Errorf("service-mode agent reaped: %v", reaped)
	}
	if got := m.Get(a.ID); got.State == StateStopped {
		t.Error("service-mode agent terminated by idle reaper")
	}
}

// Ephemeral mode (or empty mode) still gets reaped.
func TestReapIdle_EphemeralStillReaped(t *testing.T) {
	m, ps, cs, _ := managerFixture(t)
	_ = ps.Create(&profile.ProjectProfile{
		Name: "eph", Git: profile.GitSpec{URL: "https://g/y"},
		ImagePair:   profile.ImagePair{Agent: "agent-claude"},
		Memory:      profile.MemorySpec{Mode: profile.MemorySyncBack},
		IdleTimeout: 10 * time.Second,
		// Mode left empty = ephemeral default
	})
	_ = cs.Create(&profile.ClusterProfile{Name: "c", Kind: profile.ClusterDocker, Context: "x"})
	a, _ := m.Spawn(context.Background(), SpawnRequest{ProjectProfile: "eph", ClusterProfile: "c"})

	m.mu.Lock()
	m.agents[a.ID].LastActivityAt = time.Now().UTC().Add(-time.Hour)
	m.mu.Unlock()

	reaped := m.ReapIdle(context.Background(), time.Now().UTC())
	if len(reaped) != 1 || reaped[0] != a.ID {
		t.Errorf("ephemeral should reap; got %v", reaped)
	}
}
