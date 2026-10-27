// questions_rest_test.go — REST-level tests for /api/questions
// (POST raise, GET list, GET by id): they drive the real
// autonomous.QuestionStore (the same store the daemon runs at
// <data_dir>/questions.jsonl) through the handlers with httptest, so the
// JSON wire shape, the status mapping (201/200/400/404/405/503) and the
// store validation errors are all exercised.

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

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// newQuestionsRESTServer wires the real QuestionStore into a Server — the
// REST layer sits directly on the store, exactly like main.go.
func newQuestionsRESTServer(t *testing.T) (*Server, *autonomous.QuestionStore) {
	t.Helper()
	store, err := autonomous.NewQuestionStore(filepath.Join(t.TempDir(), "questions.jsonl"))
	if err != nil {
		t.Fatalf("NewQuestionStore: %v", err)
	}
	cfg := testQuestionsServerConfig()
	return &Server{
		questionStore: store,
		// The operator-questions switch on: REST is its surface.
		cfg: &cfg,
	}, store
}

// testQuestionsServerConfig is the same struct shape the daemon resolves:
// autonomous.questions.enabled is the switch the handler reads.
func testQuestionsServerConfig() config.Config {
	return config.Config{
		Autonomous: config.AutonomousConfig{
			Questions: config.AutonomousQuestionsConfig{Enabled: true},
		},
	}
}

