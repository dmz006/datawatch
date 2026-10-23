// Every autonomous.automation key must be settable and readable on each
// server-side config surface a unit test can reach: GET and PUT /api/config,
// PUT /api/autonomous/config, and the chat `configure` command. One row per key.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/router"
)

func automationConfigField(t *testing.T, s *Server, field string) string {
	t.Helper()
	var auto map[string]json.RawMessage
	raw, ok := getAutonomousConfigMap(t, s)["automation"]
	if !ok {
		t.Fatalf("GET /api/config autonomous lacks automation")
	}
	if err := json.Unmarshal(raw, &auto); err != nil {
		t.Fatalf("decode automation: %v", err)
	}
	return strings.Trim(string(auto[field]), `"`)
}

func TestAutomationConfigKeysEverySurface(t *testing.T) {
	rows := []struct {
		field   string
		value   any
		want    string
		invalid any // rejected by every surface; nil when any value is accepted
	}{
		{"idle_alert_minutes", 45, "45", nil},
		{"agent_insert_policy", "require_operator", "require_operator", "sometimes"},
	}
	for _, row := range rows {
		key := "autonomous.automation." + row.field
		t.Run(row.field+"/rest_config", func(t *testing.T) {
			s, mgr, _ := newMaxConcurrentTasksServer(t)
			putJSON(t, s.handlePutConfig, "/api/config", map[string]any{key: row.value})
			if got := automationConfigField(t, s, row.field); got != row.want {
				t.Fatalf("GET after PUT = %q, want %q", got, row.want)
			}
			live := mgr.Config().Automation
			if row.field == "idle_alert_minutes" && live.IdleAlertMinutes != 45 {
				t.Fatalf("live manager IdleAlertMinutes = %d", live.IdleAlertMinutes)
			}
			if row.field == "agent_insert_policy" && live.AgentInsertPolicy != row.want {
				t.Fatalf("live manager AgentInsertPolicy = %q", live.AgentInsertPolicy)
			}
			if row.invalid != nil {
				b, _ := json.Marshal(map[string]any{key: row.invalid})
				rr := httptest.NewRecorder()
				s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(b)))
				if rr.Code != http.StatusBadRequest {
					t.Fatalf("invalid %v: want 400, got %d", row.invalid, rr.Code)
				}
				if got := automationConfigField(t, s, row.field); got != row.want {
					t.Fatalf("value after rejected PUT = %q, want %q", got, row.want)
				}
			}
		})
		t.Run(row.field+"/rest_autonomous_config", func(t *testing.T) {
			s, mgr, _ := newMaxConcurrentTasksServer(t)
			putJSON(t, s.handleAutonomousConfig, "/api/autonomous/config",
				map[string]any{"automation": map[string]any{row.field: row.value}})
			live := mgr.Config().Automation
			if row.field == "idle_alert_minutes" && live.IdleAlertMinutes != 45 {
				t.Fatalf("live manager IdleAlertMinutes = %d", live.IdleAlertMinutes)
			}
			if row.field == "agent_insert_policy" && live.AgentInsertPolicy != row.want {
				t.Fatalf("live manager AgentInsertPolicy = %q", live.AgentInsertPolicy)
			}
			if row.invalid != nil {
				b, _ := json.Marshal(map[string]any{"automation": map[string]any{row.field: row.invalid}})
				rr := httptest.NewRecorder()
				s.handleAutonomousConfig(rr, httptest.NewRequest(http.MethodPut, "/api/autonomous/config", bytes.NewReader(b)))
				if rr.Code != http.StatusBadRequest {
					t.Fatalf("invalid %v: want 400, got %d", row.invalid, rr.Code)
				}
			}
		})
		t.Run(row.field+"/comm_configure", func(t *testing.T) {
			s, _, _ := newMaxConcurrentTasksServer(t)
			r := router.NewRouter("h", "", nil, nil, 0, nil)
			r.SetConfigureFunc(func(k, v string) error {
				var val any = v
				var n json.Number
				if json.Unmarshal([]byte(v), &n) == nil {
					val = n
				}
				b, _ := json.Marshal(map[string]any{k: val})
				rr := httptest.NewRecorder()
				s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(b)))
				if rr.Code != http.StatusOK {
					return &configureErr{rr.Body.String()}
				}
				return nil
			})
			resp := r.HandleTestMessage("configure " + key + "=" + row.want)
			if len(resp) == 0 || !strings.Contains(resp[0], "Set "+key) {
				t.Fatalf("configure response = %v", resp)
			}
			if got := automationConfigField(t, s, row.field); got != row.want {
				t.Fatalf("GET after configure = %q, want %q", got, row.want)
			}
		})
	}
}

type configureErr struct{ msg string }

func (e *configureErr) Error() string { return e.msg }
