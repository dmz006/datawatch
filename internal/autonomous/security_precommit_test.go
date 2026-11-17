package autonomous

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newSecurityPrecommitManager — a manager over a private temp dir, same
// shape as the sibling Record* hook tests.
func newSecurityPrecommitManager(t *testing.T) *Manager {
	t.Helper()
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return m
}

// singleTaskPRD — one PRD with one story carrying one existing task,
// returned as (prdID, taskID).
func singleTaskPRD(t *testing.T, m *Manager) (prdID, taskID string) {
	t.Helper()
	prd, err := m.CreatePRD("spec", "/p", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if err := m.Store().SetStories(prd.ID, []Story{
		{Title: "S", Status: StoryPending, Tasks: []Task{{Title: "T1", Status: TaskCompleted}}},
	}); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	prd, ok := m.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatalf("GetPRD %s", prd.ID)
	}
	return prd.ID, prd.Story[0].Tasks[0].ID
}

// taskByID — re-read the task under test from the store.
func taskByID(t *testing.T, m *Manager, prdID, taskID string) Task {
	t.Helper()
	prd, ok := m.Store().GetPRD(prdID)
	if !ok {
		t.Fatalf("PRD %s not found", prdID)
	}
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			if prd.Story[si].Tasks[ti].ID == taskID {
				return prd.Story[si].Tasks[ti]
			}
		}
	}
	t.Fatalf("task %s not found in prd %s", taskID, prdID)
	return Task{}
}

// The scan helper itself: warn-only by shape. Disabled (or no directory
// to walk) means simply "no findings"; a failed walk is swallowed, never
// an error, never a panic.
func TestRunPreCommitSecurityScan_WarnOnlyShape(t *testing.T) {
	dir := t.TempDir()
	writeFindingFile(t, dir)

	if got := RunPreCommitSecurityScan(false, dir, nil); got != nil {
		t.Fatalf("disabled scan must return nil, got %v", got)
	}
	if got := RunPreCommitSecurityScan(true, "", nil); got != nil {
		t.Fatalf("empty projectDir must return nil, got %v", got)
	}
	if got := RunPreCommitSecurityScan(true, filepath.Join(dir, "does-not-exist"), nil); got != nil {
		t.Fatalf("a failed walk must be swallowed (nil), got %v", got)
	}
}

// Enabled scan finds the known-bad constructs in the worktree with the
// scanner's default (.py/.pyw) extensions, and recording them lands the
// findings and the count on the task — one decision-log entry — with the
// task's status untouched.
func TestRunPreCommitSecurityScanOn_FindsAndRecordTaskSecurityFindings(t *testing.T) {
	m := newSecurityPrecommitManager(t)
	prdID, taskID := singleTaskPRD(t, m)
	dir := t.TempDir()
	writeFindingFile(t, dir)

	findings := RunPreCommitSecurityScan(true, dir, nil)
	if len(findings) < 3 {
		t.Fatalf("expected >=3 findings (os.system, eval, exec), got %d: %v", len(findings), findings)
	}

	if err := m.RecordTaskSecurityFindings(prdID, taskID, findings); err != nil {
		t.Fatalf("RecordTaskSecurityFindings: %v", err)
	}
	task := taskByID(t, m, prdID, taskID)
	if len(task.SecurityFindings) != len(findings) {
		t.Fatalf("SecurityFindings = %d entries, want %d", len(task.SecurityFindings), len(findings))
	}
	if task.SecurityFindingCount != len(findings) {
		t.Fatalf("SecurityFindingCount = %d, want %d", task.SecurityFindingCount, len(findings))
	}
	if task.Status != TaskCompleted {
		t.Fatalf("recording findings must not change the task status, now %q", task.Status)
	}

	prd, ok := m.Store().GetPRD(prdID)
	if !ok {
		t.Fatalf("PRD %s not found", prdID)
	}
	seen := 0
	for _, d := range prd.Decisions {
		if d.Kind == "security_scan_findings" {
			seen++
			if !strings.Contains(d.Note, taskID) {
				t.Fatalf("decision note must name the task id, got %q", d.Note)
			}
			if !strings.Contains(d.Note, "warn-only") {
				t.Fatalf("decision note must say warn-only, got %q", d.Note)
			}
			if !strings.Contains(d.Note, "findings=") {
				t.Fatalf("decision note must carry the full count, got %q", d.Note)
			}
		}
	}
	if seen != 1 {
		t.Fatalf("expected exactly 1 security_scan_findings decision entry, got %d", seen)
	}
}

