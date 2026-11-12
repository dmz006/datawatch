// Config-parity test for the operator-question channel on the CLI: datawatch
// config set must send autonomous.questions.enabled as a real boolean, and the
// daemon's stored config carries it (Configuration-Accessibility rule — the
// switch is settable from the CLI without editing YAML), mirroring
// TestFailureEscalationCLIConfigSetMaxFailures.
package main

import (
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// TestQuestionsCLIConfigSetTogglesChannel: config set round-trips the channel
// switch.
func TestQuestionsCLIConfigSetTogglesChannel(t *testing.T) {
	d := startEscalationCLIDaemon(t)
	cfg := &config.Config{}
	if _, err := captureEscalationStdout(t, func() error {
		return runConfigSet(cfg, "autonomous.questions.enabled", "true")
	}); err != nil {
		t.Fatalf("config set: %v", err)
	}
	if got := d.config["autonomous.questions.enabled"]; got != true {
		t.Errorf("stored autonomous.questions.enabled = %#v, want true", got)
	}
}