// validQuestion raises a question that passes ValidateQuestion.
func validQuestion() map[string]any {
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
// recorder (status + body asserted by the test).
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

func TestQuestionsRaiseListGetRoundTrip(t *testing.T) {
	s, store := newQuestionsRESTServer(t)

	// POST -> 201, with an id assigned by the server.
	rr := doQ(t, s, http.MethodPost, "/api/questions", validQuestion())
	if rr.Code != http.StatusCreated {
		t.Fatalf("POST: want 201, got %d body=%s", rr.Code, rr.Body.String())
	}
	created := decodeQuestion(t, rr)
	if created.ID == "" {
		t.Fatalf("201 but no question id (body=%q)", rr.Body.String())
	}

	// GET (list) contains the new question.
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

func TestQuestionsRaiseDefaults(t *testing.T) {
	s, _ := newQuestionsRESTServer(t)

	// A raise without asked_by/asker_kind/context/label/risk defaults to
	// the caller (an operator in the test), the operator asker kind, and
	// the none context on the store record.
	b := validQuestion()
	delete(b, "label")
	delete(b, "context")
	delete(b, "asker_kind")
	delete(b, "asked_by")
	delete(b, "risk")
	rr := doQ(t, s, http.MethodPost, "/api/questions", b)
	if rr.Code != http.StatusCreated {
		t.Fatalf("POST: want 201, got %d body=%s", rr.Code, rr.Body.String())
	}
	got := decodeQuestion(t, rr)
	if got.Context != "none" {
		t.Fatalf("default context: %q, want %q", got.Context, "none")
	}
	if got.AskerKind != autonomous.QuestionAskerOperator {
		t.Fatalf("default asker kind: %q, want %q", got.AskerKind, autonomous.QuestionAskerOperator)
	}
	if got.AskedBy == "" {
		t.Fatalf("default asked_by empty")
	}
	if got.Label == "" {
		t.Fatalf("default label empty")
	}
}

func TestQuestionsRaiseValidationErrors(t *testing.T) {
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
		{"context prd needs a prd id", map[string]any{"text": "t", "default": "a", "options": []map[string]any{{"id": "a", "label": "A"}}, "context": "prd"}, "prd"},
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
	req := httptest.NewRequest(http.MethodPost, "/api/questions", bytes.NewReader([]byte("`not json")))
	rec := httptest.NewRecorder()
	s.handleQuestions(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed body: want 400, got %d body=%s", rec.Code, rec.Body.String())
	}
	if got := len(store.List(autonomous.QuestionFilter{})); got != 0 {
		t.Fatalf("malformed body stored a question: %d questions", got)
	}

	// An oversize body is refused (413), before it is read to the end.
	big := strings.Repeat("x", maxQuestionBodyBytes+1)
	req = httptest.NewRequest(http.MethodPost, "/api/questions", strings.NewReader(big))
	rec = httptest.NewRecorder()
	s.handleQuestions(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize body: want 413, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestQuestionsListFiltering(t *testing.T) {
	s, store := newQuestionsRESTServer(t)

	raise := func(mut func(map[string]any)) autonomous.Question {
		b := validQuestion()
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

	// Filtering narrows the list, and stays consistent with the store.
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

func TestQuestionsMethodNotAllowed(t *testing.T) {
	s, _ := newQuestionsRESTServer(t)

	rr := doQ(t, s, http.MethodPut, "/api/questions", validQuestion())
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("PUT list: want 405, got %d body=%s", rr.Code, rr.Body.String())
	}
	// By-id accepts GET only.
	rr = doQ(t, s, http.MethodPost, "/api/questions/some-id", validQuestion())
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST by id: want 405, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestQuestionsReservedPathsAre404(t *testing.T) {
	s, _ := newQuestionsRESTServer(t)

	// The answer paths and the withdraw path are reserved sub-paths of this
	// API (the store has the verbs; the surface does not): fixed 404 for
	// every method, the same for POST, GET and DELETE.
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

func TestQuestionsNilStoreIs503(t *testing.T) {
	cfg := testQuestionsServerConfig()
	s := &Server{cfg: &cfg} // nothing wired
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
			t.Fatalf("%s %s: want 503, got %d body=%s", tc.method, tc.path, rr.Code, rr.Body.String())
		}
	}
}

func TestQuestionsDisabledSwitchIs503(t *testing.T) {
	// The switch off (the default): a wired store still refuses service,
	// and the error names the switch it is gated on.
	s, _ := newQuestionsRESTServer(t)
	s.cfg.Autonomous.Questions.Enabled = false
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
}

func TestQuestionsNoCfgStillServed(t *testing.T) {
	// A test server built without any config (no switch to read) serves
	// the wired store — the disabled-state is the explicit switch=false,
	// not the absence of config.
	s, _ := newQuestionsRESTServer(t)
	s.cfg = nil

	b, _ := json.Marshal(validQuestion())
	req := httptest.NewRequest(http.MethodPost, "/api/questions", bytes.NewReader(b))
	rec := httptest.NewRecorder()
	s.handleQuestions(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST with cfg=nil: want 201, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestQuestionsFederationCaps(t *testing.T) {
	s, _ := newQuestionsRESTServer(t)
	body, _ := json.Marshal(validQuestion())

	// A peer without the autonomous caps is rejected on reads and writes
	// (403), before the handler even touches the store.
	peerReq := asPeer(httptest.NewRequest(http.MethodGet, "/api/questions", nil), "sessions:read")
	rec := httptest.NewRecorder()
	s.handleQuestions(rec, peerReq)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("GET as caps-less peer: want 403, got %d body=%s", rec.Code, rec.Body.String())
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

	// ...and autonomous:write does.
	peerReq = asPeer(httptest.NewRequest(http.MethodPost, "/api/questions", bytes.NewReader(body)), "autonomous:write")
	rec = httptest.NewRecorder()
	s.handleQuestions(rec, peerReq)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST as autonomous:write peer: want 201, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestQuestionsMuxRoutable is the regression for the ServeMux routing gap:
// the collection pattern "/api/questions" alone does not match
// "/api/questions/{id}", so GET-by-id 404s through the real mux even
// though the handler test (which calls the handler directly) passed.
// Registering the trailing-slash subtree pattern covers both forms.
func TestQuestionsMuxRoutable(t *testing.T) {
	// Structural guard: server.go must keep registering the
	// trailing-slash subtree pattern for the question routes; if it
	// disappears, GET /api/questions/{id} 404s from the real ServeMux.
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

	b, _ := json.Marshal(validQuestion())
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
