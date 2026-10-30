// Config-parity test for the worker question marker switch on the CLI:
// datawatch config set must send autonomous.questions.worker_marker as a real
// boolean the daemon's stored config carries, and datawatch config get must
// read the value back.
package main

import (
	"strconv"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

const questionWorkerMarkerCLIKey = "autonomous.questions.worker_marker"

func TestQuestionWorkerMarkerConfig_CLISetSwitch(t *testing.T) {
	d := startEscalationCLIDaemon(t)
	cfg := &config.Config{}
	for _, val := range []bool{true, false} {
		if _, err := captureEscalationStdout(t, func() error {
			return runConfigSet(cfg, questionWorkerMarkerCLIKey, strconv.FormatBool(val))
		}); err != nil {
			t.Fatalf("config set %s = %v: %v", questionWorkerMarkerCLIKey, val, err)
		}
		if got := d.config[questionWorkerMarkerCLIKey]; got != val {
			t.Fatalf("stored %s = %#v, want %v", questionWorkerMarkerCLIKey, got, val)
		}
	}
}

func TestQuestionWorkerMarkerConfig_CLIGetReadsSwitch(t *testing.T) {
	d := startEscalationCLIDaemon(t)
	cfg := &config.Config{}
	for _, want := range []string{"true", "false"} {
		d.configBody = `{"autonomous":{"questions":{"enabled":true,"worker_marker":` + want + `}}}`
		out, err := captureEscalationStdout(t, func() error {
			return runConfigGet(cfg, questionWorkerMarkerCLIKey)
		})
		if err != nil {
			t.Fatalf("config get %s: %v", questionWorkerMarkerCLIKey, err)
		}
		if got := strings.TrimSpace(out); got != want {
			t.Errorf("config get %s printed %q, want %q", questionWorkerMarkerCLIKey, got, want)
		}
	}
}
