// decision_records.go — the decision record routes over REST.
//
// A decision record is the structured description of one operator decision;
// the daemon raises the queue question from it. The routes:
//
//	POST /api/autonomous/decision-records               — create a record (autonomous:write)
//	GET  /api/autonomous/decision-records               — list, newest first, with the folder watch status (autonomous:read)
//	GET  /api/autonomous/decision-records/{id}          — one record (autonomous:read)
//	PUT  /api/autonomous/decision-records/{id}          — replace the content of an open record (autonomous:write)
//	POST /api/autonomous/decision-records/{id}/withdraw — withdraw a record and its question (autonomous:write)
//
// The service behind them is autonomous.DecisionRecords; it is wired at
// daemon start (cmd/datawatch/main.go) and is nil in unit tests that do not
// provide one, where every route answers 503. The two switches the service
// needs (autonomous.decisions.enabled and autonomous.questions.enabled) are
// read live by the service itself; a create or update while one is off is a
// 503 that names it.

package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/federation"
)

// DecisionRecordsAPI is the decision record service as the REST layer sees
// it. *autonomous.DecisionRecords satisfies it; tests may provide a fake. Nil
// (the default) means the routes are not wired and answer 503.
type DecisionRecordsAPI interface {
	Create(rec autonomous.DecisionRecord, source, sourceRef string) (autonomous.DecisionRecordView, []autonomous.QualityFinding, error)
	Update(id string, content autonomous.DecisionRecord) (autonomous.DecisionRecordView, []autonomous.QualityFinding, error)
	Withdraw(id, actor, reason string) (autonomous.DecisionRecordView, error)
	Get(id string) (autonomous.DecisionRecordView, bool)
	List(f autonomous.DecisionRecordFilter) []autonomous.DecisionRecordView
	FolderStatus() autonomous.DecisionFolderStatus
}

// SetDecisionRecords wires the decision record service into the server for
// /api/autonomous/decision-records. Nil (the default) disables the routes
// (they answer 503).
func (s *Server) SetDecisionRecords(api DecisionRecordsAPI) {
	if s == nil {
		return
	}
	s.decisionRecords = api
}

// SetDecisionRecords is the HTTPServer delegate, so main.go can wire it next
// to SetDecisionFollowUps with the same service object.
func (s *HTTPServer) SetDecisionRecords(api DecisionRecordsAPI) {
	if s == nil || s.api == nil {
		return
	}
	s.api.SetDecisionRecords(api)
}

// decisionRecordBody is the create and update body: the content fields of a
// record and nothing else. A field outside this set is a 400, so a caller can
// never set the state, the question link, the source or the answer.
type decisionRecordBody struct {
	Label          string                             `json:"label"`
	Title          string                             `json:"title"`
	Situation      string                             `json:"situation"`
	Deciding       string                             `json:"deciding"`
	Options        []autonomous.DecisionOption        `json:"options"`
	Recommendation *autonomous.DecisionRecommendation `json:"recommendation"`
	Default        string                             `json:"default"`
	Links          autonomous.DecisionLinks           `json:"links"`
}

func (b decisionRecordBody) record() autonomous.DecisionRecord {
	return autonomous.DecisionRecord{
		Label:          b.Label,
		Title:          b.Title,
		Situation:      b.Situation,
		Deciding:       b.Deciding,
		Options:        b.Options,
		Recommendation: b.Recommendation,
		Default:        b.Default,
		Links:          b.Links,
	}
}

// decisionRecordWithdrawBody is the optional withdraw body.
type decisionRecordWithdrawBody struct {
	Reason string `json:"reason"`
}

// decisionRecordView is a record on the wire: every field of the record
// (answer, doc_change and follow_up appear once they are set) plus the state
// of its queue question.
type decisionRecordView struct {
	autonomous.DecisionRecord
	QuestionState string `json:"question_state,omitempty"`
}

