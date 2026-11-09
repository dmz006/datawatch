# [sync] Sessions page: every session's placement (system) tag chips on the first load — mobile parity (datawatch app)

**Filed 2026-11-08 as https://github.com/dmz006/datawatch-app/issues/401 (release v9.112.8).** Original note: — this is the tracked draft; the GitHub issue (or
follow-up comment) is opened when the operator files it. Filing the issue
does not change a line of this repo. This draft is a file write only: it
never runs any hosting CLI and creates nothing on any host.

**Defect this work addresses** (found 2026-11-08 on the operator's phone):
opening the Sessions page on the phone showed session cards without their
placement (system) tag chips — `compute:<node>`, `peer:<name>`,
`backend:<llm>`, `lane:<lane>`, `role:<role>`, `project:<name>` — on the
first load; the chips only appeared later, once a full session record for
that row had arrived.

## Affected area

The defect sits in the first-load data path of the session list, which every
operator-facing client shares through one REST contract. The Sessions page
load is one paged call — `GET /api/sessions?view=summary` — whose rows feed
the card drawing directly; before this work the summary row shape
(`sessionSummaryRow`, `internal/server/session_list_page.go`) carried the
session's own `tags` and its Automaton-inherited `inherited_tags` but dropped
`system_tags`, so the machine-derived placement chips were structurally
absent on first paint. The fix lands on the server in that one row shape and
inherits everywhere the server builds a row from it — the paged summary
list, the windowed session frames (the `Hub.summaryRow` path that feeds
`session_state`), the legacy full list record, and the bounded connect-time
frame — and `internal/server/session_summary_row_system_tags_test.go` pins
the field on every one of those shapes so it can no longer silently drop
out of any of them. On the PWA the chips are drawn from whatever a row
carries, so the server-side fix alone closes the visible gap: the placement
chips now sit beside the own-tag chips and the dashed inherited-tag chips
on the first draw, and the group-by-placement-tag control sees them
immediately as well.

Operator guidance for the surrounding surface lives in
[docs/howto/automata-tags.md](../../howto/automata-tags.md) (placement
tags, chips on the session card),
[docs/howto/list-endpoints-and-paging.md](../../howto/list-endpoints-and-paging.md)
(summary row contents), the *Placement axes* entry in
[docs/datawatch-definitions.md](../../datawatch-definitions.md), and the
session-list section of [docs/operations.md](../../operations.md).

## Current behavior (what this work delivers on the server and PWA)

The defect is closed on the datawatch side: every summary row the server
emits now carries the session's `system_tags` (`omitempty`, so a session
without placement tags serializes exactly as before), the PWA draws those
chips on the first load without waiting for full records, and the
windowed/session_state row path — the one that pushes live rows to open
clients — carries them too, so a row arriving over WebSocket while the page
is open shows its chips. No setting changed, no migration, and the field is
purely additive: any client that predates it and does not read
`system_tags` from a summary row continues to work, it just renders that
row without the placement chips.

The gap this leaves is on the operator-facing mobile clients, and it is
smaller than the earlier placement-tags work suggested: the whole
settings/filter/sweep surface of that work is already tracked under the
existing datawatch-app issue for the tag-and-placement surface
(`dmz006/datawatch-app#394`). What that issue predates is this fact — that
the summary rows of `GET /api/sessions?view=summary` now carry
`system_tags`, so any sessions screen that paints its cards from summary
rows can draw the placement chips at first load with no per-session
follow-up fetch, and that any path that pushes windowed rows or
`session_state` now ships the same field. A sessions screen that builds its
cards from full session records is unaffected on the wire; a sessions
screen that pages the summary view and does not read `system_tags` either
way misses the chips until (and unless) it fetches each full record — which
is exactly the gap the operator saw on the phone.

## Expected behavior

Once parity is met, the app's sessions screen on both Android and iPhone
does what the PWA does: it draws each session's placement tags as
recognizably distinct chips (the PWA's form is a solid green chip with a ⚙️
mark, beside normal own-tag chips and dashed inherited-tag chips) on the
first render of the list, without a fetch of every full record, and it keeps
them correct when rows update in place. If the app pages
`GET /api/sessions?view=summary` — the same contract the PWA uses — it does
that by reading `system_tags` from each row. If it lists sessions from
full records on both platforms instead, the acceptance bar is simply that
a row whose payload now carries an additive `system_tags` array renders
without drop or misrender, and that placement tags remain filterable/grouped
the way the tag-and-placement issue already requires. Capability parity is
required; implementation parity is not — each client may deliver the same
chips-first-load result with its idiomatic list architecture.

## Suggested next step

The relevant issue already exists — `dmz006/datawatch-app#394` covers the
placement-tags surface for the Compose app — so per the project's
mobile-parity rule the action is a follow-up comment on that issue, not a
new one (unless it is already closed, in which case file one
`fix(sessions):`-prefixed issue that references #394 as the parent): the
operator-visible change (placement chips on every Sessions card at first
load, found on the phone), the contract fact behind it (summary rows of
`GET /api/sessions?view=summary`, and the windowed/session_state rows built
on the same shape, now carry `system_tags`), and the acceptance criteria —
a seeded page of summary rows, some with `system_tags` and some without,
must render the placement chips beside the own and inherited chips on first
draw on Android and iPhone, with the rows without the field rendering
exactly as they did before. Point the comment at this draft and the two
howtos above for wording: the PWA wording is "placement (system) tags …
drawn as chips on the first draw of the page, so the chips never wait for
the full record to load." Nothing here introduces a new endpoint, a new
setting, a new locale key, or a renamed field — the field is additive and
already localized-agnostic — so the issue is a display pass-through, not an
integration story.

## Related draft

[2026-11-08-sessions-tag-chips-drawing-discipline-parity.md](./2026-11-08-sessions-tag-chips-drawing-discipline-parity.md)
tracks the adjacent drawing-discipline gap found on the same find (chip
drawing behavior on the operator's phone); this draft covers only what the
first-load data path delivers or leaves for each surface.

## Parity surface

| Surface | Decision | Why |
|---------|----------|-----|
| REST | included | The contract change itself: `system_tags` is now a field on summary rows of `GET /api/sessions?view=summary` and on the windowed rows/session_state built from the same row shape. Additive and `omitempty`; no endpoint, method, or existing field changed. |
| MCP | included, unchanged | No MCP tool wraps the sessions summary list; the session tag inventory tool (`session_tag_list`) already existed from the placement-tags work. Nothing newly exposed. |
| CLI | included, unchanged | `datawatch sessions` reads the same list endpoint and already renders what a session carries; no separate CLI surface is touched by this fix. |
| comm channel | included, unchanged | The comm session-listing verbs hit the same endpoint; the additive field passes through without a comm-side change. |
| YAML/config | not a target | No configuration is involved — placement tags are on by default already and this work changes only what a list row ships, not how tags are derived. |
| PWA | included | The Sessions page draws every session's placement chips on the first load now; the group-by-placement-tag control sees them immediately. This is the fix the operator found on the phone. |
| Android | excluded (no code in this repo) | Capability parity is required and is tracked by this draft and by #394: placement chips on the sessions cards at first render and a clean pass-through of the additive `system_tags` field — implementation parity is not. |
| iPhone/iOS | excluded (no code in this repo) | Same as Android: the same two items are tracked here and under #394 for the native client. |
