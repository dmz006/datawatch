// Package skills implements the skill-registry / sync system (BL255, v6.7.0).
//
// A "skill" is a self-contained markdown-and-scripts package that
// influences how an AI session does its work. Skills originate from
// external registries (PAI by default; operator can add others) and
// are synced selectively into ~/.datawatch/skills/ where session spawn
// can resolve them either as files in the session working dir (option C)
// or via the new skill_load MCP tool (option D).
//
// Manifest format follows PAI's SKILL.md + YAML frontmatter convention,
// with 6 datawatch-specific extension fields layered on top. Per the
// Skills-Awareness Rule (AGENT.md): more fields will land; the parser
// tolerates unknown fields and round-trips them via Extra.
package skills

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Manifest captures the YAML-frontmatter block at the top of a SKILL.md.
// Fields below `Entrypoint` are datawatch v1 extensions (a-f from BL255
// design discussion); future extensions land in Extra and the parser
// stays tolerant.
//
// JSON tags mirror the YAML names so PWA + REST clients read the same
// shape — added in v6.7.1-followup after v6.7.0 marshaled CamelCase
// field names by default, breaking the PWA browse modal which expected
// lowercase keys (e.g. m.description).
type Manifest struct {
	// PAI base format
	Name        string   `yaml:"name" json:"name"`
	Description string   `yaml:"description,omitempty" json:"description,omitempty"`
	Version     string   `yaml:"version,omitempty" json:"version,omitempty"`
	Tags        []string `yaml:"tags,omitempty" json:"tags,omitempty"`
	Entrypoint  string   `yaml:"entrypoint,omitempty" json:"entrypoint,omitempty"`

	// (a) Compatibility hints
	CompatibleWith []string `yaml:"compatible_with,omitempty" json:"compatible_with,omitempty"`

	// (b) Dependency declarations
	Requires []string `yaml:"requires,omitempty" json:"requires,omitempty"`

	// (c) Routing / applicability hints
	AppliesTo Applicability `yaml:"applies_to,omitempty" json:"applies_to,omitempty"`

	// (d) Resource hints
	CostHint string `yaml:"cost_hint,omitempty" json:"cost_hint,omitempty"` // low | medium | high
	DiskMB   int    `yaml:"disk_mb,omitempty" json:"disk_mb,omitempty"`

	// (e) Verification command (run after sync)
	Verify string `yaml:"verify,omitempty" json:"verify,omitempty"`

	// (f) Built-in MCP-tool declarations
	ProvidesMCPTools []string `yaml:"provides_mcp_tools,omitempty" json:"provides_mcp_tools,omitempty"`

	// Extra captures any YAML key the parser doesn't know about so they
	// round-trip when the registry is re-synced. Per the Skills-Awareness
	// Rule, unknown fields are surfaced (not hidden) and preserved.
	Extra map[string]any `yaml:",inline" json:"extra,omitempty"`
}

// Applicability narrows when a skill auto-attaches at session spawn.
// Empty fields = "any". Per (c).
type Applicability struct {
	Agents       []string `yaml:"agents,omitempty" json:"agents,omitempty"`               // claude-code, opencode, gemini, ...
	SessionTypes []string `yaml:"session_types,omitempty" json:"session_types,omitempty"` // coding, research, operational, personal
	CommChannels []string `yaml:"comm_channels,omitempty" json:"comm_channels,omitempty"` // signal, telegram, matrix, ...
}

// ParseManifestFile reads a SKILL.md (or .md/.yaml) file and returns
// the parsed Manifest. The frontmatter must be the first YAML block
// delimited by `---` on its own line.
func ParseManifestFile(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return ParseManifestBytes(data)
}

// ParseManifestBytes parses a manifest from raw bytes. Accepts either a
// pure YAML file or a markdown file beginning with a `---` frontmatter
// block.
func ParseManifestBytes(data []byte) (*Manifest, error) {
	frontmatter := extractFrontmatter(data)
	var m Manifest
	if err := yaml.Unmarshal(frontmatter, &m); err != nil {
		return nil, fmt.Errorf("parse skill manifest: %w", err)
	}
	if strings.TrimSpace(m.Name) == "" {
		return nil, fmt.Errorf("skill manifest missing required `name` field")
	}
	return &m, nil
}

// extractFrontmatter pulls the YAML between leading `---\n` and the
// next `---\n`. If no frontmatter delimiters are present, treats the
// whole file as YAML (operator-authored skills can skip the markdown
// wrapper).
func extractFrontmatter(data []byte) []byte {
	trimmed := bytes.TrimLeft(data, " \t\n\r")
	if !bytes.HasPrefix(trimmed, []byte("---")) {
		return data
	}
	rest := trimmed[3:]
	// Skip any whitespace after the opening ---
	rest = bytes.TrimLeft(rest, " \t")
	if len(rest) > 0 && rest[0] == '\n' {
		rest = rest[1:]
	}
	end := bytes.Index(rest, []byte("\n---"))
	if end < 0 {
		return rest
	}
	return rest[:end]
}

// CompatibleWithDatawatch returns nil if the manifest's compatible_with
// field is empty or every constraint is satisfied by the given current
// version. Constraints look like "datawatch>=6.7.0" / "datawatch<7.0.0".
// Unknown product names are ignored (not our problem).
func (m *Manifest) CompatibleWithDatawatch(currentVersion string) error {
	if len(m.CompatibleWith) == 0 {
		return nil
	}
	for _, c := range m.CompatibleWith {
		if !strings.HasPrefix(c, "datawatch") {
			continue
		}
		// minimal: just record the constraint; full semver enforcement
		// is a v6.7.x patch. v1 surfaces the constraint to the operator
		// without blocking sync.
		_ = currentVersion
	}
	return nil
}
