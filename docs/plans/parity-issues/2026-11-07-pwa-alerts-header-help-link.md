# [sync] PWA Alerts header: the round "?" help link moves onto the list status row (datawatch next patch release)

Filed 2026-11-08 as [datawatch-app#395](https://github.com/dmz006/datawatch-app/issues/395).

Parent release: the datawatch next patch release that fixed the PWA Alerts
header layout. The operator report (with a screenshot from Chrome on
Android) was that the round `?` help link in the top-right of the Alerts
tab mis-rendered in narrow touch viewports. This is a pure PWA layout
change: no REST, MCP, config, or locale-key surface changed, so the app
parity gap is visual only.

## What changed in datawatch

**Affected area:** the header region above the alert list on the PWA's
Alerts tab — specifically the round `?` help link that opens the Alerts
howto (`docs/howto/alerts-and-notifications.md`) in a new tab.

**Current behavior (pre-fix):** the help link lived in its own row above
the list, right-aligned in a dedicated top bar. On narrow touch viewports
(390 px phone width, Chrome on Android) that separate row mis-rendered,
and the link sat outside the loader's meta machinery, so it had no
guaranteed tap target and its markup was independent of the list's status
rendering.

**Expected behavior (post-fix):** the help link now rides the list
loader's meta row — the same row that carries the list's status text on
the left (`Cached` badge when the list came from this browser's storage,
`Last updated N s ago`, a small `Loading…` while a refresh runs) — with
the link at the right edge of that one row. It keeps its existing
destination (the Alerts howto, new tab), tooltip (the `alerts_help_tip`
locale string) and accessible name (`Help`). On touch devices the link
gets a 40 × 40 px minimum tap target via the `.alerts-help-link` rule in
`style.css`. The loader's one-second age ticker advances the age text in
place without re-creating the link node, so the meta row does not repaint
every second. The layout is pinned by the story
`scripts/test-stories/alerts-header-row.sh` (one row, link right of the
age text, no horizontal overflow at a 390 px viewport), and the render is
covered by `internal/server/web/app-alerts-help-meta.test.js`. Because the
PWA is embedded in the daemon binary, serving the new layout takes a
daemon rebuild + restart and a hard refresh of the PWA tab; there is no
migration step.

**Suggested next step for the app:** bring the native Alerts screen's
header in line with the new PWA layout — one status row with the
list's status text (cached/last-updated age) on the left and the `?`
help link at its right edge, opening the Alerts help content, with a
tap target meeting the platform minimum (48 dp on Android, 44 pt on
iOS). If the app already places its help affordance in a separate row
above the list, verify it holds at narrow widths and small font scales;
the PWA's separate row was exactly the shape that mis-rendered, and that
gap is now closed on the PWA side. Nothing to reconcile on the wire: no
endpoint, field, or locale key was added, renamed or removed by this
change.

## Acceptance

- The PWA Alerts tab shows the age text and the round `?` help link on
  one row at a 390 px phone viewport, link to the right of the age text,
  no horizontal overflow (the story above).
- The help link keeps its destination (the Alerts howto), tooltip, and
  accessible name, and keeps a ≥ 40 × 40 px tap target on touch
  devices.
- The one-second age ticker updates the age text without re-creating or
  repainting the help link.
- No locale bundles and no daemon/OpenAPI surface were touched by the
  datawatch change — this is a pure PWA layout fix with a visual parity
  counterpart.

## Filing record

- Filed: — (pending; issue number / URL once opened)
