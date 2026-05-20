// BL332 T42b — Discussion Scopes: federated sync, throttle, and conflict API tests.
//
// TS-1: TestDiscussionThrottle_Enforced     — 61 POSTs; first 60 succeed, 61st → 429
// TS-2: TestDiscussionParticipants_SetAndGet — PUT participants; GET returns them
// TS-3: TestDiscussionConflicts_Detected     — 2 WAL entries with same content from
//
//	different origin_peers within 5s → GET /conflicts returns 1 conflict

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// newSyncTestServer creates a fresh Server with an isolated temp HOME and a
// clean throttle map state. Each test gets its own Server so throttle buckets
// don't bleed across tests.
func newSyncTestServer(t *testing.T) *Server {
	t.Helper()
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	// Remove any pre-existing throttle bucket that could leak across subtests.
	// (Not strictly necessary when HOME is isolated, but keeps state clean.)
	return &Server{
		memoryBackend: &fakeDiscussionBackend{},
	}
}

// resetThrottleMap removes all buckets from the global throttle map so that
// tests starting with a fresh server see a full 60-token bucket.
// Called at the start of each test that exercises the throttle.
func resetThrottleMap() {
	discussionThrottleMap.Range(func(k, _ any) bool {
		discussionThrottleMap.Delete(k)
		return true
	})
}

// -------------------------------------------------------------------
// TS-1: TestDiscussionThrottle_Enforced
// -------------------------------------------------------------------

// TestDiscussionThrottle_Enforced sends 61 POST requests to
// /api/memory/discussion/throttle-test. The first 60 must succeed with 200,
// the 61st must return 429.
func TestDiscussionThrottle_Enforced(t *testing.T) {
	resetThrottleMap()
	s := newSyncTestServer(t)

	const id = "throttle-test"
	const bearer = "test-bearer-throttle"

	post := func() int {
		body := map[string]any{"content": "hello", "role": "user"}
		bBody, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/api/memory/discussion/"+id, bytes.NewReader(bBody))
		req.Header.Set("Authorization", "Bearer "+bearer)
		rr := httptest.NewRecorder()
		s.handleDiscussionScope(rr, req)
		return rr.Code
	}

	// First 60 should succeed.
	for i := 1; i <= 60; i++ {
		code := post()
		if code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i, code)
		}
	}

	// 61st should be rate-limited.
	code := post()
	if code != http.StatusTooManyRequests {
		t.Fatalf("request 61: expected 429, got %d", code)
	}
}

// -------------------------------------------------------------------
// TS-2: TestDiscussionParticipants_SetAndGet
// -------------------------------------------------------------------

// TestDiscussionParticipants_SetAndGet PUTs a participant list, then GETs it
// and verifies the same peers are returned.
func TestDiscussionParticipants_SetAndGet(t *testing.T) {
	s := newSyncTestServer(t)

	const id = "participants-test"
	peers := []string{"node-alpha", "node-beta"}

	// PUT participants.
	putBody, _ := json.Marshal(map[string]any{"peers": peers})
	req := httptest.NewRequest(http.MethodPut, "/api/memory/discussion/"+id+"/participants", bytes.NewReader(putBody))
	rr := httptest.NewRecorder()
	s.handleDiscussionScope(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT participants: expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	// GET participants.
	req2 := httptest.NewRequest(http.MethodGet, "/api/memory/discussion/"+id+"/participants", nil)
	rr2 := httptest.NewRecorder()
	s.handleDiscussionScope(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("GET participants: expected 200, got %d body=%s", rr2.Code, rr2.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rr2.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode GET participants: %v", err)
	}
	gotPeers, _ := resp["peers"].([]any)
	if len(gotPeers) != len(peers) {
		t.Fatalf("expected %d peers, got %d (body=%s)", len(peers), len(gotPeers), rr2.Body.String())
	}
	for i, p := range peers {
		if gotPeers[i].(string) != p {
			t.Errorf("peer[%d]: expected %q, got %q", i, p, gotPeers[i])
		}
	}
}

// -------------------------------------------------------------------
// TS-3: TestDiscussionConflicts_Detected
// -------------------------------------------------------------------

// TestDiscussionConflicts_Detected writes 2 WAL entries with the same content
// from different origin_peers within the 5-second conflict window and verifies
// that GET /conflicts surfaces exactly 1 conflict group.
func TestDiscussionConflicts_Detected(t *testing.T) {
	s := newSyncTestServer(t)

	const id = "conflicts-test"
	const sharedContent = "conflicting message about the same topic"

	// Write the first entry directly via the WAL helper (simulating a local write).
	_, err := discussionAppendWALEntry(id, sharedContent, "user", "node-a", nil)
	if err != nil {
		t.Fatalf("write WAL entry 1: %v", err)
	}

	// Write the second entry from a different peer within the 5s window.
	// We do this immediately so the timestamp difference is <5s.
	_, err = discussionAppendWALEntry(id, sharedContent, "user", "node-b", nil)
	if err != nil {
		t.Fatalf("write WAL entry 2: %v", err)
	}

	// GET /conflicts.
	req := httptest.NewRequest(http.MethodGet, "/api/memory/discussion/"+id+"/conflicts", nil)
	rr := httptest.NewRecorder()
	s.handleDiscussionScope(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET conflicts: expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode GET conflicts: %v", err)
	}
	conflicts, _ := resp["conflicts"].([]any)
	if len(conflicts) != 1 {
		t.Fatalf("expected 1 conflict group, got %d (body=%s)", len(conflicts), rr.Body.String())
	}

	// Verify the conflict group has 2 entries.
	group, _ := conflicts[0].(map[string]any)
	entries, _ := group["entries"].([]any)
	if len(entries) != 2 {
		t.Errorf("expected 2 entries in conflict group, got %d", len(entries))
	}
}

// Compile-time check: sync tests don't introduce additional type dependencies.
var _ sync.Map
var _ time.Duration
