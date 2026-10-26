package autonomous

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeAuditRecorder records the audit entries a store emits, in order.
type fakeAuditRecorder struct {
	mu      sync.Mutex
	actions []string
	details []map[string]any
}

func (r *fakeAuditRecorder) Record(action string, details map[string]any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.actions = append(r.actions, action)
	r.details = append(r.details, details)
}

func (r *fakeAuditRecorder) snapshot() (actions []string, details []map[string]any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.actions...), append([]map[string]any(nil), r.details...)
}

// newQuestionStoreTest builds a store on a temp file with a frozen clock
// (advanced by callers) and a deterministic, collision-free id sequence.
func newQuestionStoreTest(t *testing.T) (path string, s *QuestionStore, rec *fakeAuditRecorder, advance func(d time.Duration)) {
	t.Helper()
	path = filepath.Join(t.TempDir(), "questions.jsonl")
	store, err := NewQuestionStore(path)
	if err != nil {
		t.Fatalf("NewQuestionStore: %v", err)
	}
	ts := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return ts }
	advance = func(d time.Duration) { ts = ts.Add(d) }
	qi, ai := 0, 0
	store.newQuestionID = func(time.Time) string { qi++; return fmt.Sprintf("q-%08d", qi) }
	store.newAnswerID = func(time.Time) string { ai++; return fmt.Sprintf("a-%08d", ai) }
	rec = &fakeAuditRecorder{}
	store.SetAuditFn(rec.Record)
	return path, store, rec, advance
}

// sampleQuestion returns a question that passes validation.
func sampleQuestion(text string) Question {
	return Question{
		Text:    text,
		Context: QuestionContextNone,
		Default: "first",
		Options: []QuestionOption{
			{ID: "first", Label: "Do the first option", Tradeoffs: "faster, less thorough"},
			{ID: "second", Label: "Do the second option", Tradeoffs: "slower, more thorough"},
		},
	}
}

// mustRaise raises a question or fails the test, and returns its id.
func mustRaise(t *testing.T, s *QuestionStore, q Question) string {
	t.Helper()
	got, err := s.Raise(q)
	if err != nil {
		t.Fatalf("Raise: %v", err)
	}
	if got.ID == "" {
		t.Fatalf("Raise returned an empty id")
	}
	return got.ID
}

