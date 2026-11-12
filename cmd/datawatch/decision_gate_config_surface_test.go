// Config-parity test for the decision gate keys on the CLI. The CLI keeps no
// key list of its own: datawatch config set sends PUT /api/config and config
// get reads GET /api/config for any dotted key, so autonomous.questions.
// decision_gate (bool) and autonomous.questions.decision_gate_mode (shadow or
// on) need no CLI change. These tests pin that generic path: a boolean goes
// out as a real boolean, a mode goes out as a string (the raw attempt is not
// valid JSON, so the quoted retry carries it), a refused mode reaches the
// operator with the daemon's text and stores nothing, and config get prints
// what the daemon holds.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// decisionGateCLIDaemon is a PUT/GET /api/config stand-in that, like the real
// daemon, answers 400 to invalid JSON and to a decision gate value of the
// wrong type or an unknown mode, and stores only what it accepted.
type decisionGateCLIDaemon struct {
	config     map[string]any
	configBody string
}

func startDecisionGateCLIDaemon(t *testing.T) *decisionGateCLIDaemon {
	t.Helper()
	d := &decisionGateCLIDaemon{config: map[string]any{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/config" {
			http.NotFound(w, r)
			return
		}
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(d.configBody))
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		if v, ok := body["autonomous.questions.decision_gate"]; ok {
			if _, isBool := v.(bool); !isBool {
				http.Error(w, "autonomous.questions.decision_gate: must be a boolean (true or false)", http.StatusBadRequest)
				return
			}
		}
		if v, ok := body["autonomous.questions.decision_gate_mode"]; ok {
			if err := func() error {
				mode, isStr := v.(string)
				if !isStr {
					return fmt.Errorf("must be a string")
				}
				return config.ValidateDecisionGateMode(mode)
			}(); err != nil {
				http.Error(w, "autonomous.questions.decision_gate_mode: "+err.Error(), http.StatusBadRequest)
				return
			}
		}
		for k, v := range body {
			d.config[k] = v
		}
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(srv.Close)
	prev := serverURL
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = prev })
	return d
}

// TestDecisionGateConfigSurface_CLISetGate: config set sends decision_gate as
// a boolean, and an explicit false overwrites an explicit true.
func TestDecisionGateConfigSurface_CLISetGate(t *testing.T) {
	d := startDecisionGateCLIDaemon(t)
	cfg := &config.Config{}
	const key = "autonomous.questions.decision_gate"
	for _, val := range []bool{true, false} {
		if _, err := captureEscalationStdout(t, func() error {
			return runConfigSet(cfg, key, strconv.FormatBool(val))
		}); err != nil {
			t.Fatalf("config set %s = %v: %v", key, val, err)
		}
		if got := d.config[key]; got != val {
			t.Fatalf("stored %s = %#v, want %v", key, got, val)
		}
	}
}

// TestDecisionGateConfigSurface_CLISetMode: config set carries shadow and on
// as strings.
func TestDecisionGateConfigSurface_CLISetMode(t *testing.T) {
	d := startDecisionGateCLIDaemon(t)
	cfg := &config.Config{}
	const key = "autonomous.questions.decision_gate_mode"
	for _, val := range []string{"on", "shadow"} {
		if _, err := captureEscalationStdout(t, func() error {
			return runConfigSet(cfg, key, val)
		}); err != nil {
			t.Fatalf("config set %s = %s: %v", key, val, err)
		}
		if got := d.config[key]; got != val {
			t.Fatalf("stored %s = %#v, want %q", key, got, val)
		}
	}
}

// TestDecisionGateConfigSurface_CLIBadModeRefused: a mode other than shadow or
// on fails config set with the daemon's text and stores nothing.
func TestDecisionGateConfigSurface_CLIBadModeRefused(t *testing.T) {
	d := startDecisionGateCLIDaemon(t)
	cfg := &config.Config{}
	const key = "autonomous.questions.decision_gate_mode"
	for _, bad := range []string{"bogus", "ON", "off", "1"} {
		_, err := captureEscalationStdout(t, func() error {
			return runConfigSet(cfg, key, bad)
		})
		if err == nil {
			t.Fatalf("config set %s = %s: want a refusal, got none", key, bad)
		}
		if !strings.Contains(err.Error(), "decision_gate_mode") {
			t.Errorf("refusal for %s should name the key; got %v", bad, err)
		}
		if _, stored := d.config[key]; stored {
			t.Fatalf("a refused value %s was stored: %#v", bad, d.config[key])
		}
	}
}

// TestDecisionGateConfigSurface_CLIGetReadsKeys: config get prints the value
// the daemon holds for each key.
func TestDecisionGateConfigSurface_CLIGetReadsKeys(t *testing.T) {
	d := startDecisionGateCLIDaemon(t)
	d.configBody = `{"autonomous":{"questions":{"enabled":true,
		"decision_gate":false,"decision_gate_mode":"on"}}}`
	cfg := &config.Config{}
	want := map[string]string{
		"autonomous.questions.decision_gate":      "false",
		"autonomous.questions.decision_gate_mode": `"on"`,
	}
	for key, wantStr := range want {
		out, err := captureEscalationStdout(t, func() error {
			return runConfigGet(cfg, key)
		})
		if err != nil {
			t.Fatalf("config get %s: %v", key, err)
		}
		if got := strings.TrimSpace(out); got != wantStr {
			t.Errorf("config get %s printed %q, want %q", key, got, wantStr)
		}
	}
}
