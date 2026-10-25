// Executor guard: a worker is never started in a project directory that
// is not a real worktree of the configured base repository.
package autonomous

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
)

type guardAlert struct{ level, title, body string }

// guardFixture is a worktree-mode Automaton with one story and one task,
// approved and ready to run, against a manager with a configured base
// repository. Nothing is created at the Automaton's worktree path.
type guardFixture struct {
	m        *Manager
	prd      *PRD
	path     string
	spawnMu  sync.Mutex
	spawned  []SpawnRequest
	alertsMu sync.Mutex
	alerts   []guardAlert
}

func newGuardFixture(t *testing.T) *guardFixture {
	t.Helper()
	cfg := DefaultConfig()
	cfg.WorktreeBaseRepo = scratchGitRepo(t)
	cfg.WorktreeDir = t.TempDir()
	m, err := NewManager(t.TempDir(), cfg, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("spec", "", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "t", Spec: "do it"}}}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDApproved
	_ = m.Store().SavePRD(prd)
	f := &guardFixture{m: m, prd: prd, path: filepath.Join(cfg.WorktreeDir, prd.ID)}
	m.SetAlertSink(func(level, title, body string) {
		f.alertsMu.Lock()
		defer f.alertsMu.Unlock()
		f.alerts = append(f.alerts, guardAlert{level, title, body})
	})
	return f
}

// seedStandaloneRepo creates the incident shape at the worktree path: a
// fresh standalone repository holding only CLAUDE.md, an mcp json file
// and a checkpoint file, and points the Automaton at it in worktree mode.
func (f *guardFixture) seedStandaloneRepo(t *testing.T) {
	t.Helper()
	if err := os.MkdirAll(f.path, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "init", "-b", "main")
	cmd.Dir = f.path
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	for name, body := range map[string]string{
		"CLAUDE.md":     "instructions\n",
		".mcp.json":     "{}\n",
		"CHECKPOINT.md": "checkpoint\n",
	} {
		if err := os.WriteFile(filepath.Join(f.path, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	prd, _ := f.m.Store().GetPRD(f.prd.ID)
	prd.Git.Branch = branchNameFor(prd)
	prd.ProjectDir = f.path
	if err := f.m.Store().SavePRD(prd); err != nil {
		t.Fatal(err)
	}
}

func (f *guardFixture) run(t *testing.T) {
	t.Helper()
	spawn := func(_ context.Context, req SpawnRequest) (SpawnResult, error) {
		f.spawnMu.Lock()
		defer f.spawnMu.Unlock()
		f.spawned = append(f.spawned, req)
		return SpawnResult{SessionID: "s1"}, nil
	}
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		return VerificationResult{OK: true, Summary: "ok"}, nil
	}
	_ = f.m.Run(context.Background(), f.prd.ID, spawn, verify)
}

func (f *guardFixture) onlyTask(t *testing.T) *Task {
	t.Helper()
	got, _ := f.m.Store().GetPRD(f.prd.ID)
	if len(got.Story) != 1 || len(got.Story[0].Tasks) != 1 {
		t.Fatalf("want one story with one task, got %+v", got.Story)
	}
	return &got.Story[0].Tasks[0]
}

// snapshotDir lists every entry outside .git with the content of regular
// files, so a before/after comparison catches any change to the directory.
func snapshotDir(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		if rel == "." {
			return nil
		}
		if rel == ".git" {
			return filepath.SkipDir
		}
		entry := rel
		if info.Mode().IsRegular() {
			b, rerr := os.ReadFile(p)
			if rerr != nil {
				return rerr
			}
			entry += "=" + string(b)
		}
		out = append(out, entry)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(out)
	return out
}

func TestExecuteOne_RefusesLaunchWhenProjectDirNotWorktree(t *testing.T) {
	f := newGuardFixture(t)
	f.seedStandaloneRepo(t)
	before := snapshotDir(t, f.path)

	f.run(t)

	if len(f.spawned) != 0 {
		t.Fatalf("spawn was called %d time(s); a worker must not start outside a worktree", len(f.spawned))
	}
	task := f.onlyTask(t)
	if task.Status != TaskFailed {
		t.Errorf("task status = %q, want %q", task.Status, TaskFailed)
	}
	if !strings.HasPrefix(task.Error, "worker launch refused:") {
		t.Errorf("task error = %q, want prefix %q", task.Error, "worker launch refused:")
	}
	if task.RetryCount != 0 {
		t.Errorf("RetryCount = %d, want 0: the refusal must not consume the retry budget", task.RetryCount)
	}
	if task.EscalationFailures != 0 || len(task.Attempts) != 0 {
		t.Errorf("refusal counted as a failed attempt (failures=%d attempts=%d)", task.EscalationFailures, len(task.Attempts))
	}
	if task.Status == TaskReviewNeeded {
		t.Error("task was held for review; an infrastructure fault must not feed the escalation hold")
	}
	// The incident files are untouched and the refused launch left nothing
	// else behind beyond the bookkeeping written before the guard runs.
	bookkeeping := func(entry string) bool {
		return strings.HasPrefix(entry, ".gitignore=") || entry == ".datawatch"
	}
	var after []string
	for _, e := range snapshotDir(t, f.path) {
		if !bookkeeping(e) {
			after = append(after, e)
		}
	}
	if strings.Join(before, "\n") != strings.Join(after, "\n") {
		t.Errorf("directory contents changed:\nbefore: %q\nafter:  %q", before, after)
	}
}

func TestExecuteOne_RefusalAlertNamesAutomaton(t *testing.T) {
	f := newGuardFixture(t)
	f.seedStandaloneRepo(t)

	f.run(t)

	if len(f.alerts) != 1 {
		t.Fatalf("got %d alerts, want exactly 1: %+v", len(f.alerts), f.alerts)
	}
	a := f.alerts[0]
	if a.level != "error" {
		t.Errorf("alert level = %q, want error", a.level)
	}
	if a.title != "Automaton project directory is not a worktree" {
		t.Errorf("alert title = %q", a.title)
	}
	if !strings.Contains(a.body, "Automaton: "+f.prd.ID+" ("+f.prd.Title+")") {
		t.Errorf("alert body does not name the Automaton %q (%q):\n%s", f.prd.ID, f.prd.Title, a.body)
	}
	task := f.onlyTask(t)
	if !strings.Contains(a.body, "Task: "+task.ID+" ("+task.Title+")") {
		t.Errorf("alert body does not name the task:\n%s", a.body)
	}
	if !strings.Contains(a.body, f.path) {
		t.Errorf("alert body does not name the project directory %q:\n%s", f.path, a.body)
	}
}

func TestExecuteOne_LaunchesInValidWorktree(t *testing.T) {
	f := newGuardFixture(t)

	f.run(t)

	if len(f.spawned) != 1 {
		t.Fatalf("spawn called %d time(s), want 1", len(f.spawned))
	}
	got, _ := f.m.Store().GetPRD(f.prd.ID)
	if f.spawned[0].ProjectDir != got.ProjectDir || got.ProjectDir != f.path {
		t.Errorf("spawn ProjectDir = %q, PRD ProjectDir = %q, want the worktree %q", f.spawned[0].ProjectDir, got.ProjectDir, f.path)
	}
	if task := f.onlyTask(t); task.Status != TaskCompleted {
		t.Errorf("task status = %q (error %q), want %q", task.Status, task.Error, TaskCompleted)
	}
	if len(f.alerts) != 0 {
		t.Errorf("unexpected alerts: %+v", f.alerts)
	}
}
