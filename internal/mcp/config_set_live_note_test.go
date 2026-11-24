package mcp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// config_set reports, from the daemon's reply, whether the saved key is live
// now or needs a restart.
func TestConfigSet_ReportsLiveOrRestart(t *testing.T) {
	for _, c := range []struct{ key, value, reply, want string }{
		{"autonomous.tmux_kill_guard", "false", `{"status":"ok","applied":["autonomous.tmux_kill_guard"]}`,
			"Set autonomous.tmux_kill_guard = false (applied now)"},
		{"server.port", "9090", `{"status":"ok","requires_restart":["server.port"]}`,
			"Set server.port = 9090 (saved; restart the daemon to apply: server.port)"},
		{"autonomous.capacity_enabled", "true", `{"status":"ok"}`,
			"Set autonomous.capacity_enabled = true"},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body)
			_, _ = w.Write([]byte(c.reply))
		}))
		port, _ := strconv.Atoi(strings.Split(srv.URL, ":")[2])
		s := &Server{webPort: port, cfg: &config.MCPConfig{AllowSelfConfig: config.BoolPtr(true)}}
		res, err := s.handleConfigSet(context.Background(), callReq(map[string]any{"key": c.key, "value": c.value}))
		srv.Close()
		if err != nil {
			t.Fatal(err)
		}
		if got := automationText(t, res); got != c.want {
			t.Errorf("%s: config_set text = %q, want %q", c.key, got, c.want)
		}
	}
}
