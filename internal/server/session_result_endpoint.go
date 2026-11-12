// Session result endpoint — REST half of the structured result panel.
//
// Endpoints:
//
//	POST /api/sessions/{id}/result            — agent posts its structured result
//	GET  /api/schedule-results                — grouped summaries (latest per schedule + sparkline data)
//	GET  /api/schedule-results/{schedule_name} — full history for one schedule
//
// Enforcement on the POST:
//  1. capability gate — sessions:result_write (fedCap; admin always passes);
//  2. identity gate — a session-scoped caller may only post for its OWN
//     session: the credential's minted session ID must equal the path id
//     (403 otherwise);
//  3. payload validation (session.ValidateResultPayload) — 400 with the
//     typed error otherwise;
//  4. store + board — if the session has a schedule_name the entry is
//     written to the ScheduleResultStore (retention-trimmed); the board
//     result is ALWAYS updated (sessions without a schedule show the
//     latest result in their own status page).

package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/metrics"
	"github.com/dmz006/datawatch/internal/session"
)

// SetScheduleResultStore wires the per-schedule result history store that
// backs the result endpoints. Called by the daemon at startup.
func (s *Server) SetScheduleResultStore(store *session.ScheduleResultStore) {
	s.scheduleResultStore = store
}

// scheduleResultsHistoryLimit returns the configured retention limit for
// the schedule result store, falling back to the default for servers
// built without a config (tests).
func (s *Server) scheduleResultsHistoryLimit() int {
	if s.cfg != nil {
		return s.cfg.Results.EffectiveScheduleHistoryLimit()
	}
	return session.ScheduleResultDefaultLimit
}

// rejectResult counts a rejected attempt and writes the error.
func (s *Server) rejectResult(w http.ResponseWriter, status int, err error) {
	metrics.SessionResultsRejected.Inc()
	if status == http.StatusBadRequest {
		http.Error(w, err.Error(), status)
		return
	}
	http.Error(w, http.StatusText(status), status)
}

// handleSessionResultPost implements POST /api/sessions/{id}/result.
// The session is resolved from the path; when it has a schedule_name the
// payload is written to the schedule result store (retention-trimmed),
// and the session's status board result is always updated.
func (s *Server) handleSessionResultPost(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// (1) Capability gate: sessions:result_write.
	if !s.fedCap(w, r, federation.CapSessionsResultWrite) {
		metrics.SessionResultsRejected.Inc()
		return
	}

	// Path session id.
	pathID := strings.TrimPrefix(r.URL.Path, "/api/sessions/")
	pathID = strings.TrimSuffix(pathID, "/result")
	pathID = strings.Trim(pathID, "/")

	// Resolve the session (also proves the id is real).
	if s.manager == nil {
		s.rejectResult(w, http.StatusServiceUnavailable, errors.New("session manager not available"))
		return
	}
	sess, ok := s.manager.GetSession(pathID)
	if !ok || sess == nil {
		s.rejectResult(w, http.StatusNotFound, errors.New("session not found"))
		return
	}

	// (2) Identity gate for session-scoped credentials: a session token
	// may only post results for its OWN session.
	if sessionCapsFromContext(r.Context()) != nil {
		tok := callerTokenFromContext(r.Context())
		if s.sessionTokens == nil {
			s.rejectResult(w, http.StatusForbidden, errors.New("session credential cannot be verified"))
			return
		}
		sid := s.sessionTokens.SessionIDForToken(tok)
		if sid == "" || (sid != sess.FullID && sid != sess.ID) {
			s.rejectResult(w, http.StatusForbidden, errors.New("session credential does not match path session"))
			return
		}
	}

	// (3) Decode + validate the payload.
	var payload session.ResultPayload
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&payload); err != nil {
		s.rejectResult(w, http.StatusBadRequest, err)
		return
	}
	if err := session.ValidateResultPayload(&payload); err != nil {
		s.rejectResult(w, http.StatusBadRequest, err)
		return
	}

	sessName := sess.ScheduleName

	// (4a) Schedule result store write — only when the session belongs
	// to a named schedule.
	stored := false
	if sessName != "" {
		if s.scheduleResultStore == nil {
			s.rejectResult(w, http.StatusServiceUnavailable, errors.New("schedule result store not available"))
			return
		}
		entry := &session.ScheduleResultEntry{
			Payload:      payload,
			SessionID:    sess.FullID,
			ScheduleName: sessName,
		}
		if err := s.scheduleResultStore.Add(entry, s.scheduleResultsHistoryLimit()); err != nil {
			s.rejectResult(w, http.StatusBadRequest, err)
			return
		}
		stored = true
	}

	// (4b) Board result — ALWAYS updated, regardless of schedule.
	boardID := sess.FullID
	if boardID == "" {
		boardID = sess.ID
	}
	globalHookStore.setResult(boardID, &payload)

	metrics.SessionResultsPosted.Inc()
	s.audit(r.Context(), "post_session_result", "session", sess.ID, map[string]any{
		"schedule_name": sessName,
		"stored":        stored,
		"status":        payload.Status,
		"title":         payload.Title,
	})

	writeJSONOK(w, map[string]any{
		"ok":            true,
		"session_id":    sess.ID,
		"full_id":       sess.FullID,
		"schedule_name": sessName,
		"stored":        stored,
		"count":         s.scheduleResultsCount(),
	})
}

