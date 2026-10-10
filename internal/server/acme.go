// BL397 — ACME / Let's Encrypt subsystem REST surface.
// See docs/plans/historical-plans/2026-10-06-bl397-native-acme-letsencrypt.md.
package server

import (
	"encoding/json"
	"net/http"

	"github.com/dmz006/datawatch/internal/federation"
)

// handleACMEStatus returns the current cert state for every configured
// domain. GET /api/acme/status
func (s *Server) handleACMEStatus(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapConfigRead) {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if s.acmeManager == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"enabled": false,
			"domains": []string{},
		})
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
		"enabled": true,
		"domains": s.acmeManager.Status(),
	})
}

// handleACMERenew forces a re-order for every configured domain (one
// multi-SAN order — there is no per-domain renew, matching Manager.
// IssueNow's design). POST /api/acme/renew
func (s *Server) handleACMERenew(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapConfigWrite) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if s.acmeManager == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"error": "acme not enabled"}) //nolint:errcheck
		return
	}
	if err := s.acmeManager.IssueNow(); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()}) //nolint:errcheck
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "domains": s.acmeManager.Status()}) //nolint:errcheck
}

// handleACMEVerify runs the pre-flight checks the BL397 plan calls for
// before a production order: DNS resolution of every configured domain,
// and reachability of the configured ACME directory endpoint. Does NOT
// perform an actual ACME order — this is meant to catch misconfiguration
// (name doesn't resolve, outbound blocked) before burning a rate-limited
// attempt. GET /api/acme/verify
func (s *Server) handleACMEVerify(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapConfigRead) {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if s.acmeManager == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"error": "acme not enabled"}) //nolint:errcheck
		return
	}
	json.NewEncoder(w).Encode(s.acmeManager.Verify(r.Context())) //nolint:errcheck
}
