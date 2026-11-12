package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

func TestRestartPolicyConfig_DefaultsAndRoundTrip(t *testing.T) {
	s := &Server{cfg: config.DefaultConfig(), cfgPath: filepath.Join(t.TempDir(), "config.yaml")}
	get := func() map[string]interface{} {
		rr := httptest.NewRecorder()
		s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
		var got struct {
			Autonomous map[string]interface{} `json:"autonomous"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		return got.Autonomous
	}
	a := get()
	if a["restart_policy"] != "immediate" || a["restart_on_timeout"] != "proceed" || a["restart_quiet_timeout_seconds"] != float64(600) {
		t.Fatalf("defaults = %v %v %v", a["restart_policy"], a["restart_on_timeout"], a["restart_quiet_timeout_seconds"])
	}
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{
		"autonomous.restart_policy":                "Wait_Quiet",
		"autonomous.restart_on_timeout":            "abort",
		"autonomous.restart_quiet_timeout_seconds": 90,
	})
	a = get()
	if a["restart_policy"] != "wait_quiet" || a["restart_on_timeout"] != "abort" || a["restart_quiet_timeout_seconds"] != float64(90) {
		t.Fatalf("after PUT = %v %v %v", a["restart_policy"], a["restart_on_timeout"], a["restart_quiet_timeout_seconds"])
	}
	if s.cfg.Autonomous.RestartPolicy != "wait_quiet" {
		t.Fatalf("stored policy = %q", s.cfg.Autonomous.RestartPolicy)
	}
}

func TestRestartPolicyConfig_RejectsBadValues(t *testing.T) {
	for _, body := range []map[string]any{
		{"autonomous.restart_policy": "later"},
		{"autonomous.restart_on_timeout": "maybe"},
		{"autonomous.restart_quiet_timeout_seconds": -5},
		{"autonomous.restart_quiet_timeout_seconds": "soon"},
	} {
		s := &Server{cfg: config.DefaultConfig(), cfgPath: filepath.Join(t.TempDir(), "config.yaml")}
		b, _ := json.Marshal(body)
		rr := httptest.NewRecorder()
		s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(b)))
		if rr.Code != http.StatusBadRequest {
			t.Errorf("PUT %s: want 400, got %d", b, rr.Code)
		}
	}
}

type fakeRestartReader struct {
	AutonomousAPI
	rep autonomous.RestartInterruption
}

func (f fakeRestartReader) RestartInterruptions() autonomous.RestartInterruption { return f.rep }

func TestRestartInterruptionsRead(t *testing.T) {
	s := &Server{cfg: config.DefaultConfig()}
	rr := httptest.NewRecorder()
	s.handleAutonomousRestartInterruptions(rr, httptest.NewRequest(http.MethodGet, "/api/autonomous/restart-interruptions", nil))
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("no manager: want 503, got %d", rr.Code)
	}
	rr = httptest.NewRecorder()
	s.handleAutonomousRestartInterruptions(rr, httptest.NewRequest(http.MethodPost, "/api/autonomous/restart-interruptions", nil))
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST: want 405, got %d", rr.Code)
	}
	s.autonomousMgr = fakeRestartReader{rep: autonomous.RestartInterruption{
		RunningTasks: []autonomous.InterruptedTask{{PRDID: "p1", TaskID: "t1", Status: "verifying", SessionID: "ab12"}},
		LiveSessions: []string{"ab12"}, RunningTaskCnt: 1, LiveSessionCnt: 1, Policy: "drain",
	}}
	rr = httptest.NewRecorder()
	s.handleAutonomousRestartInterruptions(rr, httptest.NewRequest(http.MethodGet, "/api/autonomous/restart-interruptions", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	got, err := autonomous.ParseRestartInterruption(rr.Body.Bytes())
	if err != nil || got.RunningTaskCnt != 1 || got.RunningTasks[0].TaskID != "t1" || got.LiveSessions[0] != "ab12" || got.Policy != "drain" {
		t.Fatalf("body = %+v err=%v", got, err)
	}
}
