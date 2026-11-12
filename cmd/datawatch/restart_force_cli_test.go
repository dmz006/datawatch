package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// restartCLIReportBody is the daemon's GET /api/autonomous/restart-interruptions
// body for forcedReport(), the report a refused CLI restart must list.
func restartCLIReportBody(t *testing.T) []byte {
	t.Helper()
	raw, err := json.Marshal(forcedReport())
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	return raw
}

func TestRestartCLIRefusesWithoutForceAndListsSessions(t *testing.T) {
	var paths []string
	fetch := func(path string, _ url.Values) ([]byte, error) {
		paths = append(paths, path)
		return restartCLIReportBody(t), nil
	}
	var out bytes.Buffer
	err := restartCLIGuard(false, fetch, &out)
	if err == nil {
		t.Fatal("restart without --force returned nil; want a refusal (non-zero exit)")
	}
	if len(paths) != 1 || paths[0] != "/api/autonomous/restart-interruptions" {
		t.Fatalf("guard read %v; want the restart-interruptions route once", paths)
	}
	for _, want := range []string{"auto-plan-1", "prd-v/task-v", "sess-judge", "sess-a, sess-b", "datawatch restart --force"} { // service-scope-ok: fixture text the refusal prints, never run
		if !strings.Contains(out.String(), want) {
			t.Errorf("refusal output missing %q:\n%s", want, out.String())
		}
	}
}

func TestRestartCLIProceedsWithForce(t *testing.T) {
	fetched := false
	fetch := func(string, url.Values) ([]byte, error) {
		fetched = true
		return restartCLIReportBody(t), nil
	}
	var out bytes.Buffer
	if err := restartCLIGuard(true, fetch, &out); err != nil {
		t.Fatalf("restart with --force refused: %v", err)
	}
	if fetched {
		t.Error("restart with --force read the report; it should skip the check")
	}
	if out.Len() != 0 {
		t.Errorf("restart with --force printed %q; want nothing", out.String())
	}
}

func TestRestartCLIProceedsWhenNoForceNeeded(t *testing.T) {
	raw, _ := json.Marshal(autonomous.RestartInterruption{NeedsForce: false})
	fetch := func(string, url.Values) ([]byte, error) { return raw, nil }
	if err := restartCLIGuard(false, fetch, &bytes.Buffer{}); err != nil {
		t.Fatalf("restart with nothing running refused: %v", err)
	}
}

func TestRestartCLIProceedsWhenDaemonUnreachable(t *testing.T) {
	fetch := func(string, url.Values) ([]byte, error) { return nil, errors.New("daemon not reachable") }
	if err := restartCLIGuard(false, fetch, &bytes.Buffer{}); err != nil {
		t.Fatalf("restart with no running daemon refused: %v", err)
	}
}
