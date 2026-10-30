**Date:** 2026-10-29
**Version:** none until merge (the minor bump is taken at merge time)

# Search before create — Step 2 of the Automation coordinator's duties

This is the plan for Step 2 (duty 4, "Search before create") of
[the Automation supervisor duties proposal](2026-10-25-automation-supervisor-duties-proposal.md),
one of the confirmed coordinator duties. Surface answers: the operator answered
Q4 as option (b) (full set) for every duty, recorded on the proposal document
on 2026-10-25; that supersedes the document's recommendation (a) for this duty.
Depends on: none.

## What today does, and what it misses

`CreatePRD` in `internal/autonomous/manager.go` runs the injection guard and
the backend/model validation, and creates. `checkPendingDuplicate` in
`internal/autonomous/automation_insert.go` stops a second *pending insert* of
the same Automaton inside the same Automation. That is a duplicate guard, not a
search: nothing compares a new title or spec against Automata across statuses,
registry rows in `docs/plans/README.md`, or unmerged branches at create time.
Duplicates are only ever found by an operator who happens to look.

## Goal

A read-only duplicate search, run when creating: the proposed title and spec
are matched against (1) Automata across statuses, (2) the plans-registry rows,
and (3) unmerged branches. Any overlap is surfaced as a warning returned
alongside the create result on every surface where create exists. Warnings
never block: the create succeeds even with warnings, and the operator decides
whether the overlap is real.

## Scope

- `internal/autonomous` — the duplicate search (title/spec similarity across
  Automata in every status, registry rows, unmerged branch names and subjects)
  and its wiring into the create path. Read-only: it inspects Automata, the
  branch list and the registry file; it mutates no state.
- `internal/config` — one optional toggle (`autonomous.search_before_create`,
  default on) in the config layer and the config-reference entry.
- `internal/server` — the `warnings` field on the create REST response, and
  the PWA files under `internal/server/web` (read-only warning line, locale
  keys in all five bundles).
- PWA files under `internal/server/web` — the create flow shows the warnings
  as a read-only line; phone-width and mobile-fit rules.
- `internal/mcp` — the create tools (`prd_create`, `automation_create`, `automaton_create`)
  return the same warnings alongside the results they return today.
- `internal/router` — no route changes: the response envelope only carries the
  new field; checked so the warning passes through unchanged.
- `cmd/datawatch` — the CLI create commands print any warnings after the
  create confirmation.
- `internal/federation` — the federated create call carries the same response
  shape; no capability change.

Plus the mobile parity issue (datawatch-app repository) because the create
response shape changes — a REST contract change — and the docs: CHANGELOG,
`docs/testing-tracker.md`, `docs/config-reference.yaml`, the how-to for
creating Automata.

## Phases

Four stories, in order. Each ends with its phase-completion checklist and its
own verification script under `.datawatch/verify/`, and none of them changes
the status of any other row.

| # | Story | Status | Depends on |
|---|---|---|---|
| 1 | The read-only duplicate search: title and spec matched against Automata in every status, the plans-registry rows, and unmerged branches; a list of structured warnings, called from the create path. Pure search: no state is created or mutated. | Planned | - |
| 2 | Warnings on every surface where create exists: the create REST response `warnings` field, the MCP create tools, the CLI create output, the comm-channel create reply, and the federation response shape; the optional `autonomous.search_before_create` toggle in `internal/config`, default on. | Planned | 1 |
| 3 | The PWA side: the read-only warning line under the create control, locale keys in all five bundles, phone-width/mobile-fit rules, and the datawatch-app mobile parity issue filed (the create response shape is a new REST contract). | Planned | 2 |
| 4 | Docs and verify: the CHANGELOG entry, the `docs/testing-tracker.md` section, the `docs/config-reference.yaml` key, the how-to update for creating Automata, the phase-completion checklists, and this row's verification script. | Planned | 3 |

## Status

| Item | Value |
|---|---|
| Plan status | Planned |
| Confirmation | not yet (the proposal's Confirmation line covers Step 1 only; the operator's word decides filing) |
| Q4 surfaces | option (b) — full set for every duty: config, REST, MCP, CLI, comm channel, PWA; mobile parity issues where the REST or WebSocket contract changes |
| Date | 2026-10-29 |
| Version | none on this branch; the minor bump is taken at merge |
| Registry row | 252 in `docs/plans/README.md` (one above the highest first-column number) |

## Parity surface

Surface policy: the full set, per the operator's Q4 answer (option b, full set
for every duty; recorded on the proposal document on 2026-10-25). This step
changes the *shape* of the create response, so the mobile parity rule
applies.

- **YAML/config** — included: one optional toggle (`autonomous.search_before_create`, default on) in the config layer plus the config-reference entry, on every YAML/config surface.
- **REST** — included: the create response gains a read-only `warnings` field; it never blocks the create. This is the new REST contract.
- **MCP** — included: the create tools return the same warnings alongside the results they return today.
- **CLI** — included: the create commands print any warnings after the create confirmation.
- **comm channel** — included: the create reply carries the warnings, matching the other create surfaces.
- **PWA** — included: the create flow shows the warnings as a read-only line, five locale bundles, phone-width and mobile-fit rules.
- **Android** — included: the create response shape changes, i.e. the REST contract changed — the datawatch-app mobile parity issue is required and is filed in Story 3.
- **iPhone** — included: same contract change as Android; the same parity issue covers the iPhone client.

## What this does not do

- It never blocks or refuses a create: the warnings travel with the create
  result and only the operator acts on them.
- It does not change any Automaton, any registry row, or any branch: the
  search is read-only.
- It does not add alerts or notifications; the warning is returned with the
  create response on each surface.
- It does not take a version number on this branch.
