# Rules-review fixes: truncation-marker refusal, spec-read completeness, and the parity issue

Date: 2026-11-03
Task: task-7db40001 (auto-fix rules-review findings, attempt 1)
Status: done — the only thing still owed at merge is the spec-read-full-and-guard.sh live
sandbox run (see "Remaining gate debt"); the gate report is at
docs/plans/reports/archive/2026-11/2026-11-03-auto-fix-rules-review-final-gate.md

## What the rules review flagged

1. **OpenAPI:** the spec/description write refusals (`…[truncated: kept N of M bytes]`
   marker → 400) were not visible on the `spec`/`description`/`new_spec`/
   `new_description`/`title` fields; the two template routes had no spec/description
   refusal; `GET /api/autonomous/prds/{id}` did not document the field-cap contract —
   which fields are always whole, which are capped, where the full text lives
   (`?full=1`, `/outputs`) — and had no edit-operation counterpart for the PRD's own
   title/spec.
2. **MCP docs:** `autonomous_prd_create`/`autonomous_prd_edit` and the template tools
   did not advertise the refusal or the full reads; `docs/mcp.md` mentioned the cap
   but not the refusal.
3. **Testing tracker:** no section for the truncation-marker refusal feature, even
   though guard tests exist in `internal/server`, `internal/mcp`, `cmd/datawatch` and
   the helper in `internal/autonomous`.
4. **Datawatch app parity issue:** none filed. The comm surface (`internal/router`)
   was the only write surface without a local refusal — MCP and CLI refuse marker text
   locally, but the comm verbs — `autonomous create`, `autonomous edit-task`,
   `autonomous edit-story`, `autonomous add-story`, `autonomous add-task`,
   `autonomous template-create`, `autonomous template-update` — and the review-verb's
   `retry`+`new_spec` path forwarded whatever the operator typed straight to the daemon.
5. **Final gate report:** the verification suite row said PASS with no note that
   `spec-read-full-and-guard.sh` was still owed, which made the report read as
   complete when it was not.

## Scope / non-goals

In scope: the five items above, in this repo only (the parity issue is drafted and
pointed at the datawatch-app repo with its issue number; filing it there happens in
the datawatch-app change set that owns the comm verbs — not this one).

Out of scope: changing the refusal message or the marker format, new tests beyond the
one comm-surface guard test, touching the daemon's side-file store, and the
datawatch-app comm-verb wiring itself.

## Decisions

- The comm guard is a **local refusal** (no request sent) with the same
  operator-facing error text as the MCP and CLI guards: consistent operator wording,
  and it works even when the daemon it would call does not implement the guard
  (the exact gap the parity issue covers).
- `handleAutonomousReviewTask` gets the guard at the entry point: it is the one router
  path that forwards a task `spec` write and was unguarded.
- openapi.yaml: refusals land in the 400 descriptions (the file's existing style for
  these routes) plus a sentence on each operation's `description` for the write
  endpoints; the `GET /api/autonomous/prds/{id}` description gains the field-cap
  contract paragraph.
- The parity issue targets the comm-verb paths that still forward unguarded to a
  daemon without the guard: all eight `sx2_parity.go` comm verbs (create,
  edit-task, edit-story, add-story, add-task, template-create, template-update)
  and `autonomous_failure_escalation.go` `handleAutonomousReviewTask`. Until it lands,
  the refusal only fires locally (this change) — a daemon lacking the guard would
  still accept marker text on the comm path.

## Implementation

Comm guards (new):

- `internal/router/sx2_parity.go`: `commTruncationRefusal` helper + marker checks in all
  eight comm verb paths — `autonomous create` (spec), `autonomous edit-task`
  (new_spec), `autonomous edit-story` (new_description), `autonomous add-story`
  (description), `autonomous add-task` (spec), `autonomous template-create` (spec) and
  `autonomous template-update` (spec).
