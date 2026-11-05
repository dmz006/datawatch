package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/compute"
)

// newVerifierSlotsServer returns a Server with one node whose settings are
// driven from the registry, plus the node detail observer stub.
func newVerifierSlotsServer(t *testing.T, node *compute.Node) (*Server, *compute.Registry) {
	t.Helper()
	const payload = `{"gpu": [{"util_pct": 5, "mem_used_bytes": 0, "mem_total_bytes": 24000}]}`
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(payload))
	}))
	t.Cleanup(stub.Close)

	reg, err := compute.NewRegistry(t.TempDir() + "/nodes.json")
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	if err := reg.Add(node); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if node.MonitoringEndpoint == "" {
		node.MonitoringEndpoint = stub.URL
		if err := reg.Update(node); err != nil {
			t.Fatalf("Update endpoint: %v", err)
		}
	}
	return &Server{computeReg: reg, capacityLedger: capacity.New(capacity.Options{})}, reg
}

func i(p int) *int { x := p; return &x }

// TestNodePutRefusesNegativeVerifierSettings checks the replace path validates
// before it writes, with the node.go wording, for each of the three settings.
func TestNodePutRefusesNegativeVerifierSettings(t *testing.T) {
	for name, body := range map[string]string{
		"verifier_slots":           `{"name":"gpubox","kind":"ollama","address":"http://gpubox:11434","verifier_slots": -1}`,
		"verifier_min_free_mem_mb": `{"name":"gpubox","kind":"ollama","address":"http://gpubox:11434","verifier_min_free_mem_mb": -128}`,
		"verifier_context_mb":      `{"name":"gpubox","kind":"ollama","address":"http://gpubox:11434","verifier_context_mb": -1}`,
	} {
		t.Run(name, func(t *testing.T) {
			srv, reg := newVerifierSlotsServer(t, &compute.Node{Name: "gpubox", Kind: "ollama", Address: "http://gpubox:11434", MonitoringEndpoint: "http://stub", VerifierSlots: i(2)})
			rec := httptest.NewRecorder()
			srv.handleComputeNodes(rec, httptest.NewRequest(http.MethodPut, "/api/compute/nodes/gpubox?probe=skip", strings.NewReader(body)))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), "must be >= 0") {
				t.Errorf("error = %s, want the node.go \u003e= 0 wording", rec.Body.String())
			}
			if n, _ := reg.Get("gpubox"); n == nil || n.VerifierSlots == nil || *n.VerifierSlots != 2 {
				t.Errorf("the node was modified by the refused write: %+v", n)
			}
		})
	}
}

