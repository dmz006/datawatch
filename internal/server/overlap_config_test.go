// Search-before-create configuration keys — GET returns both keys with
// their effective defaults, PUT accepts off|shadow|on and a threshold in
// (0, 1], and PUT refuses a bogus mode and a zero threshold with a 400.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func putGetOver(t *testing.T, patch map[string]any) (int, map[string]interface{}, string) {
	t.Helper()
	s := &Server{
		cfg:           &config.Config{},
		cfgPath:       t.TempDir() + "/config.yaml",
		autonomousMgr: &autonomousConfigPropagationSpy{},
	}
	if patch != nil {
		b, _ := json.Marshal(patch)
		rr := httptest.NewRecorder()
		s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(b)))
		return rr.Code, nil, rr.Body.String()
	}
	rr := httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	var body map[string]interface{}
	if rr.Code == http.StatusOK {
		_ = json.Unmarshal(rr.Body.Bytes(), &body)
	}
	return rr.Code, body, rr.Body.String()
}

func TestOverlapConfigGetReturnsDefaults(t *testing.T) {
	_, body, raw := putGetOver(t, nil)
	if body == nil {
		t.Fatalf("GET: want a JSON body, got empty (raw=%s)", raw)
	}
	auto, ok := body["autonomous"].(map[string]interface{})["automation"].(map[string]interface{})
	if !ok {
		t.Fatalf("GET body is missing autonomous.automation: %v", body)
	}
	if got := auto["create_search_mode"]; got != "shadow" {
		t.Errorf("create_search_mode = %v, want shadow", got)
	}
	if got, isF := auto["create_search_threshold"].(float64); !isF || got != 0.5 {
		t.Errorf("create_search_threshold = %v, want 0.5", auto["create_search_threshold"])
	}
}

func TestOverlapConfigPutsAcceptAndRoundTrip(t *testing.T) {
	s := &Server{
		cfg:           &config.Config{},
		cfgPath:       t.TempDir() + "/config.yaml",
		autonomousMgr: &autonomousConfigPropagationSpy{},
	}
	for _, patch := range []map[string]any{
		{"autonomous.automation.create_search_mode": "off", "autonomous.automation.create_search_threshold": 0.9},
		{"autonomous.automation.create_search_mode": "on", "autonomous.automation.create_search_threshold": 0.1},
	} {
		b, _ := json.Marshal(patch)
		rr := httptest.NewRecorder()
		s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(b)))
		if rr.Code != http.StatusOK {
			t.Fatalf("PUT %v: want 200, got %d body=%s", patch, rr.Code, rr.Body.String())
		}
	}

	rr := httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET: want 200, got %d", rr.Code)
	}
	var got struct {
		Autonomous struct {
			Automation struct {
				Mode      string  `json:"create_search_mode"`
				Threshold float64 `json:"create_search_threshold"`
			} `json:"automation"`
		} `json:"autonomous"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	if got.Autonomous.Automation.Mode != "on" || got.Autonomous.Automation.Threshold != 0.1 {
		t.Errorf("GET mode/threshold = %q/%v, want on/0.1", got.Autonomous.Automation.Mode, got.Autonomous.Automation.Threshold)
	}
}

func TestOverlapConfigPutRefusesBogusValues(t *testing.T) {
	for _, patch := range []map[string]any{
		{"autonomous.automation.create_search_mode": "bogus"},
		{"autonomous.automation.create_search_mode": "Shadow"},
		{"autonomous.automation.create_search_threshold": 0},
		{"autonomous.automation.create_search_threshold": -0.1},
		{"autonomous.automation.create_search_threshold": 1.5},
	} {
		s := &Server{
			cfg:           &config.Config{},
			cfgPath:       t.TempDir() + "/config.yaml",
			autonomousMgr: &autonomousConfigPropagationSpy{},
		}
		b, _ := json.Marshal(patch)
		rr := httptest.NewRecorder()
		s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(b)))
		if rr.Code != http.StatusBadRequest {
			t.Errorf("PUT %v: want 400, got %d body=%s", patch, rr.Code, rr.Body.String())
		}
	}
}

// A mixed patch where only the new key is bad still rejects the whole PUT
// and leaves the other keys unapplied.
func TestOverlapConfigPutRejectsWholePatchOnOneBadKey(t *testing.T) {
	s := &Server{
		cfg:           &config.Config{},
		cfgPath:       t.TempDir() + "/config.yaml",
		autonomousMgr: &autonomousConfigPropagationSpy{},
	}
	b, _ := json.Marshal(map[string]any{
		"autonomous.automation.idle_alert_minutes": 20,
		"autonomous.automation.create_search_mode": "bogus",
	})
	rr := httptest.NewRecorder()
	s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(b)))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rr.Code)
	}
	if s.cfg.Autonomous.Automation.IdleAlertMinutes == 20 {
		t.Errorf("rejected PUT must not apply idle_alert_minutes either")
	}
}
