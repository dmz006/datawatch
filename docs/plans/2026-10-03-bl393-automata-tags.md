# BL393 — Nested tags for Automata/PRD organization

**Date:** 2026-10-03
**Status:** plan only. No code written, no settings changed. Operator asked
for a plan following this repo's `docs/plans/` structure — this doc is that
plan, nothing has been implemented.

## 1. Problem

Operator-raised: "automata/prd needs a folder or grouping mechanism so I can
have multiple projects and organize." Confirmed by direct observation of
this session's own usage pattern — many unrelated efforts (CVE automation,
cross-repo GitHub hardening, this very feature) all happen as separate
Automatons inside the same `datawatch` working directory. Today the Automata
list (`internal/server/web/app.js`, `loadAutomataPanel`/`_automataRenderCards`)
is a single flat list with two filter dimensions — `statusFilter` and
`typeFilter` (`_automataState`, line ~16390) — neither of which groups
anything visually; they only hide/show cards in one flat stream. There is no
mechanism today to visually section Automatons into operator-defined projects
or efforts.

## 2. Decisions already made (operator, this session — not open for revision in this plan)

Three design forks were raised and decided directly by the operator via
explicit choice, before any code was written:

1. **Grouping source: both.** An explicit, operator-set mechanism on each
   Automaton, *plus* an automatic suggested default derived from the
   existing BL27 project-alias registry (name → directory) when a new
   Automaton's `project_dir` resolves to a registered alias.
2. **Terminology: tags**, not "folder"/"group"/"project" — avoids colliding
   with this codebase's existing overloaded terms (`ProjectDir`, BL27
   "project alias").
3. **Storage: a managed registry** (first-class Tag objects with
   `id`/`name`/`parent_id`/`color`, not bare strings on each Automaton) —
   renaming or re-parenting a tag updates everywhere it's used, the way
   renaming a file doesn't require editing every program that references
   its old name.
4. **Nesting: from the start.** Tags form a tree (e.g. `Work` → `Mobile App`
   → `iOS`), not a flat namespace.
5. **Multiplicity: multiple tags per Automaton, multi-section display** —
   standard label/tag behavior. An Automaton tagged both `Mobile App` and
   `Q4 Security` appears under both when browsing by tag.

Everything below is *implementation design* following those five decisions,
not a re-opening of them. If any of this doc's downstream choices (API
shapes, specific UI mechanics, phasing) don't match what the operator
wants, those are fair to redirect — the five decisions above are not, unless
explicitly revisited.

## 3. Prior art in this codebase (why this design looks the way it does)

Two existing features are directly analogous and this design deliberately
copies their conventions where they fit, and deliberately diverges where
they're known to have a real flaw:

- **`Type` registry** (BL221, `internal/autonomous/manager.go:233-246`) —
  `AutomatonType{ID, Label, Description, Color}`, a `RegisterType`/`ListTypes`
  pair, a REST registry at `/api/autonomous/types`, and a PWA management
  panel (`loadAutomataTypeRegistryPanel`, `app.js:24702`) with inline
  create-with-color-swatch UI. Structurally the closest thing to a "managed
  registry" already in this codebase — **but it has a real bug worth not
  repeating: `m.types` is in-memory only.** Grep confirms no `loadTypes`/
  `saveTypes`/`types.jsonl` exists anywhere — operator-registered custom
  types are silently lost on every daemon restart. Any Tag registry must
  persist to disk from day one; this plan's storage design (§5) does.
- **`Skills []string` on PRD** (`internal/autonomous/models.go`) — already
  exactly the "list of foreign IDs on a PRD" pattern `TagIDs` needs. No new
  join-table infrastructure required; direct precedent already in the
  model.
- **`GuardrailProfile` store** (`internal/autonomous/store.go:530-642`) —
  the exact JSONL-persisted, `Save`/`Get`/`List`/`Delete`-by-ID pattern a
  real (persisted) Tag registry should follow: `guardrail_profiles.jsonl`,
  loaded via `loadJSONL`/`writeJSONL` helpers already in `store.go`. A
  `tags.jsonl` following this exact shape is the straightforward path.
