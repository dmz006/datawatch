---
docs:
  index: true
  topics: [autonomous, automation, ci, repo ci watch, github actions, shadow mode, rerun, hung run, superseded runs, security, alert]
exec_steps:
  - tool: autonomous_repo_ci_watch
    description: Read the repo CI watch: mode in effect, alert threshold, watched repositories, last poll, and the stored records newest first
    args: {}
    read_only: true
  - tool: autonomous_repo_ci_watch
    description: Read only the records in one watch state (green, failing, pending, hung, rerunning or superseded)
    args: {status: "{{params.status}}"}
    read_only: true
  - tool: autonomous_repo_ci_watch
    description: Read only one repository's records
    args: {repo: "{{params.repo}}"}
    read_only: true
  - tool: autonomous_config_get
    description: Read the autonomous settings, including the autonomous.automation.repo_ci_watch block
    args: {}
    read_only: true
---

# Repo CI watch

The **repo CI watch** is the daemon's standing watch over your repository's CI. On
every poll it looks at the head of `main` and the newest release tags across
**every workflow** in the repository you listed, keeps one record per CI run,
classifies each one, and — in `on` mode — takes the small, budgeted set of
actions it is allowed to take: one rerun of an infra-broken run, one cancel of
a run that hung, and the retention cleanup of runs that were superseded.

It is off by default (`mode: off`). The middle mode, **shadow**, is the
comparison mode: the pass runs, classifies, and records every action it *would*
take as explicit `would-do` lines — but writes nothing, cancels nothing and
alerts nothing. You read those would-dos next to what you (or the supervisor
loop) actually did for the same runs, and only switch to `on` once the two
agree over several episodes.

## Base requirements

- A running daemon with the autonomous engine enabled.
- The repository present at an absolute local path, added to
  `autonomous.automation.repo_ci_watch.repos`, and the watch mode set to
  `shadow` or `on`.
- CI (GitHub Actions) running in that repository, with a `main` branch and,
  if you want tag coverage, release tags.
- The Automation coordinator running — it is the loop the watch pass hangs
  off. No separate service.

## What the watch sees

For each repository in `repos`, one poll reads:

| What | How | Default |
|---|---|---|
| The head of `main` | `git rev-parse origin/main` (or the default branch) and then the CI runs on that SHA | always, for listed repos |
| The newest release tags | the newest `tag_count` tags in the repository, each of the shape `v\u003cmajor\u003e.\u003cminor\u003e.\u003cpatch\u003e` (`v2` does not count as a release tag) | 5 (0 = no tags) |

For every one of those refs the pass lists the runs on that ref's head commit — every workflow that ran on that SHA — not a fixed workflow name. So a poll covers the head of `main` plus the newest `tag_count` release tags, each across every workflow that ran there. A run belongs to the (repository, ref, workflow) triple it ran on, and each one gets at most one watch record.

## The record states

Each record keeps one watch state. The pass records four of them — `green`, `failing`, `pending` and `hung` — and the read view's status filter accepts all six vocabulary values, so it can also filter on the two records the pass marks — `rerunning` (its own rerun is recorded on the record's rerun log) and `superseded` (a later green run names it in its `superseded_run_ids`); any other value is a 400 that names the allowed values.

| State | Meaning |
|---|---|
| `green` | The run finished cleanly (success, skipped or neutral). For retention it still matters: when the ref moves on and a later run on the same ref/workflow goes green, this run is named **superseded** by the newer green record and becomes a retention-cleanup candidate. |
| `failing` | The run finished red (a failure conclusion or action-required) and carries the classification the pass reached for it. |
| `pending` | The run is still in flight. It is not `hung` until it has been in flight longer than `hung_after_minutes`. |
| `hung` | A `pending` run that outlived `hung_after_minutes` (default 60, minimum 1). In `on` mode it is cancelled once. |
| `rerunning` | Vocabulary value: the pass's rerun of this run was posted; the record's rerun log names it and the pass waits for the new attempt. |
| `superseded` | Vocabulary value: this run was preceded by a later run on the same ref/workflow, named on the green record that superseded it; it is a retention candidate, and the record keeps its run id so the pass can address it. |

## The classification and what each one means

After a poll, the pass classifies every run. The classification is what decides what the pass may do with that run, and the value you read on every surface is one of seven:

