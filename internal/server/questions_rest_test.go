// questions_rest_test.go — REST-level tests for /api/questions
// (POST raise, GET list, GET by id). Two store backings are exercised:
// the real autonomous.QuestionStore in a temp dir (the same store the
// daemon opens at <data_dir>/questions.jsonl) and a fake QuestionsAPI (the
// interface seam the daemon wires through SetQuestions). Together they
// cover the JSON wire shape, the status mapping (201/200/400/404/405/413/
// 503) and the store validation errors.

package server

import (
	"bytes"
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
	a.QuestionID = questionID
	f.current[questionID] = a
	f.counts[questionID]++
	return a, nil
}
func (f *fakeQuestions) Withdraw(id, actor, reason string) (autonomous.Question, error) {
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
	defer resp.Body.Close()
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

func TestQuestionsREST_ReservedPathsAre404(t *testing.T) {
	s, _ := newQuestionsRESTServer(t)

	// The answer paths and the withdraw path are reserved sub-paths of this
	// API (the store has the verbs; this surface does not): a fixed 404 for
	// every method — never a stub that returns success.
	paths := []string{
		"/api/questions/some-id/answer",
		"/api/questions/some-id/withdraw",
		"/api/questions/some-id/answers",
	}
	for _, path := range paths {
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
	defer resp.Body.Close()
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
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/questions/{id} via real mux: want 200, got %d (the original bug)", resp.StatusCode)
	}
}
