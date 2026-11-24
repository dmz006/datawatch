// The finished-work guard together with the closed outcome rule, the landing
// tip and the delivered-on-base proof: each keeps working with the others on.

package autonomous

import (
	"context"
	"strings"
	"testing"
	"time"
)

// composeGit is aumGit plus branch tips (rev-parse of a branch ref) and the
// two reads of the delivered-by-content proof.
type composeGit struct {
	*aumGit
	tips   map[string]string // branch -> tip commit
	ported map[string]bool   // branch -> its one changed file is the same on main
}

func (g *composeGit) Run(ctx context.Context, dir string, args ...string) (string, error) {
	last := args[len(args)-1]
	switch {
	case args[0] == "rev-parse" && strings.HasPrefix(last, "refs/heads/"):
		g.record(args)
		return g.tips[strings.TrimPrefix(last, "refs/heads/")] + "\n", nil
	case args[0] == "log" && strings.Contains(last, "..") && g.ported[last[strings.Index(last, "..")+2:]]:
		g.record(args)
		return "ported.go\x00", nil
	case args[0] == "rev-parse" && len(args) == 3 && strings.HasSuffix(last, ":ported.go"):
		g.record(args)
		return "b10b\nb10b\n", nil
	}
	return g.aumGit.Run(ctx, dir, args...)
}

func (g *composeGit) record(args []string) {
	g.mu.Lock()
	g.calls = append(g.calls, append([]string(nil), args...))
	g.mu.Unlock()
}

// newComposeRig is the guard rig with the closed outcome rule on (the daemon
// default) and the composeGit runner.
func newComposeRig(t *testing.T, git *composeGit, n int) *fwRig {
	t.Helper()
	r := newFWRig(t, git.aumGit, n)
	cfg := r.h.m.Config()
	cfg.ClosedOutcomeArchive = ClosedOutcomeOn
	r.h.m.SetConfig(cfg)
	r.h.m.mu.Lock()
	r.h.m.archivedUnmergedGit = git
	r.h.m.mu.Unlock()
	return r
}

// The archive that records an outcome in the same step is an operator path too:
// it must not get past the guard.
func TestArchiveWithOutcomeIsGuardedForUnmergedWork(t *testing.T) {
	git := &composeGit{aumGit: &aumGit{ahead: map[string]string{"work/a": "3\n"}}}
	r := newComposeRig(t, git, 1)
	id := r.finish(t, 0, PRDCompleted, "work/a", r.at.Add(-time.Hour))

	_, err := r.api.ArchiveWithOutcome(id, OutcomeInput{Outcome: OutcomeDelivered})
	if err == nil || !strings.Contains(err.Error(), "3 commit") {
		t.Fatalf("archive with an outcome of unmerged work must be refused, got %v", err)
	}
	if p := r.get(t, id); p.Status != PRDCompleted || p.ClosedOutcome != "" {
		t.Errorf("status %s outcome %q after the refusal, want completed and none", p.Status, p.ClosedOutcome)
	}
}

// Both rules apply: with the landing decided, the closed outcome rule still
// refuses a record with no outcome.
func TestClosedOutcomeStillRefusesAfterALandingDecision(t *testing.T) {
	git := &composeGit{aumGit: &aumGit{ahead: map[string]string{"work/a": "3\n"}}}
	r := newComposeRig(t, git, 1)
	id := r.finish(t, 0, PRDCompleted, "work/a", r.at.Add(-time.Hour))

	_, err := r.api.ArchiveDecided(id, "keep", "kept for review")
	if err == nil || !strings.Contains(err.Error(), "no outcome") {
		t.Fatalf("want the closed outcome refusal, got %v", err)
	}
	if st := r.get(t, id).Status; st != PRDCompleted {
		t.Errorf("status = %s, want completed", st)
	}
}