// auditKeysSorted returns the detail-entry key set of one recorded entry.
func auditKeysSorted(d map[string]any) []string {
	keys := make([]string, 0, len(d))
	for k := range d {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func TestQuestionStore_RaiseAndGet(t *testing.T) {
	_, s, rec, advance := newQuestionStoreTest(t)

	id := mustRaise(t, s, sampleQuestion("Ship the new default?"))
	if !strings.HasPrefix(id, "q-") {
		t.Errorf("question id %q does not have the q- prefix", id)
	}
	q, ok := s.Get(id)
	if !ok {
		t.Fatalf("Get found no question")
	}
	if q.State != QuestionOpen {
		t.Errorf("state = %q; want open", q.State)
	}
	wantCreated := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	if !q.CreatedAt.Equal(wantCreated) {
		t.Errorf("CreatedAt = %v; want the frozen clock time %v", q.CreatedAt, wantCreated)
	}
	if q.Label != DefaultQuestionLabels[0] {
		t.Errorf("label = %q; want the default %q", q.Label, DefaultQuestionLabels[0])
	}
	if q.Risk != QuestionRiskNormal {
		t.Errorf("risk = %q; want %q", q.Risk, QuestionRiskNormal)
	}
	if _, ok := s.Get("q-does not exist"); ok {
		t.Errorf("Get found a question the store does not hold")
	}

	actions, details := rec.snapshot()
	if len(actions) != 1 || actions[0] != "question_raised" {
		t.Fatalf("audit actions = %v; want one question_raised", actions)
	}
	wantKeys := []string{"asked_by", "asker_kind", "context", "default", "label", "option_count", "question_id", "risk", "text"}
	if got := auditKeysSorted(details[0]); !reflect.DeepEqual(got, wantKeys) {
		t.Errorf("raised audit keys = %v; want %v", got, wantKeys)
	}

	// Advance the clock: a second question is ordered later in List.
	advance(time.Hour)
	mustRaise(t, s, sampleQuestion("Pick a follow-up option?"))
	list := s.List(QuestionFilter{})
	if len(list) != 2 {
		t.Fatalf("List returned %d questions; want 2", len(list))
	}
	if list[0].Text != "Pick a follow-up option?" {
		t.Errorf("List order = %q first; want newest first", list[0].Text)
	}
}

func TestQuestionStore_ListFilters(t *testing.T) {
	_, s, _, advance := newQuestionStoreTest(t)

	// Three questions differing in state, context and asker.
	a := sampleQuestion("Alpha question for the registry row?")
	a.Context = QuestionContextRegistryRow
	a.RegistryRowKey = "row 12"
	a.AskedBy = "supervisor"
	a.AskerKind = QuestionAskerSupervisor
	idA := mustRaise(t, s, a)

	advance(time.Second)
	b := sampleQuestion("Beta question for the task?")
	b.Context = QuestionContextTask
	b.TaskID = "task 7"
	b.AskedBy = "session"
	b.AskerKind = QuestionAskerSession
	idB := mustRaise(t, s, b)

	advance(time.Second)
	c := sampleQuestion("Gamma question with no context?")
	c.AskedBy = "operator"
	c.AskerKind = QuestionAskerOperator
	idC := mustRaise(t, s, c)

	if got := s.List(QuestionFilter{State: QuestionOpen}); len(got) != 3 {
		t.Errorf("state=open list = %d; want 3", len(got))
	}
	if got := s.List(QuestionFilter{State: QuestionWithdrawn}); len(got) != 0 {
		t.Errorf("state=withdrawn list = %d; want 0", len(got))
	}
	if got := s.List(QuestionFilter{Context: QuestionContextTask}); len(got) != 1 || got[0].ID != idB {
		t.Errorf("context=task list = %v; want only %s", questionIDs(got), idB)
	}
	if got := s.List(QuestionFilter{Asker: "operator"}); len(got) != 1 || got[0].ID != idC {
		t.Errorf("asker=operator list = %v; want only %s", questionIDs(got), idC)
	}
	if got := s.List(QuestionFilter{Asker: "operator-ish"}); len(got) != 0 {
		t.Errorf("asker exact-match list = %v; want empty", questionIDs(got))
	}

	// Text matches the question text, an option label, the tradeoffs, and
	// (later) an answer quote. Case-insensitive.
	if got := s.List(QuestionFilter{Text: "ALPHA"}); len(got) != 1 || got[0].ID != idA {
		t.Errorf("text=ALPHA list = %v; want only %s", questionIDs(got), idA)
	}
	if got := s.List(QuestionFilter{Text: "more thorough"}); len(got) != 3 {
		t.Errorf("text in tradeoffs list = %d; want 3 (all carry the option)", len(got))
	}

	// An answer quote counts as searchable text.
	if _, err := s.Answer(idC, Answer{Quote: "take the gamma channel quote", Actor: "operator"}); err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if got := s.List(QuestionFilter{Text: "gamma channel quote"}); len(got) != 1 || got[0].ID != idC {
		t.Errorf("text-in-answer-quote list = %v; want only %s", questionIDs(got), idC)
	}
	if got := s.List(QuestionFilter{Text: "not any text anywhere"}); len(got) != 0 {
		t.Errorf("no-match text list = %v; want empty", questionIDs(got))
	}

	// Limit: an explicit limit caps the result, and the default limit
	// (100) applies when the caller asks for none.
	if got := s.List(QuestionFilter{Limit: 2}); len(got) != 2 {
		t.Errorf("Limit=2 list = %d; want 2", len(got))
	}
}

func TestQuestionStore_ListLimits(t *testing.T) {
	_, s, _, advance := newQuestionStoreTest(t)
	for i := 0; i < 150; i++ {
		advance(time.Microsecond)
		if _, err := s.Raise(sampleQuestion(fmt.Sprintf("limit probe question number %d?", i))); err != nil {
			t.Fatalf("Raise %d: %v", i, err)
		}
	}
	if got := s.List(QuestionFilter{}); len(got) != 100 {
		t.Errorf("default-limit list = %d; want 100", len(got))
	}
	if got := s.List(QuestionFilter{Limit: 5000}); len(got) != 150 {
		t.Errorf("Limit=5000 list = %d; want all 150 (limits are capped at the maximum 1000, not 100)", len(got))
	}
	if got := s.List(QuestionFilter{Limit: 10}); len(got) != 10 {
		t.Errorf("Limit=10 list = %d; want 10", len(got))
	}
}

// questionIDs is a test helper that renders a list of question ids. that renders a list of question ids.
func questionIDs(qs []Question) []string {
	out := make([]string, 0, len(qs))
	for _, q := range qs {
		out = append(out, q.ID)
	}
	return out
}

func TestQuestionStore_AnswerShapes(t *testing.T) {
	_, s, rec, _ := newQuestionStoreTest(t)

	// An option-only answer, a text-only answer and a both answer are all
	// valid shapes.
	idOpt := mustRaise(t, s, sampleQuestion("Option only?"))
	aOpt, err := s.Answer(idOpt, Answer{ChosenOptionID: "second", Actor: "operator", Surface: "cli"})
	if err != nil {
		t.Fatalf("option-only answer: %v", err)
	}
	if aOpt.Quote != "" || aOpt.QuoteSHA256 != "" {
		t.Errorf("option-only answer stored quote %q or hash %q; want empty", aOpt.Quote, aOpt.QuoteSHA256)
	}
	if aOpt.ChosenOptionID != "second" {
		t.Errorf("option-only answer stored option %q; want second", aOpt.ChosenOptionID)
	}

	idText := mustRaise(t, s, sampleQuestion("Text only?"))
	q := "go with the plain text route"
	aText, err := s.Answer(idText, Answer{Quote: q, Actor: "operator", Surface: "rest"})
	if err != nil {
		t.Fatalf("text-only answer: %v", err)
	}
	if aText.Quote != q || aText.ChosenOptionID != "" {
		t.Errorf("text-only answer = %+v; want the quote and no option", aText)
	}
	if aText.QuoteSHA256 != QuoteSHA256(q) {
		t.Errorf("text-only answer hash = %q; want %q", aText.QuoteSHA256, QuoteSHA256(q))
	}

	idBoth := mustRaise(t, s, sampleQuestion("Both?"))
	bothQuote := "second, because the tradeoffs say so"
	aBoth, err := s.Answer(idBoth, Answer{ChosenOptionID: "second", Quote: bothQuote, Actor: "operator"})
	if err != nil {
		t.Fatalf("both answer: %v", err)
	}
	if aBoth.ChosenOptionID != "second" || aBoth.Quote != bothQuote {
		t.Errorf("both answer = %+v; want option and quote", aBoth)
	}

	// The three raises and three answers each emit their audit actions.
	actions, details := rec.snapshot()
	wantActions := []string{"question_raised", "question_answered", "question_raised", "question_answered", "question_raised", "question_answered"}
	if !reflect.DeepEqual(actions, wantActions) {
		t.Fatalf("audit actions = %v; want %v", actions, wantActions)
	}
	for _, idx := range []int{1, 3, 5} {
		wantKeys := []string{"actor", "answer_id", "chosen_option_id", "question_id", "quote", "quote_sha256", "surface"}
		if got := auditKeysSorted(details[idx]); !reflect.DeepEqual(got, wantKeys) {
			t.Errorf("answer audit keys %d = %v; want %v", idx, got, wantKeys)
		}
	}

	// An empty answer (no option, blank quote) is refused by the state
	// rules, and the question stays open.
	idEmpty := mustRaise(t, s, sampleQuestion("Empty?"))
	if _, err := s.Answer(idEmpty, Answer{Quote: "   "}); !errors.Is(err, ErrAnswerEmpty) {
		t.Errorf("empty answer error = %v; want ErrAnswerEmpty", err)
	}
	if q, _ := s.Get(idEmpty); q.State != QuestionOpen {
		t.Errorf("question state after a refused empty answer = %q; want open", q.State)
	}
	if s.AnswerCount(idEmpty) != 0 {
		t.Errorf("answer count after a refused empty answer = %d; want 0", s.AnswerCount(idEmpty))
	}
}

func TestQuestionStore_SupersedeAndFileOnlyGrows(t *testing.T) {
	path, s, rec, _ := newQuestionStoreTest(t)
	id := mustRaise(t, s, sampleQuestion("Which route?"))

	first, err := s.Answer(id, Answer{ChosenOptionID: "first", Quote: "first route, please", Actor: "operator"})
	if err != nil {
		t.Fatalf("first answer: %v", err)
	}

	// An answer to an answered question without Supersedes is refused...
	if _, err := s.Answer(id, Answer{ChosenOptionID: "second"}); !errors.Is(err, ErrAnswerNeedsSupersedes) {
		t.Fatalf("second answer without supersedes: %v; want ErrAnswerNeedsSupersedes", err)
	}
	// ...with the wrong target it is refused too...
	if _, err := s.Answer(id, Answer{Quote: "changed mind", Supersedes: "a-wrong"}); !errors.Is(err, ErrSupersedesWrongTarget) {
		t.Fatalf("second answer with the wrong target: %v; want ErrSupersedesWrongTarget", err)
	}
	// ...and with the right target it is accepted, the question staying answered.
	second, err := s.Answer(id, Answer{ChosenOptionID: "second", Quote: "changed mind: second", Actor: "operator", Supersedes: first.ID})
	if err != nil {
		t.Fatalf("superseding answer: %v", err)
	}
	if q, _ := s.Get(id); q.State != QuestionAnswered {
		t.Errorf("question state after supersede = %q; want answered", q.State)
	}

	// The old answer line is byte-identical on disk afterwards: the file
	// only grew, so the earlier bytes are a prefix of the later bytes.
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the store file: %v", err)
	}
	if _, err := s.Answer(id, Answer{Quote: "and back again", Actor: "operator", Supersedes: second.ID}); err != nil {
		t.Fatalf("third answer: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the store file again: %v", err)
	}
	if len(after) <= len(before) || !bytes.HasPrefix(after, before) {
		t.Errorf("the store file changed earlier bytes; it must only grow")
	}

	// Exactly the right audit actions were emitted, in order.
	actions, details := rec.snapshot()
	want := []string{"question_raised", "question_answered", "answer_superseded", "answer_superseded"}
	if !reflect.DeepEqual(actions, want) {
		t.Fatalf("audit actions = %v; want %v", actions, want)
	}
	if details[2]["supersedes"] != first.ID {
		t.Errorf("first supersede entry's supersedes = %v; want %s", details[2]["supersedes"], first.ID)
	}
	if details[3]["supersedes"] != second.ID {
		t.Fatalf("second supersede entry's supersedes = %v; want %s", details[3]["supersedes"], second.ID)
	}
	wantKeys := []string{"actor", "answer_id", "chosen_option_id", "question_id", "quote", "quote_sha256", "supersedes", "surface"}
	if got := auditKeysSorted(details[2]); !reflect.DeepEqual(got, wantKeys) {
		t.Errorf("supersede audit keys = %v; want %v", got, wantKeys)
	}

	// The derived flags: the first two answers are superseded, the new one is not.
	views, ok := s.Answers(id)
	if !ok || len(views) != 3 {
		t.Fatalf("Answers = %d entries (ok=%v); want 3", len(views), ok)
	}
	if !views[0].Superseded || views[0].SupersededBy != second.ID {
		t.Errorf("first answer view = superseded=%v by=%q; want superseded by %s", views[0].Superseded, views[0].SupersededBy, second.ID)
	}
	if !views[1].Superseded || views[1].SupersededBy != views[2].Answer.ID {
		t.Errorf("second answer view = superseded=%v by=%q; want superseded by the third", views[1].Superseded, views[1].SupersededBy)
	}
	if views[2].Superseded {
		t.Errorf("third answer view = superseded; want current")
	}
	cur, ok := s.CurrentAnswer(id)
	if !ok || cur.ID != views[2].Answer.ID {
		t.Errorf("CurrentAnswer.ID = %q (ok=%v); want %s", cur.ID, ok, views[2].Answer.ID)
	}
	if views[0].Answer.Quote != "first route, please" {
		t.Errorf("stored first quote = %q; the recorded answer was never edited", views[0].Answer.Quote)
	}
}

func TestQuestionStore_ReopenPreservesViews(t *testing.T) {
	path, s, _, _ := newQuestionStoreTest(t)

	id1 := mustRaise(t, s, sampleQuestion("First question?"))
	a1, err := s.Answer(id1, Answer{ChosenOptionID: "first", Actor: "operator"})
	if err != nil {
		t.Fatalf("answer 1: %v", err)
	}
	a2, err := s.Answer(id1, Answer{Quote: "supersede it", Actor: "operator", Supersedes: a1.ID})
	if err != nil {
		t.Fatalf("answer 2: %v", err)
	}
	id2 := mustRaise(t, s, sampleQuestion("Second question?"))
	if _, err := s.Withdraw(id2, "supervisor", "obsolete"); err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	id3 := mustRaise(t, s, sampleQuestion("Third question?"))
	if _, err := s.Expire(id3, "skipped by the supervisor"); err != nil {
		t.Fatalf("expire: %v", err)
	}
	id4 := mustRaise(t, s, sampleQuestion("Fourth question stays open?"))

	// Rebuild the store from the file.
	reopened, err := NewQuestionStore(path)
	if err != nil {
		t.Fatalf("re-open the store: %v", err)
	}

	q1, ok := reopened.Get(id1)
	if !ok || q1.State != QuestionAnswered || q1.Text != "First question?" {
		t.Errorf("reopened Get(%s) = %+v (ok=%v); want answered", id1, q1, ok)
	}
	if q4, ok := reopened.Get(id4); !ok || q4.State != QuestionOpen {
		t.Errorf("reopened Get(%s) = %+v (ok=%v); want open", id4, q4, ok)
	}

	origList := s.List(QuestionFilter{Limit: 1000})
	newList := reopened.List(QuestionFilter{Limit: 1000})
	if !reflect.DeepEqual(origList, newList) {
		t.Errorf("reopened List = %v; want %v", newList, origList)
	}

	origViews, _ := s.Answers(id1)
	newViews, ok := reopened.Answers(id1)
	if !ok || !reflect.DeepEqual(newViews, origViews) {
		t.Errorf("reopened Answers = %+v (ok=%v); want %+v", newViews, ok, origViews)
	}
	if !newViews[0].Superseded || newViews[1].Superseded {
		t.Errorf("reopened superseded flags = %v, %v; want superseded, current", newViews[0].Superseded, newViews[1].Superseded)
	}

	origCur, _ := s.CurrentAnswer(id1)
	newCur, ok := reopened.CurrentAnswer(id1)
	if !ok || !reflect.DeepEqual(newCur, origCur) || newCur.ID != a2.ID {
		t.Errorf("reopened CurrentAnswer = %+v (ok=%v); want %+v", newCur, ok, origCur)
	}
	if reopened.AnswerCount(id1) != 2 || s.AnswerCount(id1) != 2 {
		t.Errorf("AnswerCount = %d/%d; want 2/2", reopened.AnswerCount(id1), s.AnswerCount(id1))
	}
	if q2, _ := reopened.Get(id2); q2.State != QuestionWithdrawn {
		t.Errorf("reopened state of the withdrawn question = %q; want withdrawn", q2.State)
	}
	if q3, _ := reopened.Get(id3); q3.State != QuestionExpired {
		t.Errorf("reopened state of the expired question = %q; want expired", q3.State)
	}
}

func TestQuestionStore_QuotePreservationAndSHA(t *testing.T) {
	path, s, _, _ := newQuestionStoreTest(t)
	id := mustRaise(t, s, sampleQuestion("Wording check?"))

	quote := "  leading and trailing spaces, \r\n a newline, \t a tab, ünïcodé — end  "
	a, err := s.Answer(id, Answer{Quote: quote, Actor: "operator"})
	if err != nil {
		t.Fatalf("answer: %v", err)
	}
	if a.Quote != quote {
		t.Fatalf("stored quote is not byte-identical:\n got %q\nwant %q", a.Quote, quote)
	}
	if a.QuoteSHA256 != QuoteSHA256(quote) {
		t.Fatalf("stored hash = %q; want %q", a.QuoteSHA256, QuoteSHA256(quote))
	}

	// After a reopen (replay), the quote is still exactly as received and
	// the hash still matches it.
	reopened, err := NewQuestionStore(path)
	if err != nil {
		t.Fatalf("re-open the store: %v", err)
	}
	views, ok := reopened.Answers(id)
	if !ok || len(views) != 1 {
		t.Fatalf("reopened Answers = %d entries (ok=%v); want 1", len(views), ok)
	}
	if views[0].Quote != quote {
		t.Errorf("reopened quote = %q; want %q (no trimming, no normalisation)", views[0].Quote, quote)
	}
	if views[0].QuoteSHA256 != QuoteSHA256(quote) {
		t.Errorf("reopened hash = %q; want %q", views[0].QuoteSHA256, QuoteSHA256(quote))
	}
}

func TestQuestionStore_RefusedAfterTerminalState(t *testing.T) {
	_, s, _, _ := newQuestionStoreTest(t)

	idW := mustRaise(t, s, sampleQuestion("Withdraw me?"))
	if _, err := s.Withdraw(idW, "operator", "not needed now"); err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	if _, err := s.Answer(idW, Answer{Quote: "too late"}); !errors.Is(err, ErrQuestionNotOpen) {
		t.Errorf("answer to a withdrawn question: %v; want ErrQuestionNotOpen", err)
	}

	idE := mustRaise(t, s, sampleQuestion("Expire me?"))
	if _, err := s.Expire(idE, "skipped"); err != nil {
		t.Fatalf("expire: %v", err)
	}
	if _, err := s.Answer(idE, Answer{Quote: "too late"}); !errors.Is(err, ErrQuestionNotOpen) {
		t.Errorf("answer to an expired question: %v; want ErrQuestionNotOpen", err)
	}

	// And the refused answers stored nothing.
	for _, id := range []string{idW, idE} {
		if n := s.AnswerCount(id); n != 0 {
			t.Errorf("AnswerCount(%s) = %d; want 0", id, n)
		}
	}
}

func TestQuestionStore_WithdrawAndExpireOnlyFromOpen(t *testing.T) {
	_, s, _, _ := newQuestionStoreTest(t)

	// Withdraw an open question.
	idOpen := mustRaise(t, s, sampleQuestion("Open one?"))
	w, err := s.Withdraw(idOpen, "operator", "withdrawing")
	if err != nil {
		t.Fatalf("withdraw an open question: %v", err)
	}
	if w.State != QuestionWithdrawn {
		t.Errorf("returned state = %q; want withdrawn", w.State)
	}

	// Withdraw an answered question: refused.
	idAns := mustRaise(t, s, sampleQuestion("Answered one?"))
	if _, err := s.Answer(idAns, Answer{Quote: "decided"}); err != nil {
		t.Fatalf("answer: %v", err)
	}
	if _, err := s.Withdraw(idAns, "operator", "too late"); !errors.Is(err, ErrQuestionNotOpen) {
		t.Errorf("withdraw an answered question: %v; want ErrQuestionNotOpen", err)
	}

	// Expire an open question.
	idOpen2 := mustRaise(t, s, sampleQuestion("Second open?"))
	e, err := s.Expire(idOpen2, "skipped by the supervisor")
	if err != nil {
		t.Fatalf("expire an open question: %v", err)
	}
	if e.State != QuestionExpired {
		t.Errorf("returned state = %q; want expired", e.State)
	}

	// Expire a withdrawn question: refused.
	if _, err := s.Expire(idOpen, "too late"); !errors.Is(err, ErrQuestionNotOpen) {
		t.Errorf("expire a withdrawn question: %v; want ErrQuestionNotOpen", err)
	}

	// Unknown ids are refused too.
	if _, err := s.Withdraw("q-none", "x", "y"); !errors.Is(err, ErrQuestionNotFound) {
		t.Errorf("withdraw an unknown question: %v; want ErrQuestionNotFound", err)
	}
	if _, err := s.Expire("q-none", "z"); !errors.Is(err, ErrQuestionNotFound) {
		t.Errorf("expire an unknown question: %v; want ErrQuestionNotFound", err)
	}
}

func TestQuestionStore_NeverExpires(t *testing.T) {
	_, s, _, advance := newQuestionStoreTest(t)

	id := mustRaise(t, s, sampleQuestion("Stays open?"))
	advance(3 * 365 * 24 * time.Hour) // years pass

	// The store has no timer and no sweeper: the question is still open,
	// still findable and still answerable.
	q, ok := s.Get(id)
	if !ok || q.State != QuestionOpen {
		t.Errorf("after years, Get = %+v (ok=%v); want open", q, ok)
	}
	if got := s.List(QuestionFilter{State: QuestionOpen}); len(got) != 1 || got[0].ID != id {
		t.Errorf("after years, the open list = %v; want the unanswered question", questionIDs(got))
	}
	if _, err := s.Answer(id, Answer{Quote: "eventually answered"}); err != nil {
		t.Errorf("answering after years: %v", err)
	}

	// Retention is forever: the store exposes no delete; a reopened view
	// still holds everything.
	if _, err := NewQuestionStore(filepath.Join(t.TempDir(), "nowhere")); err != nil {
		t.Fatalf("a fresh store from a missing file should be an empty store: %v", err)
	}
}

const testSecretAnswer = "ghp_K7J2mQ9xR4vN8wZ3pL6t" + " " + "password=hunter2xy12"

func TestQuestionStore_SecretRefused(t *testing.T) {
	path, s, rec, _ := newQuestionStoreTest(t)
	id := mustRaise(t, s, sampleQuestion("Credential check?"))

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the store file: %v", err)
	}

	_, err = s.Answer(id, Answer{Quote: testSecretAnswer, Actor: "operator", Surface: "cli"})
	if err == nil {
		t.Fatalf("secret-bearing answer was accepted; want a refusal")
	}
	if !errors.Is(err, ErrSecretInAnswer) {
		t.Errorf("refusal error = %v; want it to wrap ErrSecretInAnswer", err)
	}
	if !strings.Contains(err.Error(), "Resend it without the") {
		t.Errorf("refusal error %q does not carry the operator-facing refusal message", err)
	}
	if strings.Contains(err.Error(), testSecretAnswer) {
		t.Errorf("refusal error carries the secret text: %q", err)
	}

	// Nothing was stored, nowhere.
	if q, _ := s.Get(id); q.State != QuestionOpen {
		t.Errorf("state after a refused answer = %q; want open", q.State)
	}
	if n := s.AnswerCount(id); n != 0 {
		t.Errorf("AnswerCount after a refused answer = %d; want 0", n)
	}
	if views, _ := s.Answers(id); len(views) != 0 {
		t.Errorf("Answers after a refused answer = %d entries; want none", len(views))
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the store file again: %v", err)
	}
	if !bytes.HasPrefix(after, before) || len(after) != len(before) {
		t.Errorf("the store file changed after a refused answer; want it untouched")
	}
	if strings.Contains(string(after), testSecretAnswer) {
		t.Errorf("the store file contains the secret")
	}

	// The refusal was audited — without the text, with the rule name only.
	actions, details := rec.snapshot()
	if len(actions) != 2 {
		t.Fatalf("audit actions = %v; want raise plus refusal", actions)
	}
	refusal := details[1]
	if actions[1] != "question_answer_refused" {
		t.Fatalf("refusal audit action = %q; want question_answer_refused", actions[1])
	}
	if refusal["reason"] != "secret_detected" {
		t.Errorf("refusal reason = %v; want secret_detected", refusal["reason"])
	}
	wantPattern, _ := ScanAnswerForSecret(testSecretAnswer)
	if refusal["pattern"] != wantPattern {
		t.Errorf("refusal audit pattern = %v; want the name of the matched rule %q", refusal["pattern"], wantPattern)
	}
	for key, v := range refusal {
		if strings.Contains(fmt.Sprintf("%v", v), testSecretAnswer) {
			t.Errorf("refusal audit %q carries the secret: %v", key, v)
		}
	}
	dump, _ := json.Marshal(details[1])
	if strings.Contains(string(dump), testSecretAnswer) {
		t.Errorf("the refusal audit entry contains the secret: %s", dump)
	}
	wantKeys := []string{"actor", "pattern", "question_id", "reason", "surface"}
	if got := auditKeysSorted(details[1]); !reflect.DeepEqual(got, wantKeys) {
		t.Errorf("refusal audit keys = %v; want %v", got, wantKeys)
	}
}

