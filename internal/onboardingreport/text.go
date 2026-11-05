package onboardingreport

import (
	"fmt"
	"strings"
)

// Text renders the report as plain text. The REST route, MCP, the CLI, comm
// channels and the PWA all show this same text.
func (r Report) Text() string {
	if !r.Enabled {
		return "The onboarding report is turned off (onboarding_report.enabled).\n"
	}
	var b strings.Builder
	c := r.Configured
	b.WriteString("What is configured\n")
	fmt.Fprintf(&b, "  Backends: %s\n", listOrNone(c.Backends))
	fmt.Fprintf(&b, "  Models: %s\n", listOrNone(c.Models))
	if len(c.Projects) == 0 {
		b.WriteString("  Projects: none\n")
	} else {
		b.WriteString("  Projects:\n")
		for _, p := range c.Projects {
			fmt.Fprintf(&b, "    %s (%s)\n", p.Name, p.Dir)
		}
	}
	fmt.Fprintf(&b, "  Comm channels: %s\n", listOrNone(c.Channels))
	fmt.Fprintf(&b, "  Memory: %s\n", map[bool]string{true: "on", false: "off"}[c.Memory])
	if len(c.Requirements) == 0 {
		b.WriteString("  Requirements: not provided by this surface\n")
	} else {
		b.WriteString("  Requirements:\n")
		for _, rq := range c.Requirements {
			state := "met"
			if !rq.Satisfied {
				state = "unmet"
			}
			line := rq.Capability
			if rq.Required != "" {
				line += ": requires " + rq.Required
			}
			fmt.Fprintf(&b, "    [%s] %s\n", state, line)
		}
	}
	if len(c.MergePipeline) == 0 {
		b.WriteString("  Merge pipeline: no projects\n")
	} else {
		b.WriteString("  Merge pipeline:\n")
		for _, p := range c.MergePipeline {
			fmt.Fprintf(&b, "    %s: %s\n", p.Project, p.Source)
		}
	}
	b.WriteString("\nWhat is missing\n")
	if len(r.Checks) == 0 {
		b.WriteString("  No checks are turned on.\n")
	}
	for _, k := range r.Checks {
		fmt.Fprintf(&b, "  [%s] %s: %s (%s)\n", k.Status, k.Title, k.Reason, k.ID)
	}
	if len(r.NextSteps) > 0 {
		b.WriteString("\nWhat to try next\n")
		for i, s := range r.NextSteps {
			fmt.Fprintf(&b, "  %d. %s\n", i+1, s.Title)
			fmt.Fprintf(&b, "     How-to: %s\n     Definitions: %s\n", s.HowTo, s.Anchor)
			var where []string
			if s.Command != "" {
				where = append(where, "command: "+s.Command)
			}
			if s.MCPTool != "" {
				where = append(where, "MCP tool: "+s.MCPTool)
			}
			if s.PWAPlace != "" {
				where = append(where, "web app: "+s.PWAPlace)
			}
			if len(where) > 0 {
				fmt.Fprintf(&b, "     Do it with %s\n", strings.Join(where, "; "))
			}
		}
	}
	return b.String()
}

func listOrNone(l []string) string {
	if len(l) == 0 {
		return "none"
	}
	return strings.Join(l, ", ")
}
