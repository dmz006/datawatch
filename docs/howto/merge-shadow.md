---
title: "How to: Merge shadow"
description: "What the three merge shadow settings enable, their defaults and the values they accept, every way they are set, where the shadow state is read (REST, MCP, CLI, comm channel, PWA card) and what the daemon never does"
last_updated: "2026-11-07"
topics:
  - merge
  - shadow
  - pipeline
  - release
  - ship
  - automata
  - autonomous
  - settings
  - api
  - mcp
  - cli
  - comm
  - pwa
---

# How to: Merge shadow

The merge shadow settings control the **finish-time shadow merge plan**: when the
supervisor runs an Automaton's release, the daemon writes a *plan* describing what the
merge would do (release size, version, changelog heading, README line, registry row,
conflicts, gates, parity-issue flag) with `applied` stuck at `false`, and keeps the
plan in the shadow store. The three keys live in one settings block, are on by
default, and can be set from YAML, the API, MCP, the command line, the comm channel
or the web UI. The plan, and (once they are stored) the comparisons, are read —
never acted on — from the REST route, the MCP tool, the CLI, the comm channel and
a PWA card; see "Where the shadow state is read" below.

## What the merge shadow does — and what it never does

**Does:**

- Write one shadow merge plan per finished Automaton at the supervisor's release:
  the release size (`patch`, `minor`, `major` or `none`), the version the bump step
  would set, the changelog heading and body drawn only from the Automaton's own
  changelog entry task (strict format, empty otherwise — nothing is invented), the
  README line, the registry row, and the parity-issue flag.
- Settle merge conflicts in a **throwaway trial worktree** outside the project
  tree, uncommitted (`--no-commit --no-ff`), then remove the scratch tree; the
  conflicts, each with its file class, resolution and resolver, are recorded in
  the plan. No branch, tag or ref is ever created or moved and nothing is pushed.
- Record each gate of the trial merge with its status and duration, and each
  finding with a code and whether it blocks.

**Never does:**

- Apply the release, bump the version, move main, create a tag, or push — the
  plan is a record, never a queue, and there is no second path back to the plan
  to apply it.
- Block, retry or fail the Automaton because a gate failed.
- Be a write surface for the operator: every read below is read-only, nothing
  is filed into the defect queue, and no gate, approval or automation consumes
  the comparison. Filing the recorded candidates into the defect queue is a
  separate job (see "What you still review" below).

## Where the shadow state is read

All five surfaces proxy one daemon endpoint and read — none of them writes:

| Surface | What it reads |
| --- | --- |
| **REST** | `GET /api/autonomous/merge/shadow` (read capability required; `405` with `Allow: GET` for any other method) → `projects[]`, each with `plans` (the stored shadow plans), `comparisons` (the stored shadow comparisons) and `counts` (per release size `patch`, `minor`, `major`, `none`: matched and different comparison counts). An empty `projects` list is the valid empty state, not an error. |
| **MCP** | `autonomous_merge_shadow_status` — read-only; the same answer, with an optional `project` argument to restrict it to one project. |
| **CLI** | `datawatch autonomous merge-shadow status` — matched and different counts per release size, or `no merge shadow comparison exists yet` while nothing is stored; `datawatch autonomous merge-shadow compare <automaton-id>` — that Automaton's comparison: matched, different, not-compared, then each difference with its field, severity, planned value and landed value (or `No merge shadow comparison exists for Automaton <id>`). |
| **Comm channel** | `autonomous merge-shadow` — one summary line: `Merge shadow: patch 4 matched, 1 different; minor …; major …; none …`; `No merge comparisons yet` while nothing is stored. It takes no arguments. |
| **PWA** | The **Merge shadow** card on the Automation **Setup and policy** sub-tab: matched and different per release size, summed over all projects, each row linking to this howto. Rendered only when the shadow is enabled **and** at least one comparison has been stored — otherwise the card is absent from the page, which is the expected state, not a failure. |