- **BL27 project aliases** (`internal/server/projects.go`) — operator-
  registered `name → dir` (+ optional default backend), stored via
  `internal/config`, surfaced at `/api/projects`. This is decision #1's
  "derived suggestion" source: when an Automaton's `project_dir` matches
  a registered alias's `dir`, suggest (not silently auto-apply) a tag
  named after that alias at creation time.
- **Bulk/select-mode action bar** (`app.js:16525-16584`,
  `batchAutomataAction`) — already supports multi-select with per-action
  eligibility counts (run/approve/cancel/archive/delete). The natural
  extension point for "bulk-tag N selected Automatons" rather than a new
  UI mechanism.

## 4. Data model

```go
// internal/autonomous/models.go — new type, alongside AutomatonType
type Tag struct {
    ID          string `json:"id"`                    // 8-hex, like PRD.ID
    Name        string `json:"name"`
    ParentID    string `json:"parent_id,omitempty"`    // "" = root tag
    Color       string `json:"color,omitempty"`        // CSS color, mirrors AutomatonType.Color
    Description string `json:"description,omitempty"`
    CreatedAt   time.Time `json:"created_at"`
    UpdatedAt   time.Time `json:"updated_at"`
}
```

```go
// PRD gets one new field, following the existing Skills []string pattern:
TagIDs []string `json:"tag_ids,omitempty"`
```

**Cycle safety:** reparenting a tag (`ParentID` update) must reject a move
that would make a tag its own ancestor — walk the new parent chain to the
root on every `SetTagParent` call and refuse if the tag being moved appears
in it. Small, same shape as `Manager.recurse`'s existing
`Depth + 1 > MaxRecursionDepth` guard for PRD parent/child recursion
(`internal/autonomous/manager.go`), just a cycle check instead of a depth
cap.

**Deleting a tag with children:** needs an explicit operator decision
(§8) — see "open implementation questions" below. Deleting a tag that's
still referenced by Automatons needs one too.

## 5. Storage

New `internal/autonomous/store.go` additions, mirroring
`GuardrailProfile`'s existing shape exactly (§3):

- `Store.tags map[string]*Tag` field (alongside `profiles`).
- `SaveTag`, `GetTag`, `ListTags`, `DeleteTag` methods — same signatures as
  the `*GuardrailProfile` equivalents.
- `load()`/`save()` additions: `loadJSONL(filepath.Join(s.dir, "tags.jsonl"), ...)`
  and a matching `writeJSONL` call, same as `guardrail_profiles.jsonl`.
- `PRD.TagIDs []string` persists automatically — it's part of the existing
  `PRD` struct already serialized to `prds.jsonl`; no separate migration
  needed, old PRDs just have an empty/absent list.

## 6. API surface

Manager methods (`internal/autonomous/manager.go`), mirroring
`SetPRDType`/`RegisterType` exactly (§3):

- `CreateTag(name, parentID, color, description string) (*Tag, error)` —
  validates `parentID` exists if non-empty.
- `RenameTag(id, newName string) error`
- `SetTagParent(id, newParentID string) error` — cycle check (§4).
- `SetTagColor(id, color string) error`
- `DeleteTag(id string, mode string) error` — `mode` resolves the open
  question in §8 (cascade / reparent-children-to-root / refuse-if-children).
- `ListTags() []Tag`
- `SetPRDTags(prdID string, tagIDs []string) error` — replaces the full
  list, same "hot-path, no executor needed" shape as `SetPRDType`.

REST (`internal/server/autonomous.go`), new registry endpoint alongside the
existing `/api/autonomous/types`:

- `GET /api/autonomous/tags` — list.
- `POST /api/autonomous/tags` — create (`{name, parent_id?, color?, description?}`).
- `PATCH /api/autonomous/tags/{id}` — rename/recolor/reparent.
- `DELETE /api/autonomous/tags/{id}?mode=<cascade|reparent|refuse>`.
- New `case "set_tags":` in the existing `/api/autonomous/prds/{id}/{action}`
  switch (`internal/server/autonomous.go:1176` area, right next to
  `set_type`) — `{"tag_ids": [...]}`.

