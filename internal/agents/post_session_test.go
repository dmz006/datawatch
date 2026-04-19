// F10 sprint 5 (S5.4) — post-session PR hook tests.

package agents

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/git"
	"github.com/dmz006/datawatch/internal/profile"
)

// fakeSession satisfies the SessionLike interface used by the hook.
type fakeSession struct {
	id, agent, dir, task string
}

func (f *fakeSession) GetID() string         { return f.id }
func (f *fakeSession) GetAgentID() string    { return f.agent }
func (f *fakeSession) GetProjectDir() string { return f.dir }
func (f *fakeSession) GetTask() string       { return f.task }

// fakePusher records calls + returns canned values.
type fakePusher struct {
	mu             sync.Mutex
	branchToReturn string
	branchErr      error
	pushCalls      []string // formatted "dir|remote|branch|origin|token"
	pushErr        error
}

func (p *fakePusher) CurrentBranch(_ string) (string, error) {
	return p.branchToReturn, p.branchErr
}
func (p *fakePusher) PushBranch(dir, remoteName, branch, originURL, token string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pushCalls = append(p.pushCalls,
		strings.Join([]string{dir, remoteName, branch, originURL, token}, "|"))
	return p.pushErr
}

// recordingProvider captures OpenPR calls.
type recordingProvider struct {
	mu          sync.Mutex
	openPRCalls []git.PROptions
	openPRURL   string
	openPRErr   error
}

func (r *recordingProvider) Kind() string { return "recording" }
func (r *recordingProvider) MintToken(_ context.Context, _ string, _ time.Duration) (*git.MintedToken, error) {
	return nil, nil
}
func (r *recordingProvider) RevokeToken(_ context.Context, _ string) error { return nil }
func (r *recordingProvider) OpenPR(_ context.Context, opts git.PROptions) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.openPRCalls = append(r.openPRCalls, opts)
	return r.openPRURL, r.openPRErr
}

