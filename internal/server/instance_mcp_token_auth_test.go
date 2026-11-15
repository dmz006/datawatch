// Instance-level .mcp.json token authentication — end-to-end proof that the
// scoped, TTL-bound token the instance config carries (not the long-lived
// admin token) authenticates exactly the surface the MCP bridge tool runs
// on, and no further.
//
// The token under test in every case is the one read back out of the
// written <dataDir>/.mcp.json file (channel.InstanceMCPConfig ->
// WriteInstanceMCPConfig), not a value captured from the mint call. The
// server's admin token is an identifiable sentinel and is asserted to
// appear nowhere in the written file. No real sleeping: TTL expiry is
// driven by the store's injectable clock (SetNowForTest).

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/auth"
	"github.com/dmz006/datawatch/internal/channel"
	"github.com/dmz006/datawatch/internal/federation"
)

// Sentinel values standing in for the server's long-lived admin token.
// They must differ from every real admin token in the environment (they
// are only a stand-in here) and from the scoped token the store mints, so
// "the written file carries no server token" can actually fail.
const (
	serverTokenSentinel    = "dw-admin-sentinel-instance-mcp-test"
	serverTokenSentinelAlt = "dw-admin-sentinel-instance-mcp-test-2"
)

// instanceTokenTestCaps mirrors what the daemon grants the instance config
// (the session-default grant a self-config-enabled session gets, resolved
// to individual caps): the session-safe read surface plus self-config.
// It deliberately carries sessions:list and health:read (so session-safe
// endpoints pass) and carries NO secrets, NO identity, NO config:write and
// NO daemon control — so admin-only endpoints must 403.
var instanceTokenTestCaps = federation.Resolve(federation.SessionDefaultGrantCaps(true), nil)

// newInstanceTokenTestServer builds the harness the daemon builds:
// the SAME auth.SessionTokenStore object is both the instance config's
// minter and the server's session-token auth source, behind the same
// fedAuthMiddleware the mux runs. The admin token is a sentinel.
func newInstanceTokenTestServer(t *testing.T) (*Server, *auth.SessionTokenStore) {
	t.Helper()
	s, _ := newMCPCapTestServer(t)
	s.token = serverTokenSentinel
	store, err := auth.NewSessionTokenStore("")
	if err != nil {
		t.Fatalf("session token store: %v", err)
	}
	s.sessionTokens = store
	return s, store
}

// instanceTokenInstance builds the instance config exactly as
// cmd/datawatch wires it: the real store as minter, the resolved grant as
// caps, and NO token in the base env — the only token that should end up
// in the written file is the scoped one Refresh mints.
func instanceTokenInstance(t *testing.T, dataDir string, store *auth.SessionTokenStore) *channel.InstanceMCPConfig {
	t.Helper()
	return &channel.InstanceMCPConfig{
		DataDir:       dataDir,
		ChannelJSPath: filepath.Join(t.TempDir(), "node_modules", "datawatch-channel", "channel.js"),
		BaseEnv:       map[string]string{"DATAWATCH_API_URL": "http://127.0.0.1:8443"},
		Caps:          instanceTokenTestCaps,
		Minter:        store,
	}
}

// readInstanceMCPFileToken extracts the DATAWATCH_TOKEN the instance-level
// .mcp.json actually carries. This is the value a tool (or an attacker
// reading the file) gets — the tests use exactly this string.
func readInstanceMCPFileToken(t *testing.T, dataDir string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dataDir, ".mcp.json"))
	if err != nil {
		t.Fatalf("instance .mcp.json: %v", err)
	}
	var cfg channel.MCPProjectConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("parse instance .mcp.json: %v", err)
	}
	spec, ok := cfg.MCPServers[channel.ProjectMCPEntryName]
	if !ok {
		t.Fatalf("instance .mcp.json: no %q entry", channel.ProjectMCPEntryName)
	}
	tok := spec.Env["DATAWATCH_TOKEN"]
	if tok == "" {
		t.Fatalf("instance .mcp.json %q entry: no DATAWATCH_TOKEN", channel.ProjectMCPEntryName)
	}
	return tok
}

