// Test file for the compute_node_drain / compute_node_undrain MCP tools.
//
// The daemon under test here fakes the REST side of the drain endpoints
// the same way the real server serves them: it runs the same registry
// SetDrain call internal/server's handleComputeNodeDrain uses, applies the
// same until-validation and unknown-node 404, and replies with the node
// health JSON. So the round-trip, past-until, and unknown-node cases
// exercise the same registry semantics the REST endpoint does.

package mcp

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/compute"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/session"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

type drainDaemon struct {
	reg     *compute.Registry
	method  string
	path    string
	body    string
	reached bool
}

func newDrainDaemon(t *testing.T) (*Server, *drainDaemon) {
	t.Helper()
	reg, err := compute.NewRegistry(filepath.Join(t.TempDir(), "nodes.json"))
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	if err := reg.Add(&compute.Node{Name: "gpu-1", Kind: compute.KindLocal}); err != nil {
		t.Fatalf("Add gpu-1: %v", err)
	}
	d := &drainDaemon{reg: reg}
	srv := httptest.NewServer(d)
	t.Cleanup(srv.Close)
	return &Server{webPort: bl427Port(t, srv)}, d
}

func (d *drainDaemon) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.URL.Path, "/api/compute/nodes/") || !strings.HasSuffix(r.URL.Path, "/drain") {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/compute/nodes/"), "/drain")
	now := time.Now()
	drain := r.Method == http.MethodPost
	var until time.Time
	var reason string
	if drain {
		raw, _ := io.ReadAll(r.Body)
		d.reached = true
		d.body = string(raw)
		var body struct {
			Until  string `json:"until"`
			Reason string `json:"reason"`
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
			return
		}
		if body.Until != "" {
			parsed, err := time.Parse(time.RFC3339, body.Until)
			if err != nil {
				http.Error(w, "until must be an RFC3339 timestamp", http.StatusBadRequest)
				return
			}
			if !parsed.After(now) {
				http.Error(w, "until must be after now", http.StatusBadRequest)
				return
			}
			until = parsed
		}
		reason = body.Reason
	}
	d.method = r.Method
	d.path = r.URL.Path
	if err := d.reg.SetDrain(name, drain, until, now, reason); err != nil {
		code := http.StatusInternalServerError
		if err == compute.ErrNotFound {
			code = http.StatusNotFound
		}
		http.Error(w, err.Error(), code)
		return
	}
	n, err := d.reg.Get(name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	out := map[string]any{
		"name":         n.Name,
		"drained":      n.Drained,
		"drain_active": n.DrainActive(now),
	}
	if !n.DrainUntil.IsZero() {
		out["drain_until"] = n.DrainUntil.UTC().Format(time.RFC3339)
	}
	if n.DrainReason != "" {
		out["drain_reason"] = n.DrainReason
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func drainReq(args map[string]any) mcpsdk.CallToolRequest {
	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = args
	return req
}

// TestComputeNodeDrainUndrainRoundTrip drives drain then undrain and checks
// the REST proxy shape and the node health JSON replies.
func TestComputeNodeDrainUndrainRoundTrip(t *testing.T) {
	s, d := newDrainDaemon(t)
	until := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)

	res, err := s.handleComputeNodeDrainMCP(t.Context(), drainReq(map[string]any{
		"name":   "gpu-1",
		"until":  until,
		"reason": "maintenance",
	}))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("drain: res=%v err=%v", res, err)
	}
	if d.method != http.MethodPost || d.path != "/api/compute/nodes/gpu-1/drain" {
		t.Errorf("drain request = %s %s, want POST /api/compute/nodes/gpu-1/drain", d.method, d.path)
	}
	if !strings.Contains(d.body, `"until":"`+until) || !strings.Contains(d.body, `"reason":"maintenance"`) {
		t.Errorf("drain body = %s, want until %q and reason forwarded", d.body, until)
	}
	out := resContentText(t, res)
	for _, want := range []string{`"drained":true`, `"drain_until"`, `"drain_reason":"maintenance"`} {
		if !strings.Contains(out, want) {
			t.Errorf("drain result %s lacks %s", out, want)
		}
	}
	if n, gerr := d.reg.Get("gpu-1"); gerr == nil && (!n.Drained || n.DrainReason != "maintenance") {
		t.Errorf("registry state after drain: drained=%v reason=%q", n.Drained, n.DrainReason)
	}

	res, err = s.handleComputeNodeUndrainMCP(t.Context(), drainReq(map[string]any{"name": "gpu-1"}))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("undrain: res=%v err=%v", res, err)
	}
	if d.method != http.MethodDelete || d.path != "/api/compute/nodes/gpu-1/drain" {
		t.Errorf("undrain request = %s %s, want DELETE /api/compute/nodes/gpu-1/drain", d.method, d.path)
	}
	out = resContentText(t, res)
	if !strings.Contains(out, `"drained":false`) {
		t.Errorf("undrain result %s, want drained:false", out)
	}
	if strings.Contains(out, `"drain_until"`) || strings.Contains(out, `"drain_reason"`) {
		t.Errorf("undrain result still carries drain state: %s", out)
	}
	if n, gerr := d.reg.Get("gpu-1"); gerr == nil && (n.Drained || n.DrainReason != "") {
		t.Errorf("registry state after undrain: drained=%v reason=%q", n.Drained, n.DrainReason)
	}
}

