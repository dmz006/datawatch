# Plans, backlog and status inside datawatch, exported to a repo (design proposal, open)

Status: proposal. Nothing here is decided except what is quoted. No Automaton is drafted until the open
questions below are answered, one at a time.

## Operator direction (quoted)

2026-11-10 (pasted text, as heard): "I was just thinking, we're at a point when we get the projects and everything
altogether that we shouldn't be keeping track of all the backlog, plans, and status directly on the file system
with files and all the trouble we're having making skills to coordinate them. In fact, they should all slowly
run inside DataWatch and we should have an option to be able to have them export to a repo so people can keep
track of the status if we want. But let's make a plan for how and when we can migrate directly to DataWatch and
stop trying to manage all these planned folders. And with this in mind, we should also stop focusing so much on
skill for the synchronization between the files. That should be just a background helping task until we get all
migration data watched, and then it's a push out to repo versus trying to keep it in the repo as the main
primary source. Let's not focus so much on the skills for those pieces."

Read as two decisions:
1. The long-term primary source for backlog, plans and status is datawatch itself. The repository gets an
   optional export (a push out), not the primary copy.
2. Skills and packs for synchronizing files between datawatch and the repository are a low-priority background
   helper until the migration is done. They are not where effort goes now.

2026-11-10, on question 1 (what the migration covers first): "I tiered approach starting with A, then getting to
all, then clean up repo, this will help with leaks a lot, we may need to consider how we manage products, that
may have one or more repos, that have projects, and they can inter relate time line, features, etc." (read as:
a tiered migration: registry rows and findings first (option A), then everything else, then clean up the
repository, which will help prevent leaks; and a product layer needs designing: a product can have one or more
repositories and projects, and products and projects can relate across timeline, features and so on.)

## What is on the file system today (counted 2026-11-10)

- `docs/plans/` holds about 154 entries at the top level, plus parity issue drafts, generated drafts and gate
  reports (about 186 files across those three folders).
- `docs/plans/README.md` is the registry: about 457 numbered rows, each a finding, a shipped item or a decision.
  It was already described by the operator as a temporary tracker until project planning exists.
- Helper scripts that keep these files tidy: `scripts/tidy-plans.sh`, `scripts/plans-tidy-daily.sh`,
  `scripts/plans_registry_hygiene.py`, `scripts/plans-tidy-automation.json`, `scripts/branch-audit.py` and
  `scripts/v10-status.py`.
- Supervisor rules and lessons, design proposals, decisions and handover notes are also files.

## What datawatch already holds

- Automata (about 600 records, including about 540 archived) with stories, tasks, decisions, change records,
  review state and generated artefacts, stored as one JSON file each under the data directory.
- The Projects tab shipped in v9.127.0 with the project, arc and Automaton model decided on 2026-11-06.
- Memory (SQLite), traces, and the decisions recorded on each Automaton.
- What I have not checked: whether datawatch has a record type for a backlog item or a finding (the registry
  rows), a decision log outside an Automaton, or a document store for design proposals. The defect intake
  tools exist, but I have not read what they store.

## Proposed shape (for the operator to confirm or change)

Everything the registry and the plans folder carry becomes a record in datawatch, linked to the project, Automation
and Automaton it concerns:
- **Backlog item or finding**: today a registry row.
- **Decision**: today a quoted line in a design doc.
- **Design proposal**: today a markdown file.
- **Status and release record**: today registry rows marked shipped, plus parity issue numbers.
- **Export**: a setting-controlled push of those records to a repository as readable files, one direction only.

## Sequencing (proposed, dates open)

| Step | Content | Target |
|---|---|---|
| 1 | Inventory what datawatch can already store and what is missing (backlog item, decision, document, parity link) | open |
| 2 | Add the missing record types and surfaces (REST, MCP, CLI, messaging, web) in the same release | open |
| 3 | One-time import of the registry and the live design docs into datawatch records, with the files kept read-only as the archive | open |
| 4 | Stop writing new registry rows and plan files; the supervisor and Automata write records instead | open |
| 5 | Optional export to a repository, off by default | open |
| 5b | Clean up the repository: remove the migrated plans, registry and drafts from the tree (this also removes the main source of internal-detail leaks into the public repo) | open |
| 6 | Retire the tidy scripts and registry checks once nothing writes the folders | open |

Until step 4 the files stay as they are. The supervisor keeps adding registry rows by hand, so nothing is lost
during the move.

## Deprioritised by the operator's second decision

- The plans folder auto-tidy Automaton (`29550001`) and the daily tidy Automation definition.
- A scheduler Automaton whose main purpose was running the tidy script.
- The registry-audit unit in the supervisor-as-skills design (it audits a file that is going away). The
  supervisor-as-skills design itself stays; only the file-sync parts drop.
- Any skill or pack whose job is to keep files and datawatch records in step.

None of these is cancelled. They wait, and the migration replaces their reason.

## Open questions (one at a time)

