package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/compute"
)

// fixedDrainClock is the server clock the drain tests run against.
var fixedDrainClock = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

// newComputeDrainServer builds a Server with a registry holding one node
// ("gpu-a") and pins the server clock to fixedDrainClock.
func newComputeDrainServer(t *testing.T) (*Server, *compute.Registry) {
	t.Helper()
	reg, err := compute.NewRegistry(filepath.Join(t.TempDir(), "nodes.json"))
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	if err := reg.Add(&compute.Node{
		Name:    "gpu-a",
		Kind:    compute.KindRemote,
		Address: "https://gpu-a:11434",
	}); err != nil {
		t.Fatalf("Add node: %v", err)
	}
	prev := serverNow
	serverNow = func() time.Time { return fixedDrainClock }
	t.Cleanup(func() { serverNow = prev })
	return &Server{computeReg: reg}, reg
}

// drainRequest runs method+path through handleComputeNodes and returns the
// decoded JSON object body (when 2xx) plus the status code.
func drainRequest(t *testing.T, srv *Server, method, path, body string) (int, map[string]any) {
	t.Helper()
	w := httptest.NewRecorder()
	srv.handleComputeNodes(w, httptest.NewRequest(method, path, strings.NewReader(body)))
	out := map[string]any{}
	if w.Code >= 200 && w.Code < 300 {
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatalf("%s %s: decode %d body %s: %v", method, path, w.Code, w.Body.String(), err)
		}
	}
	return w.Code, out
}

func TestDrainSetsDrainedWithUntil(t *testing.T) {
	srv, reg := newComputeDrainServer(t)

	until := fixedDrainClock.Add(time.Hour)
	code, out := drainRequest(t, srv, http.MethodPost, "/api/compute/nodes/gpu-a/drain",
		`{"until":"`+until.Format(time.RFC3339)+`","reason":"maintenance"}`)
	if code != http.StatusOK {
		t.Fatalf("drain: got %d, want 200", code)
	}
	if out["drained"] != true {
		t.Fatalf("drained = %v, want true (body %s)", out["drained"], out)
	}
	if out["drain_until"] != until.Format(time.RFC3339) {
		t.Fatalf("drain_until = %v, want %s", out["drain_until"], until.Format(time.RFC3339))
	}
	if out["drain_reason"] != "maintenance" {
		t.Fatalf("drain_reason = %v, want maintenance", out["drain_reason"])
	}
	n, err := reg.Get("gpu-a")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !n.Drained || !n.DrainUntil.Equal(until) || n.DrainReason != "maintenance" {
		t.Fatalf("registry state = drained=%v until=%v reason=%q", n.Drained, n.DrainUntil, n.DrainReason)
	}
	// GET health reports the same new state.
	code, out = drainRequest(t, srv, http.MethodGet, "/api/compute/nodes/gpu-a/health", "")
	if code != http.StatusOK || out["drained"] != true || out["drain_until"] != until.Format(time.RFC3339) {
		t.Fatalf("health after drain: code=%d body=%v", code, out)
	}
}

func TestDrainWithoutUntilIsOpenEnded(t *testing.T) {
	srv, reg := newComputeDrainServer(t)

	code, out := drainRequest(t, srv, http.MethodPost, "/api/compute/nodes/gpu-a/drain", "")
	if code != http.StatusOK {
		t.Fatalf("open-ended drain: got %d, want 200", code)
	}
	if out["drained"] != true || out["drain_active"] != true {
		t.Fatalf("open-ended drain state = %v", out)
	}
	if _, ok := out["drain_until"]; ok {
		t.Fatalf("open-ended drain must not carry drain_until: %v", out)
	}
	n, _ := reg.Get("gpu-a")
	if !n.Drained || !n.DrainUntil.IsZero() {
		t.Fatalf("registry state = drained=%v until=%v", n.Drained, n.DrainUntil)
	}
}

func TestDrainPastUntil400(t *testing.T) {
	srv, reg := newComputeDrainServer(t)

	for _, body := range []string{
		`{"until":"` + fixedDrainClock.Add(-time.Minute).Format(time.RFC3339) + `"}`,
		`{"until":"` + fixedDrainClock.Format(time.RFC3339) + `"}`,
		`{"until":"not-a-timestamp"}`,
	} {
		code, _ := drainRequest(t, srv, http.MethodPost, "/api/compute/nodes/gpu-a/drain", body)
		if code != http.StatusBadRequest {
			t.Fatalf("drain with body %s: got %d, want 400", body, code)
		}
	}
	n, _ := reg.Get("gpu-a")
	if n.Drained {
		t.Fatalf("rejected drains must not set the drain state")
	}
}

