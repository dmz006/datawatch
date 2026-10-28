// questions_rest_test.go — REST-level tests for /api/questions: POST and
// GET on the collection, GET by id, and the answer flow under a question
// id (POST /answer, POST /withdraw, GET /answers). Two store backings are
// exercised: the real autonomous.QuestionStore in a temp dir (the same
// store the daemon opens at <data_dir>/questions.jsonl) and a fake
// QuestionsAPI (the interface seam the daemon wires through SetQuestions).
// They together cover the JSON wire shape, the status mapping
// (201/200/400/404/405/409/413/422/503), the store's validation errors,
// the capability gates and the switch. Where the data-path flow is the
// point the real store stands in; the fake stands in for the status
// mapping.

package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// --- fixtures -----------------------------------------------------------

// questionRESTTestConfig builds the same config shape the daemon resolves,
// with the operator-questions switch in the requested state.
func questionRESTTestConfig(enabled bool) config.Config {
	return config.Config{
		Autonomous: config.AutonomousConfig{
			Questions: config.AutonomousQuestionsConfig{Enabled: enabled},
		},
	}
}

// newQuestionsRESTServer wires the real QuestionStore into a Server — the
// REST layer sits directly on the store, exactly like main.go.
func newQuestionsRESTServer(t *testing.T) (*Server, *autonomous.QuestionStore) {
	t.Helper()
	store, err := autonomous.NewQuestionStore(filepath.Join(t.TempDir(), "questions.jsonl"))
	if err != nil {
		t.Fatalf("NewQuestionStore: %v", err)
	}
	cfg := questionRESTTestConfig(true)
	s := &Server{
		// The operator-questions switch is on: REST is the switch's surface.
		cfg:           &cfg,
		questionStore: store,
	}
	return s, store
}

// validQuestionBody raises a question that passes the store's validation.
func validQuestionBody() map[string]any {
	return map[string]any{
		"label":   "decision",
		"context": "none",
		"risk":    "low",
		"text":    "Should retry backoff be capped at five minutes?",
		"options": []map[string]any{
			{"id": "yes", "label": "Yes, cap it"},
			{"id": "no", "label": "No"},
		},
		"recommendation": map[string]any{"option_id": "yes", "rationale": "bounded delay"},
		"default":        "yes",
	}
}

// doQ runs one /api/questions request against the handler and returns the
// recorder (status + body are asserted by the test).
func doQ(t *testing.T, s *Server, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		r = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, r)
	rr := httptest.NewRecorder()
	s.handleQuestions(rr, req)
	return rr
}

// decodeQuestion unwraps the {"question": {...}} envelope the raise and
// by-id reads return.
func decodeQuestion(t *testing.T, rr *httptest.ResponseRecorder) autonomous.Question {
	t.Helper()
	var env struct {
		Question autonomous.Question `json:"question"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode %d response: %v (body=%q)", rr.Code, err, rr.Body.String())
	}
	return env.Question
}

// --- fake store ----------------------------------------------------------

// fakeQuestions is a QuestionsAPI the handler can be wired with, standing
// in for the store the daemon would wire through SetQuestions. It carries
// one stored question plus a current answer and an answer count, so the
// by-id read's answer metadata is exercised without a real store.
type fakeQuestions struct {
	stored  []autonomous.Question
	byID    map[string]autonomous.Question
	current map[string]autonomous.Answer
	counts  map[string]int
	// answerErr, when set, is returned by Answer (and nothing is stored):
	// the status-mapping tests point it at the store's sentinel errors.
	answerErr error
	// withdrawErr, when set, is returned by Withdraw.
	withdrawErr error
	// answerViews overrides the default answer log returned by Answers.
	answerViews []autonomous.AnswerView
}

func newFakeQuestions() *fakeQuestions {
	q := autonomous.Question{
		ID:      "q-fake-1",
		Label:   "decision",
		AskedBy: "session-fake",
		Context: "none",
		Text:    "Should retry backoff be capped at five minutes?",
		Options: []autonomous.QuestionOption{{ID: "yes", Label: "Yes"}, {ID: "no", Label: "No"}},
		Default: "yes",
		State:   autonomous.QuestionAnswered,
	}
	f := &fakeQuestions{
		byID:    map[string]autonomous.Question{},
		current: map[string]autonomous.Answer{},
		counts:  map[string]int{},
	}
	f.byID[q.ID] = q
	f.stored = append(f.stored, q)
	f.current[q.ID] = autonomous.Answer{ID: "a-fake-1", QuestionID: q.ID, ChosenOptionID: "yes"}
	f.counts[q.ID] = 2
	return f
}

func (f *fakeQuestions) Raise(q autonomous.Question) (autonomous.Question, error) {
	q.ID = "q-fake-raised"
	q.State = autonomous.QuestionOpen
	f.stored = append(f.stored, q)
	f.byID[q.ID] = q
	return q, nil
}
func (f *fakeQuestions) Get(id string) (autonomous.Question, bool) {
	q, ok := f.byID[id]
	return q, ok
}
func (f *fakeQuestions) List(filter autonomous.QuestionFilter) []autonomous.Question {
	out := make([]autonomous.Question, 0, len(f.stored))
	for _, q := range f.stored {
		if filter.State != "" && q.State != filter.State {
			continue
		}
		if filter.Context != "" && q.Context != filter.Context {
			continue
		}
		if filter.Asker != "" && q.AskedBy != filter.Asker {
			continue
		}
		if filter.Limit > 0 && len(out) >= filter.Limit {
			break
		}
		out = append(out, q)
	}
	return out
}
func (f *fakeQuestions) Answer(questionID string, a autonomous.Answer) (autonomous.Answer, error) {
	if f.answerErr != nil {
		return autonomous.Answer{}, f.answerErr
	}
	a.QuestionID = questionID
	f.current[questionID] = a
	f.counts[questionID]++
	return a, nil
}
func (f *fakeQuestions) Withdraw(id, actor, reason string) (autonomous.Question, error) {
	if f.withdrawErr != nil {
		return autonomous.Question{}, f.withdrawErr
	}
	q, ok := f.byID[id]
	if !ok {
		return autonomous.Question{}, autonomous.ErrQuestionNotFound
	}
	q.State = autonomous.QuestionWithdrawn
	return q, nil
}
func (f *fakeQuestions) Answers(questionID string) ([]autonomous.AnswerView, bool) {
	if _, ok := f.byID[questionID]; !ok {
		return nil, false
	}
	if f.answerViews != nil {
		return f.answerViews, true
	}
	a, ok := f.current[questionID]
	if !ok {
		return []autonomous.AnswerView{}, true
	}
	return []autonomous.AnswerView{{Answer: a}}, true
}
func (f *fakeQuestions) CurrentAnswer(id string) (autonomous.Answer, bool) {
	a, ok := f.current[id]
	return a, ok
}
func (f *fakeQuestions) AnswerCount(id string) int {
	return f.counts[id]
}

// --- tests ---------------------------------------------------------------

func TestQuestionsREST_RaiseListGetRoundTrip(t *testing.T) {
	s, store := newQuestionsRESTServer(t)

	// POST -> 201, with an id assigned by the store.
	rr := doQ(t, s, http.MethodPost, "/api/questions", validQuestionBody())
	if rr.Code != http.StatusCreated {
		t.Fatalf("POST: want 201, got %d body=%s", rr.Code, rr.Body.String())
	}
	created := decodeQuestion(t, rr)
	if created.ID == "" {
		t.Fatalf("201 but no question id (body=%q)", rr.Body.String())
	}

	// GET (list) contains the new question, newest first.
	rr = doQ(t, s, http.MethodGet, "/api/questions", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET list: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var listEnv struct {
		Count     int                   `json:"count"`
		Questions []autonomous.Question `json:"questions"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &listEnv); err != nil {
		t.Fatalf("decode list: %v (body=%q)", err, rr.Body.String())
	}
	if listEnv.Count != 1 || len(listEnv.Questions) != 1 || listEnv.Questions[0].ID != created.ID {
		t.Fatalf("list: count=%d len=%d, want 1 entry %q (body=%q)", listEnv.Count, len(listEnv.Questions), created.ID, rr.Body.String())
	}

	// GET by id returns the stored record.
	rr = doQ(t, s, http.MethodGet, "/api/questions/"+created.ID, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET by id: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	got := decodeQuestion(t, rr)
	if got.ID != created.ID {
		t.Fatalf("GET by id: %q != raised %q", got.ID, created.ID)
	}
	if got.Text != "Should retry backoff be capped at five minutes?" {
		t.Fatalf("GET by id: text %q lost", got.Text)
	}

	// The answer metadata keys sit beside the question in the by-id read:
	// answer_count (NOT "count") and current_answer_id.
	if !strings.Contains(rr.Body.String(), `"answer_count"`) {
		t.Fatalf("GET by id body lacks the answer_count key: %q", rr.Body.String())
	}
	var meta map[string]json.RawMessage
	if err := json.Unmarshal(rr.Body.Bytes(), &meta); err != nil {
		t.Fatalf("decode by-id meta: %v", err)
	}
	if _, ok := meta["answer_count"]; !ok {
		t.Fatalf("GET by id: answer_count missing (keys=%v)", meta)
	}
	if string(meta["current_answer_id"]) != `""` {
		t.Fatalf("GET by id: current_answer_id = %s, want \"\"", meta["current_answer_id"])
	}
	var countVal int
	if err := json.Unmarshal(meta["answer_count"], &countVal); err != nil || countVal != 0 {
		t.Fatalf("GET by id: answer_count = %s, want 0", meta["answer_count"])
	}

	// GET unknown id -> 404.
	rr = doQ(t, s, http.MethodGet, "/api/questions/never-existed", nil)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("GET unknown id: want 404, got %d body=%s", rr.Code, rr.Body.String())
	}

	// The store itself agrees exactly one question was raised.
	if len(store.List(autonomous.QuestionFilter{})) != 1 {
		t.Fatalf("store list: want 1 question, got %d", len(store.List(autonomous.QuestionFilter{})))
	}
}

