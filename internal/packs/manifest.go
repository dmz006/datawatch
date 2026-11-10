package packs

import (
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Automaton kinds a pack may apply to. They mirror the PRD kinds the
// autonomous package recognises (feature, bug, other, maintenance).
const (
	KindFeature     = "feature"
	KindBug         = "bug"
	KindOther       = "other"
	KindMaintenance = "maintenance"
)

var validKinds = map[string]bool{
	KindFeature:     true,
	KindBug:         true,
	KindOther:       true,
	KindMaintenance: true,
}

// semverPattern matches a semantic version with an optional pre-release and
// build suffix (MAJOR.MINOR.PATCH), per https://semver.org.
var semverPattern = regexp.MustCompile(
	`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)` +
		`(?:-((?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*)(?:\.(?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*))*))?` +
		`(?:\+([0-9a-zA-Z-]+(?:\.[0-9a-zA-Z-]+)*))?$`)

// Pack is the decoded form of a pack.yaml manifest.
type Pack struct {
	Name        string   `yaml:"name"`
	Version     string   `yaml:"version"`
	Description string   `yaml:"description"`
	Kinds       []string `yaml:"kinds"`
	Tasks       []Task   `yaml:"tasks"`
	Templates   []string `yaml:"templates"`
	Inputs      []Input  `yaml:"inputs"`
	// Digest is the content digest LoadPackFolder computes over the pack
	// folder's files in sorted order; it is set by the loader, never read
	// from pack.yaml.
	Digest string `yaml:"-"`
	// Dir is the absolute pack folder LoadPackFolder read the pack from; it is
	// set by the loader, never read from pack.yaml. Rule checks use it to find
	// verification scripts.
	Dir string `yaml:"-"`
}

// Task is one standard task a pack contributes.
type Task struct {
	Title              string   `yaml:"title"`
	Prompt             string   `yaml:"prompt"` // path relative to the pack folder
	VerificationScript string   `yaml:"verification_script"`
	DefaultTier        string   `yaml:"default_tier"`
	ReadOnly           bool     `yaml:"read_only"`
	ReportPath         string   `yaml:"report_path"`
	Tools              []string `yaml:"tools"`
	FileScopes         []string `yaml:"file_scopes"`
}

// Input is a value the operator supplies when the pack is instantiated.
type Input struct {
	Name     string `yaml:"name"`
	Type     string `yaml:"type"`
	Default  string `yaml:"default"`
	Required bool   `yaml:"required"`
	Secret   bool   `yaml:"secret"`
}

// ParseManifest decodes a pack.yaml document and checks the fields the
// manifest cannot be used without: a name, a semantic version, known Automaton
// kinds, and unique task titles. It does not run pack rule checks.
func ParseManifest(data []byte) (*Pack, error) {
	var p Pack
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("pack manifest: %w", err)
	}

	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" {
		return nil, fmt.Errorf("pack manifest: name is required")
	}

	p.Version = strings.TrimSpace(p.Version)
	if p.Version == "" {
		return nil, fmt.Errorf("pack manifest: version is required")
	}
	if !semverPattern.MatchString(p.Version) {
		return nil, fmt.Errorf("pack manifest: version %q is not a semantic version", p.Version)
	}

	for _, k := range p.Kinds {
		if !validKinds[strings.TrimSpace(k)] {
			return nil, fmt.Errorf("pack manifest: unknown kind %q", k)
		}
	}

	seen := make(map[string]bool, len(p.Tasks))
	for _, t := range p.Tasks {
		title := strings.TrimSpace(t.Title)
		if seen[title] {
			return nil, fmt.Errorf("pack manifest: duplicate task title %q", title)
		}
		seen[title] = true
	}

	return &p, nil
}
