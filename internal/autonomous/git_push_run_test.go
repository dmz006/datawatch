// Push-on-complete integration tests: proves Manager.Run actually calls
// pushValidatedWork where it should — once per verified task (branch
// policy pushes the work branch early; the default_branch policy stays
// silent until PRD-completion) and once at PRD completion — and never
// touches the remote's default branch while the PRD has not completed.
//
// Setup shape follows git_completion_run_test.go: real temp git repos
// only (t.TempDir), a freshly created never-pre-seeded bare remote as
// the "origin" the PRD's clone pushes to, fake spawn/verify closures.
// AutoPR is left off and no worktree is involved (directory mode, no
// project profile), so the push-on-complete engine is the only source
// of pushes in every test here.
package autonomous

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/git"
)

// pushRunFixture builds the shared harness: a temp bare remote "origin"
// seeded with one commit on its main branch, a temp clone of it (with
// git identity configured and a dedicated "work" branch checked out, so
// the branch policy has a work branch distinct from the default branch
// to push), and an approved PRD running in that clone with two
// sequential tasks (second depends on first, so the first task is
// fully verified+pushed before the second one starts).
//
// Returns the manager, the PRD, the clone dir, the bare dir, and the
// bare remote's initial main-branch SHA (what refs/heads/main must
// still read until a default_branch-policy PRD-completion push).
func pushRunFixture(t *testing.T, autoFixRetries int) (*Manager, *PRD, string, string, string) {
	t.Helper()

	bareDir := filepath.Join(t.TempDir(), "remote.git")
	if out, err := exec.Command("git", "init", "--bare", "-q", "-b", "main", bareDir).CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, out)
	}

	// Seed the bare remote from a scratch repo (own fresh history —
	// do NOT share a remote with gitCompletionFixture's test, same
	// lesson recorded there).
	src := scratchGitRepo(t)
	srcRun := func(args ...string) {
		if out, err := runGitIn(src, args...); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	srcRun("remote", "add", "origin", bareDir)
	srcRun("push", "-q", "-u", "origin", "main")
	initialMain, err := runGitIn(src, "rev-parse", "main")
	if err != nil {
		t.Fatalf("rev-parse initial main: %v", err)
	}
	initialMain = strings.TrimSpace(initialMain)

	// The clone the PRD runs in.
	cloneDir := filepath.Join(t.TempDir(), "work")
	if out, err := exec.Command("git", "clone", "-q", bareDir, cloneDir).CombinedOutput(); err != nil {
		t.Fatalf("git clone: %v\n%s", err, out)
	}
	cloneRun := func(args ...string) {
		if out, err := runGitIn(cloneDir, args...); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	cloneRun("config", "user.name", "test")
	cloneRun("config", "user.email", "test@example.com")
	cloneRun("checkout", "-q", "-b", "work") // the PRD's work branch (distinct from the remote default, main)

	cfg := DefaultConfig()
	cfg.AutoFixRetries = autoFixRetries
	m, err := NewManager(t.TempDir(), cfg, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	prd, err := m.CreatePRD("spec", cloneDir, "", "", "") // ProjectDir set -> directory mode, no worktree
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if err := m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{
		{Title: "first", Spec: "do one"},
		{Title: "second", Spec: "do two"},
	}}}); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Story[0].Tasks[1].DependsOn = []string{prd.Story[0].Tasks[0].ID} // sequential: first fully done before second starts
	prd.Status = PRDApproved
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	return m, prd, cloneDir, bareDir, initialMain
}

