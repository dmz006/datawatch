# Plan: list tags in use with counts, filter sessions by tags, on the CLI

Date: 2026-10-31.

## 1. What

One capability, two faces:

- **List the tags in use** and how many sessions carry each, on the CLI. The
  answer comes from the live session state, not from the tag registry: a tag
  that exists in the registry but is on no session must not appear (or must be
  shown with count 0 and be the obvious one), and a tag that is on a session
  but was never registered must still appear, because the sessions are the
  source of truth for "in use".
- **Filter the session list by tags**, on the CLI: `datawatch sessions` keeps
  only sessions carrying at least one of the named tags. This is the half of
  the plan's name that has not shipped yet — registry browsing, assignment and
  the tag-change surfaces exist today (phase 3, `datawatch tags`, MCP
  `session_set_tags`, REST `POST /api/sessions/tags`), but the session list
  has no tag condition.

The rest of the file is the working record: what is already in place, what the
new parts are, the decisions with their reasons, and what is deliberately not
part of this increment.

## 2. What exists today

| Surface | State |
|---|---|
| `autonomous.tag_registry` — `{scope: global/automaton, axis, value, owner_automaton_id, description}` | Implemented, `internal/autonomous/tag_registry.go`; persisted in the autonomous state |
| `autonomous.session_automaton_ids` — many sessions under one Automaton | Implemented; sessions also keep an `InheritedTags` copy for display |
| `datawatch tags list/assign/unassign`, MCP `autonomous_tag_*`, chat `autonomous tags`, PWA tags panel | Shipped (phase 3) |
| `session_set_tags` MCP, `POST /api/sessions/tags` REST | Shipped; replaces a session's own tags, inherited tags read-only |
| Tag change events | One `tag_update` WebSocket message per registry or assignment change (`internal/autonomous/tag_event.go`); the PWA tag panel re-renders from it, and `GET /api/autonomous/tag_registry` is the re-paint path. **There is no dedicated `session_tags_changed` / `prds_updated` event** — that is a separate concern (see §11) and is not required by this increment |
| Placement axes | `lane` (with `lane:supervisor`, reserved by definition), `role`, `backend`, `stage`, `kind`, `source:prd` |
| `datawatch sessions` CLI | Lists sessions with name, id, state; **no tag column, no tag filter** |
| PWA tags panel | Shows the registry and per-Automaton assignments; **no "N sessions carry this tag" figure** |
| Session tag filter | None anywhere — not in the CLI, not in the REST list, not in MCP `list_sessions` |
| `GET /api/sessions/tags` | **Not present.** The only `tags` route on the session object is the existing `POST /api/sessions/tags`, which *sets* a session's own tags (see §12 — it is reused, not replaced) |

## 3. What is new in this plan

1. **A tag inventory read**: the set of tags in use across the sessions this
   daemon knows about (live plus the completed records that are still kept),
   with, for each tag, the number of sessions that carry it. The sessions are
   the source; the registry is not consulted for membership. The inventory
   answers "which tags are in use and how many sessions carry each" — it does
   not answer "how many sessions carry my *exact* tag set" (that combination
   count comes from the filter in 2, applied to the list).
2. **A tag condition on the session list**: `datawatch sessions --tag lane:supervisor
   [--tag role:… ...]` keeps only the sessions that carry at least one of the
   named tags (the session's own tags or the tags it inherited from its
   Automaton). Repeated flags OR together; a tag named that no session
   carries keeps nothing. This is deliberately an OR, not an AND: "show me the
   supervisor-lane sessions" is the question the flag is for, and AND-matching
   on exact sets is a different, rarer question that a count of the filtered
   list answers well enough to start.
3. **The CLI surfacing both**: `datawatch sessions` shows a `tags` column
   (own tags first, inherited ones marked) and accepts the `--tag` condition;
   `datawatch tags list` gains a `sessions` column with the in-use count per
   tag, so a registry browse and a "what is actually used" glance are the same
   command. The PWA stays untouched in this increment (see §12).

Nothing here changes who owns a tag, how tags are assigned, or what a tag
means. It reads state that already exists and puts a question in front of it
that nothing currently answers.

## 4. Decisions

### 4.1 The inventory counts sessions, not sessions per tag

The unit of the answer is the tag. Each tag gets one number: how many
sessions carry it. A session that carries `lane:supervisor` and `role:review`
and `source:prd` contributes 1 to each of those three tags and nothing to any
other. Two consequences follow that are worth stating because they are where
the number could otherwise be read wrong:

