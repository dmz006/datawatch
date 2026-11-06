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

// newHeadroomGateServer is a node-registry server wired with one headroom
// store and the default autonomous config, the way the daemon wires them.
func newHeadroomGateServer(t *testing.T) (*Server, *compute.HeadroomStore, *compute.Registry) {
	t.Helper()
	srv, reg := newVerifierSlotsServer(t, &compute.Node{Name: "gpubox", Kind: "ollama", Address: "http://gpubox:11434", MonitoringEndpoint: "http://stub"})
	store := compute.NewHeadroomStore()
	srv.headroomStore = store
	srv.cfg = config.DefaultConfig()
	return srv, store, reg
}

// TestNodeWriteGate_RefusesVerifierSlotsAboveOneWithoutMeasurement pins the
// write gate on the REST node routes (which the MCP and CLI node writes use):
// a verifier_slots above 1 with no fresh measurement is refused with 409, the
// CapRefusal message naming the node, and nothing is saved.
func TestNodeWriteGate_RefusesVerifierSlotsAboveOneWithoutMeasurement(t *testing.T) {
	srv, _, reg := newHeadroomGateServer(t)
	body := `{"name":"gpubox","kind":"ollama","address":"http://gpubox:11434","verifier_slots":3}`

	rec := httptest.NewRecorder()
	srv.handleComputeNodes(rec, httptest.NewRequest(http.MethodPut, "/api/compute/nodes/gpubox?probe=skip", strings.NewReader(body)))
	if rec.Code != http.StatusConflict {
		t.Fatalf("PUT status = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	if want := "cap on node gpubox refused: no fresh measurement"; !strings.Contains(rec.Body.String(), want) {
		t.Errorf("PUT body = %q, want it to contain %q", rec.Body.String(), want)
	}
	if n, _ := reg.Get("gpubox"); n == nil || n.VerifierSlots != nil {
		t.Errorf("refused write changed the node: verifier_slots = %+v", n)
	}

	rec = httptest.NewRecorder()
	srv.handleComputeNodes(rec, httptest.NewRequest(http.MethodPost, "/api/compute/nodes?probe=skip", strings.NewReader(
		`{"name":"gpubox2","kind":"ollama","address":"http://gpubox2:11434","verifier_slots":2}`)))
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "cap on node gpubox2 refused") {
		t.Fatalf("POST status = %d body %q, want 409 naming gpubox2", rec.Code, rec.Body.String())
	}
}

// TestNodeWriteGate_AcceptsSupportedAndLowSlots pins the accept side: a
// measurement that covers the cap lets it through, and 0 or 1 never needs one.
func TestNodeWriteGate_AcceptsSupportedAndLowSlots(t *testing.T) {
	srv, store, reg := newHeadroomGateServer(t)
	store.Put(compute.HeadroomMeasurement{Node: "gpubox", FreeMB: 8192, PerRequestMB: 512, MeasuredAt: time.Now()})
	put := func(slots string) int {
		rec := httptest.NewRecorder()
		srv.handleComputeNodes(rec, httptest.NewRequest(http.MethodPut, "/api/compute/nodes/gpubox?probe=skip", strings.NewReader(
			`{"name":"gpubox","kind":"ollama","address":"http://gpubox:11434","verifier_slots":`+slots+`}`)))
		return rec.Code
	}
	if code := put("3"); code != http.StatusOK {
		t.Fatalf("cap 3 with 8 GB free: status %d, want 200", code)
	}
	if n, _ := reg.Get("gpubox"); n == nil || n.VerifierSlots == nil || *n.VerifierSlots != 3 {
		t.Fatalf("accepted write not stored: %+v", n)
	}
	if code := put("1"); code != http.StatusOK {
		t.Fatalf("cap 1: status %d, want 200 (never gated)", code)
	}
	if code := put("0"); code != http.StatusOK {
		t.Fatalf("cap 0: status %d, want 200 (never gated)", code)
	}
}

