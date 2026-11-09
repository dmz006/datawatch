// Package workerprofile defines worker session profiles: named, validated
// bundles that decide what a worker session starts with (which MCP servers
// it may use, which tools it may call, whether skills load, how much project
// context it receives, and which model and effort it runs at).
//
// Built-in profiles are provided for the common cases. Validate reports
// whether a profile is well formed before it is used to start a session.
package workerprofile

import (
	"errors"
	"fmt"
	"sort"
)

// MCP selection modes.
const (
	MCPNone      = "none"      // the worker gets no MCP servers
	MCPDatawatch = "datawatch" // the worker gets the datawatch MCP server
	MCPList      = "list"      // the worker gets only the servers in MCPSelection.Servers
)

// Tool selection modes.
const (
	ToolsAll   = "all"   // every tool is available
	ToolsAllow = "allow" // only the tools in ToolSelection.Allow are available
)

// Context levels describing how much project context a worker starts with.
const (
	ContextMinimal = "minimal"
	ContextDigest  = "digest"
	ContextFull    = "full"
)

// MCPSelection says which MCP servers a worker may use.
type MCPSelection struct {
	Mode    string   `yaml:"mode,omitempty" json:"mode,omitempty"`       // one of MCPNone, MCPDatawatch or MCPList
	Servers []string `yaml:"servers,omitempty" json:"servers,omitempty"` // required when Mode is MCPList
}

// ToolSelection says which tools a worker may call.
type ToolSelection struct {
	Mode  string   `yaml:"mode,omitempty" json:"mode,omitempty"`   // one of ToolsAll or ToolsAllow
	Allow []string `yaml:"allow,omitempty" json:"allow,omitempty"` // required when Mode is ToolsAllow
}

// WorkerProfile is a named set of choices for a worker session. The yaml and
// json keys are snake case so a profile can be written in config.yaml and
// sent over the REST API with the same field names.
type WorkerProfile struct {
	Name    string        `yaml:"name,omitempty" json:"name,omitempty"`
	MCP     MCPSelection  `yaml:"mcp,omitempty" json:"mcp,omitempty"`
	Tools   ToolSelection `yaml:"tools,omitempty" json:"tools,omitempty"`
	Skills  bool          `yaml:"skills,omitempty" json:"skills,omitempty"`
	Context string        `yaml:"context,omitempty" json:"context,omitempty"` // one of ContextMinimal, ContextDigest or ContextFull
	Model   string        `yaml:"model,omitempty" json:"model,omitempty"`
	Effort  string        `yaml:"effort,omitempty" json:"effort,omitempty"`
}

// minimal starts a worker with no MCP servers and only the core file and
// shell tools, and no skills or project context beyond the minimum.
var minimal = WorkerProfile{
	Name:    "minimal",
	MCP:     MCPSelection{Mode: MCPNone},
	Tools:   ToolSelection{Mode: ToolsAllow, Allow: []string{"Read", "Edit", "Write", "Bash", "Glob", "Grep"}},
	Skills:  false,
	Context: ContextMinimal,
}

// digest gives a worker the datawatch MCP server and all tools, with a
// digested project context and no skills.
var digest = WorkerProfile{
	Name:    "digest",
	MCP:     MCPSelection{Mode: MCPDatawatch},
	Tools:   ToolSelection{Mode: ToolsAll},
	Skills:  false,
	Context: ContextDigest,
}

// full gives a worker everything: the datawatch MCP server, all tools,
// skills, and the full project context.
var full = WorkerProfile{
	Name:    "full",
	MCP:     MCPSelection{Mode: MCPDatawatch},
	Tools:   ToolSelection{Mode: ToolsAll},
	Skills:  true,
	Context: ContextFull,
}

// datawatchTools is the built-in profile for workers that drive datawatch
// through its tools. It matches digest: datawatch MCP, all tools, no skills,
// digested context.
var datawatchTools = WorkerProfile{
	Name:    "datawatch-tools",
	MCP:     MCPSelection{Mode: MCPDatawatch},
	Tools:   ToolSelection{Mode: ToolsAll},
	Skills:  false,
	Context: ContextDigest,
}

// builtins maps each built-in profile name to its profile.
var builtins = map[string]WorkerProfile{
	minimal.Name:        minimal,
	digest.Name:         digest,
	full.Name:           full,
	datawatchTools.Name: datawatchTools,
}

// Builtin returns a copy of the built-in profile with the given name. The
// boolean is false when no built-in profile has that name. The returned
// profile does not share slices with the built-in definition, so callers may
// change it freely.
func Builtin(name string) (WorkerProfile, bool) {
	p, ok := builtins[name]
	if !ok {
		return WorkerProfile{}, false
	}
	return clone(p), true
}

// Names returns the names of all built-in profiles, sorted ascending.
func Names() []string {
	names := make([]string, 0, len(builtins))
	for name := range builtins {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Validate reports the first problem it finds in p, or nil when p is well
// formed.
func Validate(p WorkerProfile) error {
	if p.Name == "" {
		return errors.New("workerprofile: name is required")
	}
	switch p.Context {
	case ContextMinimal, ContextDigest, ContextFull:
	default:
		return fmt.Errorf("workerprofile: %q: context must be %q, %q or %q, got %q",
			p.Name, ContextMinimal, ContextDigest, ContextFull, p.Context)
	}
	switch p.MCP.Mode {
	case MCPNone, MCPDatawatch:
	case MCPList:
		if len(p.MCP.Servers) == 0 {
			return fmt.Errorf("workerprofile: %q: mcp mode %q needs at least one server", p.Name, MCPList)
		}
	default:
		return fmt.Errorf("workerprofile: %q: mcp mode must be %q, %q or %q, got %q",
			p.Name, MCPNone, MCPDatawatch, MCPList, p.MCP.Mode)
	}
	switch p.Tools.Mode {
	case ToolsAll:
	case ToolsAllow:
		if len(p.Tools.Allow) == 0 {
			return fmt.Errorf("workerprofile: %q: tools mode %q needs at least one entry", p.Name, ToolsAllow)
		}
	default:
		return fmt.Errorf("workerprofile: %q: tools mode must be %q or %q, got %q",
			p.Name, ToolsAll, ToolsAllow, p.Tools.Mode)
	}
	return nil
}

// clone returns a copy of p whose slices do not alias p's.
func clone(p WorkerProfile) WorkerProfile {
	if p.MCP.Servers != nil {
		p.MCP.Servers = append([]string(nil), p.MCP.Servers...)
	}
	if p.Tools.Allow != nil {
		p.Tools.Allow = append([]string(nil), p.Tools.Allow...)
	}
	return p
}
