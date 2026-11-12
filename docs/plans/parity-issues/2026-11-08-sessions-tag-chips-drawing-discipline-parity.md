# [sync] Sessions page: system (placement) tag chips drawn at first load, registry-independent, held across partial pushes — display-discipline parity (datawatch app)

**Filed 2026-11-08 as https://github.com/dmz006/datawatch-app/issues/402 (release v9.112.8).** Original note: — this is the tracked draft; the GitHub issue (or
follow-up comment) is opened when the operator files it. Filing the issue
does not change a line of this repo. This draft is a file write only: it
never runs any hosting CLI (no `gh`, no issue creation) and creates nothing
on any host.

**Defect this work addresses** (found 2026-11-08 on the operator's phone):
opening the Sessions page showed session cards without their system
(placement) tag chips until the tag registry had finished loading (and, for
some rows, until a later full-record push carried them), so the chips
appeared to "pop in" after first load instead of being present from the
first frame.

## Affected area

The defect sits in the PWA's **drawing discipline** for the Sessions
page's session cards (`internal/server/web/app.js`), specifically how the
chip row for a session is computed and how held session records absorb
live `session_state` pushes. Three behaviors define the discipline after
this work, all pinned by
`internal/server/web/app-session-tags-first-load.test.js`:

1. **Chips first frame, registry-independent.** The placement (system)
   chips — the solid green ⚙️ chips for `compute:<node>`, `peer:<name>`,
   `backend:<llm>`, `lane:<lane>`, `role:<role>`, `project:<name>` — are
   drawn directly from the tag-name strings a row already carries in
   `system_tags`. They do not wait for the tag registry
   (`GET /api/autonomous/tags`) to arrive, unlike the operator own-tag and
   inherited-tag chips, which are registry IDs and necessarily name-resolve
   once the registry is loaded. A page of sessions with placement tags but
   no registry loaded renders the placement chips in its first frame, and
   the page's registry request is still made exactly once — the draw does
   not multiply or pre-empt fetches.
2. **Registry arrival is additive.** When the registry does arrive, each
   card keeps the system chips and adds the resolved own and inherited
   chips beside them; no chip is dropped or re-ordered by the repaint.
3. **Held records keep their placement tags under partial pushes.** A
   `session_state` push whose payload merely *omits* `system_tags` (an
   older or thinner row shape) does not erase the chips the page already
   paints: the held record retains the tags it had. Only an *explicit*
   empty `system_tags` array on the same session removes them. Sibling
   sessions are untouched. The sessions-frame merge path
   (`_mergeSessionFields`) encodes the same rule — no-key-loses,
   empty-array-wins — so a summary row from an older server shape cannot
   strip placement chips out of a live card either.

The companion server-side change — making every summary row the server
emits actually carry `system_tags` — is tracked by the sibling draft
[2026-11-08-sessions-system-tags-chips-first-load-parity.md](2026-11-08-sessions-system-tags-chips-first-load-parity.md).
This draft does not depend on it to close: the PWA discipline works
whenever a row carries the field, in any of the row shapes (summary,
windowed, full), from any server version that sends it.

Operator guidance for the surrounding surface lives in
[docs/howto/automata-tags.md](../../howto/automata-tags.md) (placement
tags, chips on the session card), the *Placement axes* entry in
[docs/datawatch-definitions.md](../../datawatch-definitions.md), and the
session-list section of [docs/operations.md](../../operations.md).

## Current behavior (what this work delivers on the PWA)

The defect is closed on the PWA side: a session card on the Sessions page
now shows its placement (system) tag chips from the very first draw,
regardless of whether the tag registry has loaded; the registry paint that
follows is purely additive (own and inherited chips appear, system chips
stay); and the page's held records are immune to tag-stripping by partial
`session_state` pushes, losing chips only when the server explicitly
clears them. No API contract changed with this work, no setting, no
migration, no new locale key — the visible behavior comes from how the
existing fields are drawn and held.

The gap this leaves is a **display-discipline gap on the operator-facing
mobile clients**. It is not about the wire: if an Android or iPhone
sessions screen builds its cards from rows that carry `system_tags` and
resolves chips the same way — placement chips from the row's own
`system_tags` strings without a registry round-trip, operator chips after
the registry — the gap is already invisible there. The gap is any client
behavior that (a) defers drawing placement chips until a tag registry (or
any second per-session fetch) has arrived, so the chips pop in after
first paint; or (b) treats a partial live push that omits `system_tags`
as "the session has no tags now" and clears chips the server never
cleared. Neither of those behaviors needs a server change to reproduce or
to test, and neither is visible to the datawatch server.

## Expected behavior

Once parity is met, a sessions screen on Android and iPhone meets the same
display discipline as the PWA:

1. A seeded page of session rows — some with `system_tags`, some without,
   some with `tags`/`inherited_tags` — renders the placement chips for the
   tagged rows **on the first frame**, with the tag registry still
   un-fetched or still in flight; when (and only when) the registry
   resolves, the own and inherited chips appear beside the kept placement
   chips.
2. A live state push for a session whose payload omits `system_tags` does
   not remove the placement chips that were drawn; an explicit empty
   `system_tags` array does remove them; other sessions are unaffected.
3. At minimum, when the client cannot adopt (1) or (2) verbatim, it must
   not misrender: chips must not flicker off-and-on on a registry reload,
   must not disappear on a partial push, and a row *without* the field
   must render exactly as it did before.

Capability parity is required; implementation parity is not — each client
may reach "chips present at first frame, chips stable under partial
pushes" with its own list architecture, its own registry-caching
strategy, and its own row-merge rule. Nothing here requires a new
endpoint, setting, or field: the field and the REST/WS contracts are
unchanged by this work.

## Suggested next step

Confirm against the existing datawatch-app tracker first: the
tag-and-placement surface for the native clients is already covered by the
issue this family of drafts points at — `dmz006/datawatch-app#394` (the
placement-tags surface, referenced by the sibling draft) — so per the
project's mobile-parity rule the default action is a **follow-up comment**
on that issue rather than a new one, adding this display-discipline item:
the defect (placement chips absent at first load, popping in after the
tag/registry round-trip or a later full push; found 2026-11-08 on the
phone), the two acceptance behaviors above (chips drawn from the row's own
`system_tags` at first frame with the registry still pending; partial push
that omits `system_tags` never strips chips, an explicit empty array
does), and a pointer at this draft for wording. If that issue is closed,
file one `fix(sessions):`-prefixed issue carrying the same body and
referencing #394 as the parent. If the native sessions screens already
satisfy both behaviors, the resolution is a one-line "already met" comment
on the issue, and this draft is closed with that comment as its evidence.
Either way the work on the app side is display logic and row-merge
discipline — a render-and-merge pass-through story, not an integration
story, and there is nothing in this draft that requires datawatch to ship
anything further.

## Parity surface

| Surface | Decision | Why |
|---------|----------|-----|
| REST | included, unchanged | The contracts already carry `system_tags` on the row shapes that matter (summary rows per the sibling draft, full records, windowed frames); this work adds no field, endpoint, or method — it only changes how a client may *use* the existing field at draw time. |
| MCP | included, unchanged | No tool is touched; the session tag inventory tool already existed from the placement-tags work. |
| CLI | included, unchanged | `datawatch sessions` reads the same rows; list text output is unaffected by a client-side draw discipline. |
| comm channel | included, unchanged | The comm session-listing verbs hit the same endpoints; nothing new flows over the channel. |
| YAML/config | not a target | No configuration is involved — the chips, their ordering, and the hold-on-partial-push rule are display behavior, not a setting; there is intentionally nothing to configure. |
| PWA | included | The Sessions page draws every session's system (placement) chips on the first load, registry-independent, and holds them across partial `session_state` pushes — the exact behavior the operator found missing on the phone. This draft is the PWA-side record of it. |
| Android | excluded (no code in this repo) | Capability parity is required and is tracked by this draft via #394: chips first-frame from the row's own `system_tags`, stable under partial pushes — implementation parity is not. |
| iPhone/iOS | excluded (no code in this repo) | Same as Android: the same two display-discipline items are tracked here for the native client. |
