// BL463 — MCP error-mode parity with the REST surface.
//
// BL444 (boot-time SSE bind-retry gap) and HLLM-001 (.mcp.json file
// permissions) are adjacent failure classes in the same subsystem and are
// intentionally NOT exercised here — cross-reference only, not in scope for
// these tests.
//
// The parity bar being verified: for a representative set of failing calls
// (unknown session id, invalid argument, unauthorized/forbidden, upstream
// HTTP 4xx/5xx), the diagnostic carried by the MCP tool result (IsError=true
// text) must contain at LEAST the information the equivalent REST endpoint
// returns — the HTTP status code and the body message. An httptest server
// stands in for the datawatch daemon's REST API, so no live daemon is needed:
// the MCP handler proxies to exactly the loopback REST address that server
// serves, and the "REST response" baseline is captured by making the
// equivalent direct request against it.

package mcp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// errorParityUpstream starts an httptest server that stands in for the
// datawatch daemon REST API (every request, whatever the path or method,
// returns the given status + body) and returns it together with an MCP
// Server whose internal loopback (webPort) points at it.
func errorParityUpstream(t *testing.T, status int, body string) (*httptest.Server, *Server) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body)) //nolint:errcheck
	}))
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parsing upstream URL %q: %v", srv.URL, err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("parsing upstream port from %q: %v", u.Port(), err)
	}
	s := &Server{}
	s.SetWebPort(port)
	return srv, s
}

// errorParityREST makes the equivalent direct REST call (same method + path
// the MCP handler proxies) against the upstream and returns the status code
// and body — the baseline the MCP diagnostic is compared against.
func errorParityREST(t *testing.T, srv *httptest.Server, method, path string) (int, string) {
	t.Helper()
	var rdr io.Reader
	if strings.EqualFold(method, http.MethodPost) {
		rdr = strings.NewReader(`{}`)
	}
	req, err := http.NewRequest(method, srv.URL+path, rdr)
	if err != nil {
		t.Fatalf("building REST baseline request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("REST baseline request failed: %v", err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading REST baseline body: %v", err)
	}
	return resp.StatusCode, strings.TrimSpace(string(b))
}