// A second pass over the same task must accumulate: the stored findings
// are replaced (last pass wins) but the count is cumulative.
func TestRecordTaskSecurityFindings_AccumulatesCountAcrossPasses(t *testing.T) {
	m := newSecurityPrecommitManager(t)
	prdID, taskID := singleTaskPRD(t, m)
	dir := t.TempDir()
	writeFindingFile(t, dir)

	first := RunPreCommitSecurityScan(true, dir, nil)
	if err := m.RecordTaskSecurityFindings(prdID, taskID, first); err != nil {
		t.Fatal(err)
	}
	second := RunPreCommitSecurityScan(true, dir, nil)
	if err := m.RecordTaskSecurityFindings(prdID, taskID, second); err != nil {
		t.Fatal(err)
	}
	task := taskByID(t, m, prdID, taskID)
	if len(task.SecurityFindings) != len(second) {
		t.Fatalf("stored findings must be the last pass (%d), got %d", len(second), len(task.SecurityFindings))
	}
	if task.SecurityFindingCount != len(first)+len(second) {
		t.Fatalf("SecurityFindingCount = %d, want cumulative %d", task.SecurityFindingCount, len(first)+len(second))
	}
}

// The full volume is preserved in the count even when only the first 20
// findings fit on the task record.
func TestRecordTaskSecurityFindings_CapsTextButKeepsFullCount(t *testing.T) {
	m := newSecurityPrecommitManager(t)
	prdID, taskID := singleTaskPRD(t, m)

	findings := make([]string, 25)
	for i := range findings {
		findings[i] = "helper.py:1: finding " + string(rune('a'+i%26))
	}
	if err := m.RecordTaskSecurityFindings(prdID, taskID, findings); err != nil {
		t.Fatalf("RecordTaskSecurityFindings: %v", err)
	}
	task := taskByID(t, m, prdID, taskID)
	if len(task.SecurityFindings) != 20 {
		t.Fatalf("SecurityFindings capped at 20, got %d", len(task.SecurityFindings))
	}
	if task.SecurityFindingCount != 25 {
		t.Fatalf("SecurityFindingCount = %d, want full 25", task.SecurityFindingCount)
	}
}

// Configured extensions restrict the walk: a .js-only scan must ignore
// the .py finding but still find the .js one, and vice versa.
func TestRunPreCommitSecurityScan_RespectsConfiguredExtensions(t *testing.T) {
	dir := t.TempDir()
	writeFindingFile(t, dir) // helper.py
	writeJSFinding(t, dir)   // helper.js

	jsOnly := RunPreCommitSecurityScan(true, dir, []string{".js"})
	for _, f := range jsOnly {
		if strings.Contains(f, "helper.py") {
			t.Fatalf(".js-only scan must ignore the .py file, got %v", jsOnly)
		}
	}
	foundJS := false
	for _, f := range jsOnly {
		if strings.Contains(f, "helper.js") {
			foundJS = true
		}
	}
	if !foundJS {
		t.Fatalf(".js-only scan should find helper.js, got %v", jsOnly)
	}

	pyOnly := RunPreCommitSecurityScan(true, dir, []string{".py"})
	foundPy, leak := false, false
	for _, f := range pyOnly {
		if strings.Contains(f, "helper.py") {
			foundPy = true
		}
		if strings.Contains(f, "helper.js") {
			leak = true
		}
	}
	if !foundPy || leak {
		t.Fatalf(".py-only scan: found helper.py=%v, leaked helper.js=%v, findings=%v", foundPy, leak, pyOnly)
	}
}

// An unknown task is an error (the hook logs it and the commit still
// proceeds) and must not touch the PRD record.
func TestRecordTaskSecurityFindings_UnknownTaskErrors(t *testing.T) {
	m := newSecurityPrecommitManager(t)
	prdID, _ := singleTaskPRD(t, m)

	if err := m.RecordTaskSecurityFindings(prdID, "no-such-task", []string{"a:1: x"}); err == nil {
		t.Fatal("expected an error for an unknown task")
	}
	if err := m.RecordTaskSecurityFindings("prd-missing", "t", []string{"a:1: x"}); err == nil {
		t.Fatal("expected an error for a missing PRD")
	}
}

// writeFindingFile — a helper.py (and js sibling) carrying the scanner's
// known-bad constructs.
func writeFindingFile(t *testing.T, dir string) {
	t.Helper()
	src := "import os\n" +
		"x = eval(\"1+1\")\n" +
		"exec(\"pass\")\n" +
		"os.system(\"ls\")\n"
	if err := os.WriteFile(filepath.Join(dir, "helper.py"), []byte(src), 0o644); err != nil {
		t.Fatalf("write helper.py: %v", err)
	}
}

func writeJSFinding(t *testing.T, dir string) {
	t.Helper()
	src := "const x = eval(\"1+1\");\nconsole.log(x);\n"
	if err := os.WriteFile(filepath.Join(dir, "helper.js"), []byte(src), 0o644); err != nil {
		t.Fatalf("write helper.js: %v", err)
	}
}
