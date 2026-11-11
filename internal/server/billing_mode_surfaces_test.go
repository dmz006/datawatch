package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/mcp"
	"github.com/dmz006/datawatch/internal/server"
)

// llmSurfaces runs the LLM endpoints behind a loopback listener and points an
// MCP server at it, so REST and MCP calls hit the same registry.
func llmSurfaces(t *testing.T) (*server.Server, *mcp.Server) {
	t.Helper()
	srv := server.NewLLMServerForTest(t)
	ts := httptest.NewServer(srv.LLMHandlerForTest())
	t.Cleanup(ts.Close)
	u, _ := url.Parse(ts.URL)
	port, _ := strconv.Atoi(u.Port())
	m := mcp.New("h", nil, &config.MCPConfig{}, t.TempDir(), mcp.Options{})
	m.SetWebPort(port)
	return srv, m
}

func restLLM(t *testing.T, srv *server.Server, method, name string, body map[string]any) (int, string) {
	t.Helper()
	b, _ := json.Marshal(body)
	path := "/api/llms"
	if name != "" {
		path += "/" + name
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(b))
	req.URL.Path = path
	w := httptest.NewRecorder()
	srv.LLMHandlerForTest()(w, req)
	return w.Code, w.Body.String()
}

func mcpText(t *testing.T, m *mcp.Server, tool string, args map[string]any) (string, bool) {
	t.Helper()
	res, err := m.CallTool(context.Background(), tool, args)
	if err != nil {
		return err.Error(), true
	}
	var sb strings.Builder
	for _, c := range res.Content {
		if s, ok := c["text"].(string); ok {
			sb.WriteString(s)
		}
	}
	return sb.String(), res.IsError
}

func storedMode(t *testing.T, srv *server.Server, name string) string {
	t.Helper()
	l, err := srv.RegistryForTest().Get(name)
	if err != nil {
		t.Fatalf("get %s: %v", name, err)
	}
	return l.BillingMode
}

func TestBillingModeSurfaces(t *testing.T) {
	want := config.ValidateBillingMode("bogus").Error()
	cases := []struct {
		name  string
		mode  string
		valid bool
	}{
		{"api", "api", true},
		{"allowance", "allowance", true},
		{"local", "local", true},
		{"empty", "", true},
		{"bogus", "bogus", false},
		{"wrong-case", "API", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, m := llmSurfaces(t)
			wantErr := config.ValidateBillingMode(tc.mode)

			// REST create and MCP create.
			code, body := restLLM(t, srv, http.MethodPost, "", map[string]any{"name": "r-" + tc.name, "kind": "ollama", "billing_mode": tc.mode})
			out, isErr := mcpText(t, m, "llm_add", map[string]any{"name": "m-" + tc.name, "kind": "ollama", "billing_mode": tc.mode})
			if tc.valid {
				if code != http.StatusOK || isErr {
					t.Fatalf("create refused: rest=%d %s mcp=%v %s", code, body, isErr, out)
				}
				if storedMode(t, srv, "r-"+tc.name) != tc.mode || storedMode(t, srv, "m-"+tc.name) != tc.mode {
					t.Fatalf("stored mode mismatch")
				}
			} else {
				if code != http.StatusBadRequest || !strings.Contains(body, wantErr.Error()) {
					t.Fatalf("rest create: want 400 %q, got %d %q", wantErr, code, body)
				}
				if !isErr || !strings.Contains(out, wantErr.Error()) {
					t.Fatalf("mcp create: want error %q, got %v %q", wantErr, isErr, out)
				}
				if _, err := srv.RegistryForTest().Get("r-" + tc.name); err == nil {
					t.Fatalf("invalid entry was stored")
				}
				return
			}

			// REST and MCP update, then read back through both.
			other := "allowance"
			if tc.mode == "allowance" {
				other = "local"
			}
			if code, body := restLLM(t, srv, http.MethodPut, "r-"+tc.name, map[string]any{"name": "r-" + tc.name, "kind": "ollama", "billing_mode": other}); code != http.StatusOK {
				t.Fatalf("rest update: %d %s", code, body)
			}
			if out, isErr := mcpText(t, m, "llm_update", map[string]any{"name": "m-" + tc.name, "kind": "ollama", "billing_mode": other}); isErr {
				t.Fatalf("mcp update: %s", out)
			}
			for _, n := range []string{"r-" + tc.name, "m-" + tc.name} {
				if storedMode(t, srv, n) != other {
					t.Fatalf("%s: update not stored", n)
				}
			}
			if out, _ := mcpText(t, m, "llm_get", map[string]any{"name": "r-" + tc.name}); !strings.Contains(out, `"billing_mode":"`+other+`"`) {
				t.Fatalf("llm_get lacks billing_mode: %s", out)
			}
			if out, _ := mcpText(t, m, "llm_list", nil); !strings.Contains(out, `"billing_mode":"`+other+`"`) {
				t.Fatalf("llm_list lacks billing_mode: %s", out)
			}

			// Invalid update is refused identically and leaves the value alone.
			code, body = restLLM(t, srv, http.MethodPut, "r-"+tc.name, map[string]any{"name": "r-" + tc.name, "kind": "ollama", "billing_mode": "bogus"})
			out, isErr = mcpText(t, m, "llm_update", map[string]any{"name": "m-" + tc.name, "kind": "ollama", "billing_mode": "bogus"})
			if code != http.StatusBadRequest || !strings.Contains(body, want) || !isErr || !strings.Contains(out, want) {
				t.Fatalf("invalid update: rest=%d %q mcp=%v %q", code, body, isErr, out)
			}
			if storedMode(t, srv, "r-"+tc.name) != other {
				t.Fatalf("invalid update changed the stored value")
			}
		})
	}
}

func TestBillingModeConfigSurface(t *testing.T) {
	srv, _ := llmSurfaces(t)
	if code, body := restLLM(t, srv, http.MethodPost, "", map[string]any{"name": "c1", "kind": "ollama", "billing_mode": "local"}); code != http.StatusOK {
		t.Fatalf("create: %d %s", code, body)
	}
	rows := srv.LLMBillingModesForTest()
	if len(rows) != 1 || rows[0]["name"] != "c1" || rows[0]["billing_mode"] != "local" {
		t.Fatalf("config listing: %v", rows)
	}
	bad := map[string]interface{}{"llms.c1.billing_mode": "bogus"}
	code, err := srv.FirstLLMBillingModeErrorForTest(bad)
	if err == nil || code != http.StatusBadRequest || err.Error() != config.ValidateBillingMode("bogus").Error() {
		t.Fatalf("bad value: %d %v", code, err)
	}
	if code, err := srv.FirstLLMBillingModeErrorForTest(map[string]interface{}{"llms.nope.billing_mode": "api"}); err == nil || code != http.StatusNotFound {
		t.Fatalf("unknown entry: %d %v", code, err)
	}
	ok := map[string]interface{}{"llms.c1.billing_mode": "allowance", "other.key": 1}
	if _, err := srv.FirstLLMBillingModeErrorForTest(ok); err != nil {
		t.Fatal(err)
	}
	if err := srv.ApplyLLMBillingModePatchForTest(ok); err != nil {
		t.Fatal(err)
	}
	if _, left := ok["llms.c1.billing_mode"]; left || storedMode(t, srv, "c1") != "allowance" {
		t.Fatalf("patch not applied: %v", ok)
	}
}
