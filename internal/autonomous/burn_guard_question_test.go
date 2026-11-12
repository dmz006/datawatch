package autonomous

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/dmz006/datawatch/internal/burnguard"
)

// fakeBurnGuardQuestionStore is an in-memory question store for the burn guard
// question wiring. It records withdrawals (actor + reason) so a test can assert
// the guard withdrew the previous question and why.
type fakeBurnGuardQuestionStore struct {
	mu          sync.Mutex
	seq         int
	questions   map[string]Question
	withdrawals []fakeBurnGuardWithdrawal
}

type fakeBurnGuardWithdrawal struct {
	id     string
	actor  string
	reason string
}

func newFakeBurnGuardQuestionStore() *fakeBurnGuardQuestionStore {
	return &fakeBurnGuardQuestionStore{questions: map[string]Question{}}
}

func (f *fakeBurnGuardQuestionStore) Raise(q Question) (Question, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if q.State != "" {
		return Question{}, ErrInvalidQuestion
	}
	f.seq++
	q.ID = fmt.Sprintf("bgq-%02d", f.seq)
	q.State = QuestionOpen
	f.questions[q.ID] = q
	return q, nil
}

func (f *fakeBurnGuardQuestionStore) Withdraw(id, actor, reason string) (Question, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	q, ok := f.questions[id]
	if !ok {
		return Question{}, ErrQuestionNotFound
	}
	if q.State != QuestionOpen {
		return Question{}, ErrQuestionNotOpen
	}
	q.State = QuestionWithdrawn
	f.questions[id] = q
	f.withdrawals = append(f.withdrawals, fakeBurnGuardWithdrawal{id: id, actor: actor, reason: reason})
	return q, nil
}

func (f *fakeBurnGuardQuestionStore) List(filter QuestionFilter) []Question {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []Question
	for _, q := range f.questions {
		if filter.State != "" && q.State != filter.State {
			continue
		}
		if filter.Asker != "" && q.AskedBy != filter.Asker {
			continue
		}
		out = append(out, q)
	}
	sortQuestionsNewestFirst(out)
	return out
}

func sortQuestionsNewestFirst(qs []Question) {
	for i := 0; i < len(qs); i++ {
		for j := i + 1; j < len(qs); j++ {
			if qs[j].ID > qs[i].ID {
				qs[i], qs[j] = qs[j], qs[i]
			}
		}
	}
}

func (f *fakeBurnGuardQuestionStore) openGuard() []Question {
	if opened := f.List(QuestionFilter{State: QuestionOpen, Asker: BurnGuardQuestionAsker}); opened != nil {
		return opened
	}
	return nil
}

func (f *fakeBurnGuardQuestionStore) state(id string) QuestionState {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.questions[id].State
}

func (f *fakeBurnGuardQuestionStore) withdrawalsFor(id string) []fakeBurnGuardWithdrawal {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []fakeBurnGuardWithdrawal
	for _, w := range f.withdrawals {
		if w.id == id {
			out = append(out, w)
		}
	}
	return out
}

func (f *fakeBurnGuardQuestionStore) countWithdrawals() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.withdrawals)
}

// TestBurnGuardAskOnChangeFalseRaisesNothing: a guard that changes a rung with
// ask_on_change off raises no question and withdraws the open guard question,
// and a hold leaves the store alone.
func TestBurnGuardAskOnChangeFalseRaisesNothing(t *testing.T) {
	st := newFakeBurnGuardQuestionStore()
	seed, err := st.Raise(BurnGuardQuestion("I did this: lowered the inflight cap of claude-code from 3 to 2."))
	if err != nil {
		t.Fatal(err)
	}

	ApplyBurnGuardAskOnChange(st, false, burnguard.Decision{
		Direction: burnguard.DirectionClimb, Rung: burnguard.RungRoutePiPrompt,
		Changed: true, AlertText: "The burn guard is at rung 2.",
	}, "")
	if opened := st.openGuard(); len(opened) != 0 {
		t.Fatalf("open guard questions after an ask-off change = %v, want none", opened)
	}
	ws := st.withdrawalsFor(seed.ID)
	if len(ws) != 1 || ws[0].reason != BurnGuardReasonAskOff || ws[0].actor != burnGuardQuestionActor {
		t.Fatalf("withdrawals for %s = %+v, want one by the guard with the ask-off reason", seed.ID, ws)
	}

	raised, withdrawals := st.seq, st.countWithdrawals()
	ApplyBurnGuardAskOnChange(st, false, burnguard.Decision{
		Direction: burnguard.DirectionHold, Changed: false,
	}, "")
	if st.seq != raised || st.countWithdrawals() != withdrawals {
		t.Fatal("a hold changed the store, want no raise and no withdrawal")
	}
	if st.seq != 1 {
		t.Fatalf("raises = %d, want the one seeded", st.seq)
	}
}

