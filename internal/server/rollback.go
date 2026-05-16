// BL29 — git checkpoint rollback REST.
//
//   POST /api/sessions/{id}/rollback
//   Body: {"force": false}   // force discards uncommitted changes
//
// Hard-resets the session's project_dir to the pre-session checkpoint
// tag (`datawatch-pre-{id}`). Refuses when uncommitted changes are
// present unless `force=true`.

package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/dmz006/datawatch/internal/session"
)

// handleSessionsSubpath dispatches /api/sessions/{id}/<verb> patterns
// that aren't covered by the exact-match registrations above. Used by
// BL29 rollback; future per-session subresources slot in here.
func (s *Server) handleSessionsSubpath(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/sessions/")
	switch {
	case strings.HasSuffix(rest, "/rollback"):
		s.handleSessionRollback(w, r)
	case strings.HasSuffix(rest, "/hook-event"):
		// alpha.34 #202 — hook scripts POST events here.
		s.handleSessionHookEvent(w, r)
	case strings.HasSuffix(rest, "/status"):
		// alpha.34 #202 — Status sub-tab fetches the derived board.
		s.handleSessionStatus(w, r)
	case strings.HasSuffix(rest, "/telemetry"):
		// BL303 S1 — structured session telemetry with task timings + verdicts.
		s.handleSessionTelemetry(w, r)
	case strings.HasSuffix(rest, "/guardrail"):
		// BL303 S3 T15 — on-demand guardrail invocation for a session.
		s.handleSessionGuardrail(w, r)
	case strings.HasSuffix(rest, "/input"):
		// #53 — send text input to a running session.
		s.handleSessionInput(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleSessionRollback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.manager == nil {
		http.Error(w, "manager not available", http.StatusServiceUnavailable)
		return
	}
	// Path: /api/sessions/{id}/rollback
	path := strings.TrimPrefix(r.URL.Path, "/api/sessions/")
	id := strings.TrimSuffix(path, "/rollback")
	if id == "" {
		http.Error(w, "session id required in path", http.StatusBadRequest)
		return
	}
	sess, ok := s.manager.GetSession(id)
	if !ok {
		http.Error(w, "session not found: "+id, http.StatusNotFound)
		return
	}
	if sess.ProjectDir == "" {
		http.Error(w, "session has no project_dir", http.StatusBadRequest)
		return
	}

	var req struct {
		Force bool `json:"force"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	pg := session.NewProjectGit(sess.ProjectDir)
	if err := pg.Rollback(sess.ID, req.Force); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":      "ok",
		"session_id":  sess.ID,
		"project_dir": sess.ProjectDir,
		"reset_to":    "datawatch-pre-" + sess.ID,
		"force":       req.Force,
	})
}

func (s *Server) handleSessionInput(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.manager == nil {
		http.Error(w, "manager not available", http.StatusServiceUnavailable)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/sessions/")
	id := strings.TrimSuffix(path, "/input")
	if id == "" {
		http.Error(w, "session id required in path", http.StatusBadRequest)
		return
	}
	var req struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.manager.SendInput(id, req.Text, "api"); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSONOK(w, map[string]any{"session_id": id, "sent": true})
}
