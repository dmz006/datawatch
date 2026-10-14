package autonomous

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// driftAuditRecorder captures SetAuditFn calls.
type driftAuditRecorder struct {
	mu    sync.Mutex
	calls []map[string]any
	acts  []string
}

func (r *driftAuditRecorder) fn(action string, details map[string]any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.acts = append(r.acts, action)
	r.calls = append(r.calls, details)
}

// driftCheckFixture builds a Manager whose PRD points at a real temp
// git repo with base.txt + pre.txt committed and pre.txt then modified
// (dirty) before the "worker" runs. The returned task carries the
// captured PreTaskSHA/PreTaskDirtyFiles exactly as the spawn path sets
// them.
func driftCheckFixture(t *testing.T, planned []string) (*Manager, *PRD, *Task, string, *driftAuditRecorder) {
	t.Helper()
	m, _, _, _ := apiFixture(t)
	dir := driftTestRepo(t)
	driftWrite(t, dir, "base.txt", "1\n")
	driftWrite(t, dir, "pre.txt", "1\n")
	driftGit(t, dir, "add", "base.txt", "pre.txt")
	driftGit(t, dir, "commit", "-q", "-m", "base")
	driftWrite(t, dir, "pre.txt", "operator edit\n")

	prd, err := m.CreatePRD("drift", dir, "claude-code", "", EffortNormal)
	if err != nil {
		t.Fatal(err)
	}
	task := &Task{ID: "task-1", PRDID: prd.ID, FilesPlanned: planned}
	task.PreTaskSHA = driftGit(t, dir, "rev-parse", "HEAD")
	task.PreTaskDirtyFiles = CapturePreTaskDirtyFiles(context.Background(), dir, nil)
	rec := &driftAuditRecorder{}
	m.SetAuditFn(rec.fn)
	return m, prd, task, dir, rec
}

func driftDecisions(prd *PRD) []Decision {
	var out []Decision
	for _, d := range prd.Decisions {
		if d.Kind == "file_drift_detected" {
			out = append(out, d)
		}
	}
	return out
}

func TestCheckForeignFileDrift_WorktreeModeMakesZeroGitCalls(t *testing.T) {
	m, prd, task, _, rec := driftCheckFixture(t, nil)
	calls := 0
	m.driftGit = func(context.Context, string, ...string) (string, error) {
		calls++
		return "pre.txt\n", nil
	}
	prd.Git.Branch = "automaton/drift"
	if m.checkForeignFileDrift(context.Background(), prd, task) {
		t.Fatal("worktree mode must never block")
	}
	if calls != 0 {
		t.Fatalf("worktree mode made %d git calls, want 0", calls)
	}
	if task.ForeignFileDrift != nil || len(driftDecisions(prd)) != 0 || len(rec.calls) != 0 {
		t.Fatalf("worktree mode recorded something: drift=%v decisions=%v audit=%v", task.ForeignFileDrift, driftDecisions(prd), rec.calls)
	}

	// Same zero-call guarantee for missing ProjectDir / PreTaskSHA.
	prd.Git.Branch = ""
	sha := task.PreTaskSHA
	task.PreTaskSHA = ""
	if m.checkForeignFileDrift(context.Background(), prd, task) || calls != 0 {
		t.Fatalf("empty PreTaskSHA: calls=%d", calls)
	}
	task.PreTaskSHA = sha
	prd.ProjectDir = ""
	if m.checkForeignFileDrift(context.Background(), prd, task) || calls != 0 {
		t.Fatalf("empty ProjectDir: calls=%d", calls)
	}
}

