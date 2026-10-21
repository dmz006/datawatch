package mcp

// BL463 — unit tests for the shared MCP tool error helpers (errors.go).
// These helpers are the standard way handlers report failures so that
// callers (agents, CLI, REST bridge) can distinguish "no result" (empty
// success) from "operation failed" (IsError + descriptive text).

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func testHTTPCode(code int) *http.Response {
	u, err := url.Parse("http://127.0.0.1:9/api/test")
	if err != nil {
		panic(err)
	}
	return &http.Response{StatusCode: code, Header: http.Header{}, Request: &http.Request{Method: http.MethodGet, URL: u}}
}

func TestToolError_FlagsIsErrorWithPrefix(t *testing.T) {
	res := toolError("filter_list", fmt.Errorf("boom"))
	if !res.IsError {
		t.Fatal("IsError must be true")
	}
	text := resultText(t, res, nil)
	if !strings.HasPrefix(text, "filter_list: ") {
		t.Errorf("message must start with the tool name, got %q", text)
	}
	if !strings.Contains(text, "boom") {
		t.Errorf("message must contain the error text, got %q", text)
	}
}

func TestToolErrorf_FormatsArguments(t *testing.T) {
	res := toolErrorf("agent_spawn", "project %q not found", "acme")
	if !res.IsError {
		t.Fatal("IsError must be true")
	}
	text := resultText(t, res, nil)
	if !strings.Contains(text, `project "acme" not found`) {
		t.Errorf("formatted message wrong: %q", text)
	}
	if strings.Contains(text, "%") {
		t.Errorf("unformatted verb left in message: %q", text)
	}
}

func TestToolHTTPError_CarriesStatusAndBody(t *testing.T) {
	res := toolHTTPError("device_list", testHTTPCode(503), []byte(`{"error":"upstream down"}`))
	if !res.IsError {
		t.Fatal("IsError must be true")
	}
	text := resultText(t, res, nil)
	if !strings.Contains(text, "HTTP 503") {
		t.Errorf("message must carry the status code, got %q", text)
	}
	if !strings.Contains(text, "upstream down") {
		t.Errorf("message must carry the upstream body, got %q", text)
	}
	if !strings.HasPrefix(text, "device_list: ") {
		t.Errorf("message must start with the tool name, got %q", text)
	}
}

func TestToolHTTPError_NoBody(t *testing.T) {
	res := toolHTTPError("get_my_session_id", testHTTPCode(404), nil)
	text := resultText(t, res, nil)
	if !strings.Contains(text, "HTTP 404") {
		t.Errorf("message must carry the status code, got %q", text)
	}
	if strings.Contains(text, "body:") {
		t.Errorf("no-body case must not render an empty body section, got %q", text)
	}
}

func TestToolHTTPError_TruncatesOversizedBody(t *testing.T) {
	big := strings.Repeat("x", 2048*3+10)
	res := toolHTTPError("memory_recall", testHTTPCode(500), []byte(big))
	text := resultText(t, res, nil)
	maxLen := len("memory_recall: ") + len("HTTP 500, body: ") + 2048
	if len(text) > maxLen {
		t.Errorf("body not truncated to %d bytes; total len %d", 2048, len(text))
	}
}

func TestBoundedErrorBody_TrimsAndCaps(t *testing.T) {
	if got := boundedErrorBody([]byte("  hi  ")); got != "hi" {
		t.Errorf("expected trimmed %q, got %q", "hi", got)
	}
	if got := boundedErrorBody([]byte(`{"error":"x"}`)); got != `{"error":"x"}` {
		t.Errorf("short body must pass through unchanged, got %q", got)
	}
	big := strings.Repeat("y", maxToolErrorBody*3)
	out := boundedErrorBody([]byte(big))
	if len(out) != maxToolErrorBody+len("…") { // 2048 bytes + 3-byte ellipsis marker
		t.Errorf("expected capped length %d, got %d", maxToolErrorBody+len("…"), len(out))
	}
	if !strings.HasSuffix(out, "…") {
		t.Error("capped body must end with the ellipsis marker")
	}
}