func TestQuestionStore_AuditEveryStateChange(t *testing.T) {
	_, s, rec, _ := newQuestionStoreTest(t)

	id := mustRaise(t, s, sampleQuestion("Audit probe?"))
	a1, err := s.Answer(id, Answer{ChosenOptionID: "first", Quote: "first", Actor: "operator", Surface: "rest"})
	if err != nil {
		t.Fatalf("answer: %v", err)
	}
	id2 := mustRaise(t, s, sampleQuestion("Audit probe two?"))
	if _, err := s.Withdraw(id2, "operator", "retracted"); err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	id3 := mustRaise(t, s, sampleQuestion("Audit probe three?"))
	if _, err := s.Expire(id3, "skipped"); err != nil {
		t.Fatalf("expire: %v", err)
	}

	actions, details := rec.snapshot()
	want := []string{
		"question_raised", "question_answered",
		"question_raised", "question_withdrawn",
		"question_raised", "question_expired",
	}
	if !reflect.DeepEqual(actions, want) {
		t.Fatalf("audit actions = %v; want %v", actions, want)
	}

	// Each entry carries exactly the documented keys and the question id.
	byAction := map[string][]map[string]any{}
	for i, a := range actions {
		byAction[a] = append(byAction[a], details[i])
	}
	if v := byAction["question_answered"][0]; v["question_id"] != id || v["answer_id"] != a1.ID || v["chosen_option_id"] != "first" || v["quote"] != "first" || v["quote_sha256"] != QuoteSHA256("first") || v["actor"] != "operator" || v["surface"] != "rest" {
		t.Errorf("answered audit entry = %v; want the documented values", v)
	}
	wv := byAction["question_withdrawn"][0]
	if wv["question_id"] != id2 || wv["actor"] != "operator" || wv["reason"] != "retracted" {
		t.Errorf("withdrawn audit entry = %v; want the documented values", wv)
	}
	if got := auditKeysSorted(wv); !reflect.DeepEqual(got, []string{"actor", "question_id", "reason"}) {
		t.Errorf("withdrawn audit keys = %v; want the documented set", got)
	}
	ev := byAction["question_expired"][0]
	if ev["question_id"] != id3 || ev["reason"] != "skipped" {
		t.Errorf("expired audit entry = %v; want the documented values", ev)
	}
	if got := auditKeysSorted(ev); !reflect.DeepEqual(got, []string{"question_id", "reason"}) {
		t.Errorf("expired audit keys = %v; want the documented set", got)
	}
}

