// BL104 — REST surface for the agents.PeerBroker.

package server

import (
	"encoding/json"
	"net/http"

	"github.com/dmz006/datawatch/internal/agents"
)

// SetPeerBroker wires the broker so the parent's REST endpoints can
// route worker peer messages. Optional: when nil the endpoints
// return 503.
func (s *Server) SetPeerBroker(b *agents.PeerBroker) { s.peerBroker = b }

// handlePeerSend (BL104) — POST /api/agents/peer/send
//
// Body: {"from":"<sender-id>","to":["<id>",…],"topic":"…","body":"…"}
//
// Returns {"delivered":N, "dropped":[…]}. Sender authorization
// (per-profile AllowPeerMessaging) is enforced inside broker.Send;
// validation errors come back as 4xx.
func (s *Server) handlePeerSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.peerBroker == nil {
		http.Error(w, "peer broker not enabled", http.StatusServiceUnavailable)
		return
	}
	var req struct {
		From  string   `json:"from"`
		To    []string `json:"to"`
		Topic string   `json:"topic"`
		Body  string   `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	delivered, dropped, err := s.peerBroker.Send(req.From, req.To, req.Topic, req.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"delivered": delivered,
		"dropped":   dropped,
	})
}

// handlePeerInbox (BL104) — GET /api/agents/peer/inbox?id=<recipient>
//
// Returns the recipient's queued messages and CLEARS the inbox
// (Drain semantics). Pass &peek=1 for non-destructive read.
func (s *Server) handlePeerInbox(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.peerBroker == nil {
		http.Error(w, "peer broker not enabled", http.StatusServiceUnavailable)
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "missing id parameter", http.StatusBadRequest)
		return
	}
	var msgs []agents.PeerMessage
	if r.URL.Query().Get("peek") == "1" {
		msgs = s.peerBroker.Peek(id)
	} else {
		msgs = s.peerBroker.Drain(id)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"recipient": id,
		"messages":  msgs,
	})
}
