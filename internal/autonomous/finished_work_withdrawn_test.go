// A finished-work question that someone withdraws stays settled until the
// branch moves or the re-ask date the daemon states passes. Before this rule
// the next sweep raised the same question again, because the raise only looked
// for an OPEN question with the same key.

package autonomous

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// withdrawnRig is one finished Automaton with unmerged work at tip1 and the
// question the first sweep raised about it.
func withdrawnRig(t *testing.T) (*fwRig, *composeGit, string, Question) {
	t.Helper()
	git := &composeGit{aumGit: &aumGit{ahead: map[string]string{"work/a": "3\n"}}, tips: map[string]string{"work/a": "tip1"}}
	r := newComposeRig(t, git, 1)
	r.h.m.now = func() time.Time { return r.at }
	id := r.finish(t, 0, PRDCompleted, "work/a", r.at.Add(-48*time.Hour))
	r.h.m.RunFinishedWorkSweep(context.Background(), r.at)
	return r, git, id, mustOpenQ(t, r, id)
}

func withdrawQ(t *testing.T, r *fwRig, q Question, actor string) {
	t.Helper()
	if _, err := r.store.Withdraw(q.ID, actor, "not now"); err != nil {
		t.Fatalf("withdraw: %v", err)
	}
}

// sweepOpen runs one pass at the given time and returns the open questions.
func sweepOpen(r *fwRig, id string, at time.Time) []Question {
	r.h.m.RunFinishedWorkSweep(context.Background(), at)
	return r.openQuestions(id)
}

func TestWithdrawnFinishedWorkQuestionIsNotRaisedAgain(t *testing.T) {
	r, git, id, q := withdrawnRig(t)
	withdrawQ(t, r, q, "dave")
	reask := r.at.AddDate(0, 0, FinishedWorkReaskDays)

	for _, at := range []time.Time{r.at.Add(7 * time.Minute), r.at.Add(48 * time.Hour), reask.Add(-time.Minute)} {
		if n := len(sweepOpen(r, id, at)); n != 0 {
			t.Fatalf("pass at %s: %d open question(s) after a withdrawal with an unmoved branch, want 0", at, n)
		}
	}

	// The re-ask date is stated where the operator reads: the decision log and
	// the finding.
	date := reask.Format("2006-01-02")
	var noted bool
	for _, d := range r.get(t, id).Decisions {
		if d.QuestionID == q.ID && d.Actor == "dave" && strings.Contains(d.Note, date) {
			noted = true
		}
	}
	if !noted {
		t.Errorf("want a decision line by the withdrawing actor naming the re-ask date %s: %+v", date, r.get(t, id).Decisions)
	}
	f := r.h.m.SweepFindings()
	if len(f) != 1 || f[0].QuestionID != "" || !strings.Contains(f[0].Action, date) || !strings.HasPrefix(f[0].ReaskAt, date) {
		t.Errorf("finding = %+v, want it still listed, with no question, stating the re-ask date %s", f, date)
	}
	git.assertReadOnly(t)
}

func TestWithdrawnFinishedWorkQuestionIsAskedAgainWhenTheBranchMoves(t *testing.T) {
	r, git, id, q := withdrawnRig(t)
	withdrawQ(t, r, q, "dave")
	if n := len(sweepOpen(r, id, r.at.Add(time.Hour))); n != 0 {
		t.Fatalf("open = %d before the branch moved, want 0", n)
	}
	git.tips["work/a"] = "tip2"
	git.ahead["work/a"] = "4\n"
	qs := sweepOpen(r, id, r.at.Add(2*time.Hour))
	if len(qs) != 1 || !strings.Contains(qs[0].Text, "4 commit") {
		t.Fatalf("open = %+v after the branch moved, want one question about 4 commits", qs)
	}
	if n := len(sweepOpen(r, id, r.at.Add(3*time.Hour))); n != 1 {
		t.Errorf("open = %d on the next pass, want still 1", n)
	}
}