| Classification | What it means | What the pass may do with it |
|---|---|---|
| `none` | Unclassified — the state a record carries until the pass classifies it, and the one a green or pending run stays at. | Nothing; it is the default, not a verdict. |
| `infra` | The run broke because of the CI infrastructure, not the code: it finished by cancellation, timeout, a pre-start failure, or staleness (a pipeline abort, not a red test). One automatic rerun is safe and expected. | In `on` mode, **one rerun** of the recorded run — at most once per run id — and only while the infra retry budget still has reruns left. In shadow mode the rerun is recorded as a would-do and never executed. |
| `security` | A run on a workflow named in `security_workflows` finished without a green or infra outcome (its conclusion is a failure or action-required). The default list is `CodeQL`, `dependency-review`, `owasp-zap`, `secret-scan`, `security-scan`. **Record-only by design.** | Nothing is rerun or acted on. The pass does not emit a would-rerun either — a security failure is information for the operator plus whatever the operator's own security process says to do, and the watch's job is to keep the record visible, not to touch it. |
| `policy` | A failed non-security run whose **judge verdict** is a policy-gate refusal (the pass spends at most one LLM judge call per poll, on the first non-green run it meets, and never re-judges a run that already carries a verdict). Like `security`, **record-only** — the watch does not rerun or waive it. | Nothing beyond the record. The operator's policy process owns the disposition. |
| `code_or_test` | A failed non-security run the judge cleared of policy: the verdict's other category, meaning the defect is in the code or the test and only the owner of the change can fix it. Not an infrastructure fault, not a policy gate. | Nothing automatic — no rerun, no cancel. The record and the alert name it so the owner can look. |
| `judging` | A failed non-security run the pass could not reach a verdict on in this poll — the pass's single judge call was already spent, or the judge call itself failed. **A pending state, not a verdict**: a later poll reuses the cached verdict if the run has one, or re-judges it. | Nothing; it settles to `policy` or `code_or_test` once a verdict exists. |
| `hung` | The run is in flight and outlived `hung_after_minutes`. | In `on` mode it is cancelled once. |

**Why `security` is record-only.** A failed security run is evidence, not a
flake. Rerunning it is a decision: it can legitimately rerun green (the finding
was real and the rerun hid it), or it can be re-baselined in a process the
watch does not own. The pass therefore never puts a security run in its
rerun queue — not in `on` mode, and not even as a would-do in shadow mode.
The record carries the classification and the run URL so the operator can see
exactly which security workflows are failing, on which ref, and for how long.
That is the whole contract. `policy` and `code_or_test` share the same shape: the
watch records the verdict and the run URL; the disposition belongs to the
policy process or the change owner.

## The alert

Once a run has been non-green — `failing`, `pending` or `hung` — for longer
than `alert_minutes` (default **30**; `0` turns the alert off), measured from
the run's first-seen stamp, the pass raises **exactly one alert per episode**.
The alert names the repository, the ref, the workflow, the run, its
classification, the first-seen stamp and the run URL — enough for one action
per line. The alert is stamped onto the record as `alerted_at`, which is what
makes it once-per-episode: a later pass that sees the same episode does not
re-alert, and the watch will not touch that run further until it is resolved,
superseded, or the budget changes.

The alert raises in `on` mode only — in `shadow` mode nothing is raised at
all: shadow records would-dos and never interrupts. Turning `alert_minutes`
to `0` turns the alert off in any mode (the reported threshold then reads 0).

## The automatic actions in `on` mode

The pass takes three kinds of action, and only these three, and only in `on`
mode. Each one is budgeted or one-shot so the pass cannot loop:

| Action | When | How many times |
|---|---|---|
| **Infra rerun** | The record is classified `infra` and the infra retry budget — the Manager's `RetryPolicy["infra"]`, read as `0` when unset (no automatic reruns at all) — still has a rerun for that repository·ref·workflow. | One rerun per run id, shared across the repository·ref·workflow. A rerun that errors is not counted as spent — the record stays unresolved and the later pass may retry. A rerun whose new attempt is itself red is not re-re-run automatically: it needs the operator again, or a re-classification. |
| **Hung cancel** | The record is `pending` and older than `hung_after_minutes`. | Once per run id. The pass checks the run is not already cancelled before it posts the cancel, so a hung run that has already resolved is never cancelled twice. |
| **Superseded deletion** | The green record names the superseded runs in its `superseded_run_ids`. | One deletion per run, addressed by the run ID recorded on the record — not by re-deriving the ref, so a tag that moved on after the record was written does not break the delete. A run named by this very poll is deleted on the **later** pass, never on the pass that named it; and it is never rerun while it is a deletion candidate. See the deletion policy below. |

### The deletion policy (`delete_superseded`)

