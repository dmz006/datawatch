// BL427 (PRD lifecycle events, phase 3) — REST-level tests for
// /api/autonomous/subscriptions: they drive the real
// autonomous.SubscriptionStore (the same type and file layout the daemon
// runs at <data_dir>/lifecycle_subscriptions.json) through the handler
// with httptest, so the JSON wire shape, the status mapping
// (201/200/204/400/403/404/405/503) and the store validation errors are
// all exercised.

package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// newSubsRESTServer wires the real SubscriptionStore into a Server —
// the REST layer sits directly on the store, exactly like main.go.
func newSubsRESTServer(t *testing.T) (*Server, *autonomous.SubscriptionStore) {
	t.Helper()
	store, err := autonomous.NewSubscriptionStore(filepath.Join(t.TempDir(), "lifecycle_subscriptions.json"), nil)
	if err != nil {
		t.Fatalf("NewSubscriptionStore: %v", err)
	}
	return &Server{autonomousSubs: store}, store
}

// doSubs runs one /api/autonomous/subscriptions request against the
// handler and returns the recorder (status + body asserted by the test).
func doSubs(t *testing.T, s *Server, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		r = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, r)
	rr := httptest.NewRecorder()
	s.handleAutonomousSubscriptions(rr, req)
	return rr
}

// validSub is a subscription that passes ValidateSubscription: a
// session sink with a send_input action.
func validSub() map[string]any {
	return map[string]any{
		"filter":   map[string]any{"kinds": []string{"prd.completed", "task.completed"}, "level": "prd"},
		"sink":     map[string]any{"type": "session", "target": "ops"},
		"action":   map[string]any{"type": "send_input", "text": "Automaton done"},
		"one_shot": true,
	}
}

func TestSubscriptionRESTCRUDRoundTrip(t *testing.T) {
	s, store := newSubsRESTServer(t)

	// POST -> 201, with an id assigned by the server.
	rr := doSubs(t, s, http.MethodPost, "/api/autonomous/subscriptions", validSub())
	if rr.Code != http.StatusCreated {
		t.Fatalf("POST: want 201, got %d body=%s", rr.Code, rr.Body.String())
	}
	var created autonomous.Subscription
	if err := json.Unmarshal(rr.Body.Bytes(), &created); err != nil || created.ID == "" {
		t.Fatalf("decode create response: %v (body=%q)", err, rr.Body.String())
	}
	if got := len(store.List("")); got != 1 {
		t.Fatalf("store after create: %d subs, want 1", got)
	}

	// GET (list) contains the new subscription.
	rr = doSubs(t, s, http.MethodGet, "/api/autonomous/subscriptions", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var list []autonomous.Subscription
	if err := json.Unmarshal(rr.Body.Bytes(), &list); err != nil || len(list) != 1 || list[0].ID != created.ID {
		t.Fatalf("list: err=%v len=%d (body=%q), want 1 entry %q", err, len(list), rr.Body.String(), created.ID)
	}

	// DELETE -> 204, then the list is empty and the store agrees.
	rr = doSubs(t, s, http.MethodDelete, "/api/autonomous/subscriptions/"+created.ID, nil)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("DELETE: want 204, got %d body=%s", rr.Code, rr.Body.String())
	}
	if got := len(store.List("")); got != 0 {
		t.Fatalf("store after delete: %d subs, want 0", got)
	}
	rr = doSubs(t, s, http.MethodGet, "/api/autonomous/subscriptions", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET after delete: want 200, got %d", rr.Code)
	}
	if listErr := json.Unmarshal(rr.Body.Bytes(), &list); listErr != nil || list == nil || len(list) != 0 {
		t.Fatalf("list after delete: err=%v body=%q, want []", listErr, rr.Body.String())
	}

	// DELETE unknown id -> 404.
	rr = doSubs(t, s, http.MethodDelete, "/api/autonomous/subscriptions/never-existed", nil)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("DELETE unknown: want 404, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestSubscriptionRESTValidationErrors(t *testing.T) {
	s, store := newSubsRESTServer(t)

	cases := []struct {
		name string
		body map[string]any
		want string // substring the store's validation message must carry
	}{
		{"no kinds or level", map[string]any{
			"filter": map[string]any{"prd_id": "prd-1"},
			"sink":   map[string]any{"type": "comm", "target": "ops"},
		}, "filter must set at least one of kinds or level"},
		{"unknown kind", map[string]any{
			"filter": map[string]any{"kinds": []string{"bogus.event"}},
			"sink":   map[string]any{"type": "comm", "target": "ops"},
		}, "is not a known event kind"},
		{"bad level", map[string]any{
			"filter": map[string]any{"level": "story"},
			"sink":   map[string]any{"type": "comm", "target": "ops"},
		}, "filter level must be"},
		{"missing sink", map[string]any{
			"filter": map[string]any{"kinds": []string{"prd.completed"}},
		}, "sink type is required"},
		{"webhook sink deferred", map[string]any{
			"filter": map[string]any{"kinds": []string{"prd.completed"}},
			"sink":   map[string]any{"type": "webhook", "target": "https://x"},
		}, "not available in this phase"},
		{"unknown sink type", map[string]any{
			"filter": map[string]any{"kinds": []string{"prd.completed"}},
			"sink":   map[string]any{"type": "carrier-pigeon", "target": "dove"},
		}, "unknown sink type"},
		{"missing sink target", map[string]any{
			"filter": map[string]any{"kinds": []string{"prd.completed"}},
			"sink":   map[string]any{"type": "comm"},
		}, "sink target is required"},
		{"action without session sink", map[string]any{
			"filter": map[string]any{"kinds": []string{"prd.completed"}},
			"sink":   map[string]any{"type": "comm", "target": "ops"},
			"action": map[string]any{"type": "send_input", "text": "hi"},
		}, "action is only valid with a session sink"},
		{"send_input without text", map[string]any{
			"filter": map[string]any{"kinds": []string{"prd.completed"}},
			"sink":   map[string]any{"type": "session", "target": "ops"},
			"action": map[string]any{"type": "send_input"},
		}, "requires non-empty text"},
		{"expired at creation", func() map[string]any {
			past := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
			return map[string]any{
				"filter":     map[string]any{"kinds": []string{"prd.completed"}},
				"sink":       map[string]any{"type": "comm", "target": "ops"},
				"expires_at": past,
			}
		}(), "expires_at must be in the future"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr := doSubs(t, s, http.MethodPost, "/api/autonomous/subscriptions", tc.body)
			if rr.Code != http.StatusBadRequest {
				t.Fatalf("want 400, got %d body=%s", rr.Code, rr.Body.String())
			}
			if !bytes.Contains(rr.Body.Bytes(), []byte(tc.want)) {
				t.Fatalf("body %q does not mention %q", rr.Body.String(), tc.want)
			}
			if got := len(store.List("")); got != 0 {
				t.Fatalf("rejected subscription was stored: %d subs", got)
			}
		})
	}

	// Malformed JSON body -> 400 (decode failure, before validation).
	req := httptest.NewRequest(http.MethodPost, "/api/autonomous/subscriptions", bytes.NewReader([]byte("`not json")))
	rec := httptest.NewRecorder()
	s.handleAutonomousSubscriptions(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed body: want 400, got %d body=%s", rec.Code, rec.Body.String())
	}
	if got := len(store.List("")); got != 0 {
		t.Fatalf("malformed body stored a subscription: %d subs", got)
	}
}

