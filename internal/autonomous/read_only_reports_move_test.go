package autonomous

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReadOnlyReportsFolderFilesMoved(t *testing.T) {
	ctx := context.Background()
	repo := gitRepoWithDWSeed(t)
	mgr := newRoManager(t, nil)
	prd := makeReportPathPRD(t, mgr, repo, true, ".datawatch/reports/sub/summary.md")
	tk, ok := mgr.Store().GetTask(prd.Story[0].Tasks[0].ID)
	if !ok || tk == nil {
		t.Fatal("GetTask")
	}
	base, ok := mgr.readOnlyTreeStatus(ctx, prd, tk)
	if !ok {
		t.Fatal("readOnlyTreeStatus did not run")
	}
	write := func(rel, body string) {
		p := filepath.Join(repo, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("docs/plans/reports/extra.md", "new")
	mgr.settleReadOnlyReportFiles(ctx, prd, tk, base)
	if err := mgr.readOnlyViolation(ctx, prd, tk, base); err != nil {
		t.Fatalf("extra report file must not violate: %v", err)
	}
	moved := filepath.Join(repo, ".datawatch/reports/sub/extra.md")
	if b, err := os.ReadFile(moved); err != nil || string(b) != "new" {
		t.Fatalf("file not moved: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, "docs/plans/reports/extra.md")); err == nil {
		t.Fatal("source still present")
	}
	cur, _ := mgr.Store().GetPRD(prd.ID)
	found := false
	for _, d := range cur.Decisions {
		if d.Kind == "read_only_report_moved" && d.Actor == "autonomous" && strings.Contains(d.Note, "docs/plans/reports/extra.md") {
			found = true
		}
	}
	if !found {
		t.Fatal("no decision naming the moved file")
	}

	// Same name already present: the new copy is deleted, the old kept.
	write("docs/plans/reports/extra.md", "dup")
	mgr.settleReadOnlyReportFiles(ctx, prd, tk, base)
	if _, err := os.Stat(filepath.Join(repo, "docs/plans/reports/extra.md")); err == nil {
		t.Fatal("duplicate not deleted")
	}
	if b, _ := os.ReadFile(moved); string(b) != "new" {
		t.Fatal("existing file was overwritten")
	}
	if err := mgr.readOnlyViolation(ctx, prd, tk, base); err != nil {
		t.Fatalf("violation after dup delete: %v", err)
	}
	cur, _ = mgr.Store().GetPRD(prd.ID)
	if n := cur.Decisions[len(cur.Decisions)-1].Note; !strings.Contains(n, "deleted") {
		t.Fatalf("decision should say deleted: %s", n)
	}

	// Anything else still violates.
	write("other.txt", "x")
	mgr.settleReadOnlyReportFiles(ctx, prd, tk, base)
	if err := mgr.readOnlyViolation(ctx, prd, tk, base); err == nil {
		t.Fatal("other new file must violate")
	}
}

// A new untracked file already under .datawatch/reports/ is excluded from the
// status by the .datawatch pathspec exclude, so it never fails the task even
// without a settle step.
func TestReadOnlyViolationIgnoresDatawatchReportsFile(t *testing.T) {
	ctx := context.Background()
	repo := gitRepoWithDWSeed(t)
	mgr := newRoManager(t, nil)
	prd := makeReportPathPRD(t, mgr, repo, true, ".datawatch/reports/sub/summary.md")
	tk, ok := mgr.Store().GetTask(prd.Story[0].Tasks[0].ID)
	if !ok || tk == nil {
		t.Fatal("GetTask")
	}
	base, ok := mgr.readOnlyTreeStatus(ctx, prd, tk)
	if !ok {
		t.Fatal("readOnlyTreeStatus did not run")
	}
	p := filepath.Join(repo, ".datawatch/reports/other/notes.md")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := mgr.readOnlyViolation(ctx, prd, tk, base); err != nil {
		t.Fatalf("file under .datawatch/reports must not violate: %v", err)
	}
	mgr.settleReadOnlyReportFiles(ctx, prd, tk, base)
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("file under .datawatch/reports must be left alone: %v", err)
	}
}

// A report file that predates the task is in the baseline, even when git
// quotes its name (non-ASCII), so the settle step must leave it in place.
func TestReadOnlySettleKeepsQuotedBaselineFile(t *testing.T) {
	ctx := context.Background()
	repo := gitRepoWithDWSeed(t)
	mgr := newRoManager(t, nil)
	prd := makeReportPathPRD(t, mgr, repo, true, ".datawatch/reports/sub/summary.md")
	tk, ok := mgr.Store().GetTask(prd.Story[0].Tasks[0].ID)
	if !ok || tk == nil {
		t.Fatal("GetTask")
	}
	rel := "docs/plans/reports/café.md"
	p := filepath.Join(repo, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	base, ok := mgr.readOnlyTreeStatus(ctx, prd, tk)
	if !ok {
		t.Fatal("readOnlyTreeStatus did not run")
	}
	if !readOnlyBaselineUntracked(base)[rel] {
		t.Fatalf("baseline %q does not list %q as untracked", base, rel)
	}
	mgr.settleReadOnlyReportFiles(ctx, prd, tk, base)
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("pre-existing quoted file must not be moved: %v", err)
	}
	if err := mgr.readOnlyViolation(ctx, prd, tk, base); err != nil {
		t.Fatalf("pre-existing file must not violate: %v", err)
	}
}

// TestReadOnlySourceEditStillFails runs the full executor path for a
// read-only task that changes the tree outside the reports folders. Both a
// tracked source edit and a new source file, alongside a report file under
// docs/plans/reports/, must fail with the read-only tree violation.
func TestReadOnlySourceEditStillFails(t *testing.T) {
	cases := []struct {
		name string
		// setup seeds the repo before the task starts; it may commit files.
		setup func(t *testing.T, repo string)
		// edit runs as the task's verify step and changes the tree.
		edit func(t *testing.T, repo string)
		// wantName is the path the violation error must name.
		wantName string
		// wantMoved, when set, is a report file that must be moved out of
		// docs/plans/reports/ and into .datawatch/reports/ before the check.
		wantMoved string
	}{
		{
			name: "tracked source edit",
			setup: func(t *testing.T, repo string) {
				roWriteAndCommit(t, repo, "internal/a.go", "package a\n")
			},
			edit: func(t *testing.T, repo string) {
				roWriteFile(t, filepath.Join(repo, "internal/a.go"), "package a // edited\n")
			},
			wantName: "internal/a.go",
		},
		{
			name:  "report file plus new source file",
			setup: func(t *testing.T, repo string) {},
			edit: func(t *testing.T, repo string) {
				roWriteFile(t, filepath.Join(repo, "docs/plans/reports/extra.md"), "report")
				roWriteFile(t, filepath.Join(repo, "internal/new.go"), "package a\n")
			},
			wantName:  "internal/new.go",
			wantMoved: ".datawatch/reports/extra.md",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fastTreeLockPolling(t)
			repo := gitRepoWithDWSeed(t)
			tc.setup(t, repo)
			mgr := newRoManager(t, nil)
			prd := makeReportPathPRD(t, mgr, repo, true, ".datawatch/reports/summary.md")

			spawn := func(_ context.Context, req SpawnRequest) (SpawnResult, error) {
				return SpawnResult{SessionID: "sess-" + req.TaskID}, nil
			}
			verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
				tc.edit(t, repo)
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
				t.Fatalf("status = %q; want failed (source change is a read-only violation)", cur.Status)
			}
			if !strings.Contains(cur.Error, "read-only task changed the tree") {
				t.Errorf("error %q should name the read-only tree violation", cur.Error)
			}
			if !strings.Contains(cur.Error, tc.wantName) {
				t.Errorf("error %q should name %q", cur.Error, tc.wantName)
			}
			if tc.wantMoved != "" {
				if _, err := os.Stat(filepath.Join(repo, tc.wantMoved)); err != nil {
					t.Errorf("report file should have been moved to %s: %v", tc.wantMoved, err)
				}
			}
		})
	}
}

// roWriteFile writes body to p, creating its parent folders.
func roWriteFile(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
}

// roWriteAndCommit writes rel under repo and commits it, so it is tracked.
func roWriteAndCommit(t *testing.T, repo, rel, body string) {
	t.Helper()
	roWriteFile(t, filepath.Join(repo, rel), body)
	gitRun(t, repo, "add", rel)
	gitRun(t, repo, "commit", "-q", "-m", "seed "+rel)
}
