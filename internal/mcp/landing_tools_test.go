// MCP landing tools — the two operator decisions (keep / drop) proxy the
// landing REST endpoints, and the PRD read already carries the landing
// record, so the MCP surface is: exactly the two proxy tools, registered,
// posting to the exact routes with the exact body the REST handler reads.

package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestLandingMCP_BothToolsRegistered(t *testing.T) {
	have := map[string]bool{}
	for _, n := range mergeTestToolNames(t) {
		have[n] = true
	}
	for _, want := range LandingToolNames() {
		if !have[want] {
			t.Errorf("tool %q is not registered", want)
		}
	}
}

func TestLandingMCP_OnlyKeepAndDrop(t *testing.T) {
	if len(LandingToolNames()) != 2 {
		t.Fatalf("landing tool names = %v, want exactly two", LandingToolNames())
	}
	want := map[string]bool{}
	for _, n := range LandingToolNames() {
		want[n] = true
	}
	for _, n := range mergeTestToolNames(t) {
		if strings.HasPrefix(n, "autonomous_landing_") && !want[n] {
			t.Errorf("unexpected landing tool %q", n)
		}
	}
}

func TestLandingMCP_KeepPostsTheKeepRoute(t *testing.T) {
	var method, path, raw string
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		b, _ := io.ReadAll(r.Body)
		raw = string(b)
		_, _ = w.Write([]byte(`{"id":"prd-1","landing":{"state":"kept","actor":"parity-op","at":"2026-11-09T00:00:00Z","reason":"keep for reference"}}`))
	})
	res, err := s.handleAutonomousLandingKeep(context.Background(),
		callReq(map[string]any{"id": "prd-1", "reason": "keep for reference", "actor": "parity-op"}))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("landing keep: err=%v res=%+v", err, res)
	}
	if method != http.MethodPost || path != "/api/autonomous/prds/prd-1/landing/keep" {
		t.Errorf("proxied %s %s, want POST /api/autonomous/prds/prd-1/landing/keep", method, path)
	}
	var body map[string]string
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		t.Fatalf("proxied body %q: %v", raw, err)
	}
	if body["reason"] != "keep for reference" || body["actor"] != "parity-op" {
		t.Errorf("proxied body = %v, want reason + actor passed through", body)
	}
	if text := automationText(t, res); !strings.Contains(text, `"state":"kept"`) {
		t.Errorf("result lost the daemon body: %s", text)
	}
}

func TestLandingMCP_DropPostsTheDropRoute(t *testing.T) {
	var method, path, raw string
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		b, _ := io.ReadAll(r.Body)
		raw = string(b)
		_, _ = w.Write([]byte(`{"id":"prd-1","landing":{"state":"dropped","actor":"operator","at":"2026-11-09T00:00:00Z","reason":"superseded"}}`))
	})
	res, err := s.handleAutonomousLandingDrop(context.Background(),
		callReq(map[string]any{"id": "prd-1", "reason": "superseded"}))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("landing drop: err=%v res=%+v", err, res)
	}
	if method != http.MethodPost || path != "/api/autonomous/prds/prd-1/landing/drop" {
		t.Errorf("proxied %s %s, want POST /api/autonomous/prds/prd-1/landing/drop", method, path)
	}
	var body map[string]string
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		t.Fatalf("proxied body %q: %v", raw, err)
	}
	if body["reason"] != "superseded" {
		t.Errorf("proxied body = %v, want the reason", body)
	}
	if actor, ok := body["actor"]; ok && actor != "" {
		t.Errorf("proxied body carries actor %q, want the REST default to decide", actor)
	}
}

func TestLandingMCP_RequiresIdAndReason(t *testing.T) {
	var calls int
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{}`))
	})
	cases := []struct {
		name string
		args map[string]any
	}{
		{"no id", map[string]any{"reason": "r"}},
		{"no reason", map[string]any{"id": "prd-1"}},
		{"blank reason", map[string]any{"id": "prd-1", "reason": "  "}},
		{"empty args", map[string]any{}},
	}
	for _, tc := range cases {
		t.Run("keep/"+tc.name, func(t *testing.T) {
			res, err := s.handleAutonomousLandingKeep(context.Background(), callReq(tc.args))
			if err != nil {
				t.Fatalf("err=%v", err)
			}
			if res == nil || !res.IsError {
				t.Errorf("keep with %s: want an error result", tc.name)
			}
		})
		t.Run("drop/"+tc.name, func(t *testing.T) {
			res, err := s.handleAutonomousLandingDrop(context.Background(), callReq(tc.args))
			if err != nil {
				t.Fatalf("err=%v", err)
			}
			if res == nil || !res.IsError {
				t.Errorf("drop with %s: want an error result", tc.name)
			}
		})
	}
	if calls != 0 {
		t.Errorf("%d HTTP calls, want none when validation fails", calls)
	}
}
