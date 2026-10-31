# Plan coverage: stop flagging the files a plan creates and the spec's stray words

**Date:** 2026-10-30
**Status:** Done on the branch; release version set at merge time
**Tracker:** the row in `docs/plans/README.md` for "plan coverage gate
stops flagging created files" (filed from the 2026-10-30 inventory of the
things the supervisor does by hand)

Operator request, 2026-10-30, quoted: "What else are you doing by hand that
datawatch should be doing?" — the entry in the inventory that this work
closes is the one where the operator had to acknowledge, by hand and on
every Automaton, a failing plan-coverage report whose failures were all
noise.

## Defect

Before an Automaton is approved, the deterministic plan requirement
coverage gate stores a record: the spec requirements the plan does not
visibly cover, plus the structural findings (dangling references, a write
task with no files, and so on). In `block` mode (the default) a failing
record locks Approve until the operator acknowledges that exact failing
surface.

In practice every plan this repository carries came back with 20 to 50
findings that were not gaps at all, in two shapes:

- **Files the plan itself creates, reported as missing.** The structural
  check's unresolved-file rule compares the spec's file names against the
  tree *before* the work exists, so every new source file, test or doc the
  plan is about to write was a "missing file" finding — a plan's own output
  was failing the plan.
- **Stray words from the spec, counted as missing requirements.** A file
  name, a backend name or a quoted phrase in the spec body — often the
  *Read first* list of file names — was each counted as a requirement
  that the plan "missed".

So each Automaton needed a manual acknowledgement that was acknowledging
noise, not a real gap. The operator was doing datawatch's verification
work by hand.

## The two rules

### Rule 1 — a file the plan creates is not a missing file

A file that a task's title or spec says it creates, adds, writes or
generates — or that is new in the plan's own file list (the task's own
file declarations) — is the plan's own output, not a reference to
something that should already exist. The unresolved-file rule stops
reporting it.

One limit keeps the rule honest: the file's **parent folder has to exist
or be created by the plan**. A path into a folder that nothing in the
tree or in the plan provides is still reported, so a genuinely broken
reference into a folder that is never built is never silently skipped.
Earlier tasks' already-generated files keep counting as the tree they
are, and the rule only ever *withholds* a finding — it never invents
coverage.

### Rule 2 — only scoped sentences count as requirements

Requirements are extracted only from the sections named by the sections
setting (default: the **scope** and **constraints** sections). Within
those sections, a path or config key counts only through the sentence
that holds it — no more standalone file names — and a sentence shorter
than the minimum-length setting (default 30 characters) is dropped
before the coverage ratio is computed. A spec that carries none of the
named sections falls back to the whole spec, so the rule never shrinks a
spec that did not label its sections.

## Settings (all under `autonomous.`, all configurable, both filters on by default)

| Key | Type | Default | Meaning |
|---|---|---|---|
| `autonomous.plan_coverage_suppress_created_files` | boolean | on | Rule 1 active: a file a task says it creates, or that is new in the plan's own file list, is not a missing-file finding while its folder exists or is created by the plan |
| `autonomous.plan_coverage_scoped_requirements` | boolean | on | Rule 2 active: requirements are extracted only from the named sections, through their sentences |
| `autonomous.plan_coverage_requirement_sections` | comma-separated section names | `scope,constraints` | The section names whose sentences count as requirements; names are trimmed and lowercased, an empty list restores the default, and a spec without any named section falls back to the whole spec |
| `autonomous.plan_coverage_min_requirement_chars` | whole number | 30 | The shortest sentence, in characters, that counts as a requirement; `0` reads the default of 30 |

Go type: `config.PlanCoverageFilters`, with fields
`SuppressCreatedFiles`, `ScopedRequirements`, `RequirementSections` and
`MinRequirementChars` (JSON `suppress_created_files`,
`scoped_requirements`, `requirement_sections`, `min_requirement_chars`).
The two booleans are on when the key is absent (a pointer in the config
so an explicit `off` survives a YAML or JSON round-trip); the two value
settings are resolved by `EffectivePlanCoverageFilters` with the defaults
above.

## What the record shows now

The stored plan-coverage record (the one Approve gates on and the
operator acknowledges in `block` mode) gains two fields, both always
present in the JSON:

- `suppressed` — `{created_files, requirements}`: the count of findings
  each filter withheld when the record was computed. Zero when the
  filter was off at compute time or suppressed nothing. A suppressed
  finding is not hidden — it is *counted*, so the operator sees exactly
  how much the filters hid next to the findings that remain.
- `filters` — the four filter values pinned to the record when it was
  computed (like the existing mode and ratio pins), so a stored record
  can be told apart from the live config and a later config change
  cannot silently re-interpret — or be confused with — the failing
  surface that was acknowledged.

The suppressed count and the filters **never participate in the
ack fingerprint**: an acknowledgement of a genuine failing surface stays
valid when only the filters or the suppressed count change, and a plan
change still voids it exactly as before.

## Surfaces added

