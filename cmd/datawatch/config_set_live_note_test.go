package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// datawatch config set reports, from the daemon's reply, whether the saved key
// is live now or needs a restart.
func TestConfigSetCLI_ReportsLiveOrRestart(t *testing.T) {
	for _, c := range []struct {
		key, value, reply, want string
	}{
		{"autonomous.tmux_kill_guard", "false", `{"status":"ok","applied":["autonomous.tmux_kill_guard"]}`,
			"Set autonomous.tmux_kill_guard = false (applied now)"},
		{"server.port", "9090", `{"status":"ok","requires_restart":["server.port"]}`,
			"Set server.port = 9090 (saved; restart the daemon to apply: server.port)"},
		{"autonomous.capacity_enabled", "true", `{"status":"ok"}`,
			"Set autonomous.capacity_enabled = true\n"},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body)
			_, _ = w.Write([]byte(c.reply))
		}))
		prev := serverURL
		serverURL = srv.URL
		out, err := captureEscalationStdout(t, func() error {
			return runConfigSet(&config.Config{}, c.key, c.value)
		})
		serverURL = prev
		srv.Close()
		if err != nil {
			t.Fatalf("%s: %v", c.key, err)
		}
		if !strings.Contains(out, c.want) {
			t.Errorf("%s: output %q does not contain %q", c.key, out, c.want)
		}
	}
}
