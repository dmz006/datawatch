// questions_step2_fields_test.go — the priority and return-path fields on
// the operator question record: their validation rules (priority is a
// separate field from risk; the session-line return path is the per-question
// opt-in and needs the asking session), the replay of records written before
// the fields existed, the persistence of the fields in the append-only
// store file, and the JSON wire shape the REST surface raises, stores and
// reads back (POST /api/questions, then GET by id and in the list).
package autonomous

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestQuestionPriorityReturnPathValid(t *testing.T) {
	cases := []struct {
		name     string
		mutate   func(q *Question)
		wantPrio string
		wantRP   bool
	}{
		{name: "empty priority and no return path stay as-is"},
		{name: "explicit normal priority accepted", mutate: func(q *Question) { q.Priority = QuestionPriorityNormal }, wantPrio: "normal"},
		{
			name:     "high priority accepted independent of risk normal",
			mutate:   func(q *Question) { q.Risk = QuestionRiskNormal; q.Priority = QuestionPriorityHigh },
			wantPrio: "high",
		},
		{
			name:     "high priority with low risk accepted",
			mutate:   func(q *Question) { q.Risk = QuestionRiskLow; q.Priority = QuestionPriorityHigh },
			wantPrio: "high",
		},
		{
			name:   "return path opt-in with session accepted",
			mutate: func(q *Question) { q.ReturnPath = true; q.SessionID = "sess-1" },
			wantRP: true,
		},
		{
			name:     "high priority with return path and session accepted",
			mutate:   func(q *Question) { q.Priority = QuestionPriorityHigh; q.ReturnPath = true; q.SessionID = "sess-2" },
			wantPrio: "high", wantRP: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q := baseQuestion()
			if tc.mutate != nil {
				tc.mutate(q)
			}
			if err := ValidateNewQuestion(q); err != nil {
				t.Fatalf("ValidateNewQuestion() = %v, want nil", err)
			}
			if q.Priority != tc.wantPrio {
				t.Fatalf("priority = %q, want %q", q.Priority, tc.wantPrio)
			}
			if q.ReturnPath != tc.wantRP {
				t.Fatalf("return path = %v, want %v", q.ReturnPath, tc.wantRP)
			}
		})
	}
}

func TestQuestionPriorityReturnPathInvalidPriority(t *testing.T) {
	for _, bad := range []string{"urgent", "HIGH", "High", "low", "normal ", "high high"} {
		q := baseQuestion()
		q.Priority = bad
		err := ValidateNewQuestion(q)
		if err == nil {
			t.Fatalf("priority %q: ValidateNewQuestion() = nil, want an error", bad)
		}
		if !errors.Is(err, ErrInvalidQuestion) {
			t.Fatalf("priority %q: error %v does not wrap ErrInvalidQuestion", bad, err)
		}
		if !strings.Contains(err.Error(), "priority") {
			t.Fatalf("priority %q: error %q does not name the field", bad, err)
		}
	}
}

func TestQuestionPriorityReturnPathReturnPathRequiresSession(t *testing.T) {
	q := baseQuestion()
	q.ReturnPath = true
	err := ValidateNewQuestion(q)
	if !errors.Is(err, ErrInvalidQuestion) {
		t.Fatalf("return path without a session: got %v, want ErrInvalidQuestion", err)
	}
	if !strings.Contains(err.Error(), "return_path") {
		t.Fatalf("error %q does not name return_path", err)
	}
	if !strings.Contains(err.Error(), "session_id") {
		t.Fatalf("error %q does not name the missing session_id", err)
	}

	// With the asking session named, the same question passes: the REST
	// surface maps this validation error onto a 400 on both readings (the
	// raise handler and the store agree on ErrInvalidQuestion).
	q = baseQuestion()
	q.ReturnPath = true
	q.SessionID = "sess-9"
	if err := ValidateNewQuestion(q); err != nil {
		t.Fatalf("return path with a session: %v, want nil", err)
	}
}

