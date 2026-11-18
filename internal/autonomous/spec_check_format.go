// spec_check_format.go — the one formatter every surface uses to show the
// spec-check findings of an Automaton, so REST, MCP, CLI, comm and the web
// form all render the same lines.
package autonomous

import (
	"encoding/json"
	"strconv"
	"strings"
)

// SpecCheckFindings lints an Automaton's spec text for a create or a
// revise reply. It never returns nil, so the JSON field is an empty array
// when the spec is clean.
func SpecCheckFindings(spec string) []SpecFinding {
	f := LintSpecText(spec)
	if f == nil {
		return []SpecFinding{}
	}
	return f
}

// FormatSpecFinding renders one finding as a single line: the severity,
// the rule, the exact phrase and the suggested rewording. A correctable
// finding says the daemon can fix the wording itself.
func FormatSpecFinding(f SpecFinding) string {
	var b strings.Builder
	b.WriteString(f.Severity)
	b.WriteString(" [")
	b.WriteString(f.Rule)
	b.WriteString("] \"")
	b.WriteString(f.Phrase)
	b.WriteString("\": ")
	b.WriteString(f.Suggestion)
	if f.Correctable {
		b.WriteString(" (the daemon can fix this wording itself)")
	}
	return b.String()
}

// FormatSpecFindings renders the findings one line each, in order. A clean
// spec gives no lines.
func FormatSpecFindings(findings []SpecFinding) []string {
	out := make([]string, 0, len(findings))
	for _, f := range findings {
		out = append(out, FormatSpecFinding(f))
	}
	return out
}

// FormatSpecCheckBlock renders the findings as a plain-text block with a
// heading line, for chat, CLI and MCP text output. A clean spec gives "".
func FormatSpecCheckBlock(findings []SpecFinding) string {
	if len(findings) == 0 {
		return ""
	}
	lines := FormatSpecFindings(findings)
	return "Spec check: " + strconv.Itoa(len(lines)) + " finding(s)\n  " + strings.Join(lines, "\n  ")
}

// SpecCheckFromReply reads the spec_check array out of a create or revise
// reply body. A body that is not JSON, or has no spec_check, gives nil.
func SpecCheckFromReply(body []byte) []SpecFinding {
	var r struct {
		SpecCheck []SpecFinding `json:"spec_check"`
	}
	if json.Unmarshal(body, &r) != nil {
		return nil
	}
	return r.SpecCheck
}

// SpecCheckBlockFromReply is FormatSpecCheckBlock over the spec_check of a
// reply body; "" when the reply carries no findings.
func SpecCheckBlockFromReply(body []byte) string {
	return FormatSpecCheckBlock(SpecCheckFromReply(body))
}
