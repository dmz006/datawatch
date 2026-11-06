package autonomous

import (
	"context"
	"testing"
)

func TestMergeRetryPassRetriesBlockedThenMerges(t *testing.T) {
	f := newMergeFixture(t, "", map[string]string{"shared.txt": mergeBase})
	f.addTask("t1", nil, "a.txt", "a\n")
	f.seal()
	ctx := context.Background()
	writeMergeFile(t, f.prd.ProjectDir, "shared.txt", "uncommitted edit\n")

	if n := f.m.MergeRetryPass(ctx); n != 0 {
		t.Fatalf("blocked pass merged %d", n)
	}
	tk := f.task("t1")
	if tk.MergeBlock == nil || tk.MergeBlock.LastRetryAt.IsZero() || tk.MergeState != MergeStatePending {
		t.Fatalf("task = state %q block %+v", tk.MergeState, tk.MergeBlock)
	}
	if _, err := runGitInCtx(ctx, f.prd.ProjectDir, "checkout", "--", "shared.txt"); err != nil {
		t.Fatal(err)
	}
	if n := f.m.MergeRetryPass(ctx); n != 1 {
		t.Fatalf("clean pass merged %d, want 1", n)
	}
	if f.task("t1").MergeState != MergeStateMerged {
		t.Fatalf("state %q", f.task("t1").MergeState)
	}
}

func TestMergeRetryPassSkipsCancelledAndArchived(t *testing.T) {
	for _, st := range []PRDStatus{PRDCancelled, PRDArchived} {
		f := newMergeFixture(t, "", nil)
		f.addTask("t1", nil, "a.txt", "a\n")
		f.prd.Status = st
		f.seal()
		if n := f.m.MergeRetryPass(context.Background()); n != 0 || f.task("t1").MergeState == MergeStateMerged {
			t.Fatalf("%s: merged %d", st, n)
		}
	}
}