// TestNodePutStoresVerifierSettings checks PUT stores explicit values —
// including an explicit zero, which is different from unset — and that
// omitting the nullable settings clears the earlier override.
func TestNodePutStoresVerifierSettings(t *testing.T) {
	srv, reg := newVerifierSlotsServer(t, &compute.Node{Name: "gpubox", Kind: "ollama", Address: "http://gpubox:11434", VerifierSlots: i(4), VerifierMinFreeMemMB: i(2048)})

	identity := `{"name":"gpubox","kind":"ollama","address":"http://gpubox:11434"`
	rec := httptest.NewRecorder()
	srv.handleComputeNodes(rec, httptest.NewRequest(http.MethodPut, "/api/compute/nodes/gpubox?probe=skip",
		strings.NewReader(identity+`,"verifier_slots":0,"verifier_min_free_mem_mb":512,"verifier_context_mb":128}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	n, err := reg.Get("gpubox")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if n.VerifierSlots == nil || *n.VerifierSlots != 0 {
		t.Errorf("verifier_slots = %v, want explicit 0 (not unset)", n.VerifierSlots)
	}
	if n.VerifierMinFreeMemMB == nil || *n.VerifierMinFreeMemMB != 512 {
		t.Errorf("verifier_min_free_mem_mb = %v, want 512", n.VerifierMinFreeMemMB)
	}
	if n.VerifierContextMB != 128 {
		t.Errorf("verifier_context_mb = %d, want 128", n.VerifierContextMB)
	}

	// Omit the nullable settings: the replace clears them back to "unset".
	rec = httptest.NewRecorder()
	srv.handleComputeNodes(rec, httptest.NewRequest(http.MethodPut, "/api/compute/nodes/gpubox?probe=skip", strings.NewReader(identity+"}")))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if n, _ = reg.Get("gpubox"); n == nil {
		t.Fatal("the node vanished")
	}
	if n.VerifierSlots != nil || n.VerifierMinFreeMemMB != nil {
		t.Errorf("nullable settings = (%v, %v), want both cleared to unset", n.VerifierSlots, n.VerifierMinFreeMemMB)
	}
}

// TestNodeDetailCarriesVerifierFigures checks the detail adds the setting
// pair — the executor pair beside it — and the daemon's last-seen verifier
// admission state, exactly the fields a PWA or script can rely on.
func TestNodeDetailCarriesVerifierFigures(t *testing.T) {
	srv, _ := newVerifierSlotsServer(t, &compute.Node{
		Name: "gpubox", Kind: "ollama", Address: "http://gpubox:11434",
		VerifierSlots: i(2), VerifierMinFreeMemMB: i(512), VerifierContextMB: 128,
	})
	srv.capacityLedger.SetLimit("node:gpubox", 4)
	srv.capacityLedger.SetLimit("node-verify:gpubox", 2)
	suggested := 3
	srv.SetVerifierNodeStatus(func(string) (VerifierNodeStatus, bool) {
		return VerifierNodeStatus{
			ExecutorCap:    4,
			VerifierCap:    2,
			FreeMB:         8192,
			HasData:        true,
			HeadroomOK:     true,
			HeadroomReason: "8192 MB free, 512 MB required",
			SuggestedCap:   &suggested,
		}, true
	})

	rec := httptest.NewRecorder()
	srv.handleComputeNodeDetail(rec, httptest.NewRequest(http.MethodGet, "/api/compute/nodes/gpubox/detail", nil), "gpubox")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	got := map[string]json.RawMessage{}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	want := map[string]string{
		"derived_slots":                      "4",
		"executor_slots":                     "4",
		"verifier_slots":                     "2",
		"verifier_slots_effective":           "2",
		"suggested_verifier_slots":           "3",
		"verifier_min_free_mem_mb":           "512",
		"verifier_min_free_mem_mb_effective": "512",
		"verifier_context_mb":                "128",
		"headroom_ok":                        "true",
		"free_gpu_mb":                        "8192",
		"headroom_reason":                    `"8192 MB free, 512 MB required"`,
	}
	for k, v := range want {
		if string(got[k]) != v {
			t.Errorf("%s = %s, want %s", k, got[k], v)
		}
	}
}

// TestNodeDetailVerifierFigureVariants covers the two degenerate shapes: an
// unset nullable setting serialises as an explicit null with the 1024 MB
// default in effect, and a nil advice serialises as null too — never 0.
func TestNodeDetailVerifierFigureVariants(t *testing.T) {
	srv, _ := newVerifierSlotsServer(t, &compute.Node{
		Name: "gpubox", Kind: "ollama", Address: "http://gpubox:11434",
	})
	srv.SetVerifierNodeStatus(func(string) (VerifierNodeStatus, bool) {
		return VerifierNodeStatus{VerifierCap: 1, FreeMB: 4000, HasData: true, HeadroomOK: false, HeadroomReason: "4000 MB free, 1024 MB required"}, true
	})

	rec := httptest.NewRecorder()
	srv.handleComputeNodeDetail(rec, httptest.NewRequest(http.MethodGet, "/api/compute/nodes/gpubox/detail", nil), "gpubox")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	got := map[string]json.RawMessage{}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if string(got["verifier_slots"]) != "null" {
		t.Errorf("verifier_slots = %s, want null (the node never got an override)", got["verifier_slots"])
	}
	if string(got["verifier_min_free_mem_mb"]) != "null" {
		t.Errorf("verifier_min_free_mem_mb = %s, want null (unset)", got["verifier_min_free_mem_mb"])
	}
	if string(got["verifier_min_free_mem_mb_effective"]) != "1024" {
		t.Errorf("verifier_min_free_mem_mb_effective = %s, want 1024 (the default)", got["verifier_min_free_mem_mb_effective"])
	}
	if string(got["suggested_verifier_slots"]) != "null" {
		t.Errorf("suggested_verifier_slots = %s, want null (no usable figure), not some number", got["suggested_verifier_slots"])
	}
	if string(got["verifier_slots_effective"]) != "1" {
		t.Errorf("verifier_slots_effective = %s, want 1 (the cached cap)", got["verifier_slots_effective"])
	}
}

// TestNodeDetailWithoutReaderOmitsAdmissionState checks the admission figures
// are simply absent when the daemon did not wire the reader — the settings
// fields still come from the registry.
func TestNodeDetailWithoutReaderOmitsAdmissionState(t *testing.T) {
	srv, _ := newVerifierSlotsServer(t, &compute.Node{
		Name: "gpubox", Kind: "ollama", Address: "http://gpubox:11434",
		VerifierSlots: i(2), VerifierContextMB: 64,
	})

	rec := httptest.NewRecorder()
	srv.handleComputeNodeDetail(rec, httptest.NewRequest(http.MethodGet, "/api/compute/nodes/gpubox/detail", nil), "gpubox")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	got := map[string]json.RawMessage{}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	for _, k := range []string{"verifier_slots_effective", "suggested_verifier_slots", "headroom_ok", "headroom_reason", "free_gpu_mb"} {
		if _, ok := got[k]; ok {
			t.Errorf("%s = %s, want it left out (the reader is not wired)", k, got[k])
		}
	}
	if string(got["verifier_slots"]) != "2" {
		t.Errorf("verifier_slots = %s, want 2 (from the registry)", got["verifier_slots"])
	}
	if string(got["verifier_context_mb"]) != "64" {
		t.Errorf("verifier_context_mb = %s, want 64", got["verifier_context_mb"])
	}
}
