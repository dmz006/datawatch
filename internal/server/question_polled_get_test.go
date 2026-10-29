// question_polled_get_test.go — the polled GET return path for askers that
// cannot subscribe: GET /api/questions/{id} carries the state, priority and
// return_path on the question plus, when answered, the current answer
// summary (answer id, chosen option, verbatim quote, actor, surface,
// created_at); GET /api/autonomous/events returns the question kinds for a
// named queue with cursor reads; and with the switch off the reads refuse
// with 503. Driven through the handlers against the real QuestionStore and
// the real PendingEventQueue.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// polledServer wires the real QuestionStore into a Server with the
// operator-questions switch on (the events test additionally wires the
// store's event sink into a real PendingEventQueue the way the daemon does).
func polledServer(t *testing.T, enabled bool) (*Server, *autonomous.QuestionStore) {
	t.Helper()
	store, err := autonomous.NewQuestionStore(filepath.Join(t.TempDir(), "questions.jsonl"))
	if err != nil {
		t.Fatalf("NewQuestionStore: %v", err)
	}
	cfg := config.Config{
		Autonomous: config.AutonomousConfig{
			Questions: config.AutonomousQuestionsConfig{Enabled: enabled},
		},
	}
	return &Server{cfg: &cfg, questionStore: store}, store
}

