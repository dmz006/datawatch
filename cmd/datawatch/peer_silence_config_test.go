package main

import (
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// TestPeerSilenceCLIConfigSetKey: datawatch config set writes
// observer.peers.silent_alert_minutes through PUT /api/config as a number
// (0 turns the watcher off).
func TestPeerSilenceCLIConfigSetKey(t *testing.T) {
	const key = "observer.peers.silent_alert_minutes"
	for _, c := range []struct {
		value string
		want  float64
	}{{"9", 9}, {"0", 0}} {
		d := startInfraRetryConfigDaemon(t)
		out, err := captureEscalationStdout(t, func() error {
			return runConfigSet(&config.Config{}, key, c.value)
		})
		if err != nil {
			t.Fatalf("%s=%s: %v", key, c.value, err)
		}
		if got := d.config[key]; got != c.want {
			t.Errorf("daemon stored %#v, want %v", got, c.want)
		}
		if !strings.Contains(out, "Set "+key+" = "+c.value) {
			t.Errorf("output = %q", out)
		}
	}
}