- The counts are **not additive across tags** — they count session-tag
  pairs, projected onto the tag. A fleet of ten sessions each carrying two
  tags produces a total of twenty across the column, ten sessions.
- A session that carries the same tag from two places (its own list and its
  inherited list, if the Automaton also carries it) counts **once** for that
  tag. Ownership of the tag is not part of the count; the panel shows where
  it comes from, the number does not.

### 4.2 Own tags and inherited tags both count as "in use", and are not separated in the count

A session under an Automaton inherits the Automaton's tags. For the question
"which tags are in use" the inheritance is load-bearing: the fleet's
supervisor-lane sessions are precisely the sessions that inherited
`lane:supervisor` from their Automaton, and a count that only read each
session's own tag list would report zero for the tags the fleet actually runs
with. So the inventory unions a session's own tags and its `InheritedTags`,
dedupes the union per session, and counts it.

The same union drives the §3 filter: `--tag lane:supervisor` matches a
session whether the tag is its own or inherited. Whether a given match was
own or inherited is shown, not filtered: the `sessions` list prints the tags
column with inherited entries marked (the column format is the same question
the PWA tags panel already answers, so a `+` prefix or the like is a display
choice, not a semantics choice). No filter dimension is added for origin —
nobody asked "sessions that own the tag themselves" and inventing that
question is scope (see §12).

This is the one place the two tag stores (own and inherited) meet, and the
rule is a set union on the *value* of the tag, not on its storage. A tag that
is globally registered, owned by an Automaton, and inherited by three
sessions is one tag in the inventory.

### 4.3 "In use" means on a live or kept-completed session of this daemon; the registry is only a shape check

The source of the inventory is the session state the daemon holds: the live
sessions plus the completed records that have not yet been reaped. A tag on no
such session is not in use, full stop — it may still be registered, and
`tags list` shows that (the registry column stays), but the `sessions` column
is 0 and the row reads as a registered-but-unused tag.

The inverse is what makes the sessions the source of truth, not the registry:
a tag that appears on a session but is missing from the registry still counts
in the inventory, and still matches `--tag`. Phase 3 already tolerates this on
the write path (a session can carry tags its Automaton does not own, and the
registry store accepts values the daemon has not validated against a
whitelist), so the read path must not be stricter than the write path or the
list and the filter would disagree about what a tag is. The registry is used
for exactly one thing: the known axes, when the daemon wants to *suggest* or
*sort* (lane first, then role, then the long tail) — a presentation choice,
never a membership test.

The inventory is not persisted. It is computed on the read from the session
state, the same way the dashboard stats are, and it never outlives the read.
There is no `tags_in_use.json` and no `tag_inventory` store: anything that
mirrors session state will drift from it, and the whole point of §4.1 is that
the number is as fresh as the sessions.

### 4.4 The filter is OR over tags; the exact-set combination is the list's problem, not the filter's

`--tag a --tag b` means "carries a **or** b". The reasons:

- The question it is for — "give me the supervisor-lane sessions" — is a
  membership question. AND semantics ("carries both a and b") answers a
  different question and is a worse default for the flag that is going to be
  typed in a terminal.
- OR composes with the other list filters (state, backend) that the CLI
  already has as AND conditions on *session* fields: tags are a set-valued
  field on the session, and set-membership OR is the shape that matches how
  tags are used (a session wears several, and you want the ones wearing any
  of them).
- The exact-combination count is still reachable: pipe the filtered list to
  `wc -l`, or read the `sessions` column when every tag in the combination is
  on the same row. Neither requires a new filter combinator.

What is refused: an empty `--tag` value (a flag with no tag names a filter
that matches everything, which is the same as not filtering — it is rejected
with a usage error rather than silently being a no-op), and a `--tag` on a
subcommand that does not list sessions (`tags assign` already takes the tag
as its own argument and does not accept session conditions; the two stay
apart).

### 4.5 Placement axes participate in the filter as ordinary values; nothing special-cases them

`lane:supervisor`, `role:review`, `backend:<name>`, `stage:<name>`,
`kind:<name>`, `source:prd` are tag values like any other, and the filter
matches them like any other. The one exception is structural, not semantic:
`lane:supervisor` is reserved by definition (it marks the supervisor lane and
is not a free label), and if the inventory prints it that is because sessions
carry it, not because the code knows it is the supervisor lane. No axis gets
a dedicated flag (`--lane`, `--role`) in this increment — that is a
convenience alias and a second surface to keep in sync with `--tag`, and the
axes are an open list (phase 3 already anticipates new ones). If a specific
axis starts being filtered constantly, the alias is a small change then.

