// SEC-016 (docs/plans/historical-plans/2026-08-28-security-assessment-core.md)
// — the admin bearer token (server.token) was only changeable through the
// generic PUT /api/config patch path, which writes the new value to disk
// but never updates the live process (fedAuthMiddleware keeps comparing
// against the in-memory value set at startup) — so rotating it either did
// nothing until a manual/auto restart, or, if auto_restart_on_config is
// set, cut off every client still holding the old token the instant the
// new process came up, with no transition window. POST /api/auth/rotate-token
// hot-swaps the live token immediately and keeps the previous one valid for
// a short grace window so in-flight clients aren't abruptly locked out.

package server

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// constantTimeEqualBytes reports whether a and b are equal, comparing in
// constant time when they're the same length (an explicit length check
// first is not itself a meaningful timing leak -- only byte-content
// comparison is -- see assessment T3 / federation_cap.go's identical
// convention for the admin-token check this replaces).
func constantTimeEqualBytes(a, b string) bool {
	return len(a) == len(b) && len(a) != 0 && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// tokenRotationGrace is how long the previous token keeps working after a
// rotation, so a client mid-request (or one that hasn't yet picked up a
// freshly-distributed new token) doesn't get cut off instantly.
const tokenRotationGrace = 60 * time.Second

// checkToken reports whether tok is the server's current admin bearer
// token, or the previous one within its grace window (SEC-016). Uses
// constant-time comparison, matching the original single-token check this
// replaces.
func (s *Server) checkToken(tok string) bool {
	s.tokenMu.RLock()
	defer s.tokenMu.RUnlock()
	if constantTimeEqualBytes(tok, s.token) {
		return true
	}
	if s.oldToken != "" && time.Now().Before(s.oldTokenExpiry) && constantTimeEqualBytes(tok, s.oldToken) {
		return true
	}
	return false
}

// handleRotateToken mints a new random admin bearer token (or applies an
// operator-supplied one), swaps it in live, persists it to config.yaml, and
// keeps the previous token valid for tokenRotationGrace. Admin-only: a
// federation peer or session-scoped credential must never be able to
// rotate the one credential everything else is checked against.
func (s *Server) handleRotateToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if peerFromContext(r.Context()) != nil || sessionCapsFromContext(r.Context()) != nil {
		http.Error(w, "admin token required to rotate the admin token", http.StatusForbidden)
		return
	}
	if s.cfg == nil || s.cfgPath == "" {
		http.Error(w, "config not available", http.StatusServiceUnavailable)
		return
	}

	var req struct {
		NewToken string `json:"new_token"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req) // empty/absent body is fine — generate one
	}

	newToken := req.NewToken
	if newToken != "" && len(newToken) < 16 {
		http.Error(w, "new_token must be at least 16 characters", http.StatusBadRequest)
		return
	}
	if newToken == "" {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			http.Error(w, "generate token: "+err.Error(), http.StatusInternalServerError)
			return
		}
		newToken = hex.EncodeToString(b)
	}

	s.tokenMu.Lock()
	previous := s.token
	s.oldToken = previous
	s.oldTokenExpiry = time.Now().Add(tokenRotationGrace)
	s.token = newToken
	s.tokenMu.Unlock()

	s.cfg.Server.Token = newToken
	if err := config.Save(s.cfg, s.cfgPath); err != nil {
		// Live token already swapped -- roll that back too, so the
		// in-memory and on-disk values don't diverge on a save failure.
		s.tokenMu.Lock()
		s.token = previous
		s.oldToken = ""
		s.oldTokenExpiry = time.Time{}
		s.tokenMu.Unlock()
		http.Error(w, "save config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"token":                      newToken,
		"previous_token_valid_until": s.oldTokenExpiry,
	})
}
