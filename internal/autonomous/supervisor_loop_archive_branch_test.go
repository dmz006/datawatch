package autonomous

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// slbFakeGit is a MergeGitRunner that answers from the git subcommand.
type slbFakeGit struct {
	refOut   string // for-each-ref output; empty means the branch is gone
	refErr   error
	ahead    string // rev-list --count output
	aheadErr error
}

func (f *slbFakeGit) Run(_ context.Context, _ string, args ...string) (string, error) {
	switch args[0] {
	case "for-each-ref":
		return f.refOut, f.refErr
	case "rev-list":
		return f.ahead, f.aheadErr
	case "remote":
		return "", nil
	case "symbolic-ref":
		return "", errors.New("no symbolic ref")
	default: // cherry, log
		return "", nil
	}
}

// slbRig builds a rig with record 0 cancelled well past a 60-minute grace and
// a branch recorded on it.
func slbRig(t *testing.T, repo string, git *slbFakeGit) (*coordHarness, *SupervisorLoopSteps) {
	t.Helper()
	h, steps := slGraceRig(t, 60)
	cfg := h.m.Config()
	cfg.WorktreeBaseRepo = repo
	h.m.SetConfig(cfg)
	p, ok := h.m.Store().GetPRD(h.prds[0].ID)
	if !ok {
		t.Fatal("prd missing")
	}
	p.Git.Branch = "automaton/work"
	if err := h.m.Store().SavePRD(p); err != nil {
		t.Fatal(err)
	}
	slCancelRecord(t, h, 0, slBase.Add(-90*time.Minute))
	slSetClock(t, steps, slBase)
	steps.mergeGit = git
	return h, steps
}

func slbRun(t *testing.T, steps *SupervisorLoopSteps) {
	t.Helper()
	if r := resultFor(t, steps.RunPass(context.Background()), stepCloseFinishedRecords); r.Err != nil {
		t.Fatalf("pass: %+v", r)
	}
}

func TestSupervisorLoopArchiveBranchAheadKept(t *testing.T) {
	h, steps := slbRig(t, "/repo", &slbFakeGit{refOut: "refs/heads/automaton/work\n", ahead: "3\n"})
	slbRun(t, steps)
	if slStatus(t, h, 0) != PRDCancelled {
		t.Errorf("cancelled record with commits ahead of main archived, want kept")
	}
}

func TestSupervisorLoopArchiveBranchZeroAheadArchived(t *testing.T) {
	h, steps := slbRig(t, "/repo", &slbFakeGit{refOut: "refs/heads/automaton/work\n", ahead: "0\n"})
	slbRun(t, steps)
	if slStatus(t, h, 0) != PRDArchived {
		t.Errorf("record with zero commits ahead = %s, want archived", slStatus(t, h, 0))
	}
}

func TestSupervisorLoopArchiveBranchMissingArchived(t *testing.T) {
	h, steps := slbRig(t, "/repo", &slbFakeGit{refOut: "", ahead: "5\n"})
	slbRun(t, steps)
	if slStatus(t, h, 0) != PRDArchived {
		t.Errorf("record whose branch is gone = %s, want archived", slStatus(t, h, 0))
	}
}

func TestSupervisorLoopArchiveBranchNoBranchRecordedArchived(t *testing.T) {
	h, steps := slbRig(t, "/repo", &slbFakeGit{refOut: "x", ahead: "5\n"})
	p, _ := h.m.Store().GetPRD(h.prds[0].ID)
	p.Git.Branch = ""
	if err := h.m.Store().SavePRD(p); err != nil {
		t.Fatal(err)
	}
	slbRun(t, steps)
	if slStatus(t, h, 0) != PRDArchived {
		t.Errorf("record with no branch = %s, want archived", slStatus(t, h, 0))
	}
}

func TestSupervisorLoopArchiveBranchGitErrorKept(t *testing.T) {
	for name, g := range map[string]*slbFakeGit{
		"ref lookup":   {refErr: errors.New("boom")},
		"compare call": {refOut: "refs/heads/automaton/work\n", aheadErr: errors.New("boom")},
		"bad count":    {refOut: "refs/heads/automaton/work\n", ahead: "nope\n"},
	} {
		h, steps := slbRig(t, "/repo", g)
		slbRun(t, steps)
		if slStatus(t, h, 0) != PRDCancelled {
			t.Errorf("%s: git failure archived the record, want kept", name)
		}
	}
}

func TestSupervisorLoopArchiveBranchEmptyBaseRepoKept(t *testing.T) {
	h, steps := slbRig(t, "", &slbFakeGit{refOut: "", ahead: "0\n"})
	slbRun(t, steps)
	if slStatus(t, h, 0) != PRDCancelled {
		t.Errorf("empty base repository archived the record, want kept")
	}
}

func TestSupervisorLoopArchiveBranchUnmergedKeptPastAnyGrace(t *testing.T) {
	h, steps := slbRig(t, "/repo", &slbFakeGit{refOut: "refs/heads/automaton/work\n", ahead: "2\n"})
	// A year past the cancel, and again with the grace switched off.
	slSetClock(t, steps, slBase.Add(365*24*time.Hour))
	slbRun(t, steps)
	if slStatus(t, h, 0) != PRDCancelled {
		t.Fatalf("unmerged record archived a year past its grace")
	}
	h2, steps2 := slGraceRig(t, 0)
	cfg := h2.m.Config()
	cfg.WorktreeBaseRepo = "/repo"
	h2.m.SetConfig(cfg)
	p, _ := h2.m.Store().GetPRD(h2.prds[0].ID)
	p.Git.Branch = "automaton/work"
	if err := h2.m.Store().SavePRD(p); err != nil {
		t.Fatal(err)
	}
	slCancelRecord(t, h2, 0, slBase.Add(-time.Hour))
	slSetClock(t, steps2, slBase)
	steps2.mergeGit = &slbFakeGit{refOut: "refs/heads/automaton/work\n", ahead: "2\n"}
	slbRun(t, steps2)
	if slStatus(t, h2, 0) != PRDCancelled {
		t.Errorf("unmerged record archived with grace 0")
	}
}

func TestSupervisorLoopArchiveBranchOperatorArchiveStillArchives(t *testing.T) {
	h, steps := slbRig(t, "/repo", &slbFakeGit{refOut: "refs/heads/automaton/work\n", ahead: "4\n"})
	slbRun(t, steps)
	if slStatus(t, h, 0) != PRDCancelled {
		t.Fatalf("loop archived the unmerged record")
	}
	if _, err := h.m.Archive(h.prds[0].ID, "operator"); err != nil {
		t.Fatalf("operator archive: %v", err)
	}
	if slStatus(t, h, 0) != PRDArchived {
		t.Errorf("operator Archive left the record %s, want archived", slStatus(t, h, 0))
	}
}

func TestSupervisorLoopArchiveBranchDetailMentionsKept(t *testing.T) {
	_, steps := slbRig(t, "/repo", &slbFakeGit{refOut: "refs/heads/automaton/work\n", ahead: "1\n"})
	r := resultFor(t, steps.RunPass(context.Background()), stepCloseFinishedRecords)
	if !strings.Contains(r.Detail, "kept 1") {
		t.Errorf("detail %q does not report the kept record", r.Detail)
	}
}
