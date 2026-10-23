// Tests for /api/federation/fingerprint-matches — the match records
// surfaced for operator decision
// (docs/plans/2026-10-14-concurrency-and-budget-design.md §3c).

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/fingerprint"
	"github.com/dmz006/datawatch/internal/server/multiserver"
)

// testMatchStore is the shared fixture: one pending, one confirmed and
// one rejected record, in memory (empty path — nothing to disk).
func testMatchStore(t *testing.T) *fingerprint.MatchStore {
	t.Helper()
	store, err := fingerprint.NewMatchStore("")
	if err != nil {
		t.Fatalf("NewMatchStore: %v", err)
	}
	for _, m := range []struct{ peer, kind, local, peerName, state string }{
		{"alpha", fingerprint.KindNode, "node-a", "peer-node", fingerprint.StatePending},
		{"alpha", fingerprint.KindNode, "node-b", "peer-node", fingerprint.StateConfirmed},
		{"beta", fingerprint.KindBilling, "plan-x", "peer-plan", fingerprint.StateRejected},
	} {
		match, _ := store.Observe(m.peer, m.kind, m.local, m.peerName, "deadbeefcafe")
		if m.state == fingerprint.StateConfirmed {
			if _, err := store.Confirm(match.ID); err != nil {
				t.Fatalf("pre-stage %s -> confirmed: %v", match.ID, err)
			}
		}
		if m.state == fingerprint.StateRejected {
			if _, err := store.Reject(match.ID); err != nil {
				t.Fatalf("pre-stage %s -> rejected: %v", match.ID, err)
			}
		}
	}
	return store
}

func matchesPeer(name string, opts ...string) *multiserver.Entry {
	caps := []string{"federation-peer"}
	for _, cap := range opts {
		caps = append(caps, cap)
	}
	return &multiserver.Entry{
		Name:         name,
		URL:          "http://" + name + ":8080",
		Token:        "tok",
		Federated:    true,
		Enabled:      true,
		Capabilities: caps,
	}
}

// callMatches drives a handler the way the mux would: with the given
// method + path and the given peer in context (nil = admin).
func callMatches(t *testing.T, s *Server, peer *multiserver.Entry, method, target string, h http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, target, nil)
	if peer != nil {
		r = r.WithContext(context.WithValue(r.Context(), fedPeerKey, peer))
	}
	rr := httptest.NewRecorder()
	h(rr, r)
	return rr
}

