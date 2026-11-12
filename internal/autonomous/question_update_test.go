package autonomous

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/dmz006/datawatch/internal/audit"
)

// readLines returns the non-empty lines of the store file.
func readLines(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var out []string
	for _, l := range strings.Split(string(data), "\n") {
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

func TestQuestionUpdate_ChangesTextAndOptions(t *testing.T) {
	_, s, _, _ := newQuestionStoreTest(t)
	id := mustRaise(t, s, sampleQuestion("Which way?"))

	newOpts := []QuestionOption{
		{ID: "first", Label: "Take the first road"},
		{ID: "third", Label: "Take the third road", Tradeoffs: "longest"},
	}
	got, err := s.Update(id, QuestionPatch{Text: strPtr("Which road?"), Options: newOpts}, "operator")
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got.Text != "Which road?" || !reflect.DeepEqual(got.Options, newOpts) {
		t.Fatalf("returned question not updated: %+v", got)
	}
	if got.State != QuestionOpen || got.ID != id || got.CreatedAt == nil {
		t.Fatalf("id, state or creation time changed: %+v", got)
	}
	stored, _ := s.Get(id)
	if stored.Text != "Which road?" || !reflect.DeepEqual(stored.Options, newOpts) {
		t.Fatalf("stored question not updated: %+v", stored)
	}
	// Fields the patch did not name are unchanged.
	if stored.Default != "first" || stored.Context != QuestionContextNone {
		t.Fatalf("untouched fields changed: %+v", stored)
	}
}

func TestQuestionUpdate_RefusedWhenNotOpen(t *testing.T) {
	_, s, _, _ := newQuestionStoreTest(t)
	answeredID := mustRaise(t, s, sampleQuestion("answered one"))
	if _, err := s.Answer(answeredID, Answer{ChosenOptionID: "first", Actor: "op"}); err != nil {
		t.Fatalf("Answer: %v", err)
	}
	withdrawnID := mustRaise(t, s, sampleQuestion("withdrawn one"))
	if _, err := s.Withdraw(withdrawnID, "op", "no longer needed"); err != nil {
		t.Fatalf("Withdraw: %v", err)
	}
	for name, id := range map[string]string{"answered": answeredID, "withdrawn": withdrawnID} {
		before, _ := s.Get(id)
		_, err := s.Update(id, QuestionPatch{Text: strPtr("changed")}, "op")
		if !errors.Is(err, ErrQuestionNotOpen) {
			t.Errorf("%s: err = %v, want ErrQuestionNotOpen", name, err)
		}
		after, _ := s.Get(id)
		if !reflect.DeepEqual(before, after) {
			t.Errorf("%s: question changed by a refused update", name)
		}
	}
	if _, err := s.Update("q-missing", QuestionPatch{Text: strPtr("x")}, "op"); !errors.Is(err, ErrQuestionNotFound) {
		t.Errorf("unknown id: err = %v, want ErrQuestionNotFound", err)
	}
}

func TestQuestionUpdate_InvalidPatchLeavesQuestionUnchanged(t *testing.T) {
	path, s, rec, _ := newQuestionStoreTest(t)
	id := mustRaise(t, s, sampleQuestion("keep me"))
	before, _ := s.Get(id)
	linesBefore := readLines(t, path)

	cases := map[string]QuestionPatch{
		"option without a label": {Options: []QuestionOption{
			{ID: "first", Label: "ok"}, {ID: "second", Label: ""},
		}},
		"default names no option": {Default: strPtr("nowhere")},
		"empty text":              {Text: strPtr("   ")},
	}
	for name, patch := range cases {
		_, err := s.Update(id, patch, "op")
		if !errors.Is(err, ErrInvalidQuestion) {
			t.Errorf("%s: err = %v, want ErrInvalidQuestion", name, err)
		}
		after, _ := s.Get(id)
		if !reflect.DeepEqual(before, after) {
			t.Errorf("%s: stored question changed: %+v", name, after)
		}
	}
	if got := readLines(t, path); len(got) != len(linesBefore) {
		t.Errorf("refused updates appended lines: %d -> %d", len(linesBefore), len(got))
	}
	actions, _ := rec.snapshot()
	for _, a := range actions {
		if a == audit.ActionQuestionUpdated {
			t.Errorf("refused update wrote an audit entry")
		}
	}
}

func TestQuestionUpdate_SurvivesReopen(t *testing.T) {
	path, s, _, _ := newQuestionStoreTest(t)
	id := mustRaise(t, s, sampleQuestion("before"))
	newOpts := []QuestionOption{{ID: "a", Label: "Alpha"}, {ID: "b", Label: "Beta"}}
	if _, err := s.Update(id, QuestionPatch{
		Text:           strPtr("after"),
		Options:        newOpts,
		Default:        strPtr("hold"),
		Recommendation: &QuestionRecommendation{OptionID: "b", Rationale: "safer"},
	}, "op"); err != nil {
		t.Fatalf("Update: %v", err)
	}
	// A later answer must still apply on top of the replayed update.
	if _, err := s.Answer(id, Answer{ChosenOptionID: "a", Actor: "op"}); err != nil {
		t.Fatalf("Answer: %v", err)
	}

	reopened, err := NewQuestionStore(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	got, ok := reopened.Get(id)
	if !ok {
		t.Fatal("question missing after reopen")
	}
	if got.Text != "after" || !reflect.DeepEqual(got.Options, newOpts) || got.Default != "hold" {
		t.Fatalf("update lost on reopen: %+v", got)
	}
	if got.Recommendation == nil || got.Recommendation.OptionID != "b" {
		t.Fatalf("recommendation lost on reopen: %+v", got.Recommendation)
	}
	if got.State != QuestionAnswered {
		t.Fatalf("state after reopen = %q, want answered", got.State)
	}
}

func TestQuestionUpdate_AuditAndChangeHook(t *testing.T) {
	_, s, rec, _ := newQuestionStoreTest(t)
	var mu sync.Mutex
	var changes []QuestionChange
	s.SetChangeHook(func(c QuestionChange) {
		mu.Lock()
		changes = append(changes, c)
		mu.Unlock()
	})
	id := mustRaise(t, s, sampleQuestion("audit me"))

	if _, err := s.Update(id, QuestionPatch{Text: strPtr("audited")}, "operator"); err != nil {
		t.Fatalf("Update: %v", err)
	}
	actions, details := rec.snapshot()
	count := 0
	for i, a := range actions {
		if a != audit.ActionQuestionUpdated {
			continue
		}
		count++
		if details[i]["question_id"] != id || details[i]["actor"] != "operator" || details[i]["changed"] != "text" {
			t.Errorf("audit details = %v", details[i])
		}
	}
	if count != 1 {
		t.Fatalf("question_updated audit entries = %d, want 1 (actions %v)", count, actions)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(changes) != 2 || changes[1].Kind != QuestionChangeUpdated {
		t.Fatalf("changes = %+v, want raised then updated", changes)
	}
	if changes[1].Question.Text != "audited" || changes[1].Actor != "operator" {
		t.Fatalf("updated change = %+v", changes[1])
	}
}

func TestQuestionUpdate_NoChangeWritesNothing(t *testing.T) {
	path, s, rec, _ := newQuestionStoreTest(t)
	id := mustRaise(t, s, sampleQuestion("same"))
	linesBefore := readLines(t, path)
	if _, err := s.Update(id, QuestionPatch{Text: strPtr("same")}, "op"); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got := readLines(t, path); len(got) != len(linesBefore) {
		t.Errorf("no-op update appended a line")
	}
	actions, _ := rec.snapshot()
	for _, a := range actions {
		if a == audit.ActionQuestionUpdated {
			t.Errorf("no-op update wrote an audit entry")
		}
	}
}

func TestQuestionUpdate_RaiseUnlessDecisionRecord(t *testing.T) {
	path, s, _, _ := newQuestionStoreTest(t)
	q := sampleQuestion("Decide the record")
	q.DecisionRecordID = "dr-0123456789abcdef0123"

	first, raised, err := s.RaiseUnlessDecisionRecord(q)
	if err != nil || !raised {
		t.Fatalf("first call: raised=%v err=%v", raised, err)
	}
	second, raised, err := s.RaiseUnlessDecisionRecord(q)
	if err != nil || raised {
		t.Fatalf("second call: raised=%v err=%v, want raised=false", raised, err)
	}
	if second.ID != first.ID {
		t.Fatalf("second call returned %s, want %s", second.ID, first.ID)
	}

	// Still one question after a reopen, and the answered one still counts.
	reopened, err := NewQuestionStore(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	third, raised, err := reopened.RaiseUnlessDecisionRecord(q)
	if err != nil || raised || third.ID != first.ID {
		t.Fatalf("after reopen: id=%s raised=%v err=%v, want %s false", third.ID, raised, err, first.ID)
	}
	if _, err := reopened.Answer(first.ID, Answer{ChosenOptionID: "first", Actor: "op"}); err != nil {
		t.Fatalf("Answer: %v", err)
	}
	fourth, raised, err := reopened.RaiseUnlessDecisionRecord(q)
	if err != nil || raised || fourth.ID != first.ID {
		t.Fatalf("answered: id=%s raised=%v err=%v", fourth.ID, raised, err)
	}
	if n := len(reopened.List(QuestionFilter{})); n != 1 {
		t.Fatalf("questions in store = %d, want 1", n)
	}
}

func TestQuestionUpdate_DecisionRecordIDValidation(t *testing.T) {
	_, s, _, _ := newQuestionStoreTest(t)
	good := sampleQuestion("linked")
	good.DecisionRecordID = "dr-abc123"
	if _, err := s.Raise(good); err != nil {
		t.Fatalf("a dr- id was refused: %v", err)
	}
	for _, bad := range []string{"abc", "dr-", "q-123"} {
		q := sampleQuestion("bad link")
		q.DecisionRecordID = bad
		if _, err := s.Raise(q); !errors.Is(err, ErrInvalidQuestion) {
			t.Errorf("id %q: err = %v, want ErrInvalidQuestion", bad, err)
		}
	}
}
