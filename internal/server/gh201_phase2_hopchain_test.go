// GH#201 Phase 2 — federation-hop origin-actor attribution.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/server/multiserver"
)

func gh201PeerFixture(t *testing.T) (s *Server, peerToken string) {
	t.Helper()
	s, store, _ := newFedTestServer(t)
	if err := store.Add(&multiserver.Entry{
		Name:         "daemon-a",
		Token:        "shared-ab-secret",
		URL:          "http://daemon-a:8080",
		Enabled:      true,
		Federated:    true,
		Capabilities: []string{"read-only"},
	}); err != nil {
		t.Fatalf("add peer: %v", err)
	}
	return s, "shared-ab-secret"
}

func TestGH201Phase2_FedAuthMiddleware_VerifiesValidHopChain(t *testing.T) {
	s, peerToken := gh201PeerFixture(t)
	accessLog, err := audit.NewAt(t.TempDir() + "/access.log")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = accessLog.Close() }()
	s.SetAccessLog(accessLog)

	mw := s.fedAuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	entry, err := federation.SignHop(nil, "Alice", "daemon-a", 1000, peerToken)
	if err != nil {
		t.Fatalf("SignHop: %v", err)
	}
	enc := federation.EncodeChain(federation.Chain{entry})

	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	req.Header.Set("Authorization", "Bearer "+peerToken)
	req.Header.Set(federation.HopChainHeader, enc)
	rr := httptest.NewRecorder()
	mw.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	entries, err := accessLog.Read(audit.QueryFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 access-log entry, got %d", len(entries))
	}
	e := entries[0]
	if e.Actor != "peer:daemon-a" {
		t.Errorf("actor = %q, want peer:daemon-a", e.Actor)
	}
	if got := e.Details["origin_actor"]; got != "Alice" {
		t.Errorf("origin_actor = %v, want Alice", got)
	}
	if _, ok := e.Details["hop_chain"]; !ok {
		t.Error("expected hop_chain in access-log details")
	}
	if _, invalid := e.Details["hop_chain_invalid"]; invalid {
		t.Error("a valid chain must not be flagged hop_chain_invalid")
	}
}

func TestGH201Phase2_FedAuthMiddleware_DropsTamperedHopChain(t *testing.T) {
	s, peerToken := gh201PeerFixture(t)
	accessLog, err := audit.NewAt(t.TempDir() + "/access.log")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = accessLog.Close() }()
	s.SetAccessLog(accessLog)

	mw := s.fedAuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Signed with the WRONG key — simulates a forged or stale-key chain.
	entry, err := federation.SignHop(nil, "Alice", "daemon-a", 1000, "not-the-real-secret")
	if err != nil {
		t.Fatalf("SignHop: %v", err)
	}
	enc := federation.EncodeChain(federation.Chain{entry})

	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	req.Header.Set("Authorization", "Bearer "+peerToken)
	req.Header.Set(federation.HopChainHeader, enc)
	rr := httptest.NewRecorder()
	mw.ServeHTTP(rr, req)

	// GH#201 Phase 2's core safety property: an invalid chain degrades
	// to Phase 1 behavior, never a rejected request.
	if rr.Code != http.StatusOK {
		t.Fatalf("a tampered hop-chain header must not itself cause a 401/403, got %d", rr.Code)
	}

	entries, err := accessLog.Read(audit.QueryFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 access-log entry, got %d", len(entries))
	}
	e := entries[0]
	if e.Actor != "peer:daemon-a" {
		t.Errorf("actor = %q, want peer:daemon-a (Phase 1 fallback)", e.Actor)
	}
	if _, ok := e.Details["origin_actor"]; ok {
		t.Error("a tampered chain must never surface an origin_actor claim")
	}
	if invalid, _ := e.Details["hop_chain_invalid"].(bool); !invalid {
		t.Error("expected hop_chain_invalid:true to be logged for a failed verification")
	}
}

func TestGH201Phase2_FedAuthMiddleware_NoHeaderMeansNoOriginActor(t *testing.T) {
	s, peerToken := gh201PeerFixture(t)
	accessLog, err := audit.NewAt(t.TempDir() + "/access.log")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = accessLog.Close() }()
	s.SetAccessLog(accessLog)

	mw := s.fedAuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	req.Header.Set("Authorization", "Bearer "+peerToken)
	rr := httptest.NewRecorder()
	mw.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	entries, _ := accessLog.Read(audit.QueryFilter{})
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if _, ok := entries[0].Details["origin_actor"]; ok {
		t.Error("no incoming chain means no origin_actor — Phase 1 behavior must be unchanged")
	}
	if _, ok := entries[0].Details["hop_chain_invalid"]; ok {
		t.Error("no incoming chain means no hop_chain_invalid flag either")
	}
}