// polledRaise POSTs a question and returns its id.
func polledRaise(t *testing.T, s *Server) string {
	t.Helper()
	body := map[string]any{
		"label":    "decision",
		"context":  "none",
		"priority": "high",
		"text":     "Should retry backoff be capped at five minutes?",
		"options": []map[string]any{
			{"id": "yes", "label": "Yes, cap it"},
			{"id": "no", "label": "No"},
		},
		"default":        "yes",
		"asked_by":       "poller-session",
		"asker_kind":     "session",
		"session_id":     "sess_poll",
	}
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	rec := httptest.NewRecorder()
	s.handleQuestions(rec, httptest.NewRequest(http.MethodPost, "/api/questions", bytes.NewReader(b)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("raise: want 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var env struct {
		Question struct {
			ID string `json:"id"`
		} `json:"question"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode raise: %v", err)
	}
	return env.Question.ID
}

// polledAnswer POSTs an answer and returns the stored id.
func polledAnswer(t *testing.T, s *Server, qid string, body map[string]any) string {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	rec := httptest.NewRecorder()
	s.handleQuestions(rec, httptest.NewRequest(http.MethodPost, "/api/questions/"+qid+"/answer", bytes.NewReader(b)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("answer: want 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var env struct {
		Answer struct {
			ID string `json:"id"`
		} `json:"answer"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode answer: %v", err)
	}
	return env.Answer.ID
}

// polledGetEnvelope is the GET /api/questions/{id} body (the polled read's
// return path: question + answer metadata + the current answer summary).
type polledGetEnvelope struct {
	Question        autonomous.Question  `json:"question"`
	AnswerCount     int                  `json:"answer_count"`
	CurrentAnswerID string               `json:"current_answer_id"`
	Answer          *polledAnswerSummary `json:"answer"`
}

// polledGet GETs /api/questions/{id} and unwraps the envelope.
func polledGet(t *testing.T, s *Server, qid string) (*httptest.ResponseRecorder, *polledGetEnvelope) {
	t.Helper()
	rec := httptest.NewRecorder()
	s.handleQuestions(rec, httptest.NewRequest(http.MethodGet, "/api/questions/"+qid, nil))
	env := &polledGetEnvelope{}
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: want 200, got %d: %s", qid, rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return rec, env
}

// TestQuestionPolledGetOpenNoAnswer: an open question's polled read carries
// the state, priority and return_path on the question and a null answer.
func TestQuestionPolledGetOpenNoAnswer(t *testing.T) {
	s, _ := polledServer(t, true)
	qid := polledRaise(t, s)

	_, env := polledGet(t, s, qid)
	if env.Question.State != autonomous.QuestionOpen {
		t.Errorf("state = %q, want open", env.Question.State)
	}
	if env.Question.Priority != "high" {
		t.Errorf("priority = %q, want high", env.Question.Priority)
	}
	if env.Question.ReturnPath {
		t.Errorf("return_path = true, want false (the session line is opt-in and was not opted in)")
	}
	if env.Answer != nil {
		t.Errorf("answer = %+v, want null while the question is open", env.Answer)
	}
	if env.CurrentAnswerID != "" {
		t.Errorf("current_answer_id = %q, want empty", env.CurrentAnswerID)
	}
	if env.AnswerCount != 0 {
		t.Errorf("answer_count = %d, want 0", env.AnswerCount)
	}
}

// TestQuestionPolledGetAnsweredShowsAnswer: the same read after an answer
// carries the current answer summary — id, chosen option, the verbatim
// quote (padded whitespace intact), actor, surface, created_at.
func TestQuestionPolledGetAnsweredShowsAnswer(t *testing.T) {
	s, _ := polledServer(t, true)
	qid := polledRaise(t, s)

	quote := "  cap it — and log every hit  "
	aid := polledAnswer(t, s, qid, map[string]any{
		"chosen_option_id": "yes",
		"quote":            quote,
		"actor":            "op@example.net",
		"surface":          "mcp",
	})

	_, env := polledGet(t, s, qid)
	if env.Question.State != autonomous.QuestionAnswered {
		t.Fatalf("state = %q, want answered", env.Question.State)
	}
	if env.Answer == nil {
		t.Fatalf("answer is null, want the current answer summary")
	}
	if env.Answer.ID != aid {
		t.Errorf("answer.id = %q, want %q", env.Answer.ID, aid)
	}
	if env.CurrentAnswerID != aid {
		t.Errorf("current_answer_id = %q, want %q", env.CurrentAnswerID, aid)
	}
	if env.Answer.ChosenOptionID != "yes" {
		t.Errorf("chosen_option_id = %q, want yes", env.Answer.ChosenOptionID)
	}
	if env.Answer.Quote != quote {
		t.Errorf("quote = %q, want the verbatim %q", env.Answer.Quote, quote)
	}
	if env.Answer.Actor != "op@example.net" {
		t.Errorf("actor = %q, want op@example.net", env.Answer.Actor)
	}
	if env.Answer.Surface != "mcp" {
		t.Errorf("surface = %q, want mcp", env.Answer.Surface)
	}
	if env.Answer.CreatedAt == nil {
		t.Error("created_at is nil, want the answer's timestamp")
	} else if env.Answer.CreatedAt.IsZero() {
		t.Errorf("created_at = %v, want a real time", *env.Answer.CreatedAt)
	}
	if env.AnswerCount != 1 {
		t.Errorf("answer_count = %d, want 1", env.AnswerCount)
	}
}

// TestQuestionPolledGetSupersededShowsLatest: a changed mind lands as a new
// answer that supersedes the first, and the polled read afterwards shows
// the latest answer — while the count remembers both.
func TestQuestionPolledGetSupersededShowsLatest(t *testing.T) {
	s, _ := polledServer(t, true)
	qid := polledRaise(t, s)

	first := polledAnswer(t, s, qid, map[string]any{
		"chosen_option_id": "yes",
		"actor":            "op@example.net",
		"surface":          "rest",
	})
	latest := polledAnswer(t, s, qid, map[string]any{
		"chosen_option_id": "no",
		"quote":            "wait for the load window first",
		"supersedes":       first,
		"actor":            "supervisor@example.net",
		"surface":          "comm:ops",
	})

	_, env := polledGet(t, s, qid)
	if env.Question.State != autonomous.QuestionAnswered {
		t.Fatalf("state = %q, want answered", env.Question.State)
	}
	if env.Answer == nil {
		t.Fatalf("answer is null, want the latest answer")
	}
	if env.Answer.ID != latest || env.CurrentAnswerID != latest {
		t.Errorf("current answer = %q / %q, want latest %q", env.Answer.ID, env.CurrentAnswerID, latest)
	}
	if env.Answer.ChosenOptionID != "no" {
		t.Errorf("chosen_option_id = %q, want the latest answer's no", env.Answer.ChosenOptionID)
	}
	if env.Answer.Actor != "supervisor@example.net" || env.Answer.Surface != "comm:ops" {
		t.Errorf("actor/surface = %q/%q, want the latest answer's", env.Answer.Actor, env.Answer.Surface)
	}
	if env.AnswerCount != 2 {
		t.Errorf("answer_count = %d, want 2 (the superseded one stays counted)", env.AnswerCount)
	}

	// The full log is its own read: both answers, oldest first.
	rec := httptest.NewRecorder()
	s.handleQuestions(rec, httptest.NewRequest(http.MethodGet, "/api/questions/"+qid+"/answers", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("answers: want 200, got %d", rec.Code)
	}
	logEnv := struct {
		Answers []struct {
			ID string `json:"id"`
		} `json:"answers"`
	}{}
	if err := json.Unmarshal(rec.Body.Bytes(), &logEnv); err != nil {
		t.Fatalf("decode answers: %v", err)
	}
	if len(logEnv.Answers) != 2 || logEnv.Answers[0].ID != first || logEnv.Answers[1].ID != latest {
		t.Errorf("answers log = %+v, want [first latest]", logEnv.Answers)
	}
}

// TestQuestionPolledGetWithdrawnState: a withdrawn question's polled read
// still answers 200 with the withdrawn state and no answer.
func TestQuestionPolledGetWithdrawnState(t *testing.T) {
	s, _ := polledServer(t, true)
	qid := polledRaise(t, s)

	body, _ := json.Marshal(map[string]any{"reason": "duplicated by the supervisor line", "actor": "supervisor@example.net"})
	rec := httptest.NewRecorder()
	s.handleQuestions(rec, httptest.NewRequest(http.MethodPost, "/api/questions/"+qid+"/withdraw", bytes.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("withdraw: want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	_, env := polledGet(t, s, qid)
	if env.Question.State != autonomous.QuestionWithdrawn {
		t.Errorf("state = %q, want withdrawn", env.Question.State)
	}
	if env.Answer != nil {
		t.Errorf("answer = %+v, want null", env.Answer)
	}
}

// TestQuestionPolledGetEventsReadReturnsQuestionAnswered: with the store's
// event sink wired into a named queue (the way the daemon wires it), the
// events endpoint returns the question.answered event for cursor reads.
func TestQuestionPolledGetEventsReadReturnsQuestionAnswered(t *testing.T) {
	s, store := polledServer(t, true)
	queue := autonomous.NewPendingEventQueue(10)
	store.SetEventSink(func(ev autonomous.LifecycleEvent) {
		queue.Append("asker", ev)
	})
	s.autonomousEvents = queue

	qid := polledRaise(t, s)
	aid := polledAnswer(t, s, qid, map[string]any{
		"chosen_option_id": "yes",
		"quote":            "capped, please",
		"actor":            "op@example.net",
		"surface":          "rest",
	})

	rec := httptest.NewRecorder()
	s.handleAutonomousEvents(rec, httptest.NewRequest(http.MethodGet, "/api/autonomous/events?queue=asker", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("events: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Events     []autonomous.LifecycleEvent `json:"events"`
		NextCursor uint64                      `json:"next_cursor"`
		Dropped    bool                        `json:"dropped"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode events: %v", err)
	}
	if len(body.Events) != 1 {
		t.Fatalf("events = %+v, want exactly the answer event", body.Events)
	}
	ev := body.Events[0]
	if ev.Kind != autonomous.KindQuestionAnswered {
		t.Errorf("kind = %q, want question.answered", ev.Kind)
	}
	if ev.QuestionID != qid {
		t.Errorf("question_id = %q, want %q", ev.QuestionID, qid)
	}
	if ev.AnswerID != aid {
		t.Errorf("answer_id = %q, want %q", ev.AnswerID, aid)
	}
	if body.NextCursor != 1 || body.Dropped {
		t.Errorf("next_cursor/dropped = %d/%v, want 1/false", body.NextCursor, body.Dropped)
	}

	// The cursor read: passing next_cursor back returns nothing new.
	rec = httptest.NewRecorder()
	s.handleAutonomousEvents(rec, httptest.NewRequest(http.MethodGet, "/api/autonomous/events?queue=asker&since=1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("events since=1: want 200, got %d", rec.Code)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode events: %v", err)
	}
	if len(body.Events) != 0 {
		t.Errorf("events after cursor = %+v, want none", body.Events)
	}
}

// TestQuestionPolledGetSwitchOffReturns503: with the switch off — read
// live, the same read that passes with it on — the polled read refuses
// with 503 naming the switch.
func TestQuestionPolledGetSwitchOffReturns503(t *testing.T) {
	s, _ := polledServer(t, false)
	qid := "any"

	rec := httptest.NewRecorder()
	s.handleQuestions(rec, httptest.NewRequest(http.MethodGet, "/api/questions/"+qid, nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET with the switch off: want 503, got %d", rec.Code)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("autonomous.questions.enabled")) {
		t.Errorf("503 body = %q, want it to name the switch", rec.Body.String())
	}

	// The events feed is not this switch's surface: it still serves
	// (wired or 503 only when unwired), but this one has no sink wired
	// for a queue here, so the point under test is the questions read.
	rec = httptest.NewRecorder()
	s.handleAutonomousEvents(rec, httptest.NewRequest(http.MethodGet, "/api/autonomous/events?queue=asker", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("events (not wired): want 503, got %d", rec.Code)
	}
}
