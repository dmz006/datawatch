// questions.go — the operator question channel over REST.
//
// Automation raises questions to operators through these endpoints; they
// are the raise + read surface:
//
//	POST /api/questions              — raise a question (needs autonomous:write)
//	GET  /api/questions              — list, filterable (needs autonomous:read)
//	GET  /api/questions/{id}         — one question + current answer metadata (autonomous:read)
//
// The answer path (POST /api/questions/{id}/answer), the answer log
// (GET  /api/questions/{id}/answers) and the withdraw path
// (POST /api/questions/{id}/withdraw) are reserved sub-paths of this
// API: they 404 here — a fixed "not on this surface", the same for every
// method, so no client learns a false endpoint shape.
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

// QuestionsAPI is the operator question store as the REST layer sees it.
// autonomous.QuestionStore satisfies it; tests may. Nil (default) means
// the endpoints are not wired and return 503.
type QuestionsAPI interface {
	Raise(q autonomous.Question) (autonomous.Question, error)
	Get(id string) (autonomous.Question, bool)
	List(filter autonomous.QuestionFilter) []autonomous.Question
	CurrentAnswer(id string) (autonomous.Answer, bool)
	AnswerCount(id string) int
}

// raiseQuestionBody is the POST /api/questions body. Text, options and
// default are required (the store rejects a question without them); the
// absent asker, asker kind and context default to the caller, "operator"
// and "none" (the automation does not always self-attest one).
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
}

// SetQuestions wires the operator question store into the server for
// /api/questions (raise, list, get). Nil disables the endpoints (handlers
// return 503).
func (s *Server) SetQuestions(api QuestionsAPI) {
	if s == nil {
		return
	}
	s.questionStore = api
}

// questionsPath maps a request path onto the one surface shape it names.
// kind is "collection" (POST raise / GET list) or "get" (GET by id); id
// carries the question id for the get shape ("" for anything else — the
// collection path or a reserved sub-path such as /answer, /answers or
// /withdraw; those 404 here).
func questionsPath(path string) (kind, id string) {
	rest := strings.Trim(strings.TrimPrefix(path, "/api/questions"), "/")
	if rest == "" {
		return "collection", ""
	}
	if !strings.Contains(rest, "/") {
		return "get", rest
	}
	// {id}/{anything} — the reserved answer paths and anything else
	// below a question id are not on this surface.
	return "reserved", ""
}

func (s *Server) handleQuestions(w http.ResponseWriter, r *http.Request) {
	kind, qid := questionsPath(r.URL.Path)
	if kind == "reserved" {
		// A path beneath a question id (/answer, /answers, /withdraw or
		// any other sub-path). 404 — a fixed "not here", the same for
		// every method, so no client learns a false endpoint shape.
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	if kind == "get" {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
	} else {
		switch r.Method {
		case http.MethodPost, http.MethodGet:
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
	}

	// Federation parity: reads are gated on autonomous:read and the raise
	// on autonomous:write, the same gate every other autonomous REST
	// surface applies; the local (admin) caller passes as always.
	needed := federation.CapAutonomousRead
	if r.Method == http.MethodPost {
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

	if kind == "get" {
		s.handleQuestionGet(w, r, qid)
		return
	}
	if r.Method == http.MethodPost {
		s.handleQuestionRaise(w, r)
		return
	}
	s.handleQuestionList(w, r)
}

// handleQuestionRaise implements POST /api/questions.
func (s *Server) handleQuestionRaise(w http.ResponseWriter, r *http.Request) {
	buf, err := io.ReadAll(io.LimitReader(r.Body, maxQuestionBodyBytes+1))
	if err != nil {
		http.Error(w, "cannot read request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if len(buf) > maxQuestionBodyBytes {
		http.Error(w, "request body too large (cap 64 KiB)", http.StatusRequestEntityTooLarge)
		return
	}

	var body raiseQuestionBody
	dec := json.NewDecoder(strings.NewReader(string(buf)))
	if err := dec.Decode(&body); err != nil {
		http.Error(w, "invalid JSON body: "+err.Error(), http.StatusBadRequest)
		return
	}
	// Trailing content after the object is a malformed body, not a
	// silently ignored duplicate.
	if dec.More() {
		http.Error(w, "invalid JSON body: content after the JSON object", http.StatusBadRequest)
		return
	}

	// The caller who raised it — an automation's session, the local
	// operator, or a peer daemon — is the asker of record when the body
	// does not self-attest one.
	actor := s.auditActor(r.Context())

	q := autonomous.Question{
		Label:          strings.TrimSpace(body.Label),
		AskedBy:        strings.TrimSpace(body.AskedBy),
		AskerKind:      autonomous.AskerKind(strings.TrimSpace(body.AskerKind)),
		SessionID:      strings.TrimSpace(body.SessionID),
		Context:        strings.TrimSpace(body.Context),
		PRDID:          strings.TrimSpace(body.PRDID),
		TaskID:         strings.TrimSpace(body.TaskID),
		ProposalPath:   body.ProposalPath,
		RegistryRowKey: strings.TrimSpace(body.RegistryRowKey),
		Text:           body.Text,
		Options:        body.Options,
		Recommendation: body.Recommendation,
		Default:        body.Default,
		Risk:           strings.TrimSpace(body.Risk),
	}
	if q.AskedBy == "" {
		q.AskedBy = actor
	}
	if q.AskerKind == "" {
		q.AskerKind = autonomous.QuestionAskerOperator
	}
	if q.Context == "" {
		q.Context = autonomous.QuestionContextNone
	}

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
	// the answer count (every answer ever recorded, including replaced
	// ones — they are decided, never deleted) and the currently
	// effective answer's id. The answer text itself stays in the
	// reserved answers path's lane.
	count := s.questionStore.AnswerCount(id)
	currentID := ""
	if a, ok := s.questionStore.CurrentAnswer(id); ok {
		currentID = a.ID
	}

	writeJSONOK(w, map[string]any{
		"question":          q,
		"count":             count,
		"current_answer_id": currentID,
	})
}