func TestCheckForeignFileDrift_DirtyFileSweptInWarnsByDefault(t *testing.T) {
	m, prd, task, dir, rec := driftCheckFixture(t, []string{"own.txt"})
	calls := 0
	m.driftGit = func(ctx context.Context, d string, args ...string) (string, error) {
		calls++
		return defaultDriftGitRunner(ctx, d, args...)
	}
	driftWrite(t, dir, "own.txt", "task work\n")
	driftGit(t, dir, "add", "own.txt", "pre.txt") // the sweep
	driftGit(t, dir, "commit", "-q", "-m", "task commit")

	if m.checkForeignFileDrift(context.Background(), prd, task) {
		t.Fatal("BlockOnFileDrift default false must not block")
	}
	if calls != 1 {
		t.Fatalf("git calls = %d, want 1", calls)
	}
	if !reflect.DeepEqual(task.ForeignFileDrift, []string{"pre.txt"}) {
		t.Fatalf("ForeignFileDrift = %v, want [pre.txt]", task.ForeignFileDrift)
	}
	if task.Status == TaskBlocked {
		t.Fatal("warn-only must not set TaskBlocked")
	}
	ds := driftDecisions(prd)
	if len(ds) != 1 {
		t.Fatalf("want exactly 1 file_drift_detected decision, got %d", len(ds))
	}
	if ds[0].Actor != "autonomous" || !strings.Contains(ds[0].Note, "task task-1:") || !strings.HasSuffix(ds[0].Note, ": pre.txt") {
		t.Fatalf("decision = %+v", ds[0])
	}
	stored, _ := m.Store().GetPRD(prd.ID)
	if len(driftDecisions(stored)) != 1 {
		t.Fatal("decision not persisted on the stored PRD")
	}
	if len(rec.calls) != 1 || rec.acts[0] != "autonomous_file_drift_detected" {
		t.Fatalf("audit calls = %v %v", rec.acts, rec.calls)
	}
	d := rec.calls[0]
	if d["task_id"] != "task-1" || d["prd_id"] != prd.ID || !reflect.DeepEqual(d["files"], []string{"pre.txt"}) {
		t.Fatalf("audit details = %+v", d)
	}

	// A retry with an earlier finding accumulates (sorted, de-duplicated).
	task.ForeignFileDrift = []string{"zz.txt", "pre.txt"}
	m.checkForeignFileDrift(context.Background(), prd, task)
	if !reflect.DeepEqual(task.ForeignFileDrift, []string{"pre.txt", "zz.txt"}) {
		t.Fatalf("accumulated ForeignFileDrift = %v", task.ForeignFileDrift)
	}
}

func TestCheckForeignFileDrift_BlocksWhenPRDOverrideTrue(t *testing.T) {
	m, prd, task, dir, rec := driftCheckFixture(t, nil)
	on := true
	prd.BlockOnFileDrift = &on
	driftGit(t, dir, "add", "pre.txt")
	driftGit(t, dir, "commit", "-q", "-m", "task commit")

	if !m.checkForeignFileDrift(context.Background(), prd, task) {
		t.Fatal("BlockOnFileDrift=true must block")
	}
	if task.Status != TaskBlocked {
		t.Fatalf("status = %s, want blocked", task.Status)
	}
	if !strings.HasPrefix(task.Error, "foreign_file_drift: ") || !strings.Contains(task.Error, "pre.txt") {
		t.Fatalf("error = %q", task.Error)
	}
	if len(driftDecisions(prd)) != 1 || len(rec.calls) != 1 {
		t.Fatalf("decisions=%d audit=%d, want 1/1", len(driftDecisions(prd)), len(rec.calls))
	}
	// The check never touches the tree: HEAD still holds the sweep commit.
	if got := driftGit(t, dir, "log", "-1", "--format=%s"); got != "task commit" {
		t.Fatalf("HEAD subject = %q — the check must not rewrite history", got)
	}
}