// TestBurnGuardAskOnChangeOneOpenAcrossRungChanges: with ask_on_change on,
// three consecutive rung changes leave at most one open guard question, and
// each previous one is withdrawn as superseded before its successor is raised.
func TestBurnGuardAskOnChangeOneOpenAcrossRungChanges(t *testing.T) {
	st := newFakeBurnGuardQuestionStore()
	running := " 4 running session(s) were left alone."
	decisions := []burnguard.Decision{
		{Direction: burnguard.DirectionClimb, Rung: burnguard.RungLowerInflight, Changed: true,
			QuestionText: "I did this: lowered the inflight cap of claude-code from 3 to 2."},
		{Direction: burnguard.DirectionClimb, Rung: burnguard.RungRoutePiPrompt, Changed: true,
			QuestionText: "I did this: routed new claude-code work to pi-prompt."},
		{Direction: burnguard.DirectionClimb, Rung: burnguard.RungHoldClaude, Changed: true,
			QuestionText: "I did this: holding new claude-code work; running sessions continue."},
	}
	var raised []Question
	for i, d := range decisions {
		ApplyBurnGuardAskOnChange(st, true, d, running)
		open := st.openGuard()
		if len(open) != 1 {
			t.Fatalf("after change %d, open guard questions = %d, want exactly one", i, len(open))
		}
		lastRaised := st.questions[open[0].ID]
		if !strings.Contains(lastRaised.Text, d.QuestionText) || !strings.Contains(lastRaised.Text, running) {
			t.Fatalf("open question after change %d = %q, want the rung's sentence plus the running note", i, lastRaised.Text)
		}
		if lastRaised.Default != "keep" || len(lastRaised.Options) != 2 {
			t.Fatalf("open question after change %d has no default or options: %+v", i, lastRaised)
		}
		raised = append(raised, lastRaised)
		if i > 0 {
			prev := raised[i-1]
			if st.state(prev.ID) != QuestionWithdrawn {
				t.Fatalf("previous question %s state = %s, want withdrawn", prev.ID, st.state(prev.ID))
			}
			ws := st.withdrawalsFor(prev.ID)
			if len(ws) != 1 || ws[0].reason != BurnGuardReasonSuperseded {
				t.Fatalf("withdrawals for %s = %+v, want one with the superseded reason", prev.ID, ws)
			}
		}
	}
	open := st.openGuard()
	if len(open) != 1 || !strings.Contains(open[0].Text, "holding new claude-code work") {
		t.Fatalf("the one open guard question = %v, want the latest rung's question", open)
	}
}

// TestBurnGuardAskOnChangeNoQuestionForNoOpOrRelease: a no-op step and a
// disabled release never raise a question, and withdraw none either.
func TestBurnGuardAskOnChangeNoQuestionForNoOpOrRelease(t *testing.T) {
	st := newFakeBurnGuardQuestionStore()

	ApplyBurnGuardAskOnChange(st, true, burnguard.Decision{
		Direction: burnguard.DirectionHold, Changed: false,
	}, "")
	ApplyBurnGuardAskOnChange(st, true, burnguard.Decision{
		Direction: burnguard.DirectionDisabled, Rung: burnguard.RungHoldClaude, Changed: true,
		AlertText: "The burn guard is off; every changed setting is restored.",
	}, "")
	if opened := st.openGuard(); len(opened) != 0 {
		t.Fatalf("open guard questions after a no-op and a release = %v, want none", opened)
	}
	if st.seq != 0 {
		t.Fatalf("raises after a no-op and a release = %d, want none", st.seq)
	}
	if n := st.countWithdrawals(); n != 0 {
		t.Fatalf("withdrawals after a no-op and a release = %d, want none", n)
	}
}

