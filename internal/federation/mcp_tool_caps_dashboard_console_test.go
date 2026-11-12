package federation_test

import (
	"testing"

	"github.com/dmz006/datawatch/internal/federation"
)

// The operator console is read-only dashboard data: the new
// dashboard_console_get MCP tool must map to the existing dashboard:read
// capability, so an observer holding it can fetch the one-glance summary
// exactly like the other dashboard_* tools.
func TestMCPToolCap_DashboardConsoleGetIsDashread(t *testing.T) {
	cap, ok := federation.MCPToolCap["dashboard_console_get"]
	if !ok {
		t.Fatal("dashboard_console_get is missing from MCPToolCap — it would be callable by nobody but admin")
	}
	want := federation.CapDashboardRead
	if cap != want {
		t.Errorf("dashboard_console_get cap = %q, want %q", cap, want)
	}

	// A read-only observer (the builtin "monitor" group, which lists
	// dashboard:read among its member capabilities) can call it.
	granted := federation.Resolve([]string{"monitor"}, nil)
	if !federation.Check(granted, federation.CapDashboardRead) {
		t.Fatal("monitor group does not grant dashboard:read (test premise broken)")
	}
}

func TestMCPToolCap_DashboardConsoleGetAllowedForMonitor(t *testing.T) {
	granted := federation.Resolve([]string{"monitor"}, nil)
	if got, ok := federation.MCPToolCap["dashboard_console_get"]; !ok || !federation.Check(granted, got) {
		t.Error("monitor (dashboard:read) cannot call dashboard_console_get")
	}
}
