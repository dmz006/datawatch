// fingerprint_sync (per federation peer) and billing_group (per LLM) are
// first-class MCP schema parameters. Both must be forwarded to the daemon's
// REST body when passed, and both must be OMITTED when not passed: on add
// that leaves the daemon defaults in place, and on update it leaves the
// stored value untouched (REST "absent = unchanged").

package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

type fingerprintProbe struct {
	method string
	path   string
	body   map[string]any
}

// fingerprintTestServer stands in for the daemon: records the last request
// and answers with an empty JSON object.
func fingerprintTestServer(t *testing.T) (*Server, func() *fingerprintProbe) {
	t.Helper()
	rec := &fingerprintProbe{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		rec.method, rec.path = r.Method, r.URL.Path
		rec.body = nil
		_ = json.Unmarshal(raw, &rec.body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	port, _ := strconv.Atoi(strings.Split(srv.URL, ":")[2])
	return &Server{webPort: port}, func() *fingerprintProbe {
		p := *rec
		return &p
	}
}

func TestMCPFederationPeerFingerprintSyncArg(t *testing.T) {
	// Schema: the parameter must be present on both tools.
	addProps := (&Server{}).toolFederationPeerAdd().InputSchema.Properties
	if _, ok := addProps["fingerprint_sync"]; !ok {
		t.Fatalf("federation_peer_add schema missing fingerprint_sync: %v", addProps)
	}
	updProps := (&Server{}).toolFederationPeerUpdate().InputSchema.Properties
	if _, ok := updProps["fingerprint_sync"]; !ok {
		t.Fatalf("federation_peer_update schema missing fingerprint_sync: %v", updProps)
	}

	srv, seen := fingerprintTestServer(t)
	ctx := context.Background()

	// add: explicit true is forwarded to the daemon.
	if _, err := srv.handleFederationPeerAddMCP(ctx, callReq(map[string]any{
		"name": "peer-a", "url": "http://peer-a:8080", "fingerprint_sync": true,
	})); err != nil {
		t.Fatal(err)
	}
	got := seen()
	if got.method != http.MethodPost || got.path != "/api/federation/peers" {
		t.Errorf("add routed %s %s, want POST /api/federation/peers", got.method, got.path)
	}
	if got.body["fingerprint_sync"] != true {
		t.Errorf("add: fingerprint_sync = %v, want true", got.body["fingerprint_sync"])
	}

	// add: omitted — the key must not be sent (daemon default is false).
	if _, err := srv.handleFederationPeerAddMCP(ctx, callReq(map[string]any{
		"name": "peer-b", "url": "http://peer-b:8081",
	})); err != nil {
		t.Fatal(err)
	}
	if _, ok := seen().body["fingerprint_sync"]; ok {
		t.Errorf("add: fingerprint_sync should be omitted when not passed: %v", seen().body)
	}

	// update: false is a VALID value and must be forwarded explicitly —
	// that is how an operator turns the exchange back off.
	if _, err := srv.handleFederationPeerUpdateMCP(ctx, callReq(map[string]any{
		"name": "peer-a", "url": "http://peer-a:8080", "fingerprint_sync": false,
	})); err != nil {
		t.Fatal(err)
	}
	got = seen()
	if got.method != http.MethodPut || got.path != "/api/federation/peers/peer-a" {
		t.Errorf("update routed %s %s, want PUT /api/federation/peers/peer-a", got.method, got.path)
	}
	if v, ok := got.body["fingerprint_sync"]; !ok || v != false {
		t.Errorf("update: fingerprint_sync = %v (present=%v), want explicit false", v, ok)
	}

	// update: omitted — the key must not be sent, so an existing true
	// value on the daemon survives.
	if _, err := srv.handleFederationPeerUpdateMCP(ctx, callReq(map[string]any{
		"name": "peer-a", "url": "http://peer-a:8080",
	})); err != nil {
		t.Fatal(err)
	}
	if _, ok := seen().body["fingerprint_sync"]; ok {
		t.Errorf("update: fingerprint_sync should be omitted when not passed: %v", seen().body)
	}
}

func TestMCPLLMBillingGroupArg(t *testing.T) {
	addProps := (&Server{}).toolLLMAdd().InputSchema.Properties
	if _, ok := addProps["billing_group"]; !ok {
		t.Fatalf("llm_add schema missing billing_group: %v", addProps)
	}
	updProps := (&Server{}).toolLLMUpdate().InputSchema.Properties
	if _, ok := updProps["billing_group"]; !ok {
		t.Fatalf("llm_update schema missing billing_group: %v", updProps)
	}

	srv, seen := fingerprintTestServer(t)
	ctx := context.Background()

	// add: forwarded to the daemon.
	if _, err := srv.handleLLMAddMCP(ctx, callReq(map[string]any{
		"name": "shared", "kind": "claude-code", "billing_group": "shared-plan-a",
	})); err != nil {
		t.Fatal(err)
	}
	got := seen()
	if got.method != http.MethodPost || got.path != "/api/llms" {
		t.Errorf("add routed %s %s, want POST /api/llms", got.method, got.path)
	}
	if got.body["billing_group"] != "shared-plan-a" {
		t.Errorf("add: billing_group = %v, want shared-plan-a", got.body["billing_group"])
	}

	// update: forwarded to the daemon (label change).
	if _, err := srv.handleLLMUpdateMCP(ctx, callReq(map[string]any{
		"name": "shared", "kind": "claude-code", "billing_group": "shared-plan-b",
	})); err != nil {
		t.Fatal(err)
	}
	got = seen()
	if got.method != http.MethodPut || got.path != "/api/llms/shared" {
		t.Errorf("update routed %s %s, want PUT /api/llms/shared", got.method, got.path)
	}
	if got.body["billing_group"] != "shared-plan-b" {
		t.Errorf("update: billing_group = %v, want shared-plan-b", got.body["billing_group"])
	}

	// update: omitted — the key must not be sent, so the stored label
	// survives a partial update.
	if _, err := srv.handleLLMUpdateMCP(ctx, callReq(map[string]any{
		"name": "shared", "kind": "claude-code",
	})); err != nil {
		t.Fatal(err)
	}
	if _, ok := seen().body["billing_group"]; ok {
		t.Errorf("update: billing_group should be omitted when not passed: %v", seen().body)
	}
}
