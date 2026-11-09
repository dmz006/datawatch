---
docs:
  index: true
  topics: [merge, shadow, pipeline, release, ship, automata, autonomous, settings]
---

# How-to: Merge shadow (the settings behind the shadow merge plan)

When an Automaton finishes, the merge shadow works out what merging its
branch would do — the release size, the version, the changelog text, the
README line, the registry row, the conflicts and the gates — and keeps that
plan per project so the merge that actually lands can be compared with it.
It never applies anything; the supervisor's manual merge stays the only path
to a merge.

The full mechanics — what the plan carries, the throwaway trial merge that
finds the conflicts, the gates, and the post-ship comparison that grades
every difference major or minor and records a defect candidate per
difference — are in
["The shadow merge plan" in howto/merge-pipeline.md](merge-pipeline.md#the-shadow-merge-plan).
This page is about the three settings and where they are set. All of it is
Phase 4 (shadow mode) of the [autonomy handover plan](../plans/2026-11-03-autonomy-handover-plan.md).

## What the merge shadow does — and never does

**Does:** writes one plan per finished Automaton (record, not queue —
applied is always `false`, and the store refuses a plan marked applied);
keeps plans per project under `<data dir>/merge-plans/`; and, after the
merge has landed and the shipped record is written, runs the background
shadow comparison and records one defect candidate per difference under
`<data dir>/defect-candidates/`.

**Never does:** applies or schedules a merge (no branch, tag or ref is
created or moved, nothing is pushed); blocks, retries or fails anything (a
failed run is logged and dropped, and a timed-out gate only marks that gate
`timeout`); or exposes its output yet (no REST route, MCP tool, CLI verb or
PWA card reads the plans, the comparison or the candidates — review means
reading the JSON files under the data directory, and filing candidates into
the defect queue is a separate job).

## The three settings

| Key | Value | Default (when unset) | Meaning |
|---|---|---|---|
| `autonomous.merge_shadow.enabled` | boolean | on (`true`; absent reads on) | The master switch. On, finishing an Automaton writes its shadow merge plan and the post-ship comparison runs when the merge lands. Off, neither happens — no plan, no comparison, no candidates. |
| `autonomous.merge_shadow.gate_timeout` | a Go duration string (`20m`, `90s`) | `20m` | The budget each of the three gates (`build`, `lint`, `test` make targets in the throwaway worktree) waits before it is killed. Each gate gets its own budget, so one hung gate cannot eat the others' time; a timed-out gate is reported as `timeout` and the remaining gates still run. An unparseable or non-positive value is refused. |
| `autonomous.merge_shadow.plan_keep` | a whole number ≥ 1 | `200` | How many shadow merge plans are kept per project, newest first; older ones beyond the count are pruned. A value below 1 is refused. |

## Where the settings are set

All keys live under `autonomous.merge_shadow` and can be set every way
below; the question-mark help links on the card open this page.

| Method | How |
|---|---|
| **YAML** | Edit `~/.datawatch/config.yaml` → `autonomous.merge_shadow:` — `enabled`, `gate_timeout`, `plan_keep` (see [config-reference.yaml](../config-reference.yaml)) |
| **CLI** | `datawatch config set autonomous.merge_shadow.enabled false` (likewise the other two keys); read it back with `datawatch config get autonomous.merge_shadow.enabled` |
| **Web UI** | Settings → the Automation section → the **Merge shadow** card |
| **REST API** | `PUT /api/config` with e.g. `{"autonomous.merge_shadow.plan_keep": 50}`; read with `GET /api/config` |
| **Comm channel** | `configure autonomous.merge_shadow.enabled=false` (likewise the other two keys) |

Every surface re-validates the value before it is written: `enabled` must
parse as a boolean, `gate_timeout` as a positive Go duration, and
`plan_keep` as a whole number at least 1 — a bad value is refused with the
problem named and nothing is written.

## Not to be confused with

`autonomous.automation.merge_pending_mode` ([automation-tab.md](automation-tab.md))
has a `shadow` mode too, but that is the merge-pending pass recording what
it *would have done* (a decision history in `merge_shadow_decisions.json`)
without writing a record or raising an alert. That is a separate feature
from the `autonomous.merge_shadow` block on this page, which controls the
finish-time shadow merge **plan**, the per-project plans and the post-ship
comparison.

## What you still review

Everything the merge shadow produces is a review item, not an answer: every
difference the post-ship comparison grades (each one names its field, both
values and its severity), the planned lines that landed nowhere, and
whether the shadow phase's evidence bar has been met before the supervisor
steps out of the loop (see the handover plan, Phase 4).

## Related

- [howto/merge-pipeline.md — The shadow merge plan](merge-pipeline.md#the-shadow-merge-plan): the plan's contents, the trial merge and the post-ship comparison
- [datawatch-definitions.md — Shadow merge plan](../datawatch-definitions.md#shadow-merge-plan): the glossary entry
- [plans/2026-11-03-autonomy-handover-plan.md](../plans/2026-11-03-autonomy-handover-plan.md): Phase 4 modes, defaults and exit criteria
