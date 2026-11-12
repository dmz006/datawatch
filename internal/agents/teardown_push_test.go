package agents

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/dmz006/datawatch/internal/git"
	"github.com/dmz006/datawatch/internal/profile"
)

// ── git fixture ────────────────────────────────────────────────────────

// teardownGitFixture creates a temp bare remote plus a local clone with
// one checkout of `branch` holding 1 bootstrap commit +
// `extraCommits` unpushed work commits. Returns paths to the bare
// remote (the "origin" URL) and the clone.
func teardownGitFixture(t *testing.T, branch string, extraCommits int) (remotePath, cloneDir string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not available")
	}
	root := t.TempDir()
	remotePath = filepath.Join(root, "remote.git")
	cloneDir = filepath.Join(root, "clone")
	if out, err := exec.Command("git", "init", "--bare", remotePath).CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, out)
	}
	if out, err := exec.Command("git", "clone", remotePath, cloneDir).CombinedOutput(); err != nil {
		t.Fatalf("git clone: %v\n%s", err, out)
	}
	commit := func(msg string) {
		_, err := exec.Command("git", "-C", cloneDir, "-c", "user.name=t",
			"-c", "user.email=t@example.com", "commit", "--allow-empty", "-m", msg).CombinedOutput()
		if err != nil {
			t.Fatalf("git commit %q: %v", msg, err)
		}
	}
	commit("bootstrap")
	if out, err := exec.Command("git", "-C", cloneDir, "checkout", "-b", branch).CombinedOutput(); err != nil {
		t.Fatalf("git checkout -b %s: %v\n%s", branch, err, out)
	}
	for i := 0; i < extraCommits; i++ {
		commit("work")
	}
	return remotePath, cloneDir
}

// remoteHasBranch asserts the bare remote (or any repo) has the branch.
func remoteHasBranch(t *testing.T, repoDir, branch string) {
	t.Helper()
	if out, err := exec.Command("git", "-C", repoDir, "rev-parse", "--verify",
		"refs/heads/"+branch).CombinedOutput(); err != nil {
		t.Fatalf("branch %s missing in %s: %v\n%s", branch, repoDir, err, out)
	}
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ── fake TeardownPusher driver (runs real git against the temp repo) ─

// teardownFakeDriver implements TeardownPusher on top of the real
// internal/git helpers against (cloneDir). It records call order so
// tests can assert push-strictly-before-terminate.
type teardownFakeDriver struct {
	*fakeDriver
	mu       sync.Mutex
	log      []string
	pushErr  error
	cloneDir string
}

func (f *teardownFakeDriver) record(s string) {
	f.mu.Lock()
	f.log = append(f.log, s)
	f.mu.Unlock()
}

func (f *teardownFakeDriver) calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.log...)
}

func (f *teardownFakeDriver) PushUnpushed(ctx context.Context, a *Agent) (TeardownPushReport, error) {
	f.record("push")
	if f.pushErr != nil {
		return TeardownPushReport{Attempted: true, Branch: a.Branch}, f.pushErr
	}
	count, branch, err := git.UnpushedCommits(ctx, f.cloneDir, "origin")
	if err != nil {
		return TeardownPushReport{Attempted: true, Branch: a.Branch}, err
	}
	res := git.PushToBranch(ctx, f.cloneDir, "origin", branch)
	rep := TeardownPushReport{
		Attempted: true,
		Pushed:    res.Pushed || res.NothingToPush,
		Count:     count,
		Branch:    branch,
	}
	if !res.Pushed && !res.NothingToPush {
		rep.Reason = res.Reason
	}
	return rep, nil
}

func (f *teardownFakeDriver) Terminate(ctx context.Context, a *Agent) error {
	f.record("terminate")
	return f.fakeDriver.Terminate(ctx, a)
}

