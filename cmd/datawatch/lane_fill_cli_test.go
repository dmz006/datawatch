package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/dmz006/datawatch/internal/config"
)

// laneFillSampleStatus is a lane fill status with one busy GPU lane, one idle
// GPU lane with a blocker, one idle Claude lane with work waiting, and one
// decision.
const laneFillSampleStatus = `{
  "mode": "shadow",
  "last_poll_at": "2026-10-24T12:00:00Z",
  "lanes": [
    {"id": "gpu-1", "type": "gpu", "state": "occupied",
     "occupant": {"prd_id": "p1", "title": "Add paging", "backend": "pi-prompt"},
     "blocker": "", "eligible_waiting": 0},
    {"id": "gpu-2", "type": "gpu", "state": "idle", "occupant": null,
     "blocker": "no eligible Automaton", "eligible_waiting": 0},
    {"id": "claude-1", "type": "claude", "state": "idle", "occupant": null,
     "blocker": "", "eligible_waiting": 2}
  ],
  "queued_eligible": {"gpu": 0, "claude": 2},
  "evidence": {"checked_at": "2026-10-24T12:00:00Z", "gpu_active_sessions": 1, "loaded_models": [], "error": ""},
  "decisions": [
    {"at": "2026-10-24T12:01:03Z", "mode": "shadow", "lane_id": "claude-1", "lane_type": "claude",
     "action": "start_prd", "prd_id": "p2", "title": "Fix login", "reason": "lane is idle and p2 is eligible",
     "blocker": "", "executed": false, "error": "", "repeat_count": 1, "last_seen_at": "2026-10-24T12:01:03Z"}
  ],
  "alerts": []
}`

// laneFillCLIDaemon runs a fake daemon that answers every request with status
// and body, records the last request's path, query and body, and points the
// CLI at it through a temporary HOME.
type laneFillCLIDaemon struct {
	path, query, body string
	calls             int
}

func newLaneFillCLIDaemon(t *testing.T, status int, reply string) *laneFillCLIDaemon {
	t.Helper()
	d := &laneFillCLIDaemon{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.calls++
		d.path = r.URL.Path
		d.query = r.URL.RawQuery
		raw, _ := io.ReadAll(r.Body)
		d.body = string(raw)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(reply))
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	if err := writeReloadTestConfig(tmp+"/.datawatch/config.yaml", "server:\n  port: "+u.Port()+"\n"); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return d
}

// runLaneFillCmd runs c with argv and returns what it printed.
func runLaneFillCmd(t *testing.T, c *cobra.Command, argv []string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	c.SetArgs(argv)
	c.SetOut(&out)
	c.SetErr(io.Discard)
	err := c.Execute()
	return out.String(), err
}

func TestLaneFillCLI_StatusPrintsSummary(t *testing.T) {
	d := newLaneFillCLIDaemon(t, http.StatusOK, laneFillSampleStatus)
	out, err := runLaneFillCmd(t, newAutonomousLaneFillCmd(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if d.path != "/api/autonomous/lane-fill" || d.query != "" {
		t.Errorf("path=%s query=%q", d.path, d.query)
	}
	want := []string{
		"Lane fill: mode shadow\n",
		`gpu-1: occupied by p1 "Add paging" (pi-prompt)` + "\n",
		"gpu-2: idle, no eligible Automaton\n",
		"claude-1: idle, 2 eligible waiting\n",
		"Queued eligible: gpu 0, claude 2\n",
		"Newest decisions:\n",
		"  12:01:03 claude-1 start_prd p2: lane is idle and p2 is eligible [not executed]\n",
	}
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("summary lacks %q:\n%s", w, out)
		}
	}
}

func TestLaneFillCLI_LimitIsSentAsQuery(t *testing.T) {
	d := newLaneFillCLIDaemon(t, http.StatusOK, laneFillSampleStatus)
	if _, err := runLaneFillCmd(t, newAutonomousLaneFillCmd(), []string{"--limit", "7"}); err != nil {
		t.Fatal(err)
	}
	if d.query != "limit=7" {
		t.Errorf("query=%q, want limit=7", d.query)
	}
}

func TestLaneFillCLI_LimitRefusedLocally(t *testing.T) {
	for _, bad := range []string{"0", "1001", "-3"} {
		d := newLaneFillCLIDaemon(t, http.StatusOK, laneFillSampleStatus)
		if _, err := runLaneFillCmd(t, newAutonomousLaneFillCmd(), []string{"--limit", bad}); err == nil {
			t.Errorf("--limit %s: expected an error", bad)
		}
		if d.calls != 0 {
			t.Errorf("--limit %s: nothing may be sent, saw %d calls", bad, d.calls)
		}
	}
}

func TestLaneFillCLI_JSONPassesBodyUnchanged(t *testing.T) {
	newLaneFillCLIDaemon(t, http.StatusOK, laneFillSampleStatus)
	out, err := runLaneFillCmd(t, newAutonomousLaneFillCmd(), []string{"--json"})
	if err != nil {
		t.Fatal(err)
	}
	if out != laneFillSampleStatus {
		t.Errorf("--json must print the body untouched\ngot:  %q\nwant: %q", out, laneFillSampleStatus)
	}
}