- `internal/router/autonomous_failure_escalation.go`: `handleAutonomousReviewTask`
  (the review verb's `retry` replacement spec) checks the marker before
  forwarding to the daemon.
- `internal/router/sx2_parity_test.go`: `TestSx2_Comm_TruncationRefusal` — the marker
  text is refused with the exact operator-facing message, clean text passes through,
  and an empty optional value never refuses.

Docs:

- `docs/api/openapi.yaml`: 400 refusal descriptions on PRD create, PRD edit
  (`prds/{id}` PATCH — now a specified operation), `add_task`, `add_story`,
  `edit_story` (specified), `resolve_task_review`, template create and update
  (specified); the `GET /api/autonomous/prds/{id}` description documents which
  fields are always whole (Automaton `spec`, story `description`, task `spec`),
  which are capped (PRD `memory_report`, task `error`, `prior_attempt_hint`,
  `start_failure_tail[]`, `attempt_refs[]`, verdict/verifier text — the cap is
  `autonomous.detail_text_cap_bytes`), the marker, the `truncated` array, and the
  full-text reads (`?full=1`, `/outputs`, `tasks/{tid}/outputs`).
- `docs/mcp.md`: a "Truncation-marker refusal (spec and description writes)" bullet
  naming every tool that enforces it and the full reads, next to the
  `autonomous_prd_*` tool list; template tool names added.
- `docs/testing-tracker.md`: new section "Truncation-marker refusal on spec and
  description writes — unreleased" with evidence rows per surface (helper, REST,
  MCP, CLI, comm, docs, live debt).
- `CHANGELOG.md`: [Unreleased] entry for the comm guards.

Planned (this directory):

- `docs/plans/2026-11-03-spec-truncation-parity-issue.md` — the datawatch-app parity
  issue draft (ready to file, not yet filed).
- Final gate report updated to name the remaining gate debt.

## Implementation order

1. Comm guards + test (build, gofmt, tests green before docs).
2. openapi.yaml (YAML parses, no duplicate path keys introduced; the file already
   carries a duplicate `description` key at HEAD — pre-existing, untouched).
3. docs/mcp.md, testing-tracker section, CHANGELOG.
4. Parity issue draft + gate report.
5. Verify script + tests.

## Verification checklist

- [x] `gofmt -l internal/router/autonomous_failure_escalation.go internal/router/sx2_parity.go internal/router/sx2_parity_test.go` clean
- [x] `rtk go build ./...`
- [x] `rtk go test ./internal/router/ -count=1 -run 'Trunc|Spec|Cap|Detail|Edit|Marker'` (32 passed)
- [x] `rtk go test ./internal/router/ -count=1` (471 passed), `./internal/autonomous/` (203 passed), and `./internal/server/ ./internal/mcp/ ./cmd/datawatch/` with the `-run 'Trunc|Spec|Cap|Detail|Edit|Marker'` filter (268 passed top-level; the full suites pass too — see the gate report)
- [x] openapi.yaml parses (`python3 yaml` clean; single path key per route for the new routes; the pre-existing duplicate `description` key was already at HEAD)
- [x] `docs/mcp.md`, `docs/api/autonomous.md`, `docs/testing-tracker.md`, `CHANGELOG.md` updated
- [x] Parity issue drafted: `docs/plans/2026-11-03-spec-truncation-parity-issue.md`
- [x] `spec-read-full-and-guard.sh` **unit/guard steps** run against this task's changed set (gofmt, build, the marker/refusal test filter, docs changed) — PASS. The script lives in the parent worktree (its `cd` targets that tree), applied its steps to the files this task actually changed.
- [ ] `spec-read-full-and-guard.sh` **live sandbox run** — owed at final gate (see below)

## Remaining gate debt (the one line the gate report now carries)

The final gate report is written:
`docs/plans/reports/archive/2026-11/2026-11-03-auto-fix-rules-review-final-gate.md` — it shows every
unit and guard row green (above) and states explicitly that the live half of
`spec-read-full-and-guard.sh` was **not run** in the verification suite: the suite
stopped at the failed docs gate and recorded the rest as pending, while the earlier
read of the row was "PASS". The live sandbox run — create a PRD with a large
output-bearing task over an ephemeral daemon, confirm `GET prds/{id}` lists the
marker field in `truncated`, confirm the marker-bearing edit is refused 400, confirm
`?full=1` and `/outputs` return the uncapped text — is owed at final gate. Nothing
above is blocked on it; it is a live check of behaviour whose unit guards already
pass.

The second merge-time item: file the datawatch-app parity issue from the draft and
record the URL back into the draft (owner: supervisor).
