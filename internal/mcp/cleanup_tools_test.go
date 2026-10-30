package mcp

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestCleanupMCP_ToolRegistered(t *testing.T) {
	have := map[string]bool{}
	for _, n := range mergeTestToolNames(t) {
		have[n] = true
	}
	for _, want := range CleanupToolNames() {
		if !have[want] {
			t.Errorf("tool %q is not registered", want)
		}
	}
}

func TestCleanupMCP_StatusReadsRoute(t *testing.T) {
	var path, method, rawQuery string
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		path, method, rawQuery = r.URL.Path, r.Method, r.URL.RawQuery
		_, _ = w.Write([]byte(`{"worktrees":{"mode":"report","removed":3,"net_removed":2,"bytes_freed":2097152},` +
			`"temp":{},"worktree_disk":{"path":"/work","percent":42},"disk_alert_percent":85,"disk_alert":false}`))
	})
	res, err := s.handleAutonomousCleanupStatus(context.Background(), callReq(nil))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if method != http.MethodGet || path != "/api/autonomous/cleanup" || rawQuery != "" {
		t.Errorf("proxied %s %s?%s", method, path, rawQuery)
	}
	if text := automationText(t, res); !strings.Contains(text, `"net_removed":2`) {
		t.Errorf("result lost the daemon body: %s", text)
	}
}

func TestCleanupMCP_ReadOnlyOnlyOneTool(t *testing.T) {
	s := &Server{}
	tool := s.toolAutonomousCleanupStatus()
	if tool.Name != "autonomous_cleanup_status" {
		t.Errorf("tool name = %q", tool.Name)
	}
	if hint := tool.Annotations.ReadOnlyHint; hint == nil || !*hint {
		t.Error("autonomous_cleanup_status must be marked read-only")
	}
	// Only the one read exists: no tool that removes worktrees or files is
	// registered.
	if len(CleanupToolNames()) != 1 {
		t.Errorf("want exactly the one read tool, got %v", CleanupToolNames())
	}
	known := map[string]bool{}
	for _, n := range CleanupToolNames() {
		known[n] = true
	}
	for _, n := range mergeTestToolNames(t) {
		if known[n] {
			continue
		}
		if strings.HasPrefix(n, "autonomous_cleanup_delete") || strings.HasPrefix(n, "autonomous_cleanup_run") {
			t.Errorf("unexpected write-style cleanup tool %q", n)
		}
	}
}

// The autonomous_config_set tool declares the four cleanup arguments and
// forwards them, presence-aware, into the nested automation object that
// PUT /api/autonomous/config reads.
func TestCleanupConfigMCPForwardsKeys(t *testing.T) {
	s, rec := activateServer(t, http.StatusOK, `{}`)
	names := configSetArgNames()
	for _, want := range []string{"automation_cleanup_mode", "automation_cleanup_retention_hours",
		"automation_cleanup_max_removals_per_pass", "automation_disk_alert_percent"} {
		if !strings.Contains(names, want) {
			t.Errorf("autonomous_config_set does not declare %s", want)
		}
	}
	// An explicit 0 (alert off / keep defaults) is sent, not dropped.
	if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{
		"automation_cleanup_mode":                "on",
		"automation_cleanup_retention_hours":     0,
		"automation_cleanup_max_removals_per_pass": 50,
		"automation_disk_alert_percent":          90,
	})); err != nil {
		t.Fatalf("config set: %v", err)
	}
	auto, _ := rec.body["automation"].(map[string]any)
	if auto["cleanup_mode"] != "on" {
		t.Errorf("automation = %v, want cleanup_mode on", rec.body["automation"])
	}
	if got, ok := auto["cleanup_retention_hours"].(float64); !ok || got != 0 {
		t.Errorf("automation = %v, want cleanup_retention_hours 0", rec.body["automation"])
	}
	if got, ok := auto["cleanup_max_removals_per_pass"].(float64); !ok || got != 50 {
		t.Errorf("automation = %v, want cleanup_max_removals_per_pass 50", rec.body["automation"])
	}
	if got, ok := auto["disk_alert_percent"].(float64); !ok || got != 90 {
		t.Errorf("automation = %v, want disk_alert_percent 90", rec.body["automation"])
	}
	// Absent arguments send no cleanup keys.
	if _, err := s.handleAutonomousConfigSet(context.Background(), callReq(map[string]any{
		"automation_idle_alert_minutes": 0,
	})); err != nil {
		t.Fatal(err)
	}
	auto, _ = rec.body["automation"].(map[string]any)
	for _, k := range []string{"cleanup_mode", "cleanup_retention_hours", "cleanup_max_removals_per_pass", "disk_alert_percent"} {
		if _, ok := auto[k]; ok {
			t.Errorf("%s leaked into an idle-only PUT: %v", k, rec.body)
		}
	}
}
