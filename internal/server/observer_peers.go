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
	"time"

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

	// v7.0.0-alpha.23b — list peers NOT bound to any ComputeNode.
	// "Free" = no Node references this peer via Name match OR explicit
	// ObserverPeer field. Used by PWA's free-observer picker on the
	// Compute Node Add/Edit form (Q5).
	if rest == "free" && r.Method == http.MethodGet {
		bound := map[string]bool{}
		if s.computeReg != nil {
			for _, n := range s.computeReg.List() {
				if n.ObserverPeer != "" {
					bound[n.ObserverPeer] = true
				}
				bound[n.Name] = true // implicit name-match counts as bound
			}
		}
		all := s.peerRegistry.List()
		free := make([]observer.PeerEntry, 0, len(all))
		for _, p := range all {
			if !bound[p.Name] {
				free = append(free, p)
			}
		}
		writeJSONOK(w, map[string]any{"peers": free})
		return
	}

	if rest == "" {
		switch r.Method {
		case http.MethodGet:
			// v7.0.0 — #184 federated peers self-as-peer. Synthesize
			// a "self" entry from the local observer.Collector so the
			// PWA's peers panel shows ALL hosts (local + remote) in
			// ONE table. Marked is_self=true so the UI can render a
			// distinct badge. Per operator's intuition: the local
			// host IS observing itself with the same observer
			// machinery as remote peers; visual consistency wins.
			peers := s.peerRegistry.List()
			out := make([]any, 0, len(peers)+1)
			if selfEntry := s.synthesizeSelfPeer(); selfEntry != nil {
				out = append(out, selfEntry)
			}
			for _, p := range peers {
				out = append(out, p)
			}
			writeJSONOK(w, map[string]any{"peers": out})
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
			// v7.0.0-alpha.15 (#238) — cascade-delete the
			// auto-derived ComputeNode that shares this peer's name
			// (created by the alpha.7 auto-link path). Operator-flagged
			// 2026-05-09: 17 leaked smoke-peer-* ComputeNodes from
			// smoke runs that deleted the peer but left the auto-Node.
			// Best-effort: ignore errors so peer-delete still succeeds
			// when the Node was already removed.
			if s.computeReg != nil {
				if node, err := s.computeReg.Get(name); err == nil && node != nil && node.AutoCreated {
					_ = s.computeReg.Delete(name)
				}
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

// synthesizeSelfPeer — v7.0.0 #184. Returns a synthetic peer entry
// for the local datawatch instance so /api/observer/peers shows
// ALL hosts (local + remote) in one table.
//
// Per operator 2026-05-09: "datawatch-observer is connected to local
// datawatch, and it provides stats and tree details, could it be
// considered a local federated peer and shouldn't there be details
// from it in observer/federated peers?"
//
// Marked `is_self: true` so PWA can render a distinct badge.
// Returns nil when observer not wired (no self-stats to surface).
func (s *Server) synthesizeSelfPeer() map[string]any {
	if s.observerAPI == nil {
		return nil
	}
	hostname := s.hostname
	if hostname == "" {
		hostname = "self"
	}
	now := time.Now().UTC()
	return map[string]any{
		"name":          hostname,
		"shape":         "A", // A = local self (B/C are remote peers)
		"is_self":       true,
		"version":       "local",
		"registered_at": now,
		"last_push_at":  now, // local — always live
		"host_info": map[string]any{
			"hostname": hostname,
			"role":     "self",
		},
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
	// v7.0.0 S1 — auto-create a matching ComputeNode entry on first
	// stats-peer registration so operators don't have to declare each
	// Node twice. Defaults: kind=remote (or k8s for Shape C),
	// max_concurrent_models=1. Operator edits via REST/MCP/CLI/comm/UI.
	if s.computeReg != nil {
		_, _, _ = s.computeReg.EnsureFromStatsPeer(req.Name, r.RemoteAddr, req.Shape)
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
		Chain    []string                 `json:"chain,omitempty"`
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
	// S14a federation loop prevention. If the chain already names
	// this primary, the snapshot has flowed back to its origin —
	// reject with 409 so the upstream pusher logs and stops.
	if self := s.federationSelfName; self != "" {
		for _, hop := range body.Chain {
			if hop == self {
				http.Error(w, "federation loop: chain already contains "+self, http.StatusConflict)
				return
			}
		}
	}
	if err := s.peerRegistry.RecordPush(name, body.Snapshot); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSONOK(w, map[string]any{"status": "ok"})
}

// SetFederationSelfName — main.go injects the local primary name
// (typically host name) so the peer-push handler can reject
// federation snapshots whose chain has already transited this
// primary (S14a loop prevention). Empty disables the check.
func (s *Server) SetFederationSelfName(name string) { s.federationSelfName = name }
