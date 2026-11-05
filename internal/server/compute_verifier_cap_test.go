// Write-time gate for compute_nodes[].verifier_slots: the same headroom
// gate as admission (compute.CapCoverage). Writing a cap above 1 on a
// compute node is refused with HTTP 409 — {error, node, free_mb,
// shortfall_mb} — when the node has no fresh headroom measurement covering
// the extra requests, and the node keeps its current cap. A cap of 0 or 1
// is always accepted. The autonomous knobs the gate reads (the free-memory
// floor, the measurement-freshness bound) round-trip through
// PUT/GET /api/config.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/compute"
	"github.com/dmz006/datawatch/internal/config"
)

// fixed clock the write gate reads: deterministic freshness checks.
var verifierCapNow = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

// newVerifierCapServer wires a Server whose verifier_slots write gate is
// deterministic: the fixed clock above, a HeadroomStore the tests populate,
// and a compute registry that already holds the node under test.
func newVerifierCapServer(t *testing.T, node *compute.Node) (*Server, *compute.HeadroomStore) {
	t.Helper()
	dir := t.TempDir()
	reg, err := compute.NewRegistry(filepath.Join(dir, "nodes.json"))
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	if err := reg.Add(node); err != nil {
		t.Fatalf("registry Add: %v", err)
	}
	hs := compute.NewHeadroomStore()
	s := &Server{
		cfg:                 config.DefaultConfig(),
		cfgPath:             filepath.Join(dir, "config.yaml"),
		computeReg:          reg,
		verifierHeadroom:    hs,
		verifierHeadroomNow: func() time.Time { return verifierCapNow },
	}
	return s, hs
}

// putVerifierSlots writes the node via PUT /api/compute/nodes/<name>
// (?probe=skip, so the gate — not the model probe — decides the outcome)
// and returns the status code plus the decoded JSON body when present.
func putVerifierSlots(t *testing.T, s *Server, name string, slots int) (int, map[string]any) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"name": name, "kind": compute.KindLocal, "verifier_slots": slots})
	rr := httptest.NewRecorder()
	s.handleComputeNodes(rr, httptest.NewRequest(http.MethodPut, "/api/compute/nodes/"+name+"?probe=skip", bytes.NewReader(body)))
	var out map[string]any
	if b := rr.Body.Bytes(); len(b) > 0 {
		if err := json.Unmarshal(b, &out); err != nil {
			out = map[string]any{"raw": string(b)}
		}
	}
	return rr.Code, out
}

func TestVerifierCap_PutNodeRefusedWithoutMeasurement(t *testing.T) {
	// A cap of 2 with no headroom measurement at all is refused, the 409
	// body names the node, and the stored cap stays at 1.
	s, _ := newVerifierCapServer(t, &compute.Node{Name: "gpu-a", Kind: compute.KindLocal, VerifierSlots: 1})
	code, body := putVerifierSlots(t, s, "gpu-a", 2)
	if code != http.StatusConflict {
		t.Fatalf("PUT verifier_slots=2 without measurement: want 409, got %d (%v)", code, body)
	}
	if body["node"] != "gpu-a" {
		t.Fatalf("409 body node = %v, want gpu-a", body["node"])
	}
	err, _ := body["error"].(string)
	if !strings.Contains(err, "gpu-a") {
		t.Fatalf("409 error %q does not name the node", err)
	}
	n, errGet := s.computeReg.Get("gpu-a")
	if errGet != nil {
		t.Fatalf("registry Get: %v", errGet)
	}
	if n.VerifierSlots != 1 {
		t.Fatalf("stored cap after refusal = %d, want 1 (unchanged)", n.VerifierSlots)
	}
}

