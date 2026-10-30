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

// cleanupCLISample is a cleanup status with a run in the worktrees pass,
// the temp pass not run yet, two disk readings, one of them over the
// threshold, and the disk alert active.
const cleanupCLISample = `{
  "worktrees": {
    "mode": "report",
    "last_run": "2026-11-01T09:00:00Z",
    "removed": 3, "would_remove": 2, "skipped": 1, "failed": 1,
    "bytes_freed": 2097152,
    "last_error": "permission denied",
    "owner_removed": 2, "net_removed": 1,
    "owner_bytes_freed": 1048576, "net_bytes_freed": 1048576
  },
  "temp": {"mode": "on"},
  "disk_alert_percent": 85,
  "worktree_disk": {"path": "/home/dmz/repo", "percent": 82, "alert": false},
  "temp_disk": {"path": "/tmp", "percent": 91, "alert": true},
  "disk_alert": true,
  "last_disk_alert_at": "2026-11-01T08:00:00Z"
}`

func TestCleanupCLI_StatusPrints(t *testing.T) {
	d := newLaneFillCLIDaemon(t, http.StatusOK, cleanupCLISample)
	out, err := runLaneFillCmd(t, newAutonomousCleanupCmd(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if d.path != "/api/autonomous/cleanup" || d.query != "" {
		t.Errorf("path=%s query=%q; want the cleanup route with no filters", d.path, d.query)
	}
	for _, want := range []string{
		"Automaton cleanup (disk alert threshold: 85%):",
		"worktrees pass (mode report): last run 2026-11-01T09:00:00Z, removed 3, would-remove 2, skipped 1, failed 1, freed 2097152 bytes",
		"owner vs net: 2/1 removed, 1048576/1048576 bytes freed",
		"last error: permission denied",
		"temp pass: not run yet",
		"worktree disk: /home/dmz/repo at 82%",
		"temp dir disk: /tmp at 91% (alert)",
		"disk alert: active (last raised 2026-11-01T08:00:00Z)",
		"This is a status read only: the cleanup passes do the removing, not this read.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("summary lacks %q:\n%s", want, out)
		}
	}
}

func TestCleanupCLI_JSONPassesBodyUnchanged(t *testing.T) {
	newLaneFillCLIDaemon(t, http.StatusOK, cleanupCLISample)
	out, err := runLaneFillCmd(t, newAutonomousCleanupCmd(), []string{"--json"})
	if err != nil {
		t.Fatal(err)
	}
	if out != cleanupCLISample {
		t.Errorf("--json must print the body untouched\ngot:  %q\nwant: %q", out, cleanupCLISample)
	}
}

func TestCleanupCLI_ErrorPassthrough(t *testing.T) {
	newLaneFillCLIDaemon(t, http.StatusBadRequest, "the daemon has no cleanup status")
	_, err := runLaneFillCmd(t, newAutonomousCleanupCmd(), nil)
	if err == nil || !strings.Contains(err.Error(), "HTTP 400") || !strings.Contains(err.Error(), "no cleanup status") {
		t.Errorf("expected the daemon error to pass through, got %v", err)
	}
}

func TestCleanupCLI_Registered(t *testing.T) {
	names := map[string]bool{}
	for _, c := range newAutonomousCmd().Commands() {
		names[c.Name()] = true
	}
	if !names["cleanup"] {
		t.Error("cleanup is not registered under autonomous")
	}
	if !strings.Contains(newAutonomousCmd().Long, "cleanup [--json]") {
		t.Error("the autonomous help block lacks cleanup")
	}
}

// The generic config set sends the dotted key straight to PUT /api/config,
// so the four cleanup knobs reach the daemon under their autonomous.
// automation.* / autonomous.* names, number as typed and word as typed.
func TestCleanupCLI_ConfigSetAcceptsCleanupKeys(t *testing.T) {
	var sent map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if !json.Valid(raw) {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		sent = nil
		_ = json.Unmarshal(raw, &sent)
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	cfg.Server.Port = port

	cases := []struct {
		key, value string
		want       any
	}{
		{"autonomous.automation.cleanup_mode", "report", "report"},
		{"autonomous.automation.cleanup_retention_hours", "48", float64(48)},
		{"autonomous.automation.cleanup_max_removals_per_pass", "100", float64(100)},
		{"autonomous.automation.disk_alert_percent", "90", float64(90)},
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
