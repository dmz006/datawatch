// Explicit-model policy for Claude Code launches.
//
// Every autonomous launch path that starts a Claude Code session resolves
// its model here, so no session can start without a model choice that an
// operator can see and name:
//
//   - A model the operator explicitly named always passes through
//     unchanged, with its source recorded as "operator".
//   - When nothing names one, the daemon default is sonnet for normal
//     work and haiku for the mechanical task types (the types the default
//     task-routing table routes to the mechanical row).
//   - Premium models (Opus/Fable class) are only ever permitted because
//     the operator explicitly named them; they are never a daemon default
//     and never a resolver output.
package autonomous

import "strings"

// Where the resolved model comes from.
const (
	// ModelSourceOperator: the operator (or the task/story/Automaton level
	// that carries the operator's explicit choice) named this model.
	ModelSourceOperator = "operator"
	// ModelSourceDaemonDefault: nothing named a model; the daemon default
	// row applied.
	ModelSourceDaemonDefault = "daemon-default"
)

// Daemon default models for Claude Code sessions. Neither is premium.
const (
	// DefaultClaudeModel is used for a Claude Code session that names no
	// model and whose task type is not a mechanical type.
	DefaultClaudeModel = "sonnet"
	// MechanicalClaudeModel is used for a Claude Code session that names
	// no model and whose task type is a mechanical type.
	MechanicalClaudeModel = "haiku"
)

// IsMechanicalTaskType reports whether the task type is in the mechanical
// set (mechanicalTaskTypes, defined in task_routing_classify.go): docs
// set, changelog entry, parity-issue draft, bounded load step, version
// note, locale strings, formatting cleanup, tests — the task types the
// daemon does by itself, routed to the mechanical row of the default
// task-routing table.
func IsMechanicalTaskType(taskType string) bool {
	return mechanicalTaskTypes[taskType]
}

// IsPremiumModel reports whether the model name names a premium
// (Opus/Fable class) model. Premium models are only ever allowed because
// the operator explicitly named them; the resolver never emits one.
func IsPremiumModel(model string) bool {
	l := strings.ToLower(model)
	return strings.Contains(l, "opus") || strings.Contains(l, "fable")
}

// ResolveClaudeModel applies the explicit-model policy to one Claude Code
// launch. namedModel is the model the operator (task/story/Automaton
// level) named, or empty when nobody named one; taskType is the task's
// routing task type. It returns the model to launch with and its source:
// a non-empty named model passes through unchanged with source
// ModelSourceOperator; an empty named model resolves to
// MechanicalClaudeModel on a mechanical task type and DefaultClaudeModel
// otherwise, both with source ModelSourceDaemonDefault. Whitespace-only
// names are treated as empty.
func ResolveClaudeModel(namedModel, taskType string) (model string, source string) {
	if strings.TrimSpace(namedModel) != "" {
		return namedModel, ModelSourceOperator
	}
	if IsMechanicalTaskType(taskType) {
		return MechanicalClaudeModel, ModelSourceDaemonDefault
	}
	return DefaultClaudeModel, ModelSourceDaemonDefault
}
