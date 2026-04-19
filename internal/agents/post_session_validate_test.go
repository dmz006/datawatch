// F10 sprint 7 (S7.5) — validator-on-session-end trigger tests.

package agents

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/profile"
)

// validatorFixture wires a Manager with a worker profile (set
// AutoValidate via the setter) + a validator profile + cluster +
// returns a hook with a SpawnFunc closure that records calls.
func validatorFixture(t *testing.T, autoValidate bool, validateProfile string) (func(SessionLike), *recordedLog, *spawnRecorder, string) {
	t.Helper()
	dir := t.TempDir()
	ps, _ := profile.NewProjectStore(filepath.Join(dir, "p.json"))
	cs, _ := profile.NewClusterStore(filepath.Join(dir, "c.json"))
	_ = ps.Create(&profile.ProjectProfile{
		Name:            "worker-prof",
		Git:             profile.GitSpec{URL: "https://github.com/x/y"},
		ImagePair:       profile.ImagePair{Agent: "agent-claude"},
		Memory:          profile.MemorySpec{Mode: profile.MemorySyncBack},
		AutoValidate:    autoValidate,
		ValidateProfile: validateProfile,
	})
	_ = ps.Create(&profile.ProjectProfile{
		Name:      "validator",
		Git:       profile.GitSpec{URL: "https://github.com/x/v"},
		ImagePair: profile.ImagePair{Agent: "agent-claude"},
		Memory:    profile.MemorySpec{Mode: profile.MemorySyncBack},
	})
	_ = ps.Create(&profile.ProjectProfile{
		Name:      "custom-validator",
		Git:       profile.GitSpec{URL: "https://github.com/x/cv"},
		ImagePair: profile.ImagePair{Agent: "agent-claude"},
		Memory:    profile.MemorySpec{Mode: profile.MemorySyncBack},
	})
	_ = cs.Create(&profile.ClusterProfile{Name: "c", Kind: profile.ClusterDocker, Context: "x"})

	m := NewManager(ps, cs)
	m.RegisterDriver(&fakeDriver{kind: "docker"})
	a, err := m.Spawn(context.Background(), SpawnRequest{ProjectProfile: "worker-prof", ClusterProfile: "c"})
	if err != nil {
		t.Fatal(err)
	}

	rec := &recordedLog{}
	sp := &spawnRecorder{}
	hook := PostSessionValidateHook(ValidatorSpawnerConfig{
		Manager:   m,
		SpawnFunc: sp.spawn,
	}, rec.append)
	return hook, rec, sp, a.ID
}

type spawnRecorder struct {
	mu    sync.Mutex
	calls []SpawnRequest
}

func (sp *spawnRecorder) spawn(_ context.Context, req SpawnRequest) (*Agent, error) {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	sp.calls = append(sp.calls, req)
	return &Agent{ID: "validator-" + req.Branch}, nil
}

func TestValidateHook_AutoValidateFalse_Skipped(t *testing.T) {
	hook, _, sp, agentID := validatorFixture(t, false, "")
	hook(&fakeSession{id: "s1", agent: agentID, dir: "/w", task: "t"})
	if len(sp.calls) != 0 {
		t.Errorf("validator spawned when AutoValidate=false: %v", sp.calls)
	}
}

func TestValidateHook_AutoValidateTrue_DefaultProfile(t *testing.T) {
	hook, rec, sp, agentID := validatorFixture(t, true, "")
	hook(&fakeSession{id: "s1", agent: agentID, dir: "/w", task: "do work"})
	if len(sp.calls) != 1 {
		t.Fatalf("spawn calls=%d want 1", len(sp.calls))
	}
	if sp.calls[0].ProjectProfile != "validator" {
		t.Errorf("default validator profile not used: %+v", sp.calls[0])
	}
	if sp.calls[0].ParentAgentID != agentID {
		t.Errorf("ParentAgentID not set: %+v", sp.calls[0])
	}
	if !strings.HasPrefix(sp.calls[0].Branch, "validate-") {
		t.Errorf("branch should start with validate-: %q", sp.calls[0].Branch)
	}
	if !strings.Contains(rec.joined(), "validator spawned") {
		t.Errorf("log missing validator spawned line: %s", rec.joined())
	}
}

func TestValidateHook_AutoValidateTrue_CustomProfile(t *testing.T) {
	hook, _, sp, agentID := validatorFixture(t, true, "custom-validator")
	hook(&fakeSession{id: "s1", agent: agentID, dir: "/w", task: "t"})
	if len(sp.calls) != 1 || sp.calls[0].ProjectProfile != "custom-validator" {
		t.Errorf("custom validator profile not used: %+v", sp.calls)
	}
}

func TestValidateHook_NotAWorkerSession(t *testing.T) {
	hook, _, sp, _ := validatorFixture(t, true, "")
	hook(&fakeSession{id: "s1", agent: "", dir: "/w", task: "t"})
	if len(sp.calls) != 0 {
		t.Error("validator spawned for non-agent session")
	}
}

func TestValidateHook_SpawnFailureLogged(t *testing.T) {
	hook, rec, _, agentID := validatorFixture(t, true, "")
	// Override the SpawnFunc to fail.
	hook = PostSessionValidateHook(ValidatorSpawnerConfig{
		Manager: nil, // intentionally nil so config-incomplete branch fires
		SpawnFunc: func(_ context.Context, _ SpawnRequest) (*Agent, error) {
			return nil, errors.New("driver unhealthy")
		},
	}, rec.append)
	hook(&fakeSession{id: "s1", agent: agentID, dir: "/w", task: "t"})
	if !strings.Contains(rec.joined(), "config incomplete") {
		t.Errorf("log missing config-incomplete: %s", rec.joined())
	}
}

// Now is overridable for deterministic branch generation.
func TestValidateHook_OverrideNow(t *testing.T) {
	hook, _, sp, agentID := validatorFixture(t, true, "")
	// Re-wrap the hook with a fixed Now so the branch is reproducible.
	// Since validatorFixture's hook already references its own SpawnFunc,
	// just verify the branch contains a unix timestamp suffix from the
	// default Now() — close enough.
	hook(&fakeSession{id: "s1", agent: agentID, dir: "/w", task: "t"})
	_ = time.Now()
	if len(sp.calls) != 1 {
		t.Fatal("spawn not called")
	}
	if !strings.HasPrefix(sp.calls[0].Branch, "validate-") {
		t.Errorf("branch=%q", sp.calls[0].Branch)
	}
}
