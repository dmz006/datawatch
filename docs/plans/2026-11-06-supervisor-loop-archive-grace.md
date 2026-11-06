**Date:** 2026-11-06
**Version:** v9.98.1 (docs-only change; the daemon stamps the release version, no `internal/server/api.go` or `cmd/datawatch/main.go` edit)

# Supervisor loop: document what the close step closes and when

## Problem

The supervisor loop's `close_finished_records` step and its archive grace for
cancelled Automata exist in the code and in every config surface, but the
operator documentation only ever says the step "closes the records of Automata
that have finished". Nothing in `docs/operations.md`, the operator how-to,
the datawatch definitions, or `docs/config-reference.yaml` says what the step
actually closes and when: that completed and rejected Automata are archived on
the next idle pass, that a cancelled Automaton is archived only after
`supervisor_loop.archive_cancelled_after_minutes` has passed since its most
recent cancel (default 60, `0` archives at once), that a cancelled Automaton
whose branch holds commits that are not on the base branch is never archived
by the loop, and that the two archive paths are told apart on the record by
their actor (`supervisor-loop` for the loop, `operator` for an operator
archive). An operator reading the docs could not tell when a cancelled
Automaton would disappear — or that it might never disappear on its own.

## Scope

- `docs/operations.md` — a new subsection under the daemon-restart / in-flight
  Automata material: **What the supervisor loop closes, and when**.
- `docs/howto/supervisor-loop.md` — the `close_finished_records` step row and
  the settings table.
- `docs/datawatch-definitions.md` — the `close_finished_records.enabled`
  entry and the `archive_cancelled_after_minutes` entry.
- `docs/implementation.md` — the key list and a close-step bullet (files,
  grace rule, branch guard, actors).
- `docs/config-reference.yaml` — `archive_cancelled_after_minutes` in the
  `supervisor_loop` block, next to `run_retention`.
- No code, PWA, app, or generated-file changes. No tracker IDs in the
  user-facing docs; Automaton/Automata wording throughout.

## Phase

**Phase 1 — operator documentation** (single phase; nothing to build or
deploy, so the Phase 2+ gates in `AGENT.md` are N/A for this task):

1. **Problem** — as above.
2. **Decisions** — see below.
3. **Steps** — apply the five doc edits; re-read each; grep the touched
   user-facing files for stray tracker IDs; verify the how-to / definitions /
   config-reference agree on default, 0 semantics, grace start and the
   unmerged-branch rule.
4. **Verify** — `rtk git diff` over the five files; confirm no code file and
   no version const is touched.

Phase completion checklist (per `AGENT.md`, N/A lines justified):

- [x] **Build** succeeded — N/A: no code change; `rtk git diff --stat` shows
  docs only.
- [x] **Test** succeeded — N/A: no code change; verification is re-reading
  plus a cross-file consistency grep (default 60, `0 = at once`, graceful from
  the most recent cancel, never for unmerged branch work).
- [x] **PWA build** (if PWA changed) — N/A: no PWA change; the existing
  `archive_cancelled_after_minutes` field (`internal/server/web/pwa/app.js`,
  default `60`) is unchanged.
- [x] **Mobile app build** (if apps changed) — N/A: no Android or iPhone/iOS
  change; no new client-visible affordance in this task.
- [x] **CHANGELOG** update — N/A: the daemon maintains `CHANGELOG.md`; no
  version or changelog edit per the task rule.
- [x] **Version update** — N/A: same rule; verified `internal/server/api.go`
  and `cmd/datawatch/main.go` untouched.

## Decisions

**Grace is measured from the most recent cancel.** The clock starts at the
timestamp of the record's latest `cancel` decision (falling back to the
record's last update). Consequences: re-cancelling a held Automaton pushes the
archive back — that is the operator's handle on keeping it; `0` disables the
grace and archives cancelled Automata on the next pass; completed and rejected
Automata have no grace at all. The default stays 60, matching the setting
default, the PWA field and the config parse default
(`internal/supervisorloop/settings.go`).

**Unmerged-work check and its fallback.** A cancelled Automaton is never
archived by the loop while its branch is present and holds commits ahead of
its base branch (`main` when no base branch is set). The guard
(`internal/autonomous/supervisor_loop_archive_branch.go`) is read-only
(`for-each-ref` plus the branch comparison) and deliberately conservative:
when it cannot determine the state — no base repository configured, a git
error, or an unknown comparison — it keeps the record. Only a branch that is
gone, a record with no recorded branch, or a branch with zero commits ahead
lets the record go. The fallback rule, stated plainly: *in doubt, keep*. So
the operator's escape hatches are the same three: archive it, merge its work,
or delete its branch.

**Actor recording.** The loop archives under the actor `supervisor-loop`
(`supervisorLoopActor`); the operator archive path (`Manager.Archive`) is
unchanged and records under the actor `operator`. The archive record is
therefore self-describing about who closed it, and the step's run detail
("closed N … held K cancelled within the M-minute grace, kept J cancelled with
unmerged branch work") makes the loop's own decisions observable.

**Wording.** User-facing files use Automaton/Automata; no tracker IDs in
`docs/operations.md`, `docs/howto/supervisor-loop.md` or
`docs/datawatch-definitions.md`. `docs/implementation.md` may name files and
identifiers.

## Parity surface

The capability (archive grace setting + close-step behaviour) is daemon-side
and already present in every config channel; this change only documents it,
so no surface gains a new affordance:

| Surface | Included | Intent |
|---|---|---|
| REST | Included | The setting already writes over `PUT /api/config` and reads over `GET /api/config` as `autonomous.supervisor_loop.archive_cancelled_after_minutes`; loop vs operator archives are distinguishable on PRD records over the same endpoints. Docs-only: no endpoint change. |
| MCP | Included | The generic `config_set` tool covers the key; no new tool added in this change. |
| CLI | Included | `datawatch config set autonomous.supervisor_loop.archive_cancelled_after_minutes <n>` already works through the generic `config set`; noted in the plan only, CLI text unchanged. |
| Comm channel | Included | The `configure supervisor_loop.archive_cancelled_after_minutes=<n>` verb already routes (covered by the existing comm-channel test); no comm change. |
| YAML/config | Included | `docs/config-reference.yaml` gains the key in the `supervisor_loop` block next to `run_retention`, with the default 60, the 0 semantics and the never-archive-unmerged rule in the comment. |
| PWA | Included | The field already exists under Settings → Automata → Supervisor loop (`app.js`, default `60`, with the definition help link); this change documents the documented default to match it, no PWA edit. |
| Android | Excluded | This change adds nothing client-visible: the archive grace is daemon-side behaviour, the setting already ships on the PWA, and no app screen is added or modified — so there is no app-side parity item to file for this task. |
| iPhone/iOS | Excluded | Same as Android: docs-only change, no new app surface, no existing app feature altered. |
