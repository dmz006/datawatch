// BL172 (S11) — REST surface for the Shape B / Shape C peer registry.
//
// Endpoints:
//
//	POST   /api/observer/peers                    register, mints a token
//	GET    /api/observer/peers                    list (TokenHash redacted)
//	DELETE /api/observer/peers/{name}             de-register / rotate
//	POST   /api/observer/peers/{name}/stats       push (Bearer auth)
//	GET    /api/observer/peers/{name}/stats       last-known snapshot
//
// Auth: bearer token returned by Register. HMAC-signed push lands in
// Task 3 alongside the peer-side push loop.

package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/dmz006/datawatch/internal/observer"
)

// PeerRegistryAPI is the narrow surface the REST layer needs from
// internal/observer.PeerRegistry, defined here so server tests can fake
// it without depending on bcrypt.
type PeerRegistryAPI interface {
	Register(name, shape, version string, hostInfo map[string]any) (token string, err error)
	Verify(name, token string) (*observer.PeerEntry, error)
	RecordPush(name string, snap *observer.StatsResponse) error
	Get(name string) (observer.PeerEntry, bool)
	LastPayload(name string) *observer.StatsResponse
	List() []observer.PeerEntry
	Delete(name string) error
}

// SetPeerRegistry — wired from main.go when Shape B/C peers are
// allowed (cfg.Observer.Peers.AllowRegister).
func (s *Server) SetPeerRegistry(r PeerRegistryAPI) { s.peerRegistry = r }

func (s *Server) handleObserverPeers(w http.ResponseWriter, r *http.Request) {
	if s.peerRegistry == nil {
		http.Error(w, "observer peer registry disabled (set observer.peers.allow_register)", http.StatusServiceUnavailable)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/observer/peers")
	rest = strings.TrimPrefix(rest, "/")

	if rest == "" {
		switch r.Method {
		case http.MethodGet:
			writeJSONOK(w, map[string]any{"peers": s.peerRegistry.List()})
		case http.MethodPost:
			s.handlePeerRegister(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	parts := strings.SplitN(rest, "/", 2)
	name := parts[0]
	action := ""
	if len(parts) == 2 {
		action = parts[1]
	}

	switch action {
	case "":
		switch r.Method {
		case http.MethodGet:
			entry, ok := s.peerRegistry.Get(name)
			if !ok {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			writeJSONOK(w, entry)
		case http.MethodDelete:
			if err := s.peerRegistry.Delete(name); err != nil {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			writeJSONOK(w, map[string]any{"status": "ok", "name": name})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	case "stats":
		switch r.Method {
		case http.MethodPost:
			s.handlePeerPush(w, r, name)
		case http.MethodGet:
			snap := s.peerRegistry.LastPayload(name)
			if snap == nil {
				http.Error(w, "no snapshot for peer", http.StatusNotFound)
				return
			}
			writeJSONOK(w, snap)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	default:
		http.Error(w, "unknown action: "+action, http.StatusBadRequest)
	}
}

// handlePeerRegister mints a token for the peer. Returns
// {token, name, shape}; the token is the only opportunity to capture
// it — the parent only stores the bcrypt hash.
func (s *Server) handlePeerRegister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name     string         `json:"name"`
		Shape    string         `json:"shape"`
		Version  string         `json:"version"`
		HostInfo map[string]any `json:"host_info"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}
	if req.Shape == "" {
		req.Shape = "B"
	}
	token, err := s.peerRegistry.Register(req.Name, req.Shape, req.Version, req.HostInfo)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSONOK(w, map[string]any{
		"name":  req.Name,
		"shape": req.Shape,
		"token": token,
	})
}

// handlePeerPush accepts a Shape B / C peer's stats push. Auth is
// bearer-token in this Phase A; HMAC signature verification is added
// in Task 3 alongside the peer-side push loop.
func (s *Server) handlePeerPush(w http.ResponseWriter, r *http.Request, name string) {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		http.Error(w, "missing bearer token", http.StatusUnauthorized)
		return
	}
	token := strings.TrimPrefix(auth, "Bearer ")
	if _, err := s.peerRegistry.Verify(name, token); err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	var body struct {
		Shape    string                   `json:"shape"`
		PeerName string                   `json:"peer_name"`
		Snapshot *observer.StatsResponse  `json:"snapshot"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if body.Snapshot == nil {
		http.Error(w, "snapshot required", http.StatusBadRequest)
		return
	}
	// Mismatched peer_name in the body is a bug or a misconfiguration;
	// reject so the operator notices.
	if body.PeerName != "" && body.PeerName != name {
		http.Error(w, "peer_name mismatch", http.StatusBadRequest)
		return
	}
	if err := s.peerRegistry.RecordPush(name, body.Snapshot); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSONOK(w, map[string]any{"status": "ok"})
}
