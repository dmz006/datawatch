package genartifacts

import (
	"embed"
	"fmt"
	"path/filepath"
	"strings"
	"text/template"
)

//go:embed templates/*.tmpl
var defaultTemplates embed.FS

// Kind is one generated artefact.
type Kind string

// The artefact kinds.
const (
	GateReport     Kind = "gate_report"
	ChangelogDraft Kind = "changelog_draft"
	ParityDraft    Kind = "parity_draft"
	TrackerRows    Kind = "tracker_rows"
	RegistryRow    Kind = "registry_row"
)

// Kinds lists every kind in a stable order.
func Kinds() []Kind {
	return []Kind{GateReport, ChangelogDraft, ParityDraft, TrackerRows, RegistryRow}
}

// Valid reports whether k is a known kind.
func (k Kind) Valid() bool {
	for _, x := range Kinds() {
		if x == k {
			return true
		}
	}
	return false
}

// IDPlaceholder stands for the automaton id in a tracked path.
const IDPlaceholder = "<automaton-id>"

// DefaultPaths are the default tracked paths per kind, relative to the project
// root.
func DefaultPaths() map[Kind]string {
	return map[Kind]string{
		GateReport:     "docs/plans/reports/" + IDPlaceholder + "-gate.md",
		ChangelogDraft: "docs/plans/drafts/" + IDPlaceholder + "-changelog.md",
		ParityDraft:    "docs/plans/parity-issues/" + IDPlaceholder + ".md",
		TrackerRows:    "docs/plans/drafts/" + IDPlaceholder + "-tracker-rows.md",
		RegistryRow:    "docs/plans/drafts/" + IDPlaceholder + "-registry-row.md",
	}
}

// OverridePath is where a project places its own template for a kind,
// relative to the project root.
func OverridePath(k Kind) string {
	// Build the path from the matching entry of the fixed kind list, not from
	// the caller's value, so a kind that arrives from a request can never
	// shape a path: an unknown kind yields no path at all.
	for _, known := range Kinds() {
		if known == k {
			return filepath.ToSlash(filepath.Join(".datawatch", "templates", string(known)+".tmpl"))
		}
	}
	return ""
}

// DefaultTemplate returns the embedded default template text of a kind.
func DefaultTemplate(k Kind) (string, error) {
	if !k.Valid() {
		return "", fmt.Errorf("genartifacts: unknown kind %q", k)
	}
	b, err := defaultTemplates.ReadFile("templates/" + string(k) + ".tmpl")
	if err != nil {
		return "", fmt.Errorf("genartifacts: default template %s: %w", k, err)
	}
	return string(b), nil
}

func templateFuncs() template.FuncMap {
	return template.FuncMap{
		// cell makes text safe inside one markdown table cell.
		"cell": func(s string) string {
			s = strings.ReplaceAll(s, "|", `\|`)
			s = strings.ReplaceAll(s, "\r", "")
			return strings.ReplaceAll(s, "\n", " ")
		},
	}
}

func parseTemplate(name, text string) (*template.Template, error) {
	return template.New(name).Funcs(templateFuncs()).Option("missingkey=error").Parse(text)
}

// Describe returns a one-line description of the kind's output format.
func Describe(k Kind) string {
	switch k {
	case GateReport:
		return "Markdown report of counts only: commands run, passed, failed, files changed, packages touched, tests added, settings, routes and locale keys."
	case ChangelogDraft:
		return "CHANGELOG.md-style entry whose heading carries the placeholder vX.Y.Z; the release version is taken at merge time."
	case ParityDraft:
		return "Markdown parity issue draft: What changed, Settings to add to the mobile app, REST contract, New locale keys, Screens."
	case TrackerRows:
		return "Rows for the table in docs/testing-tracker.md, one per surface touched."
	case RegistryRow:
		return "Draft row for the table in docs/plans/README.md."
	}
	return ""
}
