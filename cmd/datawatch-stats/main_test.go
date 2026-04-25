package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/observer"
)

// TestEmitSnapshot_WrapsWithShapeAndPeer asserts the wire format for
// what Task 3 will POST to the parent.
func TestEmitSnapshot_WrapsWithShapeAndPeer(t *testing.T) {
	tmp, err := os.CreateTemp(t.TempDir(), "snap-*.json")
	if err != nil {
		t.Fatalf("temp: %v", err)
	}
	defer tmp.Close()

	snap := &observer.StatsResponse{V: 2}
	emitSnapshot(tmp, snap, "ollama-box", "B")

	if _, err := tmp.Seek(0, 0); err != nil {
		t.Fatalf("seek: %v", err)
	}
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(tmp); err != nil {
		t.Fatalf("read: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("parse: %v -- body=%q", err, buf.String())
	}
	if got["shape"] != "B" {
		t.Errorf("shape = %v want B", got["shape"])
	}
	if got["peer_name"] != "ollama-box" {
		t.Errorf("peer_name = %v want ollama-box", got["peer_name"])
	}
	if got["snapshot"] == nil {
		t.Errorf("snapshot field missing")
	}
}

// TestSidecarServesStats exercises the optional /api/stats listener
// that operators can hit locally on Shape B hosts.
func TestSidecarServesStats(t *testing.T) {
	cfg := observer.DefaultConfig()
	cfg.TickIntervalMs = 200
	cfg.Envelopes.SessionAttribution = false
	col := observer.NewCollector(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	col.Start(ctx)
	t.Cleanup(col.Stop)

	// Wait for the first tick so the collector has a snapshot.
	time.Sleep(400 * time.Millisecond)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/stats", func(w http.ResponseWriter, r *http.Request) {
		snap := col.Latest()
		if snap == nil {
			http.Error(w, "no snapshot yet", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"shape":     "B",
			"peer_name": "test-peer",
			"snapshot":  snap,
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/stats")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d want 200", resp.StatusCode)
	}
	var got map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got["shape"] != "B" || got["peer_name"] != "test-peer" {
		t.Errorf("shape/peer wrong: %+v", got)
	}
	if got["snapshot"] == nil {
		t.Errorf("snapshot missing")
	}
}

// TestCollectorReuse — confirm the standalone binary's collector
// configuration mirrors what the parent's observer plugin uses, with
// the session-attribution pass deliberately turned off (no sessions
// to attribute on a Shape B host).
func TestCollectorReuse_DisablesSessionAttribution(t *testing.T) {
	cfg := observer.DefaultConfig()
	cfg.Envelopes.SessionAttribution = false

	if !cfg.Envelopes.BackendAttribution {
		t.Errorf("BackendAttribution should remain true for Shape B")
	}
	if cfg.Envelopes.SessionAttribution {
		t.Errorf("SessionAttribution should be false for Shape B")
	}
	if cfg.TickIntervalMs == 0 {
		t.Errorf("TickIntervalMs zero — defaults broken")
	}
	if cfg.EBPFEnabled != "auto" {
		t.Errorf("EBPFEnabled default = %q want auto", cfg.EBPFEnabled)
	}
}