// TestQuestionPriorityReturnPathReplayOldRecord plays back a store line
// written before the priority and return-path fields existed: the record
// must still replay, with empty priority and no return path, and the store
// must still work around it.
func TestQuestionPriorityReturnPathReplayOldRecord(t *testing.T) {
	oldLine := `{"type":"question_raised","question":{"id":"q-0000000000000001","label":"decision","asked_by":"operator","asker_kind":"operator","context":"none","text":"an old question","options":[{"id":"yes","label":"Yes"},{"id":"no","label":"No"}],"recommendation":{"option_id":"yes"},"default":"yes","risk":"low","created_at":"2026-10-27T12:00:00Z","state":"open"},"at":"2026-10-27T12:00:00Z"}`
	path := filepath.Join(t.TempDir(), "questions.jsonl")
	if err := os.WriteFile(path, []byte(oldLine+"\n"), 0o600); err != nil {
		t.Fatalf("write old line: %v", err)
	}

	s, err := NewQuestionStore(path)
	if err != nil {
		t.Fatalf("NewQuestionStore replays an old record: %v", err)
	}
	q, ok := s.Get("q-0000000000000001")
	if !ok {
		t.Fatal("old record did not replay: Get returned no question")
	}
	if q.Priority != "" {
		t.Fatalf("replayed old record has priority %q, want empty", q.Priority)
	}
	if q.ReturnPath {
		t.Fatal("replayed old record has return path, want false")
	}
	if q.Risk != QuestionRiskLow {
		t.Fatalf("replayed old record risk = %q, want low (the pre-existing field is untouched)", q.Risk)
	}

	// The store is still fully operational around the old record: a new
	// raise lands, the old one answers, and both are listed.
	fresh := baseQuestion()
	fresh.Text = "a new question"
	fresh.Priority = QuestionPriorityHigh
	fresh.ReturnPath = true
	fresh.SessionID = "sess-new"
	raised, err := s.Raise(*fresh)
	if err != nil {
		t.Fatalf("Raise next to an old record: %v", err)
	}
	if _, err := s.Answer("q-0000000000000001", Answer{ChosenOptionID: "yes", Actor: "operator"}); err != nil {
		t.Fatalf("Answer to an old record: %v", err)
	}
	if got := len(s.List(QuestionFilter{})); got != 2 {
		t.Fatalf("List after mixing records = %d, want 2", got)
	}
	if _, ok := s.Get(raised.ID); !ok {
		t.Fatal("raised question missing from the view")
	}
}

// TestQuestionPriorityReturnPathPersistAndReopen persists the new fields to
// the append-only file and reopens the same file: both the line on disk and
// the replayed view carry them byte-for-byte.
func TestQuestionPriorityReturnPathPersistAndReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "questions.jsonl")
	s, err := NewQuestionStore(path)
	if err != nil {
		t.Fatalf("NewQuestionStore: %v", err)
	}
	q := baseQuestion()
	q.Priority = QuestionPriorityHigh
	q.ReturnPath = true
	q.SessionID = "sess-persist"
	raised, err := s.Raise(*q)
	if err != nil {
		t.Fatalf("Raise: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read store file: %v", err)
	}
	for _, want := range []string{
		`"priority":"high"`,
		`"return_path":true`,
		`"session_id":"sess-persist"`,
	} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("store file lacks %s:\n%s", want, raw)
		}
	}

	s2, err := NewQuestionStore(path)
	if err != nil {
		t.Fatalf("reopen the store file: %v", err)
	}
	replayed, ok := s2.Get(raised.ID)
	if !ok {
		t.Fatal("reopened store lost the raised question")
	}
	if replayed.Priority != QuestionPriorityHigh || !replayed.ReturnPath || replayed.SessionID != "sess-persist" {
		t.Fatalf("reopened view = priority %q, return path %v, session %q; want high, true, sess-persist",
			replayed.Priority, replayed.ReturnPath, replayed.SessionID)
	}
}

