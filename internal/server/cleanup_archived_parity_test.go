// The cleanup_archived switch over REST: a boolean on PUT /api/config, stored
// only when sent, and a type check on PUT /api/autonomous/config.

package server

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func TestCleanupArchivedParity_REST_ConfigPatch(t *testing.T) {
	cfg := &config.Config{}
	if !cfg.Autonomous.Automation.EffectiveCleanupArchived() {
		t.Fatal("an unset key must read true")
	}
	applyConfigPatch(cfg, map[string]interface{}{"autonomous.automation.cleanup_archived": false})
	if v := cfg.Autonomous.Automation.CleanupArchived; v == nil || *v {
		t.Fatalf("explicit false not stored: %v", v)
	}
	applyConfigPatch(cfg, map[string]interface{}{"autonomous.automation.cleanup_archived": "yes"})
	if cfg.Autonomous.Automation.EffectiveCleanupArchived() {
		t.Fatal("a non-boolean changed the stored value")
	}

	s := &Server{cfg: &config.Config{}, cfgPath: filepath.Join(t.TempDir(), "config.yaml")}
	rr := httptest.NewRecorder()
	s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config",
		strings.NewReader(`{"autonomous.automation.cleanup_archived":"yes"}`)))
	if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "cleanup_archived: must be true or false") {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
}

func TestCleanupArchivedParity_REST_AutonomousConfig(t *testing.T) {
	spy := &cleanupSpy{}
	s := &Server{autonomousMgr: spy, cfg: &config.Config{}}
	rr := httptest.NewRecorder()
	s.handleAutonomousConfig(rr, httptest.NewRequest(http.MethodPut, "/api/autonomous/config",
		strings.NewReader(`{"automation":{"cleanup_archived":"no"}}`)))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("a string was accepted: %d %s", rr.Code, rr.Body.String())
	}
	rr = httptest.NewRecorder()
	s.handleAutonomousConfig(rr, httptest.NewRequest(http.MethodPut, "/api/autonomous/config",
		strings.NewReader(`{"automation":{"cleanup_archived":false}}`)))
	if rr.Code >= 300 {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	auto, _ := spy.state["automation"].(map[string]any)
	if v, ok := auto["cleanup_archived"].(bool); !ok || v {
		t.Fatalf("automation = %v", auto)
	}
}