**Why the comparison reads can come back empty.** The post-ship flow does not
persist a comparison record yet: the hook persists the **defect candidates**, and
the `merge-comparisons/` store exists in the code with nothing in the production
flow writing to it (see "What is still yours to do" in
[merge-pipeline.md](merge-pipeline.md#what-is-still-yours-to-do)). Until that
wiring lands, every comparison read above returns nothing: the `comparisons`
lists and `counts` stay empty, the CLI answers "no comparison … exists", the
comm channel answers `No merge comparisons yet`, and the PWA card stays hidden.
The stored **plans** are already listed per project on every surface. The defect
**candidates** have no operator-facing read at all yet — they are the JSON file
under the data directory (`<data directory>/defect-candidates/<automaton id>.json`).

## The three settings

All three keys live under `autonomous.merge_shadow` and are **on by default** —
the block behaves as if the defaults were written. An unset block and a block set
to its defaults are indistinguishable.

| Key | Type | Default | What it does |
| --- | --- | --- | --- |
| `autonomous.merge_shadow.enabled` | bool | `true` (when unset) | Master switch: when `true` the supervisor's release step runs the shadow plan; when `false` no plan is recorded at all. It is not a mode — there is no `on` state. |
| `autonomous.merge_shadow.gate_timeout` | duration | `20m` | The wait budget each gate of the throwaway trial merge may use before giving up — a per-gate budget, not a cap across all gates. A value that is not a positive Go duration is refused with the problem named on every write surface (nothing is written); a stored value that fails to parse falls back to the default at read time. |
| `autonomous.merge_shadow.plan_keep` | int | `200` | How many shadow merge plans are kept per project; anything older than the newest N is pruned. Values below `1` fall back to the default. |

## Where the settings can be set

The settings are the same no matter which path writes them — one settings
block, one place it is read back from:

| Path | How |
| --- | --- |
| **YAML** | `config.yaml` → `autonomous.merge_shadow.enabled`, `autonomous.merge_shadow.gate_timeout`, `autonomous.merge_shadow.plan_keep` |
| **REST** | `PUT /api/config` with any of the three keys (the `GET /api/config` read returns the merged default for each) |
| **MCP** | The `autonomous_config_set` tool carries each key as its own argument |
| **CLI** | `datawatch config set autonomous.merge_shadow.enabled true|false` and friends |
| **Comm channel** | `configure autonomous.merge_shadow.enabled false`, `configure autonomous.merge_shadow.gate_timeout 1m`, `configure autonomous.merge_shadow.plan_keep 50` |
| **Web UI** | Settings → Automation section → **Merge shadow** card (one toggle + two fields, each linking to this howto); the state read above lives on the **Merge shadow** card of the Automation **Setup and policy** sub-tab |

## Do not confuse

- **Merge shadow (`autonomous.merge_shadow.*`)** — the shadow merge plan: what,
  recorded, `applied` stuck at `false`. This page.
- **Merge pending (`autonomous.automation.merge_pending_*`)** — the live
  *alert* that a finished Automaton's merge has not landed on main. It is a
  different settings block with a different job
  (see [automation-tab.md](automation-tab.md)).
- **Merge pipeline (`autonomous.merge_pipeline.*`)** — the *real* merge: the
  steps that move the release. The shadow plan reads the pipeline's steps to
  know what would happen (see [merge-pipeline.md](merge-pipeline.md#the-shadow-merge-plan)).

## What you still review

The merge shadow never applies, files or blocks — so the review is on you. Read
the stored plan for any finished Automaton (REST, MCP, the CLI `compare` or the
comm channel; the PWA card shows the comparison, not the plan), then review:

- Every difference the shadow comparison grades — that is the contract with the
  manual merge; the comparison is evidence, not a gate.
- Every defect candidate in the data directory's
  `defect-candidates/<automaton id>.json` — the candidates have no read surface
  today, and filing them into the defect queue is a separate job.
- The parity-issue and gate lines in the plan, per
  [merge-pipeline.md](merge-pipeline.md#what-is-still-yours-to-do).
- The fact that comparison records are not persisted yet — the comparison
  surfaces above stay empty until that lands; the plan is the one read that
  already works.
- Whether the Phase 4 evidence bar is met before you step out of the loop entirely —
  the shadow is how you collect that evidence
  ([2026-11-03-autonomy-handover-plan.md](../plans/2026-11-03-autonomy-handover-plan.md),
  Phase 4).

## Related

- [merge-pipeline.md](merge-pipeline.md) — the walkthrough; the shadow merge plan in
  `merge` and the settings block it reads.
- [api/autonomous.md](../api/autonomous.md) — the autonomy endpoints the daemon
  exposes (the shadow state is `GET /api/autonomous/merge/shadow` beside the
  merge reads).
- [the merge-shadow settings block](../datawatch-definitions.md#merge-shadow) — the
  settings block as a reference.
- [the autonomy-handover plan](../plans/2026-11-03-autonomy-handover-plan.md) — the
  shadow → on → off handover that these settings serve (Phase 4).
