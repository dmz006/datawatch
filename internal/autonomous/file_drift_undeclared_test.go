package autonomous

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestCommittedLineStatsSince(t *testing.T) {
	dir := driftTestRepo(t)
	driftWrite(t, dir, "base.txt", "1\n2\n3\n")
	driftWrite(t, dir, "rename-me.txt", "a\nb\n")
	driftWrite(t, dir, "binary.bin", "\x00\x01\x02")
	driftGit(t, dir, "add", "base.txt", "rename-me.txt", "binary.bin")
	driftGit(t, dir, "commit", "-q", "-m", "base")
	pre := driftGit(t, dir, "rev-parse", "HEAD")

	// Edit base.txt: add 3 lines, remove 1 (4 total).
	driftWrite(t, dir, "base.txt", "1\n2\nX\nY\nZ\n")
	// Rename reports under the new path: 1 added line (git tracks it as a
	// rename, stats cover only the diff).
	driftGit(t, dir, "mv", "rename-me.txt", "renamed.txt")
	driftWrite(t, dir, "renamed.txt", "a\nb\nc\n")
	// Binary: numstat reports "-\t-" and still counts as a touched file.
	driftWrite(t, dir, "binary.bin", "\x00\x03\x04")
	driftGit(t, dir, "add", "-A")
	driftGit(t, dir, "commit", "-q", "-m", "second")

	got, err := CommittedLineStatsSince(context.Background(), dir, pre, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"base.txt": 4, "binary.bin": 0, "renamed.txt": 1}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if got, err := CommittedLineStatsSince(context.Background(), dir, "", nil); got != nil || err != nil {
		t.Fatalf("empty preSHA: got %v, %v; want nil, nil", got, err)
	}
	if got, _ := CommittedLineStatsSince(context.Background(), "", pre, nil); got != nil {
		t.Fatalf("empty dir: got %v, want nil", got)
	}
}

// undeclaredAlertRecorder captures operator alerts.
type undeclaredAlertRecorder struct {
	alerts []string
}

func (r *undeclaredAlertRecorder) fn(level, title, body string) string {
	r.alerts = append(r.alerts, level+"|"+title+"|"+body)
	return ""
}

func TestCheckUndeclaredEdits_OffIsFullNoOp(t *testing.T) {
	cfg := DefaultConfig()
	cfg.UndeclaredEditMode = "off"
	m, _ := NewManager(t.TempDir(), cfg, nil)
	m, prd, task, _, auditRec := reattachDriftFixture(t, m)
	m.driftGit = func(context.Context, string, ...string) (string, error) {
		t.Fatal("off mode must make zero git calls")
		return "", nil
	}
	if m.checkUndeclaredEdits(context.Background(), prd, task) {
		t.Fatal("off mode must never block")
	}
	if task.Status == TaskBlocked || len(task.UndeclaredEdits) != 0 || len(auditRec.calls) != 0 {
		t.Fatalf("off mode recorded something: status=%s edits=%v audit=%v", task.Status, task.UndeclaredEdits, auditRec.calls)
	}
	for _, d := range prd.Decisions {
		if d.Kind == "undeclared_edit_detected" {
			t.Fatal("off mode must record no decisions")
		}
	}
}

func TestCheckUndeclaredEdits_WarnAccumulates(t *testing.T) {
	m, prd, task, dir, auditRec := driftCheckFixture(t, []string{"own.txt"})
	// The "worker" commits its planned own.txt plus an unplanned file it
	// never declared.
	driftWrite(t, dir, "own.txt", "planned work\n")
	driftWrite(t, dir, "undeclared.txt", "extra\nline\n")
	driftGit(t, dir, "add", "own.txt", "undeclared.txt")
	driftGit(t, dir, "commit", "-q", "-m", "task work")

	alerts := &undeclaredAlertRecorder{}
	m.SetAlertSink(alerts.fn)

	if m.checkUndeclaredEdits(context.Background(), prd, task) {
		t.Fatal("warn mode must never block")
	}
	if task.Status == TaskBlocked {
		t.Fatal("warn mode must not block the task")
	}
	want := []UndeclaredEdit{{Path: "undeclared.txt", LinesChanged: 2}}
	if !reflect.DeepEqual(task.UndeclaredEdits, want) {
		t.Fatalf("UndeclaredEdits = %v, want %v", task.UndeclaredEdits, want)
	}
	foundDecision := false
	for _, d := range prd.Decisions {
		if d.Kind == "undeclared_edit_detected" {
			foundDecision = true
			if !strings.Contains(d.Note, "undeclared.txt") {
				t.Fatalf("decision note missing file: %q", d.Note)
			}
		}
	}
	if !foundDecision {
		t.Fatal("no undeclared_edit_detected decision recorded")
	}
	if len(auditRec.acts) == 0 || auditRec.acts[len(auditRec.acts)-1] != "autonomous_undeclared_edit_detected" {
		t.Fatalf("audit actions: %v, want trailing autonomous_undeclared_edit_detected", auditRec.acts)
	}
	if len(alerts.alerts) != 1 || !strings.HasPrefix(alerts.alerts[0], "warn|") {
		t.Fatalf("alerts = %v, want exactly one warn-level alert", alerts.alerts)
	}
	// The check must never modify the repository: only the fixture's own
	// pre-dirty modification may show, nothing new.
	if status := strings.TrimSpace(driftGit(t, dir, "status", "--porcelain")); status != "M pre.txt" {
		t.Fatalf("unexpected repo status after check: %q", status)
	}
}

