package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// healthBody requests GET /api/health via the handler and decodes the
// JSON body into a map. Reuses newTestServer from health_test.go.
func healthBody(t *testing.T, s *Server) map[string]interface{} {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleHealth(rr, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/health status=%d body=%s", rr.Code, rr.Body.String())
	}
	var body map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v body=%s", err, rr.Body.String())
	}
	return body
}

func TestHealth_MCPSSE_Retrying(t *testing.T) {
	s := newTestServer(t, nil, nil)
	s.SetMCPSSEStateFn(func() string { return "retrying" })
	body := healthBody(t, s)
	if body["mcp_sse"] != "retrying" {
		t.Errorf("mcp_sse=%v want retrying (body=%v)", body["mcp_sse"], body)
	}
}

func TestHealth_MCPSSE_Listening(t *testing.T) {
	s := newTestServer(t, nil, nil)
	s.SetMCPSSEStateFn(func() string { return "listening" })
	body := healthBody(t, s)
	if body["mcp_sse"] != "listening" {
		t.Errorf("mcp_sse=%v want listening (body=%v)", body["mcp_sse"], body)
	}
}

func TestHealth_MCPSSE_DefaultDisabled(t *testing.T) {
	// No state fn wired at all (SSE off / never configured): the field
	// must still be present and read "disabled".
	s := newTestServer(t, nil, nil)
	body := healthBody(t, s)
	if body["mcp_sse"] != "disabled" {
		t.Errorf("mcp_sse=%v want disabled (body=%v)", body["mcp_sse"], body)
	}
}
