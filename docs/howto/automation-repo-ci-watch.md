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
| The head of `main` | `git rev-parse origin/main` and then the CI runs on that SHA | always, for listed repos |
| The newest release tags | the newest `tag_count` tags in the repository | 5 (0 = no tags) |

For every one of those refs the pass lists the repository's **workflows** and
then the runs on that ref. So a poll covers the head of `main` plus the newest
`tag_count` release tags, each across every workflow — the pass reads the
workflow list, it does not work from a fixed workflow name. A run belongs to
the (repository, ref, workflow) triple it ran on, and each one gets at most
one watch record.

## The record states

Each record keeps one watch state:

| State | Meaning |
|---|---|
| `green` | The run finished cleanly. It still matters for retention: once the ref moves on, the same run is **superseded** and becomes a retention-cleanup candidate. |
| `failing` | The run finished red and needs a classification before anything else can be said about it. |
| `pending` | The run has not finished yet. It is not "hung" until it has been pending longer than `hung_after_minutes`. |
| `hung` | A `pending` run that outlived `hung_after_minutes` (default 60, minimum 1). In `on` mode it is cancelled once. |
| `rerunning` | The pass reran the run (infra, budget permitting) and is waiting for the new attempt. |
| `superseded` | The run on this ref/workflow is no longer the newest one for that ref. It is a retention candidate; the record keeps its identity so the pass can address it (and, in delete mode, the pass can issue the deletion for it — see below). |

## The classification and what each one means

After a poll, every non-green run gets a classification. The classification is
what decides what the pass may do with that run:

| Classification | What it means | What the pass may do with it |
|---|---|---|
| `green` | The check the operator most needs — "did this pass on the head of `main`, or on a recent release tag?" | Record it; name it `superseded` once a newer run on the same ref/workflow exists; in `on` mode with the deletion policy set, it is a deletion candidate. |
| `infra` | The run broke because of the CI infrastructure, not the code: the runner died, a step timed out before doing work, a dependency fetch failed. One automatic rerun is safe and expected. | In `on` mode, **one rerun** of the recorded run, and only while the infra retry budget still has reruns left. In shadow mode, the rerun is recorded as a `would-do` line and never executed. |
| `security` | A security-related workflow failed (its name matches `security_workflows`; the default list is `security-review`, `codeql`, `dependency-review`). **Record-only by design.** | Nothing is rerun or acted on. The pass does **not** emit rerun candidates for security runs — a security failure is information for the operator plus whatever the operator's own security process says to do, and the watch's job is to keep the record visible, not to touch it. Shadow mode records the run with its classification and its would-do is the `superseded` retention entry only. |
| `policy` | A policy or compliance gate failed (branch protection, required checks, a custom policy step). Like `security`, **record-only** — the watch does not rerun or waive it. | Nothing beyond the record. The operator's policy process owns the disposition. |
| `failed` (unclassified) | Red, and the pass could not say `infra`, `security` or `policy`. The record stands until the operator looks. | Retention only (it becomes `superseded` when a newer run replaces it). No automatic rerun, no automatic cancel. |

**Why `security` is record-only.** A failed security run is evidence, not a
flake. Rerunning it is a decision: it can legitimately rerun green (the finding
was real and the rerun hid it), or it can be re-baselined in a process the
watch does not own. The pass therefore never puts a security run in its
rerun queue — not in `on` mode, and not even as a `would-do` in shadow mode.
The record carries the classification and the run URL so the operator can see
exactly which security workflows are failing, on which ref, and for how long.
That is the whole contract.

## The alert

Once a run has been non-green — `failing`, `pending` or `hung` — for longer
than `alert_minutes` (default 10; `0` turns the alert off), the pass raises
**exactly one alert per episode**, carrying the full resolution in the body
(repository, ref, workflow, run, classification, elapsed time) and the actor
`repo_ci_watch` explicitly named. The alert is stamped onto the record as
`alerted_at`, which is what makes it once-per-episode: a later passing that
sees the same episode does not re-alert.

In `shadow` mode the alert is not raised at all — shadow records would-dos and
never interrupts. Turning `alert_minutes` to `0` turns the alert off in any
mode.

## The automatic actions in `on` mode

The pass takes three kinds of action, and only these three, and only in `on`
mode. Each one is budgeted or one-shot so the pass cannot loop:

| Action | When | How many times |
|---|---|---|
| **Infra rerun** | The record is classified `infra` and its infra retry budget (read from `retry_policy.infra`, default 4) still has a rerun left. | One rerun per episode, then the record waits for the outcome. A rerun that also fails is not re-re-run automatically; it needs the operator again. |
| **Hung cancel** | The record is `pending` and older than `hung_after_minutes`. | Once per run. The pass checks the run is not already cancelled before it posts the cancel, so a hung run that has already resolved is never cancelled twice. |
| **Superseded deletion** | The record is `superseded`. | One deletion per run, addressed by the run ID recorded on the record — not by re-deriving the ref, so a tag that moved on after the record was written does not break the delete. See the deletion policy below. |

### The deletion policy (`delete_superseded`)

