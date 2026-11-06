package autonomous

import (
	"context"
	"strings"
	"testing"
)

// blockedFixture is a merge fixture with one finished task and a tracked file
// that carries an uncommitted edit in the Automaton worktree.
func blockedFixture(t *testing.T) (*mergeFixture, *[]string) {
	t.Helper()
	f := newMergeFixture(t, "", map[string]string{"shared.txt": mergeBase})
	f.addTask("t1", nil, "a.txt", "a\n")
	f.seal()
	var alerts []string
	f.m.SetAlertSink(func(level, title, body string) string {
		alerts = append(alerts, title+"|"+body)
		return ""
	})
	writeMergeFile(t, f.prd.ProjectDir, "shared.txt", "uncommitted edit\n")
	return f, &alerts
}

func TestMergeBlockedVisibleOnTaskAndTriage(t *testing.T) {
	f, _ := blockedFixture(t)
	out, err := f.m.MergeTaskBranch(context.Background(), f.prd, f.task("t1"))
	if err != nil || out != MergeOutcomeDeferred {
		t.Fatalf("out %s err %v", out, err)
	}
	tk := f.task("t1")
	if tk.MergeState != MergeStatePending {
		t.Fatalf("merge state = %q", tk.MergeState)
	}
	b := tk.MergeBlock
	if b == nil || b.Kind != MergeBlockKindDirtyWorktree || b.Reason == "" || b.Action == "" {
		t.Fatalf("block = %+v", b)
	}
	if !strings.Contains(b.Reason, "shared.txt") && !strings.Contains(b.Action, "shared.txt") {
		t.Fatalf("block names no file: %+v", b)
	}
	stored, ok := f.m.store.SnapshotPRD(f.prd.ID)
	if !ok || stored.Triage == nil || stored.Triage.Status != TriageStatusMergeBlocked ||
		len(stored.Triage.MergeBlocks) != 1 || stored.Triage.MergeBlocks[0].TaskID != "t1" {
		t.Fatalf("triage = %+v", stored.Triage)
	}
}

func TestMergeDirtyTreeFindingNeverTouchesEdit(t *testing.T) {
	f, _ := blockedFixture(t)
	headBefore := gitOut(t, f.prd.ProjectDir, "rev-parse", "HEAD")
	if _, err := f.m.MergeTaskBranch(context.Background(), f.prd, f.task("t1")); err != nil {
		t.Fatal(err)
	}
	if got := f.readPRDFile("shared.txt"); got != "uncommitted edit\n" {
		t.Fatalf("edit lost, file = %q", got)
	}
	if st := gitOut(t, f.prd.ProjectDir, "status", "--porcelain", "--untracked-files=no"); !strings.Contains(st, "shared.txt") {
		t.Fatalf("edit no longer uncommitted: %q", st)
	}
	if head := gitOut(t, f.prd.ProjectDir, "rev-parse", "HEAD"); head != headBefore {
		t.Fatalf("HEAD moved %s -> %s", headBefore, head)
	}
	if s := gitOut(t, f.prd.ProjectDir, "stash", "list"); s != "" {
		t.Fatalf("something was stashed: %q", s)
	}
}

func TestMergeDirtyTreeFindingClearsWhenTreeClean(t *testing.T) {
	f, _ := blockedFixture(t)
	ctx := context.Background()
	if _, err := f.m.MergeTaskBranch(ctx, f.prd, f.task("t1")); err != nil {
		t.Fatal(err)
	}
	if f.task("t1").MergeBlock == nil {
		t.Fatal("no block recorded")
	}
	if _, err := runGitInCtx(ctx, f.prd.ProjectDir, "checkout", "--", "shared.txt"); err != nil {
		t.Fatal(err)
	}
	out, err := f.m.MergeTaskBranch(ctx, f.prd, f.task("t1"))
	if err != nil || out != MergeOutcomeMerged {
		t.Fatalf("out %s err %v", out, err)
	}
	if b := f.task("t1").MergeBlock; b != nil {
		t.Fatalf("block not cleared: %+v", b)
	}
	stored, ok := f.m.store.SnapshotPRD(f.prd.ID)
	if !ok || stored.Triage != nil {
		t.Fatalf("triage entry not removed: %+v", stored.Triage)
	}
}

func TestMergeDirtyTreeFindingAlertsOnce(t *testing.T) {
	f, alerts := blockedFixture(t)
	for i := 0; i < 3; i++ {
		if _, err := f.m.MergeTaskBranch(context.Background(), f.prd, f.task("t1")); err != nil {
			t.Fatal(err)
		}
	}
	if len(*alerts) != 1 {
		t.Fatalf("alerts = %d: %v", len(*alerts), *alerts)
	}
	if a := f.task("t1").MergeBlock.Attempts; a != 3 {
		t.Fatalf("attempts = %d", a)
	}
}
