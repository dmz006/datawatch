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

func TestCheckForeignFileDrift_DefaultModeBlocksPreExisting(t *testing.T) {
	m, prd, task, dir, rec := driftCheckFixture(t, []string{"own.txt"})
	driftWrite(t, dir, "own.txt", "task work\n")
	driftGit(t, dir, "add", "own.txt", "pre.txt") // the sweep
	driftGit(t, dir, "commit", "-q", "-m", "task commit")

	if !m.checkForeignFileDrift(context.Background(), prd, task) {
		t.Fatal("DriftMode default \"block\" with no opt-out must block on pre-existing drift")
	}
	if task.Status != TaskBlocked {
		t.Fatalf("status = %s, want blocked", task.Status)
	}
	if !strings.HasPrefix(task.Error, "foreign_file_drift: ") || !strings.Contains(task.Error, "pre.txt") {
		t.Fatalf("error = %q", task.Error)
	}
	if !reflect.DeepEqual(task.ForeignFileDrift, []string{"pre.txt"}) {
		t.Fatalf("ForeignFileDrift = %v, want [pre.txt]", task.ForeignFileDrift)
	}
	// Both groups recorded: in-run (own.txt) and pre-existing (pre.txt).
	ds := driftDecisions(prd)
	if len(ds) != 2 {
		t.Fatalf("want 2 file_drift_detected decisions (in-run + pre-existing), got %d", len(ds))
	}
	if len(rec.calls) != 2 {
		t.Fatalf("audit calls = %d, want 2 (one per group)", len(rec.calls))
	}
}