func TestArchiveTakesDecisionAndOutcomeInOneStep(t *testing.T) {
	git := &composeGit{aumGit: &aumGit{ahead: map[string]string{"work/a": "3\n"}}, tips: map[string]string{"work/a": "tip1"}}
	r := newComposeRig(t, git, 1)
	id := r.finish(t, 0, PRDCompleted, "work/a", r.at.Add(-time.Hour))
	d, ok := any(r.api).(interface {
		ArchiveDecidedWithOutcome(id, decision, reason string, in OutcomeInput) (any, error)
	})
	if !ok {
		t.Fatal("the API cannot take a landing decision and an outcome together")
	}

	if _, err := d.ArchiveDecidedWithOutcome(id, "drop", "not wanted", OutcomeInput{Outcome: "bogus"}); err == nil {
		t.Fatal("an unknown outcome must be refused")
	}
	if p := r.get(t, id); p.Landing != nil && p.Landing.State != LandingPending {
		t.Fatalf("a refused request recorded a landing decision: %+v", p.Landing)
	}

	if _, err := d.ArchiveDecidedWithOutcome(id, "drop", "not wanted", OutcomeInput{Outcome: OutcomeDropped}); err != nil {
		t.Fatalf("archive with drop and dropped: %v", err)
	}
	p := r.get(t, id)
	if p.Status != PRDArchived || p.ClosedOutcome != OutcomeDropped {
		t.Errorf("status %s outcome %q, want archived and dropped", p.Status, p.ClosedOutcome)
	}
	if p.Landing == nil || p.Landing.State != LandingDropped || p.Landing.Tip != "tip1" {
		t.Errorf("landing %+v, want dropped at tip1", p.Landing)
	}
}

func TestCancelTakesDecisionAndOutcomeInOneStep(t *testing.T) {
	git := &composeGit{aumGit: &aumGit{ahead: map[string]string{"work/a": "2\n"}}, tips: map[string]string{"work/a": "tip1"}}
	r := newComposeRig(t, git, 1)
	id := r.finish(t, 0, PRDRunning, "work/a", r.at.Add(-time.Hour))
	if err := r.api.CancelWithOutcome(id, OutcomeInput{Outcome: OutcomeDropped}); err == nil {
		t.Fatal("cancel with an outcome of unmerged work must still need a landing decision")
	}
	d, ok := any(r.api).(interface {
		CancelDecidedWithOutcome(id, decision, reason string, in OutcomeInput) error
	})
	if !ok {
		t.Fatal("the API cannot take a landing decision and an outcome together")
	}
	if err := d.CancelDecidedWithOutcome(id, "keep", "for review", OutcomeInput{Outcome: "bogus"}); err == nil {
		t.Fatal("an unknown outcome must be refused")
	}
	if p := r.get(t, id); p.Landing != nil && p.Landing.State != LandingPending {
		t.Fatalf("a refused request recorded a landing decision: %+v", p.Landing)
	}
	if err := d.CancelDecidedWithOutcome(id, "keep", "for review", OutcomeInput{Outcome: OutcomeSuperseded, DeliveredVia: id}); err == nil {
		t.Fatal("an Automaton cannot be its own twin")
	}
	if err := d.CancelDecidedWithOutcome(id, "keep", "for review", OutcomeInput{Outcome: OutcomeHeld}); err != nil {
		t.Fatalf("cancel with keep and held: %v", err)
	}
	p := r.get(t, id)
	if p.Status != PRDCancelled || p.ClosedOutcome != OutcomeHeld || p.Landing == nil || p.Landing.State != LandingKept {
		t.Errorf("status %s outcome %q landing %+v", p.Status, p.ClosedOutcome, p.Landing)
	}
}

// A keep or drop decision covers the commit it was made at. Once the branch
// moves past it, the new commits need a decision of their own.
func TestGuardStopsHonouringADecisionOnceTheBranchMoves(t *testing.T) {
	for _, decision := range []string{"keep", "drop"} {
		git := &composeGit{aumGit: &aumGit{ahead: map[string]string{"work/a": "3\n"}}, tips: map[string]string{"work/a": "tip1"}}
		r := newComposeRig(t, git, 1)
		id := r.finish(t, 0, PRDCompleted, "work/a", r.at.Add(-time.Hour))
		var err error
		if decision == "keep" {
			_, err = r.h.m.KeepLanding(id, "dave", "reason")
		} else {
			_, err = r.h.m.DropLanding(id, "dave", "reason")
		}
		if err != nil {
			t.Fatalf("%s: %v", decision, err)
		}
		git.tips["work/a"] = "tip2"
		git.ahead["work/a"] = "4\n"
		_, err = r.api.ArchiveWithOutcome(id, OutcomeInput{Outcome: OutcomeDropped})
		if err == nil || !strings.Contains(err.Error(), "4 commit") {
			t.Fatalf("%s: the branch moved past the decision, archive must be refused, got %v", decision, err)
		}
		git.tips["work/a"] = "tip1"
		if _, err := r.api.ArchiveWithOutcome(id, OutcomeInput{Outcome: OutcomeDropped}); err != nil {
			t.Errorf("%s: at the decided commit the archive passes: %v", decision, err)
		}
	}
}