MCP (`internal/mcp/`, new file `bl393_tags.go` mirroring `bl221_types.go`):

- `autonomous_tag_list`, `autonomous_tag_create`, `autonomous_tag_rename`,
  `autonomous_tag_set_parent`, `autonomous_tag_delete`.
- `autonomous_prd_set_tags`.

CLI (`cmd/datawatch/cli_autonomous.go`):

- `datawatch autonomous tag-list`
- `datawatch autonomous tag-create <name> [--parent <id>] [--color <hex>]`
- `datawatch autonomous tag-rename <id> <new-name>`
- `datawatch autonomous tag-move <id> --parent <new-parent-id|root>`
- `datawatch autonomous tag-delete <id> [--mode cascade|reparent|refuse]`
- `datawatch autonomous prd-set-tags <prd-id> --tags <id1,id2,...>`

Comm channel (`internal/router/sx2_parity.go`), new cases alongside the
existing `"set-type", "set_type"` one:

- `"tags"` → list (`GET /api/autonomous/tags`).
- `"set-tags", "set_tags"` → `usage: autonomous set-tags <prd-id> <tag-id1,tag-id2,...>`.
- Tag creation/reparenting from a chat command is lower priority — the
  PWA registry panel (§7) is the primary surface for tag *management*;
  comm channel's job is quick reads + per-Automaton tag assignment, same
  division of labor `set-type` already has (no `type-register` equivalent
  was added to comm channel either — confirmed by grep, only `set-type`
  exists there, type *registration* is PWA/REST-only today).

## 7. PWA UI

1. **Tag registry panel** — new settings panel mirroring
   `loadAutomataTypeRegistryPanel` (`app.js:24702`) structurally: built-in
   section (none for tags — all operator-created) + a tree view instead of
   a flat list, indent-per-depth, same inline create form (name + optional
   parent dropdown + color swatch). Reparenting via a parent-dropdown edit
   on each row is enough for v1; true drag-and-drop reparenting is a nice-
   to-have, not required (see §9).
