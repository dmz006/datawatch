package onboardingreport

import "strings"

// Status is the outcome of one check.
type Status string

const (
	// StatusOK means the thing is set up.
	StatusOK Status = "ok"
	// StatusMissing means Automation needs it and it is not set up.
	StatusMissing Status = "missing"
	// StatusOptional means it is not set up but Automation works without it.
	StatusOptional Status = "optional"
)

// Backend is one LLM backend and the models it offers.
type Backend struct {
	Name   string   `json:"name"`
	Models []string `json:"models,omitempty"`
}

// Project is one registered project directory.
type Project struct {
	Name string `json:"name"`
	Dir  string `json:"dir"`
}

// PipelineChoice names the merge pipeline source a project resolves to
// ("file", "record" or "default").
type PipelineChoice struct {
	Project string `json:"project"`
	Source  string `json:"source"`
}

// RequirementLine is one automation-requirement the report saw: what is
// required, and whether it is satisfied in the current configuration.
type RequirementLine struct {
	Capability string `json:"capability"`
	Required   string `json:"required,omitempty"`
	Satisfied  bool   `json:"satisfied"`
	Detail     string `json:"detail,omitempty"`
}

// BackendSource lists the enabled LLM backends. Implementations adapt live
// daemon state; tests use fakes.
type BackendSource interface{ Backends() []Backend }

// ProjectSource lists the registered project directories.
type ProjectSource interface{ Projects() []Project }

// ChannelSource lists the names of the enabled comm channels.
type ChannelSource interface{ Channels() []string }

// MemorySource reports whether memory is enabled.
type MemorySource interface{ MemoryEnabled() bool }

// PipelineSource reports the merge pipeline source in effect for a project.
type PipelineSource interface{ PipelineSource(project string) string }

// RequirementsSource lists the automation requirements lines (backend
// minimums + automation backend minimums) and their satisfaction.
type RequirementsSource interface{ Requirements() []RequirementLine }

// Sources are the live-state inputs. A nil source reads as "nothing there".
type Sources struct {
	Backends     BackendSource
	Projects     ProjectSource
	Channels     ChannelSource
	Memory       MemorySource
	Pipeline     PipelineSource
	Requirements RequirementsSource
}

// Settings are the report switches (the onboarding_report block).
type Settings struct {
	Enabled          bool
	IncludeNextSteps bool
	DisabledChecks   []string
}

// DefaultSettings returns the settings an unset block reads as.
func DefaultSettings() Settings { return Settings{Enabled: true, IncludeNextSteps: true} }

// Configured is the answer to "what is configured".
type Configured struct {
	Backends      []string          `json:"backends"`
	Models        []string          `json:"models"`
	Projects      []Project         `json:"projects"`
	Channels      []string          `json:"channels"`
	Memory        bool              `json:"memory"`
	MergePipeline []PipelineChoice  `json:"merge_pipeline"`
	Requirements  []RequirementLine `json:"requirements,omitempty"`
}

// Check is one entry of "what is missing".
type Check struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status Status `json:"status"`
	Reason string `json:"reason"`
}

// Step is one suggestion of "what to try next".
type Step struct {
	CheckID string `json:"check_id"`
	Title   string `json:"title"`
	// HowTo is the path of the how-to that explains it.
	HowTo string `json:"howto"`
	// Anchor is the definitions page with its anchor.
	Anchor string `json:"anchor"`
	// Command, MCPTool and PWAPlace name where to do it.
	Command  string `json:"command,omitempty"`
	MCPTool  string `json:"mcp_tool,omitempty"`
	PWAPlace string `json:"pwa_place,omitempty"`
}

// Report is the whole onboarding report.
type Report struct {
	// Enabled is false when the report is switched off; nothing else is set.
	Enabled    bool       `json:"enabled"`
	Configured Configured `json:"configured"`
	Checks     []Check    `json:"checks"`
	NextSteps  []Step     `json:"next_steps"`
}

// Build assembles the report from live state. It only reads.
func Build(src Sources, set Settings) Report {
	if !set.Enabled {
		return Report{Enabled: false, Checks: []Check{}, NextSteps: []Step{}}
	}
	cfgd := gather(src)
	off := map[string]bool{}
	for _, id := range set.DisabledChecks {
		off[strings.TrimSpace(id)] = true
	}
	rep := Report{Enabled: true, Configured: cfgd, Checks: []Check{}, NextSteps: []Step{}}
	for _, d := range checkDefs() {
		if off[d.ID] {
			continue
		}
		st, reason := d.eval(cfgd)
		rep.Checks = append(rep.Checks, Check{ID: d.ID, Title: d.Title, Status: st, Reason: reason})
		if set.IncludeNextSteps && st != StatusOK {
			step := d.Step
			step.CheckID = d.ID
			rep.NextSteps = append(rep.NextSteps, step)
		}
	}
	return rep
}

func gather(src Sources) Configured {
	c := Configured{
		Backends: []string{}, Models: []string{}, Projects: []Project{},
		Channels: []string{}, MergePipeline: []PipelineChoice{},
	}
	if src.Backends != nil {
		seen := map[string]bool{}
		for _, b := range src.Backends.Backends() {
			c.Backends = append(c.Backends, b.Name)
			for _, m := range b.Models {
				if !seen[m] {
					seen[m] = true
					c.Models = append(c.Models, m)
				}
			}
		}
	}
	if src.Projects != nil {
		c.Projects = append(c.Projects, src.Projects.Projects()...)
	}
	if src.Channels != nil {
		c.Channels = append(c.Channels, src.Channels.Channels()...)
	}
	if src.Memory != nil {
		c.Memory = src.Memory.MemoryEnabled()
	}
	if src.Pipeline != nil {
		for _, p := range c.Projects {
			c.MergePipeline = append(c.MergePipeline, PipelineChoice{Project: p.Name, Source: src.Pipeline.PipelineSource(p.Name)})
		}
	}
	if src.Requirements != nil {
		c.Requirements = src.Requirements.Requirements()
	}
	return c
}
