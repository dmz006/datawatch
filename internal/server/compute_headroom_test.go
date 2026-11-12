package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/compute"
	"github.com/dmz006/datawatch/internal/config"
)

// fakeHeadroomProbe stands in for the Ollama probe: fixed free memory, a fixed
// per-request cost and a fixed resident model. It counts the requests it
// receives, so a test can prove a refused measurement never reached the node.
type fakeHeadroomProbe struct {
	freeMB, perReqMB int
	resident         string
	reads, requests  int
	err              error
}

func (p *fakeHeadroomProbe) ReadFreeMB(_ context.Context, _ compute.Node) (int, string, error) {
	p.reads++
	return p.freeMB, p.resident, p.err
}

func (p *fakeHeadroomProbe) RequestMemoryMB(_ context.Context, _ compute.Node, _ string) (int, error) {
	p.requests++
	return p.perReqMB, nil
}

// headroomTestEnv is one node ("gpubox") on a server wired the way the daemon
// wires it: one headroom store, the default autonomous config, the ledger, a
// served-models cache over a fixed model list, a fake probe and a fixed clock.
type headroomTestEnv struct {
	srv   *Server
	store *compute.HeadroomStore
	led   *capacity.Ledger
	probe *fakeHeadroomProbe
	now   time.Time
}

// newHeadroomTestEnv serves served (the node's models) and allows the models
// in allowed through the model-pull consent.
func newHeadroomTestEnv(t *testing.T, served []string, allowed ...string) *headroomTestEnv {
	t.Helper()
	srv, _ := newVerifierSlotsServer(t, &compute.Node{Name: "gpubox", Kind: "ollama", Address: "http://gpubox:11434", MonitoringEndpoint: "http://stub"})
	env := &headroomTestEnv{
		srv:   srv,
		store: compute.NewHeadroomStore(),
		led:   srv.capacityLedger,
		probe: &fakeHeadroomProbe{freeMB: 8192, perReqMB: 512, resident: "tiny:1b"},
		now:   time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC),
	}
	cfg := config.DefaultConfig()
	for _, m := range allowed {
		cfg.Autonomous.ModelPullConsent.Models = append(cfg.Autonomous.ModelPullConsent.Models,
			config.ModelConsentEntry{Name: m, Allowed: true})
	}
	srv.cfg = cfg
	srv.headroomStore = env.store
	srv.headroomProbe = env.probe
	srv.headroomClock = func() time.Time { return env.now }
	lister := func(_ context.Context, _ *compute.Node, _ time.Duration) ([]string, error) {
		return served, nil
	}
	srv.servedModels = compute.NewServedModelsCache(lister,
		func() time.Duration { return time.Minute },
		func() time.Duration { return time.Second },
		func() time.Time { return env.now })
	return env
}

// headroomRequest sends one method to the node's /headroom route through the
// real node dispatcher and returns the status and body.
func headroomRequest(t *testing.T, srv *Server, method, node string) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.handleComputeNodes(rec, httptest.NewRequest(method, "/api/compute/nodes/"+node+"/headroom", nil))
	return rec.Code, rec.Body.String()
}

func decodeHeadroom(t *testing.T, body string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("decode %q: %v", body, err)
	}
	return m
}

// lease holds a worker lease on the node's pool, as a running task would.
func (e *headroomTestEnv) lease(t *testing.T, node string) {
	t.Helper()
	e.led.SetLimit("node:"+node, 2)
	if err := e.led.Acquire(context.Background(), capacity.Request{
		Holder: "task-busy", PRDID: "prd-busy", Pools: []string{"node:" + node}, Node: node,
	}, 0, nil, nil); err != nil {
		t.Fatalf("acquire lease: %v", err)
	}
}