func TestQuestionStore_FailedAppendEmitsNoAudit(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses the permission bit, so the append is not guaranteed to fail")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "questions.jsonl")
	s, err := NewQuestionStore(path)
	if err != nil {
		t.Fatalf("NewQuestionStore: %v", err)
	}
	rec := &fakeAuditRecorder{}
	s.SetAuditFn(rec.Record)

	raised, err := s.Raise(sampleQuestion("First raises fine?"))
	if err != nil {
		t.Fatalf("first raise: %v", err)
	}
	id := raised.ID

	// Make the file impossible to open for writing: drop the owner write
	// bit from the file itself (a read-only directory would not block
	// opening an existing file).
	if err := os.Chmod(path, 0o400); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { os.Chmod(path, 0o600) })

	if _, err := s.Raise(sampleQuestion("This one cannot be written?")); err == nil {
		t.Fatalf("a second raise over a read-only directory was accepted; want a failure")
	}

	// The in-memory view is unchanged and nothing was audited.
	if got := s.List(QuestionFilter{}); len(got) != 1 || got[0].ID != id {
		t.Errorf("in-memory store after a failed append = %v; want only the first question", questionIDs(got))
	}
	if q, ok := s.Get(id); !ok || q.State != QuestionOpen {
		t.Errorf("in-memory Get after a failed append = %+v (ok=%v); want the open first question", q, ok)
	}
	actions, _ := rec.snapshot()
	if len(actions) != 1 || actions[0] != "question_raised" {
		t.Errorf("audit entries after a failed append = %v; want only the first raise", actions)
	}
}

