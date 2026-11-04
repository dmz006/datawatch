// Report-path validation for read-only tasks (see
// read_only_parallel.go): a read-only task may write exactly one
// repo-relative path under .datawatch/reports/, which the dirty-tree
// check then ignores.
package autonomous

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestValidateReportPathAccepts(t *testing.T) {
	valid := []string{
		"", // empty means the task writes nothing
		".datawatch/reports/a.md",
		".datawatch/reports/prd-1/summary.md",
	}
	for _, p := range valid {
		if err := validateReportPath(p); err != nil {
			t.Errorf("validateReportPath(%q) = %v; want ok", p, err)
		}
	}
}

func TestValidateReportPathRejects(t *testing.T) {
	invalid := []struct {
		path string
		want string
	}{
		{"/abs/reports/x.md", "absolute"},
		{".datawatch/reports/../x.md", ".."},
		{"a/b/../../.datawatch/reports/x", ".."},
		{"notes.md", "under .datawatch/reports/"},
		{".datawatch/verify/x.sh", "under .datawatch/reports/"},
		{".datawatch", "under .datawatch/reports/"},
		{".datawatch/reports", "under .datawatch/reports/"},
		{".datawatch//reports/x.md", "clean"},
		{".datawatch/reports/", "clean"},
		{".datawatch/reports/./x.md", "clean"},
	}
	for _, tc := range invalid {
		err := validateReportPath(tc.path)
		if err == nil {
			t.Errorf("validateReportPath(%q) = nil; want error", tc.path)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("validateReportPath(%q) error %q; want mention of %q", tc.path, err, tc.want)
		}
	}
}

func TestReportPathOf(t *testing.T) {
	const rp = ".datawatch/reports/a.md"
	if got := reportPathOf(&Task{ReadOnly: true, ReportPath: rp}); got != rp {
		t.Errorf("read-only task: reportPathOf = %q; want %q", got, rp)
	}
	if got := reportPathOf(&Task{ReadOnly: false, ReportPath: rp}); got != "" {
		t.Errorf("non-read-only task: reportPathOf = %q; want empty (ignored)", got)
	}
	if got := reportPathOf(&Task{ReadOnly: true}); got != "" {
		t.Errorf("read-only task with no path: reportPathOf = %q; want empty", got)
	}
	if got := reportPathOf(nil); got != "" {
		t.Errorf("nil task: reportPathOf = %q; want empty", got)
	}
}

// makeReportPathPRD creates an approved PRD with one task carrying the
// given read-only flag and report path in projectDir.
func makeReportPathPRD(t *testing.T, mgr *Manager, dir string, readOnly bool, reportPath string) *PRD {
	t.Helper()
	st := mgr.Store()
	prd, err := st.CreatePRD("report path test", dir, "claude-code", "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if err := st.SetStories(prd.ID, []Story{{Title: "S1", Tasks: []Task{
		{Title: "T1", Spec: "do it", ReadOnly: readOnly, ReportPath: reportPath},
	}}}); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	prd, ok := st.GetPRD(prd.ID)
	if !ok {
		t.Fatal("GetPRD")
	}
	prd.Status = PRDApproved
	if err := st.SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	return prd
}

