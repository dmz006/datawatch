// REST-level tests for PUT /api/autonomous/subscriptions/{id} and
// POST /api/autonomous/subscriptions/{id}/test.

package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/autonomous"
)

func TestSubscriptionUpdateRESTStatusCodes(t *testing.T) {
	s, store := newSubsRESTServer(t)
	created, err := store.Add(autonomous.Subscription{
		Filter: autonomous.LifecycleFilter{Kinds: []string{"prd.completed"}},
		Sink:   autonomous.SubscriptionSink{Type: "alert", Target: "a"},
	})
	if err != nil {
		t.Fatal(err)
	}
	rr := doSubs(t, s, http.MethodPut, "/api/autonomous/subscriptions/"+created.ID, validSub())
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT: want 200, got %d %s", rr.Code, rr.Body.String())
	}
	var got autonomous.Subscription
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil || got.ID != created.ID || got.Sink.Type != "session" {
		t.Fatalf("body %s err %v", rr.Body.String(), err)
	}
	bad := validSub()
	bad["filter"] = map[string]any{}
	if rr = doSubs(t, s, http.MethodPut, "/api/autonomous/subscriptions/"+created.ID, bad); rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid: want 400, got %d", rr.Code)
	}
	if rr = doSubs(t, s, http.MethodPut, "/api/autonomous/subscriptions/nope", validSub()); rr.Code != http.StatusNotFound {
		t.Fatalf("unknown: want 404, got %d", rr.Code)
	}
}

func TestSubscriptionUpdateRESTTestEndpoint(t *testing.T) {
	s, store := newSubsRESTServer(t)
	created, _ := store.Add(autonomous.Subscription{
		Filter: autonomous.LifecycleFilter{Kinds: []string{"prd.completed"}},
		Sink:   autonomous.SubscriptionSink{Type: "alert", Target: "a"},
	})
	path := "/api/autonomous/subscriptions/" + created.ID + "/test"
	// no engine attached → 503
	if rr := doSubs(t, s, http.MethodPost, path, nil); rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("no engine: want 503, got %d", rr.Code)
	}
	store.SetTestFirer(func(id string) (autonomous.TestResult, error) {
		return autonomous.TestResult{OK: true, Sink: "alert"}, nil
	})
	rr := doSubs(t, s, http.MethodPost, path, nil)
	if rr.Code != http.StatusOK || !bytes.Contains(rr.Body.Bytes(), []byte(`"ok":true`)) || !bytes.Contains(rr.Body.Bytes(), []byte(`"sink":"alert"`)) {
		t.Fatalf("want ok body, got %d %s", rr.Code, rr.Body.String())
	}
	store.SetTestFirer(func(id string) (autonomous.TestResult, error) {
		return autonomous.TestResult{Sink: "alert", Error: "sink not available"}, errors.New("sink not available")
	})
	rr = doSubs(t, s, http.MethodPost, path, nil)
	if rr.Code != http.StatusOK || !bytes.Contains(rr.Body.Bytes(), []byte(`"ok":false`)) || !bytes.Contains(rr.Body.Bytes(), []byte("sink not available")) {
		t.Fatalf("failure body, got %d %s", rr.Code, rr.Body.String())
	}
	store.SetTestFirer(func(id string) (autonomous.TestResult, error) {
		return autonomous.TestResult{}, autonomous.ErrSubscriptionNotFound
	})
	if rr = doSubs(t, s, http.MethodPost, "/api/autonomous/subscriptions/nope/test", nil); rr.Code != http.StatusNotFound {
		t.Fatalf("unknown: want 404, got %d", rr.Code)
	}
	if rr = doSubs(t, s, http.MethodGet, path, nil); rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET test: want 405, got %d", rr.Code)
	}
}

func TestSubscriptionUpdateRESTAuthAndAudit(t *testing.T) {
	s, store := newSubsRESTServer(t)
	al, err := audit.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = al.Close() })
	s.auditLog = al
	store.SetTestFirer(func(id string) (autonomous.TestResult, error) {
		return autonomous.TestResult{OK: true, Sink: "alert"}, nil
	})
	created, _ := store.Add(autonomous.Subscription{
		Filter: autonomous.LifecycleFilter{Kinds: []string{"prd.completed"}},
		Sink:   autonomous.SubscriptionSink{Type: "alert", Target: "a"},
	})
	body, _ := json.Marshal(validSub())
	for _, tc := range []struct{ method, path string }{
		{http.MethodPut, "/api/autonomous/subscriptions/" + created.ID},
		{http.MethodPost, "/api/autonomous/subscriptions/" + created.ID + "/test"},
	} {
		req := asPeer(httptest.NewRequest(tc.method, tc.path, bytes.NewReader(body)), "autonomous:read")
		rec := httptest.NewRecorder()
		s.handleAutonomousSubscriptions(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s %s read-only: want 403, got %d", tc.method, tc.path, rec.Code)
		}
		req = asPeer(httptest.NewRequest(tc.method, tc.path, bytes.NewReader(body)), "autonomous:write")
		rec = httptest.NewRecorder()
		s.handleAutonomousSubscriptions(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s %s write: want 200, got %d %s", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}
	entries, err := al.Read(audit.QueryFilter{})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, e := range entries {
		seen[e.Action] = true
	}
	if !seen["update"] || !seen["test"] {
		t.Fatalf("audit actions missing: %v", seen)
	}
}