// TestHeadroomMeasure_SuccessStoresAndGetReturnsView covers the success path:
// POST measures on demand and stores the result, and GET returns the stored
// view with its age, freshness and the cap it supports.
func TestHeadroomMeasure_SuccessStoresAndGetReturnsView(t *testing.T) {
	env := newHeadroomTestEnv(t, []string{"big:70b", "tiny:1b"}, "tiny:1b", "big:70b")

	code, body := headroomRequest(t, env.srv, http.MethodPost, "gpubox")
	if code != http.StatusOK {
		t.Fatalf("POST status %d: %s", code, body)
	}
	got := decodeHeadroom(t, body)
	// 1 + (8192 - 1024 floor) / 512 = 15 parallel requests.
	if got["supported_cap"] != float64(15) || got["fresh"] != true || got["free_mb"] != float64(8192) {
		t.Fatalf("POST view = %v, want supported_cap 15, fresh true, free_mb 8192", got)
	}
	if env.probe.requests != 1 {
		t.Errorf("probe requests = %d, want 1 (the probe model only)", env.probe.requests)
	}
	stored, ok := env.store.Get("gpubox")
	if !ok || stored.FreeMB != 8192 || stored.PerRequestMB != 512 {
		t.Fatalf("store after POST = %+v ok=%v, want the measurement stored", stored, ok)
	}

	env.now = env.now.Add(90 * time.Second)
	code, body = headroomRequest(t, env.srv, http.MethodGet, "gpubox")
	if code != http.StatusOK {
		t.Fatalf("GET status %d: %s", code, body)
	}
	got = decodeHeadroom(t, body)
	if got["age_seconds"] != float64(90) || got["fresh"] != true || got["supported_cap"] != float64(15) {
		t.Errorf("GET view = %v, want age 90, fresh, supported_cap 15", got)
	}
	if env.probe.requests != 1 {
		t.Errorf("GET measured the node: probe requests = %d, want still 1", env.probe.requests)
	}
}

// TestHeadroomMeasure_BusyNodeRefused: a node holding a lease is refused with
// 409, names the node and the held count, and nothing is measured or stored.
func TestHeadroomMeasure_BusyNodeRefused(t *testing.T) {
	env := newHeadroomTestEnv(t, []string{"tiny:1b"}, "tiny:1b")
	env.lease(t, "gpubox")

	code, body := headroomRequest(t, env.srv, http.MethodPost, "gpubox")
	if code != http.StatusConflict {
		t.Fatalf("busy POST status %d, want 409: %s", code, body)
	}
	if !strings.Contains(body, "node busy") || !strings.Contains(body, `"node":"gpubox"`) {
		t.Errorf("busy body %q, want it to name the busy node", body)
	}
	if env.probe.reads != 0 || env.probe.requests != 0 {
		t.Errorf("busy node was probed: reads %d requests %d, want none", env.probe.reads, env.probe.requests)
	}
	if _, ok := env.store.Get("gpubox"); ok {
		t.Errorf("busy refusal stored a measurement")
	}
}

// TestHeadroomMeasure_ModelNotAllowedRefused covers both model gates: no
// pull-allowed model served at all (403), and a resident model that is not on
// the pull-allowed list while the probe model is allowed (403).
func TestHeadroomMeasure_ModelNotAllowedRefused(t *testing.T) {
	t.Run("no allowed model served", func(t *testing.T) {
		env := newHeadroomTestEnv(t, []string{"big:70b"}) // no consent entries at all
		code, body := headroomRequest(t, env.srv, http.MethodPost, "gpubox")
		if code != http.StatusForbidden || !strings.Contains(body, "model not allowed") {
			t.Fatalf("status %d body %q, want 403 model not allowed", code, body)
		}
		if env.probe.requests != 0 {
			t.Errorf("refused measurement sent a probe request")
		}
		if _, ok := env.store.Get("gpubox"); ok {
			t.Errorf("refused measurement stored a result")
		}
	})
	t.Run("resident model not allowed", func(t *testing.T) {
		env := newHeadroomTestEnv(t, []string{"tiny:1b"}, "tiny:1b")
		env.probe.resident = "other:7b" // resident on the node, not pull-allowed
		code, body := headroomRequest(t, env.srv, http.MethodPost, "gpubox")
		if code != http.StatusForbidden || !strings.Contains(body, "other:7b") {
			t.Fatalf("status %d body %q, want 403 naming the resident model", code, body)
		}
		if env.probe.requests != 0 {
			t.Errorf("refused measurement sent a probe request")
		}
	})
}

