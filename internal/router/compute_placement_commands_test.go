// The comm channel's placement verbs (list / approve / deny the ask queue)
// and the five `configure compute_placement.*` keys must reach the daemon's
// REST surface on the same wire the operators already know.

package router

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// placementCommFake stands in for the daemon REST endpoints the comm
// handlers proxy: GET /api/compute/placements and the one-shot decision
// POSTs.
func placementCommFake(t *testing.T) (*httptest.Server, *struct {
	approved []string
	denied   []string
}) {
	t.Helper()
	state := struct {
		approved []string
		denied   []string
	}{}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/compute/placements", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"asks":[{"id":"ask-1","model":"llama3.1-8b","node":"gpu-box","reason":"needs 12 GB, best node fits 10 GB","requested_at":"2026-01-01T00:00:00Z","decision":"pending"}],"pending":1}`))
	})
	mux.HandleFunc("/api/compute/placements/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// path is /api/compute/placements/<id>/<action>
		parts := strings.Split(r.URL.Path, "/")
		id := parts[len(parts)-2]
		action := parts[len(parts)-1]
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if id != "ask-1" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"placement ask ` + id + ` not found"}`))
			return
		}
		switch action {
		case "approve":
			state.approved = append(state.approved, id)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "model": "llama3.1-8b", "node": "gpu-box", "decision": "approved"})
		case "deny":
			state.denied = append(state.denied, id)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "model": "llama3.1-8b", "node": "gpu-box", "decision": "denied"})
		default:
			_, _ = w.Write([]byte(`{"error":"unknown placement action ` + action + `"}`))
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &state
}

func TestComputePlacementListProxiesREST(t *testing.T) {
	srv, _ := placementCommFake(t)
	r := loopbackRouter(t, srv)
	out := captureSends(r, func() {
		r.handleComputePlacementCmd("list")
	})
	for _, want := range []string{"ask-1", "llama3.1-8b", "gpu-box", "pending"} {
		if !strings.Contains(out, want) {
			t.Errorf("list reply missing %q:\n%s", want, out)
		}
	}
}

func TestComputePlacementApproveAndDenyRouteToREST(t *testing.T) {
	srv, state := placementCommFake(t)
	r := loopbackRouter(t, srv)
	out := captureSends(r, func() {
		r.handleComputePlacementCmd("approve ask-1")
		r.handleComputePlacementCmd("deny ask-1")
	})
	if !strings.Contains(out, "approved") || !strings.Contains(out, "denied") {
		t.Errorf("decision replies missing:\n%s", out)
	}
	if len(state.approved) != 1 || state.approved[0] != "ask-1" {
		t.Errorf("approved = %v; want [ask-1]", state.approved)
	}
	if len(state.denied) != 1 || state.denied[0] != "ask-1" {
		t.Errorf("denied = %v; want [ask-1]", state.denied)
	}
}

func TestComputePlacementBadRequestShapesAreGuided(t *testing.T) {
	srv, _ := placementCommFake(t)
	r := loopbackRouter(t, srv)
	out := captureSends(r, func() {
		r.handleComputePlacementCmd("reject ask-1")
		r.handleComputePlacementCmd("approve")
		r.handleComputePlacementCmd("deny")
	})
	if !strings.Contains(out, "Usage: compute placement") {
		t.Errorf("bad forms should reply with usage:\n%s", out)
	}
}

func TestComputePlacementUnknownAsk404StaysReadable(t *testing.T) {
	srv, _ := placementCommFake(t)
	r := loopbackRouter(t, srv)
	out := captureSends(r, func() {
		r.handleComputePlacementCmd("approve ask-9")
	})
	if !strings.Contains(out, "ask-9") && !strings.Contains(out, "404") {
		t.Errorf("unknown-id reply should surface the id or the HTTP status:\n%s", out)
	}
}

// The generic configure verb must forward a compute_placement key verbatim —
// the daemon applies validation, so the router forwards whatever was typed
// and reports the outcome.
func TestConfigureComputePlacementKeyIsForwarded(t *testing.T) {
	r := &Router{hostname: "h", backend: &captureBackend{name: "test", capture: func(string) {}}}
	var gotKey, gotValue string
	r.SetConfigureFunc(func(key, value string) error {
		gotKey, gotValue = key, value
		return nil
	})
	out := captureSends(r, func() {
		r.handleConfigure(Command{Type: CmdConfigure, Text: "compute_placement.mem_headroom_pct=25"})
	})
	if gotKey != "compute_placement.mem_headroom_pct" {
		t.Fatalf("key = %q; want compute_placement.mem_headroom_pct", gotKey)
	}
	if gotValue != "25" {
		t.Fatalf("value = %q; want 25", gotValue)
	}
	if !strings.Contains(out, "compute_placement.mem_headroom_pct") {
		t.Errorf("confirmation should name the key:\n%s", out)
	}
}

func TestConfigureHelpNamesComputePlacementKeys(t *testing.T) {
	r := &Router{hostname: "h", backend: &captureBackend{name: "test", capture: func(string) {}}}
	r.SetConfigureFunc(func(key, value string) error { return nil })
	out := captureSends(r, func() {
		r.handleConfigure(Command{Type: CmdConfigure, Text: "help"})
		r.handleConfigure(Command{Type: CmdConfigure, Text: "list"})
	})
	for _, want := range []string{
		"compute_placement.missing_model",
		"compute_placement.mem_headroom_pct",
		"compute_placement.disk_headroom_gb",
		"compute_placement.ask_timeout_minutes",
		"compute_placement.assume_model_size_gb",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("configure help missing %q", want)
		}
	}
}

// With the loopback unconfigured every placement form replies with the
// documented failure, not a panic.
func TestComputePlacementLoopbackUnconfigured(t *testing.T) {
	r := &Router{hostname: "h"}
	out := captureSends(r, func() {
		r.handleComputePlacementCmd("list")
		r.handleComputePlacementCmd("approve ask-1")
	})
	if !strings.Contains(out, "loopback not configured") {
		t.Errorf("expected loopback failure reply, got:\n%s", out)
	}
}
