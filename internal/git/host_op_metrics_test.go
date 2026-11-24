package git

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/metrics"
)

// TestHostOpCountersAreInMetricsText: recorded host operations show up in the
// /metrics exposition, named for what they count.
func TestHostOpCountersAreInMetricsText(t *testing.T) {
	resetHostOpCounters()
	metrics.Register()
	RecordHostOp(HostOpPush, true)
	RecordHostOp(HostOpMerge, false)

	rec := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	b, _ := io.ReadAll(rec.Body)
	text := string(b)
	for _, want := range []string{
		`datawatch_git_host_operations_total{kind="push",outcome="ok"}`,
		`datawatch_git_host_operations_total{kind="merge",outcome="failed"}`,
		`datawatch_git_host_last_success_timestamp_seconds`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("metrics text missing %q", want)
		}
	}
}
