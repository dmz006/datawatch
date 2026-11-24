package autonomous

// Seeding the attempt count of tasks that ran before the attempt cap existed.
// Each task's count is set from its recorded attempt history (completed tasks
// too); an unfinished task over the cap is held, and the operator gets ONE
// question per Automaton that lists its held tasks. Every test runs on fakes
// and a temp data directory.

import (
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// seedHistory returns n recorded attempts, the way a task that ran a long time
// before the cap existed carries them.
func seedHistory(n int) []TaskAttempt {
	out := make([]TaskAttempt, n)
	for i := range out {
		out[i] = TaskAttempt{Number: i + 1, Outcome: attemptOutcomeFailed, StartedAt: time.Now().Add(-time.Hour), EndedAt: time.Now()}
	}
	return out
}

// addSeedPRD stores one approved Automaton whose tasks carry the given recorded
// attempts and statuses (one task per entry), counts left at zero as an old
// record has them.
func (r *capRig) addSeedPRD(t *testing.T, tasks []seedTask) *PRD {
	t.Helper()
	prd, err := r.m.CreatePRD("seed spec", t.TempDir(), "opencode", "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	var ts []Task
	for _, s := range tasks {
		ts = append(ts, Task{Title: s.title, Spec: "do " + s.title, Status: s.status, Attempts: seedHistory(s.attempts)})
	}
	if err := r.m.Store().SetStories(prd.ID, []Story{{Title: "Story", Tasks: ts}}); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	prd, _ = r.m.Store().GetPRD(prd.ID)
	prd.Status = PRDRunning
	if err := r.m.Store().SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	return prd
}

type seedTask struct {
	title    string
	status   TaskStatus
	attempts int
}

func seededTask(t *testing.T, m *Manager, prdID, title string) *Task {
	t.Helper()
	prd, ok := m.Store().GetPRD(prdID)
	if !ok {
		t.Fatalf("PRD %s not found", prdID)
	}
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			if prd.Story[si].Tasks[ti].Title == title {
				return &prd.Story[si].Tasks[ti]
			}
		}
	}
	t.Fatalf("task %q not found", title)
	return nil
}

func openSeedQuestions(r *capRig, prdID string) []Question {
	return r.store.List(QuestionFilter{State: QuestionOpen, Asker: attemptCapSeedQuestionAsker, PRDID: prdID, Limit: 50})
}

