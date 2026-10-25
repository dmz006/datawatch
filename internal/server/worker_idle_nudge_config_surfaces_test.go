// The effective idle-nudge windows are read-only config keys: an unset or
// non-positive setting reads as the default the daemon applies, and a set one
// reads back as configured.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func TestConfigEffectiveWorkerIdleNudge(t *testing.T) {
	getEffective := func(t *testing.T, s *Server) (nudge, stall int) {
		t.Helper()
		rr := httptest.NewRecorder()
		s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("GET: want 200, got %d body=%s", rr.Code, rr.Body.String())
		}
		var got struct {
			Autonomous struct {
				Nudge int `json:"worker_idle_nudge_effective_seconds"`
				Stall int `json:"worker_idle_nudge_stall_effective_seconds"`
			} `json:"autonomous"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode GET: %v", err)
		}
		return got.Autonomous.Nudge, got.Autonomous.Stall
	}

	t.Run("unset settings read as the defaults", func(t *testing.T) {
		s := &Server{
			cfg:           &config.Config{},
			cfgPath:       t.TempDir() + "/config.yaml",
			autonomousMgr: &autonomousConfigPropagationSpy{},
		}
		if nudge, stall := getEffective(t, s); nudge != 180 || stall != 600 {
			t.Fatalf("effective nudge/stall = %d/%d, want 180/600", nudge, stall)
		}
	})

	t.Run("configured settings read back as set", func(t *testing.T) {
		s := &Server{
			cfg:           &config.Config{},
			cfgPath:       t.TempDir() + "/config.yaml",
			autonomousMgr: &autonomousConfigPropagationSpy{},
		}
		b, _ := json.Marshal(map[string]any{
			"autonomous.worker_idle_nudge_seconds":       30,
			"autonomous.worker_idle_nudge_stall_seconds": 90,
		})
		rr := httptest.NewRecorder()
		s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(b)))
		if rr.Code != http.StatusOK {
			t.Fatalf("PUT: want 200, got %d body=%s", rr.Code, rr.Body.String())
		}
		if nudge, stall := getEffective(t, s); nudge != 30 || stall != 90 {
			t.Fatalf("effective nudge/stall = %d/%d, want 30/90", nudge, stall)
		}
	})
}
