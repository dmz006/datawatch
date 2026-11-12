# The template-mode setting and the daemon run of the four daemon-capable templates leave a destructive Web-UI save, a CLI write gap, and a missing config-reference entry

**Filed 2026-11-09 as dmz006/datawatch-app#420, one consolidated issue for the three daemon task mode drafts** (shipped in v9.117.0).

## Affected area

The config side of the autonomous engine's task templates, in internal/config:
`TaskTemplatesConfig.Mode` (the new `autonomous.task_templates.mode` map, a
global template-id → mode map with the modes `llm`, `daemon` and `off`) in
internal/config/decompose_lint.go along with `TaskTemplateMode`,
`DaemonCapableTaskTemplateIDs`, `DefaultTaskTemplateModes`,
`EffectiveTaskTemplateMode` and the `Validate` rules (`final_gate` may never
be daemon; daemon is only legal for the four daemon-capable ids), wired into
`AutonomousConfig.TaskTemplates` in internal/config/config.go and pinned by
internal/config/task_template_mode_test.go. It meets the apply side in
`ApplyTaskTemplates` / `applyTemplate` in
internal/autonomous/task_templates.go, which marks each daemon-mode task
`DaemonRun` (the new bool on `Task` in internal/autonomous/models.go), and the
new daemon-run path in internal/autonomous/daemon_template_task.go
(`runDaemonTemplateTask`, `generateDaemonArtefact`, `daemonStoryRecord`,
`completeDaemonTemplateTask`, `failDaemonTemplateTask`), hooked into
`executeOne` in internal/autonomous/executor.go ahead of session spawn, with
the run pinned by internal/autonomous/daemon_template_task_run_test.go and the
artefact/completion by internal/autonomous/daemon_template_task_artefact_test.go.
The story's structural checks were taught to skip daemon tasks: the per-task
rule loop in internal/autonomous/decompose_lint.go
(`daemonRunsTask`), the rule compilation in
internal/autonomous/decompose_lint_rules.go, the per-task coverage rule in
internal/autonomous/plan_coverage_findings.go, and the skips pinned by
internal/autonomous/daemon_mode_checks_test.go. The operator-facing surfaces
the change lands on are the config block (YAML), the decompose-lint config
read/write (`GET`/`PUT /api/autonomous/decompose-lint/config`, i.e. REST, the
MCP `autonomous_decompose_lint_config_set` tool and the comm `autonomous lint
config(-set)` verbs), the CLI read-only path (`datawatch autonomous lint
config`), the Web UI's decompose-lint Settings card
(`settingsDecomposeLintLoad/Save` in internal/server/web/app.js), and the
docs set updated with the mode (docs/operations.md's "Task template modes"
section, docs/howto/automaton-decompose-lint.md,
docs/howto/autonomous-planning.md, docs/datawatch-definitions.md). The
mobile side inherits from the as-yet-unshipped decompose-lint Settings card
tracked in the adjacent draft 2026-11-03-decompose-lint-and-templates.md.

## Current behavior after this work

The measured waste — twenty-five template task sessions (changelog entries,
parity-issue drafts, bounded-load and version-note tasks) spending full model
sessions on work that is mechanical — is closed for the four templates that
can be done without a model. The daemon now carries them directly. At each
apply, the mode map resolves per template id (the operator's value, else the
shipped default, else `llm`), and the shipped defaults already put
`changelog_entry`, `parity_issue_draft`, `bounded_load_step` and
`version_bump_left_to_merge` on `daemon`, while `docs_set` and `final_gate`
stay `llm`. `ApplyTaskTemplates` marks each daemon-mode task
`DaemonRun: true`, and when the executor reaches such a task it performs it
before any session spawn: `changelog_entry` and `parity_issue_draft` merge the
saved change records of the completed tasks in the same story and write one
generated artefact (the changelog draft or the parity-issue draft) under the
project directory, subject to the same generated-artefact edit-protection — a
file already edited by the operator, or a kind switched off in the
generated-artefact settings, is skipped with the reason recorded in the
task's decision note and the task still completes — while a failure the daemon
cannot make (for example no project directory) marks the task failed.
`bounded_load_step` and `version_bump_left_to_merge` write no file; their
rendered spec text is recorded as the story's decision note. Every such task
completes through the same save/status path as a worker task, so the story
roll-up counts it, and there is no worker session in the session list for it:
no model spend, no verifier, no retry loop. Because a handler performs a
daemon task rather than a judge, the structural checks that assume a session
— decompose-lint's per-task rules, the per-task verification-script rule, the
report-presence rule and the file-token rule — skip it; the exemption only
matters to an operator who flips such a template back to `llm`, at which point
the checks apply to it like any other task. `mode` is validated on every
write surface: unknown modes are refused, daemon is refused for `final_gate`
and for any id without a daemon handler, and the config package's own load
path runs the same `Validate`, so a hand-edited `config.yaml` carrying an
illegal mode does not load. The docs set was corrected in the same work:
operations.md carries the access table and the explicit warning that every
write replaces the `task_templates` block whole (a write that carries only
`mode` resets `enabled` to false and drops the `templates` and `projects`
overrides), and it states there is no per-project mode — the `projects` map
only switches template application on or off per project.

