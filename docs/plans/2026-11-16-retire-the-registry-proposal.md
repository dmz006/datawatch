# Retire the file registry: plans, backlog and status as datawatch records (proposal)

- Date: 2026-11-16
Confirmed: no

- Status: proposal, written for operator confirmation. Nothing here is decided except what is quoted above.
- Scope: a proposal only, with evidence. It proposes retiring the file-based plans registry (`docs/plans/README.md`) and the findings ledgers (`docs/plans/**/*findings*.md`) as primary sources of truth, replacing them with datawatch's own constructs — the project, plans (including release plans and themes), Automations and Automata — and keeping `README.md` as a configurable, off-by-default export. It does not draft any Automaton, change any code, setting, document or file, and it files nothing.
- Recommendation (the position this document proposes, not a decision): the datawatch records are the primary source for backlog, plans, findings and status; the repository files become a read-only archive plus a configured export; the registry and finding record kinds are retired in favor of the actual constructs the operator named.

## Operator direction (quoted verbatim)

- 2026-11-14: "registry is being retired, track in automation"
- 2026-11-16 21:15: "We're supposed to be using the actual constructs of our project plan Automations and automata"
- 2026-11-16: "the Projects tab is showing the backlog, not the plans"

The earlier design that moved registry rows and findings into daemon records is [plans, backlog and status inside datawatch](2026-11-10-plans-and-backlog-in-datawatch.md), which quotes the operator's 2026-11-10 direction and read it as: datawatch is the primary source, the repository gets an optional push-out export, and the step-1 import of registry rows and findings has already shipped. This proposal is the later part the operator's later quotes point at: stop keeping the file registry as the thing to look at, and stop the two record kinds that only mirror the file.

## 1. Target model

After retirement, every piece of information the file registry and the ledgers carry lives in a first-class construct:

| Today's file thing | Becomes |
|---|---|
| A planned work item with a spec (a registry row linking a design doc) | an **Automaton** (draft, running, or completed), grouped by theme in an **Automation**, under a **plan** |
| A backlog item with no spec yet ("file it, draft when it gets real") | an **Automaton drafted** under the Automation that groups its theme, or a **plan** item; read from Automations, not a table |
| A finding (a defect, gap or missing doc with evidence) | a **finding on an Automaton** tagged `kind:bug` (or `kind:feature`), filed the tick it is found, after the overlap check |
| A shipped row | the Automaton's completed state with the release it shipped in, read from the Automata list and the ship record |
| A decision (quoted operator direction) | the **decision record** on the Automaton, plus the quote kept in the design doc where it was heard |
| A group of related rows ("11 of them") | an **Automation** with a theme inside a **plan** |
| A release worth of work | a **release plan** (plan kind `release`, label `10.0` or `11.0` by default) whose Automata and Automations are grouped by theme; see the definitions entry for a release plan in [datawatch definitions](../datawatch-definitions.md) and `internal/planrecords/release_migrate.go` |
| The roadmap table a human reads | an **export** of those records into `docs/plans/README.md`, controlled by a setting, off by default |

What stays a file: design documents, handover plans and history. They remain the readable home for prose; they are not status sources. The status and decisions live in the records and on the Automata.

## 2. History: what we inherit from 9.0

Counts below were taken from this tree on 2026-11-16 and name the file they were counted in. The operator's line "everything from 9.0" is the horizon: all shipped releases since the 9.0 line are in scope for the conversion.