func TestQuestionStore_TruncatedFinalLineSkipped(t *testing.T) {
	path, s, _, _ := newQuestionStoreTest(t)
	id := mustRaise(t, s, sampleQuestion("Survived a crash?"))

	// Simulate a crash mid-write: a partial record appended with no final
	// newline (the file otherwise keeps its previous lines).
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open the store for the simulated crash: %v", err)
	}
	if _, err := f.WriteString(`{"type":"answer_recorded","answer":{"id"`); err != nil {
		f.Close()
		t.Fatalf("simulated crash write: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close after the simulated crash: %v", err)
	}

	// The unterminated final line is skipped, not an error.
	reopened, err := NewQuestionStore(path)
	if err != nil {
		t.Fatalf("re-open over a truncated final line: %v", err)
	}
	if q, ok := reopened.Get(id); !ok || q.State != QuestionOpen {
		t.Errorf("reopened Get = %+v (ok=%v); want the open question", q, ok)
	}

	// The next append starts with a newline, so the two never merge into
	// one line: the new record must stand alone as its own, complete line.
	got, err := reopened.Raise(sampleQuestion("Next append is valid?"))
	if err != nil {
		t.Fatalf("raise after skipping a truncated line: %v", err)
	}
	if q, ok := reopened.Get(got.ID); !ok || q.Text != "Next append is valid?" {
		t.Errorf("the store after the append lost the new question: %+v (ok=%v)", q, ok)
	}
	if q, ok := reopened.Get(id); !ok || q.State != QuestionOpen {
		t.Errorf("the store after the append lost the replayed question: %+v (ok=%v)", q, ok)
	}

	fileData, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the store file after the append: %v", err)
	}
	if !bytes.HasSuffix(fileData, []byte("\n")) {
		t.Fatal("the file after the append does not end with a newline")
	}
	lines := strings.Split(strings.TrimSuffix(string(fileData), "\n"), "\n")
	last := lines[len(lines)-1]
	var rec questionStoreRecord
	if err := json.Unmarshal([]byte(last), &rec); err != nil {
		t.Fatalf("the appended record must stand alone on its own line: %v", err)
	}
	if rec.Type != storeLineQuestionRaised || rec.Question == nil || rec.Question.ID != got.ID {
		t.Errorf("the last file line = %q; want the new question_raised record for %s", last, got.ID)
	}
}

