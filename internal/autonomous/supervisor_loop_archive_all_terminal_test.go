package autonomous

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// akuBase is the injected "now" for these tests, derived from the host clock
// so the record's own save stamps never outrun it.
func akuBase() time.Time { return time.Now().Add(48 * time.Hour) }

// akuRig puts record 0 in status with a branch recorded, the base repository
// set, git faked and the clock injected.
func akuRig(t *testing.T, status PRDStatus, git *slbFakeGit, warn bool) (*coordHarness, *SupervisorLoopSteps, time.Time) {
	t.Helper()
	h, steps := slGraceRig(t, 60)
	base := akuBase()
	cfg := h.m.Config()
	cfg.WorktreeBaseRepo = "/repo"
	h.m.SetConfig(cfg)
	p, ok := h.m.Store().GetPRD(h.prds[0].ID)
	if !ok {
		t.Fatal("prd missing")
	}
	p.Git.Branch = "automaton/work"
	p.Git.PushState = PushStatePushed
	p.Status = status
	if warn {
		p.Verdicts = append(p.Verdicts, GuardrailVerdict{Guardrail: "rules", Outcome: "warn", Summary: "minor", VerdictAt: base})
	}
	if err := h.m.Store().SavePRD(p); err != nil {
		t.Fatal(err)
	}
	if status == PRDCancelled {
		slCancelRecord(t, h, 0, base.Add(-90*time.Minute))
	}
	slSetClock(t, steps, base)
	steps.mergeGit = git
	return h, steps, base
}

func akuRun(t *testing.T, steps *SupervisorLoopSteps) string {
	t.Helper()
	r := resultFor(t, steps.RunPass(context.Background()), stepCloseFinishedRecords)
	if r.Err != nil {
		t.Fatalf("pass: %+v", r)
	}
	return r.Detail
}

var akuAhead = &slbFakeGit{refOut: "refs/heads/automaton/work\n", ahead: "3\n"}

func TestArchiveKeepsUnmergedCompleted(t *testing.T) {
	h, steps, _ := akuRig(t, PRDCompleted, akuAhead, false)
	detail := akuRun(t, steps)
	if slStatus(t, h, 0) != PRDCompleted {
		t.Errorf("completed with unmerged commits = %s, want kept", slStatus(t, h, 0))
	}
	if !strings.Contains(detail, "kept 1 completed") {
		t.Errorf("detail %q lacks the completed count", detail)
	}
	if got := steps.HeldReason(h.prds[0].ID); !strings.Contains(got, "3 ahead") {
		t.Errorf("held reason %q does not name the ahead count", got)
	}
}

func TestArchiveKeepsUnmergedCompletedWarnVerdictPushed(t *testing.T) {
	h, steps, _ := akuRig(t, PRDCompleted, akuAhead, true)
	akuRun(t, steps)
	if slStatus(t, h, 0) != PRDCompleted {
		t.Errorf("warn-verdict pushed completed record = %s, want kept", slStatus(t, h, 0))
	}
}

func TestArchiveKeepsUnmergedRejected(t *testing.T) {
	h, steps, _ := akuRig(t, PRDRejected, akuAhead, false)
	detail := akuRun(t, steps)
	if slStatus(t, h, 0) != PRDRejected {
		t.Errorf("rejected with unmerged commits = %s, want kept", slStatus(t, h, 0))
	}
	if !strings.Contains(detail, "kept 1 rejected") {
		t.Errorf("detail %q lacks the rejected count", detail)
	}
}

func TestArchiveKeepsUnmergedCancelledHeldThroughGraceThenKept(t *testing.T) {
	h, steps, base := akuRig(t, PRDCancelled, akuAhead, false)
	slCancelRecord(t, h, 0, base.Add(-10*time.Minute))
	detail := akuRun(t, steps)
	if slStatus(t, h, 0) != PRDCancelled || !strings.Contains(detail, "held 1 cancelled") {
		t.Fatalf("inside grace: status %s detail %q, want held", slStatus(t, h, 0), detail)
	}
	if steps.HeldReason(h.prds[0].ID) != "" {
		t.Errorf("grace hold carries a branch reason")
	}
	slSetClock(t, steps, base.Add(2*time.Hour))
	detail = akuRun(t, steps)
	if slStatus(t, h, 0) != PRDCancelled || !strings.Contains(detail, "kept 1 cancelled") {
		t.Errorf("past grace: status %s detail %q, want kept", slStatus(t, h, 0), detail)
	}
}

func TestArchiveKeepsUnmergedZeroAheadArchived(t *testing.T) {
	h, steps, _ := akuRig(t, PRDCompleted, &slbFakeGit{refOut: "refs/heads/automaton/work\n", ahead: "0\n"}, false)
	akuRun(t, steps)
	if slStatus(t, h, 0) != PRDArchived {
		t.Errorf("completed with zero ahead = %s, want archived", slStatus(t, h, 0))
	}
}

func TestArchiveKeepsUnmergedGitErrorKeeps(t *testing.T) {
	h, steps, _ := akuRig(t, PRDCompleted, &slbFakeGit{refErr: errors.New("boom")}, false)
	akuRun(t, steps)
	if slStatus(t, h, 0) != PRDCompleted {
		t.Errorf("git error archived the completed record, want kept")
	}
}

func TestArchiveKeepsUnmergedSettingOffArchivesCompleted(t *testing.T) {
	h, steps, _ := akuRig(t, PRDCompleted, akuAhead, false)
	off := false
	cfg := h.m.Config()
	cfg.ArchiveKeepUnmerged = &off
	h.m.SetConfig(cfg)
	akuRun(t, steps)
	if slStatus(t, h, 0) != PRDArchived {
		t.Errorf("setting off: completed = %s, want archived", slStatus(t, h, 0))
	}
}

func TestArchiveKeepsUnmergedSettingOffCancelledStillGuarded(t *testing.T) {
	h, steps, _ := akuRig(t, PRDCancelled, akuAhead, false)
	off := false
	cfg := h.m.Config()
	cfg.ArchiveKeepUnmerged = &off
	h.m.SetConfig(cfg)
	akuRun(t, steps)
	if slStatus(t, h, 0) != PRDCancelled {
		t.Errorf("setting off: cancelled with unmerged work = %s, want kept", slStatus(t, h, 0))
	}
}
