# Placement auto-tags — surface parity and the datawatch-app note

**Date:** 2026-11-03
**Feature:** placement (auto) tags — machine-derived **system tags** stamped on
every session the autonomous executor starts, recording where the work ran:
`compute:<node>`, `peer:<name>`, `backend:<name>`, `lane:<name>`,
`role:<name>`, `project:<name>` — plus operator-defined custom axes.
**Companion docs:** `docs/howto/automata-tags.md` (section *Placement tags
(the system tags)*, no tracker IDs) and the `autonomous.tags.placement` block in
`docs/config-reference.yaml`.

**Tracker context (this section only — it stays out of the operator howto):**
PRD `8e500001`; implementation tasks
`6c1b0006` (stamp on local placement, retry re-stamps the same session),
`6c1b0007` (`peer:<name>` from the explicit placement record),
`6c1b0008` (startup backfill for live leases), `6c1b0009` (Automata show the
tags as a read-only inherited set), `6c1b000b` (REST / MCP / CLI / comm
channel exposure of the settings and tag listing), `6c1b000c` (PWA filter,
grouping and settings).

## What "parity" means here

Three things exist on the daemon that any operator surface owes:

1. **The settings** — `autonomous.tags.placement.enabled`,
   `autonomous.tags.placement.<axis>.enabled` and
   `.template` for the six built-in axes (defaults: `compute`, `peer`,
   `backend`, `lane`, `role` on; `project` off), and the list-valued
   `autonomous.tags.placement.custom` (name / field / template /
   enabled), which only a list-capable surface can express.
2. **The tag reading** — the placement tags in the registry, the
   `system_tags` on each session, and the read-only inherited set on each
   Automaton; listing filtered by system tags and by one axis.
3. **The app-side display** — for the native Android / iPhone clients in
   `dmz006/datawatch-app`: the new settings keys (read; write if the app
   writes settings at all), a placement-tag **filter** on the session /
   Automaton lists, and placement-axis **grouping** of a session list — the
   PWA already has all three.

## Surface parity table

| Surface | Status | What it has / why not |
|---|---|---|
| YAML config file | **Included** | The complete surface: every key above, including the `custom` list block, in `tags_config` (see `docs/config-reference.yaml`, section *Automaton: tags*). Invalid axis names, custom names, fields or placeholders are refused at load with an empty block. |
| REST | **Included** | `PUT`/`GET /api/config` carries the scalar placement keys (the per-key `custom` is refused there — it is a list — so the `custom` block goes through the YAML file or a full config write). `GET /api/autonomous/tags` accepts `?system=true` (system tags only) and `?axis=<name>` (one axis). Sessions carry `system_tags`, Automata carry the read-only `inherited_tags`. |
| MCP | **Included** | `config_get` / `config_set` for the scalar placement axes and templates (same refusal for the `custom` list); `autonomous_tag_list` takes `system` and `axis` parameters. |
| CLI | **Included** | `datawatch config get` / `datawatch config set autonomous.tags.placement…` for the scalar keys; `datawatch tag-list --axis <name>` for the axis-scoped listing. |
| Comm channel | **Included, with one caveat** | `config get` and `configure` take the same scalar keys; the tag verbs are shared (`autonomous tags [counts]`). The `system` / `axis` listing filters are **not** exposed on the comm channel — `autonomous tags` maps to `GET /api/autonomous/tags` with at most `?counts=true` — so axis-scoped listing there is owed to REST / MCP / CLI. |
| PWA (this repo) | **Included** | Settings → General → Automata → **Placement tags** group (master switch, per-axis on/off, per-axis template text fields); the header **Tags** menu groups system placement tags by axis; the dashboard header carry a placement filter (a tag or a whole axis, `axis:*`) feeding the dashboard, tree and Gantt; the sessions list carry a group-by-placement select (`compute`, `peer`, … or none); session cards carry the session's `system_tags` read-only and Automata cards show the inherited set read-only; the session tag editor never sends them. |
| Android (`dmz006/datawatch-app`) | **N/A here** | No native app code lives in this repo. The surface is owed to the app repo; the note text for it is recorded below and this is a **record-only** note — no issue was filed and no `gh` command was run, per the task instructions. |
| iPhone / iOS (`dmz006/datawatch-app`) | **N/A here** | Same as Android: the app repo owns both native platforms; the identical note below covers both. |

## What the daemon already gives the app

| Surface | What the app gets |
|---|---|
| Config (read) | `GET /api/config` (or `config get` on any surface) → `autonomous.tags.placement.enabled` (default `true`), `autonomous.tags.placement.{compute,peer,backend,lane,role,project}.enabled` (default `true`; `project` is `false`) and `…template` (defaults `compute:{node}`, `peer:{peer}`, `backend:{backend}`, `lane:{lane}`, `role:{role}`, `project:{project}`). |
| Config (write) | `PUT /api/config` accepts the scalar axis keys; the `custom` list is refused per-key (YAML or full write only). A template missing its axis's placeholder is refused with the axis named and nothing written. |
| Tag listing | `GET /api/autonomous/tags?system=true` → the system tags (all placement tags, top-level); `&axis=lane` → one axis (`lane:<value>` names plus the bare `lane` name when it exists); both combine. |
| Sessions | every session JSON carries `system_tags` (e.g. `["compute:node-a","backend:pi","lane:default","role:worker"]`; no `peer:` tag on local work, `peer:<name>` on federated work) — read-only, replaced as a whole when a retry re-stamps the session on another node. |
| Automata | every Automaton JSON carries `inherited_tags` — the read-only union of the system tags of its own sessions (empty when it has none); the Automaton's own `tags` list is the operator's only. |
| Wording source | the PWA locale bundles (`internal/server/web/locales/*.json`) carry the operator strings for all of this, to be reused under the Terminology Rule (Automaton / Automata, never "PRD" in UI text): `settings_placement_group` + per-axis labels/templates, `dash_filter_placement_*` (filter, whole-axis option), `sessions_group_*` (group-by, untagged bucket), `tags_menu_placement_*` (grouped menu + empty state), `tags_axis_empty`. |