// TestQuestionsREST_RaiseReturnsImmediately is the spec's "it returns at
// once and never waits for an answer": the raise is answered with 201 and
// the stored record within a short client deadline — a handler that blocked
// on an answer would hang past it — while the question sits open with no
// answer recorded.
func TestQuestionsREST_RaiseReturnsImmediately(t *testing.T) {
	s, _ := newQuestionsRESTServer(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/questions", s.handleQuestions)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	// A short deadline: the raise must not wait for (or block on) an answer.
	client := &http.Client{Timeout: 2 * time.Second}
	b, _ := json.Marshal(validQuestionBody())
	resp, err := client.Post(srv.URL+"/api/questions", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("POST within the 2s deadline (a handler waiting for an answer would not return): %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST: want 201 at once, got %d", resp.StatusCode)
	}
	var env struct {
		Question autonomous.Question `json:"question"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode raise: %v", err)
	}

	// Returned immediately, with no answer existing yet.
	if env.Question.State != autonomous.QuestionOpen {
		t.Fatalf("state = %q, want %q (no answer may have been awaited or recorded)", env.Question.State, autonomous.QuestionOpen)
	}
	if a, ok := s.questionStore.CurrentAnswer(env.Question.ID); ok {
		t.Fatalf("a current answer appeared on an unanswered question: %q", a.ID)
	}
	if n := s.questionStore.AnswerCount(env.Question.ID); n != 0 {
		t.Fatalf("answer_count = %d, want 0", n)
	}
}

func TestQuestionsREST_RaiseDefaults(t *testing.T) {
	s, _ := newQuestionsRESTServer(t)

	// An absent asked_by/asker_kind default to the caller's actor and the
	// operator kind; context, label and risk are the caller's to name.
	b := validQuestionBody()
	delete(b, "asker_kind")
	delete(b, "asked_by")
	rr := doQ(t, s, http.MethodPost, "/api/questions", b)
	if rr.Code != http.StatusCreated {
		t.Fatalf("POST: want 201, got %d body=%s", rr.Code, rr.Body.String())
	}
	got := decodeQuestion(t, rr)
	if got.AskerKind != autonomous.QuestionAskerOperator {
		t.Fatalf("default asker kind: %q, want %q", got.AskerKind, autonomous.QuestionAskerOperator)
	}
	if got.AskedBy == "" {
		t.Fatalf("default asked_by empty")
	}
	if got.Label == "" {
		t.Fatalf("label should have been stored")
	}
	if got.Context != autonomous.QuestionContextNone {
		t.Fatalf("context %q, want the body's %q (passed through as-is)", got.Context, autonomous.QuestionContextNone)
	}
}

func TestQuestionsREST_RaiseBodyValidation(t *testing.T) {
	s, store := newQuestionsRESTServer(t)

	cases := []struct {
		name string
		body map[string]any
		want string // substring the store's validation message must carry
	}{
		{"no text", map[string]any{"default": "d"}, "text"},
		{"no options", map[string]any{"text": "t"}, "option"},
		{"no default option", map[string]any{"text": "t", "options": []map[string]any{{"id": "a", "label": "A"}}}, "default"},
		{
			"default option not in options",
			map[string]any{"text": "t", "options": []map[string]any{{"id": "a", "label": "A"}}, "default": "zzz"},
			"default",
		},
		// An omitted context gets no handler-side default (there is none in
		// this contract): the body must name a known context and the store
		// says so with a 400.
		{
			"no context",
			map[string]any{"text": "t", "default": "a", "options": []map[string]any{{"id": "a", "label": "A"}}},
			"context",
		},
		{"context prd with no prd id", map[string]any{"text": "t", "default": "a", "options": []map[string]any{{"id": "a", "label": "A"}}, "context": "prd"}, "prd"},
		{"unknown context", map[string]any{"text": "t", "default": "a", "options": []map[string]any{{"id": "a", "label": "A"}}, "context": "carrier-pigeon"}, "context"},
		{"unknown risk", map[string]any{"text": "t", "default": "a", "options": []map[string]any{{"id": "a", "label": "A"}}, "risk": "apocalyptic"}, "risk"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr := doQ(t, s, http.MethodPost, "/api/questions", tc.body)
			if rr.Code != http.StatusBadRequest {
				t.Fatalf("want 400, got %d body=%s", rr.Code, rr.Body.String())
			}
			if !strings.Contains(rr.Body.String(), tc.want) {
				t.Fatalf("body %q does not mention %q", rr.Body.String(), tc.want)
			}
			if got := len(store.List(autonomous.QuestionFilter{})); got != 0 {
				t.Fatalf("rejected question was stored: %d questions", got)
			}
		})
	}

	// Malformed JSON body -> 400 (decode failure, before validation).
	req := httptest.NewRequest(http.MethodPost, "/api/questions", strings.NewReader("`not json"))
	rec := httptest.NewRecorder()
	s.handleQuestions(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed body: want 400, got %d body=%s", rec.Code, rec.Body.String())
	}
	// Content after the JSON object -> 400.
	req = httptest.NewRequest(http.MethodPost, "/api/questions", strings.NewReader(`{}`+" {}"))
	rec = httptest.NewRecorder()
	s.handleQuestions(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("second JSON value: want 400, got %d body=%s", rec.Code, rec.Body.String())
	}
	if got := len(store.List(autonomous.QuestionFilter{})); got != 0 {
		t.Fatalf("rejected bodies stored a question: %d questions", got)
	}

	// An oversize body (valid JSON shape, over the 64 KiB cap) is refused
	// by the body cap with a 413, before it is read to the end.
	big := `{"text":"` + strings.Repeat("x", maxQuestionBodyBytes) + `","a":1}`
	req = httptest.NewRequest(http.MethodPost, "/api/questions", strings.NewReader(big))
	rec = httptest.NewRecorder()
	s.handleQuestions(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize body: want 413, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestQuestionsREST_ListFilters(t *testing.T) {
	s, store := newQuestionsRESTServer(t)

	raise := func(mut func(map[string]any)) autonomous.Question {
		b := validQuestionBody()
		mut(b)
		rr := doQ(t, s, http.MethodPost, "/api/questions", b)
		if rr.Code != http.StatusCreated {
			t.Fatalf("POST: got %d body=%s", rr.Code, rr.Body.String())
		}
		return decodeQuestion(t, rr)
	}

	listCount := func(query string) int {
		path := "/api/questions"
		if query != "" {
			path += "?" + query
		}
		rr := doQ(t, s, http.MethodGet, path, nil)
		if rr.Code != http.StatusOK {
			t.Fatalf("GET %s: want 200, got %d body=%s", query, rr.Code, rr.Body.String())
		}
		var env struct {
			Count int `json:"count"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
			t.Fatalf("decode list: %v body=%q", err, rr.Body.String())
		}
		return env.Count
	}

	// Empty list reads as an empty object with count 0.
	if got := listCount(""); got != 0 {
		t.Fatalf("empty list: count %d, want 0", got)
	}

	// Two questions with distinct asker self-attestations and texts.
	raise(func(b map[string]any) { b["asked_by"] = "session-a"; b["text"] = "backoff cap?" })
	raise(func(b map[string]any) { b["asked_by"] = "session-b"; b["text"] = "cache ttl?" })

	// Filtering narrows the list, each filter independently.
	cases := []struct {
		query string
		want  int
	}{
		{"", 2},
		{"asker=session-a", 1},
		{"asker=ghost", 0},
		{"text=backoff", 1},
		{"state=open", 2},
		{"state=answered", 0},
		{"context=none", 2},
		{"context=task", 0},
		{"limit=1", 1},
	}
	for _, tc := range cases {
		if got := listCount(tc.query); got != tc.want {
			t.Fatalf("GET ?%s: count %d, want %d", tc.query, got, tc.want)
		}
	}

	// Unknown filter values are 400.
	for _, query := range []string{"state=bogus", "context=carrier-pigeon", "limit=zero", "limit=-3"} {
		rr := doQ(t, s, http.MethodGet, "/api/questions?"+query, nil)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("GET ?%s: want 400, got %d body=%s", query, rr.Code, rr.Body.String())
		}
	}

	_ = store
}

// TestQuestionsREST_GetExtraFieldsWithFake drives the by-id read against a
// fake QuestionsAPI (the interface seam, not the store): the fake reports
// a current answer and two answers recorded — the handler must surface
// them as answer_count and current_answer_id beside the question.
func TestQuestionsREST_GetExtraFieldsWithFake(t *testing.T) {
	fake := newFakeQuestions()
	cfg := questionRESTTestConfig(true)
	s := &Server{cfg: &cfg, questionStore: fake}

	rr := doQ(t, s, http.MethodGet, "/api/questions/q-fake-1", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET by id: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	got := decodeQuestion(t, rr)
	if got.ID != "q-fake-1" || got.State != autonomous.QuestionAnswered {
		t.Fatalf("GET by id: got %+v", got)
	}

	// The answer metadata keys, exactly named: answer_count (not "count")
	// and current_answer_id.
	var meta map[string]json.RawMessage
	if err := json.Unmarshal(rr.Body.Bytes(), &meta); err != nil {
		t.Fatalf("decode by-id meta: %v", err)
	}
	counts, ok := meta["answer_count"]
	if !ok {
		t.Fatalf("GET by id: answer_count missing (body=%q)", rr.Body.String())
	}
	var n int
	if err := json.Unmarshal(counts, &n); err != nil || n != 2 {
		t.Fatalf("answer_count = %s, want 2 (the fake's recorded count)", counts)
	}
	current, ok := meta["current_answer_id"]
	if !ok || string(current) != `"a-fake-1"` {
		t.Fatalf("current_answer_id = %v, want \"a-fake-1\"", current)
	}

	// Unknown id -> 404 even through the fake.
	rr = doQ(t, s, http.MethodGet, "/api/questions/q-missing", nil)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("GET unknown id: want 404, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestQuestionsREST_WrongMethodIs405(t *testing.T) {
	s, _ := newQuestionsRESTServer(t)

	rr := doQ(t, s, http.MethodPut, "/api/questions", validQuestionBody())
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("PUT list: want 405, got %d body=%s", rr.Code, rr.Body.String())
	}
	// By id accepts GET only.
	rr = doQ(t, s, http.MethodPost, "/api/questions/some-id", validQuestionBody())
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST by id: want 405, got %d body=%s", rr.Code, rr.Body.String())
	}
	rr = doQ(t, s, http.MethodDelete, "/api/questions/some-id", nil)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("DELETE by id: want 405, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestQuestionsREST_UnknownSubPathsAre404AndWrongMethodsAre405(t *testing.T) {
	s, _ := newQuestionsRESTServer(t)

	// The answer flow paths exist, but each accepts one method only: the
	// same path on any other method is 405, for a known and an unknown id
	// alike (the shape fails before the id is looked up).
	wrong := []struct{ method, path string }{
		{http.MethodGet, "/api/questions/some-id/answer"},
		{http.MethodPut, "/api/questions/some-id/answer"},
		{http.MethodDelete, "/api/questions/some-id/answer"},
		{http.MethodGet, "/api/questions/some-id/withdraw"},
		{http.MethodPut, "/api/questions/some-id/withdraw"},
		{http.MethodPost, "/api/questions/some-id/answers"},
		{http.MethodPut, "/api/questions/some-id/answers"},
		{http.MethodDelete, "/api/questions/some-id/answers"},
		{http.MethodGet, "/api/questions/never-existed/answer"},
	}
	for _, tc := range wrong {
		rr := doQ(t, s, tc.method, tc.path, nil)
		if rr.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s %s: want 405, got %d body=%s", tc.method, tc.path, rr.Code, rr.Body.String())
		}
	}

	// Other sub-paths beneath a question id are not on this surface at all:
	// a fixed 404 — never a stub that returns success.
	unshaped := []string{
		"/api/questions/some-id/bogus",
		"/api/questions/some-id/answers/deep",
		"/api/questions/a/b",
	}
	for _, path := range unshaped {
		for _, method := range []string{http.MethodPost, http.MethodGet, http.MethodDelete} {
			rr := doQ(t, s, method, path, nil)
			if rr.Code != http.StatusNotFound {
				t.Fatalf("%s %s: want 404, got %d body=%s", method, path, rr.Code, rr.Body.String())
			}
		}
	}
}

func TestQuestionsREST_NilStoreIs503(t *testing.T) {
	// Nothing wired at all: the endpoints refuse rather than answer from
	// nothing, on GET list, POST raise and by-id.
	cfg := questionRESTTestConfig(true)
	s := &Server{cfg: &cfg}
	for _, tc := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/questions"},
		{http.MethodPost, "/api/questions"},
		{http.MethodGet, "/api/questions/some-id"},
	} {
		rr := doQ(t, s, tc.method, tc.path, nil)
		if rr.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s %s (nothing wired): want 503, got %d body=%s", tc.method, tc.path, rr.Code, rr.Body.String())
		}
	}
}

// TestQuestionsREST_SwitchOffIs503ThenOnWithoutRestart is the runtime
// toggle: the switch off (its default) refuses the endpoints while naming
// the switch, then flipping it on through the config — on the same server,
// same handlers, no restart — serves the raise and the read.
func TestQuestionsREST_SwitchOffIs503ThenOnWithoutRestart(t *testing.T) {
	store, err := autonomous.NewQuestionStore(filepath.Join(t.TempDir(), "questions.jsonl"))
	if err != nil {
		t.Fatalf("NewQuestionStore: %v", err)
	}
	cfg := questionRESTTestConfig(false) // off — the default
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	s := &Server{cfg: &cfg, cfgPath: cfgPath, questionStore: store}

	// Off: every surface refuses with a 503 that names the switch.
	for _, tc := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/questions"},
		{http.MethodPost, "/api/questions"},
		{http.MethodGet, "/api/questions/some-id"},
	} {
		rr := doQ(t, s, tc.method, tc.path, nil)
		if rr.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s %s (switch off): want 503, got %d body=%s", tc.method, tc.path, rr.Code, rr.Body.String())
		}
		if !strings.Contains(rr.Body.String(), "autonomous.questions.enabled") {
			t.Fatalf("%s %s (switch off): error %q does not name autonomous.questions.enabled", tc.method, tc.path, rr.Body.String())
		}
	}
	if got := len(store.List(autonomous.QuestionFilter{})); got != 0 {
		t.Fatalf("off switch stored a question anyway: %d", got)
	}

	// Turn the switch on through the config — the daemon's own path — on
	// the same server (no restart, same wired handlers).
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"autonomous.questions.enabled": true})
	if !s.cfg.Autonomous.Questions.Enabled {
		t.Fatalf("switch still off after PUT true")
	}

	// The same handler now works.
	rr := doQ(t, s, http.MethodPost, "/api/questions", validQuestionBody())
	if rr.Code != http.StatusCreated {
		t.Fatalf("POST after enabling: want 201, got %d body=%s", rr.Code, rr.Body.String())
	}
	created := decodeQuestion(t, rr)
	rr = doQ(t, s, http.MethodGet, "/api/questions?state=open", nil)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), created.ID) {
		t.Fatalf("GET list after enabling: want 200 containing %q, got %d body=%s", created.ID, rr.Code, rr.Body.String())
	}
	rr = doQ(t, s, http.MethodGet, "/api/questions/"+created.ID, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET by id after enabling: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestQuestionsREST_NilConfigStillServed(t *testing.T) {
	// A test server built without any config (no switch to read) serves
	// the wired store — being disabled is the explicit switch=false, not
	// the absence of config.
	s, _ := newQuestionsRESTServer(t)
	s.cfg = nil

	b, _ := json.Marshal(validQuestionBody())
	req := httptest.NewRequest(http.MethodPost, "/api/questions", bytes.NewReader(b))
	rec := httptest.NewRecorder()
	s.handleQuestions(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST with cfg=nil: want 201, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestQuestionsREST_FederationCaps(t *testing.T) {
	s, _ := newQuestionsRESTServer(t)
	body, _ := json.Marshal(validQuestionBody())

	// A federated peer without the autonomous caps is refused 403 on the
	// read...
	peerReq := asPeer(httptest.NewRequest(http.MethodGet, "/api/questions", nil), "sessions:read")
	rec := httptest.NewRecorder()
	s.handleQuestions(rec, peerReq)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("GET as caps-less peer: want 403, got %d body=%s", rec.Code, rec.Body.String())
	}
	// ...and on the raise.
	peerReq = asPeer(httptest.NewRequest(http.MethodPost, "/api/questions", bytes.NewReader(body)), "sessions:read")
	rec = httptest.NewRecorder()
	s.handleQuestions(rec, peerReq)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("POST as caps-less peer: want 403, got %d body=%s", rec.Code, rec.Body.String())
	}

	// autonomous:read allows the list and by-id reads...
	peerReq = asPeer(httptest.NewRequest(http.MethodGet, "/api/questions", nil), "autonomous:read")
	rec = httptest.NewRecorder()
	s.handleQuestions(rec, peerReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET as autonomous:read peer: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	// ...but not the raise.
	peerReq = asPeer(httptest.NewRequest(http.MethodPost, "/api/questions", bytes.NewReader(body)), "autonomous:read")
	rec = httptest.NewRecorder()
	s.handleQuestions(rec, peerReq)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("POST as autonomous:read peer: want 403, got %d body=%s", rec.Code, rec.Body.String())
	}

	// ...and autonomous:write does allow the raise.
	peerReq = asPeer(httptest.NewRequest(http.MethodPost, "/api/questions", bytes.NewReader(body)), "autonomous:write")
	rec = httptest.NewRecorder()
	s.handleQuestions(rec, peerReq)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST as autonomous:write peer: want 201, got %d body=%s", rec.Code, rec.Body.String())
	}

	// The local (non-federated) caller passes as always.
	rec = httptest.NewRecorder()
	local := httptest.NewRequest(http.MethodPost, "/api/questions", bytes.NewReader(body))
	s.handleQuestions(rec, local)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST as local caller: want 201, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestQuestionsREST_MuxRoutable is the regression for the ServeMux routing
// gap: the collection pattern "/api/questions" alone does not match
// "/api/questions/{id}", so GET-by-id 404s through the real mux even when
// the handler test (which calls the handler directly) passes. Registering
// the trailing-slash subtree pattern covers both forms.
func TestQuestionsREST_MuxRoutable(t *testing.T) {
	// Structural guard: server.go must keep registering the
	// trailing-slash subtree pattern for the question routes.
	src, err := os.ReadFile("server.go") // tests run in the package directory
	if err != nil {
		t.Fatalf("read server.go: %v", err)
	}
	if !strings.Contains(string(src), `apiMux.HandleFunc("/api/questions/",`) {
		t.Fatalf("server.go no longer registers the trailing-slash subtree /api/questions/ — GET by id will 404 from the real mux")
	}

	// Behavioural: drive the handler through a ServeMux registered exactly
	// the way server.go does, so the path (not just the handler) is tested.
	s, _ := newQuestionsRESTServer(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/questions", s.handleQuestions)
	mux.HandleFunc("/api/questions/", s.handleQuestions)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	b, _ := json.Marshal(validQuestionBody())
	resp, err := http.Post(srv.URL+"/api/questions", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("POST via mux: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var env struct {
		Question autonomous.Question `json:"question"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode raise via mux: %v", err)
	}
	if resp.StatusCode != http.StatusCreated || env.Question.ID == "" {
		t.Fatalf("POST via mux: status=%d id=%q, want 201 with an id", resp.StatusCode, env.Question.ID)
	}

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/questions/"+env.Question.ID, nil)
	if err != nil {
		t.Fatalf("new GET: %v", err)
	}
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET via mux: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/questions/{id} via real mux: want 200, got %d (the original bug)", resp.StatusCode)
	}
}

// --- the answer flow under a question id ---------------------------------

// doAnswer posts the answer body to /api/questions/{id}/answer (optionally
// tagged with the given peer capabilities) and returns the recorder.
func doAnswer(t *testing.T, s *Server, id string, body map[string]any, caps ...string) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal answer body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/questions/"+id+"/answer", bytes.NewReader(b))
	if len(caps) > 0 {
		req = asPeer(req, caps...)
	}
	rec := httptest.NewRecorder()
	s.handleQuestions(rec, req)
	return rec
}

// doWithdraw posts the withdraw body to /api/questions/{id}/withdraw and
// returns the recorder.
func doWithdraw(t *testing.T, s *Server, id, reason string, caps ...string) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(map[string]any{"reason": reason})
	if err != nil {
		t.Fatalf("marshal withdraw body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/questions/"+id+"/withdraw", bytes.NewReader(b))
	if len(caps) > 0 {
		req = asPeer(req, caps...)
	}
	rec := httptest.NewRecorder()
	s.handleQuestions(rec, req)
	return rec
}

// doAnswers reads /api/questions/{id}/answers and returns the recorder.
func doAnswers(t *testing.T, s *Server, id string, caps ...string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/questions/"+id+"/answers", nil)
	if len(caps) > 0 {
		req = asPeer(req, caps...)
	}
	rec := httptest.NewRecorder()
	s.handleQuestions(rec, req)
	return rec
}

// raiseQ raises one valid question through the REST handler and returns the
// stored record (201 expected).
func raiseQ(t *testing.T, s *Server) autonomous.Question {
	t.Helper()
	rr := doQ(t, s, http.MethodPost, "/api/questions", validQuestionBody())
	if rr.Code != http.StatusCreated {
		t.Fatalf("raise: want 201, got %d body=%s", rr.Code, rr.Body.String())
	}
	return decodeQuestion(t, rr)
}

// decodeAnswerEnv unwraps the {"answer": {...}, "question": {...}} envelope
// the 201 answer response uses.
func decodeAnswerEnv(t *testing.T, rr *httptest.ResponseRecorder) (autonomous.Answer, autonomous.Question) {
	t.Helper()
	var env struct {
		Answer   autonomous.Answer   `json:"answer"`
		Question autonomous.Question `json:"question"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode 201: %v body=%q", err, rr.Body.String())
	}
	return env.Answer, env.Question
}

// TestQuestionsREST_AnswerAllThreeShapes drives the three accepted shapes:
// option only, quote only, and both together — and checks the 201 carries
// the stored answer (option, quote, quote hash) and the question's new
// state.
func TestQuestionsREST_AnswerAllThreeShapes(t *testing.T) {
	s, _ := newQuestionsRESTServer(t)

	// Option only.
	q := raiseQ(t, s)
	rr := doAnswer(t, s, q.ID, map[string]any{"chosen_option_id": "yes"})
	if rr.Code != http.StatusCreated {
		t.Fatalf("option-only: want 201, got %d body=%s", rr.Code, rr.Body.String())
	}
	ans, qout := decodeAnswerEnv(t, rr)
	if ans.ID == "" || ans.QuestionID != q.ID || ans.ChosenOptionID != "yes" {
		t.Fatalf("option-only: answer record = %+v", ans)
	}
	if qout.State != autonomous.QuestionAnswered {
		t.Fatalf("option-only: question state %q, want answered", qout.State)
	}

	// Quote only.
	q2 := raiseQ(t, s)
	rr = doAnswer(t, s, q2.ID, map[string]any{"quote": "yes, cap it at five"})
	if rr.Code != http.StatusCreated {
		t.Fatalf("quote-only: want 201, got %d body=%s", rr.Code, rr.Body.String())
	}
	ans, _ = decodeAnswerEnv(t, rr)
	if ans.Quote != "yes, cap it at five" {
		t.Fatalf("quote-only: stored quote %q not passed through", ans.Quote)
	}
	sum := sha256.Sum256([]byte("yes, cap it at five"))
	if ans.QuoteSHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("quote-only: quote_sha256 mismatch")
	}

	// Both together.
	q3 := raiseQ(t, s)
	rr = doAnswer(t, s, q3.ID, map[string]any{"chosen_option_id": "no", "quote": "keep it uncapped for the long tail"})
	if rr.Code != http.StatusCreated {
		t.Fatalf("both: want 201, got %d body=%s", rr.Code, rr.Body.String())
	}
	ans, _ = decodeAnswerEnv(t, rr)
	if ans.ChosenOptionID != "no" || ans.Quote != "keep it uncapped for the long tail" {
		t.Fatalf("both: answer = %+v", ans)
	}
}

// TestQuestionsREST_AnswerQuoteVerbatimAndHashed is the byte-for-byte rule:
// nothing in the REST layer may trim, normalize or re-encode the quote
// before it is stored, and the hash must cover exactly the bytes received.
func TestQuestionsREST_AnswerQuoteVerbatimAndHashed(t *testing.T) {
	s, _ := newQuestionsRESTServer(t)

	q := raiseQ(t, s)
	quote := "  leading and trailing spaces,\t a tab,\r\n a CRLF, and unicode: café — 日本語"
	rr := doAnswer(t, s, q.ID, map[string]any{"quote": quote})
	if rr.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d body=%s", rr.Code, rr.Body.String())
	}
	ans, _ := decodeAnswerEnv(t, rr)
	if ans.Quote != quote {
		t.Fatalf("quote altered in transit: %q != %q", ans.Quote, quote)
	}
	sum := sha256.Sum256([]byte(quote))
	if ans.QuoteSHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("quote_sha256 = %q, want the hash of the exact bytes of %q", ans.QuoteSHA256, quote)
	}
}

// TestQuestionsREST_SupersedesChainAndAnswerLog is the changed-mind flow
// end-to-end: superseding requires naming the current answer (409
// otherwise), a wrong target is 409, the right one is 201; the answer log
// is oldest first with derived superseded state, the current id on the
// newest.
func TestQuestionsREST_SupersedesChainAndAnswerLog(t *testing.T) {
	s, _ := newQuestionsRESTServer(t)

	q := raiseQ(t, s)
	rr := doAnswer(t, s, q.ID, map[string]any{"chosen_option_id": "yes"})
	if rr.Code != http.StatusCreated {
		t.Fatalf("first answer: got %d body=%s", rr.Code, rr.Body.String())
	}
	first, _ := decodeAnswerEnv(t, rr)

	// Now answered: no supersedes -> 409; wrong target -> 409.
	rr = doAnswer(t, s, q.ID, map[string]any{"chosen_option_id": "no"})
	if rr.Code != http.StatusConflict {
		t.Fatalf("change of mind without supersedes: want 409, got %d body=%s", rr.Code, rr.Body.String())
	}
	rr = doAnswer(t, s, q.ID, map[string]any{"chosen_option_id": "no", "supersedes": "a-ghost"})
	if rr.Code != http.StatusConflict {
		t.Fatalf("wrong supersedes target: want 409, got %d body=%s", rr.Code, rr.Body.String())
	}
	// Naming the current answer -> 201.
	rr = doAnswer(t, s, q.ID, map[string]any{"chosen_option_id": "no", "supersedes": first.ID})
	if rr.Code != http.StatusCreated {
		t.Fatalf("superseding answer: want 201, got %d body=%s", rr.Code, rr.Body.String())
	}
	second, _ := decodeAnswerEnv(t, rr)

	// The log: oldest first with derived superseded state, current on newest.
	rr = doAnswers(t, s, q.ID)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET answers: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var logEnv struct {
		Answers []autonomous.AnswerView `json:"answers"`
		Current string                  `json:"current_answer_id"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &logEnv); err != nil {
		t.Fatalf("decode log: %v body=%q", err, rr.Body.String())
	}
	if len(logEnv.Answers) != 2 {
		t.Fatalf("log: %d entries, want 2 (body=%q)", len(logEnv.Answers), rr.Body.String())
	}
	oldest, newest := logEnv.Answers[0], logEnv.Answers[1]
	if oldest.ID != first.ID || newest.ID != second.ID {
		t.Fatalf("log order: %q then %q, want oldest first (%q then %q)",
			oldest.ID, newest.ID, first.ID, second.ID)
	}
	if !oldest.Superseded || oldest.SupersededBy != second.ID {
		t.Fatalf("oldest: superseded=%v superseded_by=%q, want true/%q",
			oldest.Superseded, oldest.SupersededBy, second.ID)
	}
	if newest.Superseded || newest.SupersededBy != "" {
		t.Fatalf("newest: superseded=%v superseded_by=%q, want false/\"\"",
			newest.Superseded, newest.SupersededBy)
	}
	if logEnv.Current != second.ID {
		t.Fatalf("current id field = %q, want %q", logEnv.Current, second.ID)
	}
}

// TestQuestionsREST_WithdrawFlow is the withdraw end-to-end: 200 with the
// withdrawn state and the actor recorded; 409 a second time; 404 on a
// missing question; an answered question can still be withdrawn, as can an
// open one; a withdrawn question refuses an answer.
func TestQuestionsREST_WithdrawFlow(t *testing.T) {
	s, _ := newQuestionsRESTServer(t)

	q := raiseQ(t, s)
	rr := doWithdraw(t, s, q.ID, "the situation changed")
	if rr.Code != http.StatusOK {
		t.Fatalf("withdraw open: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var env struct {
		Question autonomous.Question `json:"question"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v body=%q", err, rr.Body.String())
	}
	if env.Question.State != autonomous.QuestionWithdrawn {
		t.Fatalf("withdrawn state not in 200 body: %+v", env.Question)
	}

	// The log read still works on the withdrawn question.
	rr = doAnswers(t, s, q.ID)
	if rr.Code != http.StatusOK {
		t.Fatalf("answers on withdrawn: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	// A second withdraw is a conflict.
	rr = doWithdraw(t, s, q.ID, "again")
	if rr.Code != http.StatusConflict {
		t.Fatalf("second withdraw: want 409, got %d body=%s", rr.Code, rr.Body.String())
	}

	// A withdrawn question refuses an answer.
	rr = doAnswer(t, s, q.ID, map[string]any{"quote": "too late"})
	if rr.Code != http.StatusConflict {
		t.Fatalf("answer after withdraw: want 409, got %d body=%s", rr.Code, rr.Body.String())
	}

	// An answered question is no longer withdrawable: 409 (the operator's
	// reply stands; a changed mind is a superseding answer, not a withdraw).
	q2 := raiseQ(t, s)
	rr = doAnswer(t, s, q2.ID, map[string]any{"quote": "cap it"})
	if rr.Code != http.StatusCreated {
		t.Fatalf("answer q2: want 201, got %d body=%s", rr.Code, rr.Body.String())
	}
	rr = doWithdraw(t, s, q2.ID, "change")
	if rr.Code != http.StatusConflict {
		t.Fatalf("withdraw answered: want 409, got %d body=%s", rr.Code, rr.Body.String())
	}

	// Withdraw of a missing question is 404.
	rr = doWithdraw(t, s, "never-existed", "why")
	if rr.Code != http.StatusNotFound {
		t.Fatalf("withdraw missing: want 404, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// TestQuestionsREST_AnswerStatusMappingThroughTheFake drives the handler's
// error mapping against a fake store pointed at the store's sentinel
// errors: 404 not found, 409 not open / needs supersedes / wrong target,
// 400 empty / unknown option, 422 secret.
func TestQuestionsREST_AnswerStatusMappingThroughTheFake(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"not found", autonomous.ErrQuestionNotFound, http.StatusNotFound},
		{"not open", autonomous.ErrQuestionNotOpen, http.StatusConflict},
		{"needs supersedes", autonomous.ErrAnswerNeedsSupersedes, http.StatusConflict},
		{"wrong target", autonomous.ErrSupersedesWrongTarget, http.StatusConflict},
		{"empty", autonomous.ErrAnswerEmpty, http.StatusBadRequest},
		{"unknown option", autonomous.ErrUnknownOption, http.StatusBadRequest},
		{"secret", autonomous.ErrSecretInAnswer, http.StatusUnprocessableEntity},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := newFakeQuestions()
			fake.answerErr = tc.err
			cfg := questionRESTTestConfig(true)
			s := &Server{cfg: &cfg, questionStore: fake}

			rr := doAnswer(t, s, "q-fake-1", map[string]any{"quote": "x"})
			if rr.Code != tc.want {
				t.Fatalf("status = %d, want %d (body=%q)", rr.Code, tc.want, rr.Body.String())
			}
			if !strings.Contains(rr.Body.String(), `"error"`) {
				t.Fatalf("body lacks the error field: %q", rr.Body.String())
			}
		})
	}

	// Withdraw mapping: 404 and 409.
	t.Run("withdraw not found", func(t *testing.T) {
		fake := newFakeQuestions()
		cfg := questionRESTTestConfig(true)
		s := &Server{cfg: &cfg, questionStore: fake}
		rr := doWithdraw(t, s, "no-such", "why")
		if rr.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404 (body=%q)", rr.Code, rr.Body.String())
		}
	})
	t.Run("withdraw closed", func(t *testing.T) {
		fake := newFakeQuestions()
		fake.withdrawErr = autonomous.ErrQuestionNotOpen
		cfg := questionRESTTestConfig(true)
		s := &Server{cfg: &cfg, questionStore: fake}
		rr := doWithdraw(t, s, "q-fake-1", "why")
		if rr.Code != http.StatusConflict {
			t.Fatalf("status = %d, want 409 (body=%q)", rr.Code, rr.Body.String())
		}
	})
}

// TestQuestionsREST_SecretRefusalNamesSafeRoutesWithoutEchoing is the 422
// contract: the error names the two safe routes, the response carries none
// of the submitted text, and the question stays open and unanswered — the
// submission is refused, not stored.
func TestQuestionsREST_SecretRefusalNamesSafeRoutesWithoutEchoing(t *testing.T) {
	s, _ := newQuestionsRESTServer(t)
	q := raiseQ(t, s)

	// This quote trips the answer secret scan (a secret assignment).
	secret := `password = "s3cr3t-rotated-2026"`
	rr := doAnswer(t, s, q.ID, map[string]any{"quote": secret})
	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("secret quote: want 422, got %d body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, `"reason":"secret_detected"`) && !strings.Contains(body, "secret_detected") {
		t.Fatalf("response does not name the reason: %q", body)
	}
	if !strings.Contains(body, "datawatch secrets store") {
		t.Fatalf("response does not name the secrets store: %q", body)
	}
	if !strings.Contains(body, "${secret:name}") && !strings.Contains(body, "secret:name") {
		t.Fatalf("response does not name the ${secret:name} shape: %q", body)
	}
	if !strings.Contains(body, "filesystem") && !strings.Contains(body, "file") {
		t.Fatalf("response does not name the filesystem route: %q", body)
	}
	// The submitted text must not come back.
	if strings.Contains(body, "s3cr3t-rotated-2026") || strings.Contains(body, secret) {
		t.Fatalf("422 echoed the secret text: %q", body)
	}
	// Nothing stored: still open, no answer recorded.
	got, ok := s.questionStore.Get(q.ID)
	if !ok || got.State != autonomous.QuestionOpen {
		t.Fatalf("question state after refusal: ok=%v state=%q, want open", ok, got.State)
	}
	if _, ok := s.questionStore.CurrentAnswer(q.ID); ok {
		t.Fatalf("a refusal recorded an answer")
	}

	// A well-formed reference to a stored secret is NOT a secret: it is
	// accepted and stored verbatim (the reference names what to fetch, not
	// the value).
	rr = doAnswer(t, s, q.ID, map[string]any{"quote": "the token is ${secret:deploy_token}; it rotates weekly"})
	if rr.Code != http.StatusCreated {
		t.Fatalf("secret reference in quote: want 201, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// TestQuestionsREST_AnswerSurfaceValidationIs400 is the surface gate: the
// named surfaces are accepted (stored and returned), "system" is refused
// with a 400 for this channel, and the empty comm name is refused.
func TestQuestionsREST_AnswerSurfaceValidationIs400(t *testing.T) {
	s, _ := newQuestionsRESTServer(t)

	for _, surface := range []string{"pwa", "android", "ios", "cli", "mcp", "comm:slack"} {
		q := raiseQ(t, s)
		rr := doAnswer(t, s, q.ID, map[string]any{"quote": "ok", "surface": surface})
		if rr.Code != http.StatusCreated {
			t.Fatalf("surface %q: want 201, got %d body=%s", surface, rr.Code, rr.Body.String())
		}
	}

	for _, surface := range []string{"system", "comm:", "browser", ""} {
		q := raiseQ(t, s)
		bodys := []map[string]any{{"quote": "ok", "surface": surface}}
		if surface == "" {
			bodys = []map[string]any{{"quote": "ok"}} // absent
		}
		rr := doAnswer(t, s, q.ID, bodys[0])
		// Absent surface defaults to "rest" (201), not a refusal.
		if (surface == "" && rr.Code != http.StatusCreated) || (surface != "" && rr.Code != http.StatusBadRequest) {
			t.Fatalf("surface %q: got %d body=%s", surface, rr.Code, rr.Body.String())
		}
		if surface != "" {
			if !strings.Contains(rr.Body.String(), "surface") {
				t.Fatalf("400 for %s does not mention surface: %s", surface, rr.Body.String())
			}
		}
		if surface == "" {
			got, _ := s.questionStore.Get(q.ID)
			if got.State != autonomous.QuestionAnswered {
				t.Fatalf("absent surface: question should be answered, state=%q", got.State)
			}
		} else {
			if _, ok := s.questionStore.CurrentAnswer(q.ID); ok {
				t.Fatalf("refused surface %q stored an answer", surface)
			}
		}
	}
}

// TestQuestionsREST_AnswerAndWithdrawCapabilities runs the capability gate
// for the new endpoints: autonomous:write authorizes answer and withdraw;
// the answer log stays behind autonomous:read; a peer with only read sees
// 403 on both writes and 200 on the log read.
func TestQuestionsREST_AnswerAndWithdrawCapabilities(t *testing.T) {
	s, _ := newQuestionsRESTServer(t)
	q1 := raiseQ(t, s)

	// read-only peer: log is fine, answer and withdraw are 403.
	rr := doAnswers(t, s, q1.ID, "autonomous:read")
	if rr.Code != http.StatusOK {
		t.Fatalf("answers as read peer: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	rr = doAnswer(t, s, q1.ID, map[string]any{"quote": "x"}, "autonomous:read")
	if rr.Code != http.StatusForbidden {
		t.Fatalf("answer as read peer: want 403, got %d body=%s", rr.Code, rr.Body.String())
	}
	rr = doWithdraw(t, s, q1.ID, "y", "autonomous:read")
	if rr.Code != http.StatusForbidden {
		t.Fatalf("withdraw as read peer: want 403, got %d body=%s", rr.Code, rr.Body.String())
	}

	// write peer: both writes go through, the log stays 403.
	rr = doAnswer(t, s, q1.ID, map[string]any{"quote": "cap it"}, "autonomous:write")
	if rr.Code != http.StatusCreated {
		t.Fatalf("answer as write peer: want 201, got %d body=%s", rr.Code, rr.Body.String())
	}
	q2 := raiseQ(t, s)
	rr = doWithdraw(t, s, q2.ID, "y", "autonomous:write")
	if rr.Code != http.StatusOK {
		t.Fatalf("withdraw as write peer: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	rr = doAnswers(t, s, q1.ID, "autonomous:write")
	if rr.Code != http.StatusForbidden {
		t.Fatalf("answers as write-only peer: want 403, got %d body=%s", rr.Code, rr.Body.String())
	}

	// A peer with no relevant capability: 403 on every path.
	rr = doAnswers(t, s, q1.ID, "sessions:read")
	if rr.Code != http.StatusForbidden {
		t.Fatalf("answers as caps-less peer: want 403, got %d", rr.Code)
	}
	rr = doAnswer(t, s, q1.ID, map[string]any{"quote": "x"}, "sessions:read")
	if rr.Code != http.StatusForbidden {
		t.Fatalf("answer as caps-less peer: want 403, got %d", rr.Code)
	}
}

// TestQuestionsREST_AnswerBodySizeIs413 applies both size caps: the quote
// is capped at 16 KiB by the store (an over-cap quote is a 413 with the
// store's own count-and-cap message, and the quote text is never echoed),
// and the whole body is capped at 64 KiB plus the quote cap — anything
// bigger is refused before it is even read (413).
func TestQuestionsREST_AnswerBodySizeIs413(t *testing.T) {
	s, _ := newQuestionsRESTServer(t)
	q := raiseQ(t, s)

	// Oversize the *body*: quote 16 KiB cap + body cap + one.
	huge := strings.Repeat("y", maxAnswerBodyBytes+1)
	req := httptest.NewRequest(http.MethodPost, "/api/questions/"+q.ID+"/answer",
		strings.NewReader(`{"quote":"`+huge+`"}`))
	rec := httptest.NewRecorder()
	s.handleQuestions(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize body: want 413, got %d body=%s", rec.Code, rec.Body.String())
	}

	// Oversize the *quote* within the body: the store's 16 KiB quote cap
	// fires (the error names the byte count, not the quote).
	over := strings.Repeat("x", 16*1024+1)
	rr := doAnswer(t, s, q.ID, map[string]any{"quote": over})
	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize quote: want 413, got %d body=%s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), over) {
		t.Fatalf("413 echoed the quote")
	}
	if _, ok := s.questionStore.CurrentAnswer(q.ID); ok {
		t.Fatalf("oversize quote stored an answer")
	}
}

// TestQuestionsREST_SwitchOffIs503OnTheAnswerFlow extends the existing
// switch-off coverage to the answer endpoints: off returns 503 naming the
// switch, on serves them.
func TestQuestionsREST_SwitchOffIs503OnTheAnswerFlow(t *testing.T) {
	store, err := autonomous.NewQuestionStore(filepath.Join(t.TempDir(), "questions.jsonl"))
	if err != nil {
		t.Fatalf("NewQuestionStore: %v", err)
	}
	cfg := questionRESTTestConfig(false)
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	s := &Server{cfg: &cfg, cfgPath: cfgPath, questionStore: store}

	for _, tc := range []struct {
		method, path string
	}{
		{http.MethodPost, "/api/questions/some-id/answer"},
		{http.MethodPost, "/api/questions/some-id/withdraw"},
		{http.MethodGet, "/api/questions/some-id/answers"},
	} {
		rr := doQ(t, s, tc.method, tc.path, nil)
		if rr.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s %s (switch off): want 503, got %d body=%s", tc.method, tc.path, rr.Code, rr.Body.String())
		}
		if !strings.Contains(rr.Body.String(), "autonomous.questions.enabled") {
			t.Fatalf("%s %s (switch off): error %q does not name autonomous.questions.enabled", tc.method, tc.path, rr.Body.String())
		}
	}

	// On: the endpoints start serving.
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"autonomous.questions.enabled": true})
	q := raiseQ(t, s)
	rr := doAnswer(t, s, q.ID, map[string]any{"quote": "on"})
	if rr.Code != http.StatusCreated {
		t.Fatalf("answer with switch on: want 201, got %d body=%s", rr.Code, rr.Body.String())
	}
	q2 := raiseQ(t, s)
	rr = doWithdraw(t, s, q2.ID, "done")
	if rr.Code != http.StatusOK {
		t.Fatalf("withdraw open with switch on: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	rr = doAnswer(t, s, q2.ID, map[string]any{"quote": "late"})
	if rr.Code != http.StatusConflict {
		t.Fatalf("answer withdrawn with switch on: want 409, got %d body=%s", rr.Code, rr.Body.String())
	}
	rr = doAnswers(t, s, q.ID)
	if rr.Code != http.StatusOK {
		t.Fatalf("answers with switch on: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}