// TestAttemptSeed_OverCapHeldWithOneGroupedQuestion: a task with 71 recorded
// attempts and an unfinished status is held at the first check, however many
// tasks of the Automaton are held, with ONE question that lists them; a
// completed task with 71 is not held; every count equals its history.
func TestAttemptSeed_OverCapHeldWithOneGroupedQuestion(t *testing.T) {
	r := newCapRig(t, t.TempDir(), noBackoff(6))
	prd := r.addSeedPRD(t, []seedTask{
		{"Pending over", TaskPending, 71},
		{"Failed over", TaskFailed, 71},
		{"Done over", TaskCompleted, 71},
		{"Pending under", TaskPending, 2},
	})

	rep := r.m.SeedAttemptCounts()

	for title, want := range map[string]int{"Pending over": 71, "Failed over": 71, "Done over": 71, "Pending under": 2} {
		if got := seededTask(t, r.m, prd.ID, title).AttemptsTotal; got != want {
			t.Errorf("%s: attempts_total = %d, want the recorded history %d", title, got, want)
		}
	}
	for _, title := range []string{"Pending over", "Failed over"} {
		got := seededTask(t, r.m, prd.ID, title)
		if got.Status != TaskReviewNeeded || !got.AttemptCapHeld {
			t.Errorf("%s: status %q held %v, want held at the cap", title, got.Status, got.AttemptCapHeld)
		}
	}
	if got := seededTask(t, r.m, prd.ID, "Done over"); got.Status != TaskCompleted || got.AttemptCapHeld {
		t.Errorf("a completed task was touched: status %q held %v", got.Status, got.AttemptCapHeld)
	}
	if got := seededTask(t, r.m, prd.ID, "Pending under"); got.Status != TaskPending || got.AttemptCapHeld {
		t.Errorf("a task under the cap was held: status %q held %v", got.Status, got.AttemptCapHeld)
	}

	open := openSeedQuestions(r, prd.ID)
	if len(open) != 1 {
		t.Fatalf("open questions for the Automaton = %d, want exactly one", len(open))
	}
	for _, title := range []string{"Pending over", "Failed over"} {
		if !strings.Contains(open[0].Text, title) {
			t.Errorf("the grouped question does not list %q: %s", title, open[0].Text)
		}
	}
	for _, title := range []string{"Done over", "Pending under"} {
		if strings.Contains(open[0].Text, title) {
			t.Errorf("the grouped question lists %q, which is not held: %s", title, open[0].Text)
		}
	}
	if n := len(openCapQuestions(r, prd.ID)); n != 0 {
		t.Errorf("per-task cap questions = %d, want none: the grouped question replaces them", n)
	}
	if n := len(alertsTitled(&r.alerts, attemptCapSeedAlertTitle)); n != 1 {
		t.Errorf("seed alerts = %d, want one per Automaton", n)
	}
	if rep.Seeded != 4 || rep.HeldTasks != 2 || rep.HeldAutomata != 1 {
		t.Errorf("report = %+v, want 4 seeded, 2 held tasks, 1 held Automaton", rep)
	}
}

func openCapQuestions(r *capRig, prdID string) []Question {
	return r.store.List(QuestionFilter{State: QuestionOpen, Asker: attemptCapQuestionAsker, PRDID: prdID, Limit: 50})
}

// TestAttemptSeed_OneQuestionPerAutomaton: two Automata, two questions.
func TestAttemptSeed_OneQuestionPerAutomaton(t *testing.T) {
	r := newCapRig(t, t.TempDir(), noBackoff(6))
	a := r.addSeedPRD(t, []seedTask{{"A1", TaskPending, 10}, {"A2", TaskBlocked, 9}})
	b := r.addSeedPRD(t, []seedTask{{"B1", TaskPending, 7}})
	clean := r.addSeedPRD(t, []seedTask{{"C1", TaskPending, 3}})

	rep := r.m.SeedAttemptCounts()

	if n := len(openSeedQuestions(r, a.ID)); n != 1 {
		t.Errorf("Automaton A questions = %d, want 1", n)
	}
	if n := len(openSeedQuestions(r, b.ID)); n != 1 {
		t.Errorf("Automaton B questions = %d, want 1", n)
	}
	if n := len(openSeedQuestions(r, clean.ID)); n != 0 {
		t.Errorf("a clean Automaton got %d questions, want none", n)
	}
	if rep.HeldTasks != 3 || rep.HeldAutomata != 2 {
		t.Errorf("report = %+v, want 3 held tasks in 2 Automata", rep)
	}
}