// TestConfigPut_VerifierHeadroomKeys covers the REST config surface: a
// negative value is a 400 that names the key; a valid value is applied and the
// effective value reads back (0 floor = check off, 0 age = the 24h default).
func TestConfigPut_VerifierHeadroomKeys(t *testing.T) {
	cfg := config.DefaultConfig()
	s := &Server{cfg: cfg, cfgPath: filepath.Join(t.TempDir(), "config.yaml")}
	put := func(patch map[string]interface{}) *httptest.ResponseRecorder {
		body, _ := json.Marshal(patch)
		req := httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		s.handlePutConfig(rr, req)
		return rr
	}

	for _, k := range []string{"autonomous.verifier_min_free_mem_mb", "autonomous.verifier_headroom_max_age_hours"} {
		rr := put(map[string]interface{}{k: -5})
		if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), k) {
			t.Errorf("%s = -5: status %d body %q, want 400 naming the key", k, rr.Code, rr.Body.String())
		}
	}
	if got := cfg.Autonomous.EffectiveVerifierMinFreeMB(); got != 1024 {
		t.Fatalf("refused write changed the floor to %d", got)
	}

	if rr := put(map[string]interface{}{"autonomous.verifier_min_free_mem_mb": 0, "autonomous.verifier_headroom_max_age_hours": 48}); rr.Code != http.StatusOK {
		t.Fatalf("valid PUT status = %d: %s", rr.Code, rr.Body.String())
	}
	if got := cfg.Autonomous.EffectiveVerifierMinFreeMB(); got != 0 {
		t.Errorf("floor after 0 = %d, want 0 (check off)", got)
	}
	if got := cfg.Autonomous.EffectiveVerifierHeadroomMaxAge(); got != 48*time.Hour {
		t.Errorf("max age after 48 = %v, want 48h", got)
	}
	if rr := put(map[string]interface{}{"autonomous.verifier_headroom_max_age_hours": 0}); rr.Code != http.StatusOK {
		t.Fatalf("PUT 0 status = %d", rr.Code)
	}
	if got := cfg.Autonomous.EffectiveVerifierHeadroomMaxAge(); got != 24*time.Hour {
		t.Errorf("max age after 0 = %v, want the 24h default", got)
	}
}

// TestNodeWriteGate_SaveWithStoredVerifierSlotsIsGated pins that the gate
// covers every save that leaves verifier_slots above 1, not only a raise: a
// node already stored with an unsupported verifier_slots 3 is refused when it
// is saved again with the same 3, with no measurement, and a save with 3 is
// accepted once a fresh measurement supports it.
func TestNodeWriteGate_SaveWithStoredVerifierSlotsIsGated(t *testing.T) {
	srv, store, reg := newHeadroomGateServer(t)
	three := 3
	// Store the node with verifier_slots 3 directly in the registry, bypassing
	// the handler (the gate is in the handler), as a node saved earlier would be.
	if err := reg.Update(&compute.Node{Name: "gpubox", Kind: "ollama", Address: "http://gpubox:11434", VerifierSlots: &three}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	put := func(body string) (int, string) {
		rec := httptest.NewRecorder()
		srv.handleComputeNodes(rec, httptest.NewRequest(http.MethodPut, "/api/compute/nodes/gpubox?probe=skip", strings.NewReader(body)))
		return rec.Code, rec.Body.String()
	}
	body := `{"name":"gpubox","kind":"ollama","address":"http://gpubox:11434","verifier_slots":3,"session_slots":2}`
	if code, out := put(body); code != http.StatusConflict || !strings.Contains(out, "cap on node gpubox refused: no fresh measurement") {
		t.Fatalf("save with unsupported verifier_slots 3: status %d body %q, want 409 naming gpubox", code, out)
	}
	if n, _ := reg.Get("gpubox"); n == nil || n.SessionSlots != 0 {
		t.Fatalf("refused save changed the node: %+v", n)
	}
	store.Put(compute.HeadroomMeasurement{Node: "gpubox", FreeMB: 8192, PerRequestMB: 512, MeasuredAt: time.Now()})
	if code, out := put(body); code != http.StatusOK {
		t.Fatalf("save with supported verifier_slots 3: status %d body %q, want 200", code, out)
	}
	if n, _ := reg.Get("gpubox"); n == nil || n.SessionSlots != 2 {
		t.Fatalf("accepted save not stored: %+v", n)
	}
}
