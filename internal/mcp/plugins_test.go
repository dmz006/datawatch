package mcp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// pluginEnableBody calls plugin_enable with args and returns the raw body
// the REST loopback received.
func pluginEnableBody(t *testing.T, args map[string]any) string {
	t.Helper()
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/plugins/svc/enable" || r.Method != http.MethodPost {
			http.Error(w, "unexpected", http.StatusNotFound)
			return
		}
		b, _ := io.ReadAll(r.Body)
		got = string(b)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()
	port, _ := strconv.Atoi(strings.Split(srv.URL, ":")[2])
	s := &Server{webPort: port}

	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = args
	if _, err := s.handlePluginEnable(context.Background(), req); err != nil {
		t.Fatalf("handlePluginEnable: %v", err)
	}
	return got
}

func TestPluginEnable_NoManagedSendsNoBody(t *testing.T) {
	if got := pluginEnableBody(t, map[string]any{"name": "svc"}); got != "" {
		t.Errorf("body=%q want empty", got)
	}
}

func TestPluginEnable_ManagedSendsBody(t *testing.T) {
	got := pluginEnableBody(t, map[string]any{"name": "svc", "managed": true})
	if !strings.Contains(got, `"managed":true`) {
		t.Errorf("body=%q want managed:true", got)
	}
	got = pluginEnableBody(t, map[string]any{"name": "svc", "managed": false})
	if !strings.Contains(got, `"managed":false`) {
		t.Errorf("body=%q want managed:false", got)
	}
}

func TestPluginEnable_ToolDeclaresManagedArg(t *testing.T) {
	tool := (&Server{}).toolPluginEnable()
	if _, ok := tool.InputSchema.Properties["managed"]; !ok {
		t.Error("plugin_enable tool missing optional managed arg")
	}
}
