// Tests for GET /api/federation/fingerprints — the endpoint a peer
// calls to read this daemon's resource fingerprint hashes
// (docs/plans/2026-10-14-concurrency-and-budget-design.md §3c).

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/compute"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/fingerprint"
	"github.com/dmz006/datawatch/internal/inference"
	"github.com/dmz006/datawatch/internal/server/multiserver"
)

// testFingerprintExporter is the shared responder-side fixture: one
// resolvable node (with a raw address the response must never leak)
// and one LLM in a shared billing group.
func testFingerprintExporter() *fingerprint.Exporter {
	return &fingerprint.Exporter{
		Nodes: &fingerprint.IdentityResolver{
			Nodes: func() []compute.Node {
				return []compute.Node{{Name: "alpha", Kind: compute.KindLocal, Address: "http://10.0.0.5:11434"}}
			},
			Local: func() map[string]any {
				return map[string]any{"gpu_uuids": []string{"uuid-1", "uuid-2"}}
			},
		},
		LLMs: func() []inference.LLM {
			return []inference.LLM{{Name: "claude-x", BillingGroup: "shared-plan-a"}}
		},
	}
}

// callFingerprints drives the handler the way the mux would: a GET
// (or the given method) with the given peer in context (nil = admin)
// and the given bearer token in the Authorization header.
func callFingerprints(t *testing.T, s *Server, peer *multiserver.Entry, method, token string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, "/api/federation/fingerprints", nil)
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	if peer != nil {
		r = r.WithContext(context.WithValue(r.Context(), fedPeerKey, peer))
	}
	rr := httptest.NewRecorder()
	s.handleFederationFingerprints(rr, r)
	return rr
}

func fingerprintPeer(name, token string, opts ...string) *multiserver.Entry {
	caps := []string{"federation-peer"}
	for _, cap := range opts {
		caps = append(caps, cap)
	}
	return &multiserver.Entry{
		Name:            name,
		URL:             "http://" + name + ":8080",
		Token:           token,
		Federated:       true,
		Enabled:         true,
		Capabilities:    caps,
		FingerprintSync: true,
	}
}