func TestQuestionStore_CorruptLineFailsWithLineNumber(t *testing.T) {
	path, s, _, _ := newQuestionStoreTest(t)
	mustRaise(t, s, sampleQuestion("Line one?"))
	if _, err := s.Raise(sampleQuestion("Line two?")); err != nil {
		t.Fatalf("second raise: %v", err)
	}
	mustRaise(t, s, sampleQuestion("Line three?"))

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the store file: %v", err)
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("store file has %d lines; want 3", len(lines))
	}
	// Corrupt a middle line (terminated): replay must fail and name it.
	lines[1] = "{ definitely not a record "
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("write the corrupted file: %v", err)
	}

	_, err = NewQuestionStore(path)
	if err == nil {
		t.Fatal("replaying a corrupt middle line succeeded; want an error")
	}
	if !strings.Contains(err.Error(), "line 2") {
		t.Errorf("corrupt-line error %q does not name line 2", err)
	}
}

func TestQuestionStore_ConcurrentRaise(t *testing.T) {
	_, s, _, _ := newQuestionStoreTest(t)

	const n = 20
	var wg sync.WaitGroup
	ids := make([]string, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			q := sampleQuestion(fmt.Sprintf("Concurrent question %d?", i))
			got, err := s.Raise(q)
			if err != nil {
				errs[i] = err
				return
			}
			ids[i] = got.ID
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("concurrent raise %d: %v", i, err)
			continue
		}
		if ids[i] == "" {
			t.Errorf("concurrent raise %d returned an empty id", i)
		}
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if id == "" {
			continue
		}
		if seen[id] {
			t.Errorf("duplicate id %q from concurrent raises", id)
		}
		seen[id] = true
	}
	if got := s.List(QuestionFilter{Limit: 1000}); len(got) != n {
		t.Errorf("store holds %d questions; want %d", len(got), n)
	}
}

