package onboardingreport

import (
	"fmt"
	"strings"
)

// Check ids. They are stable: onboarding_report.disabled_checks names them.
const (
	CheckBackends      = "backends"
	CheckModels        = "models"
	CheckProjects      = "projects"
	CheckChannels      = "channels"
	CheckMemory        = "memory"
	CheckMergePipeline = "merge_pipeline"
	CheckRequirements  = "requirements"
)

// AllCheckIDs returns every check id in report order.
func AllCheckIDs() []string {
	defs := checkDefs()
	ids := make([]string, len(defs))
	for i, d := range defs {
		ids[i] = d.ID
	}
	return ids
}

const definitionsPage = "docs/datawatch-definitions.md"

// checkDef is one check as data: what it is called, how it is judged against
// the configured state, and the suggestion it produces when not ok.
type checkDef struct {
	ID    string
	Title string
	eval  func(Configured) (Status, string)
	Step  Step
}

func checkDefs() []checkDef {
	return []checkDef{
		{
			ID: CheckBackends, Title: "An LLM backend is available",
			eval: func(c Configured) (Status, string) {
				if len(c.Backends) == 0 {
					return StatusMissing, "No LLM backend is enabled, so Automation has nothing to run its tasks with."
				}
				return StatusOK, fmt.Sprintf("%d backend(s) are enabled.", len(c.Backends))
			},
			Step: Step{
				Title: "Add an LLM backend", HowTo: "docs/howto/llm-registry.md",
				Anchor:  definitionsPage + "#llm-registry",
				Command: "datawatch llm add", MCPTool: "llm_add", PWAPlace: "Settings > LLM",
			},
		},
		{
			ID: CheckModels, Title: "A model is available to run",
			eval: func(c Configured) (Status, string) {
				switch {
				case len(c.Backends) == 0:
					return StatusMissing, "There is no backend, so there are no models to choose from."
				case len(c.Models) == 0:
					return StatusMissing, "The enabled backends list no models yet."
				}
				return StatusOK, fmt.Sprintf("%d model(s) can be chosen.", len(c.Models))
			},
			Step: Step{
				Title: "Add or refresh the models of a backend", HowTo: "docs/howto/llm-registry.md",
				Anchor:  definitionsPage + "#llm-registry",
				Command: "datawatch llm refresh-models", MCPTool: "llm_refresh_models", PWAPlace: "Settings > LLM",
			},
		},
		{
			ID: CheckProjects, Title: "A project directory is registered",
			eval: func(c Configured) (Status, string) {
				if len(c.Projects) == 0 {
					return StatusMissing, "No project directory is registered, so an Automaton has no place to work in."
				}
				return StatusOK, fmt.Sprintf("%d project(s) are registered.", len(c.Projects))
			},
			Step: Step{
				Title: "Register a project directory", HowTo: "docs/howto/automation-tab.md",
				Anchor:  definitionsPage + "#project-profiles",
				Command: "datawatch projects upsert <name>", MCPTool: "project_upsert", PWAPlace: "Settings > Projects",
			},
		},
		{
			ID: CheckChannels, Title: "A comm channel is enabled",
			eval: func(c Configured) (Status, string) {
				if len(c.Channels) == 0 {
					return StatusOptional, "No comm channel is enabled; Automation runs, but nothing reaches you outside the web app."
				}
				return StatusOK, fmt.Sprintf("%d channel(s) are enabled.", len(c.Channels))
			},
			Step: Step{
				Title: "Enable a comm channel", HowTo: "docs/howto/comm-channels.md",
				Anchor:  definitionsPage + "#channel-routing-v830",
				Command: "datawatch config set", MCPTool: "config_set", PWAPlace: "Settings > Comms",
			},
		},
		{
			ID: CheckMemory, Title: "Memory is enabled",
			eval: func(c Configured) (Status, string) {
				if !c.Memory {
					return StatusOptional, "Memory is off, so Automata cannot recall earlier work."
				}
				return StatusOK, "Memory is on."
			},
			Step: Step{
				Title: "Turn memory on", HowTo: "docs/howto/cross-agent-memory.md",
				Anchor:  definitionsPage + "#what-it-gives-you",
				Command: "datawatch config set memory.enabled true", MCPTool: "config_set", PWAPlace: "Settings > Memory",
			},
		},
		{
			ID: CheckMergePipeline, Title: "Projects use their own merge pipeline",
			eval: func(c Configured) (Status, string) {
				if len(c.MergePipeline) == 0 {
					return StatusOptional, "There is no project to choose a merge pipeline for."
				}
				n := 0
				for _, p := range c.MergePipeline {
					if p.Source == "default" {
						n++
					}
				}
				if n > 0 {
					return StatusOptional, fmt.Sprintf("%d project(s) use the built-in merge pipeline; give them their own to change the merge steps.", n)
				}
				return StatusOK, "Every project has its own merge pipeline."
			},
			Step: Step{
				Title: "Give a project its own merge pipeline", HowTo: "docs/howto/automation-merged-member.md",
				Anchor:  definitionsPage + "#automation-detail-and-pipeline-strip",
				Command: "datawatch autonomous merge-pipeline show", MCPTool: "autonomous_merge_pipeline_get", PWAPlace: "Settings > Automation > Merge pipeline",
			},
		},
		{
			ID: CheckRequirements, Title: "The automation backend requirements are met",
			eval: func(c Configured) (Status, string) {
				if len(c.Requirements) == 0 {
					// No source (a surface without the requirements engine): the
					// requirement cannot be judged, so it is optional, not missing.
					return StatusOptional, "No requirements report is provided by this surface."
				}
				var unmet []string
				for _, rq := range c.Requirements {
					if !rq.Satisfied {
						if rq.Detail != "" {
							unmet = append(unmet, rq.Detail)
						} else if rq.Capability != "" && rq.Required != "" {
							unmet = append(unmet, fmt.Sprintf("%s requires %s", rq.Capability, rq.Required))
						} else if rq.Capability != "" {
							unmet = append(unmet, rq.Capability)
						} else {
							unmet = append(unmet, "an automation requirement")
						}
					}
				}
				if len(unmet) > 0 {
					return StatusMissing, fmt.Sprintf("%d requirement(s) unmet: %s", len(unmet), strings.Join(unmet, "; "))
				}
				return StatusOK, fmt.Sprintf("All %d requirement(s) are met.", len(c.Requirements))
			},
			Step: Step{
				Title: "Meet the automation backend requirements", HowTo: "docs/howto/automation-requirements.md",
				Anchor:  definitionsPage + "#automation-requirements",
				Command: "datawatch automation requirements-report", MCPTool: "automation_requirements_report",
			},
		},
	}
}
