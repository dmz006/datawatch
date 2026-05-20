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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/federation"
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
	if !s.fedCap(w, r, federation.CapSessionsInput) {
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

	// BL316 S2 — cross-host routing: "<peer_name>/<session_id>/input"
	// If id contains a slash, the first segment is a peer name.
	if idx := strings.Index(id, "/"); idx != -1 {
		peerName := id[:idx]
		remoteSessionID := id[idx+1:]
		s.proxySessionInput(w, r, peerName, remoteSessionID)
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

// proxySessionInput forwards an input request to a named federation peer.
// Called when the URL path contains "/<peer_name>/<session_id>/input".
func (s *Server) proxySessionInput(w http.ResponseWriter, r *http.Request, peerName, sessionID string) {
	if s.serverStore == nil {
		http.Error(w, "server registry not configured", http.StatusServiceUnavailable)
		return
	}
	peer, ok := s.serverStore.Get(peerName)
	if !ok {
		http.Error(w, fmt.Sprintf("peer %q not found", peerName), http.StatusNotFound)
		return
	}
	if !peer.Federated {
		http.Error(w, fmt.Sprintf("%q is not a federation peer", peerName), http.StatusBadRequest)
		return
	}
	if peer.URL == "" {
		http.Error(w, fmt.Sprintf("peer %q has no URL", peerName), http.StatusBadGateway)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 64*1024))
	if err != nil {
		http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
		return
	}

	target := strings.TrimRight(peer.URL, "/") + "/api/sessions/" + sessionID + "/input"
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		http.Error(w, "build request: "+err.Error(), http.StatusInternalServerError)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if peer.Token != "" {
		req.Header.Set("Authorization", "Bearer "+peer.Token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		http.Error(w, "proxy error: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close() //nolint:errcheck
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(respBody)
}