func TestQuestionStore_IDFormatAndOrder(t *testing.T) {
	// With the default (non-injected) generators the ids are 'q-' or 'a-'
	// followed by 20 lowercase hex characters: a zero-padded nanosecond
	// timestamp plus 4 random hex characters. They are time-sortable.
	path := filepath.Join(t.TempDir(), "idprobe.jsonl")
	s, err := NewQuestionStore(path)
	if err != nil {
		t.Fatalf("NewQuestionStore: %v", err)
	}
	base := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return base }

	q := sampleQuestion("Id probe?")
	got, err := s.Raise(q)
	if err != nil {
		t.Fatalf("Raise: %v", err)
	}
	if len(got.ID) != 2+20 || got.ID[:2] != "q-" {
		t.Fatalf("default question id = %q; want q- plus 20 hex characters", got.ID)
	}
	for _, c := range got.ID[2:] {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			t.Fatalf("default question id %q is not lowercase hex after the prefix", got.ID)
		}
	}

	s.now = func() time.Time { return base.Add(time.Second) }
	got2, err := s.Answer(got.ID, Answer{Quote: "id probe answer"})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if len(got2.ID) != 2+20 || got2.ID[:2] != "a-" {
		t.Fatalf("default answer id = %q; want a- plus 20 hex characters", got2.ID)
	}
}