// TestAttemptSeed_RunsOnce: the sweep is one-shot, so an operator's force
// reset (count back to zero, history kept) is not undone at the next start.
func TestAttemptSeed_RunsOnce(t *testing.T) {
	dir := t.TempDir()
	r := newCapRig(t, dir, noBackoff(6))
	prd := r.addSeedPRD(t, []seedTask{{"Old", TaskPending, 71}})
	r.m.SeedAttemptCounts()
	id := seededTask(t, r.m, prd.ID, "Old").ID
	if _, err := r.m.ResetTaskWithOptions(prd.ID, id, "operator", true, false); err != nil {
		t.Fatalf("operator force reset: %v", err)
	}
	if got := seededTask(t, r.m, prd.ID, "Old"); got.AttemptsTotal != 0 || got.AttemptCapHeld {
		t.Fatalf("setup: after the force reset attempts_total %d held %v", got.AttemptsTotal, got.AttemptCapHeld)
	}

	if n := len(openSeedQuestions(r, prd.ID)); n != 0 {
		t.Fatalf("the grouped question is still open after its only held task was force-reset: %d", n)
	}

	// The same data directory opened again is a restart.
	again := newCapRig(t, dir, noBackoff(6))
	rep := again.m.SeedAttemptCounts()
	if rep.Ran {
		t.Errorf("the sweep ran again after a restart: %+v", rep)
	}
	if got := seededTask(t, again.m, prd.ID, "Old"); got.AttemptsTotal != 0 || got.AttemptCapHeld || got.Status == TaskReviewNeeded {
		t.Errorf("a restart undid the operator's reset: attempts_total %d held %v status %q", got.AttemptsTotal, got.AttemptCapHeld, got.Status)
	}
}

// TestAttemptSeed_CapOffSeedsButHoldsNothing: with the cap off the counts are
// still made accurate, and no task is held.
func TestAttemptSeed_CapOffSeedsButHoldsNothing(t *testing.T) {
	off := false
	r := newCapRig(t, t.TempDir(), config.AttemptCapConfig{Enabled: &off, MaxTotal: 6})
	prd := r.addSeedPRD(t, []seedTask{{"Old", TaskPending, 71}})
	r.m.SeedAttemptCounts()
	got := seededTask(t, r.m, prd.ID, "Old")
	if got.AttemptsTotal != 71 || got.AttemptCapHeld || got.Status != TaskPending {
		t.Fatalf("cap off: attempts_total %d held %v status %q, want 71, not held, pending", got.AttemptsTotal, got.AttemptCapHeld, got.Status)
	}
	if n := len(openSeedQuestions(r, prd.ID)); n != 0 {
		t.Fatalf("cap off raised %d questions", n)
	}
}

// TestAttemptSeed_AnswerClearsForOperatorOnly: the grouped question's answer
// force-resets every task it lists when the operator gives it; a session's
// answer clears nothing.
func TestAttemptSeed_AnswerClearsForOperatorOnly(t *testing.T) {
	r := newCapRig(t, t.TempDir(), noBackoff(6))
	prd := r.addSeedPRD(t, []seedTask{{"One", TaskPending, 71}, {"Two", TaskPending, 20}})
	r.m.SeedAttemptCounts()
	open := openSeedQuestions(r, prd.ID)
	if len(open) != 1 {
		t.Fatalf("setup: want one grouped question, got %d", len(open))
	}
	change := func(actor, option string) QuestionChange {
		return QuestionChange{Kind: QuestionChangeAnswered, Question: open[0],
			Answer: &Answer{ChosenOptionID: option, Actor: actor}}
	}
	api := r.api

	if !api.resolveAttemptCapAnswer(change("session:abc", attemptCapActionForceReset)) {
		t.Fatalf("the grouped answer was not recognised")
	}
	if got := seededTask(t, r.m, prd.ID, "One"); got.AttemptsTotal != 71 || !got.AttemptCapHeld {
		t.Fatalf("a session's answer cleared a task: attempts_total %d held %v", got.AttemptsTotal, got.AttemptCapHeld)
	}
	api.resolveAttemptCapAnswer(change("operator", attemptCapActionKeepHeld))
	if got := seededTask(t, r.m, prd.ID, "One"); !got.AttemptCapHeld {
		t.Fatalf("keep held released a task")
	}
	api.resolveAttemptCapAnswer(change("operator", attemptCapActionForceReset))
	for _, title := range []string{"One", "Two"} {
		if got := seededTask(t, r.m, prd.ID, title); got.AttemptsTotal != 0 || got.AttemptCapHeld {
			t.Errorf("%s after the operator's answer: attempts_total %d held %v, want cleared", title, got.AttemptsTotal, got.AttemptCapHeld)
		}
	}
}
