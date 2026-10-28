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
	"github.com/dmz006/datawatch/internal/session"
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
// A non-pi session never returns progress — it 404s with
// "no session log available", even when a stale pi log for the same project
// dir could be located (BL647). A pi session's log is chosen by exact
// session-id match in the log file name first (interactive pi sessions are
// launched with --session-id, named from the session's recorded name), and
// falls back to the time/project-dir lookup for sessions that recorded no id
// (one-shot pi-prompt sessions).
//
// 200 → {"session_id","full_id","state", ...TaskProgress fields...}
// 404 → session not found, or the session has no log to read (non-pi
//
//	backends, pi log not yet flushed).
//
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
	// A non-pi session never has a pi log. 404 it instead of answering from
	// a stale pi log that happens to sit in the same project dir (BL647:
	// a claude-code session was served another session's numbers).
	if !session.IsPiBackendFamily(sess.BackendFamily) {
		http.Error(w, "no session log available", http.StatusNotFound)
		return
	}
	logPath, err := pilog.LocateLogFor(piSessionsRootServer(), sess.ProjectDir, sess.CreatedAt, sess.Name)
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
		SessionID string `json:"session_id"`
		FullID    string `json:"full_id"`
		State     string `json:"state"`
		LogPath   string `json:"log_path"`
		*autonomous.TaskProgress
	}{
		SessionID:    sess.ID,
		FullID:       sess.FullID,
		State:        string(sess.State),
		LogPath:      logPath,
		TaskProgress: snap,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp) //nolint:errcheck
}
