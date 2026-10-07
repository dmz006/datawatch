// BL317 — GET /api/cost/aggregated, the Dashboard's "all servers" mode.
// Mirrors the existing (untested, but same-shape) handleAggregatedAlerts/
// handleAggregatedPRDs pattern in bl312_aggregated.go.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/server/multiserver"
)

func TestHandleAggregatedCost_FansOutAndTagsEachServer(t *testing.T) {
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/cost" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"sessions": 2, "total_tokens_in": 10, "total_tokens_out": 5, "total_usd": 0.75,
		})
	}))
	defer remote.Close()

	dir := t.TempDir()
	store, err := multiserver.NewStore(dir, nil)
	if err != nil {
		t.Fatalf("store init: %v", err)
	}
	if err := store.Add(&multiserver.Entry{Name: "pi-node", URL: remote.URL, Enabled: true}); err != nil {
		t.Fatalf("store add: %v", err)
	}

	s := &Server{serverStore: store, cfg: &config.Config{}} // manager left nil, same guard handleCostSummary itself uses

	req := httptest.NewRequest(http.MethodGet, "/api/cost/aggregated", nil)
	w := httptest.NewRecorder()
	s.handleAggregatedCost(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	var results []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &results); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected exactly 1 entry (nil manager, 1 remote), got %d: %v", len(results), results)
	}
	if results[0]["server"] != "pi-node" {
		t.Errorf("server = %v, want pi-node", results[0]["server"])
	}
	if results[0]["total_usd"] != 0.75 {
		t.Errorf("total_usd = %v, want 0.75", results[0]["total_usd"])
	}
}

func TestHandleAggregatedCost_UnreachableRemoteIsSkippedNotFatal(t *testing.T) {
	dir := t.TempDir()
	store, err := multiserver.NewStore(dir, nil)
	if err != nil {
		t.Fatalf("store init: %v", err)
	}
	// Nobody listens on this port -- simulates a peer that's down.
	if err := store.Add(&multiserver.Entry{Name: "down-node", URL: "http://127.0.0.1:1", Enabled: true}); err != nil {
		t.Fatalf("store add: %v", err)
	}

	s := &Server{serverStore: store, cfg: &config.Config{}}
	req := httptest.NewRequest(http.MethodGet, "/api/cost/aggregated", nil)
	w := httptest.NewRecorder()
	s.handleAggregatedCost(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 even with an unreachable peer (body: %s)", w.Code, w.Body.String())
	}
	var results []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &results); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 entries (nil manager, unreachable remote skipped), got %d: %v", len(results), results)
	}
}

func TestHandleAggregatedCost_RejectsNonGET(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest(http.MethodPost, "/api/cost/aggregated", nil)
	w := httptest.NewRecorder()
	s.handleAggregatedCost(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", w.Code)
	}
}
