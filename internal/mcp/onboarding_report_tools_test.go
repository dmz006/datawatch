package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/onboardingreport"
)

func onboardingSampleReport() onboardingreport.Report {
	return onboardingreport.Report{
		Enabled: true,
		Configured: onboardingreport.Configured{
			Backends: []string{"claude-code"}, Models: []string{"sonnet"},
			Projects: []onboardingreport.Project{{Name: "web", Dir: "/srv/web"}},
			Channels: []string{}, Memory: true,
		},
		Checks:    []onboardingreport.Check{{ID: "channels", Title: "Comm channel", Status: onboardingreport.Status("missing"), Reason: "none configured"}},
		NextSteps: []onboardingreport.Step{{CheckID: "channels", Title: "Add a comm channel", HowTo: "docs/howto/x.md", Anchor: "docs/config.md#c"}},
	}
}

func TestOnboardingReport_ToolSchema(t *testing.T) {
	tool := (&Server{}).toolOnboardingReport()
	if tool.Name != "onboarding_report" {
		t.Fatalf("name = %q", tool.Name)
	}
	if _, ok := tool.InputSchema.Properties["format"]; !ok {
		t.Error("tool lacks the format parameter")
	}
	if len(tool.InputSchema.Required) != 0 {
		t.Errorf("format must be optional, got %v", tool.InputSchema.Required)
	}
	if tool.Annotations.ReadOnlyHint == nil || !*tool.Annotations.ReadOnlyHint {
		t.Error("onboarding_report must be read-only")
	}
	if strings.Contains(tool.Description, "PRD") {
		t.Error("description must not say PRD")
	}
}

func TestOnboardingReport_ToolIsListed(t *testing.T) {
	s := New("testhost", nil, &config.MCPConfig{Enabled: true}, t.TempDir(), Options{Version: "test"})
	tools, err := s.ListTools()
	if err != nil {
		t.Fatal(err)
	}
	for _, tl := range tools {
		if tl.Name == "onboarding_report" {
			return
		}
	}
	t.Error("onboarding_report is not registered")
}

func onboardingServer(t *testing.T, rep onboardingreport.Report) (*Server, *string) {
	t.Helper()
	body, _ := json.Marshal(rep)
	path := new(string)
	return capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		*path = r.URL.Path + "?" + r.URL.RawQuery
		_, _ = w.Write(body)
	}), path
}

func TestOnboardingReport_TextUsesSharedRenderer(t *testing.T) {
	rep := onboardingSampleReport()
	s, path := onboardingServer(t, rep)
	res, err := s.handleOnboardingReport(context.Background(), callReq(nil))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if *path != "/api/automation/onboarding-report?format=json" {
		t.Errorf("proxied %s", *path)
	}
	if got := automationText(t, res); got != rep.Text() {
		t.Errorf("text differs from the shared renderer:\n%s", got)
	}
}

func TestOnboardingReport_JSONFormatIsRawBody(t *testing.T) {
	s, _ := onboardingServer(t, onboardingSampleReport())
	res, err := s.handleOnboardingReport(context.Background(), callReq(map[string]any{"format": "json"}))
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	var back onboardingreport.Report
	if err := json.Unmarshal([]byte(automationText(t, res)), &back); err != nil || !back.Enabled {
		t.Errorf("not the report JSON: %v", err)
	}
}

func TestOnboardingReport_DisabledSetting(t *testing.T) {
	s, _ := onboardingServer(t, onboardingreport.Report{Enabled: false, Checks: []onboardingreport.Check{}, NextSteps: []onboardingreport.Step{}})
	res, err := s.handleOnboardingReport(context.Background(), callReq(nil))
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if got := automationText(t, res); !strings.Contains(got, "onboarding_report.enabled") {
		t.Errorf("disabled text = %q", got)
	}
}

func TestOnboardingReport_BadFormat(t *testing.T) {
	s, _ := onboardingServer(t, onboardingSampleReport())
	res, err := s.handleOnboardingReport(context.Background(), callReq(map[string]any{"format": "xml"}))
	if err != nil || res == nil || !res.IsError {
		t.Fatalf("want a tool error, got err=%v res=%+v", err, res)
	}
}

func TestOnboardingReport_RESTErrorAndBadBody(t *testing.T) {
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) { http.Error(w, "boom", http.StatusInternalServerError) })
	if res, _ := s.handleOnboardingReport(context.Background(), callReq(nil)); res == nil || !res.IsError {
		t.Error("a REST error must be a tool error")
	}
	s = capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("not json")) })
	if res, _ := s.handleOnboardingReport(context.Background(), callReq(nil)); res == nil || !res.IsError {
		t.Error("an undecodable body must be a tool error")
	}
}