func TestSubscriptionRESTMethodNotAllowed(t *testing.T) {
	s, _ := newSubsRESTServer(t)

	rr := doSubs(t, s, http.MethodPut, "/api/autonomous/subscriptions", validSub())
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("PUT list: want 405, got %d body=%s", rr.Code, rr.Body.String())
	}
	// An id sub-path only accepts DELETE in this surface.
	rr = doSubs(t, s, http.MethodGet, "/api/autonomous/subscriptions/some-id", nil)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET by id: want 405, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestSubscriptionRESTNilStoreIs503(t *testing.T) {
	s := &Server{} // nothing wired — the daemon's engine never came up
	for _, tc := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/autonomous/subscriptions"},
		{http.MethodPost, "/api/autonomous/subscriptions"},
		{http.MethodDelete, "/api/autonomous/subscriptions/x"},
	} {
		rr := doSubs(t, s, tc.method, tc.path, nil)
		if rr.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s %s: want 503, got %d body=%s", tc.method, tc.path, rr.Code, rr.Body.String())
		}
	}
}

func TestSubscriptionRESTFederationCaps(t *testing.T) {
	s, _ := newSubsRESTServer(t)

	// A peer without the autonomous caps is rejected on reads and writes
	// (403), before the handler even touches the store.
	peerReq := asPeer(httptest.NewRequest(http.MethodGet, "/api/autonomous/subscriptions", nil), "sessions:read")
	rec := httptest.NewRecorder()
	s.handleAutonomousSubscriptions(rec, peerReq)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("GET as caps-less peer: want 403, got %d body=%s", rec.Code, rec.Body.String())
	}

	// autonomous:read allows the list but not the create...
	peerReq = asPeer(httptest.NewRequest(http.MethodGet, "/api/autonomous/subscriptions", nil), "autonomous:read")
	rec = httptest.NewRecorder()
	s.handleAutonomousSubscriptions(rec, peerReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET as autonomous:read peer: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	body, _ := json.Marshal(validSub())
	peerReq = asPeer(httptest.NewRequest(http.MethodPost, "/api/autonomous/subscriptions", bytes.NewReader(body)), "autonomous:read")
	rec = httptest.NewRecorder()
	s.handleAutonomousSubscriptions(rec, peerReq)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("POST as autonomous:read peer: want 403, got %d body=%s", rec.Code, rec.Body.String())
	}

	// ...while autonomous:write allows create and delete.
	peerReq = asPeer(httptest.NewRequest(http.MethodPost, "/api/autonomous/subscriptions", bytes.NewReader(body)), "autonomous:write")
	rec = httptest.NewRecorder()
	s.handleAutonomousSubscriptions(rec, peerReq)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST as autonomous:write peer: want 201, got %d body=%s", rec.Code, rec.Body.String())
	}
	var created autonomous.Subscription
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || created.ID == "" {
		t.Fatalf("decode create: %v (body=%q)", err, rec.Body.String())
	}

	peerReq = asPeer(httptest.NewRequest(http.MethodDelete, "/api/autonomous/subscriptions/"+created.ID, nil), "autonomous:write")
	rec = httptest.NewRecorder()
	s.handleAutonomousSubscriptions(rec, peerReq)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE as autonomous:write peer: want 204, got %d body=%s", rec.Code, rec.Body.String())
	}
}
