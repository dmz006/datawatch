// MCP surface tests for the operator hold tools: each proxies one request to
// the hold REST route with the right method, path and body, carries the right
// federation capability, and turns a daemon refusal into a tool error.

package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/session"
)

// holdSeen is what the fake daemon saw for one proxied call.
type holdSeen struct {
	method, path, rawQuery string
	body                   map[string]any
	rawBody                string
}

func holdFakeDaemon(t *testing.T, seen *holdSeen, status int, reply string) *Server {
	t.Helper()
	return capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		*seen = holdSeen{method: r.Method, path: r.URL.Path, rawQuery: r.URL.RawQuery, rawBody: string(b)}
		if len(b) > 0 {
			if err := json.Unmarshal(b, &seen.body); err != nil {
				t.Errorf("request body is not JSON: %q", b)
			}
		}
		if status != http.StatusOK {
			http.Error(w, reply, status)
			return
		}
		_, _ = w.Write([]byte(reply))
	})
}

func TestHoldToolsRegisteredAndProxied(t *testing.T) {
	ctx := context.Background()

	t.Run("hold", func(t *testing.T) {
		var seen holdSeen
		s := holdFakeDaemon(t, &seen, http.StatusOK, `{"id":"aaaa0001","status":"held"}`)
		res, err := s.handleAutonomousPRDHold(ctx, callReq(map[string]any{
			"id": "aaaa0001", "reason": "wait for the release", "condition": "until after v10", "until": "72h", "actor": "alex",
		}))
		if err != nil || res == nil || res.IsError {
			t.Fatalf("err=%v res=%+v", err, res)
		}
		if seen.method != http.MethodPost || seen.path != "/api/autonomous/prds/aaaa0001/hold" {
			t.Fatalf("proxied %s %s", seen.method, seen.path)
		}
		want := map[string]any{"reason": "wait for the release", "condition": "until after v10", "until": "72h", "actor": "alex"}
		if !reflect.DeepEqual(seen.body, want) {
			t.Errorf("body = %v, want %v", seen.body, want)
		}
		if out := resContentText(t, res); !strings.Contains(out, `"held"`) {
			t.Errorf("result = %q; want the daemon's reply", out)
		}
		tool := s.toolAutonomousPRDHold()
		if tool.Name != "autonomous_prd_hold" {
			t.Errorf("name = %q", tool.Name)
		}
		for _, p := range []string{"id", "reason", "condition", "until", "actor"} {
			if _, ok := tool.InputSchema.Properties[p]; !ok {
				t.Errorf("missing parameter %q", p)
			}
		}
		if !reflect.DeepEqual(tool.InputSchema.Required, []string{"id", "reason"}) {
			t.Errorf("required = %v, want id and reason", tool.InputSchema.Required)
		}
		if got := federation.MCPToolCap["autonomous_prd_hold"]; got != federation.CapAutonomousWrite {
			t.Errorf("capability = %q, want %q", got, federation.CapAutonomousWrite)
		}
	})

	t.Run("hold defaults the actor and escapes the id", func(t *testing.T) {
		var seen holdSeen
		s := holdFakeDaemon(t, &seen, http.StatusOK, `{}`)
		if _, err := s.handleAutonomousPRDHold(ctx, callReq(map[string]any{"id": " a/b ", "reason": "r"})); err != nil {
			t.Fatal(err)
		}
		if seen.path != "/api/autonomous/prds/a/b/hold" {
			t.Errorf("path = %q", seen.path)
		}
		if seen.body["actor"] != "operator" {
			t.Errorf("actor = %v, want operator", seen.body["actor"])
		}
	})

	t.Run("release", func(t *testing.T) {
		var seen holdSeen
		s := holdFakeDaemon(t, &seen, http.StatusOK, `{"id":"aaaa0001","status":"blocked"}`)
		res, err := s.handleAutonomousPRDReleaseHold(ctx, callReq(map[string]any{"id": "aaaa0001", "note": "release shipped"}))
		if err != nil || res == nil || res.IsError {
			t.Fatalf("err=%v res=%+v", err, res)
		}
		if seen.method != http.MethodPost || seen.path != "/api/autonomous/prds/aaaa0001/release_hold" {
			t.Fatalf("proxied %s %s", seen.method, seen.path)
		}
		want := map[string]any{"note": "release shipped", "actor": "operator"}
		if !reflect.DeepEqual(seen.body, want) {
			t.Errorf("body = %v, want %v", seen.body, want)
		}
		tool := s.toolAutonomousPRDReleaseHold()
		if tool.Name != "autonomous_prd_release_hold" {
			t.Errorf("name = %q", tool.Name)
		}
		if !reflect.DeepEqual(tool.InputSchema.Required, []string{"id"}) {
			t.Errorf("required = %v, want id only", tool.InputSchema.Required)
		}
		if got := federation.MCPToolCap["autonomous_prd_release_hold"]; got != federation.CapAutonomousWrite {
			t.Errorf("capability = %q, want %q", got, federation.CapAutonomousWrite)
		}
	})

	t.Run("held group read", func(t *testing.T) {
		var seen holdSeen
		s := holdFakeDaemon(t, &seen, http.StatusOK, `{"count":1,"items":[{"id":"aaaa0001"}]}`)
		res, err := s.handleAutonomousHeld(ctx, callReq(map[string]any{}))
		if err != nil || res == nil || res.IsError {
			t.Fatalf("err=%v res=%+v", err, res)
		}
		if seen.method != http.MethodGet || seen.path != "/api/autonomous/held" || seen.rawBody != "" {
			t.Fatalf("proxied %s %s body=%q", seen.method, seen.path, seen.rawBody)
		}
		if out := resContentText(t, res); !strings.Contains(out, "aaaa0001") {
			t.Errorf("result = %q", out)
		}
		tool := s.toolAutonomousHeld()
		if tool.Name != "autonomous_held" || len(tool.InputSchema.Required) != 0 {
			t.Errorf("name=%q required=%v", tool.Name, tool.InputSchema.Required)
		}
		if got := federation.MCPToolCap["autonomous_held"]; got != federation.CapAutonomousRead {
			t.Errorf("capability = %q, want %q", got, federation.CapAutonomousRead)
		}
	})

	t.Run("migrate real run", func(t *testing.T) {
		var seen holdSeen
		s := holdFakeDaemon(t, &seen, http.StatusOK, `{"dry_run":false,"held":2}`)
		res, err := s.handleAutonomousHeldMigrate(ctx, callReq(map[string]any{
			"dry_run": false, "ids": []any{"aaaa0001", " bbbb0002 ", ""}, "reason": "parked", "condition": "after release", "until": "2026-12-01",
		}))
		if err != nil || res == nil || res.IsError {
			t.Fatalf("err=%v res=%+v", err, res)
		}
		if seen.method != http.MethodPost || seen.path != "/api/autonomous/held/migrate" {
			t.Fatalf("proxied %s %s", seen.method, seen.path)
		}
		want := map[string]any{
			"dry_run": false, "ids": []any{"aaaa0001", "bbbb0002"}, "reason": "parked",
			"condition": "after release", "until": "2026-12-01", "actor": "operator",
		}
		if !reflect.DeepEqual(seen.body, want) {
			t.Errorf("body = %v, want %v", seen.body, want)
		}
		tool := s.toolAutonomousHeldMigrate()
		if tool.Name != "autonomous_held_migrate" {
			t.Errorf("name = %q", tool.Name)
		}
		for _, p := range []string{"dry_run", "ids", "reason", "condition", "until", "actor"} {
			if _, ok := tool.InputSchema.Properties[p]; !ok {
				t.Errorf("missing parameter %q", p)
			}
		}
		if len(tool.InputSchema.Required) != 0 {
			t.Errorf("no parameter may be required, got %v", tool.InputSchema.Required)
		}
		if got := federation.MCPToolCap["autonomous_held_migrate"]; got != federation.CapAutonomousWrite {
			t.Errorf("capability = %q, want %q", got, federation.CapAutonomousWrite)
		}
	})

	t.Run("migrate dry run sends only what was given", func(t *testing.T) {
		var seen holdSeen
		s := holdFakeDaemon(t, &seen, http.StatusOK, `{"dry_run":true,"candidates":[]}`)
		if _, err := s.handleAutonomousHeldMigrate(ctx, callReq(map[string]any{"dry_run": true})); err != nil {
			t.Fatal(err)
		}
		want := map[string]any{"dry_run": true, "actor": "operator"}
		if !reflect.DeepEqual(seen.body, want) {
			t.Errorf("body = %v, want %v", seen.body, want)
		}
		// No arguments at all: the daemon decides the default, so dry_run is not sent.
		if _, err := s.handleAutonomousHeldMigrate(ctx, callReq(nil)); err != nil {
			t.Fatal(err)
		}
		if _, sent := seen.body["dry_run"]; sent {
			t.Errorf("dry_run must be left out when not given: %v", seen.body)
		}
	})

	t.Run("a daemon refusal is a tool error", func(t *testing.T) {
		var seen holdSeen
		s := holdFakeDaemon(t, &seen, http.StatusBadRequest, "a hold needs a reason; say why the Automaton is waiting")
		res, err := s.handleAutonomousPRDHold(ctx, callReq(map[string]any{"id": "aaaa0001", "reason": ""}))
		if err != nil || res == nil || !res.IsError {
			t.Fatalf("a 400 must come back as a tool error: err=%v res=%+v", err, res)
		}
		if out := resContentText(t, res); !strings.Contains(out, "needs a reason") {
			t.Errorf("error text = %q", out)
		}
		s = holdFakeDaemon(t, &seen, http.StatusConflict, "held migration: a final gate is running")
		res, err = s.handleAutonomousHeldMigrate(ctx, callReq(map[string]any{"ids": []any{"x"}, "reason": "r", "dry_run": false}))
		if err != nil || res == nil || !res.IsError {
			t.Fatalf("a 409 must come back as a tool error: err=%v res=%+v", err, res)
		}
	})

	t.Run("registered in the server listing", func(t *testing.T) {
		dir := t.TempDir()
		mgr, err := session.NewManager("testhost", dir, "/bin/echo", 0)
		if err != nil {
			t.Fatalf("session.NewManager: %v", err)
		}
		allowTestTemp(mgr)
		trackSessionManager(t, mgr)
		mgr.WithFakeTmux()
		srv := New("testhost", mgr, &config.MCPConfig{Enabled: true}, dir, Options{Version: "test"})
		tools, err := srv.ListTools()
		if err != nil {
			t.Fatalf("ListTools: %v", err)
		}
		listed := map[string]bool{}
		for _, tool := range tools {
			listed[tool.Name] = true
		}
		for _, n := range []string{"autonomous_prd_hold", "autonomous_prd_release_hold", "autonomous_held", "autonomous_held_migrate"} {
			if !listed[n] {
				t.Errorf("%s is not registered on the server", n)
			}
			if _, ok := federation.MCPToolCap[n]; !ok {
				t.Errorf("%s has no federation capability entry", n)
			}
		}
	})
}
