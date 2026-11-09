package autonomous

import (
	"context"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"testing"
)

// initReadOnlyRepo makes a git repo with one committed file, the tree a
// read-only task runs against.
func initReadOnlyRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "README.md")
	run("commit", "-q", "-m", "init")
	return dir
}

func writeRepoFile(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// finalGateTask returns the final_gate task a decomposed story gets from the
// built-in pack, after the templates are applied to it.
func finalGateTask(t *testing.T, story Story) Task {
	t.Helper()
	out, _ := ApplyTaskTemplates([]Story{story}, "task templates", "", testTaskTemplateCfg())
	for _, s := range out {
		for _, tk := range s.Tasks {
			if tk.Kind == "final_gate" {
				return tk
			}
		}
	}
	t.Fatal("no final_gate task applied")
	return Task{}
}

// A freshly decomposed final gate starts on a clean tree and passes when it
// writes only its declared report: no finding on the task, and the
// read-only finish check accepts the tree.
func TestFinalGateFreshlyDecomposedPassesReadOnlyCheck(t *testing.T) {
	dir := initReadOnlyRepo(t)
	fg := finalGateTask(t, testTaskTemplateStory(t))
	if !fg.ReadOnly || !taskHasUsableReportPath(fg) {
		t.Fatalf("final gate read_only=%v report=%q, want read-only with a usable report path", fg.ReadOnly, fg.ReportPath)
	}

	prd := &PRD{ID: "a1000001", ProjectDir: dir}
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	ctx := context.Background()
	baseline, ok := m.readOnlyTreeStatus(ctx, prd, &fg)
	if !ok {
		t.Fatal("tree status not inspectable before the gate starts")
	}
	strays := m.strayReportSnapshot(prd, &fg)

	// The gate's one permitted write: its declared report.
	writeRepoFile(t, dir, fg.ReportPath, "PASS\n")
	if err := m.readOnlyFinishCheck(ctx, prd, &fg, baseline, strays); err != nil {
		t.Fatalf("final gate that wrote only its report failed the finish check: %v", err)
	}
}

// A read-only task that leaves its report in docs/plans/reports/ has the file
// adopted into its report path's folder instead of failing.
func TestReadOnlyStrayReportAdoptedIntoReportFolder(t *testing.T) {
	dir := initReadOnlyRepo(t)
	fg := finalGateTask(t, testTaskTemplateStory(t))
	prd := &PRD{ID: "a1000001", ProjectDir: dir}
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	ctx := context.Background()
	baseline, ok := m.readOnlyTreeStatus(ctx, prd, &fg)
	if !ok {
		t.Fatal("tree status not inspectable")
	}
	strays := m.strayReportSnapshot(prd, &fg)

	writeRepoFile(t, dir, "docs/plans/reports/verdict.txt", "PASS\n")
	if err := m.readOnlyFinishCheck(ctx, prd, &fg, baseline, strays); err != nil {
		t.Fatalf("stray report under docs/plans/reports/ failed the task: %v", err)
	}
	wantDir := pathDirForTest(fg.ReportPath)
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(wantDir), "verdict.txt")); err != nil {
		t.Errorf("stray report not adopted into %s: %v", wantDir, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "docs/plans/reports/verdict.txt")); !os.IsNotExist(err) {
		t.Errorf("stray report still in docs/plans/reports/: err=%v", err)
	}
}

// A stray report is adopted from .datawatch/reports/ too, and a file that was
// already there before the task started is never moved.
func TestReadOnlyStrayReportPreexistingNotMoved(t *testing.T) {
	dir := initReadOnlyRepo(t)
	writeRepoFile(t, dir, "docs/plans/reports/old.txt", "from an earlier run\n")
	writeRepoFile(t, dir, ".datawatch/reports/other/new.txt", "")
	fg := finalGateTask(t, testTaskTemplateStory(t))
	prd := &PRD{ID: "a1000001", ProjectDir: dir}
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	ctx := context.Background()
	baseline, _ := m.readOnlyTreeStatus(ctx, prd, &fg)
	strays := m.strayReportSnapshot(prd, &fg)

	writeRepoFile(t, dir, ".datawatch/reports/other/fresh.txt", "PASS\n")
	if err := m.readOnlyFinishCheck(ctx, prd, &fg, baseline, strays); err != nil {
		t.Fatalf("finish check failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "docs/plans/reports/old.txt")); err != nil {
		t.Errorf("pre-existing file was moved: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(pathDirForTest(fg.ReportPath)), "fresh.txt")); err != nil {
		t.Errorf("fresh file not adopted into the report folder: %v", err)
	}
}

// A genuine write outside the report folders still fails the read-only task:
// adoption covers only the stray report folders.
func TestReadOnlyTreeEditOutsideReportsStillFails(t *testing.T) {
	dir := initReadOnlyRepo(t)
	fg := finalGateTask(t, testTaskTemplateStory(t))
	prd := &PRD{ID: "a1000001", ProjectDir: dir}
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	ctx := context.Background()
	baseline, _ := m.readOnlyTreeStatus(ctx, prd, &fg)
	strays := m.strayReportSnapshot(prd, &fg)

	writeRepoFile(t, dir, "README.md", "edited\n")
	if err := m.readOnlyFinishCheck(ctx, prd, &fg, baseline, strays); err == nil {
		t.Error("edit to a tracked file passed the read-only finish check; want failure")
	}
}

// Without a usable report path nothing is adopted: the stray file stays where
// the task left it and the tree stays dirty.
func TestRelocateStrayReportFilesNoReportPathMovesNothing(t *testing.T) {
	root := t.TempDir()
	writeRepoFile(t, root, "docs/plans/reports/x.txt", "x")
	moved, err := relocateStrayReportFiles(root, "", map[string]bool{})
	if err != nil || len(moved) != 0 {
		t.Fatalf("moved=%v err=%v, want nothing moved", moved, err)
	}
	if _, err := os.Stat(filepath.Join(root, "docs/plans/reports/x.txt")); err != nil {
		t.Errorf("stray file moved without a report path: %v", err)
	}
}

// A destination that already exists is never overwritten.
func TestRelocateStrayReportFilesNeverOverwrites(t *testing.T) {
	root := t.TempDir()
	writeRepoFile(t, root, "docs/plans/reports/v.txt", "new")
	writeRepoFile(t, root, ".datawatch/reports/d/v.txt", "keep")
	moved, err := relocateStrayReportFiles(root, ".datawatch/reports/d/final.txt", map[string]bool{})
	if err != nil {
		t.Fatalf("relocate: %v", err)
	}
	if len(moved) != 0 {
		t.Errorf("moved = %v, want none over an existing destination", moved)
	}
	body, _ := os.ReadFile(filepath.Join(root, ".datawatch/reports/d/v.txt"))
	if string(body) != "keep" {
		t.Errorf("existing destination overwritten: %q", body)
	}
}

// pathDirForTest is the folder of a slash-separated repo path.
func pathDirForTest(p string) string { return path.Dir(p) }
