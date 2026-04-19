// BL102 — Worker comm-channel proxy-send.
//
// S7.7 ships DATAWATCH_COMM_INHERIT (CSV of channel names the parent
// is running). BL102 wires the parent route the worker posts to:
//
//   POST /api/proxy/comm/{channel}/send
//   Body: {"recipient": "...", "message": "..."}
//
// `recipient` is optional — when empty the parent uses the channel's
// configured default group/room (signal group_id, telegram chat_id,
// etc.) as recorded by SetCommDefaults. The parent looks the channel
// name up in the registry installed via SetCommBackends and calls
// `backend.Send(recipient, message)`. 404 when the channel isn't
// active on this parent; 503 when no registry has been wired.

package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/dmz006/datawatch/internal/messaging"
)

// SetCommBackends wires the per-name comm-backend registry. Empty or
// nil disables the proxy endpoint (handler returns 503).
func (s *Server) SetCommBackends(b map[string]messaging.Backend) {
	s.commBackends = b
}

// SetCommDefaults wires the per-channel default recipient (group ID,
// channel ID, room, etc.) so workers can POST without knowing the
// parent's group config.
func (s *Server) SetCommDefaults(d map[string]string) {
	s.commDefaults = d
}

// handleCommProxySend serves POST /api/proxy/comm/{channel}/send.
func (s *Server) handleCommProxySend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.commBackends == nil {
		http.Error(w, "comm backends not configured", http.StatusServiceUnavailable)
		return
	}

	// Path: /api/proxy/comm/{channel}/send
	rest := strings.TrimPrefix(r.URL.Path, "/api/proxy/comm/")
	rest = strings.TrimSuffix(rest, "/send")
	channel := strings.TrimSuffix(rest, "/")
	if channel == "" || strings.Contains(channel, "/") {
		http.Error(w, "expected /api/proxy/comm/{channel}/send", http.StatusBadRequest)
		return
	}

	backend, ok := s.commBackends[channel]
	if !ok {
		http.Error(w, "channel not active on this parent: "+channel, http.StatusNotFound)
		return
	}

	var req struct {
		Recipient string `json:"recipient"`
		Message   string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Message == "" {
		http.Error(w, "message is required", http.StatusBadRequest)
		return
	}
	if req.Recipient == "" {
		req.Recipient = s.commDefaults[channel]
	}
	if req.Recipient == "" {
		http.Error(w,
			"no recipient supplied and no default configured for channel "+channel,
			http.StatusBadRequest)
		return
	}

	if err := backend.Send(req.Recipient, req.Message); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":   "sent",
		"channel":  channel,
		"backend":  backend.Name(),
	})
}
