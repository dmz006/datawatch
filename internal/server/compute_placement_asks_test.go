// Placement asks on the REST surface:
//
//	GET  /api/compute/placements            → pending asks (oldest first)
//	POST /api/compute/placements/<id>/approve
//	POST /api/compute/placements/<id>/deny
//
// Decisions are one-shot; unknown IDs are 404; stale asks expire.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/compute"
)

func newPlacementAsksSurfaceTest(t *testing.T) *Server {
	t.Helper()
	t.Cleanup(resetPlacementAsksForTest)
	resetPlacementAsksForTest()
	s, _ := newPlacementServer(t)
	return s
}

// listPlacements calls GET /api/compute/placements and returns the decoded body.
func listPlacements(t *testing.T, s *Server) map[string]any {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleComputePlacements(rr, httptest.NewRequest(http.MethodGet, "/api/compute/placements", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/compute/placements: want 200, got %d (%s)", rr.Code, rr.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return body
}

// decideAsk calls POST /api/compute/placements/<id>/<action> and returns (status, body).
func decideAsk(t *testing.T, s *Server, id, action string) (int, string) {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleComputePlacementAskDecision(rr, httptest.NewRequest(http.MethodPost, "/api/compute/placements/"+id+"/"+action, nil))
	return rr.Code, rr.Body.String()
}

func TestPlacementAsks_GetEmpty(t *testing.T) {
	s := newPlacementAsksSurfaceTest(t)
	body := listPlacements(t, s)
	if body["pending"] != float64(0) {
		t.Fatalf("pending = %v, want 0", body["pending"])
	}
	if asks, ok := body["asks"].([]any); !ok || len(asks) != 0 {
		t.Fatalf("asks = %v, want an empty list", body["asks"])
	}
}

func TestPlacementAsks_ListShowsPendingOldestFirst(t *testing.T) {
	s := newPlacementAsksSurfaceTest(t)
	now := time.Now()
	st := PlacementAskRegistry()
	st.Add("llama3.1-8b", "gpu-box", "missing model policy ask", 5, now.Truncate(time.Second), time.Hour)
	st.Add("qwen2.5-7b", "gpu-box", "missing model policy ask", 0, now.Add(time.Second).Truncate(time.Second), time.Hour)

	st.Answer("ask-2", compute.AskDecisionDenied, now.Add(2*time.Second), "operator") //nolint:errcheck

	body := listPlacements(t, s)
	if body["pending"] != float64(1) {
		t.Fatalf("pending = %v, want 1 (the denied ask is not pending)", body["pending"])
	}
	asks := body["asks"].([]any)
	first, _ := asks[0].(map[string]any)
	if first["id"] != "ask-1" || first["model"] != "llama3.1-8b" || first["decision"] != "pending" {
		t.Fatalf("first ask = %v", first)
	}
}

func TestPlacementAsks_ApproveIsOneShot(t *testing.T) {
	s := newPlacementAsksSurfaceTest(t)
	st := PlacementAskRegistry()
	st.Add("llama3.1-8b", "gpu-box", "", 5, time.Now().Truncate(time.Second), time.Hour)

	code, body := decideAsk(t, s, "ask-1", "approve")
	if code != http.StatusOK {
		t.Fatalf("approve: want 200, got %d (%s)", code, body)
	}
	if !strings.Contains(body, `"approved"`) {
		t.Fatalf("approve body = %s", body)
	}
	if got := listPlacements(t, s)["pending"]; got != float64(0) {
		t.Fatalf("after approve, pending = %v, want 0", got)
	}
	code, body = decideAsk(t, s, "ask-1", "approve")
	if code != http.StatusConflict {
		t.Fatalf("second approve: want 409, got %d (%s)", code, body)
	}
	code, body = decideAsk(t, s, "ask-1", "deny")
	if code != http.StatusConflict {
		t.Fatalf("deny after approve: want 409, got %d (%s)", code, body)
	}
}

func TestPlacementAsks_UnknownIDIs404(t *testing.T) {
	s := newPlacementAsksSurfaceTest(t)
	for _, action := range []string{"approve", "deny"} {
		code, body := decideAsk(t, s, "ask-99", action)
		if code != http.StatusNotFound {
			t.Fatalf("%s ask-99: want 404, got %d (%s)", action, code, body)
		}
	}
}

func TestPlacementAsks_BadShapesAreRejected(t *testing.T) {
	s := newPlacementAsksSurfaceTest(t)
	st := PlacementAskRegistry()
	st.Add("llama3.1-8b", "gpu-box", "", 0, time.Now().Truncate(time.Second), time.Hour)

	// unknown action:
	rr := httptest.NewRecorder()
	s.handleComputePlacementAskDecision(rr, httptest.NewRequest(http.MethodPost, "/api/compute/placements/ask-1/maybe", nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("unknown action: want 400, got %d", rr.Code)
	}
	// missing id:
	rr = httptest.NewRecorder()
	s.handleComputePlacementAskDecision(rr, httptest.NewRequest(http.MethodPost, "/api/compute/placements//approve", nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing id: want 400, got %d", rr.Code)
	}
	// method not allowed:
	rr = httptest.NewRecorder()
	s.handleComputePlacementAskDecision(rr, httptest.NewRequest(http.MethodGet, "/api/compute/placements/ask-1/approve", nil))
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET decision: want 405, got %d", rr.Code)
	}
	// POST on the list route:
	rr = httptest.NewRecorder()
	s.handleComputePlacements(rr, httptest.NewRequest(http.MethodPost, "/api/compute/placements", nil))
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST list: want 405, got %d", rr.Code)
	}
}

func TestPlacementAsks_ExpiredAsksAreNotPending(t *testing.T) {
	s := newPlacementAsksSurfaceTest(t)
	st := PlacementAskRegistry()
	now := time.Now().Truncate(time.Second)
	a := st.Add("llama3.1-8b", "gpu-box", "", 0, now, time.Minute)
	if !a.ExpiresAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("ExpiresAt = %s, want %s", a.ExpiresAt, now.Add(time.Minute))
	}
	// past the expiry:
	st.ExpireStale(now.Add(2 * time.Minute))
	body := listPlacements(t, s)
	if body["pending"] != float64(0) {
		t.Fatalf("after expiry, pending = %v, want 0", body["pending"])
	}
	code, bodyStr := decideAsk(t, s, a.ID, "approve")
	if code != http.StatusConflict {
		t.Fatalf("approving an expired ask: want 409, got %d (%s)", code, bodyStr)
	}
}