- Releases since 9.0: 638 release headers in `CHANGELOG.md` (all `## v9.` lines); 417 release headers before the 9.0 line. `HISTORY.md` already carries the shipped-work summaries, and the live registry's own rules say "shipped work is summarized in HISTORY.md, not listed here" (first lines of `docs/plans/README.md`).
- Registry rows: 549 numbered rows in `docs/plans/README.md` (row numbers run to 560) plus 17 archived rows (row 7 to row 379) in `docs/plans/roadmap-archive-2026-11.md` — 566 rows across the two files.
- Attribution evidence: 262 of the live rows and 13 of the archived rows name the PRD or Automaton branch that did the work (matches on `PRD` or `automaton/` in the row text) — 275 rows with a concrete Automaton or PRD to link to.
- Status shape (raw keyword mentions in `docs/plans/README.md`, rows overlap, so this reads as shape not as a partition): shipped 412, done 219, draft 198, blocked 80, waiting 24, cancelled 103, planned 16, in progress 12, deferred 7.
- Findings ledgers: 13 files matching `*findings*.md` under `docs/plans` (for example `docs/plans/restart-matrix-findings.md`, `docs/plans/bl462-restart-audit-findings.md`, `docs/plans/silent-first-tool-call-findings.md`, and the newest, `docs/plans/2026-11-16-promotion-findings.md`), carrying 87 level-2 sections, which is the shape the import reads one finding per. The newest ledger already says on its own header that the tracking moved to the daemon (Automata grouped under one Automation) and that the eleven records it created are to be converted by the registry-retirement work this proposal describes.
- Parity issues: 178 markdown files under `docs/plans/parity-issues` (including its `archive` subfolders).
- Supporting files: 109 reports under `docs/plans/reports`, 260 archived design docs under `docs/plans/historical-plans`, 182 past release notes under `docs/plans/historical-releasenotes`; 950 markdown files under `docs/plans` in all.
- Automata: the design doc recorded about 600 Automaton records with about 540 archived (2026-11-10, from the daemon's own data). The finish-automation board generated 2026-11-16 from the daemon's own data (`scripts/automation-board.py`, rule: regenerated at each full tick) lists 46 finished Automata whose commits never reached main, 10 failed or blocked with work left, and 18 waiting for review, approval or planning — 74 open work items the registry is not even supposed to track on its own anymore.
- Operator decisions: quoted operator direction sits inside roughly 40 of the top-level design and handover plans (files that carry a quoted operator line), for example `docs/plans/2026-11-10-plans-and-backlog-in-datawatch.md`, `docs/plans/2026-11-16-promotion-findings.md` and the supervisor rules in `docs/plans/2026-10-17-supervisor-rules-and-lessons.md`.

## 3. Conversion of what is not just a plan, backlog item, finding or status

The import already ran (step 1, shipped): `internal/planrecords/import.go` reads the registry rows and the findings files into records, is idempotent, and keeps `legacy_number` so row numbers survive. The retirement conversion is the second direction: every surviving record kind's content is *moved into* the constructs it was mirroring, not deleted.

| Content | Where it goes | Evidence |
|---|---|---|
| Registry rows that name a spec or a design doc | the Automaton (or a new draft Automation/Automaton) under a theme in a plan; the row becomes the Automaton's description and links | 275 rows with PRD/Automaton attribution (section 2); `scripts/plans-migrate.py` already "lists the plans the plans migration made from registry rows" per [howto/plan-records](../howto/plan-records.md) |
| Registry rows that are shipped status | the completed Automaton with its release and commit; `HISTORY.md` keeps the prose | the `registry_row` write-through hook `internal/mergepipeline/posthook_plan_record.go` already records "the shipped row as a plan record" with `ShippedBy`, `ShippedVersion`; rows 412 mention shipped |
| Findings ledgers | finding records tagged `kind:bug`/`kind:feature`, attached to the Automation that groups the theme — the promotion-findings file already shows the target shape | `docs/plans/2026-11-16-promotion-findings.md` header rule 2: "File the Automaton the same tick, after the overlap check"; 87 sections across 13 files |
| Parity issue drafts (178 files) | they stay documents (rules: design docs and handover plans stay files); the *status* of a parity item moves to a finding or Automaton on the project | `docs/plans/parity-issues/post-ship-hooks.md` and the 178 files under `docs/plans/parity-issues` |
| Decisions (quoted direction) | the decision record on the Automaton (`internal/autonomous/decision_record.go`); the quote stays in the doc where it was heard | 40 design/handover files carrying quoted operator direction (section 2) |
| Release grouping | rows and findings that are part of a release attach to that release plan (label `10.0` or `11.0` default); unattributed work gets a plan item | `internal/planrecords/release_migrate.go`, `internal/planrecords/themes.go`; definitions entry "release plan" in `docs/datawatch-definitions.md` |

The record store itself stays: plans, release plans, themes, links and search (`internal/planrecords/`) are the substrate. What retires are the two kinds that exist only because a file had to be mirrored: the registry row and the finding-as-file-mirror, replaced by plan items, Automata and findings-on-Automata.

## 4. `README.md` as a configurable export

- Shape: `docs/plans/README.md` remains the human-readable roadmap, but from then on it is *generated* from the records — one pass that lists the open Automata and plan items of the project, grouped by theme, with the shipped rows summarized in `HISTORY.md` as the file's own rules already want. Nothing hand-edits it.
- The setting is off by default, matching the operator's "we should have an option to be able to have them export to a repo" (quoted in `docs/plans/2026-11-10-plans-and-backlog-in-datawatch.md`, step 5: "Optional export to a repository, off by default"). The existing write-through switch `autonomous.plan_records.write_through` (defined in `internal/config/config.go`, and which the supervisor rules say to leave off) is the precedent for how this kind of setting ships and stays off.
- One direction only: the export pushes records out; a change to the file is never read back as truth. The file's link to `docs/plans/roadmap-archive-2026-11.md` and the tidy scripts' archive step become part of the export layout or die with the scripts (section 5).
- The export naming: it lists Automata and plan items, not rows; row numbers stay available as `legacy_number` on records and links keep working through `internal/planrecords/links.go`.

## 5. Retirement steps: tidy scripts, registry checks, the post-ship hook, the supervisor tick

Every place that reads or writes the file registry today, and what replaces it. Nothing in this section happens until the Conditions in section 7 hold.

| Surface | What it does with the file registry today | After retirement |
|---|---|---|
| Tidy scripts: `scripts/tidy-plans.sh`, `scripts/plans-tidy-daily.sh`, `scripts/plans-tidy-automation.json`, `scripts/plans_registry_hygiene.py`, `scripts/plans_monthly_archive.py` | move closed rows to `roadmap-archive-*.md`, run registry hygiene over `docs/plans/README.md` | retired; grouping happens as Automata complete and themes close. The design doc already lists `5b` (clean up the repository) and step 6 (retire the tidy scripts) as the later steps the operator decides |
| Link and report gates: `scripts/check-plans-links.sh`, `scripts/check-plans-links.py`, `scripts/plans_links.py`, `scripts/check-plan-registry-reports.sh` | keep the registry's row links to docs consistent | retired for registry rows; the link rule moves to the export (links generated from records) and to the normal docs link checks |
| The registry-versus-README check, one of the four automatic Automata checks (`autonomous.checks.*`) | parses the table in `docs/plans/README.md` against the docs under `docs/plans/`; report-only | `internal/autonomous/registry_readme_check.go` retires; the equivalent question ("is this shipped work recorded?") is answered by reading the Automata list and the ship records |
| The after-done checklist item "registry row for this Automaton" | `internal/autonomous/after_done_checklist.go` item 5 fails when the README has no matching row | the item retires; completion is the Automaton's own state plus its ship record |
| Overlap warnings that read registry rows | `internal/autonomous/overlap_sources.go` lists "registry rows from `docs/plans/README.md`" as a candidate source, kind `registry_row` | the source list becomes Automata, unmerged branches and plan items; the operator's 2026-11-14 quote is exactly this: the construct already exists, the file read goes |
| The post-ship write-through hook | `internal/mergepipeline/posthook_plan_record.go` upserts the shipped registry row as a record when the write-through setting is on | the hook retires in favor of the ship record on the Automaton (what `scripts/merge-shipped-record.sh` already does for merged branches); the `autonomous.plan_records.write_through` setting retires |
| The supervisor tick | the supervisor rules (check 7, "Registry audit is mandatory every tick") re-audit `docs/plans/README.md` each hourly tick; the finish-automation board is "regenerated at each full tick and any change is committed with the registry row" (`docs/plans/2026-11-16-finish-automation-board.md`); rules say "Do not add rows to `docs/plans/README.md`, do not create registry rows or findings" (`docs/plans/2026-10-17-supervisor-rules-and-lessons.md`) | the tick reads Automata and Automations for status (the rules already say so), the board keeps being generated from the daemon's own data, and the "registry audit" step is replaced by the overlap check plus the board's three sections |
| The Records sub-tab of the Projects page | `internal/server/web/app.js` renders the registry rows and findings of a project; [howto/plan-records](../howto/plan-records.md) and the definitions entries "Registry", "Registry row record", "Registry row link" describe it; REST `GET /api/plan-records/registry-rows` and `/api/plan-records/findings` (`internal/server/plan_records.go`); MCP plan-record tools (`internal/mcp/plan_record_tools.go`); CLI (`cmd/datawatch/cli_plan_records.go`); the comm verb router (`internal/router/plan_records_comm.go`) | the sub-tab shows plans and Automata instead; the registry-row and finding endpoints and tools retire in the same release as their kinds. This is the open design question in section 6, not decided here |

Sequencing, all operator-decided dates (matching the design doc's step numbers): (1) conversion dry-run reviewed (section 3); (2) export switch exists and is off (section 4); (3) stop adding new rows and findings files — the supervisor rules already direct this; (4) retire the two record kinds, the hook, the check and the scripts together, in one release; (5) the file registry folder becomes read-only archive; (6) repository cleanup (design doc step `5b`) only after the operator says when.

## 6. The Projects page question and other open design questions

This is the operator's decision to make, and it is the reason the document is a proposal:

- **The Projects tab is showing the backlog, not the plans** (quoted above). Once the registry and finding kinds retire, the sub-tab that listed them has two honest options, and only the operator can pick: show the project's **plans** (with their release plans and themes, the Automata under each), or show the **backlog** (the open Automata and drafted Automations grouped by Automation theme). Both reads already exist over REST/MCP/CLI; the question is what the PWA page leads with, and whether the two get separate tabs or one. No task may decide this.
- Where in the PWA a finding on an Automaton is shown (list, detail, search results) — the same class of placement question the promotion-findings file lists as "open questions for the operator".
- The export's layout: one file (today's `README.md`) or the file plus `HISTORY.md` summaries; table vs list; when the export pushes (at ship, on a schedule, on demand).
- Which release plan takes the unshipped rows: the `10.0` or the `11.0` label. The definitions' entry for release plans makes clear the labels are operator defaults, so the choice belongs to the operator.

## 7. Conditions before an Automaton drafts

No draft Automation for this work is filed until every condition below holds, and the open questions above are answered one at a time by the operator:

1. The operator confirms the three quoted directions and this proposal (`Confirmed:` flips from `no`).
2. A conversion dry run lists every registry row and finding section with its destination (Automaton, plan item, finding on an Automaton, decision record, or prose that stays), and shows no orphan; the import stays idempotent (`internal/planrecords/import.go`) so re-running never duplicates.
3. The export setting exists and is off, the generated `README.md` matches the live records, and link behavior is proven by the export's own generation rather than by `scripts/check-plans-links.sh`.
4. The tick, the overlap check, the after-done checklist and the board all read the records and the Automata, with the file reads from section 5 removed in the same release.
5. The PWA, the definitions, the how-tos (`docs/howto/plan-records.md`, `docs/howto/plans-folder-tidy.md`) and the REST/MCP/CLI/chat surfaces are updated in the same release as the kinds retire — no half-open surfaces.
6. The tidy, hygiene and archive scripts are retired only after the operator approves the repository cleanup step, and the `roadmap-archive` files and `historical-plans` stay as the read-only archive.

## 8. Open questions

1. Projects page: plans or backlog first on the Projects tab, and one tab or two? (Section 6; the operator's 2026-11-16 quote is the question itself.)
2. Export: one file or file plus summaries, table or list, and when does it push?
3. `10.0` or `11.0`: which release plan receives the unshipped rows and findings?
4. Findings-on-Automata placement in the PWA (list, detail, search)?
5. When exactly do new rows and new findings files stop being written — the tick already reads the rules in the supervisor doc; the hard stop follows the operator's go.
6. Parity issue status: does the status of a parity draft move to a finding on the project, or stay in the parity file's own header?
7. Scope of the cleanup: does repository step `5b` cover `docs/plans/reports`, `parity-issues`, `historical-releasenotes` as well, or only the registry and ledgers?
