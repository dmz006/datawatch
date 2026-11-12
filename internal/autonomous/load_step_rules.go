// load_step_rules.go — the bounded CPU-load / stress-step rule and the
// static rules text the decompose prompt carries about it.
//
// Rule (a plan defect if a task spec asks for a load step without the
// bounds): a CPU-load or stress step a task asks for — a busy loop, a
// stress test, a load or soak test, or any other command that pins the
// CPU — must be bounded before it runs:
//
//   - run it niced (nice -n 19) so it yields to the system's other work;
//   - use at most a quarter of the cores (cap the worker count at
//     nproc/4, minimum one);
//   - give it its own hard timeout (a bounded `timeout <duration> ...`
//     wrapper) so it always dies on its own;
//   - clean up its children with a trap on EXIT INT TERM so no
//     orphaned load survives; and
//   - it must never outlive its owning task — when the task ends, the
//     load ends with it.
//
// The rule text lives in a single source (this const) and is carried
// into three operator-visible surfaces that all say the same thing:
//
//   - here (boundedLoadStepRule) — the single source of the wording;
//   - the built-in bounded_load_step task template
//     (task_templates_defaults.go) — the posture note a fresh story
//     starts with;
//   - Manager.loadStepSuffix — the rules text appended to the decompose
//     prompt (both decompose-prompt sites in manager.go) so the planner
//     is told the rule before it writes the JSON plan;
//   - the decompose-lint unbounded_load_step rule
//     (decompose_lint_rules.go) — a warn-severity finding when a task
//     spec asks for a load step, so the lint hook flags specs the
//     decomposer should bound.
//
// The text is static instruction only (no repo reads, no growth with
// repo contents), in the style of verificationScriptSuffix.
//
// User-visible wording names Automaton / Automata.
package autonomous

// boundedLoadStepRule is the single source for the bounded
// CPU-load / stress-step rule. It is embedded verbatim in the
// built-in bounded_load_step task template
// (task_templates_defaults.go) and in Manager.loadStepSuffix (the
// decompose-prompt rules text). The decompose-lint unbounded_load_step
// rule (decompose_lint_rules.go) restates the compact version of the
// same bounds in its finding message.
const boundedLoadStepRule = "Any CPU-load or stress step a task asks for — a busy loop, a stress test, a load or soak test, or any other command that pins the CPU — must be bounded before it runs: run it niced (nice -n 19) so it yields to the system's other work; use at most a quarter of the cores (cap the worker count at nproc/4, minimum one); give it its own hard timeout (a bounded \"timeout <duration> ...\" wrapper) so it always dies on its own; clean up its children with a trap on EXIT INT TERM so no orphaned load survives; and it must never outlive its owning task — when the task ends, the load ends with it. Automaton plans tasks to leave the host as it found it, so an unbounded load step is a defect in the plan."

// loadStepSuffix returns the bounded CPU-load / stress-step rules block
// appended to the decompose prompt — the rules text the decomposer
// reads while planning the task graph. Gated on ProjectDir like
// rulesContextSuffix and verificationScriptSuffix: an empty ProjectDir
// means the decompose backend is headless, and there is no project to
// point the rules at, so the block is not appended.
//
// The text is the shared boundedLoadStepRule (the same bounds the
// built-in bounded_load_step task template and the decompose-lint
// unbounded_load_step rule restated the decomposer must not trip),
// under a small heading.
func (m *Manager) loadStepSuffix(prd *PRD) string {
	if prd.ProjectDir == "" {
		return ""
	}
	return "\n\n--- CPU-load and stress steps ---\n" + boundedLoadStepRule
}
