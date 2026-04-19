// BL90 — httptest-driven API endpoint coverage.
//
// Construct a real Server with a real session.Manager (FakeTmux swapped
// in via BL89) and exercise the HTTP contract of the most-used endpoints:
// sessions, config get/set, devices, federation, health/info.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/devices"
	"github.com/dmz006/datawatch/internal/session"
)

func bl90Server(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	mgr, err := session.NewManager("testhost", dir, "/bin/echo", 0)
	if err != nil {
		t.Fatal(err)
	}
	mgr.WithFakeTmux()
	devStore, err := devices.NewStore(dir + "/devices.json")
	if err != nil {
		t.Fatal(err)
	}
	return &Server{
		manager:  mgr,
		hostname: "testhost",
		cfg: &config.Config{
			Session: config.SessionConfig{
				MaxSessions:      10,
				ScheduleSettleMs: 200,
				TailLines:        20,
			},
			Server: config.ServerConfig{Host: "127.0.0.1", Port: 8080},
		},
		cfgPath:     dir + "/cfg.yaml",
		deviceStore: devStore,
	}
}

func TestBL90_Health(t *testing.T) {
	s := bl90Server(t)
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rr := httptest.NewRecorder()
	s.handleHealth(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("health code=%d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "\"status\"") {
		t.Errorf("health body missing status: %s", rr.Body.String())
	}
}

func TestBL90_Info_ReportsVersion(t *testing.T) {
	s := bl90Server(t)
	req := httptest.NewRequest(http.MethodGet, "/api/info", nil)
	rr := httptest.NewRecorder()
	s.handleInfo(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("info code=%d body=%s", rr.Code, rr.Body.String())
	}
	var v map[string]any
	_ = json.NewDecoder(rr.Body).Decode(&v)
	if v["version"] == nil {
		t.Errorf("info missing version: %+v", v)
	}
}

func TestBL90_Sessions_List_Empty(t *testing.T) {
	s := bl90Server(t)
	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	rr := httptest.NewRecorder()
	s.handleSessions(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("sessions code=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestBL90_Sessions_List_ContainsSeeded(t *testing.T) {
	s := bl90Server(t)
	_ = s.manager.SaveSession(&session.Session{
		ID: "aa", FullID: "testhost-aa", State: session.StateRunning,
		UpdatedAt: time.Now(),
	})
	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	rr := httptest.NewRecorder()
	s.handleSessions(rr, req)
	if !strings.Contains(rr.Body.String(), "testhost-aa") {
		t.Errorf("sessions list missing seeded id: %s", rr.Body.String())
	}
}

func TestBL90_Config_GetExposesScheduleSettleMs(t *testing.T) {
	s := bl90Server(t)
	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	rr := httptest.NewRecorder()
	s.handleConfig(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("config code=%d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "schedule_settle_ms") {
		t.Errorf("config missing schedule_settle_ms: %s", rr.Body.String())
	}
}

func TestBL90_Config_PutUpdatesScheduleSettleMs(t *testing.T) {
	s := bl90Server(t)
	patch := map[string]any{"session.schedule_settle_ms": 50}
	buf, _ := json.Marshal(patch)
	req := httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	s.handleConfig(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("config PUT code=%d body=%s", rr.Code, rr.Body.String())
	}
	if s.cfg.Session.ScheduleSettleMs != 50 {
		t.Errorf("cfg not updated: got %d", s.cfg.Session.ScheduleSettleMs)
	}
	if s.manager.ScheduleSettleMs() != 50 {
		t.Errorf("manager not updated: got %d", s.manager.ScheduleSettleMs())
	}
}

func TestBL90_Config_RejectsPost(t *testing.T) {
	s := bl90Server(t)
	req := httptest.NewRequest(http.MethodPost, "/api/config", strings.NewReader("{}"))
	rr := httptest.NewRecorder()
	s.handleConfig(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("want 405 for POST /api/config, got %d", rr.Code)
	}
}

func TestBL90_Devices_RegisterListDelete(t *testing.T) {
	s := bl90Server(t)
	payload := `{"device_token":"tok-abc","kind":"fcm","platform":"android","app_version":"1.0.0"}`
	req := httptest.NewRequest(http.MethodPost, "/api/devices/register",
		strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	s.handleDevicesRegister(rr, req)
	if rr.Code != http.StatusOK && rr.Code != http.StatusCreated {
		t.Fatalf("register code=%d body=%s", rr.Code, rr.Body.String())
	}
	var reg map[string]any
	_ = json.NewDecoder(rr.Body).Decode(&reg)
	id, _ := reg["device_id"].(string)
	if id == "" {
		t.Fatalf("register returned no device_id: %+v", reg)
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/devices", nil)
	s.handleDevicesList(rr, req)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "tok-abc") {
		t.Fatalf("list missing token: code=%d body=%s",
			rr.Code, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodDelete, "/api/devices/"+id, nil)
	s.handleDevicesList(rr, req)
	if rr.Code != http.StatusOK && rr.Code != http.StatusNoContent {
		t.Fatalf("delete code=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestBL90_Federation_RejectsPost_Shadow(t *testing.T) {
	s := bl90Server(t)
	req := httptest.NewRequest(http.MethodPost, "/api/federation/sessions", nil)
	rr := httptest.NewRecorder()
	s.handleFederationSessions(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("status=%d want 405", rr.Code)
	}
}
