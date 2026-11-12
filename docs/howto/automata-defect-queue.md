---
docs:
  index: true
  topics: [autonomous, automata, defect queue, defect batch, tiny fix, intake, split out, batch.size_cap, tiny_fix]
exec_params:
  - name: project
    description: Project the defects belong to
  - name: title
    description: Short defect title
  - name: files
    description: Repo-relative paths the fix is expected to touch
  - name: kind
    description: Declared change kind - test_only, docs_only, script_only, rule_tweak or lint_tweak
exec_steps:
  - tool: autonomous_defect_tinyfix_check
    description: Ask whether a change would qualify for the tiny-fix lane (read-only, creates nothing)
    args:
      files: "{{params.files}}"
      kind: "{{params.kind}}"
      project: "{{params.project}}"
    read_only: true
  - tool: autonomous_defect_batches
    description: List the open defect batches and the story-to-defect map
    args:
      project: "{{params.project}}"
    read_only: true
---

# Running a defect queue with batches and tiny fixes

A defect queue turns a stream of small defect reports into as few Automata as
possible. Related defects share one **batch** Automaton, a change that only
touches tests, docs or scripts runs in the **tiny-fix lane**, and every
routing decision is written to a log you can read afterwards. This guide
covers why it exists, how batches and the tiny-fix lane work, how intake
decides, a worked example, every setting, and every command on every surface.

