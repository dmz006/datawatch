// supervisor_wake_comm_test.go — the chat verb "supervisor wake": the status
// read renders the settings and last wake times, and set sends one config
// write with typed values before showing the result.
package router

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

const supervisorWakeCommBody = `{"enabled":true,"kinds":["task.failed","prd.blocked"],"coalesce_seconds":45,"fallback_interval_minutes":10,"last_event":"0001-01-01T00:00:00Z","last_scheduled_check":"2026-11-01T10:00:00Z","event_count":0,"scheduled_count":3}`

func TestSupervisorWakeStatus(t *testing.T) {
	log, port := laneFillCommDaemon(t, http.StatusOK, supervisorWakeCommBody)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous supervisor wake"), "\n")
	if log.path != "/api/supervisor-loop/wake" || log.calls != 1 {
		t.Errorf("path=%s calls=%d; want one read of the wake route", log.path, log.calls)
	}
	for _, want := range []string{"wake: on", "task.failed, prd.blocked", "45s", "10 min", "last event: never", "2026-11-01T10:00:00Z"} {
		if !strings.Contains(got, want) {
			t.Errorf("reply lacks %q: %q", want, got)
		}
	}
}

func TestSupervisorWakeSetSendsTypedConfig(t *testing.T) {
	log, port := laneFillCommDaemon(t, http.StatusOK, supervisorWakeCommBody)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous supervisor wake set enabled=true kinds=task.failed,prd.blocked coalesce_seconds=45"), "\n")
	if log.calls != 2 || log.path != "/api/supervisor-loop/wake" {
		t.Fatalf("calls=%d last path=%s; want a config write then the status read", log.calls, log.path)
	}
	if !strings.Contains(got, "wake: on") {
		t.Errorf("reply lacks the status: %q", got)
	}
}

func TestSupervisorWakeSetRefusesBadInput(t *testing.T) {
	log, port := laneFillCommDaemon(t, http.StatusOK, supervisorWakeCommBody)
	r := setupPeerRouter(t, port)
	for _, text := range []string{
		"autonomous supervisor wake set",
		"autonomous supervisor wake set bogus=1",
		"autonomous supervisor wake set enabled=maybe",
		"autonomous supervisor wake set coalesce_seconds=x",
		"autonomous supervisor wake nope",
	} {
		got := strings.Join(r.HandleTestMessage(text), "\n")
		if !strings.Contains(got, "usage: supervisor") {
			t.Errorf("%q: reply lacks the usage: %q", text, got)
		}
	}
	if log.calls != 0 {
		t.Errorf("bad input must not reach the daemon, got %d calls", log.calls)
	}
}

func TestSupervisorWakeSetBodyKeys(t *testing.T) {
	var body string
	_, port := newFakePeerParent(t, func(w http.ResponseWriter, rq *http.Request) {
		if rq.Method == http.MethodPut {
			b := make([]byte, 512)
			n, _ := rq.Body.Read(b)
			body = string(b[:n])
		}
		_, _ = w.Write([]byte(supervisorWakeCommBody))
	})
	r := setupPeerRouter(t, port)
	r.HandleTestMessage("autonomous supervisor wake set enabled=false kinds=a,b fallback_interval_minutes=5")
	var m map[string]any
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("config body %q: %v", body, err)
	}
	if m["supervisor_loop.wake.enabled"] != false || m["supervisor_loop.wake.fallback_interval_minutes"] != float64(5) {
		t.Errorf("config body = %v", m)
	}
	if k, _ := m["supervisor_loop.wake.kinds"].([]any); len(k) != 2 {
		t.Errorf("kinds = %v", m["supervisor_loop.wake.kinds"])
	}
}
