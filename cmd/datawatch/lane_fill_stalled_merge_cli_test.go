package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// stalledMergeCLIStatus is a lane fill status with one idle Claude lane and
// two Automata waiting with no live work.
const stalledMergeCLIStatus = `{
  "mode": "on",
  "lanes": [
    {"id": "claude-1", "type": "claude", "state": "idle", "occupant": null,
     "blocker": "", "eligible_waiting": 1}
  ],
  "queued_eligible": {"gpu": 0, "claude": 1},
  "evidence": {"error": ""},
  "decisions": [],
  "alerts": [],
  "waiting": [
    {"prd_id": "w1", "title": "Add export", "backend": "claude-code",
     "wait_reason": "its merge has been pending for 41 minutes with no progress", "reason_code": "merge_stalled"},
    {"prd_id": "w2", "title": "Fix login", "backend": "pi-prompt",
     "wait_reason": "every task is completed", "reason_code": "waiting_review_merge"}
  ]
}`

// TestStalledMergeCLI_StatusListsWaitingWithReason: the lane-fill status
// prints each waiting Automaton with its title, reason text and reason code.
func TestStalledMergeCLI_StatusListsWaitingWithReason(t *testing.T) {
	newLaneFillCLIDaemon(t, http.StatusOK, stalledMergeCLIStatus)
	out, err := runLaneFillCmd(t, newAutonomousLaneFillCmd(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Waiting with no live work",
		`w1 "Add export" (claude-code): its merge has been pending for 41 minutes with no progress [merge_stalled]`,
		`w2 "Fix login" (pi-prompt): every task is completed [waiting_review_merge]`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("status output lacks %q:\n%s", want, out)
		}
	}
}

// TestStalledMergeCLI_JSONKeepsWaitingList: --json prints the body untouched,
// waiting list included.
func TestStalledMergeCLI_JSONKeepsWaitingList(t *testing.T) {
	newLaneFillCLIDaemon(t, http.StatusOK, stalledMergeCLIStatus)
	out, err := runLaneFillCmd(t, newAutonomousLaneFillCmd(), []string{"--json"})
	if err != nil {
		t.Fatal(err)
	}
	if out != stalledMergeCLIStatus {
		t.Errorf("--json changed the body:\n%s", out)
	}
}

// stalledMergeConfigDaemon answers every config PUT with 200 and records each
// request body, so a test can tell what the CLI sent and what it refused.
func stalledMergeConfigDaemon(t *testing.T) (*config.Config, *[]map[string]any) {
	t.Helper()
	var sent []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		sent = append(sent, body)
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
	return cfg, &sent
}

// TestStalledMergeCLI_ConfigSetSendsWholeNumbers: the config key takes 0 (off),
// the default 30 and any other whole number, sent as a JSON number.
func TestStalledMergeCLI_ConfigSetSendsWholeNumbers(t *testing.T) {
	for value, want := range map[string]float64{"0": 0, "30": 30, "45": 45, " 12 ": 12} {
		cfg, sent := stalledMergeConfigDaemon(t)
		if err := runConfigSet(cfg, config.LaneFillStalledMergeMinutesKey, value); err != nil {
			t.Fatalf("config set %q: %v", value, err)
		}
		exp := []map[string]any{{config.LaneFillStalledMergeMinutesKey: want}}
		if !reflect.DeepEqual(*sent, exp) {
			t.Errorf("config set %q: daemon received %v, want %v", value, *sent, exp)
		}
	}
}

// TestStalledMergeCLI_ConfigSetRefusesTheRestBeforeSending: a negative, a
// fraction and text are refused locally, with the message the REST endpoint
// uses, and nothing is sent.
func TestStalledMergeCLI_ConfigSetRefusesTheRestBeforeSending(t *testing.T) {
	want := config.ValidateLaneFillStalledMergeMinutes(-1).Error()
	for _, bad := range []string{"-1", "-30", "1.5", "soon", "true", "[30]", ""} {
		cfg, sent := stalledMergeConfigDaemon(t)
		err := runConfigSet(cfg, config.LaneFillStalledMergeMinutesKey, bad)
		if err == nil {
			t.Errorf("config set %q: want an error", bad)
			continue
		}
		if !strings.Contains(err.Error(), want) {
			t.Errorf("config set %q: error %q lacks %q", bad, err, want)
		}
		if len(*sent) != 0 {
			t.Errorf("config set %q reached the daemon: %v", bad, *sent)
		}
	}
}

// TestStalledMergeCLI_DaemonRefusalPassesThrough: should the daemon refuse a
// value, its message comes back to the operator unchanged.
func TestStalledMergeCLI_DaemonRefusalPassesThrough(t *testing.T) {
	msg := config.ValidateLaneFillStalledMergeMinutes(-1).Error()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, msg, http.StatusBadRequest)
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())
	cfg := &config.Config{}
	cfg.Server.Port = port
	err := runConfigSet(cfg, config.LaneFillStalledMergeMinutesKey, "30")
	if err == nil || !strings.Contains(err.Error(), msg) {
		t.Errorf("error = %v, want one carrying %q", err, msg)
	}
}

// TestStalledMergeCLI_HelpNamesTheSettingWithoutTrackerIDs: the autonomous
// command's help names the key, its range and the how-to, with no tracker id.
func TestStalledMergeCLI_HelpNamesTheSettingWithoutTrackerIDs(t *testing.T) {
	// Only the lane-fill entry is checked: the rest of that help is older and
	// covers other commands.
	long := newAutonomousCmd().Long
	start := strings.Index(long, "  lane-fill [--limit N]")
	end := strings.Index(long, "  lane-utilization")
	if start < 0 || end < start {
		t.Fatal("autonomous help has no lane-fill entry")
	}
	long = long[start:end]
	for _, want := range []string{
		config.LaneFillStalledMergeMinutesKey,
		"0 = off",
		"default 30",
		"docs/howto/automation-lane-fill.md",
	} {
		if !strings.Contains(long, want) {
			t.Errorf("autonomous help lacks %q", want)
		}
	}
	if regexp.MustCompile(`\b(BL|GH#?|TS-?)\d+\b`).MatchString(long) {
		t.Error("autonomous help carries a tracker id")
	}
}
