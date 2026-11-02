package router

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/onboardingreport"
)

func onboardingCommReport(enabled bool) onboardingreport.Report {
	rep := onboardingreport.Report{Enabled: enabled, Checks: []onboardingreport.Check{}, NextSteps: []onboardingreport.Step{}}
	if enabled {
		rep.Configured = onboardingreport.Configured{Backends: []string{"claude-code"}, Models: []string{}, Projects: []onboardingreport.Project{}, Channels: []string{}}
		rep.Checks = []onboardingreport.Check{{ID: "channels", Title: "Comm channel", Status: onboardingreport.Status("missing"), Reason: "none configured"}}
	}
	return rep
}

func onboardingCommBody(t *testing.T, rep onboardingreport.Report) string {
	t.Helper()
	b, err := json.Marshal(rep)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestOnboardingReportComm_TextUsesSharedRenderer(t *testing.T) {
	rep := onboardingCommReport(true)
	log, port := laneFillCommDaemon(t, http.StatusOK, onboardingCommBody(t, rep))
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous onboarding-report"), " ")
	if log.path != "/api/automation/onboarding-report" || log.query != "format=json" {
		t.Errorf("path=%s query=%q", log.path, log.query)
	}
	for _, want := range []string{"What is configured", "claude-code", "What is missing", "Comm channel"} {
		if !strings.Contains(got, want) {
			t.Errorf("reply lacks %q: %q", want, got)
		}
	}
}

func TestOnboardingReportComm_Variants(t *testing.T) {
	_, port := laneFillCommDaemon(t, http.StatusOK, onboardingCommBody(t, onboardingCommReport(true)))
	r := setupPeerRouter(t, port)
	for _, msg := range []string{"autonomous onboarding_report", "autonomous onboarding-report text"} {
		if got := strings.Join(r.HandleTestMessage(msg), " "); !strings.Contains(got, "What is configured") {
			t.Errorf("%q: %q", msg, got)
		}
	}
	got := strings.Join(r.HandleTestMessage("autonomous onboarding-report json"), " ")
	if !strings.Contains(got, `"enabled":true`) {
		t.Errorf("json reply = %q", got)
	}
}

func TestOnboardingReportComm_DisabledSetting(t *testing.T) {
	_, port := laneFillCommDaemon(t, http.StatusOK, onboardingCommBody(t, onboardingCommReport(false)))
	r := setupPeerRouter(t, port)
	if got := strings.Join(r.HandleTestMessage("autonomous onboarding-report"), " "); !strings.Contains(got, "onboarding_report.enabled") {
		t.Errorf("disabled reply = %q", got)
	}
}

func TestOnboardingReportComm_BadUsageAndErrors(t *testing.T) {
	_, port := laneFillCommDaemon(t, http.StatusOK, onboardingCommBody(t, onboardingCommReport(true)))
	r := setupPeerRouter(t, port)
	for _, msg := range []string{"autonomous onboarding-report xml", "autonomous onboarding-report json extra"} {
		if got := strings.Join(r.HandleTestMessage(msg), " "); !strings.Contains(got, "usage: automation onboarding-report") {
			t.Errorf("%q: %q", msg, got)
		}
	}
	_, port = laneFillCommDaemon(t, http.StatusInternalServerError, "boom")
	r = setupPeerRouter(t, port)
	if got := strings.Join(r.HandleTestMessage("autonomous onboarding-report"), " "); !strings.Contains(got, "failed") {
		t.Errorf("error reply = %q", got)
	}
	_, port = laneFillCommDaemon(t, http.StatusOK, "not json")
	r = setupPeerRouter(t, port)
	if got := strings.Join(r.HandleTestMessage("autonomous onboarding-report"), " "); !strings.Contains(got, "decode report") {
		t.Errorf("bad body reply = %q", got)
	}
}

func TestOnboardingReportComm_VerbList(t *testing.T) {
	v := OnboardingReportCommVerbs()
	if len(v) != 1 || v[0] != "onboarding-report" {
		t.Errorf("verbs = %v", v)
	}
}