// errorParityText concatenates the text content of a CallToolResult.
func errorParityText(t *testing.T, res *mcpsdk.CallToolResult) string {
	t.Helper()
	if res == nil {
		t.Fatal("nil tool result")
	}
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(mcpsdk.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

// assertErrorParity is the parity bar: the MCP tool result must be flagged
// IsError and its message must contain at least the REST status code (as a
// whole number, so "404" is not matched inside "4041") and the REST body
// message.
func assertErrorParity(t *testing.T, res *mcpsdk.CallToolResult, restStatus int, restBody string) {
	t.Helper()
	if !res.IsError {
		t.Fatalf("MCP result must be flagged IsError; got IsError=false, text=%q", errorParityText(t, res))
	}
	got := errorParityText(t, res)
	statusRe := regexp.MustCompile(`\b` + strconv.Itoa(restStatus) + `\b`)
	if !statusRe.MatchString(got) {
		t.Errorf("MCP diagnostic %q does not carry the REST status code %d", got, restStatus)
	}
	if restBody != "" && !strings.Contains(got, restBody) {
		t.Errorf("MCP diagnostic %q does not carry the REST body message %q", got, restBody)
	}
}

// Unknown session id: the REST endpoint replies 404 with an error body; the
// MCP tool result for the same call must carry the same status + message.
func TestErrorParity_UnknownSessionID(t *testing.T) {
	const id = "deadbeef-no-such-session"
	body := `{"error":"session not found: ` + id + `"}`
	srv, s := errorParityUpstream(t, http.StatusNotFound, body)

	restStatus, restBody := errorParityREST(t, srv, http.MethodPost, "/api/sessions/"+id+"/rollback")
	if restStatus != http.StatusNotFound {
		t.Fatalf("REST baseline = %d, want 404", restStatus)
	}

	res, err := s.handleSessionRollback(context.Background(), call(map[string]any{"id": id}))
	if err != nil {
		t.Fatalf("tool handler returned error: %v", err)
	}
	assertErrorParity(t, res, restStatus, restBody)
}

// Invalid argument: the REST endpoint replies 400 with a validation message;
// the MCP tool result must carry the same status + message.
func TestErrorParity_InvalidArgument(t *testing.T) {
	body := `{"error":"dir must be an absolute path"}`
	srv, s := errorParityUpstream(t, http.StatusBadRequest, body)

	restStatus, restBody := errorParityREST(t, srv, http.MethodPost, "/api/projects")
	if restStatus != http.StatusBadRequest {
		t.Fatalf("REST baseline = %d, want 400", restStatus)
	}

	res, err := s.handleProjectUpsert(context.Background(), call(map[string]any{
		"name": "parity",
		"dir":  "relative/path",
	}))
	if err != nil {
		t.Fatalf("tool handler returned error: %v", err)
	}
	assertErrorParity(t, res, restStatus, restBody)
}

// Unauthorized: the REST endpoint replies 401; the MCP tool result must
// carry the same status + message.
func TestErrorParity_Unauthorized(t *testing.T) {
	body := `{"error":"unauthorized: missing bearer token"}`
	srv, s := errorParityUpstream(t, http.StatusUnauthorized, body)

	restStatus, restBody := errorParityREST(t, srv, http.MethodGet, "/api/templates")
	if restStatus != http.StatusUnauthorized {
		t.Fatalf("REST baseline = %d, want 401", restStatus)
	}

	res, err := s.handleTemplateList(context.Background(), call(nil))
	if err != nil {
		t.Fatalf("tool handler returned error: %v", err)
	}
	assertErrorParity(t, res, restStatus, restBody)
}

// Forbidden: the REST endpoint replies 403; the MCP tool result must carry
// the same status + message.
func TestErrorParity_Forbidden(t *testing.T) {
	body := `{"error":"forbidden: caller lacks capability device.aliases.write"}`
	srv, s := errorParityUpstream(t, http.StatusForbidden, body)

	restStatus, restBody := errorParityREST(t, srv, http.MethodPost, "/api/device-aliases")
	if restStatus != http.StatusForbidden {
		t.Fatalf("REST baseline = %d, want 403", restStatus)
	}

	res, err := s.handleDeviceAliasUpsert(context.Background(), call(map[string]any{
		"alias":  "parity",
		"server": "parity-server",
	}))
	if err != nil {
		t.Fatalf("tool handler returned error: %v", err)
	}
	assertErrorParity(t, res, restStatus, restBody)
}

// Upstream HTTP 4xx: a 422 with a body message; the MCP tool result must
// carry the same status + message.
func TestErrorParity_Upstream4xx(t *testing.T) {
	body := `{"error":"effort must be one of quick, normal, thorough"}`
	srv, s := errorParityUpstream(t, http.StatusUnprocessableEntity, body)

	restStatus, restBody := errorParityREST(t, srv, http.MethodPost, "/api/templates")
	if restStatus != http.StatusUnprocessableEntity {
		t.Fatalf("REST baseline = %d, want 422", restStatus)
	}

	res, err := s.handleTemplateUpsert(context.Background(), call(map[string]any{
		"name":   "parity",
		"effort": "bogus",
	}))
	if err != nil {
		t.Fatalf("tool handler returned error: %v", err)
	}
	assertErrorParity(t, res, restStatus, restBody)
}

// Upstream HTTP 5xx: a 500 with a body message; the MCP tool result must
// carry the same status + message.
func TestErrorParity_Upstream5xx(t *testing.T) {
	body := `{"error":"internal error: audit store unavailable"}`
	srv, s := errorParityUpstream(t, http.StatusInternalServerError, body)

	restStatus, restBody := errorParityREST(t, srv, http.MethodGet, "/api/audit")
	if restStatus != http.StatusInternalServerError {
		t.Fatalf("REST baseline = %d, want 500", restStatus)
	}

	res, err := s.handleAuditQuery(context.Background(), call(nil))
	if err != nil {
		t.Fatalf("tool handler returned error: %v", err)
	}
	assertErrorParity(t, res, restStatus, restBody)
}

// Upstream HTTP 5xx with an empty body (e.g. a load balancer drop): the
// status code must still be present; the body-message requirement is vacuous.
func TestErrorParity_Upstream5xxEmptyBody(t *testing.T) {
	srv, s := errorParityUpstream(t, http.StatusServiceUnavailable, ``)

	restStatus, restBody := errorParityREST(t, srv, http.MethodGet, "/api/cooldown")
	if restStatus != http.StatusServiceUnavailable {
		t.Fatalf("REST baseline = %d, want 503", restStatus)
	}

	res, err := s.handleCooldownStatus(context.Background(), call(nil))
	if err != nil {
		t.Fatalf("tool handler returned error: %v", err)
	}
	assertErrorParity(t, res, restStatus, restBody)
}