func TestCheckForeignFileDrift_UntrackedFileSweptInIsFlagged(t *testing.T) {
	m, prd, task, dir, rec := driftCheckFixture(t, nil)
	driftWrite(t, dir, "scratch.md", "operator notes\n")
	task.PreTaskDirtyFiles = nil
	task.PreTaskUntrackedFiles = []string{"scratch.md"}
	driftGit(t, dir, "add", "scratch.md")
	driftGit(t, dir, "commit", "-q", "-m", "task commit")

	if m.checkForeignFileDrift(context.Background(), prd, task) {
		t.Fatal("default must not block")
	}
	if !reflect.DeepEqual(task.ForeignFileDrift, []string{"scratch.md"}) {
		t.Fatalf("ForeignFileDrift = %v", task.ForeignFileDrift)
	}
	if len(rec.calls) != 1 {
		t.Fatalf("audit calls = %d", len(rec.calls))
	}
}

func TestCheckForeignFileDrift_PlannedFileNotFlagged(t *testing.T) {
	m, prd, task, dir, rec := driftCheckFixture(t, []string{"./pre.txt"})
	driftGit(t, dir, "add", "pre.txt")
	driftGit(t, dir, "commit", "-q", "-m", "task commit")

	if m.checkForeignFileDrift(context.Background(), prd, task) {
		t.Fatal("planned file must not block")
	}
	if task.ForeignFileDrift != nil || len(driftDecisions(prd)) != 0 || len(rec.calls) != 0 {
		t.Fatalf("planned file flagged: drift=%v decisions=%d audit=%d", task.ForeignFileDrift, len(driftDecisions(prd)), len(rec.calls))
	}
}

// Executor-level: a warn-only finding still ends TaskCompleted, with
// foreign_file_drift set and the decision + audit recorded.
func TestExecutor_ForeignFileDriftWarnOnlyStillCompletes(t *testing.T) {
	m, api, _, _ := apiFixture(t)
	dir := driftTestRepo(t)
	driftWrite(t, dir, "base.txt", "1\n")
	driftWrite(t, dir, "pre.txt", "1\n")
	driftGit(t, dir, "add", "base.txt", "pre.txt")
	driftGit(t, dir, "commit", "-q", "-m", "base")
	driftWrite(t, dir, "pre.txt", "operator edit\n")

	rec := &driftAuditRecorder{}
	m.SetAuditFn(rec.fn)
	prd, err := m.CreatePRD("drift-e2e", dir, "claude-code", "", EffortNormal)
	if err != nil {
		t.Fatal(err)
	}
	_ = m.Store().SetStories(prd.ID, []Story{
		{Title: "S", Tasks: []Task{{Title: "T", Spec: "write own.txt", FilesPlanned: []string{"own.txt"}}}},
	})

	spawn := func(ctx context.Context, r SpawnRequest) (SpawnResult, error) {
		sha := driftGit(t, dir, "rev-parse", "HEAD")
		dirty := CapturePreTaskDirtyFiles(ctx, dir, nil)
		// The "worker" does its own work and sweeps the operator's file in.
		driftWrite(t, dir, "own.txt", "task work\n")
		driftGit(t, dir, "add", "own.txt", "pre.txt")
		driftGit(t, dir, "commit", "-q", "-m", "task commit")
		return SpawnResult{SessionID: "s-" + r.TaskID, PreTaskSHA: sha, PreTaskDirtyFiles: dirty}, nil
	}
	verify := func(context.Context, *PRD, *Task) (VerificationResult, error) {
		return VerificationResult{OK: true}, nil
	}

	got := runToTerminalOrBlocked(t, m, api, prd.ID, spawn, verify)
	if got.Status != PRDCompleted {
		t.Fatalf("PRD status = %s, want completed", got.Status)
	}
	task := got.Story[0].Tasks[0]
	if task.Status != TaskCompleted {
		t.Fatalf("task status = %s, want completed", task.Status)
	}
	if !reflect.DeepEqual(task.ForeignFileDrift, []string{"pre.txt"}) {
		t.Fatalf("ForeignFileDrift = %v, want [pre.txt]", task.ForeignFileDrift)
	}
	if len(driftDecisions(got)) != 1 {
		t.Fatalf("file_drift_detected decisions = %d, want 1", len(driftDecisions(got)))
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.calls) != 1 {
		t.Fatalf("audit calls = %d, want 1", len(rec.calls))
	}
}
