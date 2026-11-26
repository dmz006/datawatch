package router

import (
	"reflect"
	"strings"
	"testing"
)

// `autonomous tags repair` is a dry run; only the word apply makes it write.
func TestTagRepairVerbRequest(t *testing.T) {
	for _, tc := range []struct {
		args []string
		body map[string]any
	}{
		{[]string{"tags", "repair"}, map[string]any{"dry_run": true}},
		{[]string{"tags", "repair", "dry-run"}, map[string]any{"dry_run": true}},
		{[]string{"tags", "repair", "apply"}, map[string]any{"dry_run": false}},
	} {
		req, usage := buildCommTagRequest("tags", tc.args)
		if usage != "" || req.method != "POST" || req.path != "/api/autonomous/tags/repair" || !reflect.DeepEqual(req.body, tc.body) {
			t.Errorf("%v = %+v usage %q, want body %v", tc.args, req, usage, tc.body)
		}
	}
	for _, args := range [][]string{{"tags", "repair", "now"}, {"tags", "repair", "apply", "more"}} {
		if _, usage := buildCommTagRequest("tags", args); !strings.Contains(usage, "repair [apply]") {
			t.Errorf("%v: usage = %q", args, usage)
		}
	}
	if help := joinReplies((&Router{}).HandleTestMessage("autonomous")); !strings.Contains(help, "tags repair [apply]") {
		t.Errorf("help misses the repair verb")
	}
}
