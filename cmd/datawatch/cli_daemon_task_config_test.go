package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func daemonTaskConfigCLIConfig(t *testing.T, h http.HandlerFunc) *config.Config {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	cfg.Server.Port = port
	return cfg
}

// The generic config set sends each dotted key straight to PUT /api/config.
func TestDaemonTaskConfigSetCLI_SendsKeys(t *testing.T) {
	var sent map[string]any
	cfg := daemonTaskConfigCLIConfig(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if r.Method != http.MethodPut || r.URL.Path != "/api/config" || !json.Valid(raw) {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		sent = nil
		_ = json.Unmarshal(raw, &sent)
		_, _ = w.Write([]byte(`{}`))
	})
	cases := []struct {
		key, value string
		want       any
	}{
		{"autonomous.task_templates.mode.changelog", "shadow", "shadow"},
		{"autonomous.script_gate_timeout_seconds", "120", float64(120)},
		{"autonomous.project_gate_command", "make check", "make check"},
	}
	for _, c := range cases {
		sent = nil
		if err := runConfigSet(cfg, c.key, c.value); err != nil {
			t.Fatalf("%s=%s: %v", c.key, c.value, err)
		}
		if !reflect.DeepEqual(sent, map[string]any{c.key: c.want}) {
			t.Errorf("%s=%s: daemon received %v", c.key, c.value, sent)
		}
	}
}

func TestDaemonTaskConfigSetCLI_ShowsRefusal(t *testing.T) {
	const refusal = "autonomous.script_gate_timeout_seconds must be a whole number of seconds"
	cfg := daemonTaskConfigCLIConfig(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, refusal, http.StatusBadRequest)
	})
	err := runConfigSet(cfg, "autonomous.script_gate_timeout_seconds", "-5")
	if err == nil || !strings.Contains(err.Error(), refusal) {
		t.Errorf("expected the daemon refusal to reach the operator, got %v", err)
	}
}