func TestVerifierCap_PutNodeRefusedWhenNotCovering(t *testing.T) {
	// A fresh measurement that does not cover the extra request is refused
	// with the measured free memory and the shortfall in the body.
	s, hs := newVerifierCapServer(t, &compute.Node{Name: "gpu-a", Kind: compute.KindLocal, VerifierSlots: 1})
	s.cfg.Autonomous.VerifierMinFreeMemMB = 512
	hs.Put(compute.HeadroomMeasurement{
		Node: "gpu-a", FreeMB: 3000, PerRequestMB: 4000,
		MeasuredAt: verifierCapNow.Add(-5 * time.Minute),
	})
	code, body := putVerifierSlots(t, s, "gpu-a", 2)
	if code != http.StatusConflict {
		t.Fatalf("PUT verifier_slots=2 with insufficient headroom: want 409, got %d (%v)", code, body)
	}
	if body["node"] != "gpu-a" {
		t.Fatalf("409 body node = %v, want gpu-a", body["node"])
	}
	// available = 3000 - 512 = 2488 < 4000 needed.
	if got := body["free_mb"].(float64); got != 3000 {
		t.Fatalf("409 body free_mb = %v, want 3000", body["free_mb"])
	}
	if got := body["shortfall_mb"].(float64); got != 4000-2488 {
		t.Fatalf("409 body shortfall_mb = %v, want 1512", body["shortfall_mb"])
	}
	n, _ := s.computeReg.Get("gpu-a")
	if n.VerifierSlots != 1 {
		t.Fatalf("stored cap after refusal = %d, want 1 (unchanged)", n.VerifierSlots)
	}
}

func TestVerifierCap_PutNodeAcceptedWhenCovering(t *testing.T) {
	// A fresh measurement that covers the extra request is accepted and the
	// cap is stored.
	s, hs := newVerifierCapServer(t, &compute.Node{Name: "gpu-a", Kind: compute.KindLocal, VerifierSlots: 1})
	s.cfg.Autonomous.VerifierMinFreeMemMB = 512
	hs.Put(compute.HeadroomMeasurement{
		Node: "gpu-a", FreeMB: 10000, PerRequestMB: 1000,
		MeasuredAt: verifierCapNow.Add(-5 * time.Minute),
	})
	code, body := putVerifierSlots(t, s, "gpu-a", 2)
	if code != http.StatusOK {
		t.Fatalf("PUT verifier_slots=2 with covering headroom: want 200, got %d (%v)", code, body)
	}
	n, errGet := s.computeReg.Get("gpu-a")
	if errGet != nil {
		t.Fatalf("registry Get: %v", errGet)
	}
	if n.VerifierSlots != 2 {
		t.Fatalf("stored cap after accepted write = %d, want 2", n.VerifierSlots)
	}
}

func TestVerifierCap_PutNodeRefusedWhenStale(t *testing.T) {
	// The same covering measurement, older than the 24-hour default
	// freshness bound, is refused again.
	s, hs := newVerifierCapServer(t, &compute.Node{Name: "gpu-a", Kind: compute.KindLocal, VerifierSlots: 1})
	s.cfg.Autonomous.VerifierMinFreeMemMB = 512
	hs.Put(compute.HeadroomMeasurement{
		Node: "gpu-a", FreeMB: 10000, PerRequestMB: 1000,
		MeasuredAt: verifierCapNow.Add(-25 * time.Hour),
	})
	code, body := putVerifierSlots(t, s, "gpu-a", 2)
	if code != http.StatusConflict {
		t.Fatalf("PUT verifier_slots=2 with stale measurement: want 409, got %d (%v)", code, body)
	}
	if body["node"] != "gpu-a" {
		t.Fatalf("409 body node = %v, want gpu-a", body["node"])
	}
	err, _ := body["error"].(string)
	if !strings.Contains(err, "stale") {
		t.Fatalf("409 error %q does not say the measurement is stale", err)
	}
	n, _ := s.computeReg.Get("gpu-a")
	if n.VerifierSlots != 1 {
		t.Fatalf("stored cap after stale refusal = %d, want 1 (unchanged)", n.VerifierSlots)
	}
}