// doInstanceTokenREST drives one REST request through the exact middleware
// the mux runs, against the named handler.
func doInstanceTokenREST(t *testing.T, s *Server, method, path, bearer string, handler http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rr := httptest.NewRecorder()
	s.fedAuthMiddleware(handler).ServeHTTP(rr, req)
	return rr
}

// TestInstanceMCPToken_FileTokenAuthenticates — the token read from the
// instance .mcp.json works on session-safe REST and MCP endpoints, and the
// same token is rejected (403) on admin-only endpoints.
func TestInstanceMCPToken_FileTokenAuthenticates(t *testing.T) {
	s, store := newInstanceTokenTestServer(t)
	dataDir := t.TempDir()
	ic := instanceTokenInstance(t, dataDir, store)

	rewritten, err := ic.Refresh()
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if !rewritten {
		t.Fatalf("first Refresh should write the instance config")
	}
	fileTok := readInstanceMCPFileToken(t, dataDir)
	if strings.HasPrefix(fileTok, "dw-admin-sentinel") {
		t.Fatalf("file token looks like the admin sentinel: %q", fileTok)
	}

	// Session-safe REST: GET /api/sessions requires sessions:list, which
	// the session-default grant carries.
	rr := doInstanceTokenREST(t, s, http.MethodGet, "/api/sessions", fileTok, s.handleSessions)
	if rr.Code != http.StatusOK {
		t.Errorf("file token on /api/sessions: want 200, got %d (%s)", rr.Code, rr.Body.String())
	}
	if !bytes.HasPrefix(rr.Body.Bytes(), []byte("[")) {
		t.Errorf("/api/sessions body should be a JSON list, got %q", rr.Body.String())
	}

	// Session-safe MCP: the tool catalog for a session-scoped token must
	// advertise the session-safe tool but not the admin-only one.
	rr = callMCP(s, http.MethodGet, "/api/mcp/tools", fileTok, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("file token on /api/mcp/tools: want 200, got %d (%s)", rr.Code, rr.Body.String())
	}
	var tools []map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &tools); err != nil {
		t.Fatalf("parse tool list: %v", err)
	}
	names := map[string]bool{}
	for _, tool := range tools {
		names[tool["name"]] = true
	}
	if !names["get_my_session_id"] || !names["get_my_scope"] {
		t.Errorf("session-safe tools should be advertised, got %v", names)
	}
	if names["secret_set"] {
		t.Errorf("admin-only tool secret_set must not be advertised to a session-scoped token, got %v", names)
	}

	// Session-safe MCP call: get_my_session_id (health:read) must succeed.
	rr = callMCP(s, http.MethodPost, "/api/mcp/call", fileTok, `{"tool":"get_my_session_id"}`)
	if rr.Code != http.StatusOK {
		t.Errorf("file token calling get_my_session_id: want 200, got %d (%s)", rr.Code, rr.Body.String())
	}

	// Admin-only surface with the SAME file token: capability must
	// refuse.
	//
	// (The session-default grant intentionally does not include
	// secrets:write or any daemon-control cap.)
	rr = callMCP(s, http.MethodPost, "/api/mcp/call", fileTok, `{"tool":"secret_set"}`)
	if rr.Code != http.StatusForbidden {
		t.Errorf("file token calling secret_set (admin-only): want 403, got %d (%s)", rr.Code, rr.Body.String())
	}

	// Control: the admin (sentinel) token IS allowed on the admin-only
	// tool — proving the 403 above is capability-driven, not a broken
	// handler.
	rr = callMCP(s, http.MethodPost, "/api/mcp/call", s.token, `{"tool":"secret_set"}`)
	if rr.Code != http.StatusOK {
		t.Errorf("admin token on secret_set (control): want 200, got %d (%s)", rr.Code, rr.Body.String())
	}
}