func TestLaneFillCLI_ErrorPassthrough(t *testing.T) {
	newLaneFillCLIDaemon(t, http.StatusBadRequest, "limit must be a whole number from 1 to 1000")
	_, err := runLaneFillCmd(t, newAutonomousLaneFillCmd(), nil)
	if err == nil || !strings.Contains(err.Error(), "HTTP 400") || !strings.Contains(err.Error(), "limit must be a whole number") {
		t.Errorf("expected the daemon refusal to pass through, got %v", err)
	}
}

func TestLaneFillCLI_SetLanePreferencePostsBody(t *testing.T) {
	for in, want := range map[string]string{"gpu": "gpu", "claude": "claude", "any": "any", " GPU ": "gpu"} {
		d := newLaneFillCLIDaemon(t, http.StatusOK, `{}`)
		if _, err := runLaneFillCmd(t, newAutonomousPRDSetLanePreferenceCmd(), []string{"p1", in}); err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		var got map[string]any
		_ = json.Unmarshal([]byte(d.body), &got)
		if d.path != "/api/autonomous/prds/p1/set_lane_preference" || !reflect.DeepEqual(got, map[string]any{"lane_preference": want}) {
			t.Errorf("%q: path=%s body=%s", in, d.path, d.body)
		}
	}
}

func TestLaneFillCLI_SetLanePreferenceRejectsUnknownBeforeSending(t *testing.T) {
	for _, bad := range []string{"fast", "", "gpu2"} {
		d := newLaneFillCLIDaemon(t, http.StatusOK, `{}`)
		_, err := runLaneFillCmd(t, newAutonomousPRDSetLanePreferenceCmd(), []string{"p1", bad})
		if err == nil || !strings.Contains(err.Error(), "gpu, claude or any") {
			t.Errorf("%q: expected a clear error, got %v", bad, err)
		}
		if d.calls != 0 {
			t.Errorf("%q: nothing may be sent for an unknown preference, saw %d calls", bad, d.calls)
		}
	}
}

func TestLaneFillCLI_SetLanePreferenceArgCount(t *testing.T) {
	for _, argv := range [][]string{{"p1"}, {"p1", "gpu", "extra"}} {
		d := newLaneFillCLIDaemon(t, http.StatusOK, `{}`)
		if _, err := runLaneFillCmd(t, newAutonomousPRDSetLanePreferenceCmd(), argv); err == nil {
			t.Errorf("%v: expected an argument error", argv)
		}
		if d.calls != 0 {
			t.Errorf("%v: nothing may be sent on an argument error", argv)
		}
	}
}

func TestLaneFillCLI_CreateSendsLanePreference(t *testing.T) {
	d := newLaneFillCLIDaemon(t, http.StatusOK, `{}`)
	if _, err := runLaneFillCmd(t, newAutonomousPRDCreateCmd(), []string{"Add paging", "--lane-preference", "Claude"}); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	_ = json.Unmarshal([]byte(d.body), &got)
	if d.path != "/api/autonomous/prds" || got["lane_preference"] != "claude" {
		t.Errorf("path=%s body=%s", d.path, d.body)
	}
}

func TestLaneFillCLI_CreateOmitsLanePreferenceByDefault(t *testing.T) {
	d := newLaneFillCLIDaemon(t, http.StatusOK, `{}`)
	if _, err := runLaneFillCmd(t, newAutonomousPRDCreateCmd(), []string{"Add paging"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(d.body, "lane_preference") {
		t.Errorf("lane_preference must be left out when the flag is absent, body=%s", d.body)
	}
}

func TestLaneFillCLI_CreateRejectsUnknownLanePreferenceBeforeSending(t *testing.T) {
	d := newLaneFillCLIDaemon(t, http.StatusOK, `{}`)
	_, err := runLaneFillCmd(t, newAutonomousPRDCreateCmd(), []string{"Add paging", "--lane-preference", "cpu"})
	if err == nil || !strings.Contains(err.Error(), "gpu, claude or any") {
		t.Errorf("expected a clear error, got %v", err)
	}
	if d.calls != 0 {
		t.Errorf("nothing may be sent for an unknown lane preference, saw %d calls", d.calls)
	}
}

func TestLaneFillCLI_Registered(t *testing.T) {
	names := map[string]bool{}
	for _, c := range newAutonomousCmd().Commands() {
		names[c.Name()] = true
	}
	for _, want := range []string{"lane-fill", "prd-set-lane-preference"} {
		if !names[want] {
			t.Errorf("%s is not registered under autonomous", want)
		}
	}
	if !strings.Contains(newAutonomousCmd().Long, "lane-fill [--limit N] [--json]") {
		t.Error("the autonomous help block lacks lane-fill")
	}
}

// The generic config set sends the dotted key straight to PUT /api/config. A
// value that is not valid JSON as typed is retried quoted, so both a number
// and a word reach the daemon under the autonomous.lane_fill.* key.
func TestLaneFillCLI_ConfigSetAcceptsLaneFillKeys(t *testing.T) {
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
		{"autonomous.lane_fill.mode", "on", "on"},
		{"autonomous.lane_fill.gpu_lanes", "3", float64(3)},
		{"autonomous.lane_fill.claude_backend", "claude-code", "claude-code"},
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
