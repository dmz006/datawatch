// questions.go — the operator question channel over REST.
//
// Automation raises questions to operators through these endpoints; they
// are the raise + read surface:
//
//	POST /api/questions              — raise a question (needs autonomous:write)
//	GET  /api/questions              — list, filterable (needs autonomous:read)
//	GET  /api/questions/{id}         — one question + current answer metadata (autonomous:read)
//	POST /api/questions/{id}/answer  — record an answer (needs autonomous:write)
//	POST /api/questions/{id}/withdraw — withdraw an open question (autonomous:write)
//	GET  /api/questions/{id}/answers — the answer log, oldest first (autonomous:read)
//
// An operator authorized for the Automation may answer from any
// communication channel: answering needs only the write, so no separate
// answering capability exists. An answer is immutable; a changed mind is
// a new answer that supersedes the old one. An answer whose text looks
// like a secret is refused 422 with the safe routes named, is never
// stored, and the response never echoes any part of the submitted text.
//
// Every surface is gated on the autonomous.questions.enabled switch (off
// by default): while it is false the endpoints refuse service with a 503
// that names the switch. The store is wired at daemon start
// (cmd/datawatch/main.go) and is nil in unit tests that do not provide
// one — the handlers return 503 in that case.
package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/federation"
)

// maxQuestionBodyBytes caps a raise body. A question is one line of text
// plus a small option set; anything bigger is a mistake (or an attack)
// and is refused before the body is read to the end.
const maxQuestionBodyBytes = 64 * 1024

// maxAnswerBodyBytes caps an answer body: the same 64 KiB base plus the
// quote cap (16 KiB), the largest a stored quote may be.
const maxAnswerBodyBytes = maxQuestionBodyBytes + 16*1024

// maxWithdrawBodyBytes caps a withdraw body: it carries at most a reason
// and an actor, so the 64 KiB base cap on its own is far beyond what a
// real body needs.
const maxWithdrawBodyBytes = maxQuestionBodyBytes

// answerSurfaces are the surface names an answer may carry over REST.
// Anything else must be a comm channel ("comm:<channel name>").
var answerSurfaces = map[string]bool{
	"pwa":     true,
	"android": true,
	"ios":     true,
	"cli":     true,
	"rest":    true,
	"mcp":     true,
}

// validAnswerSurface reports whether surface is a surface this REST surface
// may record: one of the fixed names, or a comm channel with a non-empty
// channel name. "system" (and anything else) is not accepted over REST.
func validAnswerSurface(surface string) bool {
	if answerSurfaces[surface] {
		return true
	}
	return strings.HasPrefix(surface, "comm:") && len(surface) > len("comm:")
}