// teardownSpawn wires manager fixture + project/cluster profiles + the
// driver under test, then spawns one agent.
func teardownSpawn(t *testing.T, d Driver, projectGitURL string, branch string) (*Manager, string) {
	t.Helper()
	m, ps, cs, _ := managerFixture(t)
	if err := ps.Create(&profile.ProjectProfile{
		Name:      "p",
		Git:       profile.GitSpec{URL: projectGitURL, Branch: branch},
		ImagePair: profile.ImagePair{Agent: "agent-claude", Sidecar: "lang-go"},
		Memory:    profile.MemorySpec{Mode: profile.MemorySyncBack},
	}); err != nil {
		t.Fatalf("project profile: %v", err)
	}
	if err := cs.Create(&profile.ClusterProfile{
		Name:    "docker-local",
		Kind:    profile.ClusterDocker,
		Context: "local",
	}); err != nil {
		t.Fatalf("cluster profile: %v", err)
	}
	m.RegisterDriver(d)
	a, err := m.Spawn(context.Background(), SpawnRequest{
		ProjectProfile: "p",
		ClusterProfile: "docker-local",
		Task:           "echo hi",
		Branch:         branch,
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	return m, a.ID
}

// ── Manager-level tests ────────────────────────────────────────────────

// TestTeardownPushRunsBeforeTerminateAndStoresReport — the canonical
// ordering + report contract: the pusher runs before driver.Terminate,
// the report lands on the Agent, the remote receives the branch, and a
// successful push does NOT fire the failure hook.
func TestTeardownPushRunsBeforeTerminateAndStoresReport(t *testing.T) {
	const branch = "worker-42"
	remotePath, cloneDir := teardownGitFixture(t, branch, 1)

	d := &teardownFakeDriver{fakeDriver: &fakeDriver{kind: "docker"}, cloneDir: cloneDir}
	m, id := teardownSpawn(t, d, remotePath, branch)

	hookFires := 0
	m.OnTeardownPushFailure = func(*Agent, TeardownPushReport) { hookFires++ }

	if err := m.Terminate(context.Background(), id); err != nil {
		t.Fatalf("Terminate: %v", err)
	}
	if got, want := d.calls(), []string{"push", "terminate"}; !sameStrings(got, want) {
		t.Fatalf("call order = %v, want %v (push must run before terminate)", got, want)
	}
	snap := m.Get(id)
	if snap == nil {
		t.Fatal("agent vanished after Terminate")
	}
	if snap.TeardownPush == nil {
		t.Fatal("agent has no TeardownPush report")
	}
	rep := snap.TeardownPush
	if !rep.Attempted || !rep.Pushed {
		t.Errorf("report = %+v, want attempted+pushed", rep)
	}
	if rep.Count < 1 {
		t.Errorf("report.Count = %d, want >= 1 unpushed commit pushed", rep.Count)
	}
	if rep.Branch != branch {
		t.Errorf("report.Branch = %q, want %q", rep.Branch, branch)
	}
	if hookFires != 0 {
		t.Errorf("failure hook fired %d times on successful push, want 0", hookFires)
	}
	remoteHasBranch(t, remotePath, branch)
}

// TestTeardownPushFailingPusherStillTerminates — a pusher that errors
// (or times out the same way) must never block termination; the error
// is recorded on the Agent and the failure hook fires before the
// driver destroys the worker.
func TestTeardownPushFailingPusherStillTerminates(t *testing.T) {
	const branch = "worker-42"
	remotePath, cloneDir := teardownGitFixture(t, branch, 1)

	d := &teardownFakeDriver{
		fakeDriver: &fakeDriver{kind: "docker"},
		cloneDir:   cloneDir,
		pushErr:    context.DeadlineExceeded,
	}
	m, id := teardownSpawn(t, d, remotePath, branch)

	hookFires := 0
	var hookRep TeardownPushReport
	m.OnTeardownPushFailure = func(a *Agent, rep TeardownPushReport) {
		hookFires++
		hookRep = rep
		d.record("hook")
	}

	if err := m.Terminate(context.Background(), id); err != nil {
		t.Fatalf("Terminate must not fail on pusher error: %v", err)
	}
	if !d.terminated[id] {
		t.Error("driver.Terminate was not reached — pusher error blocked termination")
	}
	if got, want := d.calls(), []string{"push", "hook", "terminate"}; !sameStrings(got, want) {
		t.Errorf("call order = %v, want %v", got, want)
	}
	if hookFires != 1 {
		t.Fatalf("failure hook fired %d times, want 1", hookFires)
	}
	if hookRep.Pushed {
		t.Error("hook report says pushed, want not pushed")
	}
	if !strings.Contains(hookRep.Reason, "deadline") {
		t.Errorf("hook report reason = %q, want the pusher error", hookRep.Reason)
	}
	snap := m.Get(id)
	if snap.TeardownPush == nil {
		t.Fatal("agent has no TeardownPush report after failing push")
	}
	if snap.TeardownPush.Pushed || !snap.TeardownPush.Attempted {
		t.Errorf("stored report = %+v, want attempted, not pushed", snap.TeardownPush)
	}
}

// TestTeardownPushPolicyNeverSkips — with git.push_on_complete=never
// the push-first step is skipped entirely: no pusher call, no report,
// no hook.
func TestTeardownPushPolicyNeverSkips(t *testing.T) {
	const branch = "worker-42"
	remotePath, cloneDir := teardownGitFixture(t, branch, 1)

	d := &teardownFakeDriver{fakeDriver: &fakeDriver{kind: "docker"}, cloneDir: cloneDir}
	m, id := teardownSpawn(t, d, remotePath, branch)
	m.PushPolicy = func() string { return "never" }
	hookFires := 0
	m.OnTeardownPushFailure = func(*Agent, TeardownPushReport) { hookFires++ }

	if err := m.Terminate(context.Background(), id); err != nil {
		t.Fatalf("Terminate: %v", err)
	}
	if got, want := d.calls(), []string{"terminate"}; !sameStrings(got, want) {
		t.Errorf("call order = %v, want %v (policy=never must skip the push)", got, want)
	}
	if snap := m.Get(id); snap.TeardownPush != nil {
		t.Errorf("agent kept a TeardownPush report (+%v), want none", snap.TeardownPush)
	}
	if hookFires != 0 {
		t.Errorf("failure hook fired %d times, want 0", hookFires)
	}
}

// TestTeardownPushDriverWithoutPusherUnchanged — a driver that does
// not implement TeardownPusher terminates exactly as before: no
// report, no hook, no error.
func TestTeardownPushDriverWithoutPusherUnchanged(t *testing.T) {
	const branch = "worker-42"
	remotePath, _ := teardownGitFixture(t, branch, 1)

	d := &fakeDriver{kind: "docker"}
	m, id := teardownSpawn(t, d, remotePath, branch)
	hookFires := 0
	m.OnTeardownPushFailure = func(*Agent, TeardownPushReport) { hookFires++ }

	if err := m.Terminate(context.Background(), id); err != nil {
		t.Fatalf("Terminate: %v", err)
	}
	if !d.terminated[id] {
		t.Error("driver.Terminate was not reached")
	}
	if snap := m.Get(id); snap.TeardownPush != nil {
		t.Errorf("agent kept a TeardownPush report (+%v), want none", snap.TeardownPush)
	}
	if hookFires != 0 {
		t.Errorf("failure hook fired %d times, want 0", hookFires)
	}
}

// TestK8sTeardownPushReportsUnavailable — the k8s driver cannot exec
// into the worker, so it reports Attempted=false with the standard
// reason (the Manager's failure hook still surfaces it).
func TestK8sTeardownPushReportsUnavailable(t *testing.T) {
	d := NewK8sDriver("kubectl", "", "v0", "http://127.0.0.1:9")
	rep, err := d.PushUnpushed(context.Background(), &Agent{ID: "agent-1"})
	if err != nil {
		t.Fatalf("PushUnpushed: %v", err)
	}
	if rep.Attempted || rep.Pushed {
		t.Errorf("report = %+v, want attempted=false pushed=false", rep)
	}
	if rep.Reason != "driver cannot run commands in the worker" {
		t.Errorf("reason = %q, want the standard unavailable reason", rep.Reason)
	}
}

// ── DockerDriver command-construction test ─────────────────────────────

// fakeDockerScript is the `docker` CLI stand-in: it emulates
// `docker exec <cid> <cmd...>` by running <cmd...> locally (in
// FAKE_DOCKER_CWD) and logs every invocation for assertions.
const fakeDockerScript = `#!/bin/sh
[ -n "$FAKE_DOCKER_LOG" ] && echo "$*" >> "$FAKE_DOCKER_LOG"
if [ "$1" = "exec" ]; then shift; shift; fi
[ -n "$FAKE_DOCKER_CWD" ] && cd "$FAKE_DOCKER_CWD" || exit 97
exec "$@"
`

// TestDockerTeardownPushRunsRealPushAndBuildsGitCommands — the docker
// implementation must build the expected git command line (branch
// detection, unpushed count, plain `push --set-upstream`, never
// `--force`) and the script must actually deliver the worker's commits
// to the bare remote.
func TestDockerTeardownPushRunsRealPushAndBuildsGitCommands(t *testing.T) {
	const branch = "worker-42"
	remotePath, cloneDir := teardownGitFixture(t, branch, 1)

	bindir := t.TempDir()
	bin := filepath.Join(bindir, "docker")
	if err := os.WriteFile(bin, []byte(fakeDockerScript), 0o755); err != nil {
		t.Fatal(err)
	}
	logFile := filepath.Join(t.TempDir(), "docker-calls.log")
	t.Setenv("FAKE_DOCKER_CWD", cloneDir)
	t.Setenv("FAKE_DOCKER_LOG", logFile)

	d := NewDockerDriver(bin, "", "v0", "http://127.0.0.1:9")
	a := &Agent{
		ID:             "agent-1",
		DriverInstance: "cid-1",
		Branch:         branch,
		project:        &profile.ProjectProfile{Git: profile.GitSpec{URL: remotePath, Branch: branch}},
	}

	rep, err := d.PushUnpushed(context.Background(), a)
	if err != nil {
		t.Fatalf("PushUnpushed: %v", err)
	}
	if !rep.Attempted {
		t.Errorf("report = %+v, want attempted", rep)
	}
	if !rep.Pushed {
		t.Errorf("report = %+v, want pushed (reason=%q)", rep, rep.Reason)
	}
	if rep.Branch != branch {
		t.Errorf("rep.Branch = %q, want %q", rep.Branch, branch)
	}
	if rep.Count < 1 {
		t.Errorf("rep.Count = %d, want >= 1", rep.Count)
	}
	remoteHasBranch(t, remotePath, branch)

	// The driver must have invoked `docker exec <cid> sh -c <script>`
	// and the script must build the expected git commands.
	raw, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("docker call log: %v", err)
	}
	calls := string(raw)
	for _, want := range []string{"exec", "cid-1", "sh", "-c"} {
		if !strings.Contains(calls, want) {
			t.Errorf("docker invocation %q missing expected token %q", calls, want)
		}
	}
	for _, want := range []string{
		"symbolic-ref --short HEAD",
		"rev-list --count HEAD --not --remotes=origin",
		"push --set-upstream origin",
	} {
		if !strings.Contains(calls, want) {
			t.Errorf("built script missing git command %q", want)
		}
	}
	if strings.Contains(calls, "--force") {
		t.Error("built script uses --force; teardown push must be fast-forward-only, never force")
	}
}

// TestDockerTeardownPushNoContainer — without a container to exec
// into the driver reports Attempted=false and a reason, never an
// error, so termination can proceed.
func TestDockerTeardownPushNoContainer(t *testing.T) {
	d := NewDockerDriver("docker-not-on-path", "", "v0", "http://127.0.0.1:9")
	rep, err := d.PushUnpushed(context.Background(), &Agent{ID: "agent-1"})
	if err != nil {
		t.Fatalf("PushUnpushed: %v", err)
	}
	if rep.Attempted || rep.Pushed {
		t.Errorf("report = %+v, want attempted=false pushed=false", rep)
	}
	if rep.Reason == "" {
		t.Error("expected a non-empty reason")
	}
}
