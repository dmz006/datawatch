// CLI coverage for the --stage-gate flag of the Automation create and update
// verbs. Each test serves the daemon side with captureDaemon.
package main

import (
	"net/http"
	"testing"
)

func TestAutomationCLIStageGateCreate(t *testing.T) {
	c := &captureDaemon{replyBody: `{"id":"ab12","name":"gated","status":"draft","version":1,"members":[]}`}
	withCaptureServer(t, c)

	var err error
	captureStdout(t, func() {
		err = runCmd(t, newAutomationCmd(), "create", "--name", "gated", "--stage-gate")
	})
	if err != nil {
		t.Fatalf("create --stage-gate: %v", err)
	}
	if c.gotMethod != http.MethodPost || c.gotPath != "/api/automations" {
		t.Errorf("proxied %s %s, want POST /api/automations", c.gotMethod, c.gotPath)
	}
	if got := c.body(t)["stage_gate"]; got != true {
		t.Errorf("stage_gate = %v, want true", got)
	}

	// Left unset it is not sent, so the daemon default (off) applies.
	captureStdout(t, func() {
		err = runCmd(t, newAutomationCmd(), "create", "--name", "plain")
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, ok := c.body(t)["stage_gate"]; ok {
		t.Errorf("unset --stage-gate was sent: %v", c.body(t))
	}
}

func TestAutomationCLIStageGateUpdate(t *testing.T) {
	c := &captureDaemon{replyBody: `{"id":"ab12","name":"gated","status":"draft","version":2,"members":[]}`}
	withCaptureServer(t, c)

	var err error
	captureStdout(t, func() {
		err = runCmd(t, newAutomationCmd(), "update", "ab12", "--stage-gate=true")
	})
	if err != nil {
		t.Fatalf("update --stage-gate=true: %v", err)
	}
	body := c.body(t)
	if len(body) != 1 || body["stage_gate"] != true {
		t.Errorf("body = %v, want only stage_gate:true", body)
	}

	captureStdout(t, func() {
		err = runCmd(t, newAutomationCmd(), "update", "ab12", "--stage-gate=false")
	})
	if err != nil {
		t.Fatalf("update --stage-gate=false: %v", err)
	}
	if got, ok := c.body(t)["stage_gate"]; !ok || got != false {
		t.Errorf("stage_gate = %v (sent %v), want false sent so the gate can be turned off", got, ok)
	}

	captureStdout(t, func() {
		err = runCmd(t, newAutomationCmd(), "update", "ab12", "--name", "renamed")
	})
	if err != nil {
		t.Fatalf("update --name: %v", err)
	}
	if _, ok := c.body(t)["stage_gate"]; ok {
		t.Errorf("unset --stage-gate was sent on update: %v", c.body(t))
	}
}