func (s *Server) scheduleResultsCount() int {
	if s.scheduleResultStore == nil {
		return 0
	}
	return s.scheduleResultStore.Count()
}

// handleScheduleResultsList implements GET /api/schedule-results.
// Returns one summary per schedule (name-sorted) with the latest entry,
// the number of retained entries, and the recent status sequence that
// powers the status sparkline.
func (s *Server) handleScheduleResultsList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapAutonomousRead) {
		return
	}
	if s.scheduleResultStore == nil {
		http.Error(w, "schedule result store not available", http.StatusServiceUnavailable)
		return
	}

	names := make([]string, 0)
	summaries := s.scheduleResultStore.List()
	const recentStatusLimit = 50
	out := make([]map[string]any, 0, len(summaries))
	for _, sum := range summaries {
		names = append(names, sum.ScheduleName)
		hist := s.scheduleResultStore.History(sum.ScheduleName)
		statuses := make([]string, 0, len(hist))
		for _, e := range hist {
			statuses = append(statuses, e.Payload.Status)
		}
		if len(statuses) > recentStatusLimit {
			statuses = statuses[len(statuses)-recentStatusLimit:]
		}
		out = append(out, map[string]any{
			"schedule_name":   sum.ScheduleName,
			"latest":          sum.Latest,
			"count":           sum.Count,
			"recent_statuses": statuses,
		})
	}

	s.audit(r.Context(), "list_schedule_results", "schedule_results", "", map[string]any{"schedules": len(names)})
	writeJSONOK(w, out)
}

// handleScheduleResultsDispatch routes GET /api/schedule-results/{name}
// to the full-history handler (POST /api/schedule-results is not
// supported — the write path is the per-session result endpoint).
func (s *Server) handleScheduleResultsDispatch(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/api/schedule-results/")
	name = strings.Trim(name, "/")
	if name == "" {
		http.Error(w, "missing schedule name", http.StatusNotFound)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.handleScheduleResultHistory(w, r, name)
}

// handleScheduleResultHistory implements GET /api/schedule-results/{name}:
// the full retained history for one schedule, oldest to newest.
func (s *Server) handleScheduleResultHistory(w http.ResponseWriter, r *http.Request, name string) {
	if !s.fedCap(w, r, federation.CapAutonomousRead) {
		return
	}
	if s.scheduleResultStore == nil {
		http.Error(w, "schedule result store not available", http.StatusServiceUnavailable)
		return
	}

	hist := s.scheduleResultStore.History(name)
	if len(hist) == 0 {
		http.Error(w, "no results recorded for this schedule", http.StatusNotFound)
		return
	}

	s.audit(r.Context(), "get_schedule_results", "schedule_results", name, map[string]any{"entries": len(hist)})
	writeJSONOK(w, hist)
}