func newDecisionRecordView(v autonomous.DecisionRecordView) decisionRecordView {
	out := decisionRecordView{DecisionRecord: v.DecisionRecord, QuestionState: string(v.QuestionState)}
	// The answer is on the record once it is applied; until then (the daemon
	// poll has not run yet) it is read from the answered question.
	if out.Answer == nil && v.QuestionState == autonomous.QuestionAnswered && (v.AnsweredQuote != "" || v.AnsweredBy != "") {
		out.Answer = &autonomous.DecisionAnswer{
			Quote:      v.AnsweredQuote,
			Actor:      v.AnsweredBy,
			Surface:    v.AnsweredSurface,
			AnsweredAt: v.AnsweredAt,
		}
	}
	return out
}

// decisionRecordResponse is the body of the create, update, read and withdraw
// answers.
type decisionRecordResponse struct {
	Record     decisionRecordView        `json:"record"`
	QuestionID string                    `json:"question_id"`
	Warnings   *[]questionQualityFinding `json:"warnings,omitempty"`
}

// decisionRecordsPath maps a request path onto the one shape it names: kind
// is "collection", "item" (by id) or "withdraw"; anything else below the
// prefix is "reserved" and 404s the same way for every method.
func decisionRecordsPath(path string) (kind, id string) {
	rest := strings.Trim(strings.TrimPrefix(path, "/api/autonomous/decision-records"), "/")
	if rest == "" {
		return "collection", ""
	}
	id, sub, hasSub := strings.Cut(rest, "/")
	if id == "" {
		return "reserved", ""
	}
	if !hasSub {
		return "item", id
	}
	if sub == "withdraw" {
		return "withdraw", id
	}
	return "reserved", ""
}