## Parity note for `dmz006/datawatch-app` (recorded, not filed)

Per the task instructions this note is **recorded here only**: no `gh`
command was run and no issue was filed in `dmz006/datawatch-app`. The text
below is the complete, self-contained wording to hand over (or file) when
that repo next takes mobile parity work.

> **Placement auto-tags — Android / iPhone parity**
>
> The daemon now derives placement (system) tags for every session the
> autonomous executor starts — one per enabled axis with a value:
> `compute:<node>`, `peer:<name>` (federated work only), `backend:<name>`,
> `lane:<name>`, `role:<name>`, `project:<name>` (off by default) — and
> optional operator-defined axes. Defaults: everything on except `project`.
> The PWA ships the full surface; the two native platforms owe three things.
>
> 1. **New settings keys** (read, and write if the app writes settings):
>    `autonomous.tags.placement.enabled`;
>    `autonomous.tags.placement.{compute,peer,backend,lane,role,project}.enabled`
>    (default `true`; `project` is `false`) and `…template` (defaults
>    `compute:{node}`, `peer:{peer}`, `backend:{backend}`, `lane:{lane}`,
>    `role:{role}`, `project:{project}`). Per-key writes reject the
>    list-valued `autonomous.tags.placement.custom` (name / field / template
>    / enabled) and any template missing its axis's placeholder — surface
>    both rejections as-is, and route `custom` editing to the YAML config
>    file.
> 2. **Tag filter** on the session and Automaton lists: filter by one
>    placement tag (e.g. `compute:node-a`) or by a whole axis (`compute:*`).
>    Server side: `GET /api/autonomous/tags?system=true[&axis=<name>]` for
>    the tag list; `system_tags` on each session and the read-only
>    `inherited_tags` on each Automaton for the matching. Empty value means
>    "no filter"; an axis with no tags is not one to offer.
> 3. **Grouping**: group the session list by a placement axis (e.g.
>    `compute`, `peer`, `backend`) with an "No `<axis>` tag" bucket for
>    sessions that lack that axis's tag, and a "no grouping" default.
>
> Display rules: placement tags are **system tags** — read-only in every
> place (no rename / move / delete action, and no way to edit a session's
> `system_tags` or an Automaton's `inherited_tags`); a local session writes no
> `peer:` tag and a retried session shows only its newest placement (the
> whole tag list is replaced on re-stamp); a tag only exists for axes enabled
> and valued, so never invent absent axes.
>
> Wording: reuse the PWA source strings under the Terminology Rule
> (Automaton / Automata, never "PRD" in UI text): `settings_placement_*`,
> `dash_filter_placement_*`, `sessions_group_*`, `tags_menu_placement_*`,
> `tags_axis_empty`, from `internal/server/web/locales/` in the daemon repo.
>
> Acceptance (app side): (1) the placement tag/axis filter and the
> group-by-axis session list work and match the PWA's results for the same
> data, on both platforms, in en and at least one other locale; (2) the PWA's
> read-only posture is preserved — no rename/move/delete affordance on system
> tags, no editing of `system_tags` / `inherited_tags`; (3) local work shows
> no `peer` option and retried sessions show exactly one latest placement
> axis set; (4) the six settings keys render with their PWA defaults.
>
> Daemon-side detail: `<this repo>/docs/howto/automata-tags.md` (Placement
> tags section) and `docs/config-reference.yaml` (`autonomous.tags.placement`).

## No external side effects

No `gh` command was run in this task, no issue was filed in
`dmz006/datawatch-app`, and no version bump or CHANGELOG entry is owed: this
is a docs-only change (one howto section, this parity record).

## 2026-11-08: which sessions carry which placement tags

Question to the operator: the planner (decompose) sessions carry `automaton:`, `kind:decompose`, `lane:` and `role:planner` but no `compute:` tag, and the supervisor session carries no system tags at all; should they get the full set? The operator answered, quoted: "Yes all that we are able to add tag for should". Decision: every session that can carry a placement tag carries every axis that applies to it, including `compute:` for the node it runs on. Draft Automaton filed from the spec in the supervisor scratch folder.

**Docs follow-up (2026-11-08) — operator review items.** The howto, the operations
Tag browser section, the glossary entry and the CHANGELOG `[Unreleased]` entry now
describe the every-applicable-axis rule. Still for the operator to review:

- The supervisor session's exact axis set is written as "its applicable axes, with
  `role:supervisor`, `compute:` and, when it runs in one, `backend:` and `lane:`".
  Confirm that set against a live supervisor session once the change ships.
- Existing sessions gain the new tags only through the tag sweep; the docs tell the
  operator to run `--dry-run` first. Confirm that is the wanted rollout, not an
  automatic re-stamp.
- The planner row lists `compute:` as added. The docs do not claim any other new
  axis for planner sessions.
