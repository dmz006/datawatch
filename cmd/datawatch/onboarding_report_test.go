package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/onboardingreport"
)

func onboardingCLIBody(t *testing.T, enabled bool) (string, onboardingreport.Report) {
	t.Helper()
	rep := onboardingreport.Report{Enabled: enabled, Checks: []onboardingreport.Check{}, NextSteps: []onboardingreport.Step{}}
	if enabled {
		rep.Configured = onboardingreport.Configured{Backends: []string{"claude-code"}, Models: []string{}, Projects: []onboardingreport.Project{}, Channels: []string{}}
		rep.Checks = []onboardingreport.Check{{ID: "channels", Title: "Comm channel", Status: onboardingreport.Status("missing"), Reason: "none configured"}}
	}
	b, err := json.Marshal(rep)
	if err != nil {
		t.Fatal(err)
	}
	return string(b), rep
}

func TestOnboardingReportCLI_TextUsesSharedRenderer(t *testing.T) {
	body, rep := onboardingCLIBody(t, true)
	d := newLaneFillCLIDaemon(t, http.StatusOK, body)
	out, err := runLaneFillCmd(t, newAutomationOnboardingReportCmd(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if d.path != "/api/automation/onboarding-report" || d.query != "format=json" {
		t.Errorf("path=%s query=%q", d.path, d.query)
	}
	if out != rep.Text() {
		t.Errorf("output differs from the shared renderer:\n%s", out)
	}
}

func TestOnboardingReportCLI_JSONFormat(t *testing.T) {
	body, _ := onboardingCLIBody(t, true)
	newLaneFillCLIDaemon(t, http.StatusOK, body)
	out, err := runLaneFillCmd(t, newAutomationOnboardingReportCmd(), []string{"--format", "json"})
	if err != nil {
		t.Fatal(err)
	}
	if out != body {
		t.Errorf("json output = %q", out)
	}
}

func TestOnboardingReportCLI_DisabledSetting(t *testing.T) {
	body, _ := onboardingCLIBody(t, false)
	newLaneFillCLIDaemon(t, http.StatusOK, body)
	out, err := runLaneFillCmd(t, newAutomationOnboardingReportCmd(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "onboarding_report.enabled") {
		t.Errorf("disabled output = %q", out)
	}
}

func TestOnboardingReportCLI_Errors(t *testing.T) {
	body, _ := onboardingCLIBody(t, true)
	d := newLaneFillCLIDaemon(t, http.StatusOK, body)
	if _, err := runLaneFillCmd(t, newAutomationOnboardingReportCmd(), []string{"--format", "xml"}); err == nil {
		t.Error("a bad format must be refused")
	}
	if d.calls != 0 {
		t.Errorf("a bad format reached the daemon %d times", d.calls)
	}
	if _, err := runLaneFillCmd(t, newAutomationOnboardingReportCmd(), []string{"extra"}); err == nil {
		t.Error("a stray argument must be refused")
	}
	newLaneFillCLIDaemon(t, http.StatusInternalServerError, "boom")
	if _, err := runLaneFillCmd(t, newAutomationOnboardingReportCmd(), nil); err == nil {
		t.Error("a REST error must fail")
	}
	newLaneFillCLIDaemon(t, http.StatusOK, "not json")
	if _, err := runLaneFillCmd(t, newAutomationOnboardingReportCmd(), nil); err == nil {
		t.Error("an undecodable body must fail")
	}
}