// TestQuestionPriorityReturnPathRESTRoundTrip is the wire round trip the
// REST surface performs: the POST /api/questions body shape decodes into the
// record, the store stores it, and the GET by-id and list responses encode
// both fields back out. The body and the record share the JSON field names
// (label, context, options, default, risk, priority, return_path, ...), so
// decoding the body JSON straight into a Question is the same mapping the
// raise handler performs field by field.
func TestQuestionPriorityReturnPathRESTRoundTrip(t *testing.T) {
	body := `{"label":"decision","context":"none","text":"round trip question","options":[{"id":"yes","label":"Yes"},{"id":"no","label":"No"}],"default":"yes","risk":"low","priority":"high","return_path":true,"session_id":"sess-wire"}`
	var q Question
	if err := json.Unmarshal([]byte(body), &q); err != nil {
		t.Fatalf("decode raise body: %v", err)
	}
	if q.Priority != "high" || !q.ReturnPath || q.SessionID != "sess-wire" {
		t.Fatalf("body decode = priority %q, return path %v, session %q; want high, true, sess-wire",
			q.Priority, q.ReturnPath, q.SessionID)
	}
	if err := ValidateNewQuestion(&q); err != nil {
		t.Fatalf("ValidateNewQuestion() = %v, want nil", err)
	}

	s, err := NewQuestionStore(filepath.Join(t.TempDir(), "questions.jsonl"))
	if err != nil {
		t.Fatalf("NewQuestionStore: %v", err)
	}
	raised, err := s.Raise(q)
	if err != nil {
		t.Fatalf("Raise: %v", err)
	}

	// The get-by-id body: the response envelope is {"question": {...}}.
	get, ok := s.Get(raised.ID)
	if !ok {
		t.Fatal("Get after Raise: no question")
	}
	envelope, err := json.Marshal(map[string]any{"question": get})
	if err != nil {
		t.Fatalf("encode get response: %v", err)
	}
	for _, want := range []string{`"priority":"high"`, `"return_path":true`, `"session_id":"sess-wire"`} {
		if !strings.Contains(string(envelope), want) {
			t.Fatalf("get response lacks %s:\n%s", want, envelope)
		}
	}

	// The list body: {"count": n, "questions": [...]}.
	listEnv, err := json.Marshal(map[string]any{"count": 1, "questions": s.List(QuestionFilter{})})
	if err != nil {
		t.Fatalf("encode list response: %v", err)
	}
	if !strings.Contains(string(listEnv), `"priority":"high"`) ||
		!strings.Contains(string(listEnv), `"return_path":true`) {
		t.Fatalf("list response lacks the fields:\n%s", listEnv)
	}

	// Decoding either response back into a record preserves both fields
	// (the read side of the same wire shape).
	var out struct {
		Question Question `json:"question"`
	}
	if err := json.Unmarshal(envelope, &out); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	if out.Question.Priority != QuestionPriorityHigh || !out.Question.ReturnPath {
		t.Fatalf("round trip = priority %q, return path %v; want high, true",
			out.Question.Priority, out.Question.ReturnPath)
	}

	// The refusal side of the wire: a body that opts into the return path
	// without a session fails with ErrInvalidQuestion, which the raise
	// handler maps to a 400 naming return_path.
	var bad Question
	if err := json.Unmarshal([]byte(`{"label":"decision","context":"none","text":"bad","options":[{"id":"a","label":"A"},{"id":"b","label":"B"}],"default":"a","return_path":true}`), &bad); err != nil {
		t.Fatalf("decode bad body: %v", err)
	}
	err = ValidateNewQuestion(&bad)
	if !errors.Is(err, ErrInvalidQuestion) {
		t.Fatalf("bad body: got %v, want ErrInvalidQuestion (the handler's 400)", err)
	}
	if !strings.Contains(err.Error(), "return_path") {
		t.Fatalf("bad body error %q does not name the field", err)
	}
}
