// The resource-fingerprint reconciliation wiring (design §3c, see
// internal/fingerprint): the daemon builds ONE MatchStore under its data
// directory, hands an Exporter to the server for
// GET /api/federation/fingerprints, and runs the slow Syncer that pulls
// each OPTED-IN federation peer's fingerprint list once every 15 minutes
// (internal/fingerprint.DefaultSyncInterval). An admission-time gate makes
// a CONFIRMED shared-node match count the peer's usage of the same
// underlying node against the local limit; capacity_wiring.go composes
// that gate into the ledger's autonomous backpressure chain through
// sharedNodeBackpressure() — "node:" pools only, never the host/llm:/
// external pools, never interactive starts, and the gate itself fails open
// on peer errors.
//
// The gate reads CACHED peer usage (fingerprint.PeerUsageCache) and never
// makes a network call from the ledger callback: one cache is shared by the
// gate and the syncer, which refreshes it at the end of every sync pass.
//
// Everything is opt-in in both directions: the syncer makes no HTTP call
// for a peer whose entry is not FingerprintSync, and the gate blocks only
// for a match an operator CONFIRMED. With no opted-in peers and no
// confirmed matches the wiring is fully idle — identical behavior to
// before this feature exists.
package main

import (
	"context"
	"log"
	"path/filepath"

	auditpkg "github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/compute"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/fingerprint"
	"github.com/dmz006/datawatch/internal/inference"
	"github.com/dmz006/datawatch/internal/observerpeer"
	"github.com/dmz006/datawatch/internal/server"
	"github.com/dmz006/datawatch/internal/server/multiserver"
)

// fingerprintPeerGate is the admission-time gate the capacity admission
// path consults (sharedNodeBackpressure in capacity_wiring.go). Set once
// by wireFingerprint; nil means no server store — the gate contributes
// nothing, which is the fail-open default.
var fingerprintPeerGate *fingerprint.PeerGate

// fingerprintPath returns the MatchStore persistence path under the daemon
// data directory (the same DATAWATCH_DATA_DIR root as the other ledgers).
func fingerprintPath(dataDir string) string {
	return filepath.Join(expandHome(dataDir), "autonomous", "fingerprint", "matches.json")
}

// wireFingerprint builds the fingerprint reconciliation components and
// wires them in: the MatchStore (persisted under the data directory, so
// operator decisions survive restarts), the Exporter and MatchStore to
// the HTTP server, the PeerGate the capacity admission path consults, and
// the slow Syncer loop under ctx. A nil-capable daemon (no registry, no
// server store) still gets an idle wiring, not a broken one; the store is
// always non-nil and handed to the server.
func wireFingerprint(ctx context.Context, cfg *config.Config, httpServer *server.HTTPServer, led *capacity.Ledger) *fingerprint.MatchStore {
	store, inMemory, path := newFingerprintMatchStore(cfg)
	if inMemory {
		log.Printf("[fingerprint] WARN: persisting match store failed; continuing with in-memory store. Decisions this restart are still audited.")
	}

	if httpServer != nil {
		resolver := fingerprintResolver(httpServer)
		exporter := &fingerprint.Exporter{Nodes: resolver, LLMs: llmList(httpServer)}
		httpServer.SetFingerprintExporter(exporter)
		httpServer.SetFingerprintMatchStore(store)

		if ms := httpServer.ServerStore(); ms != nil {
			syncer := newFingerprintSyncer(ms, exporter, store)
			gate := newFingerprintGate(ms, store, fingerprintLocalHeld(led))
			syncer.AfterPass = gate.Usage.RefreshAll
			fingerprintPeerGate = gate
			go syncer.Run(ctx)
		}
	}

	log.Printf("[fingerprint] wired: store=%s interval=%s peers=%d",
		pathOrMemory(path, inMemory), fingerprint.DefaultSyncInterval, fingerprintPeerCount(httpServer))
	return store
}

// newFingerprintMatchStore returns the MatchStore and whether it is
// in-memory (persistence failed). A corrupt existing file is a real error
// the operator needs to see, but never one that takes the daemon down.
func newFingerprintMatchStore(cfg *config.Config) (*fingerprint.MatchStore, bool, string) {
	path := fingerprintPath(cfg.DataDir)
	store, err := fingerprint.NewMatchStore(path)
	if err != nil || store == nil {
		log.Printf("[fingerprint] WARN existing match store unreadable at %s (%v); continuing in-memory", path, err)
		store, _ = fingerprint.NewMatchStore("")
		return store, true, path
	}
	// Audit hook: every operator decision lands in the JSON-lines audit
	// log + CEF mirror, the same shape as the budget ceiling/resume events.
	store.OnDecision = func(m fingerprint.Match, action string) {
		emitBackoffAudit(fingerprintAuditAction(action), map[string]any{
			"kind":  m.Kind,
			"local": m.LocalName,
			"peer":  m.Peer,
		})
	}
	return store, false, path
}

