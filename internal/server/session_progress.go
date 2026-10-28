package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/pilog"
)

// piSessionsRootServer returns the directory under which pi stores its
// per-session JSONL logs: $PI_CODING_AGENT_DIR/sessions when that env is set,
// otherwise ~/.pi/agent/sessions. Same policy as cmd/datawatch's piSessionsRoot
// (pi_log_age.go), duplicated here so the server package can locate logs
// without a main-package import.
func piSessionsRootServer() string {
	if v := os.Getenv("PI_CODING_AGENT_DIR"); v != "" {
		return filepath.Join(v, "sessions")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".pi", "agent", "sessions")
}

// GET /api/sessions/{id}/progress
//
// Returns the live progress snapshot parsed from pi session's own JSONL log
// (how many assistant turns, which tools have been called and how often, the
// most recent action, cumulative tokens). Same shape as the per-task
// `progress` field that prd-get returns for an in-progress autonomous task
// (autonomous.TaskProgress), plus the session's own IDs, so a caller can
// answer "what is this session actually doing right now" without having to
// know the PRD or task at all.
//
// 200 → {"session_id","full_id","state", ...TaskProgress fields...}
// 404 → session not found, or the session has no log to read (non-pi
//       backends, pi log not yet flushed).
// 405 → any method but GET.
func (s *Server) handleSessionProgress(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapSessionsRead) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/sessions/")
	id := strings.TrimSuffix(path, "/progress")
	if id == "" {
		http.Error(w, "session id required", http.StatusBadRequest)
		return
	}
	sess, ok := s.manager.GetSession(id)
	if !ok {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	logPath, err := pilog.LocateLog(piSessionsRootServer(), sess.ProjectDir, sess.CreatedAt)
	if err != nil {
		http.Error(w, "no session log available", http.StatusNotFound)
		return
	}
	p, err := pilog.Read(logPath)
	if err != nil {
		http.Error(w, "failed to read session log", http.StatusInternalServerError)
		return
	}
	snap := autonomous.FromPilog(p)
	resp := struct {
		SessionID string   `json:"session_id"`
		FullID    string   `json:"full_id"`
		State     string   `json:"state"`
		LogPath   string   `json:"log_path"`
		*autonomous.TaskProgress
	}{
		SessionID:       sess.ID,
		FullID:          sess.FullID,
		State:           string(sess.State),
		LogPath:         logPath,
		TaskProgress:    snap,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp) //nolint:errcheck
}
