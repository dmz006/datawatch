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
	"sort"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/federation"
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
	if !s.fedCap(w, r, federation.CapObserversRead) {
		return
	}
	if s.peerRegistry == nil {
		http.Error(w, "observer peer registry disabled (set observer.peers.allow_register)", http.StatusServiceUnavailable)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/observer/peers")
	rest = strings.TrimPrefix(rest, "/")

	// v7.0.0-alpha.23b — list peers NOT bound to any ComputeNode.
	// "Free" = no Node references this peer via Name match OR explicit
	// ObserverPeer field. Used by PWA's free-observer picker on the
	// Compute Node Add/Edit form (Q5). Merged rows include pulled peers
	// and shadow rows (see mergedObserverPeers).
	if rest == "free" && r.Method == http.MethodGet {
		bound := s.peersBoundToNodes()
		free := []map[string]any{}
		for _, row := range s.mergedObserverPeers() {
			if name, _ := row["name"].(string); !s.peerRowBound(bound, name, row) {
				free = append(free, row)
			}
		}
		writeJSONOK(w, map[string]any{"peers": free})
		return
	}

	// v7.0.0-alpha.24 — group local peers by their bound ComputeNode.
	// Used by PWA's "Group by ComputeNode" toggle on the Federated
	// Peers card and by /api/federation/meta-peers for the merged
	// cross-instance view. Per #231: every observer/peer record
	// associates with a ComputeNode so the snapshot maps process →
	// ComputeNode → datawatch instance. Rows are the merged push+pull
	// set (see mergedObserverPeers), so pulled peers group here too.
	if rest == "by-node" && r.Method == http.MethodGet {
		byNode := map[string][]map[string]any{}
		unbound := []map[string]any{}
		for _, row := range s.mergedObserverPeers() {
			node, _ := row["compute_node"].(string)
			if node == "" {
				unbound = append(unbound, row)
				continue
			}
			byNode[node] = append(byNode[node], row)
		}
		writeJSONOK(w, map[string]any{"by_node": byNode, "unbound": unbound})
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
			out := []map[string]any{}
			if selfEntry := s.synthesizeSelfPeer(); selfEntry != nil {
				if name, _ := selfEntry["name"].(string); name != "" {
					selfEntry["source"] = "push"
					selfEntry["shadow"] = false
					selfEntry["pull"] = s.pullInfoFor(name)
				}
				out = append(out, selfEntry)
			}
			// alpha.24 #231 — enrich every peer with its bound
			// compute_node so consumers don't have to second-fetch
			// /api/compute/nodes to discover the binding. The merged set
			// also carries pulled peers (source="pull") and shadow rows
			// (federated, pull off) — see mergedObserverPeers.
			out = append(out, s.mergedObserverPeers()...)
			writeJSONOK(w, map[string]any{"peers": out})
		case http.MethodPost:
			if !s.fedCap(w, r, federation.CapObserversWrite) {
				return
			}
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
			// Pull-merge — the single-peer read carries source + the
			// pull object exactly like the list view; pre-existing
			// fields are unchanged.
			writeJSONOK(w, s.observerPeerRow(entry))
		case http.MethodDelete:
			if !s.fedCap(w, r, federation.CapObserversWrite) {
				return
			}
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
			if !s.fedCap(w, r, federation.CapObserversWrite) {
				return
			}
			s.handlePeerPush(w, r, name)
		case http.MethodGet:
			snap := s.peerRegistry.LastPayload(name)
			if snap == nil {
				// GATE alpha.36 (operator 2026-05-10): self-peer has
				// no pushed snapshot — it's the local instance. Fall
				// back to live local observer stats so clicking the
				// synthetic-self row shows real data instead of 404.
				if name == s.hostname && s.observerAPI != nil {
					s.handleObserverStats(w, r)
					return
				}
				http.Error(w, "no snapshot for peer (peer hasn't pushed stats yet)", http.StatusNotFound)
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
		"compute_node":  s.peerToNodeName(hostname),
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
	// Bind to an existing same-name ComputeNode; never creates a node
	// and never derives an address from the request.
	if s.computeReg != nil {
		_, _ = s.computeReg.BindObservedPeer(req.Name)
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
		Shape    string                  `json:"shape"`
		PeerName string                  `json:"peer_name"`
		Snapshot *observer.StatsResponse `json:"snapshot"`
		Chain    []string                `json:"chain,omitempty"`
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

// alpha.24 — peer ↔ ComputeNode helpers shared by /free and /by-node
// endpoints and by the federation meta-peers aggregator.

// peersBoundToNodes returns the set of peer names that ARE bound to
// some ComputeNode (via Name match OR explicit ObserverPeer). The
// inverse (peers NOT in this set) is the "free" set.
func (s *Server) peersBoundToNodes() map[string]bool {
	if s.computeReg == nil {
		return map[string]bool{}
	}
	return s.computeReg.BoundPeerNames()
}

// peerRowBound reports whether a merged peer row counts as bound to a
// ComputeNode for the /free view. Shadow rows (federated, observer_pull
// off, never pushed) never auto-bind by name match — they carry no data
// to bind and attaching them is rejected (409) — so only an explicit
// ObserverPeer reference on a Node binds them.
func (s *Server) peerRowBound(bound map[string]bool, peerName string, row map[string]any) bool {
	if shadow, _ := row["shadow"].(bool); shadow {
		return s.peerToNodeNameExplicit(peerName) != ""
	}
	return bound[peerName]
}

// peerToNodeNameExplicit is like peerToNodeName but WITHOUT the implicit
// name-match fallback: it returns the Node that explicitly references
// peerName in ObserverPeer, or "" if none.
func (s *Server) peerToNodeNameExplicit(peerName string) string {
	if s.computeReg == nil {
		return ""
	}
	for _, n := range s.computeReg.List() {
		if n.ObserverPeer == peerName {
			return n.Name
		}
	}
	return ""
}

// observerPeerAttachCheck classifies name for ComputeNode binding.
// bindable: a registered push peer, or a federated peer opted into
// observer pull (observer_pull on, enabled, URL set).
// conflict: a shadow peer — present in the federated peer store but
// not registered for push and not opted into pull (observer_pull off,
// no data). Shadow peers are rejected with HTTP 409 so the operator
// learns the peer carries no data to bind.
func (s *Server) observerPeerAttachCheck(name string) (bindable, conflict bool) {
	if s.peerRegistry != nil {
		if _, ok := s.peerRegistry.Get(name); ok {
			return true, false
		}
	}
	if s.serverStore != nil {
		if e, ok := s.serverStore.Get(name); ok && e != nil {
			if s.pullConfigured(name) {
				return true, false
			}
			return false, true // shadow: in the store but not bindable
		}
	}
	return false, false
}

// peerToNodeName returns the ComputeNode name that observes peerName,
// or "" if no Node is bound (peer is free). Explicit ObserverPeer
// references take priority over implicit name match.
func (s *Server) peerToNodeName(peerName string) string {
	if s.computeReg == nil {
		return ""
	}
	for _, n := range s.computeReg.List() {
		if n.ObserverPeer == peerName {
			return n.Name
		}
	}
	for _, n := range s.computeReg.List() {
		if n.Name == peerName {
			return n.Name
		}
	}
	return ""
}

// peersGroupedByNode returns (byNode, unbound). Each peer is placed
// under its bound ComputeNode key; peers with no binding land in
// unbound. Used by alpha.24 #231 per-node grouping UI.
func (s *Server) peersGroupedByNode() (map[string][]observer.PeerEntry, []observer.PeerEntry) {
	byNode := map[string][]observer.PeerEntry{}
	unbound := []observer.PeerEntry{}
	for _, p := range s.peerRegistry.List() {
		nodeName := s.peerToNodeName(p.Name)
		if nodeName == "" {
			unbound = append(unbound, p)
			continue
		}
		byNode[nodeName] = append(byNode[nodeName], p)
	}
	return byNode, unbound
}

// --- Pull-merge: pulled peers in the observer peer views --------------
//
// Every observable peer (pushed via the PeerRegistry or pulled by the
// observer.PullCollector) shows up exactly once in the observer peer
// endpoints. Each row carries a stable source field ("push" | "pull")
// plus a pull object with the pull protocol's opt-in state and the
// collector's last observed state, so the UI can distinguish a
// push-registered peer that is ALSO pulled from a pull-only peer and
// from a shadow peer (federated, pull off). Pre-existing row fields are
// unchanged; source/shadow/pull are additions.

// peerPullInfo is the pull object attached to every observer peer row.
// LastSuccessAt / LastAttemptAt are null while the collector has no
// record of a fetch yet.
type peerPullInfo struct {
	Enabled       bool       `json:"enabled"`
	Stale         bool       `json:"stale"`
	StaleReason   string     `json:"stale_reason"`
	LastSuccessAt *time.Time `json:"last_success_at,omitempty"`
	LastAttemptAt *time.Time `json:"last_attempt_at,omitempty"`
	IntervalS     int        `json:"interval_s"`
}

// pullConfigured reports whether the federation peer store has name opted
// in to the observer pull protocol — the same gate as the collector's
// target set: enabled, federated, observer_pull, URL present.
func (s *Server) pullConfigured(name string) bool {
	if s == nil || s.serverStore == nil {
		return false
	}
	e, ok := s.serverStore.Get(name)
	if !ok || e == nil {
		return false
	}
	return e.Enabled && e.Federated && e.ObserverPull && e.URL != ""
}

// pullIntervalS resolves name's effective pull interval in seconds: the
// entry's value when set, else the observer.pull global default.
func (s *Server) pullIntervalS(name string) int {
	if s != nil && s.serverStore != nil {
		if e, ok := s.serverStore.Get(name); ok && e != nil && e.ObserverPullIntervalS > 0 {
			return e.ObserverPullIntervalS
		}
	}
	return observerPullDefaults(s).DefaultIntervalS
}

// pullStateFor returns the pull collector's last recorded state for name.
func (s *Server) pullStateFor(name string) (observer.PullState, bool) {
	if s == nil || s.observerPull == nil || s.observerPull.col == nil {
		return observer.PullState{}, false
	}
	return s.observerPull.col.Last(name)
}

// pullCollectorDown reports whether name is opted in for pull while the
// collector is nil or stopped, so its stats are never refreshed.
func (s *Server) pullCollectorDown(name string) bool {
	if !s.pullConfigured(name) {
		return false
	}
	return s.observerPull == nil || s.observerPull.col == nil || !s.observerPull.col.Running()
}

// pullSnapshotFor returns the last successfully pulled snapshot for name,
// or nil when the pull collector has none.
func (s *Server) pullSnapshotFor(name string) *observer.StatsResponse {
	st, ok := s.pullStateFor(name)
	if !ok || st.LastSnapshot == nil {
		return nil
	}
	return st.LastSnapshot
}

// pulledPeerNames returns every peer the daemon pulls — configured for
// pull in the peer store, or already holding pull-collector state —
// sorted by name for deterministic views.
func (s *Server) pulledPeerNames() []string {
	set := map[string]bool{}
	if s != nil && s.observerPull != nil && s.observerPull.col != nil {
		for _, st := range s.observerPull.col.Snapshot() {
			set[st.Name] = true
		}
	}
	if s != nil && s.serverStore != nil {
		for _, e := range s.serverStore.ListFederated() {
			if e == nil || e.Name == "" {
				continue
			}
			if e.Enabled && e.Federated && e.ObserverPull && e.URL != "" {
				set[e.Name] = true
			}
		}
	}
	out := make([]string, 0, len(set))
	for name := range set {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// pullInfoFor builds the row's pull object for one peer. StaleReason is
// "none" when there is no failure to report.
func (s *Server) pullInfoFor(name string) peerPullInfo {
	info := peerPullInfo{StaleReason: observer.ReasonNone}
	if s.pullConfigured(name) {
		info.Enabled = true
		info.IntervalS = s.pullIntervalS(name)
	}
	if s.pullCollectorDown(name) {
		info.Stale = true
		info.StaleReason = observer.ReasonCollectorNotRunning
	}
	if st, ok := s.pullStateFor(name); ok {
		if !s.pullCollectorDown(name) {
			info.Stale = st.Stale
			if st.StaleReason != "" {
				info.StaleReason = st.StaleReason
			}
		}
		if !st.LastSuccessAt.IsZero() {
			t := st.LastSuccessAt
			info.LastSuccessAt = &t
		}
		if !st.LastAttemptAt.IsZero() {
			t := st.LastAttemptAt
			info.LastAttemptAt = &t
		}
	}
	return info
}

// observerPeerRow is one push-peer row in the merged observer peer list:
// every pre-existing field plus source, shadow and pull.
func (s *Server) observerPeerRow(p observer.PeerEntry) map[string]any {
	return map[string]any{
		"name":          p.Name,
		"shape":         p.Shape,
		"version":       p.Version,
		"host_info":     p.HostInfo,
		"registered_at": p.RegisteredAt,
		"last_push_at":  p.LastPushAt,
		"compute_node":  s.peerToNodeName(p.Name),
		"source":        "push",
		"shadow":        false,
		"pull":          s.pullInfoFor(p.Name),
	}
}

// mergedObserverPeers returns every observable peer exactly once as a row
// map, sorted by name. Push peers (PeerRegistry) keep every pre-existing
// field and gain source="push" plus their pull object; a pulled peer that
// never pushed appears once as a source="pull" row; a federated peer that
// neither pushes nor pulls (observer_pull off) surfaces once as a shadow
// row (shadow=true, source="pull", pull.enabled=false, no envelope data).
func (s *Server) mergedObserverPeers() []map[string]any {
	pushNames := map[string]bool{}
	rows := []map[string]any{}
	if s.peerRegistry != nil {
		for _, p := range s.peerRegistry.List() {
			pushNames[p.Name] = true
			rows = append(rows, s.observerPeerRow(p))
		}
	}
	// Pull pass — pulled peers that have no push registration.
	pulled := s.pulledPeerNames()
	pulledSet := map[string]bool{}
	for _, name := range pulled {
		pulledSet[name] = true
		if pushNames[name] {
			continue // the push row already carries this peer's pull object
		}
		row := map[string]any{
			"name":         name,
			"source":       "pull",
			"shadow":       false,
			"compute_node": s.peerToNodeName(name),
			"pull":         s.pullInfoFor(name),
		}
		if st, ok := s.pullStateFor(name); ok && !st.LastSuccessAt.IsZero() {
			row["last_push_at"] = st.LastSuccessAt
		}
		rows = append(rows, row)
	}
	// Shadow pass — federated peers with pull disabled that never pushed.
	// Listed for visibility; excluded from the all-peers envelope view
	// and the meta-peers buckets (no envelope data exists for them).
	if s.serverStore != nil {
		for _, e := range s.serverStore.ListFederated() {
			if e == nil || e.Name == "" || e.ObserverPull {
				continue
			}
			if pushNames[e.Name] || pulledSet[e.Name] {
				continue
			}
			rows = append(rows, map[string]any{
				"name":         e.Name,
				"source":       "pull",
				"shadow":       true,
				// Shadow rows never auto-bind by name match — they carry
				// no pushed data — so only an explicit ObserverPeer
				// reference counts here.
				"compute_node": s.peerToNodeNameExplicit(e.Name),
				"pull":         s.pullInfoFor(e.Name),
			})
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		a, _ := rows[i]["name"].(string)
		b, _ := rows[j]["name"].(string)
		return a < b
	})
	return rows
}
