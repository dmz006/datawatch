// Every usage.calculator key must be settable and readable on each server-side
// config surface a unit test can reach: PUT and GET /api/config, the chat
// `configure` command and the generic config_set tool, which both funnel into
// PUT /api/config. Invalid values are a 400 and change nothing.

package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/router"
)

const usageCalcPrefix = "usage.calculator."

// usageCalcGet returns usage.calculator from GET /api/config.
func usageCalcGet(t *testing.T, s *Server) map[string]any {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/config = %d", rr.Code)
	}
	var top struct {
		Usage struct {
			Calculator map[string]any `json:"calculator"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &top); err != nil {
		t.Fatalf("decode GET /api/config: %v", err)
	}
	if top.Usage.Calculator == nil {
		t.Fatalf("GET /api/config has no usage.calculator block")
	}
	return top.Usage.Calculator
}

func usageCalcPutRaw(s *Server, body []byte) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(body)))
	return rr
}

func usageCalcRows() []struct {
	field string
	value any
	want  any
} {
	return []struct {
		field string
		value any
		want  any
	}{
		{"variant", "lane-planner", "lane-planner"},
		{"variant", "burn-ledger", "burn-ledger"},
		{"variant", "pace-gauge", "pace-gauge"},
		{"variant", "all", "all"},
		{"policy", "finish-early", "finish-early"},
		{"policy", "pace", "pace"},
		{"safety_points", 0, float64(0)},
		{"safety_points", 12.5, 12.5},
		{"safety_points", 50, float64(50)},
		{"cost_per_task_points", 0.25, 0.25},
		{"baseline_per_24h", 0, float64(0)},
		{"baseline_per_24h", 20, float64(20)},
		{"max_lanes", 1, float64(1)},
		{"max_lanes", 8, float64(8)},
		{"reset_weekday", "friday", "friday"},
		{"reset_weekday", "Saturday", "saturday"},
		{"reset_time", "19:30", "19:30"},
		{"reset_zone", "America/New_York", "America/New_York"},
		{"keeper", "gpu-box", "gpu-box"},
		{"keeper", "", ""},
	}
}

func TestUsageCalcConfigDefaultsOnGet(t *testing.T) {
	s, _, _ := newMaxConcurrentTasksServer(t)
	got := usageCalcGet(t, s)
	want := map[string]any{
		"variant": "all", "policy": "use-all", "safety_points": float64(5), "cost_per_task_points": 0.1,
		"baseline_per_24h": float64(3), "max_lanes": float64(3), "reset_weekday": "sunday",
		"reset_time": "08:00", "reset_zone": "UTC", "keeper": "",
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("default %s%s = %#v, want %#v", usageCalcPrefix, k, got[k], w)
		}
	}
}

func TestUsageCalcConfigPutEveryKey(t *testing.T) {
	for _, row := range usageCalcRows() {
		key := usageCalcPrefix + row.field
		t.Run(fmt.Sprintf("%s=%v", row.field, row.value), func(t *testing.T) {
			s, _, cfgPath := newMaxConcurrentTasksServer(t)
			putJSON(t, s.handlePutConfig, "/api/config", map[string]any{key: row.value})
			if got := usageCalcGet(t, s)[row.field]; got != row.want {
				t.Fatalf("GET after PUT %s = %#v, want %#v", key, got, row.want)
			}
			raw, err := os.ReadFile(cfgPath)
			if err != nil {
				t.Fatalf("config not saved: %v", err)
			}
			if !strings.Contains(string(raw), "usage:") || !strings.Contains(string(raw), "calculator:") {
				t.Fatalf("config.yaml lacks the usage.calculator block:\n%s", raw)
			}
		})
	}
}

func TestUsageCalcConfigPutAllKeysTogetherKeepsOthers(t *testing.T) {
	s, _, _ := newMaxConcurrentTasksServer(t)
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{
		usageCalcPrefix + "variant": "burn-ledger", usageCalcPrefix + "max_lanes": 6, usageCalcPrefix + "reset_zone": "Europe/London",
	})
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{usageCalcPrefix + "policy": "pace"})
	got := usageCalcGet(t, s)
	if got["variant"] != "burn-ledger" || got["max_lanes"] != float64(6) || got["reset_zone"] != "Europe/London" || got["policy"] != "pace" {
		t.Fatalf("settings not kept across PUTs: %#v", got)
	}
	if got["safety_points"] != float64(5) {
		t.Fatalf("untouched key changed: %#v", got["safety_points"])
	}
}

func TestUsageCalcConfigRejectsInvalidValues(t *testing.T) {
	bad := []struct {
		field string
		value any
	}{
		{"variant", "radar-dial"}, {"variant", true}, {"variant", 3},
		{"policy", "burn"}, {"policy", false},
		{"safety_points", -1}, {"safety_points", 51}, {"safety_points", "lots"}, {"safety_points", true},
		{"cost_per_task_points", 0}, {"cost_per_task_points", -0.5}, {"cost_per_task_points", "free"},
		{"baseline_per_24h", -1}, {"baseline_per_24h", 20.5}, {"baseline_per_24h", []int{1}},
		{"max_lanes", 0}, {"max_lanes", 9}, {"max_lanes", 2.5}, {"max_lanes", "many"}, {"max_lanes", 1e12},
		{"reset_weekday", "someday"}, {"reset_weekday", 1},
		{"reset_time", "24:00"}, {"reset_time", "8:00"}, {"reset_time", "08:61"}, {"reset_time", 800},
		{"reset_zone", "Mars/Olympus"}, {"reset_zone", "+05:00"}, {"reset_zone", "UTC+5"}, {"reset_zone", "Etc/GMT-3"}, {"reset_zone", "Local"}, {"reset_zone", 5},
		{"keeper", "has space"}, {"keeper", true},
	}
	s, _, _ := newMaxConcurrentTasksServer(t)
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{usageCalcPrefix + "variant": "lane-planner"})
	before := usageCalcGet(t, s)
	for _, b := range bad {
		body, _ := json.Marshal(map[string]any{usageCalcPrefix + b.field: b.value})
		if rr := usageCalcPutRaw(s, body); rr.Code != http.StatusBadRequest {
			t.Errorf("PUT %s: want 400, got %d body=%s", body, rr.Code, rr.Body.String())
		}
	}
	after := usageCalcGet(t, s)
	for k, v := range before {
		if after[k] != v {
			t.Errorf("rejected PUTs changed %s: %#v -> %#v", k, v, after[k])
		}
	}
}

func TestUsageCalcConfigInvalidKeyRejectsWholePut(t *testing.T) {
	s, _, _ := newMaxConcurrentTasksServer(t)
	body, _ := json.Marshal(map[string]any{usageCalcPrefix + "variant": "burn-ledger", usageCalcPrefix + "max_lanes": 99})
	if rr := usageCalcPutRaw(s, body); rr.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rr.Code)
	}
	if got := usageCalcGet(t, s)["variant"]; got != "all" {
		t.Fatalf("the valid key was applied despite the rejected PUT: variant = %#v", got)
	}
}

// TestUsageCalcConfigCommConfigure drives the chat `configure <key>=<value>`
// command, which passes the value to PUT /api/config as a number when it
// parses as one and as a string otherwise (the shape main.go uses).
func TestUsageCalcConfigCommConfigure(t *testing.T) {
	for _, row := range usageCalcRows() {
		if row.value == "" {
			continue // `configure key=` carries no value
		}
		key := usageCalcPrefix + row.field
		value := fmt.Sprint(row.value)
		t.Run(row.field+"="+value, func(t *testing.T) {
			s, _, _ := newMaxConcurrentTasksServer(t)
			r := router.NewRouter("h", "", nil, nil, 0, nil)
			r.SetConfigureFunc(func(k, v string) error {
				var val any = v
				var n json.Number
				if json.Unmarshal([]byte(v), &n) == nil {
					val = n
				}
				b, _ := json.Marshal(map[string]any{k: val})
				if rr := usageCalcPutRaw(s, b); rr.Code != http.StatusOK {
					return &configureErr{rr.Body.String()}
				}
				return nil
			})
			resp := r.HandleTestMessage("configure " + key + "=" + value)
			if len(resp) == 0 || !strings.Contains(resp[0], "Set "+key) {
				t.Fatalf("configure response = %v", resp)
			}
			if got := usageCalcGet(t, s)[row.field]; got != row.want {
				t.Fatalf("GET after configure = %#v, want %#v", got, row.want)
			}
		})
	}
	t.Run("invalid value is refused", func(t *testing.T) {
		s, _, _ := newMaxConcurrentTasksServer(t)
		r := router.NewRouter("h", "", nil, nil, 0, nil)
		r.SetConfigureFunc(func(k, v string) error {
			b, _ := json.Marshal(map[string]any{k: v})
			if rr := usageCalcPutRaw(s, b); rr.Code != http.StatusOK {
				return &configureErr{rr.Body.String()}
			}
			return nil
		})
		resp := r.HandleTestMessage("configure " + usageCalcPrefix + "variant=radar-dial")
		if len(resp) == 0 || strings.Contains(resp[0], "Set ") {
			t.Fatalf("configure of an invalid variant answered %v", resp)
		}
		if got := usageCalcGet(t, s)["variant"]; got != "all" {
			t.Fatalf("variant changed to %#v", got)
		}
	})
}

// usageCalcConfigSetTool sends one key and value the way the generic
// config_set MCP tool does (internal/mcp/memory_tools.go handleConfigSet): the
// value spliced raw into the JSON body, then, when that is rejected, quoted.
func usageCalcConfigSetTool(s *Server, key, value string) int {
	rr := usageCalcPutRaw(s, []byte(fmt.Sprintf(`{"%s": %s}`, key, value)))
	if rr.Code == http.StatusBadRequest {
		rr = usageCalcPutRaw(s, []byte(fmt.Sprintf(`{"%s": "%s"}`, key, value)))
	}
	return rr.Code
}

// TestUsageCalcConfigToolPath drives usage.calculator.variant (and the other
// keys) through the config_set tool's request shape. The tool is generic, so
// no key needs its own tool.
func TestUsageCalcConfigToolPath(t *testing.T) {
	s, _, _ := newMaxConcurrentTasksServer(t)
	for _, v := range []string{"pace-gauge", "lane-planner", "burn-ledger", "all"} {
		if code := usageCalcConfigSetTool(s, usageCalcPrefix+"variant", v); code != http.StatusOK {
			t.Fatalf("config_set variant=%s: HTTP %d", v, code)
		}
		if got := usageCalcGet(t, s)["variant"]; got != v {
			t.Fatalf("variant after config_set %s = %#v", v, got)
		}
	}
	if code := usageCalcConfigSetTool(s, usageCalcPrefix+"variant", "radar-dial"); code != http.StatusBadRequest {
		t.Fatalf("config_set of a retired variant: HTTP %d, want 400", code)
	}
	if got := usageCalcGet(t, s)["variant"]; got != "all" {
		t.Fatalf("variant after a refused config_set = %#v, want all", got)
	}
	for _, c := range []struct {
		key, value string
		want       any
	}{
		{"max_lanes", "5", float64(5)},
		{"cost_per_task_points", "0.4", 0.4},
		{"reset_time", "06:45", "06:45"},
		{"reset_zone", "Asia/Tokyo", "Asia/Tokyo"},
		{"policy", "finish-early", "finish-early"},
	} {
		if code := usageCalcConfigSetTool(s, usageCalcPrefix+c.key, c.value); code != http.StatusOK {
			t.Fatalf("config_set %s=%s: HTTP %d", c.key, c.value, code)
		}
		if got := usageCalcGet(t, s)[c.key]; got != c.want {
			t.Fatalf("%s after config_set = %#v, want %#v", c.key, got, c.want)
		}
	}
}
