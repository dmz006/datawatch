// Tests for the MCP tools over the fingerprint match records
// (docs/plans/2026-10-14-concurrency-and-budget-design.md §3c):
// federation_fingerprint_matches / _confirm / _reject.

package mcp

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"

	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/server/multiserver"
)

// newFingerprintTestServer builds an MCP server whose REST loopback
// (webPort) is pointed at a fake /api/federation/fingerprint-matches
// implementation. rest is called with every request the tools send.
func newFingerprintTestServer(t *testing.T, rest func(w http.ResponseWriter, r *http.Request, q url.Values)) *Server {
	t.Helper()
	ht := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rest(w, r, r.URL.Query())
	}))
	t.Cleanup(ht.Close)
	_, portStr, err := net.SplitHostPort(strings.TrimPrefix(ht.URL, "http://"))
	if err != nil {
		t.Fatalf("parse test server url: %v", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("port: %v", err)
	}
	return &Server{webPort: port}
}

// mcpPeer makes a context the way the SSE middleware would: a
// federation peer with the given capabilities in it.
func mcpPeer(caps ...string) context.Context {
	return context.WithValue(context.Background(), mcpFedPeerKey, &multiserver.Entry{
		Name:         "alpha",
		URL:          "http://alpha:8080",
		Token:        "tok",
		Federated:    true,
		Enabled:      true,
		Capabilities: caps,
	})
}

// TestMCPFingerprintList — the list tool's name and optional state
// parameter, its proxy call (including the state filter being forwarded
// as a query parameter), and the capability deny for a peer without
// federation:list.
func TestMCPFingerprintList(t *testing.T) {
	tool := (&Server{}).toolFingerprintMatches()
	if tool.Name != "federation_fingerprint_matches" {
		t.Fatalf("tool name = %q", tool.Name)
	}
	for _, r := range tool.InputSchema.Required {
		if r == "state" {
			t.Errorf("state must be optional, required list = %v", tool.InputSchema.Required)
		}
	}

	var gotQ url.Values
	s := newFingerprintTestServer(t, func(w http.ResponseWriter, _ *http.Request, q url.Values) {
		gotQ = q
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"matches":[{"id":"m1","state":"pending"}]}`))
	})

	req := mcpsdk.CallToolRequest{Params: mcpsdk.CallToolParams{Arguments: map[string]any{"state": "pending"}}}
	res, err := s.handleFingerprintMatchesMCP(context.Background(), req)
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if res == nil || res.IsError {
		t.Fatalf("expected success, got %+v", res)
	}
	if gotQ.Get("state") != "pending" {
		t.Errorf("state filter not forwarded: %v", gotQ)
	}

	// A peer without federation:list is denied with the capability named.
	res, _ = s.handleFingerprintMatchesMCP(mcpPeer("federation-peer"), req)
	if res == nil || !res.IsError {
		t.Fatalf("expected capability deny, got %+v", res)
	}
	if !strings.Contains(resToString(res), "federation:list") {
		t.Errorf("deny should name federation:list, got: %s", resToString(res))
	}
}

// TestMCPFingerprintConfirmReject — the two decision tools' names and
// required id, their POST proxies (method + path per decision), and the
// capability deny for a peer without federation:write.
func TestMCPFingerprintConfirmReject(t *testing.T) {
	confirmTool := (&Server{}).toolFingerprintConfirm()
	rejectTool := (&Server{}).toolFingerprintReject()
	if confirmTool.Name != "federation_fingerprint_confirm" {
		t.Fatalf("confirm tool name = %q", confirmTool.Name)
	}
	if rejectTool.Name != "federation_fingerprint_reject" {
		t.Fatalf("reject tool name = %q", rejectTool.Name)
	}
	for _, tool := range []mcpsdk.Tool{confirmTool, rejectTool} {
		found := false
		for _, r := range tool.InputSchema.Required {
			if r == "id" {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: id must be required, got %v", tool.Name, tool.InputSchema.Required)
		}
	}

	var gotMethod, gotPath string
	s := newFingerprintTestServer(t, func(w http.ResponseWriter, r *http.Request, _ url.Values) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"m1","state":"confirmed"}`))
	})

	req := mcpsdk.CallToolRequest{Params: mcpsdk.CallToolParams{Arguments: map[string]any{"id": "m1"}}}

	res, err := s.handleFingerprintConfirmMCP(context.Background(), req)
	if err != nil || res == nil || res.IsError {
		t.Fatalf("confirm: %+v / %v", res, err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/federation/fingerprint-matches/m1/confirm" {
		t.Errorf("confirm proxied to %s %s, want POST /api/federation/fingerprint-matches/m1/confirm",
			gotMethod, gotPath)
	}

	res, err = s.handleFingerprintRejectMCP(context.Background(), req)
	if err != nil || res == nil || res.IsError {
		t.Fatalf("reject: %+v / %v", res, err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/federation/fingerprint-matches/m1/reject" {
		t.Errorf("reject proxied to %s %s, want POST /api/federation/fingerprint-matches/m1/reject",
			gotMethod, gotPath)
	}

	// A peer without federation:write is denied on both decisions.
	plain := mcpPeer("federation-peer")
	for name, h := range map[string]func(context.Context, mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error){
		"federation_fingerprint_confirm": s.handleFingerprintConfirmMCP,
		"federation_fingerprint_reject":  s.handleFingerprintRejectMCP,
	} {
		res, _ = h(plain, req)
		if res == nil || !res.IsError {
			t.Errorf("%s: expected capability deny, got %+v", name, res)
			continue
		}
		if !strings.Contains(resToString(res), "federation:write") {
			t.Errorf("%s deny should name federation:write, got: %s", name, resToString(res))
		}
	}
}

// TestMCPFingerprintToolsHaveCapabilities — every tool these tests
// exercise must have an entry in the shared tool→capability map, with
// list = federation:list and both decisions = federation:write. This
// is the same map the capability middleware consults.
func TestMCPFingerprintToolsHaveCapabilities(t *testing.T) {
	want := map[string]string{
		"federation_fingerprint_matches": federation.CapFederationList,
		"federation_fingerprint_confirm": federation.CapFederationWrite,
		"federation_fingerprint_reject":  federation.CapFederationWrite,
	}
	for tool, cap := range want {
		got, ok := federation.MCPToolCap[tool]
		if !ok {
			t.Errorf("MCPToolCap has no entry for %q", tool)
			continue
		}
		if got != cap {
			t.Errorf("MCPToolCap[%q] = %q, want %q", tool, got, cap)
		}
	}
}

// resToString flattens a CallToolResult's text content for assertions.
func resToString(res *mcpsdk.CallToolResult) string {
	if res == nil {
		return ""
	}
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(mcpsdk.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	if b.Len() > 0 {
		return b.String()
	}
	raw, _ := json.Marshal(res)
	return string(raw)
}