// "Leave it alone" is a keep decision, so it records the tip too: without it
// the unlanded list would honour it for ever.
func TestLeaveAloneAnswerRecordsTheTip(t *testing.T) {
	git := &composeGit{aumGit: &aumGit{ahead: map[string]string{"work/a": "3\n"}}, tips: map[string]string{"work/a": "tip1"}}
	r := newComposeRig(t, git, 1)
	id := r.finish(t, 0, PRDCompleted, "work/a", r.at.Add(-48*time.Hour))
	r.h.m.RunFinishedWorkSweep(context.Background(), r.at)
	qs := r.openQuestions(id)
	if len(qs) != 1 {
		t.Fatalf("open questions = %d, want 1", len(qs))
	}
	answerQ(t, r, qs[0], FinishedWorkOptionLeave)
	p := r.get(t, id)
	if p.Landing == nil || p.Landing.State != LandingKept || p.Landing.Tip != "tip1" {
		t.Fatalf("landing %+v, want kept at tip1", p.Landing)
	}
}

// Work ported to the default branch by hand is delivered: the record views say
// so, and the guard, sweep and audit must agree.
func TestHandPortedBranchIsNotUnmergedWork(t *testing.T) {
	git := &composeGit{aumGit: &aumGit{ahead: map[string]string{"work/p": "3\n"}}, ported: map[string]bool{"work/p": true}}
	r := newComposeRig(t, git, 1)
	id := r.finish(t, 0, PRDCompleted, "work/p", r.at.Add(-48*time.Hour))

	pass := r.h.m.RunFinishedWorkSweep(context.Background(), r.at)
	if pass.Found != 0 || len(r.openQuestions(id)) != 0 {
		t.Errorf("sweep found %d and asked %d about hand-ported work", pass.Found, len(r.openQuestions(id)))
	}
	res, err := r.h.m.RunFinishedWorkAudit(context.Background(), r.at)
	if err != nil {
		t.Fatalf("audit: %v", err)
	}
	if len(res.Findings) != 0 {
		t.Errorf("audit findings for hand-ported work: %+v", res.Findings)
	}
	if _, err := r.api.ArchiveWithOutcome(id, OutcomeInput{Outcome: OutcomeDelivered}); err != nil {
		t.Errorf("archive of hand-ported work: %v", err)
	}
	git.assertReadOnly(t)
}

// The closed outcome refusal at the close step is reported once in the refusal
// ledger. The step is not held, so a recorded outcome closes it on the next
// pass.
func TestCloseStepClosedOutcomeRefusalIsInTheLedgerOnce(t *testing.T) {
	git := &aumGit{ahead: map[string]string{"work/m": "0\n"}}
	h, steps, at := aumRig(t, git, 1)
	cfg := h.m.Config()
	cfg.ClosedOutcomeArchive = ClosedOutcomeOn
	h.m.SetConfig(cfg)
	id := aumArchive(t, h, 0, "work/m", "Merged", at)
	p, _ := h.m.Store().GetPRD(id)
	p.Status, p.Archive = PRDCompleted, nil
	h.m.Store().WithLock(func() { p.UpdatedAt = at.Add(-time.Hour) })

	for i := 0; i < 3; i++ {
		res := resultFor(t, steps.RunPass(context.Background()), stepCloseFinishedRecords)
		if res.Err != nil || !strings.Contains(res.Detail, "no outcome") {
			t.Fatalf("pass %d: %+v", i, res)
		}
	}
	entries := func() (out []RefusalEntry) {
		for _, e := range h.m.RefusalLedger().List() {
			if e.Automaton == id {
				out = append(out, e)
			}
		}
		return out
	}
	if got := entries(); len(got) != 1 || got[0].Repeats != 3 || !strings.Contains(got[0].Reason, "no outcome") {
		t.Fatalf("ledger entries = %+v, want one naming the missing outcome, seen 3 times", got)
	}
	if _, err := h.m.SetClosedOutcome(id, "dave", OutcomeInput{Outcome: OutcomeDropped}); err != nil {
		t.Fatalf("outcome: %v", err)
	}
	resultFor(t, steps.RunPass(context.Background()), stepCloseFinishedRecords)
	if got, _ := h.m.Store().GetPRD(id); got.Status != PRDArchived {
		t.Errorf("status = %s on the pass after the outcome, want archived", got.Status)
	}
	if got := entries(); len(got) != 0 {
		t.Errorf("ledger entries after the record closed = %+v", got)
	}
}
