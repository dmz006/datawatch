<!-- Filed 2026-11-08 as https://github.com/dmz006/datawatch-app/issues/404 (release v9.112.10). -->
# Fix the built-in task templates so a decomposed Automaton's final gate and version note tasks pass the verifier

## Affected area

The built-in task-template pack and the pipeline that turns it into runnable tasks,
all in the autonomous engine: `BuiltInTaskTemplates` in
internal/autonomous/task_templates_defaults.go (the two tasks at the center of this
story are `final_gate` and `version_bump_left_to_merge`), the merge-and-apply logic
in internal/autonomous/task_templates.go (`EffectiveTaskTemplates`,
`ApplyTaskTemplates`, `applyTemplatesToStory`, the new
`validateTemplateVerificationScript` / `validateTaskTemplateVerificationScripts`),
and the config-write guard in internal/autonomous/api.go that runs the same check
when a `task_templates` block is saved. It meets the executor's verification side:
the verifier's prestart check that refuses a task whose `verification_script` is not
a repo-relative script file, and the read-only report check that verifies a
read-only task by the presence of its report file. The surrounding surfaces the
change touches are the config surfaces for `autonomous.task_templates` (YAML, REST
config write, and the decompose-lint Settings card and CLI/comm/MCP verbs that carry
that block), the docs set (docs/howto/automaton-decompose-lint.md,
docs/howto/autonomous-planning.md, docs/operations.md,
docs/datawatch-definitions.md, docs/config-reference.yaml, docs/api/openapi.yaml),
and the tests that pin the behavior (internal/autonomous/task_templates_test.go,
internal/autonomous/task_templates_script_command_test.go,
internal/autonomous/readonly_unverifiable_test.go). On the mobile side this lands
on the already-planned decompose-lint Settings card tracked in the adjacent draft
2026-11-03-decompose-lint-and-templates.md; the app inherits the daemon's
accept/reject behavior through the config contract rather than through any
app-side logic of its own.

## Current behavior after this work

The story's core gap is closed for the two tasks that used to fail. Before it, the
built-in `final_gate` template carried its check as a `verification_script` holding
a command string, and the verifier's prestart — which for a verification script
requires one repo-relative script file — rejected it, so the story's closing gate
could never pass; and the version-bump template described work in a way the
verifier could not treat as done. Now `final_gate` is a read-only template
(`read_only: true`) whose `spec` tells the task to write its verdict and evidence
to one report file, `.datawatch/reports/<feature>-final-gate.txt` (the feature
name slugged into the file name), declared in the template's `report_path`, and it
carries no `verification_script` at all; a read-only task with a usable report path
is verified by the read-only report check, so the gate passes the verifier by
producing its report instead of by naming a command that was never a file. The
version note (`version_bump_left_to_merge`) is a plain `note`-kind, no-files
record: it states that the release version is the next patch above main, resolved
at merge time, and it never writes a file and never touches a version field, so
the verifier has nothing to run against it and nothing to hold for it. Both
templates keep the two hard properties the pack text is written to hold for every
feature name: no blocking decompose-lint finding, and no hosting-CLI side effects.

The work also closed a class of gap around operator-saved templates: a
`verification_script` given as a command string rather than a single repo-relative
script file is now refused. At config write time the daemon rejects the
`task_templates` block with a 400 whose error names the offending template id and
says a verification script must be a repo-relative script file (for example
scripts/verify.sh); the check looks for whitespace, shell operators, expansion,
quoting, absolute paths, `..` segments, and the project root, and inspects the
value untrimmed since it is copied verbatim onto the task. At apply time the same
check runs again and a template it rejects is skipped: it is never instantiated
into a task that could not be verified, and a template whose report path is
invalid is refused the same way. A nil or empty `verification_script` remains the
normal "carries no script" case and is not a command. The behavior is pinned by
tests in task_templates_test.go (the gate verifies through its report, the two
hard properties hold for every feature name a story can carry),
task_templates_script_command_test.go (each rejected form, the accepted forms,
the apply-time skip), and readonly_unverifiable_test.go (a template whose report
path fails validation is never instantiated as an unverifiable read-only task).
The docs set was updated to state all of this — the howtos and operations say the
final gate is read-only and verified by its report under .datawatch/reports/, the
version note is a note the verifier reads rather than runs, and a command string
in `verification_script` is refused at config write — and the config reference
records the same constraint on the field.

