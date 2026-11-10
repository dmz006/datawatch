// burn guard config set/get tools: ask_on_change lives in the set schema with a
// description, the two round-trip through the daemon (set → PUT body, get →
// result text) and a server 400 comes back as an error result.

package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

type bgRec struct {
	method, path string
	body         map[string]any
}

// bgServer answers every request with reply (status code code) and records it.
func bgServer(t *testing.T, code int, reply string) (*Server, *bgRec) {
	t.Helper()
	rec := &bgRec{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(raw))
		rec.method, rec.path, rec.body = r.Method, r.URL.Path, nil
		_ = json.Unmarshal(raw, &rec.body)
		w.WriteHeader(code)
		_, _ = w.Write([]byte(reply))
	}))
	t.Cleanup(srv.Close)
	port, _ := strconv.Atoi(strings.Split(srv.URL, ":")[2])
	return &Server{webPort: port}, rec
}

func TestBurnGuardAskOnChangeMCPSetSchemaAndBody(t *testing.T) {
	def := (&Server{}).toolBurnGuardConfigSet()
	propAny, ok := def.InputSchema.Properties["ask_on_change"]
	if !ok {
		t.Fatalf("burn_guard_config_set schema lacks ask_on_change: %v", def.InputSchema.Properties)
	}
	prop, ok := propAny.(map[string]any)
	if !ok {
		t.Fatalf("ask_on_change property = %T (%v), want map", propAny, propAny)
	}
	if prop["type"] != "boolean" {
		t.Errorf("ask_on_change type = %v, want boolean", prop["type"])
	}
	desc, _ := prop["description"].(string)
	if !strings.Contains(strings.ToLower(desc), "alert") || !strings.Contains(strings.ToLower(desc), "question") {
		t.Errorf("description does not explain the setting: %q", desc)
	}
	if got := (&Server{}).toolBurnGuardConfigGet(); !strings.Contains(got.Description, "ask_on_change") {
		t.Errorf("burn_guard_config_get description lacks ask_on_change: %q", got.Description)
	}

	s, rec := bgServer(t, http.StatusOK, `{"ok":true,"config":{"ask_on_change":true}}`)
	res, err := s.handleBurnGuardConfigSet(context.Background(), callReq(map[string]any{"ask_on_change": true}))
	if err != nil || res == nil {
		t.Fatalf("err=%v res=%v", err, res)
	}
	if rec.method != http.MethodPut || rec.path != "/api/usage/burn-guard/config" {
		t.Fatalf("proxied %s %s", rec.method, rec.path)
	}
	if rec.body["ask_on_change"] != true {
		t.Fatalf("PUT body = %v, want ask_on_change=true", rec.body)
	}

	// A server rejection surfaces as an error result.
	s400, _ := bgServer(t, http.StatusBadRequest, `usage.burn_guard.ask_on_change: must be true or false`)
	res, _ = s400.handleBurnGuardConfigSet(context.Background(), callReq(map[string]any{"ask_on_change": "maybe"}))
	if text := textContent(t, res); !strings.Contains(text, "HTTP 400") || !strings.Contains(text, "must be true or false") {
		t.Errorf("rejection not surfaced: %s", text)
	}
}

func TestBurnGuardAskOnChangeMCPGetRoundTrip(t *testing.T) {
	s, rec := bgServer(t, http.StatusOK, `{"enabled":true,"ask_on_change":true}`)
	res, err := s.handleBurnGuardConfigGet(context.Background(), callReq(nil))
	if err != nil || res == nil {
		t.Fatalf("err=%v res=%v", err, res)
	}
	if rec.method != http.MethodGet || rec.path != "/api/usage/burn-guard/config" {
		t.Fatalf("proxied %s %s", rec.method, rec.path)
	}
	if text := textContent(t, res); !strings.Contains(text, `"ask_on_change":true`) && !strings.Contains(text, `"ask_on_change": true`) {
		t.Errorf("result lacks ask_on_change: %s", text)
	}
}

func textContent(t *testing.T, res *mcpsdk.CallToolResult) string {
	t.Helper()
	if res == nil || len(res.Content) == 0 {
		return ""
	}
	if tc, ok := res.Content[0].(mcpsdk.TextContent); ok {
		return tc.Text
	}
	return ""
}

// TestBurnGuardProjectionGapMCPRefusesInvalidAndAppliesValid proves the MCP tool
// forwards max_projection_gap_points to the daemon: a value the daemon refuses comes
// back as an error result and nothing is applied, while 0 and 25 are sent as numbers
// and the daemon's stored value is returned as a success result.
func TestBurnGuardProjectionGapMCPRefusesInvalidAndAppliesValid(t *testing.T) {
	prop, ok := (&Server{}).toolBurnGuardConfigSet().InputSchema.Properties["max_projection_gap_points"].(map[string]any)
	if !ok || prop["type"] != "number" {
		t.Fatalf("max_projection_gap_points schema = %v, want a number property", prop)
	}

	for _, bad := range []float64{-1, 100.5, 101} {
		s, rec := bgServer(t, http.StatusBadRequest, "usage.burn_guard.max_projection_gap_points: must be from 0 to 100 (0 never compares the projections)")
		res, err := s.handleBurnGuardConfigSet(context.Background(), callReq(map[string]any{"max_projection_gap_points": bad}))
		if err != nil || res == nil {
			t.Fatalf("%v: err=%v res=%v", bad, err, res)
		}
		if rec.body["max_projection_gap_points"] != bad {
			t.Fatalf("%v: forwarded body = %v", bad, rec.body)
		}
		if !res.IsError {
			t.Errorf("%v: refusal not an error result: %s", bad, textContent(t, res))
		}
		if text := textContent(t, res); !strings.Contains(text, "HTTP 400") || !strings.Contains(text, "max_projection_gap_points") {
			t.Errorf("%v: refusal not surfaced: %s", bad, text)
		}
	}

	for _, good := range []float64{0, 25} {
		s, rec := bgServer(t, http.StatusOK, `{"ok":true,"config":{"max_projection_gap_points":`+strconv.FormatFloat(good, 'f', -1, 64)+`}}`)
		res, err := s.handleBurnGuardConfigSet(context.Background(), callReq(map[string]any{"max_projection_gap_points": good}))
		if err != nil || res == nil {
			t.Fatalf("%v: err=%v res=%v", good, err, res)
		}
		if rec.method != http.MethodPut || rec.path != "/api/usage/burn-guard/config" {
			t.Fatalf("%v: proxied %s %s", good, rec.method, rec.path)
		}
		if rec.body["max_projection_gap_points"] != good {
			t.Fatalf("%v: PUT body = %v", good, rec.body)
		}
		if res.IsError {
			t.Errorf("%v: valid value returned an error: %s", good, textContent(t, res))
		}
	}
}