// hookFixture wires a Manager with one agent (whose project has the
// supplied AutoPR setting), plus the supplied pusher + provider, and
// returns the hook function, a log capture, and the agent ID.
func hookFixture(t *testing.T, autoPR bool, pusher BranchPusher, prov git.Provider) (func(SessionLike), *recordedLog, string) {
	t.Helper()
	dir := t.TempDir()
	ps, _ := profile.NewProjectStore(filepath.Join(dir, "p.json"))
	cs, _ := profile.NewClusterStore(filepath.Join(dir, "c.json"))
	_ = ps.Create(&profile.ProjectProfile{
		Name:      "p",
		Git:       profile.GitSpec{URL: "https://github.com/owner/repo", Branch: "main", AutoPR: autoPR},
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
	// Inject a fake token so the hook proceeds past the no-token check.
	m.mu.Lock()
	m.agents[a.ID].GitToken = "fake-tok"
	m.mu.Unlock()

	rec := &recordedLog{}
	hook := PostSessionPRHook(PRHookConfig{
		Manager:  m,
		Provider: prov,
		Pusher:   pusher,
		Now:      func() time.Time { return time.Date(2026, 4, 18, 0, 0, 0, 0, time.UTC) },
	}, rec.append)
	return hook, rec, a.ID
}

type recordedLog struct {
	mu    sync.Mutex
	lines []string
}

func (r *recordedLog) append(format string, args ...interface{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, fmt.Sprintf(format, args...))
}
func (r *recordedLog) joined() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.Join(r.lines, "\n")
}

func TestPostSessionPRHook_AutoPRFalse_Skipped(t *testing.T) {
	pusher := &fakePusher{branchToReturn: "feat/x"}
	prov := &recordingProvider{}
	hook, _, agentID := hookFixture(t, false, pusher, prov)
	hook(&fakeSession{id: "s1", agent: agentID, dir: "/work/repo", task: "do x"})
	if len(pusher.pushCalls) != 0 {
		t.Errorf("push called when AutoPR=false: %v", pusher.pushCalls)
	}
	if len(prov.openPRCalls) != 0 {
		t.Errorf("OpenPR called when AutoPR=false: %v", prov.openPRCalls)
	}
}

func TestPostSessionPRHook_AutoPRTrue_PushAndOpenPR(t *testing.T) {
	pusher := &fakePusher{branchToReturn: "feat/x"}
	prov := &recordingProvider{openPRURL: "https://github.com/owner/repo/pull/7"}
	hook, rec, agentID := hookFixture(t, true, pusher, prov)
	hook(&fakeSession{id: "s1", agent: agentID, dir: "/work/repo", task: "do x"})

	if len(pusher.pushCalls) != 1 {
		t.Fatalf("push calls=%d want 1", len(pusher.pushCalls))
	}
	if !strings.Contains(pusher.pushCalls[0], "fake-tok") ||
		!strings.Contains(pusher.pushCalls[0], "feat/x") ||
		!strings.Contains(pusher.pushCalls[0], "https://github.com/owner/repo") {
		t.Errorf("push call missing fields: %s", pusher.pushCalls[0])
	}
	if len(prov.openPRCalls) != 1 {
		t.Fatalf("OpenPR calls=%d want 1", len(prov.openPRCalls))
	}
	pr := prov.openPRCalls[0]
	if pr.Repo != "owner/repo" || pr.HeadBranch != "feat/x" || pr.BaseBranch != "main" {
		t.Errorf("PROptions wrong: %+v", pr)
	}
	if !strings.Contains(rec.joined(), "PR opened") {
		t.Errorf("log missing PR opened line: %s", rec.joined())
	}
}

func TestPostSessionPRHook_NotAWorkerSession(t *testing.T) {
	pusher := &fakePusher{}
	prov := &recordingProvider{}
	hook, _, _ := hookFixture(t, true, pusher, prov)
	hook(&fakeSession{id: "s1", agent: "", dir: "/work/repo", task: "do x"})
	if len(pusher.pushCalls) != 0 || len(prov.openPRCalls) != 0 {
		t.Error("hook should be no-op without agent_id")
	}
}

func TestPostSessionPRHook_DetachedHEAD(t *testing.T) {
	pusher := &fakePusher{branchToReturn: "HEAD"}
	prov := &recordingProvider{}
	hook, rec, agentID := hookFixture(t, true, pusher, prov)
	hook(&fakeSession{id: "s1", agent: agentID, dir: "/w", task: "x"})
	if len(pusher.pushCalls) != 0 || len(prov.openPRCalls) != 0 {
		t.Error("hook should not push/PR on detached HEAD")
	}
	if !strings.Contains(rec.joined(), "branch") {
		t.Errorf("log missing branch warning: %s", rec.joined())
	}
}

func TestPostSessionPRHook_PushFailure(t *testing.T) {
	pusher := &fakePusher{branchToReturn: "feat/x", pushErr: errors.New("auth denied")}
	prov := &recordingProvider{}
	hook, rec, agentID := hookFixture(t, true, pusher, prov)
	hook(&fakeSession{id: "s1", agent: agentID, dir: "/w", task: "x"})
	if len(prov.openPRCalls) != 0 {
		t.Error("OpenPR should not run after push failure")
	}
	if !strings.Contains(rec.joined(), "push failed") {
		t.Errorf("log missing push-failed: %s", rec.joined())
	}
}

func TestPostSessionPRHook_OpenPRFailure(t *testing.T) {
	pusher := &fakePusher{branchToReturn: "feat/x"}
	prov := &recordingProvider{openPRErr: errors.New("rate limited")}
	hook, rec, agentID := hookFixture(t, true, pusher, prov)
	hook(&fakeSession{id: "s1", agent: agentID, dir: "/w", task: "x"})
	if !strings.Contains(rec.joined(), "OpenPR failed") {
		t.Errorf("log missing OpenPR failure: %s", rec.joined())
	}
}