It assumes a running daemon (see [Setup + install](setup-and-install.md)) and
that you know what an Automaton is (see
[Autonomous planning](autonomous-planning.md)). The terms are defined in the
[definitions](../datawatch-definitions.md); the REST reference is in
[docs/api/autonomous.md](../api/autonomous.md#defect-queue).

## Why group defects

An Automaton has fixed overhead: a verification gate, a documentation task, a
changelog entry and a parity or locale check. Measured over real defect
Automata, a single-defect Automaton averages about 7.5 tasks, and only about
two of them are the change itself. The other five are the same chores, run
again for every defect.

Grouping related defects pays that overhead once:

```
one defect per Automaton            one batch of three related defects
------------------------            ----------------------------------
fix + test + gate + docs +          story A: fix + test
changelog + parity   (x3)           story B: fix + test
= about 22 tasks                    story C: fix + test
                                    + gate + docs + changelog + parity
                                    = about 10 tasks
```

Grouping has one cost: a failure in one defect must not hold up the others.
That is why a batch is built from independent stories and why a failed story
can be split out (below).

## How batches work

A batch is one Automaton with this shape:

| Part | What it is |
|---|---|
| One story per defect | The story is titled with the defect it fixes. It holds a fix task and, when the defect lists files, a regression-test task that depends on the fix. |
| Shared gate | One verification gate for the whole batch. It runs the build and tests and reports counts only (passed, failed, skipped). It depends on every story's tasks. |
| Documentation task | One task, after the gate, updating the documentation the fixes affect. |
| Changelog entry | One task, after the documentation task, adding a single changelog entry covering the batch. |
| Parity-or-locale task | One task, after the documentation task, checking parity across surfaces and locale strings. |

Which defects share a batch is decided by `batch.grouping_keys`:

| Key | Two defects are related when |
|---|---|
| `project_area` | They are in the same project and name the same non-empty area |
| `shared_files` | Their file lists share at least one path |
| `shared_surfaces` | Their surface lists share at least one entry |

A batch holds at most `batch.size_cap` defects (default 5). Defects of
different projects never share a batch. With `batch.allow_unrelated` on, a
defect joins any open batch with room, related or not.

**Priority rule.** A batch carries one priority. With `highest_member` (the
default) it is the highest priority among its defects, so one urgent defect
raises the whole batch. With `first_member` it is the priority of the defect
that started the batch. Higher numbers are more urgent. The priority is
recomputed whenever a defect joins or is split out.

**Split-out of a failed story.** If one story fails, you do not have to hold
the rest. Split it out: the story moves, with its own fix and test tasks, into
a new batch of one (`batch-split-<defect-id>`) that gets its own gate,
documentation and changelog tasks (no parity task). The original batch keeps
its other stories and shared tasks, the dependencies on the moved tasks are
removed so it stays runnable, and its priority is recomputed.

## How the tiny-fix lane works

Some defects do not need a gate, documentation and changelog at all: a flaky
test, a wrong comment in a doc, a script typo. The tiny-fix lane runs these as
a one-task Automaton: one story, one task, one verification script. The
change is merged through the normal review.

A change qualifies only when **all** of these hold (the `tiny_fix` keys):

1. `tiny_fix.enabled` is on, and the defect queue itself is on.
2. It touches between 1 and `tiny_fix.max_files` files (default 3).
3. Its declared kind is in `tiny_fix.allowed_kinds`: `test_only`,
   `docs_only`, `script_only`, `rule_tweak`, `lint_tweak`.
4. No path matches `tiny_fix.operator_visible_path_globs`.
5. Every path matches `tiny_fix.allowed_path_globs`.

**When it refuses an operator-visible change.** An operator-visible change is
one an operator can see or that changes the contract: the web UI, the command
line, the API handlers. By default that is `internal/server/web/**`, `cmd/**`
and `internal/server/api.go`. A change touching any of them is never a tiny
fix, whatever its declared kind, because it needs the gate, the documentation,
the changelog and the parity or locale check that only a batch provides. The
refusal names the offending paths and sends the change to the normal path: a
normal single-defect Automaton or a defect batch.

The same check runs a second time against the files **actually** changed when
the lane finishes. If the worker touched a visible or out-of-lane path, the
result is refused with a reason that lists the paths.

## How intake decides, and logs

Intake routes one defect, applies the route, and appends a line to the
decision log. The order is fixed:

1. **`tiny_fix`**: the change qualifies for the lane and the operator-visible
   re-check does not refuse.
2. **`open_batch`**: a **draft** batch of the same project and area exists
   with room under `batch.size_cap`. With `batch.allow_unrelated`, any draft
   batch of the project with room qualifies. A batch that has started running
   is not a draft and never takes new defects.
3. **`new_batch`**: otherwise a new batch is created for the defect.

Every reason is written as a sentence, for example `area "web-sessions" has
open draft batch batch-D1 with 1/5 stories` or `starting a new batch: no open
batch for area "backup"`. A defect with no area always starts a new batch
(unless `allow_unrelated` is on), because nothing can match it.

If the tiny-fix lane declines a change that looked eligible at first and the
re-check refuses it, intake falls back to batch routing and records the
refusal in the `tiny_fix_refusal` field next to the final route. If the
classifier already declined the change (wrong kind, too many files, visible
path), the log shows the batch route only; ask `tiny-fix-check` to see the
classifier's reason.

The log is an append-only JSON-lines file, `defect_intake_decisions.jsonl`
in the daemon data directory. Each line has `at`, `defect_id`, `route`,
`reason`, `batch_id`, `tiny_fix_refusal` and `apply_error` (set when the route
was decided but creating or extending the Automaton failed; the decision is
still logged).

## Worked example

A project named `webapp` runs with the defaults (`batch.size_cap` 5, grouping
on `project_area`, `shared_files` and `shared_surfaces`, priority rule
`highest_member`). Five defects arrive in order.

| # | Defect | Area | Kind | Files | Priority |
|---|---|---|---|---|---|
| D1 | Session list flickers on reconnect | `web-sessions` | `bug` | `internal/server/web/app.js` | 2 |
| D2 | Session list shows a stale count | `web-sessions` | `bug` | `internal/server/web/app.js` | 5 |
| D3 | Backup job ignores the retention setting | `backup` | `bug` | `internal/backup/prune.go` | 3 |
| D4 | Flaky assertion in the prune test | `backup` | `test_only` | `internal/backup/prune_test.go` | 1 |
| D5 | Tooltip lint warning in the session list | `web-sessions` | `lint_tweak` | `internal/server/web/app.js` | 1 |

**D1 - `new_batch`.** No open batch exists for `web-sessions`. Reason:
`starting a new batch: no open batch for area "web-sessions"`. Batch
`batch-D1` is created in draft with one story, priority 2.

**D2 - `open_batch`.** Same project and area, `batch-D1` is a draft with 1 of
5 stories. Reason: `area "web-sessions" has open draft batch batch-D1 with 1/5
stories`. The batch now has two stories, and under `highest_member` its
priority rises from 2 to 5. D1 and D2 also share a file, so they would group
under `shared_files` as well.

**D3 - `new_batch`.** It is in a different area, so no open batch matches.
Reason: `starting a new batch: no open batch for area "backup"`. This is the
unrelated defect: it does not ride along with the web batch, because
`batch.allow_unrelated` is off.

**D4 - `tiny_fix`.** One file, kind `test_only`, a path that matches the
default `**/*_test.go`, nothing operator-visible. Reason: `tiny fix: 1
file(s), kind "test_only", all paths allowed`. Intake creates a one-task
Automaton with one verification script; no gate, documentation or changelog
task is added. It never touches batch `batch-D3` even though it shares the
`backup` area, because the lane is checked first.

**D5 - refused, then `open_batch`.** The reporter calls it a lint tweak, a
kind the lane allows, but the file is `internal/server/web/app.js`, which is
under the default operator-visible path `internal/server/web/**`. Running the
check first shows the refusal:

```
$ datawatch autonomous defect tiny-fix-check --file internal/server/web/app.js --kind lint_tweak --project webapp
eligible: false
reason:   the change touches operator-visible path(s) internal/server/web/app.js
```

The lane declines, so intake takes the batch route and the defect joins
`batch-D1`: `area "web-sessions" has open draft batch batch-D1 with 2/5
stories`. The batch now has three stories, and because a visible file is
touched, the shared gate, documentation, changelog and parity-or-locale tasks
all apply to it. This is the rule working: the change a person can see in the web UI
went through the full set of chores, not the shortcut.

**Split-out.** `batch-D1` runs. The D2 story's fix fails its test and cannot
be fixed quickly, while D1 and D5 pass. Rather than hold the batch, you split
the D2 story out:

```
$ datawatch autonomous defect split batch-D1 --story story-D2
{"automaton_id":"...","batch_id":"batch-D1","story_id":"story-D2"}
```

D2 now lives in its own Automaton `batch-split-D2` (priority 5, with its own
gate, documentation and changelog tasks, no parity task). `batch-D1` keeps
D1 and D5, its dependencies on the D2 tasks are removed, and its priority
falls from 5 to 2 because D2 was the highest member. Both can now finish
independently.

## Settings

Every key can be set from the web UI (Settings, Automata, **Defect queue**,
translated in the five shipped locales), the config file, `PUT /api/config`,
MCP `config_set`, the CLI `config set`, and the comm channel `configure`
verb. All surfaces share one validator, so a refused value has the same
message everywhere. Lists are comma separated in the web UI and the CLI.
Precedence for each setting is: project override, then the block-level value,
then the default.

| Key | Default | What it tunes |
|---|---|---|
| `autonomous.defect_queue.enabled` | `true` | Master switch. Off: intake answers 409 for the project and nothing is tiny-fix eligible. |
| `autonomous.defect_queue.batch.size_cap` | `5` | Most defects in one batch (whole number, at least 1). The batch_size_cap-equivalent setting. |
| `autonomous.defect_queue.batch.grouping_keys` | `project_area, shared_files, shared_surfaces` | Which attributes make two defects related. Legal values: `project_area`, `shared_files`, `shared_surfaces`. |
| `autonomous.defect_queue.batch.allow_unrelated` | `false` | Let defects with no shared grouping key share a batch. |
| `autonomous.defect_queue.batch.priority_rule` | `highest_member` | How a batch takes its priority: `highest_member` or `first_member`. |
| `autonomous.defect_queue.tiny_fix.enabled` | `true` | Switch the tiny-fix lane. Off: every defect goes to a batch. |
| `autonomous.defect_queue.tiny_fix.max_files` | `3` | Most files a tiny fix may touch (at least 1). |
| `autonomous.defect_queue.tiny_fix.allowed_path_globs` | test files and `testdata`, `tests/**`, `test/**`, `docs/**`, `**/*.md`, `scripts/**`, `.golangci.yml`, `.golangci.yaml`, `.trivyignore`, `.gitleaks.toml`, `.markdownlint*` | Paths a tiny fix may touch. Every path must match one. |
| `autonomous.defect_queue.tiny_fix.allowed_kinds` | `test_only, docs_only, script_only, rule_tweak, lint_tweak` | Declared kinds that qualify. |
| `autonomous.defect_queue.tiny_fix.operator_visible_path_globs` | `internal/server/web/**`, `cmd/**`, `internal/server/api.go` | Paths that make a change operator-visible and so never a tiny fix. |
| `autonomous.defect_queue.projects.<project>.<setting>` | unset | Per-project override of any `batch.*` or `tiny_fix.*` key above, for example `autonomous.defect_queue.projects.webapp.batch.size_cap`. Setting it to null clears the override. |

Example: allow batches of eight defects for one project:

```
datawatch config set autonomous.defect_queue.projects.webapp.batch.size_cap 8
```

## Commands on every surface

| Action | REST | MCP | CLI (`datawatch autonomous defect`) | Comm channel (`autonomous ...`) |
|---|---|---|---|---|
| Route one defect | `POST /api/autonomous/defects/intake` | `autonomous_defect_intake` | `intake --title --project [--id --description --kind --area --priority --file ...]` | `defect-intake <description> project=<p> [kind= area= priority= files=a,b id=]` |
| List open batches | `GET /api/autonomous/defects/batches[?project=]` | `autonomous_defect_batches` | `batches [--project]` | `defect-batches [project=<p>]` |
| Split a story out | `POST /api/autonomous/defects/batches/{id}/split` | `autonomous_defect_batch_split` | `split <batch-id> --story <story-id>` | `defect-split <batch-id> <story-id>` |
| Would this be a tiny fix? | `POST /api/autonomous/defects/tiny-fix/check` | `autonomous_defect_tinyfix_check` | `tiny-fix-check --file ... --kind ... [--project]` | use the REST passthrough |

Intake and split need the autonomous write permission; the batch list and the
tiny-fix check are read-only and need only read. The tiny-fix check creates
nothing, so it is safe to call before filing a defect. The web UI shows the
settings above under Settings, Automata, **Defect queue**.

The intake body is `{id, title, project, description?, files?, kind?, area?,
priority?}`; `id`, `title` and `project` are required. The answer is the
decision: `{route, batch_id?, reason, tiny_fix_refusal?}`.

## Troubleshooting

| You see | Meaning and fix |
|---|---|
| `409 defect queue is disabled for project ...` | `autonomous.defect_queue.enabled` is off. Turn it on. |
| `503 defect queue unavailable` | The daemon has not wired the queue (autonomous disabled). Enable autonomous. |
| Every defect starts a new batch | The defects have no `area` and share no files. Pass `--area` or `--file`, or turn on `batch.allow_unrelated`. |
| A docs change was not a tiny fix | Check the kind and paths with `tiny-fix-check`: the reason names the rule. |
| `404` on split | The batch id or the story id is wrong; list the batches to see the story ids. |

## See also

- [Autonomous planning](autonomous-planning.md)
- [Automata checks and ship steps](automata-checks-and-ship.md)
- [Automata capacity](automata-capacity.md)
- [API reference: autonomous](../api/autonomous.md#defect-queue)
