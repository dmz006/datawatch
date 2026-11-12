# The daemon-run final gate leaves its two settings on YAML only, and its report contract is written "append" in two docs that do not append

**Filed 2026-11-09 as dmz006/datawatch-app#420, one consolidated issue for the three daemon task mode drafts** (shipped in v9.117.0).

## Affected area

The daemon's script gate — the part of the "daemon does it itself" work that
replaces the model session behind `final_gate` tasks. On the config side:
`AutonomousConfig.ScriptGateTimeoutSeconds`
(`autonomous.script_gate_timeout_seconds`, default 900, `0` = no cap, negative
refused on write) and `AutonomousConfig.ProjectGateCommand`
(`autonomous.project_gate_command`, default empty) in
internal/config/config.go, with the validation and resolution helpers in
internal/config/script_gate.go (`ValidateProjectGateCommand` — one line, no
NUL, at most 1024 bytes; `EffectiveProjectGateCommand` — policy value else
global, an explicit empty policy value unsetting the key for that project;
`EffectiveScriptGateTimeout`; `validateScriptGateTimeout`), the per-project
override `ProjectPolicy.ProjectGateCommand` in
internal/config/project_policy.go (validated in the policy's own `Validate`)
and its three-tier view in the resolver there, pinned by
internal/config/script_gate_timeout_test.go and
internal/config/project_gate_command_test.go. On the run side: the single
gate-launch site `RunScriptGate` in internal/autonomous/script_gate.go
(process-group kill, 2-second wait delay, output cap, `env -u TMUX -u
TMUX_TMPDIR`, scripts through `bash` resolved strictly inside the project's
`.datawatch/verify` with symlink-escape refusal, commands through `sh -c`),
the check resolution `ResolveGateCheck` in the same file (the task's own
`verification_script` first, then the project gate command, then the story's
other tasks' scripts in order), the daemon task path
`runScriptGateTask` / `finishScriptGateTask` / `writeGateReport` in
internal/autonomous/script_gate_task.go, and the hook at the top of
`executeOne` in internal/autonomous/executor.go ahead of any session spawn.
The operator-facing surfaces the change lands on are: YAML (both global keys,
validated at config load), the per-project policy write surface
(`PUT /api/projects/{name}/policy` in internal/server/projects.go, which runs
`config.ValidateProjectPolicy` and therefore already carries
`project_gate_command`, and its read view `GET /api/projects/{name}/policy/
effective`), the story's own state (a `script_gate` decision record and the
task's report file under `.datawatch/reports/`), and the docs set —
docs/operations.md's "The `final_gate` exception" paragraph, the
"### The script gate" section of docs/howto/automaton-decompose-lint.md, the
Task templates and Template mode entries of
docs/datawatch-definitions.md, and the two `##` sections of
docs/implementation.md. It meets the adjacent daemon-mode work (the
`task_templates.mode` map and its daemon-run templates) at the executor hook
and in `ResolveGateCheck`'s fallback, tracked in the adjacent draft
2026-11-09-daemon-template-task-modes.md — this draft covers only the
script-gate surface and its two settings.

## Current behavior after this work

The measured waste — sessions spent performing a closing gate whose only real
substance is "run the project's checks and write down what happened" — is
closed for every `final_gate`. When the executor reaches a task of kind
`final_gate` in internal/autonomous/executor.go's `executeOne`, it asks
`runScriptGateTask` first, before any session spawn, verifier or retry loop.
The checks resolve in the fixed order of `ResolveGateCheck`: the task's own
`verification_script` when it is a real file under `.datawatch/verify/`; else
the project gate command — the project's `policy project_gate_command` when
the policy resolver reports a value, else the global
`autonomous.project_gate_command`, first set one wins; else the `verification_
script` of every other task in the story that is such a file, run in order
until the first failure. Every script the gate runs passes the shared
verification-script checks and must resolve strictly inside the project's
`.datawatch/verify/` — outside paths and symlink escapes are refused — and
the gate runs them with `env -u TMUX -u TMUX_TMPDIR`, so nothing it launches
can reach the operator's tmux server. All checks share one deadline
(`autonomous.script_gate_timeout_seconds`, default 900 seconds, `0` for no
cap, negative refused on every write surface that validates config); on
timeout the whole process group is killed and the gate reports the limit.
The run's combined stdout and stderr, each check's verdict and the final
verdict are written to the task's `report_path` — the template-appended gate's
`.datawatch/reports/\<feature\>-final-gate.txt`, or
`.datawatch/reports/final-gate.txt` for a custom gate that carries no path —
a `script_gate` decision record is appended to the story either way, the task
completes on a clean pass and fails with the reason named on a failed check,
a timeout, or a gate the daemon could not run at all, and that failure is a
failed task the operator can see, not a story left open. There is no worker
session for it: no verifier, no retry loop, no model spend — which is exactly
the point of the work, and which the docs set now states at every one of the
four pages above.

## Expected behavior

An operator who wired a final gate into a story expects the same closing
guarantee as before — the story does not close unless the checks pass — but
without the session in the middle, and expects the two new knobs to behave
like every other autonomous setting: changeable at runtime from any surface
the operator already uses, readable back from somewhere, and described in the
config reference like the rest of the block. An operator who fails the gate
once and re-runs the task expects the earlier evidence to still be findable
or to be told, plainly, that the newer run replaced it. Every surface the
operator reads — operations, the how-to, the definitions, the config
reference — should agree on what the report file contains after a second run
and on which surfaces can set the two keys. That is the state this work
leaves except for the named gaps below.

## Gaps this work leaves

The first and largest gap is that the two global settings have no non-YAML
surface at all, and the project's own Configuration Accessibility Rule lists
YAML, Web UI, REST, comm channel, MCP and CLI as the required set. Both keys
live in `config.AutonomousConfig` and the config package validates them on
load, but `handleGetConfig` in internal/server/api.go builds its response as
a fixed hand-written map and `handlePutConfig` accepts a fixed set of dot-path
keys — and neither names `autonomous.script_gate_timeout_seconds` or
`autonomous.project_gate_command`. Because that PUT is the single route the
comm channel's `configure`, the MCP config tools and the CLI's `config set`
all funnel through (the handler's own comments say so — the shared validator
"is the same … so the comm, MCP and CLI surfaces show the same message"),
one missing pair of cases means all four surfaces are missing at once: a
`configure autonomous.script_gate_timeout_seconds=1800` or any MCP/CLI
variant cannot set the cap, and there is no read-back of either value outside
editing `config.yaml` and restarting — an operator watching a slow gate hit
its 900-second limit has no way to raise it for the next run without a config
file edit and a daemon restart. The Web UI has no card or field for either
key in internal/server/web/app.js, and `docs/config-reference.yaml`, whose
stated rule is that every new config field gets an entry, carries neither
key — a reader grepping it for the timeout they were told exists in
operations and the how-to finds nothing, while docs/implementation.md (which
is not subject to the no-internals rule the same way) describes both in full.
The asymmetry is sharpest on the gate command: the per-project
`policy project_gate_command` is already fully surfaced — the policy write in
internal/server/projects.go validates it through `ValidateProjectPolicy` and
the `/policy/effective` view resolves it — while its own documented
fallback, the global `autonomous.project_gate_command`, is not reachable on
any live surface, so an operator can set the override and not the value the
override stands in for. The mobile surface inherits this: Android and iOS
can only show what the daemon's config read returns, and the read returns
neither key, so both clients are blocked on the same missing cases; the
adjacent app-side draft 2026-11-09-daemon-template-task-modes.md already
carries the mobile follow-on for the template-mode block, and this key pair
belongs on the same issue or its successor.

The second gap is a contract the docs state and the code does not. Both
docs/operations.md ("appends each check's combined stdout and stderr to the
task's report path") and the how-to's Deadline-and-report paragraph ("Each
check's combined stdout and stderr is appended to the report as it runs")
describe the report file as something the gate's output is appended to. The
code does not append anywhere: `runScriptGateTask` builds the entire report
in memory during the run and `finishScriptGateTask` → `writeGateReport`
writes it with a plain `os.WriteFile`, once, per run. The practical
consequence is the one the docs never mention: when the operator re-runs the
failed gate — reset, retry, or a corrected script — the previous run's
verdict, exit codes and output are silently replaced, and nothing in the
story, the decision record or the task's error text says a report was there
before. That is defensible as a contract (one report per report path, latest
run wins), but it is the opposite of the word the two docs use, and it is the
one place in this work where an operator who trusted the how-to is left
holding an evidence file that does not match what the doc promised. Fix one,
or the other: either the wording (a single report, rewritten each run,
earlier runs not preserved) or the code (append a dated section so a re-run
keeps its predecessor), but in either case the operations paragraph, the
how-to, the definitions entry and the implementation sections must all say
the same thing afterwards.

A smaller, code-internal gap worth naming while the area is open:
`config.EffectiveProjectGateCommand` — the policy-else-global resolution
with the explicit-empty-unsets rule, pinned by its test — has no non-test
caller. The daemon's actual resolution lives separately in `projectGateCommand`
in internal/autonomous/script_gate_task.go (policy resolver value else
manager config, trimmed), and `runScriptGateTask` reads the timeout through a
freshly built `config.AutonomousConfig` copy in the same file. Two
implementations of one rule is the standard way the "policy override, empty
unsets" behavior drifts when someone changes one and not the other, and the
config-side function reads like it was written to be the shared one; the run
path should call it, or it should go, but it should not sit unused as the
apparent authority.

Finally, a wording gap the docs already half-admit: `final_gate`'s mode is
documented as `llm` or `off`, and every page explains that `llm` no longer
means a session — the daemon performs the gate regardless of mode. A mode map
whose allowed value for its one member is named for the thing that no longer
does it is the kind of thing a reader who has not read the
three-pages-of-explanation will act on wrong (set `daemon` and get refused,
or set `llm` and be surprised by no session). This is the cheapest gap to
close and the safest: the wire value can stay for compatibility, but the docs
set should say so explicitly at the value's first appearance, or the
validator's refusal message should carry the current wording, so the refusal
itself teaches the contract instead of just rejecting.

None of these gaps change what the daemon already does well: the gate runs
with no session, no verifier and no retry loop; the tmux-unset and the
verify-folder symlink-escape handling are both in place; a gate with nothing
runnable fails visibly instead of hanging the story. The gaps are that two
of the work's own knobs are reachable from exactly one surface, that the
report contract is documented as append and implemented as replace, and that
one resolution rule exists in two places with the unused one looking like the
real one.

## Suggested next step

File this as a datawatch (daemon) issue, cross-referencing the app-side draft
2026-11-09-daemon-template-task-modes.md (whose issue already owns the
client-side follow-on for the autonomous block) and naming the four docs
pages by section. In that issue, first close the surface gap on the daemon
side where all four missing channels converge: add both keys to
`handleGetConfig` (the timeout via `EffectiveScriptGateTimeout`, so the read
returns what the daemon would actually enforce, and the command with its
empty-means-unset semantics) and to `handlePutConfig` through one shared
validator that calls the config package's own `validateScriptGateTimeout`
and `ValidateProjectGateCommand`, exactly the pattern the neighboring
`autonomous.tags.*` and `autonomous.auto_tags.*` keys already use, so the
comm, MCP and CLI rejections carry the same message as a REST one; add the
two keys to `docs/config-reference.yaml`'s autonomous block with their
defaults, the 900/0/negative rule, the 1024-byte one-line rule and the
policy-override ordering; and record on the app-side issue that Android and
iOS inherit both once the daemon read/write exist, so no client carries the
key its own daemon will not return. Second, settle the report contract in
one direction: the cheaper is to correct operations.md, the how-to, the
definitions entry and the implementation sections to say the report is
rewritten on each run and earlier runs are not preserved, and to add that
one line to the `script_gate` decision note when a previous report existed —
a cheap, one-line, no-new-behavior fix — or to implement the append the docs
currently promise, dated per run, if the operator reads the evidence-retention
version as the intended one; either way add a test on the rerun path, since
`writeGateReport`'s overwrite is exactly the kind of thing a first-run test
never sees. Third, collapse the dual resolution: make
`runScriptGateTask` resolve through `config.EffectiveProjectGateCommand`
(and its `EffectiveScriptGateTimeout`) instead of rebuilding the rule, so the
test-pinned function in the config package is the one the daemon actually
runs. Last, and only in the same issue as a line, not a behavior change,
decide what the docs say at `final_gate`'s `llm` value's first appearance —
keep the value, keep the compatibility, but make the sentence carry the
"no session either way" clause at the point of first mention rather than in
the three-page explanation that follows — and if the validator's refusal
wording for a `daemon` mode on `final_gate` is changed at all, change it
here, where the contract now lives. None of this touches the run itself:
the order of checks, the verify-folder and symlink rules, the process-group
kill, the tmux-unset, and the "a gate with nothing runnable is a failed task"
rule all stand as this work shipped them.

## Filing record

Not yet filed. Suggested target: a datawatch daemon issue, cross-referencing
2026-11-09-daemon-template-task-modes.md for the client-side tracking and the
script-gate sections of docs/operations.md,
docs/howto/automaton-decompose-lint.md, docs/datawatch-definitions.md and
docs/implementation.md as the pages the fixes must land on; record the URL
and date here when filed. Drafted 2026-11-09, with the code-level claims —
the two keys and their validation in internal/config/config.go and
internal/config/script_gate.go, the policy override and its resolver in
internal/config/project_policy.go, the policy write/read in
internal/server/projects.go, the fixed-map/fixed-key config handlers in
internal/server/api.go, the gate run and its report write in
internal/autonomous/script_gate.go and internal/autonomous/script_gate_task.go,
the executor hook in internal/autonomous/executor.go — checked against the
current tree on the same day.
