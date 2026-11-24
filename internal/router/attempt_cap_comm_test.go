package router

import (
	"encoding/json"
	"net/http"
	"testing"
)

// TestCommResetTaskForce: the comm channel is the operator's, so a reset sent
// from it is the operator's; the force token requeues a completed or cancelled
// task and, as the operator's force reset, clears the attempt count.
func TestCommResetTaskForce(t *testing.T) {
	cases := []struct {
		msg       string
		wantForce bool
		wantClean bool
	}{
		{"autonomous reset-task p1 t1", false, false},
		{"autonomous reset-task p1 t1 force", true, false},
		{"autonomous reset-task p1 t1 force clean-restart", true, true},
		{"autonomous reset-task p1 t1 clean_restart", false, true},
	}
	for _, c := range cases {
		cap := runAutonomousTaskCommand(t, c.msg)
		if cap.method != http.MethodPost || cap.path != "/api/autonomous/prds/p1/reset_task" {
			t.Fatalf("%q: request = %s %s", c.msg, cap.method, cap.path)
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(cap.body), &m); err != nil {
			t.Fatalf("%q: body: %v (%q)", c.msg, err, cap.body)
		}
		force, _ := m["force"].(bool)
		clean, _ := m["clean_restart"].(bool)
		if m["task_id"] != "t1" || m["actor"] != "operator" || force != c.wantForce || clean != c.wantClean {
			t.Errorf("%q: body = %v, want task t1, actor operator, force %v, clean_restart %v", c.msg, m, c.wantForce, c.wantClean)
		}
	}
}