func TestCheckForeignFileDrift_DirtyFileSweptInWarnsOnExplicitOptOut(t *testing.T) {
	m, prd, task, dir, rec := driftCheckFixture(t, []string{"own.txt"})
	off := false
	prd.BlockOnFileDrift = &off // explicit PRD opt-out from blocking
	calls := 0
	m.driftGit = func(ctx context.Context, d string, args ...string) (string, error) {
		calls++
		return defaultDriftGitRunner(ctx, d, args...)
	}
	driftWrite(t, dir, "own.txt", "task work\n")
	driftGit(t, dir, "add", "own.txt", "pre.txt") // the sweep
	driftGit(t, dir, "commit", "-q", "-m", "task commit")

	if m.checkForeignFileDrift(context.Background(), prd, task) {
		t.Fatal("block_on_file_drift=false on the PRD must opt out of blocking")
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
	if len(ds) != 2 {
		t.Fatalf("want exactly 2 file_drift_detected decisions (in-run own.txt + pre-existing pre.txt), got %d", len(ds))
	}
	var preNote, inRunNote string
	for _, d := range ds {
		switch {
		case strings.Contains(d.Note, "in-run edit by the task's own worker"):
			inRunNote = d.Note
		case strings.Contains(d.Note, "pre-existing"):
			preNote = d.Note
		}
	}
	if inRunNote == "" || !strings.Contains(inRunNote, "own.txt") || inRunNote == preNote {
		t.Fatalf("in-run decision missing/wrong: %q", inRunNote)
	}
	if preNote == "" || !strings.Contains(preNote, "task task-1:") || !strings.HasSuffix(preNote, ": pre.txt") {
		t.Fatalf("pre-existing decision = %q", preNote)
	}
	stored, _ := m.Store().GetPRD(prd.ID)
	if len(driftDecisions(stored)) != 2 {
		t.Fatal("decisions not persisted on the stored PRD")
	}
	if len(rec.calls) != 2 {
		t.Fatalf("audit calls = %d %v, want 2 (one per group)", len(rec.calls), rec.acts)
	}
	var preAudit map[string]any
	for _, d := range rec.calls {
		if d["kind"] == "pre_existing" {
			preAudit = d
		}
	}
	if preAudit == nil {
		t.Fatalf("no pre_existing audit call: %+v", rec.calls)
	}
	if preAudit["task_id"] != "task-1" || preAudit["prd_id"] != prd.ID || !reflect.DeepEqual(preAudit["files"], []string{"pre.txt"}) {
		t.Fatalf("audit details = %+v", preAudit)
	}

	// A retry with an earlier finding accumulates (sorted, de-duplicated).
	task.ForeignFileDrift = []string{"zz.txt", "pre.txt"}
	m.checkForeignFileDrift(context.Background(), prd, task)
	if !reflect.DeepEqual(task.ForeignFileDrift, []string{"pre.txt", "zz.txt"}) {
		t.Fatalf("accumulated ForeignFileDrift = %v", task.ForeignFileDrift)
	}
}

func TestCheckForeignFileDrift_WarnModePreExistingDoesNotBlock(t *testing.T) {
	m, prd, task, dir, rec := driftCheckFixture(t, []string{"own.txt"})
	cfg := DefaultConfig()
	cfg.DriftMode = "warn"
	m.SetConfig(cfg)
	driftWrite(t, dir, "own.txt", "task work\n")
	driftGit(t, dir, "add", "own.txt", "pre.txt")
	driftGit(t, dir, "commit", "-q", "-m", "task commit")

	if m.checkForeignFileDrift(context.Background(), prd, task) {
		t.Fatal("DriftMode=\"warn\" must never block, even on pre-existing drift")
	}
	if task.Status == TaskBlocked {
		t.Fatal("warn mode must not set TaskBlocked")
	}
	if !reflect.DeepEqual(task.ForeignFileDrift, []string{"pre.txt"}) {
		t.Fatalf("ForeignFileDrift = %v, want [pre.txt]", task.ForeignFileDrift)
	}
	if len(driftDecisions(prd)) != 2 || len(rec.calls) != 2 {
		t.Fatalf("warn mode still records everything: decisions=%d audit=%d", len(driftDecisions(prd)), len(rec.calls))
	}
}

func TestCheckForeignFileDrift_ProfileExplicitFalseOptOutDoesNotBlock(t *testing.T) {
	m, prd, task, dir, rec := driftCheckFixture(t, nil)
	p, err := m.CreateGuardrailProfile("lenient", "", nil, boolPtr(false))
	if err != nil {
		t.Fatalf("CreateGuardrailProfile: %v", err)
	}
	if prd, err = m.SetPRDGuardrails(prd.ID, p.ID, nil, nil, nil, nil); err != nil {
		t.Fatalf("SetPRDGuardrails: %v", err)
	}
	driftGit(t, dir, "add", "pre.txt")
	driftGit(t, dir, "commit", "-q", "-m", "task commit")

	if m.checkForeignFileDrift(context.Background(), prd, task) {
		t.Fatal("profile block_on_file_drift=false must opt out of blocking")
	}
	if !reflect.DeepEqual(task.ForeignFileDrift, []string{"pre.txt"}) {
		t.Fatalf("ForeignFileDrift = %v, want [pre.txt]", task.ForeignFileDrift)
	}
	if len(driftDecisions(prd)) != 1 || len(rec.calls) != 1 {
		t.Fatalf("finding must still be recorded: decisions=%d audit=%d", len(driftDecisions(prd)), len(rec.calls))
	}
}

func TestCheckForeignFileDrift_InRunOnlyNeverBlocks(t *testing.T) {
	m, prd, task, dir, rec := driftCheckFixture(t, []string{"own.txt"})
	// The worker commits only its own file — no pre-existing sweep.
	driftWrite(t, dir, "own.txt", "task work\n")
	driftGit(t, dir, "add", "own.txt")
	driftGit(t, dir, "commit", "-q", "-m", "task commit")

	if m.checkForeignFileDrift(context.Background(), prd, task) {
		t.Fatal("in-run findings NEVER block")
	}
	if task.Status == TaskBlocked {
		t.Fatal("in-run findings must not set TaskBlocked")
	}
	if task.ForeignFileDrift != nil {
		t.Fatalf("ForeignFileDrift = %v, want nil (in-run is not foreign drift)", task.ForeignFileDrift)
	}
	ds := driftDecisions(prd)
	if len(ds) != 1 {
		t.Fatalf("want exactly 1 in-run file_drift_detected decision, got %d", len(ds))
	}
	if !strings.Contains(ds[0].Note, "in-run edit by the task's own worker") || !strings.Contains(ds[0].Note, "own.txt") {
		t.Fatalf("in-run decision note = %q", ds[0].Note)
	}
	if len(rec.calls) != 1 || rec.calls[0]["kind"] != "in_run" {
		t.Fatalf("audit calls = %+v, want one in_run call", rec.calls)
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
	off := false
	prd.BlockOnFileDrift = &off
	driftWrite(t, dir, "scratch.md", "operator notes\n")
	task.PreTaskDirtyFiles = nil
	task.PreTaskUntrackedFiles = []string{"scratch.md"}
	driftGit(t, dir, "add", "scratch.md")
	driftGit(t, dir, "commit", "-q", "-m", "task commit")

	if m.checkForeignFileDrift(context.Background(), prd, task) {
		t.Fatal("explicit opt-out must not block")
	}
	if !reflect.DeepEqual(task.ForeignFileDrift, []string{"scratch.md"}) {
		t.Fatalf("ForeignFileDrift = %v", task.ForeignFileDrift)
	}
	if len(rec.calls) != 1 || rec.calls[0]["kind"] != "pre_existing" {
		t.Fatalf("audit calls = %+v; scratch.md was untracked → pre_existing only", rec.calls)
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
	off := false
	prd.BlockOnFileDrift = &off // warn-only e2e: opt out of blocking
	_ = m.Store().SavePRD(prd)
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
	if len(driftDecisions(got)) != 2 {
		t.Fatalf("file_drift_detected decisions = %d, want 2 (in-run own.txt + pre-existing pre.txt)", len(driftDecisions(got)))
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.calls) != 2 {
		t.Fatalf("audit calls = %d, want 2 (one per group)", len(rec.calls))
	}
}

// TestInRunEditWarnsAndDoesNotBlock (case 1): the worker edits a file that
// was clean before the task started and commits it. classifyFileDrift puts it
// in the in-run group, checkForeignFileDrift must return false (never block),
// the task must not be TaskBlocked, and a file_drift_detected decision must be
// recorded.
func TestInRunEditWarnsAndDoesNotBlock(t *testing.T) {
	m, prd, task, dir, rec := driftCheckFixture(t, []string{"own.txt"})
	// own.txt is clean before the task (not in PreTaskDirtyFiles, not
	// untracked). The worker edits it and commits only it.
	driftWrite(t, dir, "own.txt", "task work\n")
	driftGit(t, dir, "add", "own.txt")
	driftGit(t, dir, "commit", "-q", "-m", "task commit")

	if m.checkForeignFileDrift(context.Background(), prd, task) {
		t.Fatal("an in-run edit must never block")
	}
	if task.Status == TaskBlocked {
		t.Fatal("in-run edit must not set TaskBlocked")
	}
	if task.ForeignFileDrift != nil {
		t.Fatalf("ForeignFileDrift = %v, want nil (in-run is not foreign drift)", task.ForeignFileDrift)
	}
	ds := driftDecisions(prd)
	if len(ds) != 1 {
		t.Fatalf("want exactly 1 file_drift_detected decision, got %d", len(ds))
	}
	if !strings.Contains(ds[0].Note, "in-run edit by the task's own worker") || !strings.Contains(ds[0].Note, "own.txt") {
		t.Fatalf("in-run decision note = %q", ds[0].Note)
	}
	if len(rec.calls) != 1 || rec.calls[0]["kind"] != "in_run" {
		t.Fatalf("audit calls = %+v, want one in_run call", rec.calls)
	}
}

// TestUntrackedFileSweptInBlocks (case 2): a file that was untracked before
// the task (an operator's .mcp.json) is swept into the task's own commit by
// the worker. Under the default drift_mode (block) with no opt-out this is
// pre-existing drift and must block the task with a "foreign_file_drift:"
// error.
func TestUntrackedFileSweptInBlocks(t *testing.T) {
	m, prd, task, dir, rec := driftCheckFixture(t, nil)
	driftWrite(t, dir, ".mcp.json", `{"mcpServers":{}}\n`)
	task.PreTaskUntrackedFiles = []string{".mcp.json"}
	// The worker commits the untracked file along with (only) it.
	driftGit(t, dir, "add", ".mcp.json")
	driftGit(t, dir, "commit", "-q", "-m", "task commit")

	if !m.checkForeignFileDrift(context.Background(), prd, task) {
		t.Fatal("default drift_mode must block a pre-existing untracked file swept into the task commit")
	}
	if task.Status != TaskBlocked {
		t.Fatalf("status = %s, want blocked", task.Status)
	}
	if !strings.HasPrefix(task.Error, "foreign_file_drift:") || !strings.Contains(task.Error, ".mcp.json") {
		t.Fatalf("error = %q, want prefix \"foreign_file_drift:\" containing .mcp.json", task.Error)
	}
	if !reflect.DeepEqual(task.ForeignFileDrift, []string{".mcp.json"}) {
		t.Fatalf("ForeignFileDrift = %v, want [.mcp.json]", task.ForeignFileDrift)
	}
	if len(driftDecisions(prd)) != 1 || len(rec.calls) != 1 {
		t.Fatalf("decisions=%d audit=%d, want 1/1", len(driftDecisions(prd)), len(rec.calls))
	}
	// The check never rewrites history: HEAD still holds the sweep commit.
	if got := driftGit(t, dir, "log", "-1", "--format=%s"); got != "task commit" {
		t.Fatalf("HEAD subject = %q — the check must not rewrite history", got)
	}
}

// TestPreexistingDefaultsToBlock (case 3): an explicitly empty DriftMode
// config must fail-safe to the "block" mode and still block on pre-existing
// drift.
func TestPreexistingDefaultsToBlock(t *testing.T) {
	m, prd, task, dir, rec := driftCheckFixture(t, []string{"own.txt"})
	cfg := DefaultConfig()
	cfg.DriftMode = ""
	m.SetConfig(cfg)
	driftWrite(t, dir, "own.txt", "task work\n")
	driftGit(t, dir, "add", "own.txt", "pre.txt") // the sweep
	driftGit(t, dir, "commit", "-q", "-m", "task commit")

	if !m.checkForeignFileDrift(context.Background(), prd, task) {
		t.Fatal("empty DriftMode must fail-safe to block")
	}
	if task.Status != TaskBlocked {
		t.Fatalf("status = %s, want blocked", task.Status)
	}
	if !strings.HasPrefix(task.Error, "foreign_file_drift:") || !strings.Contains(task.Error, "pre.txt") {
		t.Fatalf("error = %q", task.Error)
	}
	if !reflect.DeepEqual(task.ForeignFileDrift, []string{"pre.txt"}) {
		t.Fatalf("ForeignFileDrift = %v, want [pre.txt]", task.ForeignFileDrift)
	}
	// Both groups are still recorded: in-run (own.txt) + pre-existing (pre.txt).
	if len(driftDecisions(prd)) != 2 || len(rec.calls) != 2 {
		t.Fatalf("empty DriftMode still records everything: decisions=%d audit=%d", len(driftDecisions(prd)), len(rec.calls))
	}
}

// TestPreexistingDriftModeWarnOnlyWarns (case 4): the same pre-existing-drift
// setup as TestPreexistingDefaultsToBlock, but with DriftMode "warn" the
// finding is recorded yet the task is never blocked and carries no Error.
func TestPreexistingDriftModeWarnOnlyWarns(t *testing.T) {
	m, prd, task, dir, rec := driftCheckFixture(t, []string{"own.txt"})
	cfg := DefaultConfig()
	cfg.DriftMode = "warn"
	m.SetConfig(cfg)
	driftWrite(t, dir, "own.txt", "task work\n")
	driftGit(t, dir, "add", "own.txt", "pre.txt") // the sweep
	driftGit(t, dir, "commit", "-q", "-m", "task commit")

	if m.checkForeignFileDrift(context.Background(), prd, task) {
		t.Fatal(`DriftMode="warn" must never block, even on pre-existing drift`)
	}
	if task.Status == TaskBlocked {
		t.Fatal("warn mode must not set TaskBlocked")
	}
	if task.Error != "" {
		t.Fatalf("warn mode must not set Error, got %q", task.Error)
	}
	if !reflect.DeepEqual(task.ForeignFileDrift, []string{"pre.txt"}) {
		t.Fatalf("ForeignFileDrift = %v, want [pre.txt]", task.ForeignFileDrift)
	}
	if len(driftDecisions(prd)) != 2 || len(rec.calls) != 2 {
		t.Fatalf("warn mode still records everything: decisions=%d audit=%d", len(driftDecisions(prd)), len(rec.calls))
	}
}