| Value | What the pass does with superseded runs |
|---|---|
| `report` (default) | The pass names the superseded runs in the record (their `would-do` is the retention entry) and stops there. Deleting is the operator's explicit act. This is the safe default: retention cleanup is a repository-management decision, and the pass should propose it, not make it. |
| `delete` | In addition to recording, the pass issues the deletion — but only in `on` mode, and the pass that records the would-delete does not also issue it: the issue is picked up on the **later** pass, after the record is durably written, so a crash between "decide" and "issue" costs nothing. The result of the delete call is recorded on the record. In `shadow` mode `delete` behaves exactly like `report` — recording only, no deletion call. |

There is no `delete` that fires in `shadow`: shadow never issues any call that
changes state, and the deletion policy's `delete` value is not a write switch,
it is a declaration of what the pass is allowed to do once `mode` is `on`.

## Shadow mode — and how to compare it with the supervisor's actions

**Shadow** is the mode you run first. On every poll the pass does everything
it would do in `on` mode, except the write:

- it polls the refs and workflows, classifies every non-green run,
- it records the would-rerun for each `infra` run (with the budget it would
  spend), the would-cancel for each hung run, and the would-delete for each
  `superseded` run,
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
   `security`?), budget (would the watch have rerun something you would have
   left alone, or left alone something you would have rerun?), and order
   (would the watch have cancelled a run you were still debugging, or rerun one
   you wanted to hold for evidence?).

When those agree over several episodes — including at least one `security`
episode you deliberately left alone, and one `superseded` run you deliberately
deleted or kept — `on` is a reasonable next step. When they disagree, the
disagreement is the finding: fix the policy (`security_workflows`,
`hung_after_minutes`, `retry_policy.infra`) and re-shadow.

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
| `autonomous.automation.repo_ci_watch.mode` | `off` | `off` (the pass does not run and nothing is recorded), `shadow` (everything runs, nothing writes, no alert) or `on` (the actions above are taken, budgeted). |
| `autonomous.automation.repo_ci_watch.repos` | empty | Comma-separated absolute paths of the repositories to watch. Empty means nothing is watched. |
| `autonomous.automation.repo_ci_watch.poll_seconds` | `300` | Seconds between polls. Values below 60 are raised to 60. |
| `autonomous.automation.repo_ci_watch.alert_minutes` | `10` | How long a non-green run may sit before the once-per-episode alert. `0` turns the alert off (in which case the reported threshold reads 0). |
| `autonomous.automation.repo_ci_watch.tag_count` | `5` | How many of the newest release tags are covered per poll. `0` means no tags. |
| `autonomous.automation.repo_ci_watch.hung_after_minutes` | `60` | How long a run may be `pending` before it is `hung`. Minimum 1. |
| `autonomous.automation.repo_ci_watch.security_workflows` | `security-review, codeql, dependency-review` | Workflow names that classify as `security` (record-only). Empty list means nothing classifies as `security` — set it deliberately before turning `on`. |
| `autonomous.automation.repo_ci_watch.delete_superseded` | `report` | `report` (name the superseded runs, do not delete them) or `delete` (in `on` mode, the pass issues the deletion for the superseded records it would delete, on the later pass). |
| `retry_policy.infra` | `4` | The infra retry budget the `on`-mode rerun draws from. A budget of 0 means the pass never reruns an infra run, in any mode. |

Every key is read live from `config.yaml`. Changing `mode`, `alert_minutes`,
`tag_count`, `hung_after_minutes`, `security_workflows` or
`delete_superseded` takes effect on the next poll — no restart. A value the
pass cannot use (a negative `poll_seconds`, an unknown `mode`) is refused and
nothing is written. The `repos` list and the classification vocabulary are
read on every poll, so adding a repository mid-episode starts being watched on
the following pass, and removing one stops it on the following pass without
erasing its records (the records stay readable on every surface).

## What the watch never does

It never merges, never judges design or difficulty, and never holds or starts
Automata — the watch is about the repository's CI runs, not about the
Automation queue. It never reruns a `security` or `policy` run, in any mode,
and never as a `would-do`. It never posts a second cancel for a run that is
already resolving. It never issues a deletion in `shadow`, in `off`, or in
`report`. It never re-alerts an episode that already alerted. It never spends
retry budget it does not have — a rerun that comes from a zero budget is not a
rerun, it is the operator deciding. And in `shadow` it never writes anything
at all: the record, the audit line and the would-do text are the entire
surface.

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

**Audit.** Every alert, every would-do, every rerun, cancel and deletion the
pass issues carries the actor `repo_ci_watch` in the audit trail, so the read
view and the audit trail can be cross-checked. The `alerted_at` stamp on the
record is the once-per-episode proof; the `would-do` lines in shadow mode are
the comparison evidence.

**The deletion is irreversible.** `delete_superseded: delete` in `on` mode
issues real deletions of real CI runs on the ref the record names. Before you
set it, confirm the repository's retention expectations with the process that
owns them — once a run is deleted it is gone from the CI UI, and the record
keeps the run ID but not the run's output.