// TestFingerprintEndpointNeedsCapability — the method guard runs
// first (405), then the capability guard: a peer without
// federation:fingerprint gets 403, a full-control peer (which holds
// it) passes; with no exporter wired the answer is an empty list.
func TestFingerprintEndpointNeedsCapability(t *testing.T) {
	s := &Server{}

	rr := callFingerprints(t, s, fingerprintPeer("alpha", "tok", "full-control"), http.MethodPost, "tok")
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST: expected 405, got %d body=%s", rr.Code, rr.Body.String())
	}

	rr = callFingerprints(t, s, fingerprintPeer("beta", "tok"), http.MethodGet, "tok")
	if rr.Code != http.StatusForbidden {
		t.Fatalf("peer without the capability: expected 403, got %d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "capability: "+federation.CapFederationFingerprint) {
		t.Errorf("403 body should name the missing capability, got: %s", rr.Body.String())
	}

	rr = callFingerprints(t, s, fingerprintPeer("gamma", "tok", "full-control"), http.MethodGet, "tok")
	if rr.Code != http.StatusOK {
		t.Fatalf("full-control peer: expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var body struct {
		Fingerprints []fingerprint.Item `json:"fingerprints"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Fingerprints == nil {
		t.Errorf("no exporter wired: fingerprints must be an empty list, not null: %s", rr.Body.String())
	}
}

// TestFingerprintEndpointOptInRequired — the capability is not
// enough: the peer's registered entry must have FingerprintSync on,
// otherwise 403 with the exact opt-in message (both daemons must opt
// in for the exchange).
func TestFingerprintEndpointOptInRequired(t *testing.T) {
	s := &Server{}

	peer := fingerprintPeer("delta", "tok", "full-control")
	peer.FingerprintSync = false
	rr := callFingerprints(t, s, peer, http.MethodGet, "tok")
	if rr.Code != http.StatusForbidden {
		t.Fatalf("unopted-in peer: expected 403, got %d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "fingerprint exchange is not enabled for this peer") {
		t.Errorf("403 body must say 'fingerprint exchange is not enabled for this peer', got: %s", rr.Body.String())
	}

	peer.FingerprintSync = true
	rr = callFingerprints(t, s, peer, http.MethodGet, "tok")
	if rr.Code != http.StatusOK {
		t.Fatalf("opted-in peer: expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// TestFingerprintEndpointAdminTokenRejected — the exchange is
// peer-to-peer; an admin-token caller has no peer identity (nothing
// to opt in under) and gets 400, not the peer-facing 403s.
func TestFingerprintEndpointAdminTokenRejected(t *testing.T) {
	s := &Server{}

	rr := callFingerprints(t, s, nil, http.MethodGet, "admin-token")
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("admin token: expected 400, got %d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "no peer identity") {
		t.Errorf("400 body should explain the missing peer identity, got: %s", rr.Body.String())
	}
}

// TestFingerprintEndpointOnlyHashes — the 200 body carries the keyed
// hashes and the operator's own labels, but NEITHER the raw node
// identity, the node address, NOR the billing label.
func TestFingerprintEndpointOnlyHashes(t *testing.T) {
	s := &Server{fingerprintExporter: testFingerprintExporter()}
	peer := fingerprintPeer("alpha", "peer-token", "full-control")

	rr := callFingerprints(t, s, peer, http.MethodGet, "peer-token")
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	for _, raw := range []string{"uuid-1,uuid-2", "uuid-1", "10.0.0.5", "shared-plan-a"} {
		if strings.Contains(body, raw) {
			t.Errorf("response must not contain the raw value %q: %s", raw, body)
		}
	}
	key, err := fingerprint.PairKey("peer-token")
	if err != nil {
		t.Fatalf("PairKey: %v", err)
	}
	for _, want := range []string{nodeFingerprintHash(t, key), billingFingerprintHash(t, key)} {
		if !strings.Contains(body, want) {
			t.Errorf("response must contain hash %s: %s", want, body)
		}
	}
	// The operator's own labels are the only plain-text fields.
	for _, label := range []string{`"name":"alpha"`, `"name":"claude-x"`} {
		if !strings.Contains(body, label) {
			t.Errorf("response should carry the operator label %s", label)
		}
	}
}

// TestFingerprintEndpointKeyedPerCaller — the same node, read by two
// peers with different bearer tokens, yields DIFFERENT hashes for
// each (each is keyed under its own pair), and each matches the hash
// the caller would compute under its own token.
func TestFingerprintEndpointKeyedPerCaller(t *testing.T) {
	s := &Server{fingerprintExporter: testFingerprintExporter()}

	type peerView struct {
		Kind string
		Name string
		Hash string
	}
	readNodeHash := func(t *testing.T, peerName, token string) (string, string) {
		t.Helper()
		peer := fingerprintPeer(peerName, token, "full-control")
		rr := callFingerprints(t, s, peer, http.MethodGet, token)
		if rr.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d body=%s", peerName, rr.Code, rr.Body.String())
		}
		var body struct {
			Fingerprints []peerView `json:"fingerprints"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		for _, it := range body.Fingerprints {
			if it.Name == "alpha" {
				return it.Kind, it.Hash
			}
		}
		t.Fatalf("no fingerprint entry for node 'alpha': %s", rr.Body.String())
		return "", ""
	}

	_, h1 := readNodeHash(t, "peer-one", "peer-one-token")
	kind, h2 := readNodeHash(t, "peer-two", "peer-two-token")
	if kind != fingerprint.KindNode {
		t.Errorf("node entry kind = %q, want %q", kind, fingerprint.KindNode)
	}
	if h1 == h2 {
		t.Errorf("two peers with different tokens must receive different hashes, both got %s", h1)
	}
	key1, err := fingerprint.PairKey("peer-one-token")
	if err != nil {
		t.Fatalf("PairKey: %v", err)
	}
	want1, err := fingerprint.Hash(key1, fingerprint.KindNode, "gpu:uuid-1,uuid-2")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if h1 != want1 {
		t.Errorf("peer-one hash = %s, want %s", h1, want1)
	}
}

func nodeFingerprintHash(t *testing.T, key []byte) string {
	t.Helper()
	h, err := fingerprint.Hash(key, fingerprint.KindNode, "gpu:uuid-1,uuid-2")
	if err != nil {
		t.Fatalf("Hash(node): %v", err)
	}
	return h
}

func billingFingerprintHash(t *testing.T, key []byte) string {
	t.Helper()
	h, err := fingerprint.Hash(key, fingerprint.KindBilling, "shared-plan-a")
	if err != nil {
		t.Fatalf("Hash(billing): %v", err)
	}
	return h
}
