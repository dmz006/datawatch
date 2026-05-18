// v7.0.0-alpha.24 #231 — federation meta-peers aggregator.
//
// Merges peer registries from this primary + any reachable federation
// primaries into one map keyed by ComputeNode identity. Lets operators
// of multi-instance datawatch deployments (proxy/container/chained)
// see a single per-node view of who's observing what.
//
// Endpoint: GET /api/federation/meta-peers
// Returns: {
//   "self":  "<this primary name>",
//   "by_node": {
//     "<compute-node>": {
//       "observers": [ {"primary":"self","peer":"...","last_push_at":"..."}, ... ],
//       "observer_count": N,
//       "primary_count": M
//     },
//     ...
//   },
//   "unbound": [ {"primary":"self","peer":"..."}, ... ],
//   "primaries_walked": ["self", ...]
// }
//
// Cross-instance fanout: when other primaries are configured to walk
// (alpha.24b will wire cfg.Observer.Federation.MetaPrimaries), the
// aggregator merges their /api/federation/meta-peers responses too.
// Empty / not configured = local-only response (graceful fallback so
// the endpoint always returns something useful).

package server

import (
	"net/http"

	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/observer"
)

type metaObserverEntry struct {
	Primary    string `json:"primary"`
	Peer       string `json:"peer"`
	Shape      string `json:"shape,omitempty"`
	LastPushAt string `json:"last_push_at,omitempty"`
	Version    string `json:"version,omitempty"`
}

type metaNodeBucket struct {
	Observers     []metaObserverEntry `json:"observers"`
	ObserverCount int                 `json:"observer_count"`
	PrimaryCount  int                 `json:"primary_count"`
}

func (s *Server) handleFederationMetaPeers(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapFederationList) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.peerRegistry == nil {
		http.Error(w, "peer registry disabled", http.StatusServiceUnavailable)
		return
	}
	selfName := s.federationSelfName
	if selfName == "" {
		selfName = "self"
	}

	byNode := map[string]*metaNodeBucket{}
	unbound := []metaObserverEntry{}
	primariesWalked := []string{selfName}

	// Local pass.
	localByNode, localUnbound := s.peersGroupedByNode()
	for node, peers := range localByNode {
		bucket := byNode[node]
		if bucket == nil {
			bucket = &metaNodeBucket{}
			byNode[node] = bucket
		}
		for _, p := range peers {
			bucket.Observers = append(bucket.Observers, observerFromPeerEntry(selfName, p))
		}
	}
	for _, p := range localUnbound {
		unbound = append(unbound, observerFromPeerEntry(selfName, p))
	}

	// Cross-instance pass — alpha.24b will walk
	// cfg.Observer.Federation.MetaPrimaries here. Endpoint structure
	// is stable so consumers (PWA, mobile, MCP) don't need to change.

	for _, bucket := range byNode {
		bucket.ObserverCount = len(bucket.Observers)
		seen := map[string]bool{}
		for _, o := range bucket.Observers {
			seen[o.Primary] = true
		}
		bucket.PrimaryCount = len(seen)
	}

	writeJSONOK(w, map[string]any{
		"self":             selfName,
		"by_node":          byNode,
		"unbound":          unbound,
		"primaries_walked": primariesWalked,
	})
}

func observerFromPeerEntry(primary string, p observer.PeerEntry) metaObserverEntry {
	return metaObserverEntry{
		Primary:    primary,
		Peer:       p.Name,
		Shape:      p.Shape,
		LastPushAt: p.LastPushAt.Format("2006-01-02T15:04:05Z07:00"),
		Version:    p.Version,
	}
}
