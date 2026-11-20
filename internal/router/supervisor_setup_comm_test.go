package router

import (
	"net/http"
	"slices"
	"strings"
	"testing"
)

func TestSupervisorSetupComm_RunPostsBody(t *testing.T) {
	if !slices.Contains(SupervisorSetupCommVerbs(), "supervisor-setup") {
		t.Fatal("verb list must contain supervisor-setup")
	}
	if c := Parse("autonomous supervisor-setup run"); c.Type != CmdAutonomous {
		t.Fatalf("type %v", c.Type)
	}
	log, port := laneFillCommDaemon(t, http.StatusOK, `{"project":"demo"}`)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous supervisor-setup run project=demo model=claude-haiku-5-5 schedules=watch,full"), " ")
	if log.path != "/api/supervisor-setup" || log.method != http.MethodPost {
		t.Fatalf("%s %s", log.method, log.path)
	}
	if !strings.Contains(log.body, `"model":"claude-haiku-5-5"`) || !strings.Contains(log.body, `"name":"full"`) || !strings.Contains(got, "demo") {
		t.Fatalf("body %s reply %s", log.body, got)
	}
}

func TestSupervisorSetupComm_StateAndCorrections(t *testing.T) {
	log, port := laneFillCommDaemon(t, http.StatusOK, `{"ok":true}`)
	r := setupPeerRouter(t, port)
	r.HandleTestMessage("autonomous supervisor-setup state set project=demo id=d1 section=decision text=keep two lanes who=op why=policy change")
	if log.method != http.MethodPost || log.path != "/api/supervisor-setup/state" ||
		!strings.Contains(log.body, `"text":"keep two lanes"`) || !strings.Contains(log.body, `"why":"policy change"`) {
		t.Fatalf("%s %s %s", log.method, log.path, log.body)
	}
	r.HandleTestMessage("autonomous supervisor-setup state remove project=demo id=d1 who=op why=done")
	if log.method != http.MethodDelete {
		t.Fatalf("method %s", log.method)
	}
	r.HandleTestMessage("autonomous supervisor-setup correction list project=demo")
	if log.path != "/api/supervisor-setup/corrections" || log.query != "project=demo" {
		t.Fatalf("%s?%s", log.path, log.query)
	}
	r.HandleTestMessage("autonomous supervisor-setup correction promote project=demo id=c1 who=op why=durable")
	if log.path != "/api/supervisor-setup/corrections/promote" {
		t.Fatalf("path %s", log.path)
	}
	log.calls = 0
	got := strings.Join(r.HandleTestMessage("autonomous supervisor-setup bogus"), " ")
	if log.calls != 0 || !strings.Contains(got, "unknown sub-verb") {
		t.Fatalf("calls %d reply %s", log.calls, got)
	}
}