// TestHeadroomGet_NoMeasurementYet: a node never measured answers 404 on GET
// and nothing is measured by the GET.
func TestHeadroomGet_NoMeasurementYet(t *testing.T) {
	env := newHeadroomTestEnv(t, []string{"tiny:1b"}, "tiny:1b")
	code, body := headroomRequest(t, env.srv, http.MethodGet, "gpubox")
	if code != http.StatusNotFound || !strings.Contains(body, "no headroom measurement") {
		t.Fatalf("GET status %d body %q, want 404 no headroom measurement", code, body)
	}
	if env.probe.reads != 0 {
		t.Errorf("GET probed the node: reads %d", env.probe.reads)
	}
}

// TestHeadroomGet_StaleMeasurementSupportsOneCap: once the stored measurement
// is older than the configured age bound the view reads not fresh and the
// supported cap falls back to 1.
func TestHeadroomGet_StaleMeasurementSupportsOneCap(t *testing.T) {
	env := newHeadroomTestEnv(t, []string{"tiny:1b"}, "tiny:1b")
	if code, body := headroomRequest(t, env.srv, http.MethodPost, "gpubox"); code != http.StatusOK {
		t.Fatalf("POST status %d: %s", code, body)
	}
	env.now = env.now.Add(25 * time.Hour) // default bound is 24h
	code, body := headroomRequest(t, env.srv, http.MethodGet, "gpubox")
	if code != http.StatusOK {
		t.Fatalf("GET status %d: %s", code, body)
	}
	got := decodeHeadroom(t, body)
	if got["fresh"] != false || got["supported_cap"] != float64(1) {
		t.Errorf("stale view = %v, want fresh false and supported_cap 1", got)
	}
}

// TestHeadroomMeasure_UnknownNodeAndProbeFailure: an unknown node is 404; a
// probe that cannot read the node is 502 and stores nothing.
func TestHeadroomMeasure_UnknownNodeAndProbeFailure(t *testing.T) {
	env := newHeadroomTestEnv(t, []string{"tiny:1b"}, "tiny:1b")
	if code, _ := headroomRequest(t, env.srv, http.MethodPost, "nope"); code != http.StatusNotFound {
		t.Errorf("unknown node status %d, want 404", code)
	}
	env.probe.err = errors.New("observer silent")
	if code, body := headroomRequest(t, env.srv, http.MethodPost, "gpubox"); code != http.StatusBadGateway {
		t.Errorf("probe failure status %d body %q, want 502", code, body)
	}
	if _, ok := env.store.Get("gpubox"); ok {
		t.Errorf("failed probe stored a measurement")
	}
}

// TestHeadroomSurfaces_ShareOneService is the parity check on the REST pair:
// the POST response is exactly the stored view the GET returns, and both are
// the one view the service renders. The MCP, CLI and comm surfaces reach the
// same route, which their own tests pin.
func TestHeadroomSurfaces_ShareOneService(t *testing.T) {
	env := newHeadroomTestEnv(t, []string{"tiny:1b"}, "tiny:1b")
	code, posted := headroomRequest(t, env.srv, http.MethodPost, "gpubox")
	if code != http.StatusOK {
		t.Fatalf("POST status %d: %s", code, posted)
	}
	code, got := headroomRequest(t, env.srv, http.MethodGet, "gpubox")
	if code != http.StatusOK {
		t.Fatalf("GET status %d: %s", code, got)
	}
	if posted != got {
		t.Errorf("POST and GET disagree:\nPOST %s\nGET  %s", posted, got)
	}
	view, err := env.srv.nodeHeadroomView("gpubox")
	if err != nil {
		t.Fatalf("nodeHeadroomView: %v", err)
	}
	direct, _ := json.Marshal(view)
	if string(direct) != strings.TrimSpace(got) {
		t.Errorf("service view %s != REST GET %s", direct, got)
	}
}
