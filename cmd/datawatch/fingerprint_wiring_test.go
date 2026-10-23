// Wiring tests for the fingerprint reconciliation (design §3c): the
// syncer must stay silent without opt-in, the capacity gate must block
// exactly on a confirmed shared-node match, fail open when the peer
// errors, and every operator decision must land in the audit log.

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	auditpkg "github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/compute"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/fingerprint"
	"github.com/dmz006/datawatch/internal/server/multiserver"
)

const testGPU = "GPU-TEST-UUID-1"

// localNodeExporter is a daemon-side exporter with one resolvable local
// node (same shape as the fingerprint package's test fixture): one
// kind=local node whose host reports testGPU.
func localNodeExporter(name, uuid string) *fingerprint.Exporter {
	return &fingerprint.Exporter{
		Nodes: &fingerprint.IdentityResolver{
			Nodes: func() []compute.Node {
				return []compute.Node{{Name: name, Kind: compute.KindLocal}}
			},
			Local: func() map[string]any {
				return map[string]any{"gpu_uuids": []string{uuid}}
			},
		},
	}
}

// peerFingerprintServer serves BOTH endpoints the wiring talks to, on one
// base URL: /api/federation/fingerprints (the bare items JSON the syncer
// pulls, after the bearer check) and /api/capacity (the pool list the
// gate polls). hits counts fingerprint-endpoint requests; capStatus is
// the status the capacity endpoint answers with.
func peerFingerprintServer(t *testing.T, peerExp *fingerprint.Exporter, token string, capStatus int, capPool string, capHeld int) (*httptest.Server, func() int64) {
	t.Helper()
	var hits int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/federation/fingerprints":
			atomic.AddInt64(&hits, 1)
			if got, want := r.Header.Get("Authorization"), "Bearer "+token; got != want {
				t.Errorf("authorization = %q, want %q", got, want)
				w.WriteHeader(http.StatusForbidden)
				return
			}
			key, err := fingerprint.PairKey(token)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode(peerExp.Export(key))
		case "/api/capacity":
			if capStatus != http.StatusOK {
				w.WriteHeader(capStatus)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"pools": []map[string]any{{"name": capPool, "held": capHeld}},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, func() int64 { return atomic.LoadInt64(&hits) }
}

// newTestPeerStore builds a persisted multiserver store with a single
// entry for the peer URL (opted in iff on).
func newTestPeerStore(t *testing.T, url, token string, on bool) *multiserver.Store {
	t.Helper()
	st, err := multiserver.NewStore(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := st.Add(&multiserver.Entry{Name: "buddy", URL: url, Token: token, FingerprintSync: on}); err != nil {
		t.Fatalf("store.Add: %v", err)
	}
	return st
}

// resetFingerprintGate isolates the package-level gate between tests.
func resetFingerprintGate(t *testing.T) {
	t.Helper()
	t.Cleanup(func() { fingerprintPeerGate = nil })
}

// confirmSharedMatch confirms the (exactly one) pending match for the
// local node and returns it.
func confirmSharedMatch(t *testing.T, store *fingerprint.MatchStore, localNode string) fingerprint.Match {
	t.Helper()
	pending := store.List(fingerprint.StatePending)
	if len(pending) != 1 {
		t.Fatalf("pending matches = %d (names %v), want exactly 1", len(pending), pending)
	}
	m, err := store.Confirm(pending[0].ID)
	if err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	if m.LocalName != localNode || m.Peer != "buddy" || m.Kind != fingerprint.KindNode {
		t.Fatalf("confirmed match = %+v, want local %q peer buddy kind %q", m, localNode, fingerprint.KindNode)
	}
	return m
}

func TestFingerprintWiringIdleWithoutOptIn(t *testing.T) {
	// The peer exists in the multiserver store but its entry is NOT
	// opted in: one full sync pass makes no HTTP request at all, records
	// no match, and the composed gate admits every node: pool — the
	// daemon behaves exactly as if the feature did not exist.
	resetFingerprintGate(t)

	local := localNodeExporter("gpu1", testGPU)
	peer := localNodeExporter("peerNode", testGPU) // shares the same GPU
	srv, hits := peerFingerprintServer(t, peer, "tok", http.StatusOK, "node:peerNode", 0)
	st := newTestPeerStore(t, srv.URL, "tok", false) // FingerprintSync off

	store, mem, path := newFingerprintMatchStore(&config.Config{DataDir: t.TempDir()})
	if mem {
		t.Fatalf("match store fell back to memory at %q", path)
	}
	syncer := newFingerprintSyncer(st, local, store)
	syncer.SyncOnce(context.Background())

	if got := hits(); got != 0 {
		t.Errorf("fingerprint endpoint hits = %d, want 0 (peer not opted in)", got)
	}
	if got := store.List(""); len(got) != 0 {
		t.Errorf("matches after unoptimized pass = %d, want 0", len(got))
	}
	// The gate is nil (no wiring ran with a store in this setup) and,
	// when consulted, admits everything.
	for _, pool := range []string{"node:gpu1", "host", "llm:gpt", "external:1"} {
		blocked, why := sharedNodeBackpressure()(pool)
		if blocked || why != "" {
			t.Errorf("gate blocked %s: %q, want not blocked (no opt-in)", pool, why)
		}
	}
}

func TestCapacityGateComposesPeerGate(t *testing.T) {
	// One opted-in peer sharing the same physical node, one CONFIRMED
	// match: the local hold (1 of 2) plus the peer's hold (1) meet the
	// limit, so the composed capacity gate blocks the node: pool only —
	// host/llm:/external pools are untouched.
	resetFingerprintGate(t)

	local := localNodeExporter("gpu1", testGPU)
	peer := localNodeExporter("peerNode", testGPU)
	srv, _ := peerFingerprintServer(t, peer, "tok", http.StatusOK, "node:peerNode", 1)
	st := newTestPeerStore(t, srv.URL, "tok", true)

	store := testFingerprintStore(t)
	newFingerprintSyncer(st, local, store).SyncOnce(context.Background())
	confirmSharedMatch(t, store, "gpu1")

	localHeld := func(pool string) (int, int) {
		if pool == "node:gpu1" {
			return 1, 2 // one local hold of two
		}
		return 0, 0
	}
	gate := newFingerprintGate(st, store, localHeld)
	fingerprintPeerGate = gate // sharedNodeBackpressure consults the package gate
	if why := gate.Reason("gpu1"); why == "" {
		t.Fatalf("gate admitted shared node with confirmed match; want blocked")
	}
	if why := gate.Reason("other-node"); why != "" {
		t.Errorf("gate blocked unrelated node %q, want admitted", why)
	}

	composed := sharedNodeBackpressure()
	blocked, why := composed("node:gpu1")
	if !blocked || why == "" {
		t.Fatalf("composed gate admitted node:gpu1 (blocked=%v why=%q), want blocked", blocked, why)
	}
	if !strings.HasPrefix(why, "shared node: ") {
		t.Errorf("wait reason = %q, want it to carry the shared-node wording", why)
	}
	for _, pool := range []string{"host", "llm:zeta", "external:pool-9"} {
		blocked, why := composed(pool)
		if blocked || why != "" {
			t.Errorf("composed gate blocked %s: %q, want not blocked (wrong pool kind)", pool, why)
		}
	}
}

func TestCapacityGateFailsOpenOnPeerError(t *testing.T) {
	// The confirmed shared-node peer answers its capacity poll with
	// HTTP 500: the gate must fail open — the peer contributes zero and
	// admission proceeds as if the match never existed.
	resetFingerprintGate(t)

	local := localNodeExporter("gpu1", testGPU)
	peer := localNodeExporter("peerNode", testGPU)
	srv, _ := peerFingerprintServer(t, peer, "tok", http.StatusServiceUnavailable, "node:peerNode", 99)
	st := newTestPeerStore(t, srv.URL, "tok", true)

	store := testFingerprintStore(t)
	newFingerprintSyncer(st, local, store).SyncOnce(context.Background())
	confirmSharedMatch(t, store, "gpu1")

	// Local hold alone would NOT fill the pool (1 of 2) — only the
	// peer's hold plus the local one would; with the peer unreachable
	// both readings must admit.
	localHeld := func(pool string) (int, int) {
		if pool == "node:gpu1" {
			return 1, 2
		}
		return 0, 0
	}
	gate := newFingerprintGate(st, store, localHeld)
	fingerprintPeerGate = gate // sharedNodeBackpressure consults the package gate
	if why := gate.Reason("gpu1"); why != "" {
		t.Fatalf("gate blocked on an unreachable peer: %q, want fail-open (admit)", why)
	}
	blocked, _ := sharedNodeBackpressure()("node:gpu1")
	if blocked {
		t.Errorf("composed gate blocked on peer error, want fail-open")
	}
}

func TestFingerprintWiringAuditsDecisions(t *testing.T) {
	// Every operator decision on the wiring's MatchStore lands in the
	// JSON-lines audit log with the fingerprint action names, both for
	// the confirm and the reject path (AGENT.md Audit Logging Rule — the
	// CEF side is covered in internal/audit/fingerprint_events_test.go).
	dir := t.TempDir()
	auditLog, err := auditpkg.New(dir)
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	setBackoffAuditLog(auditLog)
	t.Cleanup(func() { setBackoffAuditLog(nil) })

	store, mem, _ := newFingerprintMatchStore(&config.Config{DataDir: t.TempDir()})
	if mem {
		t.Fatalf("match store fell back to memory")
	}
	store.Observe("buddy", fingerprint.KindNode, "gpu1", "peerNode", "h1")
	store.Observe("buddy", fingerprint.KindBilling, "gpt", "peer-gpt", "h2")

	if _, err := store.Confirm(store.List(fingerprint.StatePending)[0].ID); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	rejected, err := store.Reject(store.List(fingerprint.StatePending)[0].ID)
	if err != nil {
		t.Fatalf("reject: %v", err)
	}

	_ = auditLog.Close()
	entries := readAuditEntries(t, filepath.Join(dir, "audit.log"))
	if len(entries) != 2 {
		t.Fatalf("audit entries = %d, want 2: %+v", len(entries), entries)
	}
	byAction := map[string]auditpkg.Entry{}
	for _, e := range entries {
		byAction[e.Action] = e
	}
	confirmed, ok := byAction[auditpkg.ActionFingerprintMatchConfirmed]
	if !ok {
		t.Fatalf("no %s audit entry: %+v", auditpkg.ActionFingerprintMatchConfirmed, entries)
	}
	if confirmed.Details["kind"] != fingerprint.KindNode || confirmed.Details["local"] != "gpu1" || confirmed.Details["peer"] != "buddy" {
		t.Errorf("confirm details = %v, want node/gpu1/buddy", confirmed.Details)
	}
	r, ok := byAction[auditpkg.ActionFingerprintMatchRejected]
	if !ok {
		t.Fatalf("no %s audit entry: %+v", auditpkg.ActionFingerprintMatchRejected, entries)
	}
	if r.Details["kind"] != fingerprint.KindBilling || rejected.State != fingerprint.StateRejected {
		t.Errorf("reject entry = %+v (record state %s), want billing-group record rejected", r, rejected.State)
	}
}

// testFingerprintStore builds the wiring's persisted MatchStore under a
// temp dir (with the wiring's OnDecision left unset — these tests are
// about the syncer/gate, not the audit hook).
func testFingerprintStore(t *testing.T) *fingerprint.MatchStore {
	t.Helper()
	s, err := fingerprint.NewMatchStore(filepath.Join(t.TempDir(), "matches.json"))
	if err != nil {
		t.Fatalf("NewMatchStore: %v", err)
	}
	return s
}

// readAuditEntries parses an audit JSON-lines file into Entry values.
func readAuditEntries(t *testing.T, path string) []auditpkg.Entry {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer func() { _ = f.Close() }()
	var out []auditpkg.Entry
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var e auditpkg.Entry
		if err := json.Unmarshal([]byte(sc.Text()), &e); err != nil {
			t.Fatalf("invalid audit line %q: %v", sc.Text(), err)
		}
		out = append(out, e)
	}
	return out
}
