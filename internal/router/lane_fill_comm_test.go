package router

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// laneFillCommStatus builds a lane fill status with decisions d1..dN, newest
// first, so a test can tell which decisions a reply showed.
func laneFillCommStatus(decisions int) string {
	var ds []string
	for i := 1; i <= decisions; i++ {
		ds = append(ds, fmt.Sprintf(`{"at":"2026-10-24T12:01:%02dZ","lane_id":"claude-1","action":"start_prd","prd_id":"d%d","reason":"r%d","executed":false,"repeat_count":1}`, i, i, i))
	}
	return `{"mode":"shadow","lanes":[` +
		`{"id":"gpu-1","type":"gpu","state":"occupied","occupant":{"prd_id":"p1","title":"Add paging","backend":"pi-prompt"},"blocker":"","eligible_waiting":0},` +
		`{"id":"claude-1","type":"claude","state":"idle","occupant":null,"blocker":"","eligible_waiting":2}],` +
		`"queued_eligible":{"gpu":0,"claude":2},"evidence":{"error":""},` +
		`"decisions":[` + strings.Join(ds, ",") + `],"alerts":[]}`
}

// laneFillCommDaemon answers every request with status and body and records
// the last request's path and query.
func laneFillCommDaemon(t *testing.T, status int, body string) (*lanePathLog, int) {
	t.Helper()
	log := &lanePathLog{}
	_, port := newFakePeerParent(t, func(w http.ResponseWriter, r *http.Request) {
		log.calls++
		log.path = r.URL.Path
		log.query = r.URL.RawQuery
		raw, _ := io.ReadAll(r.Body)
		log.body = string(raw)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
	return log, port
}

type lanePathLog struct {
	calls       int
	path, query string
	body        string
}

func TestLaneFillComm_StatusRendersLanesQueuedAndNewestFive(t *testing.T) {
	log, port := laneFillCommDaemon(t, http.StatusOK, laneFillCommStatus(7))
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous lane-fill"), " ")
	if log.path != "/api/autonomous/lane-fill" || log.query != "limit=5" {
		t.Errorf("path=%s query=%q, want the lane fill route with limit=5", log.path, log.query)
	}
	for _, want := range []string{
		`gpu-1: occupied by p1 "Add paging" (pi-prompt)`,
		"claude-1: idle, 2 eligible waiting",
		"Queued eligible: gpu 0, claude 2",
		"Newest decisions:",
		"d1", "d5",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("reply lacks %q: %q", want, got)
		}
	}
	for _, notWant := range []string{"d6", "d7"} {
		if strings.Contains(got, notWant) {
			t.Errorf("reply must show only the newest five decisions, but shows %s: %q", notWant, got)
		}
	}
}

func TestLaneFillComm_LimitIsPassedThrough(t *testing.T) {
	log, port := laneFillCommDaemon(t, http.StatusOK, laneFillCommStatus(2))
	r := setupPeerRouter(t, port)
	r.HandleTestMessage("autonomous lane-fill 12")
	if log.query != "limit=12" {
		t.Errorf("query=%q, want limit=12", log.query)
	}
}

func TestLaneFillComm_BadLimitShowsUsageAndSendsNothing(t *testing.T) {
	for _, msg := range []string{"autonomous lane-fill abc", "autonomous lane-fill 0", "autonomous lane-fill 1001", "autonomous lane-fill 3 extra"} {
		log, port := laneFillCommDaemon(t, http.StatusOK, laneFillCommStatus(1))
		r := setupPeerRouter(t, port)
		got := strings.Join(r.HandleTestMessage(msg), " ")
		if !strings.Contains(got, "usage: autonomous lane-fill [limit]") {
			t.Errorf("%q: expected the usage, got %q", msg, got)
		}
		if log.calls != 0 {
			t.Errorf("%q: a bad limit must not reach the daemon, got %d calls", msg, log.calls)
		}
	}
}

func TestLaneFillComm_DaemonRefusalIsReported(t *testing.T) {
	_, port := laneFillCommDaemon(t, http.StatusBadRequest, "limit must be a whole number from 1 to 1000")
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous lane-fill"), " ")
	if !strings.Contains(got, "lane-fill failed") || !strings.Contains(got, "HTTP 400") {
		t.Errorf("expected a failure reply carrying the refusal, got %q", got)
	}
}

func TestLaneFillComm_SetLanePreferencePostsBody(t *testing.T) {
	for msg, want := range map[string]string{
		"autonomous set-lane-preference p1 gpu":    "gpu",
		"autonomous set_lane_preference p1 CLAUDE": "claude",
		"autonomous set-lane-preference p1 any":    "any",
	} {
		log, port := laneFillCommDaemon(t, http.StatusOK, `{}`)
		r := setupPeerRouter(t, port)
		r.HandleTestMessage(msg)
		var got map[string]string
		_ = json.Unmarshal([]byte(log.body), &got)
		if log.path != "/api/autonomous/prds/p1/set_lane_preference" || got["lane_preference"] != want || len(got) != 1 {
			t.Errorf("%q: path=%s body=%s", msg, log.path, log.body)
		}
	}
}

func TestLaneFillComm_UnknownPreferenceShowsUsageAndSendsNothing(t *testing.T) {
	log, port := laneFillCommDaemon(t, http.StatusOK, `{}`)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous set-lane-preference p1 fast"), " ")
	if !strings.Contains(got, "usage: autonomous set-lane-preference <prd-id> <gpu|claude|any>") || !strings.Contains(got, "fast") {
		t.Errorf("expected the usage naming the value, got %q", got)
	}
	if log.calls != 0 {
		t.Errorf("an unknown preference must not reach the daemon, got %d calls", log.calls)
	}
}

func TestLaneFillComm_MissingArgsShowUsage(t *testing.T) {
	r := &Router{}
	for msg, usage := range map[string]string{
		"autonomous set-lane-preference":    "usage: autonomous set-lane-preference",
		"autonomous set-lane-preference p1": "usage: autonomous set-lane-preference",
	} {
		got := strings.Join(r.HandleTestMessage(msg), " ")
		if !strings.Contains(got, usage) {
			t.Errorf("%q: expected usage, got %q", msg, got)
		}
	}
}

func TestLaneFillComm_SetLanePreferenceDaemonRefusalIsReported(t *testing.T) {
	_, port := laneFillCommDaemon(t, http.StatusNotFound, "not found")
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous set-lane-preference nope gpu"), " ")
	if !strings.Contains(got, "set-lane-preference failed") {
		t.Errorf("expected a failure reply, got %q", got)
	}
}

func TestLaneFillComm_HelpListsVerbs(t *testing.T) {
	r := &Router{}
	help := strings.Join(r.HandleTestMessage("autonomous"), " ")
	for _, want := range []string{"lane-fill [limit]", "set-lane-preference <prd-id> <gpu|claude|any>"} {
		if !strings.Contains(help, want) {
			t.Errorf("autonomous help lacks %q: %q", want, help)
		}
	}
}

// configure forwards any key to the daemon's PUT /api/config, which checks
// every autonomous.lane_fill.* value. The router must pass the keys through
// unchanged and list them in both of its usage strings.
func TestLaneFillComm_ConfigureForwardsLaneFillKeys(t *testing.T) {
	r := &Router{}
	var gotKey, gotValue string
	r.SetConfigureFunc(func(key, value string) error {
		gotKey, gotValue = key, value
		return nil
	})
	r.HandleTestMessage("configure autonomous.lane_fill.mode=on")
	if gotKey != "autonomous.lane_fill.mode" || gotValue != "on" {
		t.Errorf("configure forwarded %q=%q", gotKey, gotValue)
	}
	r.HandleTestMessage("configure autonomous.lane_fill.gpu_lanes=3")
	if gotKey != "autonomous.lane_fill.gpu_lanes" || gotValue != "3" {
		t.Errorf("configure forwarded %q=%q", gotKey, gotValue)
	}

	keys := []string{
		"autonomous.lane_fill.mode", "autonomous.lane_fill.gpu_lanes", "autonomous.lane_fill.claude_lanes",
		"autonomous.lane_fill.idle_alert_minutes", "autonomous.lane_fill.decompose_ahead_min",
		"autonomous.lane_fill.poll_seconds", "autonomous.lane_fill.gpu_backend",
		"autonomous.lane_fill.gpu_model", "autonomous.lane_fill.claude_backend",
	}
	for _, reply := range [][]string{r.HandleTestMessage("configure help"), r.HandleTestMessage("configure list")} {
		text := strings.Join(reply, " ")
		for _, k := range keys {
			if !strings.Contains(text, k) {
				t.Errorf("configure usage lacks %s", k)
			}
		}
	}
}