// TestComputeNodeDrainPastUntilError checks a past until is refused and
// comes back as an error without touching the registry.
func TestComputeNodeDrainPastUntilError(t *testing.T) {
	s, d := newDrainDaemon(t)
	res, err := s.handleComputeNodeDrainMCP(t.Context(), drainReq(map[string]any{
		"name":  "gpu-1",
		"until": time.Now().Add(-time.Hour).UTC().Format(time.RFC3339),
	}))
	if err == nil && (res == nil || !res.IsError) {
		t.Fatal("expected an error result for a past until")
	}
	if n, gerr := d.reg.Get("gpu-1"); gerr == nil && n.Drained {
		t.Error("node was drained despite the past until")
	}
}

// TestComputeNodeDrainUnknownNodeError checks a non-existent node name comes
// back as an error on both drain and undrain.
func TestComputeNodeDrainUnknownNodeError(t *testing.T) {
	s, _ := newDrainDaemon(t)
	if res, err := s.handleComputeNodeDrainMCP(t.Context(), drainReq(map[string]any{"name": "ghost"})); err == nil && (res == nil || !res.IsError) {
		t.Fatal("drain: expected an error result for an unknown node")
	}
	if res, err := s.handleComputeNodeUndrainMCP(t.Context(), drainReq(map[string]any{"name": "ghost"})); err == nil && (res == nil || !res.IsError) {
		t.Fatal("undrain: expected an error result for an unknown node")
	}
}

// TestComputeNodeDrainToolsRegistered checks both tool names appear in the
// registered tool list of a real New()-built server.
func TestComputeNodeDrainToolsRegistered(t *testing.T) {
	dir := t.TempDir()
	mgr, err := session.NewManager("testhost", dir, "/bin/echo", 0)
	if err != nil {
		t.Fatalf("session.NewManager: %v", err)
	}
	allowTestTemp(mgr)
	trackSessionManager(t, mgr)
	mgr.WithFakeTmux()

	s := New("testhost", mgr, &config.MCPConfig{Enabled: true}, dir, Options{Version: "test"})
	tools, err := s.ListTools()
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	seen := map[string]bool{}
	for _, tl := range tools {
		seen[tl.Name] = true
	}
	for _, want := range []string{"compute_node_drain", "compute_node_undrain"} {
		if !seen[want] {
			t.Errorf("registered tool list lacks %s", want)
		}
	}
}

// TestComputeNodeDrainToolCapabilities maps both drain tools to the same
// capability as the other compute write tools.
func TestComputeNodeDrainToolCapabilities(t *testing.T) {
	for _, name := range []string{"compute_node_drain", "compute_node_undrain"} {
		cap, ok := federation.RequiredCapForMCPTool(name)
		if !ok {
			t.Errorf("%s: no federation.MCPToolCap entry", name)
			continue
		}
		if cap != federation.CapComputeWrite {
			t.Errorf("%s: mapped to %q, want %q", name, cap, federation.CapComputeWrite)
		}
	}
}
