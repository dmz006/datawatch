package router

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
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

// TestCommHeldForReviewShowsAttemptCount: the held-for-review lines carry the
// total attempt count and, for a task held at the cap, the force reset to use.
func TestCommHeldForReviewShowsAttemptCount(t *testing.T) {
	sent := make(chan string, 4)
	r := &Router{hostname: "h", backend: &captureBackend{name: "test", capture: func(m string) { sent <- m }}}
	r.replyHeldForReview([]byte(`{"review_needed":[
		{"automaton_id":"a1","automaton_title":"A","task_id":"t1","task_title":"T","failure_count":3,"attempts_total":6,"attempt_cap_held":true},
		{"automaton_id":"a1","automaton_title":"A","task_id":"t2","task_title":"U","failure_count":3,"attempts_total":3}]}`))
	var out string
	select {
	case out = <-sent: // the reply is sent from a goroutine
	case <-time.After(5 * time.Second):
		t.Fatal("no reply was sent")
	}
	for _, want := range []string{"6 attempts started in all", "held at the attempt cap", "autonomous reset-task a1 t1 force", "3 attempts started in all"} {
		if !strings.Contains(out, want) {
			t.Errorf("reply lacks %q:\n%s", want, out)
		}
	}
	if strings.Count(out, "held at the attempt cap") != 1 {
		t.Errorf("only the cap-held task names the cap:\n%s", out)
	}
}

// TestCommResetTaskReason: everything after the reason token is the reason a
// force reset carries to the audit log and the operator alert.
func TestCommResetTaskReason(t *testing.T) {
	cap := runAutonomousTaskCommand(t, "autonomous reset-task p1 t1 force reason node was flaky, retrying by hand")
	var m map[string]any
	if err := json.Unmarshal([]byte(cap.body), &m); err != nil {
		t.Fatalf("body: %v (%q)", err, cap.body)
	}
	if m["reason"] != "node was flaky, retrying by hand" || m["force"] != true {
		t.Errorf("body = %v, want force and the reason", m)
	}
	cap = runAutonomousTaskCommand(t, "autonomous reset-task p1 t1 force")
	_ = json.Unmarshal([]byte(cap.body), &m)
	if m["reason"] != "" {
		t.Errorf("no reason given must send an empty reason, got %v", m["reason"])
	}
}