func TestWithdrawnFinishedWorkQuestionIsAskedAgainOnTheReaskDate(t *testing.T) {
	r, _, id, q := withdrawnRig(t)
	withdrawQ(t, r, q, "dave")
	reask := r.at.AddDate(0, 0, FinishedWorkReaskDays)
	qs := sweepOpen(r, id, reask.Add(time.Minute))
	if len(qs) != 1 || qs[0].ID == q.ID {
		t.Fatalf("open = %+v on the re-ask date, want one new question", qs)
	}
	// Withdrawing the new one settles it again, from its own withdrawal time.
	r.h.m.now = func() time.Time { return reask.Add(time.Hour) }
	withdrawQ(t, r, qs[0], "dave")
	if n := len(sweepOpen(r, id, reask.Add(48*time.Hour))); n != 0 {
		t.Errorf("open = %d after the second withdrawal, want 0", n)
	}
}

func TestWithdrawnSettlementSurvivesARestart(t *testing.T) {
	r, _, id, q := withdrawnRig(t)
	withdrawQ(t, r, q, "dave")
	dir := filepath.Dir(r.h.m.FinishedWorkSweep().path)
	r.h.m.mu.Lock()
	r.h.m.finishedWork = NewFinishedWorkSweep(dir)
	r.h.m.mu.Unlock()
	if n := len(sweepOpen(r, id, r.at.Add(time.Hour))); n != 0 {
		t.Errorf("open = %d after reopening the finished-work store, want 0", n)
	}
}

// A withdrawal the hook never saw (a crash between the two writes, or one made
// before this rule) is settled by the next sweep that finds it.
func TestWithdrawnQuestionTheHookMissedIsSettledByTheSweep(t *testing.T) {
	r, git, id, q := withdrawnRig(t)
	r.store.SetChangeHook(nil)
	withdrawQ(t, r, q, "dave")
	if n := len(sweepOpen(r, id, r.at.Add(10*time.Minute))); n != 0 {
		t.Fatalf("open = %d after a withdrawal the hook missed, want 0", n)
	}
	if n := len(sweepOpen(r, id, r.at.Add(20*time.Minute))); n != 0 {
		t.Fatalf("open = %d on the following pass, want 0", n)
	}
	git.tips["work/a"] = "tip2"
	if n := len(sweepOpen(r, id, r.at.Add(30*time.Minute))); n != 1 {
		t.Errorf("open = %d after the branch moved, want 1", n)
	}
}

// The daemon's own withdrawal (the work landed) settles nothing: work that is
// unmerged again is asked about again.
func TestDaemonWithdrawalOfLandedWorkSettlesNothing(t *testing.T) {
	r, git, id, _ := withdrawnRig(t)
	git.ahead["work/a"] = "0\n"
	if n := len(sweepOpen(r, id, r.at.Add(time.Hour))); n != 0 {
		t.Fatalf("open = %d once the work landed, want 0", n)
	}
	git.ahead["work/a"] = "3\n"
	if n := len(sweepOpen(r, id, r.at.Add(2*time.Hour))); n != 1 {
		t.Errorf("open = %d when the work is unmerged again, want 1", n)
	}
}

// Withdrawing is not a keep or drop decision: the archive guard still refuses.
func TestWithdrawingIsNotALandingDecision(t *testing.T) {
	r, _, id, q := withdrawnRig(t)
	withdrawQ(t, r, q, "dave")
	if l := r.get(t, id).Landing; l != nil {
		t.Errorf("landing = %+v after a withdrawal, want none", l)
	}
	if _, err := r.h.m.ArchiveGuarded(id, "operator"); err == nil {
		t.Error("archive must still be refused after the question was only withdrawn")
	}
}

// Once the daemon has asked again, the old withdrawal is used up: it must not
// silence a later re-ask date, even if the branch is back at the old commit.
func TestAUsedWithdrawalSilencesNothingLater(t *testing.T) {
	r, git, id, q := withdrawnRig(t)
	withdrawQ(t, r, q, "dave")
	git.tips["work/a"] = "tip2"
	qs := sweepOpen(r, id, r.at.Add(time.Hour))
	if len(qs) != 1 {
		t.Fatalf("open = %d after the branch moved, want 1", len(qs))
	}
	r.h.m.now = func() time.Time { return r.at.Add(2 * time.Hour) }
	answerQ(t, r, qs[0], FinishedWorkOptionLand)
	git.tips["work/a"] = "tip1"
	due := r.at.AddDate(0, 0, FinishedWorkLandReaskDays+1)
	if n := len(sweepOpen(r, id, due)); n != 1 {
		t.Errorf("open = %d on the later re-ask date, want 1", n)
	}
}