// TestReadOnlyTreeStatusIgnoresReportPath checks the direct status
// helper: a file at the task's report path does not register as dirty,
// while a stray file elsewhere in the tree still does.
func TestReadOnlyTreeStatusIgnoresReportPath(t *testing.T) {
	repo := gitRepoWithDWSeed(t)
	mgr := newRoManager(t, nil)
	prd := makeReportPathPRD(t, mgr, repo, true, ".datawatch/reports/summary.md")
	id := prd.Story[0].Tasks[0].ID
	tk, ok := mgr.Store().GetTask(id)
	if !ok || tk == nil {
		t.Fatal("GetTask")
	}

	ctx := context.Background()
	before, ok := mgr.readOnlyTreeStatus(ctx, prd, tk)
	if !ok {
		t.Fatal("readOnlyTreeStatus did not run")
	}
	if before != "" {
		t.Fatalf("seeded repo should be clean; status = %q", before)
	}

	// A write at the report path only: not dirty.
	rp := filepath.Join(repo, ".datawatch", "reports", "summary.md")
	if err := os.MkdirAll(filepath.Dir(rp), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(rp, []byte("report"), 0o644); err != nil {
		t.Fatalf("write report: %v", err)
	}
	now, ok := mgr.readOnlyTreeStatus(ctx, prd, tk)
	if !ok {
		t.Fatal("readOnlyTreeStatus did not run")
	}
	if now != "" {
		t.Errorf("a write at the report path should not register dirty; status = %q", now)
	}

	// A stray file elsewhere: still dirty.
	if err := os.WriteFile(filepath.Join(repo, "stray.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write stray: %v", err)
	}
	now, ok = mgr.readOnlyTreeStatus(ctx, prd, tk)
	if !ok {
		t.Fatal("readOnlyTreeStatus did not run")
	}
	if !strings.Contains(now, "stray.txt") {
		t.Errorf("stray file should register dirty; status = %q", now)
	}
}

// TestReadOnlyTreeStatusInvalidReportPathNotExcluded checks the guard in
// readOnlyTreeStatus: a ReportPath that fails validateReportPath is never
// passed to git as a pathspec, so a write there still registers as dirty.
func TestReadOnlyTreeStatusInvalidReportPathNotExcluded(t *testing.T) {
	repo := gitRepoWithDWSeed(t)
	mgr := newRoManager(t, nil)
	// "notes.md" fails validation (not under .datawatch/reports/): a write
	// there must still register as dirty.
	prd := makeReportPathPRD(t, mgr, repo, true, "notes.md")
	id := prd.Story[0].Tasks[0].ID
	tk, ok := mgr.Store().GetTask(id)
	if !ok || tk == nil {
		t.Fatal("GetTask")
	}

	if err := os.WriteFile(filepath.Join(repo, "notes.md"), []byte("r"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	now, ok := mgr.readOnlyTreeStatus(context.Background(), prd, tk)
	if !ok {
		t.Fatal("readOnlyTreeStatus did not run")
	}
	if !strings.Contains(now, "notes.md") {
		t.Errorf("invalid report path must not be excluded; status = %q", now)
	}
}

// TestReadOnlyReportPathWriteCompletes runs the full executor path: a
// read-only task whose only write is at its report path finishes
// successfully.
func TestReadOnlyReportPathWriteCompletes(t *testing.T) {
	fastTreeLockPolling(t)
	repo := gitRepoWithDWSeed(t)
	mgr := newRoManager(t, nil)
	rp := ".datawatch/reports/summary.md"
	prd := makeReportPathPRD(t, mgr, repo, true, rp)

	spawn := func(_ context.Context, req SpawnRequest) (SpawnResult, error) {
		return SpawnResult{SessionID: "sess-" + req.TaskID}, nil
	}
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		p := filepath.Join(repo, rp)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Errorf("mkdir: %v", err)
		}
		if err := os.WriteFile(p, []byte("report"), 0o644); err != nil {
			t.Errorf("write report: %v", err)
		}
		return VerificationResult{OK: true, Summary: "ok"}, nil
	}

	runCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ran := make(chan error, 1)
	go func() { ran <- mgr.Run(runCtx, prd.ID, spawn, verify) }()
	finishRun(t, ran)

	id := prd.Story[0].Tasks[0].ID
	cur, _ := mgr.Store().GetTask(id)
	if cur.Status != TaskCompleted {
		t.Fatalf("status = %q; want completed (a write at the report path is allowed)", cur.Status)
	}
}

// TestReadOnlyStrayFileStillFails runs the full executor path with a
// report path set: a write somewhere else in the tree is still a
// read-only violation.
func TestReadOnlyStrayFileStillFails(t *testing.T) {
	fastTreeLockPolling(t)
	repo := gitRepoWithDWSeed(t)
	mgr := newRoManager(t, nil)
	prd := makeReportPathPRD(t, mgr, repo, true, ".datawatch/reports/summary.md")

	spawn := func(_ context.Context, req SpawnRequest) (SpawnResult, error) {
		return SpawnResult{SessionID: "sess-" + req.TaskID}, nil
	}
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		if err := os.WriteFile(filepath.Join(repo, "stray.txt"), []byte("x"), 0o644); err != nil {
			t.Errorf("write stray: %v", err)
		}
		return VerificationResult{OK: true, Summary: "ok"}, nil
	}

	runCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ran := make(chan error, 1)
	go func() { ran <- mgr.Run(runCtx, prd.ID, spawn, verify) }()
	finishRun(t, ran)

	id := prd.Story[0].Tasks[0].ID
	cur, _ := mgr.Store().GetTask(id)
	if cur.Status != TaskFailed {
		t.Fatalf("status = %q; want failed (stray write outside the report path)", cur.Status)
	}
	if !strings.Contains(cur.Error, "read-only") || !strings.Contains(cur.Error, "stray.txt") {
		t.Errorf("error %q should name the read-only violation and the stray file", cur.Error)
	}
}

// TestWriterTaskUnaffectedByReadOnlyCheck runs the full executor path for a
// non-read-only (writer) task that dirties the tree: the read-only check must
// not apply to it at all, so it finishes successfully.
func TestWriterTaskUnaffectedByReadOnlyCheck(t *testing.T) {
	fastTreeLockPolling(t)
	repo := gitRepoWithDWSeed(t)
	mgr := newRoManager(t, nil)
	// Non-read-only task: ReportPath is irrelevant, the tree check must be
	// skipped for it even though the tree ends dirty.
	prd := makeReportPathPRD(t, mgr, repo, false, ".datawatch/reports/summary.md")

	spawn := func(_ context.Context, req SpawnRequest) (SpawnResult, error) {
		return SpawnResult{SessionID: "sess-" + req.TaskID}, nil
	}
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		if err := os.WriteFile(filepath.Join(repo, "stray.txt"), []byte("x"), 0o644); err != nil {
			t.Errorf("write stray: %v", err)
		}
		return VerificationResult{OK: true, Summary: "ok"}, nil
	}

	runCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ran := make(chan error, 1)
	go func() { ran <- mgr.Run(runCtx, prd.ID, spawn, verify) }()
	finishRun(t, ran)

	id := prd.Story[0].Tasks[0].ID
	cur, _ := mgr.Store().GetTask(id)
	if cur.Status != TaskCompleted {
		t.Fatalf("status = %q, error %q; want completed (a writer task may change the tree)", cur.Status, cur.Error)
	}
}
