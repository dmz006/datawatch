package router

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// cleanupCommSample is a cleanup status with a run in the worktrees pass,
// the temp pass not run yet, two disk readings (one over the threshold),
// and the disk alert active.
const cleanupCommSample = `{
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

func TestCleanupComm_VerbRenders(t *testing.T) {
	log, port := laneFillCommDaemon(t, http.StatusOK, cleanupCommSample)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous cleanup"), " ")
	if log.path != "/api/autonomous/cleanup" || log.query != "" {
		t.Errorf("path=%s query=%q; want the cleanup route with no filters", log.path, log.query)
	}
	var st autonomous.CleanupStatus
	if err := json.Unmarshal([]byte(cleanupCommSample), &st); err != nil {
		t.Fatal(err)
	}
	// The reply is the shared render, line for line.
	for _, line := range strings.Split(strings.TrimRight(autonomous.RenderCleanupStatus(st), "\n"), "\n") {
		if !strings.Contains(got, strings.TrimSpace(line)) {
			t.Errorf("reply lacks shared render line %q: %q", line, got)
		}
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
		if !strings.Contains(got, want) {
			t.Errorf("reply lacks %q: %q", want, got)
		}
	}
}

// TestCleanupComm_AlertTextPostsWhenFlagSet proves the disk alert text is
// posted after the status when the flag is set, with the reading that
// crossed the threshold named and the alert-free surface wording kept.
func TestCleanupComm_AlertTextPostsWhenFlagSet(t *testing.T) {
	log, port := laneFillCommDaemon(t, http.StatusOK, cleanupCommSample)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous cleanup"), " ")
	var st autonomous.CleanupStatus
	if err := json.Unmarshal([]byte(cleanupCommSample), &st); err != nil {
		t.Fatal(err)
	}
	alert := autonomous.CleanupDiskAlertBody(st)
	if !strings.Contains(alert, "/tmp is at 91%") {
		t.Fatalf("test sample must name the hot disk; alert body: %q", alert)
	}
	if !strings.Contains(got, strings.TrimSpace(alert)) {
		t.Errorf("reply must post the disk alert text when the flag is set;\nalert: %q\nreply: %q", alert, got)
	}
	// The hot-disk note is named once per hot disk; the worktree disk reading
	// is under the threshold and must not be blamed.
	if strings.Contains(alert, "/home/dmz/repo is at 82%") {
		t.Errorf("the under-threshold disk must not be blamed: %q", alert)
	}
	_ = log
}

// TestCleanupComm_AlertTextOmittedWhenFlagClear proves the reply stays the
// plain status render when the disk alert flag is not set, even if a
// reading sits near the threshold.
func TestCleanupComm_AlertTextOmittedWhenFlagClear(t *testing.T) {
	body := `{"worktrees":{"mode":"on","removed":1},"temp":{
  "mode":"on","removed":0},
  "disk_alert_percent":85,
  "worktree_disk":{"path":"/home/dmz/repo","percent":84,"alert":false},
  "disk_alert":false}`
	_, port := laneFillCommDaemon(t, http.StatusOK, body)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous cleanup"), " ")
	if !strings.Contains(got, "disk alert: not active") {
		t.Errorf("the render must say the alert is not active: %q", got)
	}
	for _, notWant := range []string{
		"Automaton cleanup: ", // the alert body prefix, distinct from the render header
		"Removing the Automaton worktrees and session temp files",
	} {
		if strings.Contains(got, notWant) {
			t.Errorf("reply must not carry the disk alert text when the flag is clear: %q", got)
		}
	}
}

func TestCleanupComm_UnknownSubcommandShowsHelp(t *testing.T) {
	log, port := laneFillCommDaemon(t, http.StatusOK, cleanupCommSample)
	r := setupPeerRouter(t, port)

	// Extra arguments are refused with the verb's usage, without a daemon call.
	got := strings.Join(r.HandleTestMessage("autonomous cleanup everything"), " ")
	if !strings.Contains(got, "usage: autonomous cleanup") {
		t.Errorf("extra args must be refused with the usage, got: %q", got)
	}
	if log.calls != 0 {
		t.Errorf("the refusal must not reach the daemon, got %d calls", log.calls)
	}

	// An unknown verb gets the autonomous help, which lists the cleanup verb.
	got = strings.Join(r.HandleTestMessage("autonomous cleon"), " ")
	if !strings.Contains(got, "unknown verb cleon") || !strings.Contains(got, "|cleanup|") {
		t.Errorf("an unknown verb must show the help listing the cleanup verb, got: %q", got)
	}
	if log.calls != 0 {
		t.Errorf("an unknown verb must not reach the daemon, got %d calls", log.calls)
	}
}

func TestCleanupComm_ParityListHasVerb(t *testing.T) {
	verbs := CleanupCommVerbs()
	if !slices.Contains(verbs, "cleanup") {
		t.Errorf("CleanupCommVerbs() = %v, must contain %q", verbs, "cleanup")
	}
	for _, verb := range verbs {
		if c := Parse("autonomous " + verb); c.Type != CmdAutonomous {
			t.Errorf(`Parse("autonomous %s").Type = %v, want CmdAutonomous`, verb, c.Type)
		}
	}
}