- Core: the created-file allowance in the structural check and its
  helpers (does a task create a file, does the folder exist or get
  created); the scoped requirement extractor and the count of the
  requirements it stops counting; the record build that applies both
  filters and stores the `suppressed` and `filters` fields.
- Config: `config.PlanCoverageFilters`, the four keys with their
  defaults, `EffectivePlanCoverageFilters`, round-trip-safe on YAML and
  JSON.
- REST: the four keys on the autonomous config read and write; the new
  read route `GET /api/autonomous/prds/{id}/plan_coverage` (record,
  filters and suppressed counts with their total), described in both
  OpenAPI files.
- MCP: the read tool `autonomous_prd_plan_coverage_get`; the four
  settings on the autonomous config set/get tools.
- CLI: `datawatch autonomous prd-coverage <id>` shows the suppressed
  counts and the filters the record was pinned with (`--json` for the
  raw record); `datawatch config set` reaches the four keys.
- Comm channel: the `autonomous plan-coverage <id>` verb (same lines as
  the CLI); `configure autonomous.<key>=<value>` for the settings.
- PWA: the plan coverage panel gains the "Suppressed by the plan
  coverage filters" line with the created-file and requirement counts and
  a `?` help link to the glossary entry; the Automata settings card gains
  the four settings rows, each with its own `?` help link; the strings
  are in all five locale bundles.
- Docs: the how-to `docs/howto/plan-coverage-filters.md`, the operations
  section, the `docs/config-reference.yaml` entries, the
  `docs/datawatch-definitions.md` glossary entry both filters link to,
  and the API/MCP mapping note.
- Mobile: the parity issue, recorded (including the ready-to-file text
  and the filing record) at
  `docs/plans/parity-issues/plan-coverage-filters.md`.

## Tests

- `internal/autonomous` — created-file allowance (helpers and findings),
  scoped requirement extraction and its report, the record build with
  both filters and the stored suppressed count, the settings resolution
  and the gate behaviour with the filters on and off
  (`plan_coverage_created_test.go`, `plan_coverage_created_findings_test.go`,
  `plan_coverage_scope_test.go`, `plan_coverage_scope_report_test.go`,
  `plan_coverage_write_files_report_test.go`,
  `plan_coverage_settings_test.go`,
  `plan_coverage_filter_settings_test.go`,
  `plan_coverage_filters_gate_test.go`, plus the existing
  `plan_coverage_*` suites for regressions).
- `internal/server` — the four keys on the config read/write and the new
  read route (`plan_coverage_filters_config_test.go`,
  `plan_coverage_filters_read_test.go`).
- `internal/mcp` — the read tool and the four settings
  (`plan_coverage_filters_mcp_test.go`).
- `internal/router` — the comm verb and the config-set values
  (`plan_coverage_filters_comm_test.go`,
  `plan_coverage_filters_configure_test.go`).
- `internal/server/web/app-plan-coverage-filters.test.js` — the panel's
  suppressed line and `?` help link, the four settings rows, the
  no-record case, and the locale keys in all five bundles.

## Live check

The live run against a real Automaton — decomposing with both filters on,
reading the stored record's suppressed counts from every surface, and
acknowledging the remaining genuine surface in `block` mode — is still
owed. No live-check report exists for this work yet; when it is run, it
belongs under the plan requirement coverage gate rows in
`docs/testing-tracker.md`.

## Phase status

Done on the branch; release version set at merge time.

## Parity surface

- **REST:** included. The four filters are additive keys on the
  autonomous config read and write; `GET
  /api/autonomous/prds/{id}/plan_coverage` is a new read route, and the
  `suppressed` and `filters` fields appear on the existing plan read —
  both OpenAPI files describe them.
- **MCP:** included. The read tool `autonomous_prd_plan_coverage_get`
  and the four settings on the autonomous config set/get tools; no
  existing tool's contract changed.
- **CLI:** included. `datawatch autonomous prd-coverage <id> [--json]`
  shows the record's suppressed counts and pinned filters; `datawatch
  config set autonomous.<key>=<value>` writes the settings.
- **comm channel:** included. The `autonomous plan-coverage <id>` verb
  and `configure autonomous.<key>=<value>`, same values as the CLI.
- **YAML/config:** included. The four keys under `autonomous.` with the
  defaults above, round-trip-safe, documented in
  `docs/config-reference.yaml`.
- **PWA:** included. The plan coverage panel's suppressed line and `?`
  help link, the four settings rows with help links on the Automata
  settings card, and the strings in all five locale bundles.
- **Android:** excluded this phase — the four settings and the
  suppressed line have no companion-app consumer yet. The parity issue
  (ready-to-file text, setting labels, REST contract and locale keys)
  is recorded at `docs/plans/parity-issues/plan-coverage-filters.md` and
  filed in the companion-app tracker; tracked on the row in
  `docs/parity-status.md`.
- **iPhone/iOS:** excluded this phase — same reason as Android; the same
  recorded parity issue covers it (the app core is shared, so the filed
  issue names both).
