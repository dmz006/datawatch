package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func TestPeerSilenceRESTGet(t *testing.T) {
	s := newTestServer(t, nil, nil)
	s.cfg = config.DefaultConfig()
	rr := httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d", rr.Code)
	}
	var body map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	obs, _ := body["observer"].(map[string]interface{})
	peers, _ := obs["peers"].(map[string]interface{})
	if peers["silent_alert_minutes"] != float64(5) {
		t.Fatalf("silent_alert_minutes=%v want 5", peers["silent_alert_minutes"])
	}
}

func TestPeerSilenceRESTSet(t *testing.T) {
	cfg := config.DefaultConfig()
	skipped := applyConfigPatch(cfg, map[string]interface{}{"observer.peers.silent_alert_minutes": float64(9)})
	if len(skipped) != 0 {
		t.Fatalf("skipped=%v", skipped)
	}
	if cfg.Observer.Peers.SilentAlertMinutes != 9 {
		t.Fatalf("got %d want 9", cfg.Observer.Peers.SilentAlertMinutes)
	}
	applyConfigPatch(cfg, map[string]interface{}{"observer.peers.silent_alert_minutes": float64(0)})
	if cfg.Observer.Peers.SilentAlertMinutes != 0 {
		t.Fatalf("zero not applied: %d", cfg.Observer.Peers.SilentAlertMinutes)
	}
}

func TestPeerSilenceRESTNegativeRejected(t *testing.T) {
	cfg := config.DefaultConfig()
	applyConfigPatch(cfg, map[string]interface{}{"observer.peers.silent_alert_minutes": float64(-3)})
	if cfg.Observer.Peers.SilentAlertMinutes != 5 {
		t.Fatalf("negative accepted: %d", cfg.Observer.Peers.SilentAlertMinutes)
	}
}
