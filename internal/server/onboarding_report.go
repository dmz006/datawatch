// Onboarding report surface:
//
//	GET /api/automation/onboarding-report              the structured report
//	GET /api/automation/onboarding-report?format=text  the shared plain text
//
// The report says what is configured, what is missing and what to try next
// for Automation. It is built from live daemon state by package
// onboardingreport and is read-only: it never changes configuration. The
// onboarding_report block switches the report and its parts. Reads need the
// autonomous read capability.
package server

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/mergepipeline"
	"github.com/dmz006/datawatch/internal/onboardingreport"
)

// onboardingState adapts live server state to the report's source interfaces.
type onboardingState struct{ s *Server }

func (o onboardingState) Backends() []onboardingreport.Backend {
	var out []onboardingreport.Backend
	if o.s.inferenceReg == nil {
		return out
	}
	for _, l := range o.s.inferenceReg.List() {
		if l == nil || l.Disabled {
			continue
		}
		b := onboardingreport.Backend{Name: l.Name}
		for _, m := range l.Models {
			b.Models = append(b.Models, m.Model)
		}
		if len(b.Models) == 0 && l.Model != "" {
			b.Models = []string{l.Model}
		}
		out = append(out, b)
	}
	return out
}

func (o onboardingState) Projects() []onboardingreport.Project {
	var out []onboardingreport.Project
	if o.s.cfg == nil {
		return out
	}
	for name, e := range o.s.cfg.Projects {
		out = append(out, onboardingreport.Project{Name: name, Dir: e.Dir})
	}
	sortOnboardingProjects(out)
	return out
}

func (o onboardingState) Channels() []string {
	c := o.s.cfg
	if c == nil {
		return nil
	}
	var out []string
	add := func(on bool, name string) {
		if on {
			out = append(out, name)
		}
	}
	add(c.Signal.AccountNumber != "", "signal")
	add(c.Telegram.Enabled, "telegram")
	add(c.Discord.Enabled, "discord")
	add(c.Slack.Enabled, "slack")
	add(c.Matrix.Enabled, "matrix")
	add(c.Twilio.Enabled, "twilio")
	add(c.Ntfy.Enabled, "ntfy")
	add(c.Email.Enabled, "email")
	add(c.Webhook.Enabled, "webhook")
	add(c.GitHubWebhook.Enabled, "github_webhook")
	return out
}

func (o onboardingState) MemoryEnabled() bool { return o.s.cfg != nil && o.s.cfg.Memory.Enabled }

// PipelineSource resolves the merge pipeline source for a project the same
// way the merge pipeline route does, without reading the definition's steps.
func (o onboardingState) PipelineSource(project string) string {
	records, err := o.s.mergePipelineRecords()
	if err != nil {
		return string(mergepipeline.SourceDefault)
	}
	cfg := o.s.mergePipelineConfig()
	resolver := &mergepipeline.Resolver{
		Records:    records,
		ProjectDir: func(p string) (string, bool) { return o.s.mergePipelineProjectDir(p) },
		Settings:   func(p string) mergepipeline.Settings { return mergePipelineSettingsFor(cfg, p) },
	}
	res, _ := resolver.Resolve(project)
	if res.Source == "" {
		return string(mergepipeline.SourceDefault)
	}
	return string(res.Source)
}

// Requirements adapts the requirements engine's report to the onboarding
// report's lines: one line per capability minimum, plus one unmet line per
// unmet automation backend minimum. Nil when the engine is not wired,
// which the check reads as optional (not missing).
func (o onboardingState) Requirements() []onboardingreport.RequirementLine {
	rep, ok := o.s.requirementsReport()
	if !ok {
		return nil
	}
	lines := make([]onboardingreport.RequirementLine, 0, len(rep.Capabilities)+len(rep.Missing))
	for _, c := range rep.Capabilities {
		detail := "Ollama minimum " + c.MinimumOllama
		if c.Override {
			detail += " (operator override)"
		}
		nodes := make([]string, 0, len(c.Nodes))
		for _, n := range c.Nodes {
			nodes = append(nodes, fmt.Sprintf("%s has Ollama %s", n.Name, n.Found))
		}
		if len(nodes) > 0 {
			detail += "; " + strings.Join(nodes, ", ")
		}
		lines = append(lines, onboardingreport.RequirementLine{
			Capability: c.Capability, Required: c.MinimumOllama,
			Satisfied: c.Satisfied, Detail: detail,
		})
	}
	for _, m := range rep.Missing {
		lines = append(lines, onboardingreport.RequirementLine{
			Capability: "automation backends", Satisfied: false, Detail: m,
		})
	}
	return lines
}

func sortOnboardingProjects(p []onboardingreport.Project) {
	for i := 1; i < len(p); i++ {
		for j := i; j > 0 && p[j].Name < p[j-1].Name; j-- {
			p[j], p[j-1] = p[j-1], p[j]
		}
	}
}

// onboardingSettings reads the onboarding_report block.
func onboardingSettings(c config.OnboardingReportConfig) onboardingreport.Settings {
	return onboardingreport.Settings{
		Enabled:          c.EffectiveEnabled(),
		IncludeNextSteps: c.EffectiveIncludeNextSteps(),
		DisabledChecks:   c.EffectiveDisabledChecks(),
	}
}

// handleOnboardingReport serves GET /api/automation/onboarding-report.
func (s *Server) handleOnboardingReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapAutonomousRead) {
		return
	}
	if s.cfg == nil {
		http.Error(w, "config not available", http.StatusServiceUnavailable)
		return
	}
	rep := onboardingreport.Build(onboardingreport.Sources{
		Backends: onboardingState{s}, Projects: onboardingState{s}, Channels: onboardingState{s},
		Memory: onboardingState{s}, Pipeline: onboardingState{s}, Requirements: onboardingState{s},
	}, onboardingSettings(s.cfg.OnboardingReport))
	switch r.URL.Query().Get("format") {
	case "", "json":
		writeJSONOK(w, rep)
	case "text":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(rep.Text()))
	default:
		http.Error(w, "bad request: format must be json or text", http.StatusBadRequest)
	}
}
