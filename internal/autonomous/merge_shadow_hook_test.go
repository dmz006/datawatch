package autonomous

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/mergeshadow"
)

// shadowGit runs git in dir with a fixed identity and returns its output.
func shadowGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func shadowWrite(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// shadowRepo builds a project repository: main has a version file and a
// changelog, and branch "auto" (the Automaton's work branch) adds feat.go.
// It returns the repository path.
func shadowRepo(t *testing.T) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(repo, 0o750); err != nil {
		t.Fatal(err)
	}
	shadowGit(t, repo, "init", "-q", "-b", "main")
	shadowWrite(t, repo, "version.go", "package main\n\nvar Version = \"1.2.0\"\n")
	shadowWrite(t, repo, "CHANGELOG.md", "# Changelog\n\n## [1.2.0] - 2026-11-01\n\n- old\n")
	shadowWrite(t, repo, "README.md", "# Project\n")
	shadowGit(t, repo, "add", ".")
	shadowGit(t, repo, "commit", "-q", "-m", "base")
	shadowGit(t, repo, "checkout", "-q", "-b", "auto")
	shadowWrite(t, repo, "feat.go", "package main\n")
	shadowGit(t, repo, "add", ".")
	shadowGit(t, repo, "commit", "-q", "-m", "feature")
	shadowGit(t, repo, "checkout", "-q", "main")
	return repo
}

// init makes the finish-time shadow run a no-op for every test in this
// package. The real run is put back, with fake gates, by stubShadowGates in
// the hook tests below.
func init() {
	mergeShadowRun = func(context.Context, mergeshadow.Input) (*mergeshadow.MergePlan, error) {
		return nil, nil
	}
}

// stubShadowGates puts the real runner back with a fake gate runner that
// counts its calls and passes every gate. The previous run is restored on
// cleanup.
func stubShadowGates(t *testing.T) *int {
	t.Helper()
	calls := new(int)
	fake := func(_ context.Context, _ string, _ time.Duration) []mergeshadow.GateResult {
		*calls++
		return []mergeshadow.GateResult{
			{Name: "build", Status: mergeshadow.GatePass},
			{Name: "lint", Status: mergeshadow.GatePass},
			{Name: "test", Status: mergeshadow.GatePass},
		}
	}
	prev := mergeShadowRun
	mergeShadowRun = func(ctx context.Context, in mergeshadow.Input) (*mergeshadow.MergePlan, error) {
		return mergeshadow.Runner{Gates: fake}.Run(ctx, in)
	}
	t.Cleanup(func() { mergeShadowRun = prev })
	return calls
}

// finishedShadowPRD is an Automaton that has just reached the finished state:
// a work branch in the project repository and one changelog entry task.
func finishedShadowPRD(repo string) *PRD {
	return &PRD{
		ID:         "p-shadow",
		ProjectDir: repo,
		Git:        GitWorkflow{Branch: "auto", BaseBranch: "main"},
		Story: []Story{{
			ID: "s1",
			Tasks: []Task{
				{ID: "t-work", Kind: "", SessionID: "sess-work"},
				{ID: "t-changelog", Kind: DefectTaskKindChangelog, SessionID: "sess-changelog"},
			},
		}},
	}
}

// shadowManager is a manager with a data directory, the merge pipeline that
// names the version file and a reader that returns the changelog entry.
func shadowManager(t *testing.T) *Manager {
	t.Helper()
	m := &Manager{dataDir: t.TempDir()}
	m.SetMergePipelineConfig(config.MergePipelineConfig{VersionFiles: []string{"version.go"}})
	m.sessionOutputFn = func(sessionID string) (string, bool) {
		if sessionID == "sess-changelog" {
			return "## [v1.3.0] - 2026-11-09\n\n- shipped\n", true
		}
		return "", false
	}
	return m
}

