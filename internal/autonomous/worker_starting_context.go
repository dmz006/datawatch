// Worker starting context — deterministic assembly of the text a worker
// session starts with, from the caller-supplied pieces (role, profile,
// task spec, constraints, rules digest, rules full text). Output order is
// fixed, so everything before the "Task" section is identical for the same
// role and profile: no timestamps, run identifiers or session-specific
// paths may be mixed in by the surrounding pipeline, and the builder never
// invents any. The context level comes from the profile's Context field:
// minimal carries the constraints only, digest adds the rules digest, full
// adds the rules digest and the full rules text. The task spec always goes
// in the final "Task" section.
package autonomous

import (
	"fmt"
	"strings"

	"github.com/dmz006/datawatch/internal/workerprofile"
)

// StartingContextInput carries the pieces of a worker starting context.
// The caller supplies RulesDigest from BuildRulesDigest (rules_digest.go)
// and RulesFull with the verbatim rules file text.
type StartingContextInput struct {
	Role        string
	Profile     workerprofile.WorkerProfile
	TaskSpec    string
	Constraints []string
	RulesDigest string
	RulesFull   string
}

// startingContextProfileLine renders a profile as one deterministic line:
// fixed field order, fixed separators, so the same profile always yields
// the same text.
func startingContextProfileLine(p workerprofile.WorkerProfile) string {
	servers := strings.Join(p.MCP.Servers, ",")
	allow := strings.Join(p.Tools.Allow, ",")
	return fmt.Sprintf("profile=%q context=%s mcp=%s servers=%q tools=%s allow=%q skills=%t model=%q effort=%q",
		p.Name, p.Context, p.MCP.Mode, servers, p.Tools.Mode, allow, p.Skills, p.Model, p.Effort)
}

// startingContextConstraints renders the Constraints section: a "- "-prefixed
// line per constraint in the given order, or "(none)" when there are none.
func startingContextConstraints(constraints []string) string {
	var b strings.Builder
	if len(constraints) == 0 {
		b.WriteString("(none)\n")
		return b.String()
	}
	for _, c := range constraints {
		b.WriteString("- " + c + "\n")
	}
	return b.String()
}

// BuildWorkerStartingContext assembles the starting context text for a
// worker session deterministically. Section order is fixed:
// "Role", "Profile", "Constraints", then — at the digest and full levels —
// "Rules digest", and at the full level — "Rules"; the "Task" section is
// always last. A level that carries a blank digest or rules text omits
// that section; constraints are always present.
func BuildWorkerStartingContext(in StartingContextInput) string {
	level := in.Profile.Context
	var b strings.Builder

	b.WriteString("Role\n")
	b.WriteString(in.Role + "\n\n")

	b.WriteString("Profile\n")
	b.WriteString(startingContextProfileLine(in.Profile) + "\n\n")

	b.WriteString("Constraints\n")
	b.WriteString(startingContextConstraints(in.Constraints))
	b.WriteString("\n")

	if (level == workerprofile.ContextDigest || level == workerprofile.ContextFull) && in.RulesDigest != "" {
		b.WriteString("Rules digest\n")
		b.WriteString(strings.TrimRight(in.RulesDigest, "\n") + "\n\n")
	}

	if level == workerprofile.ContextFull && in.RulesFull != "" {
		b.WriteString("Rules\n")
		b.WriteString(strings.TrimRight(in.RulesFull, "\n") + "\n\n")
	}

	b.WriteString("Task\n")
	b.WriteString(strings.TrimRight(in.TaskSpec, "\n") + "\n")

	return b.String()
}
