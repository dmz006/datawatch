// MCP tool surfaces for tls_cert_fingerprint: the pin is forwarded into
// the proxied REST body exactly when the caller supplied it, and absent
// (not false/empty) when the call doesn't mention it.

package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

type pinProxyReq struct {
	method string
	path   string
	body   map[string]any
}

// pinMCPBackend records every proxied request and answers 200 with a
// canned success payload.
func pinMCPBackend(t *testing.T) (*Server, *[]pinProxyReq) {
	t.Helper()
	rec := &[]pinProxyReq{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		*rec = append(*rec, pinProxyReq{method: r.Method, path: r.URL.Path, body: body})
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true,"name":"x"}`))
	}))
	t.Cleanup(srv.Close)
	return &Server{webPort: bl427Port(t, srv)}, rec
}

func pinMCPCall(args map[string]any) mcpsdk.CallToolRequest {
	r := mcpsdk.CallToolRequest{}
	r.Params.Arguments = args
	return r
}

func TestServerAddMCP_TLSFingerprintForwarded(t *testing.T) {
	s, rec := pinMCPBackend(t)

	withPin := pinMCPCall(map[string]any{
		"name":                 "pinned",
		"url":                  "http://127.0.0.1:19999",
		"tls_cert_fingerprint": "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff",
	})
	if res, err := s.handleServerAddMCP(t.Context(), withPin); err != nil || res.IsError {
		t.Fatalf("add with pin: err=%v res=%+v", err, res)
	}
	if got := (*rec)[0].body["tls_cert_fingerprint"]; got != "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff" {
		t.Errorf("forwarded tls_cert_fingerprint = %v", got)
	}

	without := pinMCPCall(map[string]any{"name": "plain", "url": "http://127.0.0.1:19999"})
	if res, err := s.handleServerAddMCP(t.Context(), without); err != nil || res.IsError {
		t.Fatalf("add without pin: err=%v res=%+v", err, res)
	}
	if _, present := (*rec)[1].body["tls_cert_fingerprint"]; present {
		t.Errorf("pin must be absent from the body when the call doesn't mention it: %v", (*rec)[1].body)
	}
}

func TestServerUpdateMCP_TLSFingerprintOnlyWhenGiven(t *testing.T) {
	s, rec := pinMCPBackend(t)

	withPin := pinMCPCall(map[string]any{"name": "p", "tls_cert_fingerprint": "aabb"})
	if res, err := s.handleServerUpdateMCP(t.Context(), withPin); err != nil || res.IsError {
		t.Fatalf("update with pin: err=%v res=%+v", err, res)
	}
	if got := (*rec)[0].path; got != "/api/servers/p" {
		t.Errorf("update path = %q, want /api/servers/p", got)
	}
	if got := (*rec)[0].body["tls_cert_fingerprint"]; got != "aabb" {
		t.Errorf("forwarded tls_cert_fingerprint = %v, want aabb", got)
	}

	// Not mentioned at all — must not be forwarded (no accidental reset).
	unrelated := pinMCPCall(map[string]any{"name": "p", "label": "ops"})
	if res, err := s.handleServerUpdateMCP(t.Context(), unrelated); err != nil || res.IsError {
		t.Fatalf("update without pin: err=%v res=%+v", err, res)
	}
	if _, present := (*rec)[1].body["tls_cert_fingerprint"]; present {
		t.Errorf("pin must not be forwarded when unmentioned: %v", (*rec)[1].body)
	}

	// Mentioned but empty — treated as not given.
	empty := pinMCPCall(map[string]any{"name": "p", "tls_cert_fingerprint": ""})
	if res, err := s.handleServerUpdateMCP(t.Context(), empty); err != nil || res.IsError {
		t.Fatalf("update with empty pin: err=%v res=%+v", err, res)
	}
	if _, present := (*rec)[2].body["tls_cert_fingerprint"]; present {
		t.Errorf("empty pin must not be forwarded: %v", (*rec)[2].body)
	}
}

func TestFederationPeerFingerprintMCP_AddAndUpdate(t *testing.T) {
	s, rec := pinMCPBackend(t)

	addPin := pinMCPCall(map[string]any{
		"name":                 "f-peer",
		"url":                  "http://127.0.0.1:19999",
		"tls_cert_fingerprint": "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff",
	})
	if res, err := s.handleFederationPeerAddMCP(t.Context(), addPin); err != nil || res.IsError {
		t.Fatalf("fed add with pin: err=%v res=%+v", err, res)
	}
	if got := (*rec)[0].path; got != "/api/federation/peers" {
		t.Errorf("fed add path = %q", got)
	}
	if got := (*rec)[0].body["tls_cert_fingerprint"]; got != "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff" {
		t.Errorf("forwarded tls_cert_fingerprint = %v", got)
	}

	addPlain := pinMCPCall(map[string]any{"name": "f-peer2", "url": "http://127.0.0.1:19999"})
	if res, err := s.handleFederationPeerAddMCP(t.Context(), addPlain); err != nil || res.IsError {
		t.Fatalf("fed add without pin: err=%v res=%+v", err, res)
	}
	if _, present := (*rec)[1].body["tls_cert_fingerprint"]; present {
		t.Errorf("pin must be absent when unmentioned: %v", (*rec)[1].body)
	}

	upPin := pinMCPCall(map[string]any{"name": "f-peer", "tls_cert_fingerprint": "ccdd"})
	if res, err := s.handleFederationPeerUpdateMCP(t.Context(), upPin); err != nil || res.IsError {
		t.Fatalf("fed update with pin: err=%v res=%+v", err, res)
	}
	if got := (*rec)[2].path; got != "/api/federation/peers/f-peer" {
		t.Errorf("fed update path = %q", got)
	}
	if got := (*rec)[2].body["tls_cert_fingerprint"]; got != "ccdd" {
		t.Errorf("forwarded tls_cert_fingerprint = %v, want ccdd", got)
	}

	upNoPin := pinMCPCall(map[string]any{"name": "f-peer", "label": "edge"})
	if res, err := s.handleFederationPeerUpdateMCP(t.Context(), upNoPin); err != nil || res.IsError {
		t.Fatalf("fed update without pin: err=%v res=%+v", err, res)
	}
	if _, present := (*rec)[3].body["tls_cert_fingerprint"]; present {
		t.Errorf("pin must not be forwarded when unmentioned: %v", (*rec)[3].body)
	}
}