1. What does "migrate" cover first: the registry rows only, or also design proposals, handover plans and the
   supervisor lessons? Recommendation: registry rows and findings first, since they are what the supervisor
   writes every hour; documents second.
2. Where does the data live: in the existing per-record JSON files, or in the reporting database decided on
   2026-11-10 (files remain the source of truth for now)? This interacts with that decision.
3. Who writes the records: the supervisor through datawatch tools, Automata at their end, or both?
4. What does the export look like: which records, what file layout, and who may turn it on?
5. When is the cut-over (step 4)? The operator sets the date; I will list and recommend.
6. Does the registry row numbering continue, or do records get ids of their own?

## Tier order (operator, 2026-11-10)

Tier 1: registry rows and findings. Tier 2: everything else (design proposals, handover plans, supervisor lessons,
decisions, parity links). Tier 3: clean up the repository. The leak benefit is part of the reason for tier 3.

## New design area: products (operator, 2026-11-10)

The current model has projects, arcs (Automations) and Automata. I searched the Automata pages redesign doc and the
Automata code for a product concept and found none (the word appears only in "Product Requirements Document" in the
planning prompts). A product would sit above projects: one product with one or more repositories and one or more
projects, with timeline, feature and dependency relations between its projects. Open questions on this are raised
one at a time after the migration questions.

Operator answer on product question 1 (2026-11-10): "A that is exactly what i had in mind" (option A: a product is a
layer above projects; one product has one or more repositories and one or more projects; timeline, feature and
dependency relations between projects live at the product level; the model is designed now and built later, so the
migration imports records into it).

Next design questions on products (open, asked one at a time): what a product record holds, how a repository
attaches to it, how cross-project relations are expressed, and how the Projects tab and the repo export show a product.

Operator answer on migration question 2 (2026-11-10): "A" (the plans, backlog and status records live in the existing
per-record JSON files, which stay the source of truth, like Automata today; the derived SQLite reporting index decided
on 2026-11-10 is rebuildable from them; no new store and no database as source of truth yet).

## Encryption at rest (operator question, 2026-11-10)

Operator: "Are we also making sure if we have security enabled for encryption all these json files are data in sqllite
is encrypted?" (read as: when secure mode is on, are all these JSON files and the SQLite data encrypted?)

What I found (docs/encryption.md and the code, 2026-11-10):
- Secure mode (`--secure`, XChaCha20-Poly1305 with Argon2id) encrypts config, sessions, alerts, commands, filters,
  schedules, federation peers, skills index, ComputeNode and LLM registries, channel routing, discussion files,
  session output and the application log. The memory database encrypts only the `content` and `summary` fields;
  embeddings and metadata stay plaintext. `secrets.db` has its own key.
- The documented list does NOT include the Automata record files (`autonomous/prds/*.json`, about 640 MB), the trace
  files, decisions or change records, or the other SQLite databases (web search, council, docs index vectors).
- This installation is not running in secure mode (a sample of alerts, trace index and Automaton files starts with
  plain JSON), so I cannot say what a secure installation does with the Automata files beyond reading the docs and
  code. I did not test a secure start.

Consequence: moving plans, backlog and status into datawatch records puts planning content into files that secure mode
does not cover today. Encryption coverage becomes a requirement of the migration, not a nice-to-have.

Operator answer on migration question 3 (2026-11-10): "Q3, A" (both the supervisor and Automata write records through the
same datawatch tools: REST, MCP and the CLI; the supervisor writes findings and decisions, Automata write their own
shipped and status records at the end; every write lands in the audit log).

Operator answer on the encryption question (2026-11-10): "A" (the new plan, backlog and status records are encrypted
under secure mode from the first release; bringing the Automaton, trace and decision files into the same coverage is a
precondition for the cut-over; metadata that must stay readable for search or filtering stays plaintext and is named in
the docs). Step 3 of the sequence (import) and step 4 (cut-over) now depend on that coverage.

Operator answer on migration question 4, the repository export (2026-11-10): "5Off by default, but it should be useful
information, project/automation gantt time line, summary and tasks, human readable, but everything, maybe checkbox is
what is written but i want to make history of changes, current queue and status as things are worked ok, it's neat to
be a tracker by product/repo/project/ automation/automata/tasks. I don't want decisions but others may, i want status
and history" (read as option A with changes).
Decision: the export is off by default. It is a human-readable tracker, organised by product, repository, project,
Automation, Automaton and task. It carries: a Gantt timeline per project and Automation, a summary with the tasks as
checklists, the current queue and status, and a history of changes over time (the export itself builds that history).
It covers everything in those kinds, not a sample. Decisions are a record kind the operator can choose to include or
leave out; the operator does not want them in his own export, others may. Each record kind is selectable by setting.
Carried over from option A and not contradicted: one-way push, never read back. The leak filter (no deployment names,
tracker ids or secrets) stays on the export unless the operator says otherwise; it applies to the export of a
repository that others read.