// TestInstanceMCPToken_TTLExpiry — after the store's clock has advanced
// past the mint TTL (no sleeping, the clock is pinned), the file token is
// rejected with 401 and can no longer resolve to a capability set.
func TestInstanceMCPToken_TTLExpiry(t *testing.T) {
	s, store := newInstanceTokenTestServer(t)

	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	store.SetNowForTest(func() time.Time { return now })

	dataDir := t.TempDir()
	ic := instanceTokenInstance(t, dataDir, store)
	if _, err := ic.Refresh(); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	fileTok := readInstanceMCPFileToken(t, dataDir)

	// Before expiry: the token works.
	rr := doInstanceTokenREST(t, s, http.MethodGet, "/api/sessions", fileTok, s.handleSessions)
	if rr.Code != http.StatusOK {
		t.Fatalf("file token before expiry: want 200, got %d (%s)", rr.Code, rr.Body.String())
	}

	// Advance the store's clock past the mint TTL (default 1h).
	now = now.Add(channel.InstanceTokenTTL + time.Second)
	store.SetNowForTest(func() time.Time { return now })

	if caps, ok := store.CapsForToken(fileTok); ok {
		t.Fatalf("expired token still resolves capabilities %v", caps)
	}
	rr = doInstanceTokenREST(t, s, http.MethodGet, "/api/sessions", fileTok, s.handleSessions)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expired file token on /api/sessions: want 401, got %d (%s)", rr.Code, rr.Body.String())
	}
	rr = callMCP(s, http.MethodPost, "/api/mcp/call", fileTok, `{"tool":"get_my_session_id"}`)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expired file token on MCP call: want 401, got %d (%s)", rr.Code, rr.Body.String())
	}
}

// TestInstanceMCPToken_Release — Release revokes the scoped token (the
// store drops it immediately — no waiting for TTL) and unlinks the
// .mcp.json file.
func TestInstanceMCPToken_Release(t *testing.T) {
	s, store := newInstanceTokenTestServer(t)
	dataDir := t.TempDir()
	ic := instanceTokenInstance(t, dataDir, store)
	if _, err := ic.Refresh(); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	fileTok := readInstanceMCPFileToken(t, dataDir)

	rr := doInstanceTokenREST(t, s, http.MethodGet, "/api/sessions", fileTok, s.handleSessions)
	if rr.Code != http.StatusOK {
		t.Fatalf("file token before Release: want 200, got %d", rr.Code)
	}

	if err := ic.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}

	if caps, ok := store.CapsForToken(fileTok); ok {
		t.Fatalf("released token still resolves capabilities %v", caps)
	}
	rr = doInstanceTokenREST(t, s, http.MethodGet, "/api/sessions", fileTok, s.handleSessions)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("released file token on /api/sessions: want 401, got %d (%s)", rr.Code, rr.Body.String())
	}

	mcpPath := filepath.Join(dataDir, ".mcp.json")
	if _, err := os.Stat(mcpPath); !os.IsNotExist(err) {
		t.Errorf("instance .mcp.json should be unlinked by Release, stat err=%v", err)
	}
}

// TestInstanceMCPToken_RefreshReuseWithinHalfTTL — a second Refresh while
// the token still has more than half its TTL left must not supersede it:
// the file keeps the same token and it still authenticates. (The
// supersede-past-half-TTL behavior is covered in internal/channel's own
// clock-pinned tests; here the clock is real time, so we stay in the
// deterministic reuse window and assert the observable contract: running
// tools don't lose their credential on a same-window Refresh.)
func TestInstanceMCPToken_RefreshReuseWithinHalfTTL(t *testing.T) {
	_, store := newInstanceTokenTestServer(t)
	dataDir := t.TempDir()
	ic := instanceTokenInstance(t, dataDir, store)

	if _, err := ic.Refresh(); err != nil {
		t.Fatalf("Refresh #1: %v", err)
	}
	tok1 := readInstanceMCPFileToken(t, dataDir)

	// Well within half of the 1h TTL (~1ms elapsed): reuse, no re-mint.
	if _, err := ic.Refresh(); err != nil {
		t.Fatalf("Refresh #2: %v", err)
	}
	tok2 := readInstanceMCPFileToken(t, dataDir)
	if tok2 != tok1 {
		t.Fatalf("Refresh within half-TTL superseded the token a running tool holds: %q -> %q", tok1, tok2)
	}

	// The token in the file is still the one the store issued for the
	// reserved instance owner — proof the reuse path did not mint over it.
	owner := store.SessionIDForToken(tok1)
	if owner != channel.InstanceTokenOwnerID {
		t.Errorf("file token owner: want %q, got %q", channel.InstanceTokenOwnerID, owner)
	}
}

