// REST surface for the compute-node headroom measurement:
// POST /api/compute/nodes/{name}/headroom measures while the node is idle
// and stores the result (the thing the verifier_slots write gate reads),
// GET returns it with age, freshness, and the cap it supports. A busy node
// (any worker holding a lease in its pool), an empty pull-allowed model
// list, and a resident model outside that list are all refused before a
// probe runs. Fake probe, fixed clock.

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/compute"
	"github.com/dmz006/datawatch/internal/config"
)

var headroomNow = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

// fakeHeadroomProbe is the fixed transport behind the measurement: it
// reports the configured free memory and per-request cost, records which
// model the request probe used, and can fail either call.
type fakeHeadroomProbe struct {
	freeMB     int
	resident   string
	perRequest int
	readErr    error
	requestErr error
	lastModel  string
}

func (f *fakeHeadroomProbe) ReadFreeMB(context.Context, compute.Node) (int, string, error) {
	if f.readErr != nil {
		return 0, "", f.readErr
	}
	return f.freeMB, f.resident, nil
}

func (f *fakeHeadroomProbe) RequestMemoryMB(_ context.Context, _ compute.Node, model string) (int, error) {
	f.lastModel = model
	if f.requestErr != nil {
		return 0, f.requestErr
	}
	return f.perRequest, nil
}

// newHeadroomServer wires a Server with the measurement surface fully
// deterministic: a compute registry holding the node, a fresh HeadroomStore,
// the fixed clock, the supplied probe, and the supplied allow list. held>0
// takes held leases in the node's pool so the busy check is real.
func newHeadroomServer(t *testing.T, node *compute.Node, probe compute.HeadroomProbe, models []string, held int) (*Server, *compute.HeadroomStore) {
	t.Helper()
	dir := t.TempDir()
	reg, err := compute.NewRegistry(filepath.Join(dir, "nodes.json"))
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	if err := reg.Add(node); err != nil {
		t.Fatalf("registry Add: %v", err)
	}
	s := &Server{
		cfg:                 config.DefaultConfig(),
		cfgPath:             filepath.Join(dir, "config.yaml"),
		computeReg:          reg,
		verifierHeadroom:    compute.NewHeadroomStore(),
		verifierHeadroomNow: func() time.Time { return headroomNow },
		headroomProbe:       probe,
		headroomAllowedModels: models,
		headroomClock:       func() time.Time { return headroomNow },
	}
	if held > 0 {
		led := capacity.New(capacity.Options{})
		led.SetLimit("node:"+node.Name, held+1)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		for i := 0; i < held; i++ {
			if err := led.Acquire(ctx, capacity.Request{Holder: "worker" + string(rune('1'+i)), Pools: []string{"node:" + node.Name}, Node: node.Name}, 0, nil, nil); err != nil {
				t.Fatalf("take lease %d: %v", i, err)
			}
		}
		s.capacityLedger = led
	}
	return s, s.verifierHeadroom
}

func headroomReq(t *testing.T, s *Server, name string, method string) (int, map[string]any) {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleComputeNodes(rr, httptest.NewRequest(method, "/api/compute/nodes/"+name+"/headroom", bytes.NewReader(nil)))
	var out map[string]any
	if b := rr.Body.Bytes(); len(b) > 0 {
		if err := json.Unmarshal(b, &out); err != nil {
			out = map[string]any{"raw": string(b)}
		}
	}
	return rr.Code, out
}

func TestHeadroomMeasureStoresMeasurement(t *testing.T) {
	probe := &fakeHeadroomProbe{freeMB: 12000, resident: "qwen3:0.6b", perRequest: 1500}
	s, hs := newHeadroomServer(t, &compute.Node{Name: "gpu-a", Kind: compute.KindLocal}, probe,
		[]string{"qwen3:0.6b", "qwen3:4b"}, 0)

	code, body := headroomReq(t, s, "gpu-a", http.MethodPost)
	if code != http.StatusOK {
		t.Fatalf("POST headroom: want 200, got %d (%v)", code, body)
	}
	if probe.lastModel != "qwen3:0.6b" {
		t.Fatalf("probe used model %q, want the smallest allowed (qwen3:0.6b)", probe.lastModel)
	}
	if body["node"] != "gpu-a" || body["free_mb"] != float64(12000) || body["per_request_mb"] != float64(1500) {
		t.Fatalf("measurement body = %v, want node gpu-a, free_mb 12000, per_request_mb 1500", body)
	}
	if body["resident_model"] != "qwen3:0.6b" {
		t.Fatalf("resident_model = %v, want qwen3:0.6b", body["resident_model"])
	}
	if body["fresh"] != true || body["age_seconds"] != float64(0) {
		t.Fatalf("freshness = fresh %v age %v, want fresh true age 0", body["fresh"], body["age_seconds"])
	}
	if body["min_free_mb"] != float64(2048) {
		t.Fatalf("min_free_mb = %v, want 2048 (default floor)", body["min_free_mb"])
	}
	if body["supported_cap"] != float64(7) {
		// 1 + (12000-2048)/1500 = 1 + 6.
		t.Fatalf("supported_cap = %v, want 7", body["supported_cap"])
	}
	if m, ok := hs.Get("gpu-a"); !ok || m.FreeMB != 12000 || m.PerRequestMB != 1500 {
		t.Fatalf("store after measure = %+v ok=%v, want the measurement stored", m, ok)
	}

	// The GET returns the same measurement with the same cap.
	code, body = headroomReq(t, s, "gpu-a", http.MethodGet)
	if code != http.StatusOK || body["supported_cap"] != float64(7) || body["fresh"] != true {
		t.Fatalf("GET after measure: code %d body %v, want 200 with the stored measurement", code, body)
	}
}