func TestVerifierCap_PutNodeZeroAndOneAlwaysAccepted(t *testing.T) {
	// 0 (unset, reads the default cap 1) and 1 never need a measurement.
	s, _ := newVerifierCapServer(t, &compute.Node{Name: "gpu-a", Kind: compute.KindLocal, VerifierSlots: 2})
	if code, body := putVerifierSlots(t, s, "gpu-a", 0); code != http.StatusOK {
		t.Fatalf("PUT verifier_slots=0: want 200, got %d (%v)", code, body)
	}
	if n, _ := s.computeReg.Get("gpu-a"); n.VerifierSlots != 0 {
		t.Fatalf("stored cap = %d, want 0", n.VerifierSlots)
	}
	if code, body := putVerifierSlots(t, s, "gpu-a", 1); code != http.StatusOK {
		t.Fatalf("PUT verifier_slots=1: want 200, got %d (%v)", code, body)
	}
	if n, _ := s.computeReg.Get("gpu-a"); n.VerifierSlots != 1 {
		t.Fatalf("stored cap = %d, want 1", n.VerifierSlots)
	}
}

func TestVerifierCap_ConfigRoundTripsGateKnobs(t *testing.T) {
	dir := t.TempDir()
	s := &Server{cfg: config.DefaultConfig(), cfgPath: filepath.Join(dir, "config.yaml")}

	// The read surface renders the defaults the config seeds.
	got := getAutonomousConfigMap(t, s)
	if v := string(got["verifier_min_free_mem_mb"]); v != "2048" {
		t.Fatalf("GET autonomous.verifier_min_free_mem_mb = %q, want 2048", v)
	}
	if v := string(got["verifier_headroom_max_age_hours"]); v != "24" {
		t.Fatalf("GET autonomous.verifier_headroom_max_age_hours = %q, want 24 (effective default)", v)
	}
	if v := string(got["verifier_slots"]); v != "1" {
		t.Fatalf("GET autonomous.verifier_slots = %q, want 1 (default node cap)", v)
	}

	// PUT round-trips the free-memory floor through GET.
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"autonomous.verifier_min_free_mem_mb": 4096})
	if got := getAutonomousConfigMap(t, s); string(got["verifier_min_free_mem_mb"]) != "4096" {
		t.Fatalf("GET after PUT: verifier_min_free_mem_mb = %s, want 4096", got["verifier_min_free_mem_mb"])
	}

	// …and the freshness bound (0 clears back to the effective default).
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"autonomous.verifier_headroom_max_age_hours": 48})
	if got := getAutonomousConfigMap(t, s); string(got["verifier_headroom_max_age_hours"]) != "48" {
		t.Fatalf("GET after PUT: verifier_headroom_max_age_hours = %s, want 48", got["verifier_headroom_max_age_hours"])
	}
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"autonomous.verifier_headroom_max_age_hours": 0})
	if got := getAutonomousConfigMap(t, s); string(got["verifier_headroom_max_age_hours"]) != "24" {
		t.Fatalf("GET after clearing: verifier_headroom_max_age_hours = %s, want 24 (effective default)", got["verifier_headroom_max_age_hours"])
	}

	// A non-integer floor is skipped, not stored, and reported.
	resp := putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"autonomous.verifier_min_free_mem_mb": "lots"})
	skipped := string(resp["skipped"])
	if !strings.Contains(skipped, "autonomous.verifier_min_free_mem_mb") {
		t.Fatalf("PUT bad floor: skipped = %s, want it to name the key", skipped)
	}
	if got := getAutonomousConfigMap(t, s); string(got["verifier_min_free_mem_mb"]) != "4096" {
		t.Fatalf("GET after bad PUT: verifier_min_free_mem_mb = %s, want 4096 (unchanged)", got["verifier_min_free_mem_mb"])
	}
}
