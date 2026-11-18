package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// editTaskRecorder records the EditTask arguments the REST handler passes down.
type editTaskRecorder struct {
	*tmFake
	script, reason, reportPath, spec string
	calls                            int
}

func (f *editTaskRecorder) EditTask(_, _, newSpec string, _ *bool, reportPath, script, reason, _ string) (any, error) {
	f.calls++
	f.spec, f.reportPath, f.script, f.reason = newSpec, reportPath, script, reason
	return nil, nil
}

func newEditTaskRecorderServer(t *testing.T) (*Server, *editTaskRecorder) {
	t.Helper()
	srv, base := newTruncationServer(t)
	f := &editTaskRecorder{tmFake: base}
	srv.SetAutonomousAPI(f)
	return srv, f
}

func TestVerifyStable_RESTEditTaskPassesScriptAndReasonThrough(t *testing.T) {
	srv, f := newEditTaskRecorderServer(t)
	body, _ := json.Marshal(map[string]string{
		"task_id":                    "t1",
		"verification_script":        "scripts/verify.sh",
		"verification_script_reason": "docs only",
	})
	w := run(srv, http.MethodPost, "/api/autonomous/prds/p1/edit_task", string(body))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if f.calls != 1 || f.script != "scripts/verify.sh" || f.reason != "docs only" || f.spec != "" {
		t.Fatalf("EditTask got script=%q reason=%q spec=%q calls=%d", f.script, f.reason, f.spec, f.calls)
	}
}

func TestVerifyStable_RESTEditTaskReasonAloneIsAnEdit(t *testing.T) {
	srv, f := newEditTaskRecorderServer(t)
	w := run(srv, http.MethodPost, "/api/autonomous/prds/p1/edit_task", `{"task_id":"t1","verification_script_reason":"docs only"}`)
	if w.Code != http.StatusOK || f.calls != 1 || f.reason != "docs only" || f.script != "" {
		t.Fatalf("code %d calls %d script=%q reason=%q: %s", w.Code, f.calls, f.script, f.reason, w.Body.String())
	}
}

func TestVerifyStable_RESTEditTaskNothingToEditMessage(t *testing.T) {
	srv, f := newEditTaskRecorderServer(t)
	w := run(srv, http.MethodPost, "/api/autonomous/prds/p1/edit_task", `{"task_id":"t1"}`)
	if w.Code != http.StatusBadRequest || f.calls != 0 {
		t.Fatalf("code %d calls %d, want a 400 that never reaches the manager", w.Code, f.calls)
	}
	if msg := w.Body.String(); !strings.Contains(msg, "verification_script") || !strings.Contains(msg, "verification_script_reason") {
		t.Fatalf("message %q does not name the new fields", msg)
	}
}