// TestFingerprintMatchesListFilters — GET with no filter returns all
// records; each state filter returns exactly its own records (still a
// list, [] when empty, never null).
func TestFingerprintMatchesListFilters(t *testing.T) {
	s := &Server{fingerprintMatchStore: testMatchStore(t)}
	peer := matchesPeer("alpha", "full-control")

	listAll := func(t *testing.T, q string) []fingerprint.Match {
		t.Helper()
		rr := callMatches(t, s, peer, http.MethodGet, "/api/federation/fingerprint-matches"+q,
			s.handleFingerprintMatches)
		if rr.Code != http.StatusOK {
			t.Fatalf("GET %s: expected 200, got %d body=%s", q, rr.Code, rr.Body.String())
		}
		var body struct {
			Matches []fingerprint.Match `json:"matches"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body.Matches == nil {
			t.Fatalf("matches must be an empty list, not null: %s", rr.Body.String())
		}
		return body.Matches
	}

	all := listAll(t, "")
	if len(all) != 3 {
		t.Fatalf("no filter: expected 3 matches, got %d", len(all))
	}

	pending := listAll(t, "?state=pending")
	if len(pending) != 1 || pending[0].State != fingerprint.StatePending {
		t.Errorf("state=pending: expected exactly the one pending match, got %d", len(pending))
	}
	confirmed := listAll(t, "?state=confirmed")
	if len(confirmed) != 1 || confirmed[0].State != fingerprint.StateConfirmed {
		t.Errorf("state=confirmed: expected exactly the one confirmed match, got %d", len(confirmed))
	}

	rr := callMatches(t, s, peer, http.MethodGet, "/api/federation/fingerprint-matches?state=bogus",
		s.handleFingerprintMatches)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("state=bogus: expected 400, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// TestFingerprintMatchConfirm — POST /{id}/confirm moves a pending (and
// a rejected) record to confirmed, answers the updated record, and
// repeating the confirm is a plain 200.
func TestFingerprintMatchConfirm(t *testing.T) {
	s := &Server{fingerprintMatchStore: testMatchStore(t)}
	peer := matchesPeer("alpha", "full-control")
	id := findMatchID(t, s.fingerprintMatchStore, fingerprint.StatePending)

	rr := callMatches(t, s, peer, http.MethodPost, "/api/federation/fingerprint-matches/"+id+"/confirm",
		s.handleFingerprintMatchDecision)
	if rr.Code != http.StatusOK {
		t.Fatalf("confirm: expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var m fingerprint.Match
	if err := json.Unmarshal(rr.Body.Bytes(), &m); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if m.State != fingerprint.StateConfirmed || m.ID != id {
		t.Errorf("answer = %s/%s, want the confirmed record %s", m.ID, m.State, id)
	}

	// Re-confirming the already-confirmed record is a no-op 200.
	rr = callMatches(t, s, peer, http.MethodPost, "/api/federation/fingerprint-matches/"+id+"/confirm",
		s.handleFingerprintMatchDecision)
	if rr.Code != http.StatusOK {
		t.Errorf("re-confirm: expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	// A rejected record can be confirmed again (operator changed mind).
	rejectedID := findMatchID(t, s.fingerprintMatchStore, fingerprint.StateRejected)
	rr = callMatches(t, s, peer, http.MethodPost, "/api/federation/fingerprint-matches/"+rejectedID+"/confirm",
		s.handleFingerprintMatchDecision)
	if rr.Code != http.StatusOK {
		t.Errorf("confirm-after-reject: expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	m = fingerprint.Match{}
	_ = json.Unmarshal(rr.Body.Bytes(), &m)
	if m.State != fingerprint.StateConfirmed {
		t.Errorf("confirm-after-reject answer state = %s, want confirmed", m.State)
	}
}

// TestFingerprintMatchReject — POST /{id}/reject moves a record to
// rejected, answers the updated record, and repeating it is a plain
// 200 (this is how an operator un-merges a confirmed match).
func TestFingerprintMatchReject(t *testing.T) {
	s := &Server{fingerprintMatchStore: testMatchStore(t)}
	peer := matchesPeer("alpha", "full-control")
	id := findMatchID(t, s.fingerprintMatchStore, fingerprint.StatePending)

	rr := callMatches(t, s, peer, http.MethodPost, "/api/federation/fingerprint-matches/"+id+"/reject",
		s.handleFingerprintMatchDecision)
	if rr.Code != http.StatusOK {
		t.Fatalf("reject: expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var m fingerprint.Match
	if err := json.Unmarshal(rr.Body.Bytes(), &m); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if m.State != fingerprint.StateRejected || m.ID != id {
		t.Errorf("answer = %s/%s, want the rejected record %s", m.ID, m.State, id)
	}

	// Rejecting a confirmed record un-merges it.
	confirmedID := findMatchID(t, s.fingerprintMatchStore, fingerprint.StateConfirmed)
	rr = callMatches(t, s, peer, http.MethodPost, "/api/federation/fingerprint-matches/"+confirmedID+"/reject",
		s.handleFingerprintMatchDecision)
	if rr.Code != http.StatusOK {
		t.Errorf("reject-after-confirm: expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	m = fingerprint.Match{}
	_ = json.Unmarshal(rr.Body.Bytes(), &m)
	if m.State != fingerprint.StateRejected {
		t.Errorf("reject-after-confirm answer state = %s, want rejected", m.State)
	}

	// Re-rejecting is a no-op 200.
	rr = callMatches(t, s, peer, http.MethodPost, "/api/federation/fingerprint-matches/"+confirmedID+"/reject",
		s.handleFingerprintMatchDecision)
	if rr.Code != http.StatusOK {
		t.Errorf("re-reject: expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// TestFingerprintMatchUnknownID404 — confirm/reject of an id the store
// has never seen is 404, and a malformed sub-path is 404 as well.
func TestFingerprintMatchUnknownID404(t *testing.T) {
	s := &Server{fingerprintMatchStore: testMatchStore(t)}
	peer := matchesPeer("alpha", "full-control")

	for _, suffix := range []string{"/confirm", "/reject"} {
		rr := callMatches(t, s, peer, http.MethodPost, "/api/federation/fingerprint-matches/deadbeef"+suffix,
			s.handleFingerprintMatchDecision)
		if rr.Code != http.StatusNotFound {
			t.Errorf("%s unknown id: expected 404, got %d body=%s", suffix, rr.Code, rr.Body.String())
		}
	}

	rr := callMatches(t, s, peer, http.MethodPost, "/api/federation/fingerprint-matches/deadbeef/poop",
		s.handleFingerprintMatchDecision)
	if rr.Code != http.StatusNotFound {
		t.Errorf("unknown action: expected 404, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// TestFingerprintMatchesNeedCapability — the method guard runs first
// (405), then the capability guard: a peer without federation:list /
// federation:write gets 403, with the missing capability named; with
// no store wired at all the handler answers 503.
func TestFingerprintMatchesNeedCapability(t *testing.T) {
	s := &Server{fingerprintMatchStore: testMatchStore(t)}

	// Method guards first: POST to the list route, GET to a decision
	// route, before any capability question.
	rr := callMatches(t, s, matchesPeer("alpha", "full-control"), http.MethodPost,
		"/api/federation/fingerprint-matches", s.handleFingerprintMatches)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST list: expected 405, got %d body=%s", rr.Code, rr.Body.String())
	}
	rr = callMatches(t, s, matchesPeer("alpha", "full-control"), http.MethodGet,
		"/api/federation/fingerprint-matches/abc/confirm", s.handleFingerprintMatchDecision)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET decision: expected 405, got %d body=%s", rr.Code, rr.Body.String())
	}

	// Capabilities next: a plain federation-peer has neither
	// federation:list nor federation:write.
	plain := matchesPeer("beta")
	rr = callMatches(t, s, plain, http.MethodGet, "/api/federation/fingerprint-matches",
		s.handleFingerprintMatches)
	if rr.Code != http.StatusForbidden {
		t.Errorf("GET without capability: expected 403, got %d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), federation.CapFederationList) {
		t.Errorf("403 body should name %s, got: %s", federation.CapFederationList, rr.Body.String())
	}
	rr = callMatches(t, s, plain, http.MethodPost, "/api/federation/fingerprint-matches/abc/confirm",
		s.handleFingerprintMatchDecision)
	if rr.Code != http.StatusForbidden {
		t.Errorf("confirm without capability: expected 403, got %d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), federation.CapFederationWrite) {
		t.Errorf("403 body should name %s, got: %s", federation.CapFederationWrite, rr.Body.String())
	}

	// A peer with only federation:list may list but not decide.
	readOnly := matchesPeer("gamma", "federation:list")
	rr = callMatches(t, s, readOnly, http.MethodGet, "/api/federation/fingerprint-matches",
		s.handleFingerprintMatches)
	if rr.Code != http.StatusOK {
		t.Errorf("list-only peer GET: expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	rr = callMatches(t, s, readOnly, http.MethodPost, "/api/federation/fingerprint-matches/abc/confirm",
		s.handleFingerprintMatchDecision)
	if rr.Code != http.StatusForbidden {
		t.Errorf("list-only peer confirm: expected 403, got %d body=%s", rr.Code, rr.Body.String())
	}

	// No store wired — 503, even for an admin caller.
	empty := &Server{}
	rr = callMatches(t, empty, nil, http.MethodGet, "/api/federation/fingerprint-matches",
		empty.handleFingerprintMatches)
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("unwired store GET: expected 503, got %d body=%s", rr.Code, rr.Body.String())
	}
	rr = callMatches(t, empty, nil, http.MethodPost, "/api/federation/fingerprint-matches/abc/confirm",
		empty.handleFingerprintMatchDecision)
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("unwired store confirm: expected 503, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func findMatchID(t *testing.T, store *fingerprint.MatchStore, state string) string {
	t.Helper()
	for _, m := range store.List(state) {
		return m.ID
	}
	t.Fatalf("fixture has no %s match", state)
	return ""
}
