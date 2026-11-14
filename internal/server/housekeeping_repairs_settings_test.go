package server

// Tests for the autonomous.housekeeping_repairs block on the REST config
// surface: the read, the write, the shared refusal text, and the applier.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func newHousekeepingSettingsServer(t *testing.T) (*Server, string) {
	t.Helper()
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	return &Server{cfg: config.DefaultConfig(), cfgPath: cfgPath}, cfgPath
}

func putHousekeepingBody(t *testing.T, s *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(body)))
	return rr
}

func getHousekeepingSettings(t *testing.T, s *Server) map[string]interface{} {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/config: %d", rr.Code)
	}
	var top struct {
		Autonomous struct {
			HousekeepingRepairs map[string]interface{} `json:"housekeeping_repairs"`
		} `json:"autonomous"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &top); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	return top.Autonomous.HousekeepingRepairs
}

func TestHousekeepingSettings_DefaultsOnGet(t *testing.T) {
	s, _ := newHousekeepingSettingsServer(t)
	want := map[string]interface{}{
		"task_chain": "report", "advance_judge": "report", "lane_fill": "report", "leftovers": "report", "planning": "report", "unarchive": "report",
		"grace_seconds": float64(300),
	}
	if got := getHousekeepingSettings(t, s); !reflect.DeepEqual(got, want) {
		t.Fatalf("GET autonomous.housekeeping_repairs = %v, want %v", got, want)
	}
}

func TestHousekeepingSettings_PutThenGetEachGroup(t *testing.T) {
	s, _ := newHousekeepingSettingsServer(t)
	for _, body := range []string{
		`{"autonomous.housekeeping_repairs.task_chain":"on"}`,
		`{"autonomous.housekeeping_repairs.advance_judge":"off"}`,
		`{"autonomous.housekeeping_repairs.lane_fill":"ON"}`, // comm sends text as typed
		`{"autonomous.housekeeping_repairs.leftovers":"report"}`,
		`{"autonomous.housekeeping_repairs.unarchive":"on"}`,
		`{"autonomous.housekeeping_repairs.grace_seconds":"45"}`,
	} {
		if rr := putHousekeepingBody(t, s, body); rr.Code != http.StatusOK {
			t.Fatalf("PUT %s: %d %s", body, rr.Code, rr.Body.String())
		}
	}
	got := getHousekeepingSettings(t, s)
	want := map[string]interface{}{
		"task_chain": "on", "advance_judge": "off", "lane_fill": "on", "leftovers": "report", "planning": "report", "unarchive": "on",
		"grace_seconds": float64(45),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip = %v, want %v", got, want)
	}
}

func TestHousekeepingSettings_InvalidRefusedWithSharedMessage(t *testing.T) {
	cases := []struct {
		key   string
		value interface{}
		body  string
	}{
		{"autonomous.housekeeping_repairs.task_chain", "sometimes", `{"autonomous.housekeeping_repairs.task_chain":"sometimes"}`},
		{"autonomous.housekeeping_repairs.lane_fill", true, `{"autonomous.housekeeping_repairs.lane_fill":true}`},
		{"autonomous.housekeeping_repairs.grace_seconds", float64(-1), `{"autonomous.housekeeping_repairs.grace_seconds":-1}`},
		{"autonomous.housekeeping_repairs.bogus", "on", `{"autonomous.housekeeping_repairs.bogus":"on"}`},
	}
	for _, tc := range cases {
		s, _ := newHousekeepingSettingsServer(t)
		before := s.cfg.Autonomous.HousekeepingRepairs
		rr := putHousekeepingBody(t, s, tc.body)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("%s: want 400, got %d", tc.key, rr.Code)
		}
		want := config.ValidateHousekeepingRepairsSetting(tc.key, tc.value)
		if want == nil {
			t.Fatalf("validator accepted %v", tc.value)
		}
		if got := strings.TrimSpace(rr.Body.String()); got != want.Error() {
			t.Fatalf("message = %q, want %q", got, want.Error())
		}
		if !reflect.DeepEqual(s.cfg.Autonomous.HousekeepingRepairs, before) {
			t.Fatalf("a refused PUT changed the block")
		}
	}
}

func TestHousekeepingSettings_RejectedPutChangesNothing(t *testing.T) {
	s, _ := newHousekeepingSettingsServer(t)
	rr := putHousekeepingBody(t, s, `{"autonomous.housekeeping_repairs.task_chain":"on","autonomous.housekeeping_repairs.leftovers":"nope"}`)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rr.Code)
	}
	if got := getHousekeepingSettings(t, s)["task_chain"]; got != "report" {
		t.Fatalf("a refused PUT still applied task_chain = %v", got)
	}
}

func TestHousekeepingSettings_SurvivesSaveAndLoad(t *testing.T) {
	s, cfgPath := newHousekeepingSettingsServer(t)
	if rr := putHousekeepingBody(t, s, `{"autonomous.housekeeping_repairs.leftovers":"off","autonomous.housekeeping_repairs.grace_seconds":0}`); rr.Code != http.StatusOK {
		t.Fatalf("PUT: %d %s", rr.Code, rr.Body.String())
	}
	if _, err := os.ReadFile(cfgPath); err != nil {
		t.Fatalf("read config.yaml: %v", err)
	}
	back, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	h := back.Autonomous.HousekeepingRepairs
	if h.Leftovers != "off" || h.GraceSecondsOrDefault() != 0 {
		t.Fatalf("reload lost values: %+v", h)
	}
}

func TestHousekeepingSettings_ApplyConfigPatch(t *testing.T) {
	cfg := config.DefaultConfig()
	applyConfigPatch(cfg, map[string]interface{}{
		"autonomous.housekeeping_repairs.advance_judge": "on",
		"autonomous.housekeeping_repairs.grace_seconds": float64(12),
	})
	h := cfg.Autonomous.HousekeepingRepairs
	if h.AdvanceJudge != "on" || h.GraceSecondsOrDefault() != 12 {
		t.Fatalf("applier did not set the block: %+v", h)
	}
	// The applier never stores an invalid value.
	applyConfigPatch(cfg, map[string]interface{}{"autonomous.housekeeping_repairs.advance_judge": "bad"})
	if cfg.Autonomous.HousekeepingRepairs.AdvanceJudge != "on" {
		t.Fatalf("applier stored an invalid mode")
	}
}

func TestHousekeepingSettings_MCPConfigSetDescriptionNamesTheKeys(t *testing.T) {
	readRel(t, "internal/mcp/memory_tools.go",
		"autonomous.housekeeping_repairs.task_chain",
		"autonomous.housekeeping_repairs.advance_judge",
		"autonomous.housekeeping_repairs.lane_fill",
		"autonomous.housekeeping_repairs.leftovers",
		"autonomous.housekeeping_repairs.planning",
		"autonomous.housekeeping_repairs.grace_seconds",
	)
}