// pushTestCommit makes one real commit in dir (what the fake spawn
// closure does for a task) and returns the new HEAD SHA.
func pushTestCommit(t *testing.T, dir, name string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name+".txt"), []byte(name+"\n"), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	commitRun := func(args ...string) {
		if out, err := runGitIn(dir, args...); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	commitRun("add", "-A")
	commitRun("commit", "-q", "-m", "task "+name)
	sha, err := runGitIn(dir, "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}
	return strings.TrimSpace(sha)
}

// bareRef resolves refs/heads/<name> on the bare remote (via the
// clone's origin); returns "" when the ref does not exist.
func bareRef(t *testing.T, cloneDir, branch string) string {
	t.Helper()
	out, err := runGitIn(cloneDir, "ls-remote", "origin", "refs/heads/"+branch)
	if err != nil {
		t.Fatalf("ls-remote origin refs/heads/%s: %v", branch, err)
	}
	if out == "" {
		return ""
	}
	return strings.TrimSpace(strings.Fields(out)[0])
}

// TestRun_PushesWorkBranchAfterEachVerifiedTask — branch policy
// (the default): the remote gets the work branch as soon as the first
// task is verified and saved, i.e. BEFORE the PRD completes (asserted
// from the second task's verification closure, by which point the
// first task is done but the PRD is not), and the remote's default
// branch (main) is left untouched by the whole PRD.
func TestRun_PushesWorkBranchAfterEachVerifiedTask(t *testing.T) {
	m, prd, cloneDir, bareDir, initialMain := pushRunFixture(t, 0)
	// No policy override: daemon config has no git section in tests,
	// so the engine defaults to branch policy / origin.
	_ = bareDir // the bare is asserted through the clone's origin remote

	spawn := func(_ context.Context, req SpawnRequest) (SpawnResult, error) {
		pushTestCommit(t, req.ProjectDir, req.Title)
		return SpawnResult{SessionID: "s-" + req.TaskID}, nil
	}
	verify := func(_ context.Context, _ *PRD, task *Task) (VerificationResult, error) {
		if task.Title == "second" {
			// The first task is already verified + saved as completed
			// (its post-verification push has run) and the PRD has NOT
			// completed yet (the second task is still in flight).
			sha := bareRef(t, cloneDir, "work")
			if sha == "" {
				t.Fatalf("after the first verified task, the bare remote must have the work branch pushed; refs/heads/work is missing")
			}
			if got := bareRef(t, cloneDir, "main"); got != initialMain {
				t.Fatalf("branch policy must never touch the remote default branch: refs/heads/main = %s, want initial %s", got, initialMain)
			}
		}
		return VerificationResult{OK: true, Summary: "ok"}, nil
	}
	if err := m.Run(context.Background(), prd.ID, spawn, verify); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got, _ := m.Store().GetPRD(prd.ID)
	if got.Status != PRDCompleted {
		t.Fatalf("PRD status = %s, want completed", got.Status)
	}
	// The work branch ends up at the final work-clone HEAD; main is
	// still exactly where it started.
	finalHead, err := runGitIn(cloneDir, "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}
	if gotSHA := bareRef(t, cloneDir, "work"); gotSHA != strings.TrimSpace(finalHead) {
		t.Errorf("bare refs/heads/work = %s, want final work HEAD %s", gotSHA, finalHead)
	}
	if gotSHA := bareRef(t, cloneDir, "main"); gotSHA != initialMain {
		t.Errorf("bare refs/heads/main = %s, want initial %s", gotSHA, initialMain)
	}
}

// TestRun_DefaultBranchPolicyPushesMainOnlyAtPRDCompletion — with the
// default_branch policy, per-task pushes are skipped entirely (the
// default branch must not move while tasks are still completing —
// asserted from the last task's verification closure), and at PRD
// completion the work HEAD lands on the remote's main as a plain
// fast-forward.
func TestRun_DefaultBranchPolicyPushesMainOnlyAtPRDCompletion(t *testing.T) {
	m, prd, cloneDir, _, initialMain := pushRunFixture(t, 0)

	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Git.PushOnComplete = git.PushDefaultBranch // per-PRD override -> default_branch policy
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}

	spawn := func(_ context.Context, req SpawnRequest) (SpawnResult, error) {
		pushTestCommit(t, req.ProjectDir, req.Title)
		return SpawnResult{SessionID: "s-" + req.TaskID}, nil
	}
	verify := func(_ context.Context, _ *PRD, task *Task) (VerificationResult, error) {
		if task.Title == "second" {
			if got := bareRef(t, cloneDir, "main"); got != initialMain {
				t.Fatalf("default_branch policy must not push while a task is completing: refs/heads/main = %s, want initial %s", got, initialMain)
			}
		}
		return VerificationResult{OK: true, Summary: "ok"}, nil
	}
	if err := m.Run(context.Background(), prd.ID, spawn, verify); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got, _ := m.Store().GetPRD(prd.ID)
	if got.Status != PRDCompleted {
		t.Fatalf("PRD status = %s, want completed", got.Status)
	}
	// The completion push fast-forwarded main to the work HEAD.
	finalHead, err := runGitIn(cloneDir, "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}
	if gotSHA := bareRef(t, cloneDir, "main"); gotSHA != strings.TrimSpace(finalHead) {
		t.Errorf("bare refs/heads/main = %s, want final work HEAD %s", gotSHA, finalHead)
	}
	if gotPolicy := got.Git.PushOnComplete; gotPolicy != git.PushDefaultBranch {
		t.Errorf("PRD.Git.PushOnComplete = %q, want %q", gotPolicy, git.PushDefaultBranch)
	}
}

// TestRun_FailingPRDNeverPushesDefaultBranch — the default_branch
// policy must stay inert when a task fails: the PRD has not completed
// (quality gates/verification never passed), so the remote's default
// branch must still point at its initial commit after Run.
func TestRun_FailingPRDNeverPushesDefaultBranch(t *testing.T) {
	m, prd, cloneDir, _, initialMain := pushRunFixture(t, 0)

	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Git.PushOnComplete = git.PushDefaultBranch
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}

	spawn := func(_ context.Context, req SpawnRequest) (SpawnResult, error) {
		// A failed task commits nothing: the work tree never advances,
		// so there is nothing a default-branch push could have moved
		// anyway — the assertion is about the policy staying inert.
		return SpawnResult{SessionID: "s-" + req.TaskID}, nil
	}
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		return VerificationResult{OK: false, Summary: "quality gate failed"}, nil
	}
	if err := m.Run(context.Background(), prd.ID, spawn, verify); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got, _ := m.Store().GetPRD(prd.ID)
	if got.Status == PRDCompleted {
		t.Fatalf("PRD with a failed task must not complete, got status %s", got.Status)
	}
	if gotSHA := bareRef(t, cloneDir, "main"); gotSHA != initialMain {
		t.Errorf("a failing PRD must never push the default branch: refs/heads/main = %s, want initial %s", gotSHA, initialMain)
	}
}
