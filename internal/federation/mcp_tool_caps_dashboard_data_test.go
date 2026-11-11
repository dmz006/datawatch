package federation_test

import (
	"testing"

	"github.com/dmz006/datawatch/internal/federation"
)

// The dashboard data tools are read-only and map to dashboard:read.
func TestMCPToolCap_DashboardDataToolsAreDashboardRead(t *testing.T) {
	for _, tool := range []string{"dashboard_work_map_get", "dashboard_lane_pulse_get", "dashboard_series_get"} {
		if got, ok := federation.MCPToolCap[tool]; !ok || got != federation.CapDashboardRead {
			t.Errorf("%s cap = %q (present %v), want %q", tool, got, ok, federation.CapDashboardRead)
		}
	}
}
