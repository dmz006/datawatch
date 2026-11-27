// The supervisor_loop_stats tool passes the wake object through; its
// description names both wake counts.

package mcp

import (
	"strings"
	"testing"
)

func TestSupervisorLoopStatsDescribesBothWakeCounts(t *testing.T) {
	got := (&Server{}).toolSupervisorLoopStats().Description
	for _, want := range []string{"event_count", "typed", "pass_count"} {
		if !strings.Contains(got, want) {
			t.Errorf("description lacks %q: %s", want, got)
		}
	}
}
