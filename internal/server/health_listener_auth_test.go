package server

// Tests for the `listeners` array on GET /api/health.
//
// handleHealth gained a `listeners` field: one entry per daemon listener
// (name, enabled, auth_required, loopback, acknowledged), computed live
// from the config the server holds so the answer is never stale. The
// endpoint is reachable without authentication, so the body must carry
// posture only — no bind addresses, and never any token or secret value.
// The pre-existing fields (auth_required, mcp_auth_required, mcp_sse) are
// pinned by the existing TestBL90_Health and TestMCP_SSEState rows; these
// tests additionally assert they stay present alongside the new field.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// callHealth runs handleHealth through a ResponseRecorder and returns the
// raw body plus the decoded object.
func callHealth(t *testing.T, s *Server) (string, map[string]interface{}) {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleHealth(rr, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("health code=%d body=%s", rr.Code, rr.Body.String())
	}
	var body map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode health: %v", rr.Body.String())
	}
	return rr.Body.String(), body
}

// healthListeners extracts the decoded listeners array.
func healthListeners(t *testing.T, body map[string]interface{}) []map[string]interface{} {
	t.Helper()
	raw, ok := body["listeners"].([]interface{})
	if !ok {
		t.Fatalf("listeners missing or not an array: %v", body["listeners"])
	}
	out := make([]map[string]interface{}, 0, len(raw))
	for _, item := range raw {
		m, ok := item.(map[string]interface{})
		if !ok {
			t.Fatalf("listeners entry not an object: %T", item)
		}
		out = append(out, m)
	}
	return out
}

// listenerByName finds one entry; fails the test if absent.
func listenerByName(t *testing.T, listeners []map[string]interface{}, name string) map[string]interface{} {
	t.Helper()
	for _, l := range listeners {
		if l["name"] == name {
			return l
		}
	}
	t.Fatalf("listener %q not found in: %v", name, listeners)
	return nil
}

// wantBool asserts a JSON boolean with the expected value.
func wantBool(t *testing.T, entry map[string]interface{}, key string, want bool) {
	t.Helper()
	if got, ok := entry[key].(bool); !ok || got != want {
		t.Errorf("%s=%v (%T) want %v", key, entry[key], entry[key], want)
	}
}

// assertLegacyFields pins the three pre-existing health fields so a
// refactor that drops or renames one of them fails here.
func assertLegacyFields(t *testing.T, body map[string]interface{}) {
	t.Helper()
	for _, key := range []string{"auth_required", "mcp_auth_required", "mcp_sse"} {
		if _, ok := body[key]; !ok {
			t.Errorf("legacy health field %q missing: %v", key, body)
		}
	}
}

func TestListenerAuth_TokenProtectedListener(t *testing.T) {
	s := newTestServer(t, nil, nil)
	s.cfg = &config.Config{}
	s.cfg.Server.Enabled = true
	s.cfg.Server.Host = "127.0.0.1"
	s.cfg.Server.Port = 8443
	s.cfg.Server.Token = "unit-test-token"

	raw, body := callHealth(t, s)
	assertLegacyFields(t, body)
	listeners := healthListeners(t, body)
	entry := listenerByName(t, listeners, "rest")
	wantBool(t, entry, "enabled", true)
	// A server token gates the REST listener.
	wantBool(t, entry, "auth_required", true)
	wantBool(t, entry, "loopback", true)
	wantBool(t, entry, "acknowledged", false)
	// The token itself must not appear anywhere in the body.
	if strings.Contains(raw, "unit-test-token") {
		t.Errorf("health body contains the server token: %s", raw)
	}
}

func TestListenerAuth_OpenNonLoopbackListener(t *testing.T) {
	s := newTestServer(t, nil, nil)
	s.cfg = &config.Config{}
	s.cfg.Server.Enabled = true
	s.cfg.Server.Host = "0.0.0.0"
	s.cfg.Server.Port = 8443

	_, body := callHealth(t, s)
	assertLegacyFields(t, body)
	entry := listenerByName(t, healthListeners(t, body), "rest")
	wantBool(t, entry, "enabled", true)
	// Enabled, bound to all interfaces, no token: exposed and unauthenticated.
	wantBool(t, entry, "auth_required", false)
	wantBool(t, entry, "loopback", false)
	wantBool(t, entry, "acknowledged", false)
}

func TestListenerAuth_AcknowledgedOpenListener(t *testing.T) {
	s := newTestServer(t, nil, nil)
	s.cfg = &config.Config{}
	s.cfg.Server.Enabled = true
	s.cfg.Server.Host = "0.0.0.0"
	s.cfg.Server.Port = 8443
	s.cfg.Server.AcknowledgeUnauthenticated = true

	_, body := callHealth(t, s)
	assertLegacyFields(t, body)
	entry := listenerByName(t, healthListeners(t, body), "rest")
	wantBool(t, entry, "enabled", true)
	wantBool(t, entry, "auth_required", false)
	wantBool(t, entry, "loopback", false)
	// The operator's explicit acknowledgement must surface per listener.
	wantBool(t, entry, "acknowledged", true)
	// The same flag applies to every listener.
	for _, l := range healthListeners(t, body) {
		wantBool(t, l, "acknowledged", true)
	}
}

func TestListenerAuth_NoAddressOrCredentialsInBody(t *testing.T) {
	const token = "do-not-leak-token-abc123"
	const host = "203.0.113.7" // TEST-NET-1, unambiguous if leaked
	s := newTestServer(t, nil, nil)
	s.cfg = &config.Config{}
	s.cfg.Server.Enabled = true
	s.cfg.Server.Host = host
	s.cfg.Server.Port = 9876
	s.cfg.Server.Token = token
	s.cfg.Webhook.Enabled = true
	s.cfg.Webhook.Token = "webhook-token-value"

	raw, body := callHealth(t, s)
	// None of the configured secret or bind values may appear verbatim.
	for _, secret := range []string{token, "webhook-token-value", host, "9876"} {
		if strings.Contains(raw, secret) {
			t.Errorf("health body contains %q", secret)
		}
	}
	// And no entry may carry an address-like key at all.
	for _, l := range healthListeners(t, body) {
		if _, ok := l["address"]; ok {
			t.Errorf("listener entry leaked an address field: %v", l)
		}
		for key := range l {
			switch key {
			case "name", "enabled", "auth_required", "loopback", "acknowledged":
			default:
				t.Errorf("listener entry has unexpected key %q: %v", key, l)
			}
		}
	}
	// The unauthenticated webhook listener (no address to leak) is still
	// reported by posture alone.
	entry := listenerByName(t, healthListeners(t, body), "webhook")
	wantBool(t, entry, "enabled", true)
	wantBool(t, entry, "auth_required", true)
}

// TestListenerAuth_NilConfigIsSafe guards the no-config path: listeners
// must be an empty array, never a null or a panic (ListenerAuthStates
// dereferences its argument, so handleHealth must not call it with nil).
func TestListenerAuth_NilConfigIsSafe(t *testing.T) {
	s := newTestServer(t, nil, nil)
	s.cfg = nil // newTestServer leaves cfg unset
	raw, body := callHealth(t, s)
	listeners := healthListeners(t, body)
	if len(listeners) != 0 {
		t.Errorf("listeners=%v want empty array", listeners)
	}
	assertLegacyFields(t, body)
	if strings.Contains(raw, "\"listeners\":null") {
		t.Errorf("listeners must be an empty array, not null: %s", raw)
	}
}
