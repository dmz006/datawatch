# Glossary

Short definitions of terms used across the docs. The full reference is
[datawatch-definitions.md](datawatch-definitions.md).

## Archived unmerged

The fact that an archived Automaton's branch still holds commits that are not
on the base branch — its work never landed after the record was archived. Two
surfaces name it: the supervisor watch finding `archived_unmerged` (one per
such archived record, read-only and bounded, remediation: carry the work
forward into a new draft on that branch, or merge it first) and the supervisor
loop's guard, which keeps a completed, rejected or cancelled record with
unmerged branch work until it is archived, merged, or its branch is deleted
(`autonomous.archive_keep_unmerged` releases the guard for completed and
rejected only; a cancelled one additionally needs its
`autonomous.archive_grace_minutes` grace to have passed). The same branch
shows up in the **unlanded work** list. See
[howto/automation-supervisor-watch.md](howto/automation-supervisor-watch.md),
[howto/supervisor-loop.md](howto/supervisor-loop.md) and
[operations.md](operations.md#keep-unmerged-branch-work-on-archive-autonomousarchive_keep_unmerged).

## Carry forward (archived work)

The remediation the `archived_unmerged` finding names: start a new draft that
reuses the archived Automaton's title, spec and work branch, so the commits
that never reached the base are not orphaned; the new draft records its
origin in `carried_from_id`. The operation lives at the manager level
(`CarryForwardArchived`); the branch can also be handled from the **unlanded
work** list (keep, archive-branch, delete-worktree). See
[howto/automaton-worktree-cleanup.md](howto/automaton-worktree-cleanup.md#unlanded-branches-finished-work-that-never-reached-the-default-branch).

## Unlanded work

The read-only review of the archived Automata whose work branch has commits
that are not on the project's default branch, each recommended as **landed**,
**superseded** or **needs decision**, with the actions **Keep**, **Archive
branch** and **Delete worktree** (the last one takes a verified git bundle
backup first). It is the **Unlanded work** card on the Housekeeping sub-tab,
and also reached by REST, the CLI, MCP (read-only) and the chat verb. The
full definition, the counts and the once-a-day pending-branch alert are in
[datawatch-definitions.md](datawatch-definitions.md#unlanded-work). See
[howto/automaton-worktree-cleanup.md](howto/automaton-worktree-cleanup.md#unlanded-branches-finished-work-that-never-reached-the-default-branch).

## Pool fall-through

The setting `autonomous.capacity_fallthrough` (default off). When on, a worker
task whose requested capacity pool cannot be granted is admitted on an
alternate backend that has room instead of waiting. Verifiers never fall
through. See [howto/automata-capacity.md](howto/automata-capacity.md#pool-fall-through).

## Orphan slot reconcile

A check on every capacity tick that frees a pi-prompt slot held by a judge or
verifier with no live session and no node lease, or whose Automaton is finished.
It only releases the slot; it never ends a session, and a holder with a live
session is never freed. Each freed slot raises a `Capacity slot freed` alert. A lease younger than the slot reconcile grace
counts as having a node lease, so a just-admitted holder is left alone. See
[Slot reconcile grace](#slot-reconcile-grace) and
[howto/automata-capacity.md](howto/automata-capacity.md#orphan-judge-and-verifier-slots).

## Slot reconcile grace

The setting `autonomous.capacity_slot_reconcile_grace_seconds` (default 60
seconds, minimum 10, `0` reads the default). It is how long a freshly admitted
capacity lease counts as having a node lease in the orphan slot reconcile. It
was a fixed value before this setting existed. See
[howto/automata-capacity.md](howto/automata-capacity.md#orphan-judge-and-verifier-slots).

## Verifying lease release

When a task enters `verifying`, its worker's capacity slot is released at once and the verifier takes a slot of its own. A slow verification therefore does not keep a lane shut for other work. The slot is not handed over. See [howto/automata-capacity.md](howto/automata-capacity.md#the-worker-slot-at-verification).

## Allowed fall-through target

An LLM entry setting (`fallthrough_target`, default off). With pool
fall-through on, a marked entry may receive a worker whose requested pool is
full. The other lane fill backend is tried first and needs no mark. See
[howto/automata-capacity.md](howto/automata-capacity.md#pool-fall-through).

## agent-aider venv Python

The Python that runs aider inside the agent-aider container image: a uv-managed Python 3.12 in a dedicated venv, not Debian 12's system Python 3.11.2. It exists so soupsieve 4.x (needs Python 3.11.5 or newer) can be installed. See [Container image refresh](howto/container-image-refresh.md#agent-aider-python-and-the-soupsieve-floor).
## Ignored declared file

A file a task declared (planned, generated or under its write directories)
that the repository's ignore rules ignore and git does not track, such as a
report under `.datawatch/reports/`. The task commit checks each listed path
with `git check-ignore` and skips an ignored, untracked one instead of failing;
it is never force-staged. The note `skipped because it is git-ignored: <list>`
is information only: it is carried in the hold reason and audit record when the
task is held or warned, and is not stored when the task completes cleanly. No
commit is made when every listed path is ignored. A generated script meant to
travel with the work must be listed outside the ignored `.datawatch` folder.
The uncommitted-work check never lists an ignored, untracked file as leftover
work. A commit that still fails reports git's exit status and output in the
hold reason (up to 1000 characters). A tracked file is always staged normally. See
[howto/automaton-uncommitted-work.md](howto/automaton-uncommitted-work.md#what-happens-to-each-task).

## Launcher tooling file

One of the two files the session launcher writes when it starts a worker session, into a private per-session folder (owner-only, outside the project folder, removed at session end; copies an older version left at the working directory root are still ignored by name): `worker-mcp.json` (the session's own MCP config) and `worker-starting-context.md` (its starting-context block). They are launcher artifacts, not the task's work: the uncommitted-work check and a read-only task's dirty check never count an untracked root copy (a tracked root file that carries a launcher name, or a same-named file in a subfolder, still counts), the attempt snapshot and the automatic-commit staging skip both root names by name whether or not the repository tracks them (skip reason `launcher`), and before the task branch merges into the Automaton branch the daemon removes a launcher file the branch committed when the merge target has no such file. See [datawatch-definitions.md](datawatch-definitions.md#uncommitted-work-check) and [howto/worker-session-profiles.md](howto/worker-session-profiles.md#the-shape-of-the-start).

### Rooted path access

Reading or checking a file by opening a handle on a fixed folder first and resolving the name inside it, so the name cannot climb out of that folder. The file-drift check, the uncommitted-work check and the worktree cleanup status use it. See [howto/file-drift-guardrail.md](howto/file-drift-guardrail.md#how-the-checker-runs-git) and [howto/automaton-uncommitted-work.md](howto/automaton-uncommitted-work.md#path-handling-after-the-code-scanning-fix).

## Open high/critical alerts gate

The blocking job of the CodeQL workflow (named `Open high/critical alerts`): it fails the run when the repository has **open** high or critical alerts for that ref. Alerts already dismissed in code scanning do not count; the reason for each dismissal is recorded in `security/accepted-risks.yml`, which the gate itself never reads. It runs on pushes to `main` and pull requests that touch code, dependency, workflow or script paths, plus the weekly schedule and manual runs — a docs-only push starts no run. A fix lands as "fixed on main, closure pending the gate run": the gate's next analysis after the fix confirms it, and a red gate whose log says `Rate limited` is a wait-and-retry, not a finding. See [CodeQL local scan](howto/codeql-local-scan.md#what-it-is-and-why) for the local pre-push run, and [operations.md](operations.md#open-code-scanning-alerts-fixed-on-main-closure-pending-the-gate-run) for the fixed batch and the operator review items still owed.

## Quick reply

A one-tap reply offered on the web app — the dropdown on a prompt alert,
the session-detail Yes/No/Stop chips, or a quick command. It first goes
through an open-question gate: if the session has an open question and
the reply matches one of its options, it **answers that question**
(nothing is typed into the session); with no open question it is sent as
session input. A reply matching no option, or a session with more than one
open question, is refused with a message and nothing is sent. Messaging
channels apply the same gate to `send <id>: <text>` when the text is a
saved command (`!name` / `/name`), an approve/reject word, or an option
id or label of the session's open question; other text is typed into the
session as before. See
[howto/alerts-and-notifications.md](howto/alerts-and-notifications.md#messaging-channel-quick-replies).

## Verification-script preflight

Text-only checks on a verification or final-gate script that catch causes of a
failed first run unrelated to the task: a shell syntax error
(`verify_script_syntax`), a helper called with a different shell than its
shebang (`verify_script_shell_mismatch`), a piped `grep -q` under `pipefail`
(`verify_script_pipefail_grep_q`), and an allowed-file list that is not
cumulative over the whole plan (`verify_script_allowed_files`). Runs on every
decompose-lint pass (recorded even in mode `off`; drives the retry in
`enforce`) and when a task edit sets a verification script; every finding is
`block`. The dry run does not run it. See
[howto/automaton-verification-scripts.md](howto/automaton-verification-scripts.md#5a-preflight-checks-for-planner-written-scripts).

## Decompose output unparseable
The planning failure recorded when a decompose reply is over 8 MiB, empty, or fails the parse or lint step (error text starts `decompose output unparseable: `, source `decompose-parse`, category `invalid-response`, not retryable). The Automaton rolls back to `draft` with the reason in `last_decompose_failure` and the raw output kept as `decompose-output.json.failed`; the daemon keeps running. Since the 2026-11-18 fix, bad output can no longer abort the daemon. See [howto/decompose-async.md](howto/decompose-async.md#when-the-decompose-output-cannot-be-parsed) and [operations.md](operations.md#decompose-output-and-daemon-aborts-sigabrt-core-dump).

## Observer pull attempt (last attempt time)
The record of one fetch the daemon made to a pulled observer peer (`GET {peer}/api/observer/stats?v=2`). Its start time is `last_attempt_at`, and it is published together with that attempt's outcome: `stale`, `stale_reason`, and `last_success_at`. A fetch still in flight shows nothing new, so the time and the outcome always describe the same attempt. See [howto/federated-observer.md](howto/federated-observer.md#pull-mode) and [operations.md](operations.md#observer-pull-reading-a-stale-peer).

## Staging scan (agent-base)
The blocking Trivy scan the image refresh runs on the `refresh-YYYYMMDD` staging tag before promoting an image. Its exceptions come from `security/accepted-risks.yml` via the generated `.trivyignore`; a failure never replaces a published image. See [Container image refresh](howto/container-image-refresh.md#staging-scan-failures-on-agent-base).

## Per-LLM usage hold

A usage back-off flag scoped to one LLM entry (and, optionally, a named list
of its models — **held models**) rather than a whole backend. While it holds,
new autonomous work on those models waits with a `backoff:` reason naming them;
work on the other models of the entry and on other backends keeps running. It
never blocks an interactive session and survives a restart. The burn guard's
`hold_entry` rung sets it scoped to the entry it guards. See
[howto/usage-backoff.md](howto/usage-backoff.md#holding-only-certain-models).

## Cheaper-model rung

The burn guard's `route_cheaper_model` ladder rung: it keeps the entry's
backend and sends the entry's new work to the entry's cheaper model — Haiku
for a Claude entry, otherwise its own cheapest other priced model by its
per-model cost rates (or the model the entry's `route_cheaper_model` field
names). It is a no-op for an entry that resolves no cheaper model, and it is
**not in the default ladder**; you opt in by naming it where it sits, before
the hold rung. Work already running continues. See
[howto/burn-guard.md](howto/burn-guard.md#the-ladder).

## Per-model cost rate

A USD-per-1K-token rate set on one model of an LLM entry (`in_per_k`,
`out_per_k`, plus `cache_read_per_k` / `cache_write_per_k`, any subset). It
wins for that model over the entry's per-backend rate, which is the
field-by-field fallback. Per-model rates price the cost report's by-model rows,
the burn guard's per-entry projection, and the comparison the cheaper-model
rung uses to pick an entry's cheapest other model. See
[datawatch-definitions.md](datawatch-definitions.md#cost-rates-usd--1k-tokens).

## Metered vs flat guard plan

How a burn guard entry reads its budget: `flat` — a weekly percentage against
the allowance readings (the default `claude-code` entry), priced per model;
`metered` — a dollar spend figure against the rate-based projection, using the
per-model rates. An entry with `usage_source: none` is never held. See
[howto/burn-guard.md](howto/burn-guard.md#guarding-several-paid-llm-entries-entries).

## Priority stall

A high-priority Automaton (at or above the `priority_floor`, default 80) that has waited in the queue longer than `stall_threshold_minutes` (default 30) with a blocker lane fill will not clear: the blocker is one of `lane_full`, `plan_coverage_blocked`, `held`, `stale`, `proposal_not_confirmed`, `judge_timed_out`, `merge_pending` or `other_refusal`. The watch names it as a `priority_stall` finding and the daemon publishes one `priority_stall` event per Automaton per blocker, which can wake the supervisor. The detector only reads; it never approves or acknowledges. See [howto/automation-supervisor-watch.md](howto/automation-supervisor-watch.md#priority-stall-wakes-the-supervisor).
