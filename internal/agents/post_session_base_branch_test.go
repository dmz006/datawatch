package agents

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/dmz006/datawatch/internal/git"
	"github.com/dmz006/datawatch/internal/profile"
)

// Finished work is never pushed onto a main branch by the pull request hook.
func TestPostSessionPRHook_NeverPushesABaseBranch(t *testing.T) {
	for _, branch := range []string{"main", "master"} {
		pusher := &fakePusher{branchToReturn: branch}
		prov := &recordingProvider{}
		hook, _, agentID := hookFixture(t, true, pusher, prov)
		hook(&fakeSession{id: "s1", agent: agentID, dir: "/work/repo", task: "do x"})
		if len(pusher.pushCalls) != 0 || len(prov.openPRCalls) != 0 {
			t.Errorf("%s: pushed %v, opened %d", branch, pusher.pushCalls, len(prov.openPRCalls))
		}
	}
}

// The profile's base branch is skipped whatever it is called.
func TestPostSessionPRHook_NeverPushesTheProfileBaseBranch(t *testing.T) {
	pusher := &fakePusher{branchToReturn: "develop"}
	prov := &recordingProvider{}
	hook, _, agentID := hookFixtureBase(t, "develop", pusher, prov)
	hook(&fakeSession{id: "s1", agent: agentID, dir: "/work/repo", task: "do x"})
	if len(pusher.pushCalls) != 0 || len(prov.openPRCalls) != 0 {
		t.Errorf("pushed %v, opened %d", pusher.pushCalls, len(prov.openPRCalls))
	}
}

// hookFixtureBase is hookFixture for a profile whose base branch is base.
func hookFixtureBase(t *testing.T, base string, pusher BranchPusher, prov git.Provider) (func(SessionLike), *recordedLog, string) {
	t.Helper()
	dir := t.TempDir()
	ps, _ := profile.NewProjectStore(filepath.Join(dir, "p.json"))
	cs, _ := profile.NewClusterStore(filepath.Join(dir, "c.json"))
	_ = ps.Create(&profile.ProjectProfile{
		Name:      "p",
		Git:       profile.GitSpec{URL: "https://github.com/owner/repo", Branch: base, AutoPR: true},
		ImagePair: profile.ImagePair{Agent: "agent-claude"},
		Memory:    profile.MemorySpec{Mode: profile.MemorySyncBack},
	})
	_ = cs.Create(&profile.ClusterProfile{Name: "c", Kind: profile.ClusterDocker, Context: "x"})
	m := NewManager(ps, cs)
	m.RegisterDriver(&fakeDriver{kind: "docker"})
	a, err := m.Spawn(context.Background(), SpawnRequest{ProjectProfile: "p", ClusterProfile: "c"})
	if err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	m.agents[a.ID].GitToken = "fake-tok"
	m.mu.Unlock()
	rec := &recordedLog{}
	return PostSessionPRHook(PRHookConfig{Manager: m, Provider: prov, Pusher: pusher}, rec.append), rec, a.ID
}