// TestInstanceMCPToken_FamilyTable — every backend family from
// internal/tooling/artifacts.go + the operator report that consumes a
// datawatch token through the .mcp.json / per-session channel path gets a
// working session-safe credential under the same rules: mint a scoped
// token the way the daemon mints one for that session, and the session
// surface (REST + MCP) accepts it while the admin-only surface refuses.
// The table enumerates the report's families explicitly so a future
// artifact-map change that drops one is caught by a missing row.
func TestInstanceMCPToken_FamilyTable(t *testing.T) {
	s, store := newInstanceTokenTestServer(t)

	families := []string{
		// internal/tooling/artifacts.go families that consume the
		// datawatch MCP bridge / session token:
		"claude-code",
		"opencode",
		"opencode-acp",
		"goose",
		"gemini",
		"aider",
		"pi",
		"pi-prompt",
	}

	for _, family := range families {
		tok, err := store.Mint("dw-sess-"+family, instanceTokenTestCaps)
		if err != nil {
			t.Fatalf("%s: mint: %v", family, err)
		}

		// Session-safe REST accepts it...
		rr := doInstanceTokenREST(t, s, http.MethodGet, "/api/sessions", tok, s.handleSessions)
		if rr.Code != http.StatusOK {
			t.Errorf("%s: session-safe REST: want 200, got %d (%s)", family, rr.Code, rr.Body.String())
		}
		// ...and the session-safe MCP call accepts it...
		rr = callMCP(s, http.MethodPost, "/api/mcp/call", tok, `{"tool":"get_my_session_id"}`)
		if rr.Code != http.StatusOK {
			t.Errorf("%s: session-safe MCP call: want 200, got %d (%s)", family, rr.Code, rr.Body.String())
		}
		// ...while the admin-only surface refuses it.
		rr = callMCP(s, http.MethodPost, "/api/mcp/call", tok, `{"tool":"secret_set"}`)
		if rr.Code != http.StatusForbidden {
			t.Errorf("%s: admin-only MCP call: want 403, got %d (%s)", family, rr.Code, rr.Body.String())
		}
	}
}

// TestInstanceMCPToken_FileCarriesNoServerToken — the written instance
// .mcp.json must not contain the server's long-lived admin token (or the
// adjacent sentinel). The only DATAWATCH_TOKEN in the file is the scoped
// instance token, which the store can resolve and revoke.
func TestInstanceMCPToken_FileCarriesNoServerToken(t *testing.T) {
	s, store := newInstanceTokenTestServer(t)
	dataDir := t.TempDir()
	ic := instanceTokenInstance(t, dataDir, store)

	if _, err := ic.Refresh(); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dataDir, ".mcp.json"))
	if err != nil {
		t.Fatalf("read instance .mcp.json: %v", err)
	}

	for _, sentinel := range []string{serverTokenSentinel, serverTokenSentinelAlt} {
		if bytes.Contains(raw, []byte(sentinel)) {
			t.Errorf("instance .mcp.json contains the server token sentinel %q", sentinel)
		}
	}

	// Cross-check: the token the file carries must be the scoped
	// instance token the store minted — resolvable, owner-revocable —
	// not the server token.
	fileTok := readInstanceMCPFileToken(t, dataDir)
	if fileTok == s.token || bytes.Contains(raw, []byte(s.token)) {
		t.Errorf("instance .mcp.json carries the server token: %q", s.token)
	}
	owner := store.SessionIDForToken(fileTok)
	if owner != channel.InstanceTokenOwnerID {
		t.Errorf("file token not the scoped instance credential, owner=%q", owner)
	}
}
