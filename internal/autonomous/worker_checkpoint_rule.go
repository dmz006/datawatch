// workerCheckpointRule.go — the single source for the rule that keeps
// automated Automaton task workers out of the Checkpoints sections of
// AGENT.md / CLAUDE.md (or whatever file the operator configures as the
// rules file). Workers were copying dated "## Checkpoints" entries from
// the rules files into their runs, so those sections mixed human and
// machine entries and the daemon had to relocate them. The rule is
// appended to every worker prompt that gets a rules block
// (rules_context.go, withRulesContextBlock); the planning block used
// by decompose does not carry it.
//
// The text is static instruction only (no repo reads, no growth with
// repo contents), in the style of boundedLoadStepRule.
// User-visible wording names Automaton / Automata.
package autonomous

import "strings"

// workerCheckpointRuleMarker is the leading heading of the rule block;
// a prompt that already holds it is never given a second copy.
const workerCheckpointRuleMarker = "--- Task worker rules: no checkpoints in the rules files ---"

// workerCheckpointRule is the single source for the no-checkpoints-in-
// the-rules-files rule appended to task worker prompts.
const workerCheckpointRule = "As a worker of an automated Automaton task you must not add, edit or remove checkpoint, progress-note or status entries in AGENT.md, CLAUDE.md, or the configured rules file, and you must not copy the Checkpoints section of those files into your work or your notes. Checkpoint entries in those files are written only by the human operator and by the supervisor. If you need to leave a progress note, write it to CHECKPOINT.md at the root of your working directory instead — the daemon already relocates that file. Writing checkpoint entries into AGENT.md or CLAUDE.md is a defect in the run."

// withWorkerCheckpointRule appends the no-checkpoints rule to spec
// exactly once: it is a no-op when the marker line is already present,
// so a retried task is never given it twice.
func withWorkerCheckpointRule(spec string) string {
	if strings.Contains(spec, workerCheckpointRuleMarker) {
		return spec
	}
	return spec + "\n\n" + workerCheckpointRuleMarker + "\n" + workerCheckpointRule
}