2. **Tag assignment per Automaton** — a multi-select tag picker on the
   Automaton detail view (`renderPRDDetailView`) and in the create-Automaton
   modal, showing the tag tree with checkboxes. At creation time, if
   `project_dir` resolves to a BL27 alias with a matching or similarly-named
   tag, pre-check it (decision #1's "suggested default") — operator can
   uncheck before submitting; nothing is auto-applied silently.
3. **Browse-by-tag view** — new filter dimension alongside the existing
   `statusFilter`/`typeFilter` badges (`_automataState`), but behaving as
   true visual sectioning rather than a hide/show filter: a left-rail or
   top tag-tree selector: "All" (today's flat behavior, default/unchanged)
   vs. picking a tag shows only Automatons carrying it (and, for a parent
   tag, optionally its descendants' Automatons too — exact semantics is
   an open question, §8). An Automaton with multiple tags shows a small
   tag-chip row on its card (same visual slot where the existing type
   badge already renders).
4. **Bulk tag assignment** — new `tag` action in the existing batch bar
   (`batchAutomataAction`, `app.js:16565-16581`), alongside
   run/approve/cancel/archive/delete: opens the same multi-select tag
   picker, applies to every selected Automaton.
5. **Persisted UI state** — which tag(s) are currently selected for
   browsing follows the existing `cs_automata_*` localStorage convention
   (e.g. `cs_automata_tag_filter`), same as `cs_automata_filter`/
   `cs_automata_pinned` today.

## 8. Open implementation questions (for the operator, when this moves from plan to build)

These are real forks a build session will hit; flagging now rather than
silently picking an answer, consistent with "I make all decisions" from
this planning session:

1. **Deleting a tag that has children:** cascade-delete the whole subtree,
   reparent children up to the deleted tag's own parent (or to root), or
   refuse the delete until children are moved/removed first? --- Refuse to delete until children are moved/removed first.
2. **Deleting a tag that's still assigned to Automatons:** just remove it
   from their `TagIDs` silently, or refuse/warn with a count first? --- refuse/warn with a count first.
3. **Selecting a parent tag in the browse view:** show only Automatons
   tagged with that exact tag, or also everything tagged with any
   descendant (e.g. selecting `Work` also shows things tagged only
   `Work/Mobile App/iOS`)? This is a real UX choice, not just an
   implementation detail — affects whether tagging something only at a
   leaf is enough or whether operators will expect to also tag it at
   intermediate levels. --- lets iterate and discuss this; give me examples of what the options are and what that will change for both configuration, browsing, managing, ec
4. **Suggested-tag-from-alias behavior (§7.2):** pre-check a suggested tag
   derived from the BL27 alias as proposed, or go further and
   auto-*create* that tag if it doesn't exist yet (vs. only suggesting
   among tags that already exist)? --- only suggesting

## 9. Explicitly out of scope / deferred

- **Drag-and-drop tag-tree reparenting in the PWA** — a parent-dropdown
  edit achieves the same result with far less UI work; revisit only if the
  tree gets deep/wide enough that dropdown selection becomes painful.
- **Tag-based automation** (e.g. "auto-run anything tagged X") — this plan
  is purely an organizational/visual feature, not a new trigger mechanism.
- **Retroactive auto-tagging of existing Automatons** by inferring from
  their current `ProjectDir` — could be offered as a one-time opt-in
  migration helper, but isn't required for the feature to work going
  forward, and silently re-tagging existing data without being asked
  contradicts "I make all decisions."
- **Per-federation-peer tag namespacing** — tags are local to one daemon's
  registry for v1; federation-wide shared tag registries (if ever wanted)
  are a separate, bigger design question not implied by the original ask. -- keep this in mind, it is a good idea

## 10. Parity surface

Per the Mobile-Parity Rule (`AGENT.md`) and this repo's established full
parity set: `REST`, `MCP`, `CLI`, `comm channel`, `PWA`, `Android`, `iPhone/iOS`.

- **REST/MCP/CLI/comm channel/PWA:** in scope, designed above (§6-7).
- **YAML/config:** not applicable — tags are operator data (like PRDs
  themselves), not daemon configuration; no `config.yaml` field needed.
- **Android / iPhone·iOS:** new operator-visible affordance + new API
  surface (`/api/autonomous/tags`, `tag_ids` field on PRD responses) —
  triggers the Mobile-Parity Rule's clauses #3 and #4. File a
  `datawatch-app` issue once this actually ships (not at plan stage) with:
  the new endpoints, the `tag_ids` field on PRD JSON, and acceptance
  criteria (view tags on an Automaton card, assign/unassign tags, browse
  by tag — registry *management*, i.e. creating/renaming/deleting tags,
  can reasonably stay PWA-only for v1, same as the Type registry today
  which has no mobile management UI either — confirmed by checking
  `datawatch-app`'s own codebase has no `type-registry` equivalent screen).

## 11. Verification (once actually implemented)

- Unit tests: cycle-rejection on `SetTagParent`, `tags.jsonl` round-trips
  through a `Store` restart (the exact bug being avoided from `Type`'s
  in-memory-only registry, §3 — this is the regression test that proves
  it wasn't repeated).
- `GET`/`POST`/`PATCH`/`DELETE /api/autonomous/tags` manual round-trip,
  same pattern as this session's own CODEOWNERS/ruleset verification
  habit: never trust a 2xx response alone, `GET` again after every write.
- PWA manual smoke: create a 3-level tag tree, tag an Automaton with two
  leaf tags under different parents, confirm it appears under both in the
  browse view, bulk-tag 3 selected Automatons via the batch bar, delete a
  parent tag and confirm whichever §8-question-1 answer was chosen
  actually happens.
- Full `go test ./...` clean + CHANGELOG + version bump, per this repo's
  standard release cadence.
- full e2e testing configuration