| Value | What the pass does with superseded runs |
|---|---|
| `report` (default) | The pass names the superseded runs on the green record and stops there. Deleting is the operator's explicit act. This is the safe default: retention cleanup is a repository-management decision, and the pass should propose it, not make it. |
| `delete` | In addition to recording, the pass issues the deletion — but only in `on` mode, and the pass that names the runs does not also delete them: the deletion is picked up on the **later** pass, after the green record is durably written, so a crash between "decide" and "issue" costs nothing. In `shadow` mode `delete` behaves exactly like `report` — recording only, no deletion call. |

There is no `delete` that fires in `shadow`: shadow never issues any call that
changes state, and the deletion policy's `delete` value is not a write switch,
it is a declaration of what the pass is allowed to do once `mode` is `on`.

## Shadow mode — and how to compare it with the supervisor's actions

**Shadow** is the mode you run first. On every poll the pass does everything
it would do in `on` mode, except the write:

- it polls the refs, classifies every run the same way an `on` pass would,
- it records the would-rerun for each `infra` run (with the budget it would
  spend), the would-cancel for each hung run, and the would-delete for each
  superseded run,
- it does **not** post a rerun, does **not** post a cancel, does **not** issue
  a deletion, and does **not** raise the alert.

Every would-do is written into the record in a form that names the action, the
target (repository, ref, workflow, run) and the policy that would have gated
it, so reading the record is reading the decision. The actor for every such
line is `repo_ci_watch`, explicitly, so a later read of the audit trail or the
read view cannot mistake a would-do for a did-do.

**The comparison you owe before `on`.** For a handful of episodes — one red
`main`, one hung run, one run you reran yourself, and one tag that got
superseded — put the two sides side by side:

1. **What the watch would have done.** Read the records for the episode (any
   surface below; the would-do lines are in the record body). Note the
   classification, the action, the budget it would have spent and the order.
2. **What actually happened.** The supervisor loop's actions for the same runs
   — the rerun it posted, the cancel it allowed, the tag it superseded — from
   the CI UI, the audit trail, or whatever process owned that work.
3. **Judge the three things that matter here** — classification (did the watch
   call `infra` what you called `infra`, and `security` what you called
   `security`? did a `code_or_test` verdict match what you saw?),
   budget (would the watch have rerun something you would have
   left alone, or left alone something you would have rerun?), and order
   (would the watch have cancelled a run you were still debugging, or rerun one
   you wanted to hold for evidence?).

When those agree over several episodes — including at least one `security`
episode you deliberately left alone, and one superseded run you deliberately
deleted or kept — `on` is a reasonable next step. When they disagree, the
disagreement is the finding: fix the policy (`security_workflows`,
`hung_after_minutes`, the infra retry budget) and re-shadow.

## Reading the state on every surface

The watch is readable from all five operator surfaces, all of them reads of
the same stored state (mode, threshold, repos, last poll, records newest
first). None of them changes anything.

| Surface | Read the state | Filter it |
|---|---|---|
| REST | `GET /api/autonomous/repo-ci-watch` | `?repo=<repository>` and/or `?status=<state>` (one of `green`, `failing`, `pending`, `hung`, `rerunning`, `superseded`; anything else is a 400 that names the allowed values; a filter that matches nothing is 200 with an empty list) |
| MCP | `autonomous_repo_ci_watch` (read-only, same body) | `repo` and `status` parameters, same vocabulary |
| CLI | `datawatch autonomous repo-ci-watch` (a table: repository, ref, workflow, status, classification, first seen, alerted) | `--repo <repository>`, `--status <state>`, `--json` for the unfiltered body |
| Comm channel | `autonomous repo-ci-watch` (alias `repo_ci_watch`) — the mode on the first line, then one line per record: repository, ref, workflow, status, classification, run URL | the read takes no arguments; to narrow, use the CLI, MCP or REST filters |
| PWA | Automata tab → the **Repo CI watch** status line on the tab: the mode in effect, the `failing` and `pending` counts, and how long ago the coordinator last polled — refreshed every 30 s, or immediately when the tab renders | the line itself is the summary; the full records are the REST/MCP/CLI reads |

## Configuration

