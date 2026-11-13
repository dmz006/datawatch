package main

// `datawatch config set` and `config get` for the autonomous.housekeeping_repairs
// keys: a value is sent to the daemon and read back through GET /api/config, a
// refused value returns an error naming the key, and the stored value is left
// unchanged. The fake daemon applies the same shared validator the real
// PUT /api/config handler runs.

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

type housekeepingFakeDaemon struct {
	mu  sync.Mutex
	cfg config.HousekeepingRepairsConfig
}

func (d *housekeepingFakeDaemon) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	d.mu.Lock()
	defer d.mu.Unlock()
	switch r.Method {
	case http.MethodGet:
		e := d.cfg.Effective()
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"autonomous": map[string]interface{}{"housekeeping_repairs": map[string]interface{}{
			"task_chain": e.TaskChain, "advance_judge": e.AdvanceJudge, "lane_fill": e.LaneFill, "leftovers": e.Leftovers,
			"grace_seconds": e.GraceSecondsOrDefault(),
		}}})
	case http.MethodPut:
		raw, _ := io.ReadAll(r.Body)
		var patch map[string]interface{}
		if err := json.Unmarshal(raw, &patch); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		for k, v := range patch {
			if strings.HasPrefix(k, config.HousekeepingRepairsSettingPrefix) {
				if err := config.ValidateHousekeepingRepairsSetting(k, v); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
			}
		}
		for k, v := range patch {
			if f, ok := strings.CutPrefix(k, config.HousekeepingRepairsSettingPrefix); ok {
				d.cfg, _ = config.SetHousekeepingRepairsField(d.cfg, f, v)
			}
		}
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func TestHousekeepingRepairsConfigSetThenGetReadsBack(t *testing.T) {
	daemon := &housekeepingFakeDaemon{}
	srv := httptest.NewServer(daemon)
	defer srv.Close()
	old := serverURL
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = old })

	cfg := &config.Config{}
	sets := map[string]string{
		"autonomous.housekeeping_repairs.task_chain":    "on",
		"autonomous.housekeeping_repairs.advance_judge": "off",
		"autonomous.housekeeping_repairs.lane_fill":     "report",
		"autonomous.housekeeping_repairs.leftovers":     "on",
		"autonomous.housekeeping_repairs.grace_seconds": "60",
	}
	for k, v := range sets {
		if err := runConfigSet(cfg, k, v); err != nil {
			t.Fatalf("runConfigSet(%s, %s): %v", k, v, err)
		}
	}
	for k, v := range sets {
		var getErr error
		out := captureProjectsDefaultsStdout(t, func() { getErr = runConfigGet(cfg, k) })
		if getErr != nil {
			t.Fatalf("runConfigGet(%s): %v", k, getErr)
		}
		if !strings.Contains(out, v) {
			t.Errorf("runConfigGet(%s) printed %q, want %q", k, out, v)
		}
	}
}

func TestHousekeepingRepairsConfigSetRefusesInvalidValues(t *testing.T) {
	daemon := &housekeepingFakeDaemon{cfg: config.HousekeepingRepairsConfig{TaskChain: "off"}}
	srv := httptest.NewServer(daemon)
	defer srv.Close()
	old := serverURL
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = old })

	for _, tc := range []struct{ key, value string }{
		{"autonomous.housekeeping_repairs.task_chain", "sometimes"},
		{"autonomous.housekeeping_repairs.grace_seconds", "-1"},
		{"autonomous.housekeeping_repairs.nope", "on"},
	} {
		err := runConfigSet(&config.Config{}, tc.key, tc.value)
		if err == nil {
			t.Fatalf("runConfigSet(%s, %s) = nil, want refusal", tc.key, tc.value)
		}
		if !strings.Contains(err.Error(), tc.key) {
			t.Errorf("refusal %q does not name %s", err.Error(), tc.key)
		}
	}
	if daemon.cfg.TaskChain != "off" || daemon.cfg.GraceSeconds != nil {
		t.Errorf("refused sets changed the stored block: %+v", daemon.cfg)
	}
}