## Expected behavior

An operator who measured twenty-five billable model sessions on mechanical
closing tasks expects those sessions gone by default, with the stories still
closing exactly as before, and expects the split to be per-template and
changeable without touching the other members of the block: flip
`changelog_entry` back to `llm` and the old session-run behavior — with its
verifier and checks — is back for that one template; turn one off and the story
loses both the task and the spend. Every surface the operator reads or writes
should tell the same story about which mode is in effect, and a save on any
surface must not silently wipe a mode map, a template override or a
project-level override the operator took care to set. That is the state this
work leaves for every surface except the three named below.

## Gaps this work leaves

The most serious left-over gap is on the Web UI, and it conflicts with what
this work's own docs claim. The daemon's write contract is replacement of the
whole `task_templates` block — `SetDecomposeLintConfig` in
internal/autonomous/api.go unmarshals the supplied object into a fresh
`TaskTemplatesConfig` and assigns it over the stored one — and
docs/operations.md's access table says of the Settings card that "the panel's
write carries the whole block." The panel's actual write does not:
`settingsDecomposeLintSave` in internal/server/web/app.js builds its body
with `task_templates: { enabled: <the card's checkbox> }` and sends that
block on every save of the card, including a save that only changed the
decompose-lint mode or the rules. Combined with the whole-block replacement
contract, saving the decompose-lint Settings card drops the operator's custom
`mode` map and any `templates` and `projects` overrides, reverting every
template to whatever the defaults say — and because the card's GET/PUT pair
is the only in-UI path to this block, an operator who saves their lint mode in
the browser and then reads `autonomous.task_templates.mode` back finds it
gone. The documentation's claim that the panel carries the whole block is
therefore inaccurate for the current payload, and the panel has no fields to
read or write a mode map at all, so the two agree on only one field. There is
a further asymmetry inside the panel itself even for `enabled`: the load side
reads `task_templates.enabled` and the save side carries `enabled`, but the
same save that legitimately changes `decompose_lint.mode` also carries the
`enabled` checkbox value the load side read — which today is safe only because
nothing else has written the block, while it is exactly the wipe described
above once any surface has set a non-default `mode` or overrides.

The second gap is the CLI write path. The access table and this work's own
code agree that `datawatch autonomous lint config-set` covers only the
`decompose_lint` block, so changing a mode from the CLI is n/a — the only
command-line lever is `decompose_lint` itself. An operator who keeps the rest
of the block on YAML/REST/MCP is fine, but an operator who wants to flip a
template mode from the terminal has no `config set`-shaped key
(`autonomous.task_templates.mode.<id>` reads, per the access table, but has no
write verb), and the Configuration Accessibility Rule this project holds itself
to lists CLI set as a required surface "if applicable." The block itself is
writable, so this reads more as a documented limitation than a bug — but the
n/a is a permanent one, and it is the one surface where the gap between
read-access and write-access is widest.

The third gap is on the mobile side and it is the first two gaps arriving
together. The decompose-lint Settings card the operator will get on Android
and iOS is still the one tracked in
2026-11-03-decompose-lint-and-templates.md; when it ships it will face the
same daemon contract, whole-block replacement, and if it replicates the PWA's
current payload it becomes the same destructive surface on two more clients.
Whatever the card does, the daemon's 400 wording when a daemon-refusing mode
is saved for the wrong id (naming the id, the refusing rule and the allowed
values, as `Validate` now does) is the contract the card must surface intact,
and the card's display should show the four daemon defaults — `changelog_entry`,
`parity_issue_draft`, `bounded_load_step`, `version_bump_left_to_merge` on
daemon, `docs_set` and `final_gate` on llm, with `final_gate` refusing daemon
— rather than the single `enabled` toggle the block currently reduces to on
the Web UI. That confirmation has not been recorded on the app-side issue.

