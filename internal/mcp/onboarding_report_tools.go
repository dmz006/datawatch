package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"

	"github.com/dmz006/datawatch/internal/onboardingreport"
)

// The onboarding report tool is read-only and proxies
// GET /api/automation/onboarding-report. Text output is rendered by the
// onboardingreport package, the same renderer the REST route, the CLI and the
// comm verb use.

func (s *Server) toolOnboardingReport() mcpsdk.Tool {
	return mcpsdk.NewTool("onboarding_report",
		mcpsdk.WithDescription("Read the onboarding report: what is configured (backends, models, projects, comm channels, memory, merge pipeline), what is missing, and what to try next with a how-to link for each step. Read-only; same data as GET /api/automation/onboarding-report. When the setting onboarding_report.enabled is off the report says so. format is text (the default) or json."),
		mcpsdk.WithString("format", mcpsdk.Enum("text", "json"), mcpsdk.Description("Optional — text (default) or json")),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

func (s *Server) handleOnboardingReport(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	format := "text"
	if v, ok := req.GetArguments()["format"].(string); ok && strings.TrimSpace(v) != "" {
		format = strings.ToLower(strings.TrimSpace(v))
	}
	if format != "text" && format != "json" {
		return toolError("onboarding_report", fmt.Errorf("format must be text or json, got %q", format)), nil
	}
	out, err := s.proxyGet("/api/automation/onboarding-report", url.Values{"format": {"json"}})
	if err != nil {
		return toolError("onboarding_report", err), nil
	}
	if format == "json" {
		return textOK(string(out)), nil
	}
	var rep onboardingreport.Report
	if err := json.Unmarshal(out, &rep); err != nil {
		return toolError("onboarding_report", fmt.Errorf("decode report: %w", err)), nil
	}
	return textOK(rep.Text()), nil
}