func TestHeadroomMeasureRefusedWhenBusy(t *testing.T) {
	probe := &fakeHeadroomProbe{freeMB: 12000, resident: "qwen3:0.6b", perRequest: 1500}
	s, hs := newHeadroomServer(t, &compute.Node{Name: "gpu-a", Kind: compute.KindLocal}, probe,
		[]string{"qwen3:0.6b"}, 2)

	code, body := headroomReq(t, s, "gpu-a", http.MethodPost)
	if code != http.StatusConflict {
		t.Fatalf("POST on a busy node: want 409, got %d (%v)", code, body)
	}
	if body["node"] != "gpu-a" {
		t.Fatalf("409 body node = %v, want gpu-a", body["node"])
	}
	if got := body["held"]; got != float64(2) {
		t.Fatalf("409 body held = %v, want 2", got)
	}
	if _, ok := hs.Get("gpu-a"); ok {
		t.Fatal("a refused measurement must not be stored")
	}
	if probe.lastModel != "" {
		t.Fatal("no probe request may run on a busy node")
	}
}

func TestHeadroomMeasureRefusedWithoutAllowedModels(t *testing.T) {
	s, _ := newHeadroomServer(t, &compute.Node{Name: "gpu-a", Kind: compute.KindLocal},
		&fakeHeadroomProbe{}, nil, 0)

	code, body := headroomReq(t, s, "gpu-a", http.MethodPost)
	if code != http.StatusForbidden {
		t.Fatalf("POST with an empty allow list: want 403, got %d (%v)", code, body)
	}
	if got, _ := body["error"].(string); got == "" {
		t.Fatal("403 body should carry the refusal reason")
	}
}

func TestHeadroomMeasureRefusedWhenResidentNotAllowed(t *testing.T) {
	probe := &fakeHeadroomProbe{freeMB: 12000, resident: "qwen3:0.6b", perRequest: 1500}
	s, _ := newHeadroomServer(t, &compute.Node{Name: "gpu-a", Kind: compute.KindLocal}, probe,
		[]string{"qwen3:4b"}, 0)

	code, body := headroomReq(t, s, "gpu-a", http.MethodPost)
	if code != http.StatusForbidden {
		t.Fatalf("POST with the resident model outside the allow list: want 403, got %d (%v)", code, body)
	}
	if probe.lastModel != "" {
		t.Fatal("no probe request may run when the resident model is not allowed")
	}
}

func TestHeadroomMeasureProbeFailureIs502(t *testing.T) {
	s, _ := newHeadroomServer(t, &compute.Node{Name: "gpu-a", Kind: compute.KindLocal},
		&fakeHeadroomProbe{readErr: errors.New("observer down")}, []string{"qwen3:0.6b"}, 0)

	code, _ := headroomReq(t, s, "gpu-a", http.MethodPost)
	if code != http.StatusBadGateway {
		t.Fatalf("POST with a failing probe: want 502, got %d", code)
	}
}

func TestHeadroomGetUnknownNode(t *testing.T) {
	s, _ := newHeadroomServer(t, &compute.Node{Name: "gpu-a", Kind: compute.KindLocal}, nil, nil, 0)
	if code, _ := headroomReq(t, s, "ghost", http.MethodGet); code != http.StatusNotFound {
		t.Fatalf("GET for a never-measured node: want 404, got %d", code)
	}
	if code, _ := headroomReq(t, s, "ghost", http.MethodPost); code != http.StatusNotFound {
		t.Fatalf("POST for an unknown node: want 404, got %d", code)
	}
}

func TestHeadroomStaleMeasurementReadsStale(t *testing.T) {
	s, hs := newHeadroomServer(t, &compute.Node{Name: "gpu-a", Kind: compute.KindLocal}, nil, nil, 0)
	hs.Put(compute.HeadroomMeasurement{
		Node: "gpu-a", FreeMB: 12000, PerRequestMB: 1500,
		ResidentModel: "qwen3:0.6b", MeasuredAt: headroomNow.Add(-25 * time.Hour),
	})

	code, body := headroomReq(t, s, "gpu-a", http.MethodGet)
	if code != http.StatusOK {
		t.Fatalf("GET stale measurement: want 200, got %d (%v)", code, body)
	}
	if body["fresh"] != false {
		t.Fatalf("fresh = %v, want false (25h old against the 24h bound)", body["fresh"])
	}
	if got := body["age_seconds"]; got != float64(25*3600) {
		t.Fatalf("age_seconds = %v, want %d", got, 25*3600)
	}
	if body["supported_cap"] != float64(1) {
		t.Fatalf("stale supported_cap = %v, want 1 (the gate's fallback)", body["supported_cap"])
	}
}

func TestHeadroomZeroCostRequestIsUnbounded(t *testing.T) {
	s, _ := newHeadroomServer(t, &compute.Node{Name: "gpu-a", Kind: compute.KindLocal},
		&fakeHeadroomProbe{freeMB: 12000, resident: "qwen3:0.6b", perRequest: 0}, []string{"qwen3:0.6b"}, 0)

	code, body := headroomReq(t, s, "gpu-a", http.MethodPost)
	if code != http.StatusOK {
		t.Fatalf("POST with a zero-cost request: want 200, got %d (%v)", code, body)
	}
	if body["supported_cap"] != float64(0) {
		t.Fatalf("supported_cap = %v, want 0 (unbounded, the ledger's convention)", body["supported_cap"])
	}
}
