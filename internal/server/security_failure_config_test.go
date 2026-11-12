// Security failure config — REST surfaces: GET /api/config must expose
// autonomous.automation.security_failure.mode with its effective value,
// and PUT /api/config must set it or refuse a bad value. The one default
// (an unset or empty mode reads shadow, the first-release default) is
// applied in exactly one place (the EffectiveMode accessor), so the
// default surface reads it from there — never a literal.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

func getSecurityFailureSurface(t *testing.T, s *Server) string {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/config: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var got struct {
		Autonomous struct {
			Automation struct {
				SecurityFailure struct {
					Mode string `json:"mode"`
				} `json:"security_failure"`
			} `json:"automation"`
		} `json:"autonomous"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	return got.Autonomous.Automation.SecurityFailure.Mode
}

// TestSecurityFailureConfig — the security failure block's REST write
// path (the config-load path is covered in the internal/config test):
// the default surface reads shadow from the accessor, off/shadow/on are
// accepted, and any other value is refused (400) with the stored value
// left intact.
func TestSecurityFailureConfig(t *testing.T) {
	// The default surface reads shadow — from the EffectiveMode accessor
	// (the first-release default), not a literal.
	spy := &autonomousConfigPropagationSpy{}
	s := &Server{cfg: &config.Config{}, cfgPath: t.TempDir() + "/config.yaml", autonomousMgr: spy}
	if got := getSecurityFailureSurface(t, s); got != autonomous.SecurityFailureModeShadow {
		t.Fatalf("default surface mode = %q, want %q (effective from the accessor)", got, autonomous.SecurityFailureModeShadow)
	}

	// Every legal value is accepted and round-trips through the surface
	// exactly as stored.
	for _, raw := range []string{autonomous.SecurityFailureModeOff, autonomous.SecurityFailureModeShadow, autonomous.SecurityFailureModeOn} {
		spy := &autonomousConfigPropagationSpy{}
		s := &Server{cfg: &config.Config{}, cfgPath: t.TempDir() + "/config.yaml", autonomousMgr: spy}

		patch, _ := json.Marshal(map[string]interface{}{"autonomous.automation.security_failure.mode": raw})
		rr := httptest.NewRecorder()
		s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(patch)))
		if rr.Code != http.StatusOK {
			t.Fatalf("PUT mode=%s: want 200, got %d body=%s", raw, rr.Code, rr.Body.String())
		}
		if got := getSecurityFailureSurface(t, s); got != raw {
			t.Errorf("PUT mode=%s: surface = %q, want %q", raw, got, raw)
		}
	}

	// Any other value is refused (400, an error naming the key) and leaves
	// the stored value intact.
	for _, bad := range []interface{}{"loud", "enabled", "onoff", 3, true, []string{"on"}} {
		spy := &autonomousConfigPropagationSpy{}
		s := &Server{cfg: &config.Config{}, cfgPath: t.TempDir() + "/config.yaml", autonomousMgr: spy}
		// A previously-stored value must survive the refused write.
		seed, _ := json.Marshal(map[string]interface{}{"autonomous.automation.security_failure.mode": "shadow"})
		rrSeed := httptest.NewRecorder()
		s.handlePutConfig(rrSeed, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(seed)))
		if rrSeed.Code != http.StatusOK {
			t.Fatalf("seed PUT: want 200, got %d body=%s", rrSeed.Code, rrSeed.Body.String())
		}

		b, _ := json.Marshal(map[string]interface{}{"autonomous.automation.security_failure.mode": bad})
		rr := httptest.NewRecorder()
		s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(b)))
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("PUT mode=%v: want 400, got %d body=%s", bad, rr.Code, rr.Body.String())
		}
		if !bytes.Contains(rr.Body.Bytes(), []byte("security_failure")) {
			t.Errorf("PUT mode=%v: error %q does not name the security_failure key", bad, rr.Body.String())
		}
		if got := getSecurityFailureSurface(t, s); got != "shadow" {
			t.Errorf("refused write clobbered stored mode: %q, want shadow", got)
		}
	}
}