// fingerprintResolver resolves each compute node's identity value from
// the node-side sources (a bound observer peer's reported HostInfo, else
// the local host's own report for kind=local nodes) — never the daemon-
// local name or address (§3c). A node with no identity source resolves to
// nothing and is simply not fingerprinted.
func fingerprintResolver(httpServer *server.HTTPServer) *fingerprint.IdentityResolver {
	reg := httpServer.ComputeRegistry()
	if reg == nil {
		return nil
	}
	return &fingerprint.IdentityResolver{
		Nodes: func() []compute.Node {
			nodes := make([]compute.Node, 0)
			for _, n := range reg.List() {
				if n != nil {
					nodes = append(nodes, *n)
				}
			}
			return nodes
		},
		HostInfo: func(peer string) map[string]any {
			peerReg := httpServer.PeerRegistry()
			if peerReg == nil {
				return nil
			}
			entry, ok := peerReg.Get(peer)
			if !ok {
				return nil
			}
			if entry.HostInfo != nil {
				return entry.HostInfo
			}
			return nil
		},
		Local: func() map[string]any {
			hostInfo := observerpeer.HostInfo()
			if hostInfo == nil {
				hostInfo = map[string]any{}
			}
			return hostInfo
		},
	}
}

// llmList returns a nil-safe LLM lister over the inference registry (the
// Exporter reads only Name and BillingGroup).
func llmList(httpServer *server.HTTPServer) func() []inference.LLM {
	return func() []inference.LLM {
		if httpServer == nil {
			return nil
		}
		reg := httpServer.InferenceRegistry()
		if reg == nil {
			return nil
		}
		var out []inference.LLM
		for _, l := range reg.List() {
			if l != nil {
				out = append(out, *l)
			}
		}
		return out
	}
}

// newFingerprintSyncer builds the slow exchange over the multiserver
// store. The store is re-read on every pass (nil-safe), so peers added or
// removed at runtime are picked up on the next tick; the FingerprintSync
// flag stays authoritative per entry — an off entry never makes a request.
func newFingerprintSyncer(store *multiserver.Store, local *fingerprint.Exporter, matches *fingerprint.MatchStore) *fingerprint.Syncer {
	return &fingerprint.Syncer{
		Peers:   fingerprintPeers(store),
		Local:   local,
		Matches: matches,
	}
}

// fingerprintPeers adapts a multiserver store to the syncer's peer
// lister. A nil store (multiserver disabled) yields no peers at all.
func fingerprintPeers(store *multiserver.Store) func() []multiserver.Entry {
	return func() []multiserver.Entry {
		if store == nil {
			return nil
		}
		var out []multiserver.Entry
		for _, e := range store.List() {
			if e != nil {
				out = append(out, *e)
			}
		}
		return out
	}
}

// fingerprintPeerRefs adapts the same store for the gate's narrower
// PeerRef view — the fingerprint package imports no multiserver types.
func fingerprintPeerRefs(store *multiserver.Store) func() []fingerprint.PeerRef {
	return func() []fingerprint.PeerRef {
		if store == nil {
			return nil
		}
		var out []fingerprint.PeerRef
		for _, e := range store.List() {
			if e == nil {
				continue
			}
			out = append(out, fingerprint.PeerRef{
				Name:    e.Name,
				URL:     e.URL,
				Token:   e.Token,
				OptedIn: e.FingerprintSync,
			})
		}
		return out
	}
}

// fingerprintLocalHeld reads the capacity ledger snapshot for a pool's
// held/limit (0,0 when the ledger or pool is absent — the gate then has
// no local limit and contributes nothing).
func fingerprintLocalHeld(led *capacity.Ledger) func(pool string) (int, int) {
	return func(pool string) (int, int) {
		if led == nil {
			return 0, 0
		}
		for _, p := range led.Snapshot().Pools {
			if p.Name == pool {
				return p.Held, p.Limit
			}
		}
		return 0, 0
	}
}

// newFingerprintGate builds the admission-time gate over the store, the
// match records and the ledger.
func newFingerprintGate(store *multiserver.Store, matches *fingerprint.MatchStore, localHeld func(pool string) (int, int)) *fingerprint.PeerGate {
	return &fingerprint.PeerGate{
		Matches:   matches,
		Peers:     fingerprintPeerRefs(store),
		LocalHeld: localHeld,
		Usage:     &fingerprint.PeerUsageCache{Peers: fingerprintPeerRefs(store)},
	}
}

// fingerprintAuditAction maps the store's decision actions to the audit
// action names (internal/audit/events.go); OnDecision is only ever called
// with confirm or reject.
func fingerprintAuditAction(action string) string {
	if action == fingerprint.ActionReject {
		return auditpkg.ActionFingerprintMatchRejected
	}
	return auditpkg.ActionFingerprintMatchConfirmed
}

// pathOrMemory renders the store path for logging ("(memory)" when the
// store is in-memory after a persistence failure).
func pathOrMemory(path string, mem bool) string {
	if mem {
		return path + " (in-memory)"
	}
	return path
}

// fingerprintPeerCount is the start-time peer count for the log line —
// resolved lazily so a store added after wireFingerprint still shows.
func fingerprintPeerCount(httpServer *server.HTTPServer) int {
	if httpServer == nil {
		return 0
	}
	if ms := httpServer.ServerStore(); ms != nil {
		return len(ms.List())
	}
	return 0
}