### 4.6 The read path is the session store, reached through the existing manager; no new daemon route is required for the CLI

The CLI talks to the daemon the way every other `datawatch <noun>` command
does. The inventory and the filter both live in `internal/session` (the
manager that owns the session list), because the sessions live there and the
tag state on them (own + `InheritedTags`) is on the session record. The
concrete wiring:

- **Inventory**: a manager method that walks the live sessions and the kept
  completed records, unions each session's own and inherited tags, and
  tallies. Returns `{tag → count}` plus the per-session membership the
  `sessions` column needs. ~60 lines including the dedupe.
- **Filter**: a tag condition on the existing session-list query, applied
  after whatever other conditions the CLI already sends (state, backend).
  The list endpoint already returns the session records with their tag fields;
  the condition is an in-memory predicate over the same records the
  inventory reads, which keeps the two answers consistent by construction —
  they cannot disagree because they are the same data.
- **CLI**: `datawatch sessions` gains the `tags` column and `--tag` (repeatable).
  `datawatch tags list` gains the `sessions` column, populated from the same
  inventory call. Both go through the daemon's existing session-list RPC; there
  is no new daemon-to-daemon surface.

The deliberately-not-part of this: a REST `GET /api/sessions/tags`. It is the
obvious next step and it is cheap (§12 says exactly what it should return and
why the route is a new one rather than a reuse of the `POST` that already
sits at that path), but this increment is the CLI, and the PWA does not
consume it yet. Building the route now to have the CLI go through a
half-client route is the order-of-operations mistake the existing code
already avoids (the CLI for `tags list` talks to the registry RPC directly,
not through a REST shim).

## 5. Where the pieces go

| Piece | File | New or existing |
|---|---|---|
| Tag inventory over the session state | `internal/session/tag_inventory.go` | New, small |
| Tag condition on the session list | `internal/session/manager.go` (list query) | Extension of the existing list path |
| `datawatch sessions` tags column + `--tag` | `cmd/datawatch/sessions.go` | Extension |
| `datawatch tags list` `sessions` column | `cmd/datawatch/tags.go` | Extension, reuses the inventory call |
| Registry shape (for sort/suggest only) | `internal/autonomous/tag_registry.go` | Existing, read-only here |
| WebSocket `tag_update` | `internal/autonomous/tag_event.go` | Existing; **no new event in this increment** (§11) |
| `GET /api/sessions/tags` | not built here | Deferred (§12) |
| PWA tags panel count | not built here | Deferred (§12) |

## 6. Behaviour, concretely

```
$ datawatch tags list
lane:supervisor   supervisor  12
role:review       global      0
source:prd        global      3
unregistered-tag  —           1        # on a session, not in the registry; still listed, axis column blank

$ datawatch sessions --tag lane:supervisor
NAME          ID     STATE    TAGS
sup-1         a3f2   running  lane:supervisor role:review +lane:supervisor
...

$ datawatch sessions --tag lane:supervisor --tag source:prd   # OR
```

(Exact column layout is a display decision; the semantics above are the
contract.)

- A tag on no session: `sessions` column 0, filter matches nothing.
- A tag on a session but unregistered: listed with a blank axis, counted,
  matchable.
- A tag owned by an Automaton and inherited by three of its sessions: one row,
  count 3; each of the three shows it in the `tags` column marked as
  inherited.
- Reaping: when a completed session's record is reaped it drops out of the
  count on the next read. Nothing is cleaned up per-tag; the tag row simply
  falls as its sessions go.

## 7. Non-goals and what is explicitly not in this increment

1. **AND-matching / exact-set filters** on the session list (§4.4) — the OR
   flag plus the `sessions` column answers the questions that come up; the
   combinator is a follow-up if it does.
2. **Per-axis filter flags** (`--lane`, `--role`) (§4.5) — aliases, not
   semantics; add when an axis earns one.
3. **The REST `GET /api/sessions/tags` route and the PWA count column**
   (§12) — the next increment, defined here only so this one does not build it
   by accident and the next one does not have to design it from scratch.
4. **A dedicated `session_tags_changed` / `prds_updated` WebSocket event
   beyond the existing `tag_update`** (§11) — the PWA tag panel already
   re-renders from `tag_update`; whether the session list needs its own
   invalidation signal is a PWA-refresh question, not a data question, and it
   is deferred with the PWA work.
