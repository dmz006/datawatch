// GET /api/federation/fingerprints — the endpoint a peer calls to read
// this daemon's resource fingerprint hashes (the resource-fingerprint
// exchange from
// docs/plans/2026-10-14-concurrency-and-budget-design.md §3c).
//
// Order of checks follows the Federation-Parity Rule (AGENT.md): the
// method guard first, then the fedCap guard immediately after, then
// the endpoint-specific gates. Only a registered federation peer with
// federation:fingerprint granted AND FingerprintSync opted in reaches
// the answer — admin-token callers get 400, unopted-in peers get 403
// (both daemons must opt in). The key is derived from the bearer
// token the caller presented, so the answer is correct for exactly
// that peer pair and nothing raw ever crosses the wire.
package server

import (
	"net/http"
	"strings"

	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/fingerprint"
)

// SetFingerprintExporter wires the exporter behind
// GET /api/federation/fingerprints. Nil (or unwired) answers an empty
// list — same convention as the other optional collaborators (e.g.
// SetAlertRulesAPI).
func (s *Server) SetFingerprintExporter(x *fingerprint.Exporter) { s.fingerprintExporter = x }

func (s *Server) handleFederationFingerprints(w http.ResponseWriter, r *http.Request) {
	// Method guard.
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Federation-Parity Rule — capability guard immediately after the
	// method guard, before any other business logic.
	if !s.fedCap(w, r, federation.CapFederationFingerprint) {
		return
	}
	// The caller must be a registered federation peer: an admin-token
	// caller has no peer identity and cannot opt in, so it gets 400
	// (not the peer-facing 403s below).
	peer := peerFromContext(r.Context())
	if peer == nil {
		http.Error(w, "no peer identity for this token — /fingerprints is for federation peer tokens, not the admin token", http.StatusBadRequest)
		return
	}
	// Per-peer opt-in: a capability grant does not imply fingerprint
	// trust, and both daemons must have opted in for the exchange.
	if !peer.FingerprintSync {
		http.Error(w, "fingerprint exchange is not enabled for this peer", http.StatusForbidden)
		return
	}
	// Derive the pair key from the bearer token the caller presented
	// (the same material fedAuthMiddleware authenticated it with —
	// the shared secret both peers hold for this pair).
	token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if token == "" {
		http.Error(w, "bearer token required for fingerprint exchange", http.StatusUnauthorized)
		return
	}
	key, err := fingerprint.PairKey(token)
	if err != nil {
		http.Error(w, "cannot derive pair key for fingerprint exchange", http.StatusBadRequest)
		return
	}
	items := []fingerprint.Item{} // non-nil so JSON says [] not null
	if s.fingerprintExporter != nil {
		items = s.fingerprintExporter.Export(key)
	}
	writeJSONOK(w, map[string]any{"fingerprints": items})
}