func TestGH201Phase2_FedAuthMiddleware_NeverLogsThePeerToken(t *testing.T) {
	s, peerToken := gh201PeerFixture(t)
	accessLog, err := audit.NewAt(t.TempDir() + "/access.log")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = accessLog.Close() }()
	s.SetAccessLog(accessLog)

	mw := s.fedAuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	entry, err := federation.SignHop(nil, "Alice", "daemon-a", 1000, peerToken)
	if err != nil {
		t.Fatalf("SignHop: %v", err)
	}
	enc := federation.EncodeChain(federation.Chain{entry})

	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	req.Header.Set("Authorization", "Bearer "+peerToken)
	req.Header.Set(federation.HopChainHeader, enc)
	rr := httptest.NewRecorder()
	mw.ServeHTTP(rr, req)

	entries, _ := accessLog.Read(audit.QueryFilter{})
	raw, _ := json.Marshal(entries)
	if strings.Contains(string(raw), peerToken) {
		t.Fatalf("access log entry leaked the raw peer token: %s", raw)
	}
}

// TestGH201Phase2_TwoDaemonSimulation is the plan's required "live
// two-daemon test": daemon A and daemon B are two real *Server
// instances wired to each other exactly as production config would
// (A holds a federation.Entry for B with B's token; B's fedCap
// middleware is the actual code path, not a hand-rolled stand-in).
// Admin on A triggers an aggregated-sessions fetch, which forwards to
// B as a real HTTP request over a real httptest.Server listener; B's
// access log must show Alice (the admin on A) as origin_actor, not
// just "peer:daemon-a".
func TestGH201Phase2_TwoDaemonSimulation(t *testing.T) {
	// --- Daemon B: the receiver. ---
	bServer, bStore, _ := newFedTestServer(t)
	bServer.hostname = "daemon-b"
	bAccessLog, err := audit.NewAt(t.TempDir() + "/access.log")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = bAccessLog.Close() }()
	bServer.SetAccessLog(bAccessLog)
	const sharedABSecret = "ab-shared-secret-for-this-test"
	if err := bStore.Add(&multiserver.Entry{
		Name:         "daemon-a",
		Token:        sharedABSecret,
		URL:          "http://daemon-a.invalid",
		Enabled:      true,
		Federated:    true,
		Capabilities: []string{"read-only"},
	}); err != nil {
		t.Fatalf("B: add peer A: %v", err)
	}
	bMux := http.NewServeMux()
	bMux.HandleFunc("/api/sessions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("[]"))
	})
	bHTTP := httptest.NewServer(bServer.fedAuthMiddleware(bMux))
	defer bHTTP.Close()

	// --- Daemon A: the forwarder. Admin "Alice" hits A, which
	// aggregates sessions from peer B using B's shared token. ---
	aServer, _, _ := newFedTestServer(t)
	aServer.hostname = "daemon-a"
	aServer.cfg = &config.Config{
		Servers: []config.RemoteServerConfig{
			{Name: "daemon-b", URL: bHTTP.URL, Token: sharedABSecret, Enabled: true},
		},
	}

	aMw := aServer.fedAuthMiddleware(http.HandlerFunc(aServer.handleAggregatedSessions))
	aReq := httptest.NewRequest(http.MethodGet, "/api/sessions/aggregated", nil)
	aReq.Header.Set("Authorization", "Bearer admin-token") // newFedTestServer sets s.token = "admin-token"
	aRR := httptest.NewRecorder()
	aMw.ServeHTTP(aRR, aReq)
	if aRR.Code != http.StatusOK {
		t.Fatalf("A: aggregated-sessions request failed: %d %s", aRR.Code, aRR.Body.String())
	}

	// --- Assert on B's OWN access log: it must show Alice (A's admin
	// principal), not just peer:daemon-a. ---
	bEntries, err := bAccessLog.Read(audit.QueryFilter{})
	if err != nil {
		t.Fatal(err)
	}
	var sawOrigin bool
	for _, e := range bEntries {
		if e.Action != "http_access" {
			continue
		}
		if e.Actor != "peer:daemon-a" {
			continue
		}
		if origin, ok := e.Details["origin_actor"]; ok {
			if origin != "admin" {
				t.Errorf("B: origin_actor = %v, want admin (A's local principal)", origin)
			}
			sawOrigin = true
		}
	}
	if !sawOrigin {
		t.Fatalf("B never logged an origin_actor for A's forwarded request — hop chain didn't survive the real hop. Entries: %+v", bEntries)
	}
}
