package server

import (
	"encoding/json"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// mergePendingSurface reads the two merge-pending keys from GET /api/config.
func mergePendingSurface(t *testing.T, s *Server) (string, int) {
	t.Helper()
	var a struct {
		Mode    string `json:"merge_pending_mode"`
		Minutes *int   `json:"merge_pending_alert_minutes"`
	}
	if err := json.Unmarshal(getAutonomousConfigMap(t, s)["automation"], &a); err != nil {
		t.Fatalf("decode automation: %v", err)
	}
	if a.Minutes == nil {
		t.Fatal("GET /api/config dropped merge_pending_alert_minutes")
	}
	return a.Mode, *a.Minutes
}

func TestMergePendingConfigDefaultsAreOffAnd30(t *testing.T) {
	s, _, _ := newActivateServer(t, false)
	mode, minutes := mergePendingSurface(t, s)
	if mode != config.MergePendingModeOff || minutes != config.DefaultAutomationMergePendingAlertMinutes {
		t.Fatalf("GET defaults = (%q, %d), want (off, 30)", mode, minutes)
	}
}

func TestMergePendingConfigUnsetMinutesReads30(t *testing.T) {
	s, _, _ := newActivateServer(t, false)
	s.cfg.Autonomous.Automation.MergePendingMode = ""
	s.cfg.Autonomous.Automation.MergePendingAlertMinutes = nil
	mode, minutes := mergePendingSurface(t, s)
	if mode != "off" || minutes != 30 {
		t.Fatalf("unset keys read (%q, %d), want (off, 30)", mode, minutes)
	}
}

func TestMergePendingConfigExplicitZeroSurvives(t *testing.T) {
	s, _, _ := newActivateServer(t, false)
	pcPutOK(t, s.handlePutConfig, "/api/config", map[string]any{"autonomous.automation.merge_pending_alert_minutes": 0})
	if _, minutes := mergePendingSurface(t, s); minutes != 0 {
		t.Fatalf("after PUT 0 /api/config: %d, want 0", minutes)
	}
	// And on the dedicated autonomous surface, after a non-zero value.
	pcPutOK(t, s.handleAutonomousConfig, "/api/autonomous/config", map[string]any{"automation": map[string]any{"merge_pending_alert_minutes": 90}})
	if _, minutes := mergePendingSurface(t, s); minutes != 90 {
		t.Fatalf("after autonomous PUT 90: %d, want 90", minutes)
	}
	pcPutOK(t, s.handleAutonomousConfig, "/api/autonomous/config", map[string]any{"automation": map[string]any{"merge_pending_alert_minutes": 0}})
	if _, minutes := mergePendingSurface(t, s); minutes != 0 {
		t.Fatalf("after autonomous PUT 0: %d, want 0", minutes)
	}
}

func TestMergePendingConfigRejectsUnknownMode(t *testing.T) {
	s, _, _ := newActivateServer(t, false)
	for _, bad := range []any{"enabled", "ON", "", 1, true} {
		pcPutBad(t, s.handlePutConfig, "/api/config", map[string]any{"autonomous.automation.merge_pending_mode": bad})
	}
	pcPutBad(t, s.handleAutonomousConfig, "/api/autonomous/config", map[string]any{"automation": map[string]any{"merge_pending_mode": "sometimes"}})
	if mode, _ := mergePendingSurface(t, s); mode != "off" {
		t.Fatalf("rejected PUT changed the mode to %q", mode)
	}
}

func TestMergePendingConfigRejectsNegativeMinutes(t *testing.T) {
	s, _, _ := newActivateServer(t, false)
	for _, bad := range []any{-1, "soon", 1.5} {
		pcPutBad(t, s.handlePutConfig, "/api/config", map[string]any{"autonomous.automation.merge_pending_alert_minutes": bad})
	}
	pcPutBad(t, s.handleAutonomousConfig, "/api/autonomous/config", map[string]any{"automation": map[string]any{"merge_pending_alert_minutes": -5}})
	if _, minutes := mergePendingSurface(t, s); minutes != 30 {
		t.Fatalf("rejected PUT changed the minutes to %d", minutes)
	}
}

func TestMergePendingConfigPutThenGetRoundTrip(t *testing.T) {
	s, mgr, _ := newActivateServer(t, false)
	pcPutOK(t, s.handlePutConfig, "/api/config", map[string]any{
		"autonomous.automation.merge_pending_mode":          "shadow",
		"autonomous.automation.merge_pending_alert_minutes": 45,
	})
	if mode, minutes := mergePendingSurface(t, s); mode != "shadow" || minutes != 45 {
		t.Fatalf("GET after PUT = (%q, %d), want (shadow, 45)", mode, minutes)
	}
	pcPutOK(t, s.handlePutConfig, "/api/config", map[string]any{"autonomous.automation.merge_pending_mode": "on"})
	if mode, minutes := mergePendingSurface(t, s); mode != "on" || minutes != 45 {
		t.Fatalf("GET after mode-only PUT = (%q, %d), want (on, 45)", mode, minutes)
	}
	// The live manager reads the same values the config surface reports.
	if got := mgr.Config().Automation.EffectiveMergePendingMode(); got != "on" {
		t.Fatalf("live manager merge_pending_mode = %q, want on", got)
	}
	if got := mgr.Config().Automation.EffectiveMergePendingAlertMinutes(); got != 45 {
		t.Fatalf("live manager merge_pending_alert_minutes = %d, want 45", got)
	}
}

// ConfigureKeysMapped: the comm `configure` command sends dotted keys to
// PUT /api/config and the MCP autonomous_config_set tool sends a nested
// automation object to PUT /api/autonomous/config. Both must land in the same
// stored values, which GET /api/config then reports.
func TestMergePendingConfigConfigureKeysMapped(t *testing.T) {
	comm, _, _ := newActivateServer(t, false)
	pcPutOK(t, comm.handlePutConfig, "/api/config", map[string]any{
		"autonomous.automation.merge_pending_mode":          "shadow",
		"autonomous.automation.merge_pending_alert_minutes": 0,
	})
	mcp, _, _ := newActivateServer(t, false)
	pcPutOK(t, mcp.handleAutonomousConfig, "/api/autonomous/config", map[string]any{"automation": map[string]any{
		"merge_pending_mode":          "shadow",
		"merge_pending_alert_minutes": 0,
	}})
	cm, cn := mergePendingSurface(t, comm)
	mm, mn := mergePendingSurface(t, mcp)
	if cm != "shadow" || cn != 0 || mm != cm || mn != cn {
		t.Fatalf("comm key path = (%q, %d), MCP key path = (%q, %d); want both (shadow, 0)", cm, cn, mm, mn)
	}
	if comm.cfg.Autonomous.Automation.MergePendingAlertMinutes == nil || mcp.cfg.Autonomous.Automation.MergePendingAlertMinutes == nil {
		t.Fatal("an explicit 0 was stored as unset on one of the paths")
	}
}
