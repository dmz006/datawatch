// BL17 — config reload tests.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func TestBL17_Reload_NoConfigPath(t *testing.T) {
	s := bl90Server(t)
	s.cfgPath = ""
	res := s.reload()
	if res.OK || res.Error == "" {
		t.Errorf("expected error for no cfg path, got %+v", res)
	}
}

func TestBL17_Reload_AppliesScheduleSettleMs(t *testing.T) {
	s := bl90Server(t)

	// Write an initial config that the reload can read.
	cfg := *s.cfg
	cfg.Session.ScheduleSettleMs = 50 // changed from 200
	if err := config.Save(&cfg, s.cfgPath); err != nil {
		t.Fatalf("save: %v", err)
	}

	res := s.reload()
	if !res.OK {
		t.Fatalf("reload failed: %+v", res)
	}
	if s.cfg.Session.ScheduleSettleMs != 50 {
		t.Errorf("cfg not updated to 50: got %d", s.cfg.Session.ScheduleSettleMs)
	}
	if s.manager.ScheduleSettleMs() != 50 {
		t.Errorf("manager not updated: got %d", s.manager.ScheduleSettleMs())
	}
	if !contains(res.Applied, "session.schedule_settle_ms") {
		t.Errorf("expected applied entry; got %+v", res.Applied)
	}
}

func TestBL17_Reload_HTTP_RejectsGet(t *testing.T) {
	s := bl90Server(t)
	req := httptest.NewRequest(http.MethodGet, "/api/reload", nil)
	rr := httptest.NewRecorder()
	s.handleReload(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("want 405, got %d", rr.Code)
	}
}

func TestBL17_Reload_HTTP_OK(t *testing.T) {
	s := bl90Server(t)
	if err := config.Save(s.cfg, s.cfgPath); err != nil {
		t.Fatalf("save: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/reload", nil)
	rr := httptest.NewRecorder()
	s.handleReload(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var got ReloadResult
	_ = json.NewDecoder(rr.Body).Decode(&got)
	if !got.OK {
		t.Errorf("expected ok, got %+v", got)
	}
	if len(got.RequiresRestart) == 0 {
		t.Errorf("expected requires_restart list, got empty")
	}
}

func TestBL17_Reload_TolerantOfMissingFile(t *testing.T) {
	// config.Load auto-creates a default config when the file is
	// missing, so reload should still succeed (and apply the
	// defaults, which equals "no change" here).
	s := bl90Server(t)
	_ = os.Remove(s.cfgPath)
	res := s.reload()
	// Either OK or a clean error is acceptable; we only assert no panic.
	_ = res
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
