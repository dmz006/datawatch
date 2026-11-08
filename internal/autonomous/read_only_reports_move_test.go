package autonomous

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