func TestCheckUndeclaredEdits_Block(t *testing.T) {
	cfg := DefaultConfig()
	cfg.UndeclaredEditMode = "block"
	m, _ := NewManager(t.TempDir(), cfg, nil)
	m, prd, task, dir, _ := reattachDriftFixture(t, m)
	driftWrite(t, dir, "undeclared.txt", "x\n")
	driftGit(t, dir, "add", "undeclared.txt")
	driftGit(t, dir, "commit", "-q", "-m", "task work")

	if !m.checkUndeclaredEdits(context.Background(), prd, task) {
		t.Fatal("block mode must return true")
	}
	if task.Status != TaskBlocked {
		t.Fatalf("status = %s, want %s", task.Status, TaskBlocked)
	}
	if want := "undeclared_edit: undeclared.txt"; task.Error != want {
		t.Fatalf("error = %q, want %q", task.Error, want)
	}
}

func TestCheckUndeclaredEdits_ExcludesPlannedAndPreDirty(t *testing.T) {
	m, prd, task, dir, _ := driftCheckFixture(t, []string{"planned-new.txt"})
	// planned-new.txt: new file, listed in the task's plan → excluded.
	// pre.txt: already modified before the task (fixture setup) →
	// foreign-drift's territory, not undeclared.
	driftWrite(t, dir, "planned-new.txt", "planned new file\n")
	driftWrite(t, dir, "pre.txt", "operator edit (pre-dirty)\n")
	driftGit(t, dir, "add", "planned-new.txt", "pre.txt")
	driftGit(t, dir, "commit", "-q", "-m", "task work")

	if m.checkUndeclaredEdits(context.Background(), prd, task) {
		t.Fatal("must not block")
	}
	if len(task.UndeclaredEdits) != 0 {
		t.Fatalf("UndeclaredEdits = %v, want none (planned + pre-dirty excluded)", task.UndeclaredEdits)
	}
	for _, d := range prd.Decisions {
		if d.Kind == "undeclared_edit_detected" {
			t.Fatal("no undeclared_edit_detected decision expected")
		}
	}
}

func TestCheckUndeclaredEdits_AccumulatesAcrossRetries(t *testing.T) {
	m, prd, task, dir, _ := driftCheckFixture(t, nil)
	driftWrite(t, dir, "undeclared.txt", "line1\nline2\nline3\n")
	driftGit(t, dir, "add", "undeclared.txt")
	driftGit(t, dir, "commit", "-q", "-m", "attempt 1")
	m.checkUndeclaredEdits(context.Background(), prd, task)

	// Retry: new pre-task SHA at HEAD, then one more line in the same file
	// (1 added + 1 removed in place of the same line would also be fine —
	// here it's a pure add of 1 line over the 3-line base, so numstat
	// reports 1 added).
	task.PreTaskSHA = driftGit(t, dir, "rev-parse", "HEAD")
	driftWrite(t, dir, "undeclared.txt", "line1\nline2\nline3\nline4\n")
	driftGit(t, dir, "add", "undeclared.txt")
	driftGit(t, dir, "commit", "-q", "-m", "attempt 2")
	m.checkUndeclaredEdits(context.Background(), prd, task)

	// Attempt 1: 3 added lines; attempt 2: 1 added line → 4 total, one
	// entry for the file.
	want := []UndeclaredEdit{{Path: "undeclared.txt", LinesChanged: 4}}
	if !reflect.DeepEqual(task.UndeclaredEdits, want) {
		t.Fatalf("UndeclaredEdits = %v, want %v (lines summed, one entry)", task.UndeclaredEdits, want)
	}
}

func TestCheckUndeclaredEdits_MissingPreSHAIsNoOp(t *testing.T) {
	m, prd, task, _, auditRec := driftCheckFixture(t, nil)
	m.driftGit = func(context.Context, string, ...string) (string, error) {
		t.Fatal("must make zero git calls without a PreTaskSHA")
		return "", nil
	}
	task.PreTaskSHA = ""
	if m.checkUndeclaredEdits(context.Background(), prd, task) {
		t.Fatal("must not block")
	}
	if len(task.UndeclaredEdits) != 0 || len(auditRec.calls) != 0 {
		t.Fatalf("recorded something: edits=%v audit=%v", task.UndeclaredEdits, auditRec.calls)
	}
}

// reattachDriftFixture builds the same fixture as driftCheckFixture on a
// manager that the caller already created (so the caller controls its
// Config), returning the PRD, task, repo dir and a fresh audit recorder.
func reattachDriftFixture(t *testing.T, m *Manager) (*Manager, *PRD, *Task, string, *driftAuditRecorder) {
	t.Helper()
	dir := driftTestRepo(t)
	driftWrite(t, dir, "base.txt", "1\n")
	driftGit(t, dir, "add", "base.txt")
	driftGit(t, dir, "commit", "-q", "-m", "base")
	prd, err := m.CreatePRD("drift", dir, "claude-code", "", EffortNormal)
	if err != nil {
		t.Fatal(err)
	}
	task := &Task{ID: "task-1", PRDID: prd.ID}
	task.PreTaskSHA = driftGit(t, dir, "rev-parse", "HEAD")
	rec := &driftAuditRecorder{}
	m.SetAuditFn(rec.fn)
	return m, prd, task, dir, rec
}