func (s *Server) handleDecisionRecords(w http.ResponseWriter, r *http.Request) {
	kind, id := decisionRecordsPath(r.URL.Path)
	if kind == "reserved" {
		writeJSONError(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}

	// One or two methods per shape; anything else is a 405 that names them.
	var allowed []string
	switch kind {
	case "collection":
		allowed = []string{http.MethodGet, http.MethodPost}
	case "item":
		allowed = []string{http.MethodGet, http.MethodPut}
	case "withdraw":
		allowed = []string{http.MethodPost}
	}
	ok := false
	for _, m := range allowed {
		if r.Method == m {
			ok = true
		}
	}
	if !ok {
		w.Header().Set("Allow", strings.Join(allowed, ", "))
		writeJSONError(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	// Reads need autonomous:read, every change needs autonomous:write.
	needed := federation.CapAutonomousRead
	if r.Method != http.MethodGet {
		needed = federation.CapAutonomousWrite
	}
	if !s.fedCap(w, r, needed) {
		return
	}

	if s.decisionRecords == nil {
		writeJSONError(w, http.StatusServiceUnavailable, map[string]string{"error": "decision records are not available"})
		return
	}

	switch kind {
	case "collection":
		if r.Method == http.MethodPost {
			s.handleDecisionRecordCreate(w, r)
			return
		}
		s.handleDecisionRecordList(w, r)
	case "item":
		if r.Method == http.MethodPut {
			s.handleDecisionRecordUpdate(w, r, id)
			return
		}
		s.handleDecisionRecordGet(w, id)
	case "withdraw":
		s.handleDecisionRecordWithdraw(w, r, id)
	}
}

// decodeDecisionRecordJSON reads exactly one JSON object from the capped body
// into v. It answers the error itself and returns false when the body is too
// large, is not valid JSON, names a field outside v, or has anything after the
// object. With allowEmpty an empty body is accepted and leaves v as it is.
func decodeDecisionRecordJSON(w http.ResponseWriter, r *http.Request, v any, allowEmpty bool) bool {
	body := http.MaxBytesReader(w, r.Body, maxQuestionBodyBytes)
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var maxSize *http.MaxBytesError
		switch {
		case errors.As(err, &maxSize):
			writeJSONError(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "request body too large (cap 64 KiB)"})
		case allowEmpty && errors.Is(err, io.EOF):
			return true
		default:
			writeJSONError(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body: " + err.Error()})
		}
		return false
	}
	if _, err := dec.Token(); err != io.EOF {
		writeJSONError(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body: expected exactly one JSON object"})
		return false
	}
	return true
}

// writeDecisionRecordError answers the failure of a service call.
func writeDecisionRecordError(w http.ResponseWriter, err error, what string) {
	var unavailable *autonomous.ErrDecisionRecordsUnavailable
	var refused *autonomous.ErrDecisionClarityRefused
	switch {
	case errors.As(err, &unavailable):
		writeJSONError(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
	case errors.As(err, &refused):
		writeJSONError(w, http.StatusUnprocessableEntity, map[string]any{
			"error":    "decision record refused: it is not clear enough; fix the findings and send it again",
			"findings": questionQualityFindings(refused.Findings),
		})
	case errors.Is(err, autonomous.ErrInvalidDecisionRecord):
		writeJSONError(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	case errors.Is(err, autonomous.ErrDecisionRecordNotFound):
		writeJSONError(w, http.StatusNotFound, map[string]string{"error": "unknown decision record"})
	case errors.Is(err, autonomous.ErrDecisionRecordClosed),
		errors.Is(err, autonomous.ErrDecisionRecordDuplicate):
		writeJSONError(w, http.StatusConflict, map[string]string{"error": err.Error()})
	default:
		writeJSONError(w, http.StatusInternalServerError, map[string]string{"error": "cannot " + what + ": " + err.Error()})
	}
}

// writeDecisionRecordOK writes a record answer with the given status. A nil
// warnings leaves the field out; otherwise it is always an array.
func writeDecisionRecordOK(w http.ResponseWriter, status int, v autonomous.DecisionRecordView, warnings []autonomous.QualityFinding, withWarnings bool) {
	resp := decisionRecordResponse{Record: newDecisionRecordView(v), QuestionID: v.QuestionID}
	if withWarnings {
		items := questionQualityFindings(warnings)
		resp.Warnings = &items
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(resp)
}

// stalledWarning is the warning added when a record was stored but its
// question could not be brought in step; the next daemon poll retries.
func stalledWarning(err error) []autonomous.QualityFinding {
	return []autonomous.QualityFinding{{Code: "question_not_in_step", Message: err.Error()}}
}

// handleDecisionRecordCreate implements POST /api/autonomous/decision-records.
func (s *Server) handleDecisionRecordCreate(w http.ResponseWriter, r *http.Request) {
	var body decisionRecordBody
	if !decodeDecisionRecordJSON(w, r, &body, false) {
		return
	}
	view, findings, err := s.decisionRecords.Create(body.record(), autonomous.DecisionSourceREST, "")
	if err != nil {
		if view.ID != "" {
			// Stored, but the question is not in step yet: the record is
			// real, so it is answered 201 with the problem as a warning.
			writeDecisionRecordOK(w, http.StatusCreated, view, append(findings, stalledWarning(err)...), true)
			return
		}
		writeDecisionRecordError(w, err, "store decision record")
		return
	}
	writeDecisionRecordOK(w, http.StatusCreated, view, findings, true)
}

// handleDecisionRecordUpdate implements PUT /api/autonomous/decision-records/{id}.
func (s *Server) handleDecisionRecordUpdate(w http.ResponseWriter, r *http.Request, id string) {
	var body decisionRecordBody
	if !decodeDecisionRecordJSON(w, r, &body, false) {
		return
	}
	view, findings, err := s.decisionRecords.Update(strings.TrimSpace(id), body.record())
	if err != nil {
		if view.ID != "" {
			writeDecisionRecordOK(w, http.StatusOK, view, append(findings, stalledWarning(err)...), true)
			return
		}
		writeDecisionRecordError(w, err, "update decision record")
		return
	}
	writeDecisionRecordOK(w, http.StatusOK, view, findings, true)
}

// handleDecisionRecordGet implements GET /api/autonomous/decision-records/{id}.
func (s *Server) handleDecisionRecordGet(w http.ResponseWriter, id string) {
	view, ok := s.decisionRecords.Get(strings.TrimSpace(id))
	if !ok {
		writeJSONError(w, http.StatusNotFound, map[string]string{"error": "unknown decision record"})
		return
	}
	writeDecisionRecordOK(w, http.StatusOK, view, nil, false)
}

// handleDecisionRecordWithdraw implements
// POST /api/autonomous/decision-records/{id}/withdraw.
func (s *Server) handleDecisionRecordWithdraw(w http.ResponseWriter, r *http.Request, id string) {
	var body decisionRecordWithdrawBody
	if !decodeDecisionRecordJSON(w, r, &body, true) {
		return
	}
	view, err := s.decisionRecords.Withdraw(strings.TrimSpace(id), s.auditActor(r.Context()), body.Reason)
	if err != nil {
		writeDecisionRecordError(w, err, "withdraw decision record")
		return
	}
	writeDecisionRecordOK(w, http.StatusOK, view, nil, false)
}

// decisionFolderBody is the folder watch status on the wire.
type decisionFolderBody struct {
	Enabled   bool                               `json:"enabled"`
	Path      string                             `json:"path"`
	FilesSeen int                                `json:"files_seen"`
	Errors    []autonomous.DecisionFolderProblem `json:"errors"`
	LastScan  *time.Time                         `json:"last_scan"`
}

// handleDecisionRecordList implements GET /api/autonomous/decision-records.
// Filters, all exact and combinable: state (open, answered or withdrawn),
// prd_id (the record's Automaton link), limit (a positive whole number,
// default 100, capped at 1000), and the object tag selection — the
// repeatable tag / tag_id with the tag_match any|all rule and the
// project / automation / automaton shortcuts — matched against each
// record's system tags (a record naming no axes carries none and is
// absent from any tag-filtered result).
func (s *Server) handleDecisionRecordList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filter := autonomous.DecisionRecordFilter{AutomatonID: q.Get("prd_id")}
	if v := q.Get("state"); v != "" {
		switch st := autonomous.DecisionRecordState(v); st {
		case autonomous.DecisionRecordOpen, autonomous.DecisionRecordAnswered, autonomous.DecisionRecordWithdrawn:
			filter.State = st
		default:
			writeJSONError(w, http.StatusBadRequest, map[string]string{"error": `state must be one of "open", "answered" or "withdrawn"`})
			return
		}
	}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			writeJSONError(w, http.StatusBadRequest, map[string]string{"error": "limit must be a positive integer"})
			return
		}
		filter.Limit = n // the store caps it at 1000
	}
	tagSel, terr := objectTagSelectionFromRequest(r)
	if terr != nil {
		writeJSONError(w, http.StatusBadRequest, map[string]string{"error": terr.Error()})
		return
	}

	views := s.decisionRecords.List(filter)
	rows := make([]decisionRecordView, 0, len(views))
	for _, v := range views {
		if !tagSel.MatchesObjectTags(v.SystemTags, nil) {
			continue
		}
		rows = append(rows, newDecisionRecordView(v))
	}
	st := s.decisionRecords.FolderStatus()
	folder := decisionFolderBody{
		Enabled:   st.Enabled,
		Path:      st.Path,
		FilesSeen: st.FilesSeen,
		Errors:    st.Errors,
		LastScan:  st.LastScan,
	}
	if folder.Errors == nil {
		folder.Errors = []autonomous.DecisionFolderProblem{}
	}
	writeJSONOK(w, struct {
		DecisionRecords []decisionRecordView `json:"decision_records"`
		Folder          decisionFolderBody   `json:"folder"`
	}{rows, folder})
}