5. **Persisting the inventory** (§4.3) — it is a computed read, by design.
6. **Tag-level permissions and scopes** (who may *filter* by a tag, per
   Automaton tag visibility) — phase 3 scoped assignment; filtering is a local
   CLI operation on state the operator already sees, and a scope on the read
   would invent a permission model the write path does not have.

## 8. Risks and their dispositions

| Risk | Disposition |
|---|---|
| The inventory walks every kept session record on each `tags list` | The list is bounded by the kept-records retention (the same bound the dashboard stats read already walks); the walk is one pass and the tally O(distinct tags). If it ever shows up as a cost, the fix is a cached tally invalidated by session changes — not a persistent file. |
| Own/inherited union double-counts a tag a session both owns and inherits | Dedupe per session before the count (§4.1, §4.2); the test suite carries a case for exactly this shape. |
| The filter matches unregistered tags and an operator expects the registry to be the vocabulary | That is the point (§4.3): the read path is not stricter than the write path. The `tags list` row for such a tag, with a blank axis, is the visible consequence rather than a silent one. |
| `--tag` OR semantics surprise an operator who expected "all of" | §4.4 states it; the usage error on an empty `--tag` is the guard against the *worse* surprise (a filter that silently matches everything). |
| Two tag stores (registry and session fields) drift about which axes exist | The axes are open (§4.5); the registry is a suggest-list, not a gate, so a new axis appears in the inventory before it is registered and the row is the notice. |
| The deferred `GET /api/sessions/tags` lands later with a different shape than §12 sketches | §12 is the shape contract for that increment; this increment builds no client against it, so a re-shape costs the route only. |

## 9. Testing

The existing tag tests (phase 3, registry and assignment) are untouched and
stay green. New tests, per component:

- **Inventory**: the empty-state case (no tags anywhere → empty); own-only,
  inherited-only, and own-plus-inherited of the *same* tag (counted once); a
  tag on a kept-completed session counts; a tag on a reaped session does not;
  an unregistered tag is present with its axis absent.
- **Filter**: no `--tag` = today's list (regression); one tag matches own and
  inherited alike; two tags are OR (a session carrying only the second is in);
  a tag on no session yields the empty list, not an error; an empty `--tag`
  value is a usage error; the filter composes with the existing state/backend
  conditions (a session matching the tag but the wrong state is out).
- **`tags list` column**: the `sessions` figure equals the inventory for the
  same state (same call, asserted); a registered-but-unused tag shows 0; the
  blank-axis row prints.
- **Shape, not values**: the `sessions` column is a count of session-tag
  pairs projected onto the tag, and the tests assert the projection rule
  (§4.1) rather than a specific fleet, because the fleet is fixture data.

The suite runs as it does for `internal/session` and `internal/autonomous`
today (`go test ./internal/session/ ./internal/autonomous/`).

## 10. Sequencing

Phase 4 of the tags work. Phase 3 (registry, assignment, the change surfaces)
is shipped and is the read-only ground this stands on. The increment is small
and single-purpose:

1. Inventory method + tests (§5, `internal/session`).
2. Filter condition + tests, on the same list path.
3. CLI: `sessions` tags column and `--tag`; `tags list` `sessions` column;
   usage text.
4. Nothing else. The PWA count and the REST route are the follow-up that uses
   the same inventory call, and the `tag_update` re-render path already covers
   the panel.

## 11. The session-refresh event, separately

The plan's text names a `session_tags_changed` (or `prds_updated`) event
alongside the list and the filter. It is not part of this increment, and the
reason is worth recording so the split is not mistaken for an omission:

- The PWA tag panel already re-renders on the existing `tag_update` message
  (phase 3), and `tag_update` fires on every registry and assignment change —
  which is exactly the state that moves an inventory number.
- What is not covered: a change to a *session's own* tags that does not go
  through the registry path (`session_set_tags` on a session directly) does
  not, today, broadcast a session-list invalidation that a PWA session list
  observing tags would pick up on. A dedicated `session_tags_changed` is the
  candidate for that gap.
- That gap is a PWA session-list refresh question: it only matters once a PWA
  view renders per-session tags reactively, which is the deferred PWA work
  (§7.4). Until then an event with no client is a wire-format commitment made
  in the dark, and the wire format is the expensive part.

So: the data (inventory, filter) lands here; the event lands with the first
PWA consumer, defined at that time against the `tag_update` precedent. The
two are decoupled by design, and that is the cheaper order.

## 12. The deferred REST route, defined enough to build it later