// writeJSONError writes a JSON error body with the given status.
func writeJSONError(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// QuestionsAPI is the operator question store as the REST layer sees it.
// autonomous.QuestionStore satisfies it; tests may provide a fake. Nil
// (the default) means the endpoints are not wired and return 503. The
// answer, withdraw and answers methods serve the answer paths under a
// question id (/answer, /withdraw, /answers).
type QuestionsAPI interface {
	Raise(q autonomous.Question) (autonomous.Question, error)
	Get(id string) (autonomous.Question, bool)
	List(filter autonomous.QuestionFilter) []autonomous.Question
	Answer(questionID string, a autonomous.Answer) (autonomous.Answer, error)
	Withdraw(id, actor, reason string) (autonomous.Question, error)
	Answers(questionID string) ([]autonomous.AnswerView, bool)
	CurrentAnswer(id string) (autonomous.Answer, bool)
	AnswerCount(id string) int
}

// raiseQuestionBody is the POST /api/questions body. Text, options, a
// valid context and default are required (the store rejects a question
// without them); an absent asker and asker kind default to the caller and
// "operator" (the automation does not always self-attest one). Context is
// passed through as-is and must name one of the known kinds: the store's
// validation returns a 400 naming the field when it does not, and there is
// no handler-side default.
type raiseQuestionBody struct {
	Label          string                             `json:"label"`
	AskedBy        string                             `json:"asked_by"`
	AskerKind      string                             `json:"asker_kind"`
	SessionID      string                             `json:"session_id"`
	Context        string                             `json:"context"`
	PRDID          string                             `json:"prd_id"`
	TaskID         string                             `json:"task_id"`
	ProposalPath   string                             `json:"proposal_path"`
	RegistryRowKey string                             `json:"registry_row_key"`
	Text           string                             `json:"text"`
	Options        []autonomous.QuestionOption        `json:"options"`
	Recommendation *autonomous.QuestionRecommendation `json:"recommendation"`
	Default        string                             `json:"default"`
	Risk           string                             `json:"risk"`
	// Priority is empty, normal or high (the store validates it and a value
	// outside the set is a 400 naming the field). ReturnPath opts this one
	// question in to the session line and, when true, the store requires a
	// session_id; an omitted value keeps the default return path (event plus
	// polled GET) only.
	Priority   string `json:"priority"`
	ReturnPath bool   `json:"return_path"`
}

// SetQuestions wires the operator question channel into the server for
// /api/questions (raise, list, get, answer, withdraw, answers). Nil
// disables the endpoints (handlers return 503).
func (s *Server) SetQuestions(api QuestionsAPI) {
	if s == nil {
		return
	}
	s.questionStore = api
}

// questionsPath maps a request path onto the one surface shape it names.
// kind is "collection" (POST raise / GET list), "get" (GET by id),
// "answer" (POST /{id}/answer), "withdraw" (POST /{id}/withdraw) or
// "answers" (GET /{id}/answers); id carries the question id for the by-id
// shapes ("" for the collection path, and for anything else below a
// question id, which is "reserved" and 404s — a fixed "not on this
// surface", the same for every method, so no client learns a false
// endpoint shape).
func questionsPath(path string) (kind, id string) {
	rest := strings.Trim(strings.TrimPrefix(path, "/api/questions"), "/")
	if rest == "" {
		return "collection", ""
	}
	id, sub, hasSub := strings.Cut(rest, "/")
	if id == "" {
		return "reserved", ""
	}
	if !hasSub {
		return "get", id
	}
	switch sub {
	case "answer":
		return "answer", id
	case "withdraw":
		return "withdraw", id
	case "answers":
		return "answers", id
	default:
		// {id}/{anything else} — not on this surface.
		return "reserved", ""
	}
}

func (s *Server) handleQuestions(w http.ResponseWriter, r *http.Request) {
	kind, qid := questionsPath(r.URL.Path)
	if kind == "reserved" {
		// A path beneath a question id that is not one of the named
		// shapes (/answer, /withdraw or /answers). 404 — a fixed "not
		// here", the same for every method.
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	// One method per shaped path; anything else is 405, so the shape is
	// learnable only through its real method.
	switch kind {
	case "collection":
		if r.Method != http.MethodPost && r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
	case "get", "answers":
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
	case "answer", "withdraw":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
	}

	// Federation parity: the reads (list, by id, the answer log) are
	// gated on autonomous:read and the writes (raise, answer, withdraw)
	// on autonomous:write, the same gate every other autonomous REST
	// surface applies. An operator authorized for the Automation may
	// answer from any communication channel: answering needs only the
	// write, so no separate answering capability exists. The local
	// (admin) caller passes as always.
	needed := federation.CapAutonomousRead
	if kind == "answer" || kind == "withdraw" || (kind == "collection" && r.Method == http.MethodPost) {
		needed = federation.CapAutonomousWrite
	}
	if !s.fedCap(w, r, needed) {
		return
	}

	// The operator-questions switch, read live: off by default, and a
	// PUT /api/config toggle takes effect on the next request without a
	// restart. While off the endpoints refuse service and name the switch.
	if s.cfg != nil && !s.cfg.Autonomous.Questions.Enabled {
		http.Error(w, "operator questions are disabled: set autonomous.questions.enabled to true", http.StatusServiceUnavailable)
		return
	}

	// Not wired (the daemon's store failed to open, or a test server
	// without one) — refuse rather than answer from nothing.
	if s.questionStore == nil {
		http.Error(w, "operator questions are not available", http.StatusServiceUnavailable)
		return
	}

	switch kind {
	case "get":
		s.handleQuestionGet(w, r, qid)
	case "answer":
		s.handleQuestionAnswer(w, r, qid)
	case "withdraw":
		s.handleQuestionWithdraw(w, r, qid)
	case "answers":
		s.handleQuestionAnswers(w, r, qid)
	case "collection":
		if r.Method == http.MethodPost {
			s.handleQuestionRaise(w, r)
			return
		}
		s.handleQuestionList(w, r)
	}
}

// handleQuestionRaise implements POST /api/questions.
func (s *Server) handleQuestionRaise(w http.ResponseWriter, r *http.Request) {
	// The body is capped with http.MaxBytesReader at 64 KiB: a question is
	// one line of text plus a small option set, so anything bigger is a
	// mistake (or an attack) and the read is refused before it runs to the
	// end (413).
	body := http.MaxBytesReader(w, r.Body, maxQuestionBodyBytes)

	var reqBody raiseQuestionBody
	dec := json.NewDecoder(body)
	if err := dec.Decode(&reqBody); err != nil {
		var maxSize *http.MaxBytesError
		if errors.As(err, &maxSize) {
			http.Error(w, "request body too large (cap 64 KiB)", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "invalid JSON body: "+err.Error(), http.StatusBadRequest)
		return
	}
	// Content after the object (a second JSON value or trailing junk) is a
	// malformed body, not a silently ignored duplicate.
	if _, err := dec.Token(); err != io.EOF {
		http.Error(w, "invalid JSON body: expected exactly one JSON object", http.StatusBadRequest)
		return
	}

	// The caller who raised it — an automation's session, the local
	// operator, or a peer daemon — is the asker of record when the body
	// does not self-attest one.
	actor := s.auditActor(r.Context())

	q := autonomous.Question{
		Label:          strings.TrimSpace(reqBody.Label),
		AskedBy:        strings.TrimSpace(reqBody.AskedBy),
		AskerKind:      autonomous.AskerKind(strings.TrimSpace(reqBody.AskerKind)),
		SessionID:      strings.TrimSpace(reqBody.SessionID),
		Context:        strings.TrimSpace(reqBody.Context),
		PRDID:          strings.TrimSpace(reqBody.PRDID),
		TaskID:         strings.TrimSpace(reqBody.TaskID),
		ProposalPath:   reqBody.ProposalPath,
		RegistryRowKey: strings.TrimSpace(reqBody.RegistryRowKey),
		Text:           reqBody.Text,
		Options:        reqBody.Options,
		Recommendation: reqBody.Recommendation,
		Default:        reqBody.Default,
		Risk:           strings.TrimSpace(reqBody.Risk),
		Priority:       strings.TrimSpace(reqBody.Priority),
		ReturnPath:     reqBody.ReturnPath,
	}
	if q.AskedBy == "" {
		q.AskedBy = actor
	}
	if q.AskerKind == "" {
		q.AskerKind = autonomous.QuestionAskerOperator
	}
	// An omitted context is deliberately NOT defaulted here: the body must
	// name one of the known contexts, and the store's validation is the one
	// that says so (400 naming the field).

	raised, err := s.questionStore.Raise(q)
	if err != nil {
		// A validation refusal is the caller's malformed question (400);
		// the message already names the field. An append or replay
		// failure on a valid question is a store fault (500).
		if errors.Is(err, autonomous.ErrInvalidQuestion) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		http.Error(w, "cannot store question: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// The store already wrote its audit entry for this action (the audit fn is
	// wired in main.go); the surface is the record's asked_by, so no second
	// line is added here.
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{"question": raised})
}

// handleQuestionList implements GET /api/questions.
func (s *Server) handleQuestionList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filter := autonomous.QuestionFilter{}

	if v := q.Get("state"); v != "" {
		if v != "all" && v != string(autonomous.QuestionOpen) && v != string(autonomous.QuestionAnswered) &&
			v != string(autonomous.QuestionWithdrawn) && v != string(autonomous.QuestionExpired) {
			http.Error(w, "unknown state: "+v+" (want one of open, answered, withdrawn, expired or all)", http.StatusBadRequest)
			return
		}
		if v != "all" {
			filter.State = autonomous.QuestionState(v)
		}
	}
	if v := q.Get("context"); v != "" {
		if v != "all" && v != autonomous.QuestionContextNone && v != autonomous.QuestionContextPrd &&
			v != autonomous.QuestionContextTask && v != autonomous.QuestionContextProposal &&
			v != autonomous.QuestionContextRegistryRow {
			http.Error(w, "unknown context: "+v+" (want one of none, prd, task, proposal, registry_row or all)", http.StatusBadRequest)
			return
		}
		if v != "all" {
			filter.Context = v
		}
	}
	if v := q.Get("asker"); v != "" && v != "all" {
		filter.Asker = v
	}
	if v := q.Get("text"); v != "" {
		filter.Text = v
	}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			http.Error(w, "limit must be a positive integer", http.StatusBadRequest)
			return
		}
		filter.Limit = n
	}

	questions := s.questionStore.List(filter)
	if questions == nil {
		questions = []autonomous.Question{}
	}
	writeJSONOK(w, map[string]any{
		"count":     len(questions),
		"questions": questions,
	})
}

// handleQuestionGet implements GET /api/questions/{id}.
func (s *Server) handleQuestionGet(w http.ResponseWriter, r *http.Request, id string) {
	id = strings.TrimSpace(id)
	q, ok := s.questionStore.Get(id)
	if !ok {
		http.Error(w, "unknown question", http.StatusNotFound)
		return
	}

	// An answered question carries, in the same read, the metadata a UI
	// needs to show "answered, N answers so far" without a second call:
	// the answer count (every answer ever recorded, including superseded
	// ones — they are decided, never deleted) and the currently
	// effective answer's id. The full answer log (text included) is its
	// own read: GET the question's answers path.
	count := s.questionStore.AnswerCount(id)
	currentID := ""
	if a, ok := s.questionStore.CurrentAnswer(id); ok {
		currentID = a.ID
	}

	writeJSONOK(w, map[string]any{
		"question":          q,
		"answer_count":      count,
		"current_answer_id": currentID,
	})
}

// answerQuestionBody is the POST /api/questions/{id}/answer body. At least
// one of chosen_option_id or a non-blank quote must be present (the store
// rejects an answer with neither). The quote is passed through exactly as
// received: no trimming, normalising or re-encoding.
type answerQuestionBody struct {
	ChosenOptionID  string `json:"chosen_option_id"`
	Quote           string `json:"quote"`
	Supersedes      string `json:"supersedes"`
	Surface         string `json:"surface"`
	SourceMessageID string `json:"source_message_id"`
	Actor           string `json:"actor"`
}

// handleQuestionAnswer implements POST /api/questions/{id}/answer.
func (s *Server) handleQuestionAnswer(w http.ResponseWriter, r *http.Request, id string) {
	// The body is capped at 64 KiB plus the quote cap: the body is a small
	// set of ids and at most a 16 KiB quote, so anything bigger is refused
	// before it is read to the end (413).
	body := http.MaxBytesReader(w, r.Body, maxAnswerBodyBytes)

	var reqBody answerQuestionBody
	dec := json.NewDecoder(body)
	if err := dec.Decode(&reqBody); err != nil {
		var maxSize *http.MaxBytesError
		if errors.As(err, &maxSize) {
			writeJSONError(w, http.StatusRequestEntityTooLarge,
				map[string]string{"error": "request body too large (cap 64 KiB plus the quote cap)"})
			return
		}
		writeJSONError(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body: " + err.Error()})
		return
	}
	// Content after the object is a malformed body.
	if _, err := dec.Token(); err != io.EOF {
		writeJSONError(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body: expected exactly one JSON object"})
		return
	}

	// Surface: defaults to this REST surface; must be a known surface name
	// or a comm channel ("comm:<channel name>"). "system" is not a surface
	// an operator answer arrives on, so it is refused here.
	surface := reqBody.Surface
	if surface == "" {
		surface = "rest"
	}
	if !validAnswerSurface(surface) {
		writeJSONError(w, http.StatusBadRequest, map[string]string{
			"error": "unknown surface \"" + surface + "\" (want one of pwa, android, ios, cli, rest, mcp, or comm:<channel name>)",
		})
		return
	}

	// The caller answering is the actor of record when the body does not
	// self-attest one (the same derivation as the raise handler's asker).
	actor := strings.TrimSpace(reqBody.Actor)
	if actor == "" {
		actor = s.auditActor(r.Context())
	}

	a := autonomous.Answer{
		Actor:           actor,
		Surface:         surface,
		ChosenOptionID:  reqBody.ChosenOptionID,
		Quote:           reqBody.Quote, // exactly as received
		Supersedes:      reqBody.Supersedes,
		SourceMessageID: reqBody.SourceMessageID,
	}

	stored, err := s.questionStore.Answer(id, a)
	if err != nil {
		// Every error here is safe to surface: the store's messages name
		// rule violations by id or field, and the secret refusal carries
		// only the rule name — never any of the submitted text.
		switch {
		case errors.Is(err, autonomous.ErrQuestionNotFound):
			writeJSONError(w, http.StatusNotFound, map[string]string{"error": "unknown question"})
		case errors.Is(err, autonomous.ErrQuestionNotOpen),
			errors.Is(err, autonomous.ErrAnswerNeedsSupersedes),
			errors.Is(err, autonomous.ErrSupersedesWrongTarget):
			writeJSONError(w, http.StatusConflict, map[string]string{"error": err.Error()})
		case errors.Is(err, autonomous.ErrAnswerEmpty),
			errors.Is(err, autonomous.ErrUnknownOption),
			errors.Is(err, autonomous.ErrQuoteNotUTF8):
			writeJSONError(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		case strings.Contains(err.Error(), "answer quote is"):
			// The store's quote-cap error carries no sentinel, so it is
			// matched on its stable prefix (store and handler are ours,
			// same module). The message names only the byte count, never
			// the quote.
			writeJSONError(w, http.StatusRequestEntityTooLarge, map[string]string{"error": err.Error()})
		case errors.Is(err, autonomous.ErrSecretInAnswer):
			// 422 with the reason and the two safe routes as fixed static
			// text; nothing of the submitter's text is echoed.
			writeJSONError(w, http.StatusUnprocessableEntity, map[string]any{
				"error":  autonomous.SecretRefusalMessage("secret pattern"),
				"reason": "secret_detected",
				"suggestion": []string{
					"store it in the datawatch secrets store and refer to it as ${secret:name}",
					"save it on the filesystem and point the session to the file",
				},
			})
		default:
			writeJSONError(w, http.StatusInternalServerError, map[string]string{"error": "cannot store answer: " + err.Error()})
		}
		return
	}

	// 201 with the stored answer record (quote hash included) and the
	// question's new state.
	q, ok := s.questionStore.Get(id)
	if !ok {
		q = autonomous.Question{ID: id, State: autonomous.QuestionAnswered}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{"answer": stored, "question": q})
}

// withdrawQuestionBody is the POST /api/questions/{id}/withdraw body.
type withdrawQuestionBody struct {
	Reason string `json:"reason"`
	Actor  string `json:"actor"`
}

// handleQuestionWithdraw implements POST /api/questions/{id}/withdraw.
func (s *Server) handleQuestionWithdraw(w http.ResponseWriter, r *http.Request, id string) {
	body := http.MaxBytesReader(w, r.Body, maxWithdrawBodyBytes)

	var reqBody withdrawQuestionBody
	dec := json.NewDecoder(body)
	if err := dec.Decode(&reqBody); err != nil {
		var maxSize *http.MaxBytesError
		if errors.As(err, &maxSize) {
			writeJSONError(w, http.StatusRequestEntityTooLarge,
				map[string]string{"error": "request body too large (cap 64 KiB)"})
			return
		}
		writeJSONError(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body: " + err.Error()})
		return
	}
	if _, err := dec.Token(); err != io.EOF {
		writeJSONError(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body: expected exactly one JSON object"})
		return
	}

	actor := strings.TrimSpace(reqBody.Actor)
	if actor == "" {
		actor = s.auditActor(r.Context())
	}

	q, err := s.questionStore.Withdraw(id, actor, reqBody.Reason)
	if err != nil {
		switch {
		case errors.Is(err, autonomous.ErrQuestionNotFound):
			writeJSONError(w, http.StatusNotFound, map[string]string{"error": "unknown question"})
		case errors.Is(err, autonomous.ErrQuestionNotOpen):
			writeJSONError(w, http.StatusConflict, map[string]string{"error": err.Error()})
		default:
			writeJSONError(w, http.StatusInternalServerError, map[string]string{"error": "cannot withdraw question: " + err.Error()})
		}
		return
	}

	writeJSONOK(w, map[string]any{"question": q})
}

// handleQuestionAnswers implements GET /api/questions/{id}/answers: the
// answer log, oldest first, each answer with its derived superseded state,
// plus the id of the currently effective answer ("" when none).
func (s *Server) handleQuestionAnswers(w http.ResponseWriter, r *http.Request, id string) {
	views, ok := s.questionStore.Answers(id)
	if !ok {
		writeJSONError(w, http.StatusNotFound, map[string]string{"error": "unknown question"})
		return
	}
	if views == nil {
		views = []autonomous.AnswerView{}
	}
	currentID := ""
	if a, ok := s.questionStore.CurrentAnswer(id); ok {
		currentID = a.ID
	}
	writeJSONOK(w, map[string]any{
		"answers":           views,
		"current_answer_id": currentID,
	})
}