A smaller, documented-but-unchosen gap worth naming because it is a
consequence of this work's own rule: `mode` has no per-project scope. The
access table says so and the config structure enforces it — `projects` is a
map of per-project `enabled` switches only — so an operator who wants daemon
for the four in one project and session-runs for them in another has to change
the global map and live with it for every other Automaton that applies, or
write a `templates` override that changes the task's shape to dodge a mode the
story needs. That is a deliberate limitation, not a bug, but it is the kind
of thing an operator discovers by reading the access table rather than by
being told up front, and it is the one part of this work's design where the
"per-template, per-scope" shape the rest of the config vocabulary supports
(the `projects` map in the same block) is explicitly not honored.

Finally, the docs set missed one of its own targets. The new key
`autonomous.task_templates.mode` is a config field, and this project's
documentation rule is that every new config field gets a corresponding entry in
`docs/config-reference.yaml`; the `task_templates` block in that file carries
`enabled`, `templates` and `projects` with their full comments, but no
`mode:` key, no default map, no mention of the allowed values or the
`final_gate` exclusion. Every other surface describes it — the definitions
page, the howto, operations, the config validator's error text — and a reader
who greps `config-reference.yaml` for the key they were told exists finds
`enabled`, `templates`, `projects` and nothing for `mode`.

## Suggested next step

File this as a datawatch (daemon) issue, cross-referencing the app-side draft
2026-11-03-decompose-lint-and-templates.md for the Settings card that will
inherit the contract, and in that issue: close the Web-UI contradiction first,
by extending `settingsDecomposeLintSave` to read the `task_templates` block
back (the load side already fetches it) and to carry its `enabled`, `mode`,
`templates` and `projects` fields through the save, so the panel's write
actually carries the whole block as operations.md already claims — the
alternative, teaching `SetDecomposeLintConfig` a merge, is worse: it would
make the documented "whole-block replacement" contract un-true for the same
call and would hide every real whole-block reset from the operator, so the
panel should conform to the contract, not the other way round; add a test on
the PWA save path (or the daemon's) that a card save that changes only
`decompose_lint.mode` leaves a non-default `mode` map, `templates` and
`projects` entries in `task_templates` untouched and, if the daemon side is
kept as the whole-block contract, that a bare `{enabled}` body still resets
them (documented, testable) so the two cannot silently diverge again. Then
add the `mode` entry to `docs/config-reference.yaml`'s `task_templates` block
— the map's three values, the four daemon defaults and the two llm ones, the
`final_gate` exclusion, and a line noting the block-replacement contract on
writes — so the four docs that already describe it and the key itself all
point at the same place. Then, in the same issue or a follow-up, decide the
CLI: either add a `config set`-shaped verb for `autonomous.task_templates.mode.<id>`
that carries the whole block (read-modify-write, the same pattern the block
documented) or write the n/a into the access table and config reference as a
permanent, deliberate limitation with the same reasoning the `projects`
limitation carries. Finally, on the app-side issue, record the confirmation
that the card will carry the whole block on save (or that the daemon-side fix
has landed before the card ships), that it surfaces the 400 with the id and
the refusing rule intact, and that it displays the four daemon defaults and
the `final_gate` daemon-refusal as the shipped contract — recording that line
on the app-side issue as the closure of this follow-up for the mobile
surface. All of these are additive or conforming to the contract this work
shipped; none of them change the behavior the daemon already has, the four
templates remain daemon-run by default and `docs_set` and `final_gate` remain
session-run, and the block-replacement rule stays the rule it is — the changes
are to make every surface that writes the block conform to that rule, to show
the new key in its own reference file, and to say plainly what the CLI will
and will not do.

## Filing record

Not yet filed. File as a datawatch daemon issue (the Web-UI payload, the
CLI's omission and the config-reference gap all live on the daemon side),
cross-referencing the app-side draft 2026-11-03-decompose-lint-and-templates.md
for the Settings card; record the URL and date here when filed. Drafted
2026-11-09, with the code-level claims — the mode map and its validation in
internal/config/decompose_lint.go, the `DaemonRun` mark in
internal/autonomous/task_templates.go, the daemon run and its skip in
internal/autonomous/daemon_template_task.go, internal/autonomous/decompose_lint.go
and internal/autonomous/plan_coverage_findings.go, the whole-block
replacement in internal/autonomous/api.go, the PWA save payload in
internal/server/web/app.js, the operations access table and the
config-reference block's current content — checked against the current tree
on the same day.