func TestMergeShadowHookDisabledDoesNothing(t *testing.T) {
	repo := shadowRepo(t)
	calls := stubShadowGates(t)
	mainBefore := strings.TrimSpace(shadowGit(t, repo, "rev-parse", "main"))
	autoBefore := strings.TrimSpace(shadowGit(t, repo, "rev-parse", "auto"))

	m := shadowManager(t)
	off := false
	m.SetMergeShadowConfig(config.MergeShadowConfig{Enabled: &off})

	if done := m.startMergeShadow(finishedShadowPRD(repo)); done != nil {
		t.Fatal("a run was started with merge_shadow disabled")
	}
	if *calls != 0 {
		t.Errorf("gates ran %d times with merge_shadow disabled", *calls)
	}
	if _, err := os.Stat(filepath.Join(m.dataDir, "merge-plans")); !os.IsNotExist(err) {
		t.Errorf("a plan directory was written with merge_shadow disabled: %v", err)
	}
	if got := strings.TrimSpace(shadowGit(t, repo, "rev-parse", "main")); got != mainBefore {
		t.Errorf("main moved with merge_shadow disabled: %s -> %s", mainBefore, got)
	}
	if got := strings.TrimSpace(shadowGit(t, repo, "rev-parse", "auto")); got != autoBefore {
		t.Errorf("auto moved with merge_shadow disabled: %s -> %s", autoBefore, got)
	}
}

func TestMergeShadowHookEnabledStoresPlanAndLeavesBranchAlone(t *testing.T) {
	repo := shadowRepo(t)
	calls := stubShadowGates(t)
	mainBefore := strings.TrimSpace(shadowGit(t, repo, "rev-parse", "main"))
	autoBefore := strings.TrimSpace(shadowGit(t, repo, "rev-parse", "auto"))

	m := shadowManager(t) // zero settings: enabled by default
	done := m.startMergeShadow(finishedShadowPRD(repo))
	if done == nil {
		t.Fatal("no run started with merge_shadow at its default")
	}
	select {
	case <-done:
	case <-time.After(2 * time.Minute):
		t.Fatal("shadow run did not finish")
	}

	if *calls != 1 {
		t.Errorf("gates ran %d times, want 1", *calls)
	}
	plan, err := mergeshadow.Load(m.dataDir, "p-shadow")
	if err != nil {
		t.Fatalf("plan not stored: %v", err)
	}
	if plan.Applied {
		t.Error("stored plan marked applied")
	}
	if plan.PlannedVersion != "v1.3.0" || plan.ReleaseSize != mergeshadow.ReleaseSizeMinor {
		t.Errorf("planned %q size %q, want v1.3.0 minor", plan.PlannedVersion, plan.ReleaseSize)
	}
	if plan.ChangelogSource != "t-changelog" {
		t.Errorf("changelog source %q, want the changelog task", plan.ChangelogSource)
	}
	if got := strings.TrimSpace(shadowGit(t, repo, "rev-parse", "main")); got != mainBefore {
		t.Errorf("main moved: %s -> %s", mainBefore, got)
	}
	if got := strings.TrimSpace(shadowGit(t, repo, "rev-parse", "auto")); got != autoBefore {
		t.Errorf("project branch auto moved: %s -> %s", autoBefore, got)
	}
	if _, err := os.Stat(filepath.Join(repo, "feat.go")); !os.IsNotExist(err) {
		t.Errorf("the merge reached the project checkout: %v", err)
	}
	if strings.Contains(shadowGit(t, repo, "worktree", "list"), "merge-shadow") {
		t.Error("trial worktree left registered in the project")
	}
}

func TestMergeShadowHookSkipsAutomatonWithoutBranch(t *testing.T) {
	repo := shadowRepo(t)
	calls := stubShadowGates(t)
	m := shadowManager(t)
	prd := finishedShadowPRD(repo)
	prd.Git.Branch = ""

	if done := m.startMergeShadow(prd); done != nil {
		t.Fatal("a run was started for an Automaton with no work branch")
	}
	if *calls != 0 {
		t.Errorf("gates ran %d times for an Automaton with no branch", *calls)
	}
}

func TestMergeShadowHookSkipsWithoutDataDir(t *testing.T) {
	repo := shadowRepo(t)
	stubShadowGates(t)
	m := &Manager{}
	if done := m.startMergeShadow(finishedShadowPRD(repo)); done != nil {
		t.Fatal("a run was started with no data directory")
	}
}

func TestMergeShadowSettingsDefaultOn(t *testing.T) {
	m := &Manager{}
	if !m.mergeShadowSettings().EffectiveEnabled() {
		t.Error("a manager with no settings reads merge_shadow as off; the default is on")
	}
	off := false
	m.SetMergeShadowConfig(config.MergeShadowConfig{Enabled: &off})
	if m.mergeShadowSettings().EffectiveEnabled() {
		t.Error("explicit enabled=false read as on")
	}
}