func TestDrainBadJSON400(t *testing.T) {
	srv, reg := newComputeDrainServer(t)

	code, _ := drainRequest(t, srv, http.MethodPost, "/api/compute/nodes/gpu-a/drain", `{"until":`)
	if code != http.StatusBadRequest {
		t.Fatalf("drain with truncated json: got %d, want 400", code)
	}
	n, _ := reg.Get("gpu-a")
	if n.Drained {
		t.Fatalf("a malformed drain body must not set the drain state")
	}
}

func TestDrainUnknownNode404(t *testing.T) {
	srv, _ := newComputeDrainServer(t)

	code, _ := drainRequest(t, srv, http.MethodPost, "/api/compute/nodes/ghost/drain", `{}`)
	if code != http.StatusNotFound {
		t.Fatalf("drain unknown node: got %d, want 404", code)
	}
	code, _ = drainRequest(t, srv, http.MethodDelete, "/api/compute/nodes/ghost/drain", "")
	if code != http.StatusNotFound {
		t.Fatalf("undrain unknown node: got %d, want 404", code)
	}
}

func TestDrainUndrainClears(t *testing.T) {
	srv, reg := newComputeDrainServer(t)

	if code, _ := drainRequest(t, srv, http.MethodPost, "/api/compute/nodes/gpu-a/drain",
		`{"until":"`+fixedDrainClock.Add(time.Hour).Format(time.RFC3339)+`","reason":"maintenance"}`); code != http.StatusOK {
		t.Fatalf("drain: got %d, want 200", code)
	}
	code, out := drainRequest(t, srv, http.MethodDelete, "/api/compute/nodes/gpu-a/drain", "")
	if code != http.StatusOK {
		t.Fatalf("undrain: got %d, want 200", code)
	}
	if out["drained"] != false || out["drain_active"] != false {
		t.Fatalf("after undrain: %v", out)
	}
	if _, ok := out["drain_until"]; ok {
		t.Fatalf("after undrain drain_until must be gone: %v", out)
	}
	if out["drain_reason"] != "" {
		t.Fatalf("after undrain drain_reason = %v, want empty", out["drain_reason"])
	}
	n, _ := reg.Get("gpu-a")
	if n.Drained || !n.DrainUntil.IsZero() || n.DrainReason != "" {
		t.Fatalf("registry not cleared: drained=%v until=%v reason=%q", n.Drained, n.DrainUntil, n.DrainReason)
	}
	// Idempotent: undraining again is still 200.
	code, out = drainRequest(t, srv, http.MethodDelete, "/api/compute/nodes/gpu-a/drain", "")
	if code != http.StatusOK || out["drained"] != false {
		t.Fatalf("second undrain: code=%d body=%v", code, out)
	}
}

func TestDrainNodeWithRunningLeaseReportedDrained(t *testing.T) {
	srv, _ := newComputeDrainServer(t)

	// The node still holds a running lease (capacity admission held by a
	// worker). Draining must not fail or mask that; the response reports
	// the node as drained.
	led := capacity.New(capacity.Options{})
	led.SetLimit("node:gpu-a", 2)
	if err := led.Acquire(context.Background(), capacity.Request{
		Holder: "task-a", PRDID: "prd-a", Pools: []string{"node:gpu-a"}, Node: "gpu-a",
	}, 0, nil, nil); err != nil {
		t.Fatalf("acquire lease: %v", err)
	}

	code, out := drainRequest(t, srv, http.MethodPost, "/api/compute/nodes/gpu-a/drain",
		`{"reason":"maintenance"}`)
	if code != http.StatusOK {
		t.Fatalf("drain leased node: got %d, want 200", code)
	}
	if out["drained"] != true {
		t.Fatalf("drained node with a running lease must report drained=true: %v", out)
	}
	code, out = drainRequest(t, srv, http.MethodGet, "/api/compute/nodes/gpu-a/health", "")
	if code != http.StatusOK || out["drained"] != true {
		t.Fatalf("health of leased drained node: code=%d body=%v", code, out)
	}
}