// TestQuestionStore_ConcurrentWithdrawRaise is the regression guard for the
// lock discipline on the Withdraw and Expire read-back. It drives a Withdraw
// that reads the questions map in parallel with a batch of Raises that write
// to that same map. Run with -race, the race detector flags an unlocked
// questions-map read that happens while a raise is writing one, so this test
// is the probe that exercises that path. The old code read the question via a
// helper that assumed the caller held the store lock; Withdraw and Expire
// called it after releasing the lock, which raced these raises.
func TestQuestionStore_ConcurrentWithdrawRaise(t *testing.T) {
	_, s, _, advance := newQuestionStoreTest(t)

	target := mustRaise(t, s, sampleQuestion("Withdraw me while raising?"))
	advance(time.Millisecond)
	mustRaise(t, s, sampleQuestion("A second seed question?"))

	const n = 16
	raisedIDs := make([]string, n)
	errIdx := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			q, err := s.Raise(sampleQuestion(fmt.Sprintf("Concurrent raise %d?", i)))
			if err != nil {
				errIdx[i] = err
				return
			}
			raisedIDs[i] = q.ID
		}(i)
		go func() {
			defer wg.Done()
			// Several goroutines contend for the open->withdrawn move; only
			// one wins and the rest see a terminal state. The point is that
			// the read-back is race-free against the parallel Raises.
			_, _ = s.Withdraw(target, "operator", "concurrent probe")
		}()
	}
	wg.Wait()

	for i, err := range errIdx {
		if err != nil {
			t.Errorf("concurrent raise %d: %v", i, err)
			continue
		}
		if raisedIDs[i] == "" {
			t.Errorf("concurrent raise %d returned an empty id", i)
		}
	}
	seen := map[string]bool{}
	for _, id := range raisedIDs {
		if id == "" {
			continue
		}
		if seen[id] {
			t.Errorf("concurrent raises produced a duplicate id %q", id)
		}
		seen[id] = true
	}

	q, ok := s.Get(target)
	if !ok || q.State != QuestionWithdrawn {
		t.Errorf("target state = %q (ok=%v); want withdrawn", q.State, ok)
	}
	if got := len(s.List(QuestionFilter{Limit: 1000})); got != n+2 {
		t.Errorf("store holds %d questions; want %d (no raise was lost to a collision)", got, n+2)
	}
}

// TestQuestionStore_RaiseIDNeverReusedOnCollision pins the id-uniqueness
// guard. The real generator draws fresh random bytes each call, so a repeat
// is vanishingly rare, but the invariant to test is deterministic: if the
// generator hands the store an id it already holds, Raise must take a
// distinct id instead of overwriting the existing question.
func TestQuestionStore_RaiseIDNeverReusedOnCollision(t *testing.T) {
	path, s, _, _ := newQuestionStoreTest(t)

	first := mustRaise(t, s, sampleQuestion("First, id already used?"))

	// The degenerate case: a generator that keeps handing back an id the store
	// already holds. A raise must never reuse that id, so the store resolves
	// a unique one instead of clobbering the existing question — and the
	// resolution must terminate rather than loop forever.
	s.newQuestionID = func(time.Time) string { return first }

	second, err := s.Raise(sampleQuestion("Second, must not collide?"))
	if err != nil {
		t.Fatalf("raise the colliding question: %v", err)
	}
	if second.ID == first {
		t.Fatalf("Raise reused id %q; question ids must never be reused", first)
	}
	if !strings.HasPrefix(second.ID, first) {
		t.Errorf("second id %q does not extend the colliding base %q; want a disambiguated id, not a reuse", second.ID, first)
	}
	if q, ok := s.Get(first); !ok || q.Text != "First, id already used?" {
		t.Errorf("the first question = %+v (ok=%v); a collision must not clobber it", q, ok)
	}
	if q, ok := s.Get(second.ID); !ok || q.Text != "Second, must not collide?" {
		t.Errorf("the second question = %+v (ok=%v); want it under its distinct id", q, ok)
	}
	if got := len(s.List(QuestionFilter{Limit: 1000})); got != 2 {
		t.Errorf("store holds %d questions; want 2", got)
	}

	// Both survive a reopen: the file holds two distinct raise records.
	reopened, err := NewQuestionStore(path)
	if err != nil {
		t.Fatalf("reopen the store: %v", err)
	}
	if got := len(reopened.List(QuestionFilter{Limit: 1000})); got != 2 {
		t.Errorf("reopened store holds %d questions; want 2", got)
	}
	if _, ok := reopened.Get(first); !ok {
		t.Errorf("reopened store lost the first question %s", first)
	}
	if _, ok := reopened.Get(second.ID); !ok {
		t.Errorf("reopened store lost the second question %s", second.ID)
	}
}
