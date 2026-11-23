# Retire the file registry: plans, backlog and status as datawatch records

- Date: 2026-11-16
Confirmed: yes

Confirmed by the operator on 2026-11-17, answer `Confirm` to question `q-18e8d6b7c20b6a7f2d85` (stored in datawatch). The three open questions below are still open for the operator; step 3 (repository cleanup) waits until the operator says when.

This is a proposal only. Nothing below is decided except what is quoted; no implementation Automaton is drafted until the line above reads `Confirmed: yes`. No code, setting, document or file changes with it — only this file, plus the embedded copy `make sync-docs` produces from it. Scope: propose retiring the file-based plans registry (`docs/plans/README.md`) and the findings ledgers as the primary sources of truth, in favor of datawatch's own constructs — the project, plans (including release plans and themes), Automations and Automata — with `README.md` kept as a configurable, off-by-default export, and the file system kept as a read-only archive.

## Operator direction (quoted verbatim, the source of this proposal)

- 2026-11-14: "anything waiting on me should have a detailed question --- isn't a backlog just an automation that isn't built out yet? i was clear in the designs earlier an export of this which would be the readme and for size/content the historical history path with older stuff and our normal release / archiving cadent in the main README we can also have ability (configurable) to export automations/plans to document in readme since many teams will track status in github -- we will have to clean up the docs/plans folder(s) and make sure we have it all in datawatch when it migrates but the individual registry is not needed in datawatch it has other constructs like i and you said that would fit. this will also allow datawatch better control since it knows what is needed, bugs, backlog, future, feature requests (gh submitted stuff), etc"
- 2026-11-16: "We're supposed to be using the actual constructs of our project plan Automations and automata we don't have the registry that was supposed to be a file system construct. When is that being removed? I thought we had filed that like yesterday or day before."
- 2026-11-16: "What about historical, want everything from 9.0"

The design that this proposal completes is [plans, backlog and status inside datawatch](historical-plans/2026-11-10-plans-and-backlog-in-datawatch.md), which quotes the 2026-11-10 direction ("we should have an option to be able to have them export to a repo so people can keep track of the status if we want") and whose step 5 already reads "Optional export to a repository, off by default". Its step 3 (the one-time import of the registry rows and findings into records) has been built: `internal/planrecords/import.go` reads the registry rows and the findings files into records and is idempotent. This proposal is the later part the directions above point at: stop looking at the file registry, and retire the two record kinds that only mirror a file.

## 1. Target model

After retirement, every piece of information the file registry and the ledgers carry lives in a first-class construct of the project:

| Today's file thing | Becomes |
|---|---|
| A backlog item with no spec yet ("file it now, draft when it gets real") | a **draft Automation** not built out yet, or a draft Automaton inside a draft Automation under the plan that groups the theme — never a table row |
| A planned work item with a spec (a registry row linking a design doc) | an **Automaton** (draft, running, or completed), grouped by theme in an **Automation**, under a **plan** |
| A bug, a future item, or a feature request (including one submitted through GitHub) | **the same construct — an Automaton drafted under its Automation — with a tag**: `kind:bug`, or the future / feature-request tag. No separate record kind for bugs, future work or requests |
| A finding (a defect, gap or missing doc with evidence) | a finding on an Automaton the tick it is found, after the overlap check; the 2026-11-14 direction is exactly this: datawatch "knows what is needed, bugs, backlog, future, feature requests (gh submitted stuff)" |
| A group of related rows (a theme, "eleven of them") | an **Automation** with a theme inside a **plan** |
| A release worth of work | a **release plan** (plan kind `release`, label `10.0` or `11.0` by default) whose Automata and Automations are grouped by theme — see `internal/planrecords/release_migrate.go` and the "release plan" entry in `docs/datawatch-definitions.md` |
| A shipped row | the completed Automaton with its ship record (release and commit), read from the Automata list and the shipped records |
| A decision (operator direction, quoted) | the **decision record** on the Automaton (`internal/autonomous/decision_record_store.go` and `internal/autonomous/decision_record.go`), with the quote kept in the document where it was heard |
| Roadmap prose a human reads | an **export** of the records into `docs/plans/README.md`, controlled by a setting, off by default |

