package autonomous

import (
	"context"
	"strings"
	"testing"
)

// fakeCUGit is a fake CompletionLandingGit keyed by branch.
type fakeCUGit struct {
	merged map[string]bool
	calls  []string
}

func (f *fakeCUGit) BranchMerged(_ context.Context, repo, branch string) (bool, string) {
	f.calls = append(f.calls, repo+"|"+branch)
	if f.merged[branch] {
		return true, ""
	}
	return false, ""
}

func cuPRD(id, branch string, st PRDStatus) *PRD {
	p := &PRD{ID: id, Status: st}
	p.Git.Branch = branch
	return p
}

func TestCompletedUnmergedListsOnlyUnmergedWithoutReason(t *testing.T) {
	git := &fakeCUGit{merged: map[string]bool{"b-merged": true}}
	noCode := cuPRD("c-nocode", "b-nocode", PRDCompleted)
	noCode.NoCodeReason = "docs only"
	blocked := cuPRD("e-blocked", "b-blocked", PRDCompleted)
	prds := []*PRD{
		cuPRD("z-open", "b-open", PRDCompleted),
		cuPRD("a-merged", "b-merged", PRDCompleted),
		noCode,
		cuPRD("d-running", "b-run", PRDRunning),
		blocked,
		nil,
		cuPRD("b-open2", "b-open2", PRDCompleted),
	}
	judge := func(p *PRD) string {
		if p.ID == "e-blocked" {
			return "block"
		}
		return "pass"
	}
	got := ListCompletedUnmerged(context.Background(), git, "/repo", prds, judge)
	if len(got) != 2 || got[0].ID != "b-open2" || got[1].ID != "z-open" {
		t.Fatalf("CompletedUnmerged list = %+v, want b-open2 and z-open", got)
	}
	if !strings.Contains(got[0].Detail, "ancestor") {
		t.Errorf("detail = %q", got[0].Detail)
	}
	for _, c := range git.calls {
		if !strings.HasPrefix(c, "/repo|") {
			t.Errorf("git asked about %q, want repo /repo", c)
		}
	}
}

func TestCompletedUnmergedNilGitListsNothing(t *testing.T) {
	if got := ListCompletedUnmerged(nil, nil, "/r", []*PRD{cuPRD("x", "b", PRDCompleted)}, nil); got != nil {
		t.Fatalf("nil git listed %+v", got)
	}
}

func TestCompletedUnmergedSkipsRecordedLandingEvidence(t *testing.T) {
	git := &fakeCUGit{}
	mc := cuPRD("m", "b1", PRDCompleted)
	mc.MergeCommit = "abc123"
	onMain := cuPRD("o", "main", PRDCompleted)
	noBranch := cuPRD("n", "", PRDCompleted)
	got := ListCompletedUnmerged(context.Background(), git, "/r", []*PRD{mc, onMain, noBranch}, nil)
	if len(got) != 0 || len(git.calls) != 0 {
		t.Fatalf("got %+v, git calls %v; want none", got, git.calls)
	}
}
