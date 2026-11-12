// Config-parity test for the operator-question channel's notify toggles on
// the CLI: datawatch config set must send each of
// autonomous.questions.notify_messaging, .notify_alert and .return_path (all
// defaulting on when absent) as a real boolean the daemon's stored config
// carries, and datawatch config get must read the value back (Configuration-
// Accessibility rule — settable and readable from the CLI without editing
// YAML), mirroring TestQuestionsCLIConfigSetTogglesChannel.
package main

import (
	"strconv"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func questionNotifyTCliKeys() []string {
	return []string{
		"autonomous.questions.notify_messaging",
		"autonomous.questions.notify_alert",
		"autonomous.questions.return_path",
	}
}

// TestQuestionNotifyConfigSurface_SetToggles: config set round-trips each
// notify toggle, and an explicit false overwrites an explicit true.
func TestQuestionNotifyConfigSurface_SetToggles(t *testing.T) {
	d := startEscalationCLIDaemon(t)
	cfg := &config.Config{}
	for _, key := range questionNotifyTCliKeys() {
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
}

// TestQuestionNotifyConfigSurface_GetReadsToggles: config get prints the
// value the daemon holds for each toggle.
func TestQuestionNotifyConfigSurface_GetReadsToggles(t *testing.T) {
	d := startEscalationCLIDaemon(t)
	d.configBody = `{"autonomous":{"questions":{"enabled":true,
		"notify_messaging":false,"notify_alert":false,"return_path":true}}}`
	cfg := &config.Config{}
	want := map[string]string{
		"autonomous.questions.notify_messaging": "false",
		"autonomous.questions.notify_alert":     "false",
		"autonomous.questions.return_path":      "true",
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