## Expected behavior

The expected state after this work is the one described above and nothing more:
every task the built-in pack appends to a decomposed Automaton — including the
closing gate and the release version note — starts in a shape the verifier can
actually verify, the daemon cannot be made to save a template whose
`verification_script` is not a repo-relative script file, and the docs describe
exactly that. The parity gap on the daemon side for the final gate and the version
note as such is therefore closed, not left: the tasks pass the verifier, an
unverifiable template shape is rejected at save time, and the operator-facing
surfaces (config, docs, tests) all agree on the rule.

## Gaps this work leaves

The first left-over gap is the path the write-time guard does not cover: a
`task_templates` block written directly into config.yaml by hand. The
command-string check lives in the autonomous engine (the REST write path and the
apply-time skip); the config package's own validation does not inspect
`verification_script` for command strings. So a hand-edited config that carries
such a value still loads cleanly, and the story it applies to simply loses that
template — the skip at apply time is silent: it names nothing, logs nothing,
records no finding and produces no hold or warning the operator can see. A story
whose operator expected a verification task from that template goes ahead without
one, with no surface anywhere telling them the template was dropped.

The second left-over gap is on the app side, and it is the same gap in new
clothing: the decompose-lint Settings card tracked in
2026-11-03-decompose-lint-and-templates.md will, when it ships, let an operator
save template definitions on Android and iOS. Whatever the app does, the daemon's
400 on a command-string `verification_script` is the contract it must surface
cleanly (an error that names the template and the rule, not a bare failure), and
the card's display of the built-in pack should show `final_gate` the way it now
is — read-only, report-verified, no verification script — rather than the shape
that used to be saved and rejected. That confirmation has not been recorded on
the app issue yet.

A smaller asymmetry worth noting: the write-time guard checks the templates
in a saved block, and the apply-time guard re-checks and skips, but there is no
third place — no decompose-lint rule, no dry-run output, no per-Automaton panel
entry — that reports which saved templates are being skipped on a given apply, so
an operator only discovers the drop by noticing the expected task is missing from
a story.

## Suggested next step

File this as a datawatch (daemon) issue since the uncovered surfaces are all
daemon-side, cross-referencing the app-side draft
2026-11-03-decompose-lint-and-templates.md for the Settings card that will expose
the contract. In that issue, close the config-load gap first: extend the
command-string rule into the config package's template validation (or call the
autonomous engine's `validateTaskTemplateVerificationScripts` from the config
load/validate path) so a hand-edited config.yaml cannot carry a template shape the
daemon would refuse at apply time, with a config-level test for each rejected form
mirroring task_templates_script_command_test.go. Then make the apply-time skip
say something: when `applyTemplatesToStory` refuses a template, record it where
the operator can see it — a decompose-lint `warn` finding or an equivalent labeled
note on the Automaton — naming the template id and the reason, so a story that
silently lost a verification step is one the operator can find instead of guess
at. And finally confirm on the app side that the card surfaces the 400 with the
template id and rule intact and displays `final_gate` as report-verified with no
verification script — recording that confirmation on the app-side issue as the
closure of this follow-up. All of these are additive; none of them change the
behavior this work shipped, the gate and the version note pass the verifier
exactly as the docs now describe.

## Filing record

Not yet filed. File as a datawatch daemon issue (the config-load and apply-skip
surfaces live in internal/config and internal/autonomous), cross-referencing the
app-side draft 2026-11-03-decompose-lint-and-templates.md for the Settings card;
record the URL and date here when filed. Drafted 2026-11-08, with the code-level
claims — the report-only `final_gate` and note-only version template in
task_templates_defaults.go, the write-time and apply-time guards in api.go and
task_templates.go, the config package carrying no command-string check, and the
matching howto, operations, definitions, config-reference and openapi wording —
checked against the current tree on the same day.