| Key | Default | Meaning |
|---|---|---|
| `autonomous.automation.repo_ci_watch.mode` | `off` | `off` (the pass does not run and nothing is recorded), `shadow` (everything runs, nothing writes, no alert) or `on` (the actions above are taken, budgeted, and the alert raises). |
| `autonomous.automation.repo_ci_watch.repos` | empty | Comma-separated absolute paths of the repositories to watch. Empty means nothing is watched. |
| `autonomous.automation.repo_ci_watch.poll_seconds` | `300` (unset or `0` reads `300`) | Seconds between polls. Values below 60 are **refused on write** by every path — the minimum is 60; nothing is ever clamped up for you. |
| `autonomous.automation.repo_ci_watch.alert_minutes` | `30` | How long a non-green run may sit before the once-per-episode alert. `0` turns the alert off (in which case the reported threshold reads 0); negative values are refused. |
| `autonomous.automation.repo_ci_watch.tag_count` | `5` | How many of the newest release tags (shaped `v\u003cmajor\u003e.\u003cminor\u003e.\u003cpatch\u003e`) are covered per poll. `0` means no tags; negative values are refused. |
| `autonomous.automation.repo_ci_watch.hung_after_minutes` | `60` | How long a run may be `pending` before it is `hung`. Minimum 1; below that is refused. |
| `autonomous.automation.repo_ci_watch.security_workflows` | `CodeQL, dependency-review, owasp-zap, secret-scan, security-scan` | Workflow names that classify a non-green run as `security` (record-only). Empty list means nothing classifies as `security` — set it deliberately before turning `on`. |
| `autonomous.automation.repo_ci_watch.delete_superseded` | `report` | `report` (name the superseded runs, do not delete them) or `delete` (in `on` mode, the pass issues the deletion for the runs a green record names, on the later pass). |
| infra retry budget — the Manager's `RetryPolicy["infra"]` (`retry_policy.infra`) | unset reads `0` — no automatic reruns at all | The cap on automatic infra reruns shared per repository·ref·workflow. A budget of 0 means the pass never reruns an `infra` run; set it to allow one rerun per run id while it holds. |

Every key is read live from `config.yaml`. Changing `mode`, `poll_seconds`,
`alert_minutes`, `tag_count`, `hung_after_minutes`, `security_workflows` or
`delete_superseded` takes effect on the next poll — no restart. A bad value is
refused on write by every path — an unknown `mode`, a non-absolute repository
path, a `poll_seconds` below 60, a negative `alert_minutes` or `tag_count`, a
`hung_after_minutes` below 1 — and nothing is written. The `repos` list and
the classification vocabulary are read on every poll, so adding a repository
mid-episode starts being watched on the following pass, and removing one stops
it on the following pass without erasing its records (the records stay readable
on every surface).

## What the watch never does

It never merges, never judges design or difficulty, and never holds or starts
Automata — the watch is about the repository's CI runs, not about the
Automation queue. It never reruns a `security` or `policy` run, in any mode,
and never as a `would-do`. It never posts a second cancel for a run that is
already resolving, and never spends infra budget on a run it named for
deletion. It never issues a deletion in `shadow`, in `off`, or in
`report` — and never a deletion on the same pass that named the run. It never
re-judges a run that already carries a verdict, and it never re-alerts an
episode that already alerted. It never spends retry budget it does not have —
a rerun that comes from a zero budget is not a rerun, it is the operator
deciding. And in `shadow` it never writes anything at all: the record and the
would-do text are the entire surface — no action, no alert.

## Operations

**Turning it off.** Set `mode` to `off`. The pass stops, the alert stops, and
every record already written stays readable on every surface — turning the
watch off does not delete history.

**Restarting the daemon.** The records are persisted to
`<data-dir>/autonomous/repo_ci_watch.jsonl` and reloaded at startup, so the
watch resumes with its full history. A run that was `pending` across the
restart is still `pending` when the next pass runs, and its hung age is read
from the CI run's own start time — the restart does not reset the hung
timer — and a rerun or cancel already in flight before the restart is not
re-issued after it (the record's state is what the pass acts on, not a
queue that survives the process).

**Audit.** Every would-do, rerun, cancel and deletion the pass makes is
recorded on the record itself — the `would-do` lines, the rerun log, the
failure notes and the `alerted_at` stamp — so the read view is the durable
trace. In `on` mode the alert goes through the daemon's alert sink with the
run's identity, classification, first-seen stamp and URL in the body. The
`alerted_at` stamp on the record is the once-per-episode proof; the `would-do`
lines in shadow mode are the comparison evidence.

**The deletion is irreversible.** `delete_superseded: delete` in `on` mode
issues real deletions of real CI runs on the ref the record names. Before you
set it, confirm the repository's retention expectations with the process that
owns them — once a run is deleted it is gone from the CI UI, and the record
keeps the run ID but not the run's output.