Done (task 606c0018, 2026-11-06, REST + MCP surfaces). `GET /api/sessions/tags` ships as `{"tags":[{"tag","axis","value","count"}]}` — one entry per tag in use across live and archived sessions (own, system and inherited sets; a session counted once per tag), untagged tags sorted last, then by count. The `GET /api/sessions` filter gained the placement axis shortcuts (`project`/`automaton`/`automation`/`stage`/`kind`/`role`/`lane`/`compute`, each an `axis:<value>` tag combined with `tag` under `tag_match`) and system tags now count for `?tag=` matching as the plan's §4.2 union requires. MCP: `list_sessions` gained the eight axis params and the new `session_tag_list` tool (cap `sessions:list`). **The `backend` axis shortcut:** `backend` is both the existing BL361 session-backend filter (family names: claudecode, opencode) and the placement axis (claude, pi, ollama, vllm), so it is implemented as an OR: a session matches when its backend family equals the value or it carries the `backend:<value>` tag. Every result the old filter returned is still returned, and `backend=<axis value>` now works. Tests: `internal/server/session_tag_filter_test.go`, `internal/session/tag_inventory.go` (layer), `internal/mcp/session_tag_list_test.go`; docs: `docs/api-mcp-mapping.md`, both OpenAPI specs, `docs/testing-tracker.md`. The CLI column/filter and the comm-channel listing landed on the same inventory read (task f06c5a3bb), and the PWA session-list tag filter with per-tag counts landed on top (tasks 202f9f994, 3671eb1e0); current per-surface state is recorded in the Parity surface section below.

`GET /api/sessions/tags` (new route; the existing `POST /api/sessions/tags`
keeps its set-a-session's-tags job). Sketch:

- `200` → `{ "tags": [ { "tag": "lane:supervisor", "axis": "lane",
  "value": "supervisor", "sessions": 12 }, … ] }` — the inventory, one entry
  per tag in use, same source and same dedupe as the CLI (`sessions` is the
  count).
- `?axis=lane` filters the response to one axis (a presentational cut of the
  same read, not a different one).
- `401` as on the other session routes; `500` if the session store is
  unreachable.
- The PWA tags panel's missing "N sessions" column is this read rendered, and
  it re-rendered-on-`tag_update` exactly the same way the registry rows
  already do, so no new event is needed for the panel specifically.

It is deferred because the CLI is the increment and no client consumes the
route yet; it is defined here so the follow-up is wiring, not design, and so
this increment does not build a REST shim the CLI does not go through.

## Parity surface

Current state of the tag inventory read and the session tag filter across the
full parity set (as updated 2026-11-06/07, after the REST + MCP, then CLI +
comm, then PWA tasks):

| Surface | State |
|---|---|
| REST | In — `GET /api/sessions/tags` returns the inventory (`tag`, `axis`, `value`, `count`); `GET /api/sessions` accepts `tag` (repeatable, `tag_match` `any`/`all`) plus the eight placement axis shortcuts (`project`/`automaton`/`automation`/`stage`/`kind`/`role`/`lane`/`compute`), with `backend` matching family-or-tag as an OR so no earlier result is lost (§4.5, §12) |
| MCP | In — `list_sessions` gained the same axis params (including `tag`/`tag_match` semantics), and `session_tag_list` (cap `sessions:list`) returns the same inventory |
| CLI | In — `datawatch sessions` carries the `tags` column and the repeatable OR `--tag` flag (empty value is a usage error, §4.4), and `datawatch tags list` carries the `sessions` count column from the same inventory call (§3.3, §4.6) |
| comm channel | In — the session-listing verbs accept the tag condition and print the in-use listing, consuming the same read as the CLI |
| YAML/config | Excluded — this feature is a read view over session state that already exists; it introduces no setting, so there is no configuration surface to keep in sync (tag assignment itself remains governed by the existing `autonomous.*` config from phase 3) |
| PWA | In — the session list filters by tags and the tag chips show the per-tag counts from the same inventory read (tasks 202f9f994, 3671eb1e0); the tags panel keeps its `tag_update` re-render path, so no new event is required for it (§11) |
| Android | Excluded here, tracked separately — capability parity (list tags with counts, filter sessions by tag) is the standard for the native clients; implementation idioms are the mobile team's, and the app-side notes for this work sit in `docs/plans/2026-10-23-datawatch-app-tags-note.md` and `docs/plans/2026-11-03-placement-auto-tags-parity.md` |
| iPhone/iOS | Excluded here, tracked separately — same standard and same app-side notes as the Android row; all REST and WebSocket contracts it would consume are platform-neutral (Mobile-Parity Rule) |
