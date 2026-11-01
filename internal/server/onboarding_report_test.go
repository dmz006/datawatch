package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/onboardingreport"
)

func TestOnboardingReportRoute(t *testing.T) {
	s, _ := newMergePipelineServer(t)
	s.cfg.Memory.Enabled = true
	rec := mpDo(s.handleOnboardingReport, http.MethodGet, "/api/automation/onboarding-report", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var rep onboardingreport.Report
	if err := json.Unmarshal(rec.Body.Bytes(), &rep); err != nil {
		t.Fatal(err)
	}
	if !rep.Enabled || len(rep.Configured.Projects) != 1 || rep.Configured.Projects[0].Name != "demo" || !rep.Configured.Memory {
		t.Errorf("configured state not read from the daemon: %+v", rep.Configured)
	}
	if len(rep.Configured.MergePipeline) != 1 || rep.Configured.MergePipeline[0].Source == "" {
		t.Errorf("merge pipeline source missing: %+v", rep.Configured.MergePipeline)
	}
	statuses := map[string]onboardingreport.Status{}
	for _, c := range rep.Checks {
		statuses[c.ID] = c.Status
	}
	if statuses[onboardingreport.CheckBackends] != onboardingreport.StatusMissing || statuses[onboardingreport.CheckProjects] != onboardingreport.StatusOK {
		t.Errorf("unexpected statuses: %v", statuses)
	}
	if len(rep.NextSteps) == 0 {
		t.Error("expected next steps")
	}
}

func TestOnboardingReportRouteTextAndSwitches(t *testing.T) {
	s, _ := newMergePipelineServer(t)
	rec := mpDo(s.handleOnboardingReport, http.MethodGet, "/api/automation/onboarding-report?format=text", "")
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/plain") ||
		!strings.Contains(rec.Body.String(), "What to try next") {
		t.Fatalf("text: %d %q %s", rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
	}
	f := false
	s.cfg.OnboardingReport.IncludeNextSteps = &f
	s.cfg.OnboardingReport.DisabledChecks = []string{"backends"}
	rec = mpDo(s.handleOnboardingReport, http.MethodGet, "/api/automation/onboarding-report", "")
	var rep onboardingreport.Report
	_ = json.Unmarshal(rec.Body.Bytes(), &rep)
	if len(rep.NextSteps) != 0 {
		t.Error("include_next_steps=false should drop the steps")
	}
	for _, c := range rep.Checks {
		if c.ID == "backends" {
			t.Error("backends check should be disabled")
		}
	}
	s.cfg.OnboardingReport.Enabled = &f
	rec = mpDo(s.handleOnboardingReport, http.MethodGet, "/api/automation/onboarding-report", "")
	_ = json.Unmarshal(rec.Body.Bytes(), &rep)
	if rep.Enabled {
		t.Error("enabled=false should switch the report off")
	}
}

func TestOnboardingReportRouteRefusals(t *testing.T) {
	s, _ := newMergePipelineServer(t)
	if rec := mpDo(s.handleOnboardingReport, http.MethodPost, "/api/automation/onboarding-report", ""); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST: want 405, got %d", rec.Code)
	}
	if rec := mpDo(s.handleOnboardingReport, http.MethodGet, "/api/automation/onboarding-report?format=xml", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("bad format: want 400, got %d", rec.Code)
	}
}