What stays a file: design documents, handover plans and history. They remain the readable home for prose; they are no longer status sources. Status and decisions live in the records and on the Automata.

## 2. History from 9.0: what we inherit, by kind

The 2026-11-16 direction sets the horizon: "What about historical, want everything from 9.0" — every shipped release since the 9.0 line is in scope. Counts were taken from this tree on 2026-11-16 and each names the file it was counted in.

| Kind | Count and where | Held by (construct) | Stays as a file? |
|---|---|---|---|
| Shipped releases since 9.0 | 638 release entries in `CHANGELOG.md` (the `v9.*` headers); 417 before the 9.0 line | the **release record** on the shipped Automaton (`ShippedBy` is the Automaton id, `ShippedVersion` the release — `internal/mergepipeline/posthook_plan_record.go`), recorded by `datawatch autonomous shipped record` and the `completed_mark` helper step in `scripts/merge-shipped-record.sh`; `HISTORY.md` is already pointed to for the prose, per the registry's own header lines | `CHANGELOG.md` and `HISTORY.md` stay as the file history, exactly as they are |
| Registry rows | 549 numbered rows in `docs/plans/README.md` (row numbers run to 560) + 17 closed rows in `docs/plans/roadmap-archive-2026-11.md` = 566 | 279 of the live rows and 15 of the archived rows name the PRD or Automaton that did the work (a `PRD` or `automaton/` match in the row text) — **294 rows with a concrete Automaton to link to**; the rest become plan items or prose. Row numbers survive as `legacy_number` (`internal/planrecords/import.go`, `internal/planrecords/doc.go`) | the two files stay, as the read-only archive |
| Findings | 13 files matching `*findings*.md` under `docs/plans` (for example `docs/plans/restart-matrix-findings.md`, `docs/plans/silent-first-tool-call-findings.md`, `docs/plans/2026-11-16-promotion-findings.md`), carrying 90 level-2 sections | a **finding on an Automaton** tagged `kind:bug` or `kind:feature`, attached to the Automation that groups the theme — the newest ledger already prescribes that shape ("File the Automaton the same tick, after the overlap check", `docs/plans/2026-11-16-promotion-findings.md`) | the ledgers stay as the file archive |
| Parity issues | 178 markdown files under `docs/plans/parity-issues` (including its `archive` subfolders) | the *status* of a parity item moves to a finding or Automaton; the draft documents themselves follow the rule that design docs and handover plans stay files | yes, the files stay |
| Shipped-and-finished Automata, running from 9.0 | the design doc recorded about 600 Automaton records with about 540 archived (from the daemon's own data, 2026-11-10); the finish-automation board generated 2026-11-16 from the daemon's own data (`scripts/automation-board.py`) lists 46 finished Automata whose commits never reached main, 10 failed or blocked with work left, and 18 waiting for review, approval or planning — `docs/plans/historical-plans/2026-11-16-finish-automation-board.md` | the **archived or shipped Automaton** itself, read from the Automata list (no file read needed) | the board file stays as a generated snapshot |
| Operator decisions quoted in design and handover documents | 60 top-level `docs/plans` markdown files carry a line that attributes a quoted line to the operator (files with a line naming the operator together with a quoted span of at least 10 characters), for example `docs/plans/2026-11-10-plans-and-backlog-in-datawatch.md`, `docs/plans/2026-10-17-supervisor-rules-and-lessons.md`, `docs/plans/2026-11-16-promotion-findings.md` | the **decision record** on the Automaton that carries the decision; the quoted line stays in the document where it was heard | yes, the documents stay |
| Reports | 109 markdown files under `docs/plans/reports` | read-only context for the Automata they describe; no record kind of their own | yes, they stay |
| Plan and design documents | 260 archived design docs in `docs/plans/historical-plans`, 182 past release notes in `docs/plans/historical-releasenotes`; 950 markdown files in `docs/plans` in all, excluding this proposal | **plans and themes** hold the grouping (`internal/planrecords/themes.go`, `internal/planrecords/release_migrate.go`); the design prose stays in the documents | yes, they stay |

Shape of the status vocabulary in the live registry (rows mentioning each keyword, case-insensitive; rows overlap, so this is shape, not a partition — the same rows the conversion reads): shipped 407, done 218, draft 218, blocked 81, waiting 36, cancelled 106, planned 16, in progress 11, deferred 3.

**How the result is checked against the files so nothing is dropped:** the import is idempotent on (project, kind, `legacy_number`), so a re-run never duplicates (`internal/planrecords/import.go`); the conversion dry run lists every one of the 566 rows and all 90 finding sections with its destination (Automaton, plan item, finding on an Automaton, decision record, or prose that stays) and shows no orphan; the 294 attributed rows are checked against the Automata list by id; and the 950 `docs/plans` files are untouched because nothing in this proposal deletes file content — the files become read-only, which is the strongest possible guarantee that nothing is dropped.

## 3. Conversion of every existing record, by count

The import already ran (shipped): `internal/planrecords/import.go` reads the registry rows and findings into records, idempotently, keeping `legacy_number`. The retirement conversion is the other direction: the content of the two record kinds is *moved into* the constructs they mirror, then the two kinds retire.

| Content (count) | Where it goes | Evidence |
|---|---|---|
| Open/draft/blocked rows that name a spec, plan or Automaton (294 attributed rows, 279 of them live) | the named Automaton (or a draft Automation or Automaton under the plan that groups the theme); the row text becomes the description and links | `internal/planrecords/import.go` (idempotent on `legacy_number`); the per-row attribution matches in `docs/plans/README.md` (279 rows) and `docs/plans/roadmap-archive-2026-11.md` (15 rows) |
| Rows that are shipped status (407 rows mention shipped) | the completed Automaton with its ship record: release and commit; `HISTORY.md` keeps the prose, as the registry header already points to it | `ShippedBy`/`ShippedVersion` in `internal/mergepipeline/posthook_plan_record.go`; `datawatch autonomous shipped record` + `completed_mark` in `scripts/merge-shipped-record.sh` |
| Shipped/done rows with no Automaton (the remainder of the 407/218 mentions) | a **plan item** under the plan for the period, or a completed Automaton recovered from the branch — the finish-automation board already surfaces the 46 finished-work cases whose commits never reached main, so they are not silently lost | `docs/plans/historical-plans/2026-11-16-finish-automation-board.md` section A |
| Findings sections (90 across 13 files) | findings on Automata tagged `kind:bug` or `kind:feature`, attached to the Automation that groups the theme — the target shape the newest ledger already prescribes | `docs/plans/2026-11-16-promotion-findings.md` rule 2; the 90-section count over the 13 ledger files |
| Parity issue drafts (178 files) | the status line of each moves to a finding or Automaton; the documents stay files | the 178 files under `docs/plans/parity-issues` (including `docs/plans/parity-issues/post-ship-hooks.md`) |
| Quoted decisions (60 documents carry them) | decision records on the Automata that carry the decision; the quotes stay in the documents | `internal/autonomous/decision_record_store.go`, `internal/autonomous/decision_record.go` |
| Release grouping | shipped rows and findings attach to the release plan they belong to (label `10.0` or `11.0` by default); unattributed work gets a plan item | `internal/planrecords/release_migrate.go`, `internal/planrecords/themes.go`, `internal/planrecords/release_rowplans.go` |

**What is lost, if anything:** nothing. Every converted row keeps its number as `legacy_number` so "row 26" keeps resolving (`internal/planrecords/doc.go`), links keep resolving through `internal/planrecords/links.go`, and no file is deleted — the registry, the ledgers and `HISTORY.md` remain as read-only history. The only thing that stops existing is the *dual source of truth*: the row and the ledger stop being places the operator looks for status.

## 4. `README.md` as a configurable export of plans and Automations

- **What it lists:** the open Automata and plan items of the project, grouped by theme under their plans (release plans `10.0`/`11.0` and other plans), with a pointer to `HISTORY.md` for shipped work — today's header already says shipped work is summarized in `HISTORY.md`, so the export only tightens what the file already promises.
- **How large:** no larger than the live set of open items grouped by theme. Older items leave by the normal release and archiving cadence: when work ships, it rolls into `HISTORY.md`'s summaries and out of the export the way closed rows already leave the live table into `docs/plans/roadmap-archive-2026-11.md` — same cadence, different source. That is the 2026-11-14 direction: "the historical history path with older stuff and our normal release / archiving cadent in the main README".
- **Configurable:** per the 2026-11-14 direction ("we can also have ability (configurable) to export automations/plans to document in readme") and the design doc's step 5 ("Optional export to a repository, off by default"). Precedent for how this kind of switch ships and stays off: `autonomous.plan_records.write_through` (`internal/config/config.go`), which the supervisor rules keep off.
- **One direction only:** the daemon is the source of truth. The export pushes records out to the file; an edit to the file is never read back as truth. Link behavior comes from the export's own generation (`internal/planrecords/links.go`), not from a separate link check over a hand-maintained table.

## 5. Retirement steps, in order

Nothing in this section happens until the conditions in section 7 hold.

1. **Stop the post-ship hook writing registry rows or records.** `internal/mergepipeline/posthook_plan_record.go` (upserts the shipped registry row as a record, gated on `autonomous.plan_records.write_through`) stops writing; the shipped state keeps being recorded on the Automaton the same way `scripts/merge-shipped-record.sh` already does (`ShippedBy`, `ShippedVersion`). Reversible — the setting already exists to flip it off.
2. **Retire the registry and finding record kinds from every surface, keeping stored data readable as an archive.** REST `GET /api/plan-records/registry-rows` and `/api/plan-records/findings` (`internal/server/plan_records.go`) remain readable for stored rows; the write paths, and the PWA Records sub-tab's row/finding rendering (`internal/server/web/app.js`), the MCP plan-record tools (`internal/mcp/plan_record_tools.go`), the CLI (`cmd/datawatch/cli_plan_records.go`) and the comm verb (`internal/router/plan_records_comm.go`) move to plans, Automata and findings-on-Automata. The definitions entries and how-tos (`docs/howto/plan-records.md`, `docs/howto/plans-folder-tidy.md`) update in the same release. The registry-versus-README check (`internal/autonomous/registry_readme_check.go`, report-only) retires with the table it parses: the question it asked ("is this shipped work recorded?") is answered by reading the Automata list and ship records. The after-done checklist's fifth item, "registry row for this Automaton" (`internal/autonomous/after_done_checklist.go`), retires; completion is the Automaton's own state plus its ship record.
3. **Clean `docs/plans` only after everything is in datawatch.** With the dry run from section 2 showing no orphan, the registry and ledgers become read-only archive and `docs/plans` is cleaned the way the 2026-11-14 direction wants ("we will have to clean up the docs/plans folder(s) and make sure we have it all in datawatch when it migrates"). Design documents, handover plans, `CHANGELOG.md`, `HISTORY.md` and the archive files stay.
4. **Retire the tidy scripts and registry checks.** `scripts/tidy-plans.sh`, `scripts/plans-tidy-daily.sh`, `scripts/plans-tidy-automation.json`, `scripts/plans_registry_hygiene.py`, `scripts/plans_monthly_archive.py`, and the link checks over the hand-maintained table (`scripts/check-plans-links.sh`, `scripts/check-plans-links.py`, `scripts/plans_links.py`, `scripts/check-plan-registry-reports.sh`) — grouping happens as Automata complete and themes close, and links come from the export. The overlap check stops reading registry rows (`internal/autonomous/overlap_sources.go` lists "registry rows from docs/plans/README.md" as a candidate source) and reads Automata, unmerged branches and plan items.
5. **Change the supervisor tick and merge procedure to use Automata and Automations only.** The supervisor rules already direct the read side — "Findings and backlog are Automata and Automations, not files and not registry rows", with the 2026-11-14 and 2026-11-16 directions quoted there — and already say "Do not add rows to docs/plans/README.md, do not create plan-record registry rows or findings". What changes is the write and audit side: the mandatory "Registry audit" check (rule 7, `docs/plans/2026-10-17-supervisor-rules-and-lessons.md`) becomes the overlap check plus the finish-automation board ("The board is regenerated at each hourly tick and any change is committed with the registry row", `docs/plans/historical-plans/2026-11-16-finish-automation-board.md`), and the tick reports on Automata and Automations instead of rows.

The order matters only one way: step 3 (repository cleanup) comes last, and only after the operator says when — everything before it keeps the files exactly as they are.

## 6. A backlog Automation on the Projects page (open question for the operator)

Written as an open question, per the 2026-11-14 direction that "isn't a backlog just an automation that isn't built out yet?" — not decided by this proposal. The Projects page's Records sub-tab today renders the registry rows and findings of a project; once those two kinds retire, the sub-tab has nothing left to render from them, and the project's backlog — draft Automations and Automata not built out yet, including bugs, future items and GitHub-submitted feature requests — needs a place. Options, trade-offs and a Recommendation are in the Open questions (Q1). A task may not decide this.

## 7. Conditions that must hold before each step runs

A draft Automation is not filed until every condition holds and the open questions are answered one at a time by the operator:

1. The operator confirms the three quoted directions and this proposal — the `Confirmed:` line flips from `no` to `yes`.
2. The conversion dry run lists all 566 rows and all 90 finding sections with their destination (Automaton, plan item, finding on an Automaton, decision record, or prose that stays) and shows no orphan; the import stays idempotent, so re-running never duplicates.
3. The export setting exists and is **off**, the generated `README.md` matches the live records, and links resolve from the export's own generation.
4. The tick, the overlap check, the after-done checklist and the board all read Automata and Automations; the file reads of section 5 step 4 are removed in the same release as the kinds retire — no half-open surface over REST, MCP, CLI, chat/comm, PWA, settings, definitions and how-tos.
5. The tidy, hygiene, archive and link scripts retire only after the operator approves the repository-cleanup step (section 5, step 3), with `roadmap-archive-2026-11.md`, `historical-plans`, `historical-releasenotes`, `CHANGELOG.md` and `HISTORY.md` left as the read-only archive.

**What can be switched on first with no data loss:** only the export, and off. It reads the records and writes a generated file; it changes nothing stored, deletes nothing and never reads the file back. Stopping writes (section 5, step 1) is the second, also lossless — the data already lives as records and on the Automata, and the switch that gates it already exists. Nothing that deletes, retires or cleans runs before conditions 2 and 3 both hold.

## Open questions

Each is open for the operator, with options, trade-offs and a Recommendation.

**Q1 — The Projects page (question 6): how a backlog Automation is shown.** The direction asks whether "a backlog [is] just an automation that isn't built out yet?" — the Projects page should show exactly that: the project's draft Automations and draft Automata, including bugs, future items and feature requests (GitHub-submitted ones among them), as the backlog view.

- Options: (a) the Records sub-tab leads with the **backlog** — draft Automations not built out yet, grouped by Automation theme, with a plans tab alongside; (b) it leads with the **plans** — the plans, release plans and themes, with the backlog visible under each plan; (c) one combined tab, plans and backlog interleaved.
- Trade-offs: (a) answers the direction most directly and makes filing "draft now, build out later" visible at a glance, but reads as a queue without the grouping that explains why items exist; (b) keeps the grouping and the release context (labels `10.0`/`11.0`) front and center, but buries "what is waiting to be built" one level down; (c) is simplest to read, but mixes two record shapes and makes "draft Automation" and "plan" hard to tell apart at a glance.
- Recommendation: (a) — lead with the backlog as draft Automations grouped by theme, because the 2026-11-14 direction defines the backlog as draft Automations, with the plans available one tap away. The daemon is the source of truth for both, so nothing is lost either way; the operator decides.

**Q2 — The export: what file, what shape, when it pushes.**

- Options: (a) one generated `docs/plans/README.md`, table-shaped as today; (b) the same file plus `HISTORY.md` summaries in the same pass; (c) a directory export (roadmap plus per-plan files) instead of one file.
- Trade-offs: (a) is the smallest change and matches "the readme" in the direction, but ships and backlog share one file; (b) keeps the size down by the "normal release / archiving cadent" the direction names, at the cost of a second generated file; (c) is cleanest per plan, but is more than one file — the direction says "the readme", singular.
- Recommendation: (b) — one generated `README.md` for the live backlog/plans plus the existing `HISTORY.md` path for shipped work, regenerated when a ship or a theme change happens and on schedule, because it is the layout the 2026-11-14 direction already describes. The operator decides.

**Q3 — Which release plan takes the unshipped rows: `10.0` or `11.0`.**

- Options: (a) all pre-release work attaches to the `10.0` release plan; (b) split by theme — engine-trust buildout to `10.0` (as `docs/plans/README.md` itself frames it: the v10-era release is "the point where the remaining backlog is built by datawatch's own Automata engine"), everything else to `11.0`; (c) no release plan yet; work stays under theme plans until the operator says which release it targets.
- Trade-offs: (a) is the simplest and is what the registry's own framing already does; (b) is more honest about the two horizons, but the split lines are a judgment the operator has to draw; (c) loses nothing and defers the decision, but leaves the work ungrouped by release on screen.
- Recommendation: (a) with a theme review — attach everything to `10.0` now, and move themes to `11.0` when the operator says so. The release-plan labels are operator defaults (definitions entry "release plan"), nothing is forced. The operator decides.

**Q4 — Where findings on Automata are read in the PWA.**

- Options: (a) on the Automaton detail, as today's ledger sections become the Automaton's finding list; (b) also in the project search results; (c) in both, plus a findings filter on the backlog tab.
- Trade-offs: (a) is the smallest surface and matches "file it on the Automaton the tick it is found"; (b) and (c) make findings discoverable from the outside, at the cost of more PWA surface, which is exactly the surface the design wants to shrink.
- Recommendation: (a) with (b) — findings live on the Automaton and are findable in project search. A dedicated surface would recreate the ledger as a page, against the direction. The operator decides.

**Q5 — When new rows and findings files stop being written.**

- Options: (a) from the date the tick rule changes (section 5, step 5) — the supervisor rules already direct "Do not add rows to docs/plans/README.md, do not create plan-record registry rows or findings"; (b) from the date the record kinds retire (step 2); (c) already, right now, with existing rows kept read-only.
- Trade-offs: (c) is earliest and loses nothing (the write paths are off-by-settings today), but leaves the two files as live archives; (a) and (b) tie the stop to the release, which is safer for the audit trail but later.
- Recommendation: (c) — the write side is already directed off by the supervisor rules and the `write_through` setting already defaults off, so new rows and findings files stop being written now, with the existing files kept exactly as they are. The operator confirms.

**Q6 — Question 6 of the numbered requirements: what counts as a "backlog item".** The 2026-11-14 direction defines it — "isn't a backlog just an automation that isn't built out yet?" — so this is a question to confirm the reading, not to re-decide.

- Options: (a) a backlog item is a draft Automation not built out yet, bug/future/feature-request included, tagged accordingly; (b) a backlog item is any open plan item without an Automaton, and the draft Automation is a separate shape.
- Trade-offs: (a) is one construct and one list to look at, matching "datawatch better control since it knows what is needed, bugs, backlog, future, feature requests"; (b) keeps two lists, which is the registry shape being retired.
- Recommendation: (a) — one construct, tagged, in the same backlog view as Q1. The operator confirms.

**Q7 — How far the `docs/plans` cleanup reaches.**

- Options: (a) registry and ledgers only — the registry, the 13 findings files and the link checks over the table retire, everything else stays; (b) also consolidate `docs/plans/reports` (109 files), `docs/plans/parity-issues` (178 files) and `docs/plans/historical-releasenotes` (182 files) into their Automata and plans, as archives only; (c) a wider cleanup of all 950 files once the operator inventories by theme.
- Trade-offs: (a) is bounded and reversible; (b) is cleaner but touches files that are still cited by live work; (c) is thorough but unbounded until the inventory passes.
- Recommendation: (a) first, then (b) theme by theme after the operator approves the theme inventory. Nothing in the file system is deleted at any step — "clean up" becomes "re-point and mark archive". The operator decides.
