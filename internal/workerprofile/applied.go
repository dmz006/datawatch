// applied.go records, per worker-profile field, what a non-Claude-Code
// backend actually honoured at launch.
//
// A worker profile can set six fields (mcp, tools, skills, context,
// model, effort). Claude Code honours all of them in its own launch
// path. The other backends honour none of them, or only some (e.g. the
// model pin when the backend takes one). RecordApplied turns a
// resolved profile plus the set of fields a backend claims to honour
// into one deterministic outcome per field, so the session start record
// can show the operator exactly which profile fields the backend
// dropped instead of silently ignoring them.
package workerprofile

import (
	"fmt"
	"sort"
	"strings"
)

// FieldNames lists the profile fields RecordApplied reports on, in the
// stable order every outcome list and FormatApplied summary uses.
var FieldNames = []string{"mcp", "tools", "skills", "context", "model", "effort"}

// CannotHonourReason is the Reason recorded on every field the backend
// does not support.
const CannotHonourReason = "backend cannot honour this field"

// FieldOutcome is the per-field record of what a backend did with one
// worker-profile field at launch.
type FieldOutcome struct {
	Field   string `json:"field"`
	Applied bool   `json:"applied"`
	// Reason explains a dropped field; empty when Applied is true.
	Reason string `json:"reason,omitempty"`
}

// RecordApplied returns one outcome per profile field, in FieldNames
// order, for the profile p and the backend's supported set. A field
// that is missing from supported, or whose value is false, is recorded
// as not applied with Reason "backend cannot honour this field"; a
// field supported is recorded as applied. A nil or empty supported map
// drops every field. The profile argument is part of the signature so
// callers record the profile they actually passed to the backend.
func RecordApplied(p WorkerProfile, supported map[string]bool) []FieldOutcome {
	outcomes := make([]FieldOutcome, 0, len(FieldNames))
	for _, name := range FieldNames {
		if supported[name] {
			outcomes = append(outcomes, FieldOutcome{Field: name, Applied: true})
			continue
		}
		outcomes = append(outcomes, FieldOutcome{
			Field:   name,
			Applied: false,
			Reason:  CannotHonourReason,
		})
	}
	return outcomes
}

// FormatApplied renders outcomes as a stable one-line summary in
// FieldNames order (any field outside FieldNames sorts alphabetically
// after it). Each entry is "field=applied" or
// "field=unapplied: <reason>", so the output for a given outcome list
// never depends on input order or map iteration.
func FormatApplied(outcomes []FieldOutcome) string {
	sorted := make([]FieldOutcome, len(outcomes))
	copy(sorted, outcomes)
	rank := make(map[string]int, len(FieldNames))
	for i, name := range FieldNames {
		rank[name] = i
	}
	sort.SliceStable(sorted, func(i, j int) bool {
		ri, oki := rank[sorted[i].Field]
		rj, okj := rank[sorted[j].Field]
		switch {
		case oki && okj:
			return ri < rj
		case oki:
			return true
		case okj:
			return false
		default:
			return sorted[i].Field < sorted[j].Field
		}
	})
	parts := make([]string, 0, len(sorted))
	for _, o := range sorted {
		if o.Applied {
			parts = append(parts, o.Field+"=applied")
			continue
		}
		reason := o.Reason
		if reason == "" {
			reason = "unapplied"
		}
		parts = append(parts, fmt.Sprintf("%s=unapplied: %s", o.Field, reason))
	}
	return strings.Join(parts, " ")
}