// TestBurnGuardAskOnChangeStartWithdrawsStaleKeepsCurrent: at start, an open
// guard question the state no longer requires (ladder at rung 0) is withdrawn
// with a reason, an open question the state still applies is kept, older open
// ones are withdrawn, and questions of other askers are never touched.
func TestBurnGuardAskOnChangeStartWithdrawsStaleKeepsCurrent(t *testing.T) {
	t.Run("ladder at rung 0 withdraws the stale question", func(t *testing.T) {
		st := newFakeBurnGuardQuestionStore()
		seed, err := st.Raise(BurnGuardQuestion("I did this: holding new claude-code work; running sessions continue."))
		if err != nil {
			t.Fatal(err)
		}
		other := Question{ID: "qw-1", AskedBy: "request-watch", State: QuestionOpen, Text: "an operator question of another asker"}
		st.mu.Lock()
		st.questions[other.ID] = other
		st.mu.Unlock()

		withdrawn := PruneBurnGuardQuestionsAtStart(st, burnguard.LadderState{})
		if len(withdrawn) != 1 || withdrawn[0].ID != seed.ID {
			t.Fatalf("withdrawn at start = %v, want the one guard question", withdrawn)
		}
		ws := st.withdrawalsFor(seed.ID)
		if len(ws) != 1 || ws[0].reason != BurnGuardReasonStaleAtStart || ws[0].actor != burnGuardQuestionActor {
			t.Fatalf("withdrawals for %s = %+v, want one by the guard with the stale-at-start reason", seed.ID, ws)
		}
		if opened := st.openGuard(); len(opened) != 0 {
			t.Fatalf("open guard questions after start = %v, want none", opened)
		}
		if st.state(other.ID) != QuestionOpen {
			t.Fatalf("another asker's question state = %s, want still open", st.state(other.ID))
		}
	})

	t.Run("a rung is applied so the current question is kept", func(t *testing.T) {
		st := newFakeBurnGuardQuestionStore()
		q, err := st.Raise(BurnGuardQuestion("I did this: holding new claude-code work; running sessions continue."))
		if err != nil {
			t.Fatal(err)
		}
		state := burnguard.LadderState{
			Applied: []string{burnguard.RungLowerInflight, burnguard.RungRoutePiPrompt, burnguard.RungHoldClaude},
		}
		if withdrawn := PruneBurnGuardQuestionsAtStart(st, state); len(withdrawn) != 0 {
			t.Fatalf("withdrawn at start = %v, want none", withdrawn)
		}
		if opened := st.openGuard(); len(opened) != 1 || opened[0].ID != q.ID {
			t.Fatalf("open guard questions after start = %v, want the current one kept", opened)
		}
	})

	t.Run("older open questions are withdrawn so one stays", func(t *testing.T) {
		st := newFakeBurnGuardQuestionStore()
		old, err := st.Raise(BurnGuardQuestion("I did this: routed new claude-code work to pi-prompt."))
		if err != nil {
			t.Fatal(err)
		}
		cur, err := st.Raise(BurnGuardQuestion("I did this: holding new claude-code work; running sessions continue."))
		if err != nil {
			t.Fatal(err)
		}
		state := burnguard.LadderState{
			Applied: []string{burnguard.RungLowerInflight, burnguard.RungRoutePiPrompt, burnguard.RungHoldClaude},
		}
		withdrawn := PruneBurnGuardQuestionsAtStart(st, state)
		if len(withdrawn) != 1 || withdrawn[0].ID != old.ID {
			t.Fatalf("withdrawn at start = %v, want the older question", withdrawn)
		}
		ws := st.withdrawalsFor(old.ID)
		if len(ws) != 1 || ws[0].reason != BurnGuardReasonStaleAtStart {
			t.Fatalf("withdrawals for %s = %+v, want one with the stale-at-start reason", old.ID, ws)
		}
		if opened := st.openGuard(); len(opened) != 1 || opened[0].ID != cur.ID {
			t.Fatalf("open guard questions after start = %v, want the current one kept", opened)
		}
	})
}
