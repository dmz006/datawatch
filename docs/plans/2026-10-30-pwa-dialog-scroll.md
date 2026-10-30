# PWA dialogs: cap to the viewport and scroll the body

**Date:** 2026-10-30
**Version:** ships in the next patch above main (a fix)
**Tracker:** row 270 in `docs/plans/README.md`; PRD `3dbf0001`

Confirmed: yes — operator report, 2026-10-30, phone PWA, quoted: "I opened the github ql test quotation and i couldn't scroll it so i couldn't accept nor cancel, the buttons were off the screen and couldn't scroll".

## Problem

The shared PWA dialog (`showModal`) had no height limit and no scrolling area. A long body, such as a long question in the answer dialog, made the dialog taller than a phone screen, so the Cancel and submit buttons sat below the visible area and nothing could be scrolled to reach them.

## Rule used

- Cap the dialog to the dynamic viewport height (a `vh` fallback, `dvh` where supported), keeping the overlay's 12px padding.
- The dialog is a flex column: the title and the button row are fixed, only the body scrolls.
- Keyboard aware: the dialog follows `window.visualViewport`, so the buttons stay reachable with the on-screen keyboard open.
- A short dialog keeps its content-sized height and looks as before.

## Scope

- `internal/server/web/app.js` — `showModal` builds the dialog with classes `confirm-modal generic-modal` and tracks the visual viewport.
- `internal/server/web/style.css` — the `.generic-modal` height cap and flex column, and the scrolling `.generic-modal-body`.
- `internal/server/web/app-modal-scroll.test.js` — Node tests: markup, CSS contract, a 360 by 640 layout model built from the real CSS, the visualViewport keyboard path, the short-dialog regression.
- `CHANGELOG.md`, `docs/testing-tracker.md`, this document and the row 270 status in `docs/plans/README.md`.

No new locale keys were added: the dialog's own text is unchanged.

## Phases

| Phase | What | Status |
|---|---|---|
| 1 | Cap `showModal` to the visible height, scroll only the body, keep title and buttons fixed | Done (PRD `3dbf0001`) |
| 2 | Keyboard awareness through `window.visualViewport` | Done (PRD `3dbf0001`) |
| 3 | Node tests: markup, CSS contract, 360 by 640 layout model, keyboard path, short-dialog regression | Done (PRD `3dbf0001`) |
| 4 | Docs: changelog, testing tracker section, this plan, registry row | Done (PRD `3dbf0001`) |
| 5 | Live look on a phone-width browser with a long question and the keyboard open | Pending (tracker Validated stays No until then) |
| 6 | datawatch-app parity issue | Pending — filed by the supervisor at merge |

## Collision note

The draft question-clarity PRD `71110001` changes the question dialog's content (`_questionAnswerBodyHTML`). This fix only changes the `showModal` wrapper and its CSS, so the two touch different code and can land in either order.

## Parity surface

| Channel | Intent | Reason |
|---|---|---|
| REST | Excluded | No API change; a client-side layout fix. |
| MCP | Excluded | No tool change; a client-side layout fix. |
| CLI | Excluded | No command change; a client-side layout fix. |
| comm channel | Excluded | No command or message change; a client-side layout fix. |
| YAML/config | Excluded | No config key added or changed; a client-side layout fix. |
| PWA | The fix | `showModal` and its CSS, as above. |
| Android | Issue required | The app's own dialogs must be checked for the same overflow. |
| iPhone | Issue required | The app's own dialogs must be checked for the same overflow. |

### Parity issue (datawatch-app), ready to paste

This task did not file it. The supervisor files it at merge.

**Title:** `fix(dialog): cap dialogs to the viewport and scroll the body so buttons stay reachable`

**Body:**

The datawatch PWA fixed a dialog that could not be answered on a phone. Operator report, 2026-10-30, quoted: "I opened the github ql test quotation and i couldn't scroll it so i couldn't accept nor cancel, the buttons were off the screen and couldn't scroll". A long question made the dialog taller than the screen and its Cancel and submit buttons were off screen with nothing to scroll.

The rule the PWA now uses, for the app to check against its own dialogs on Android and iPhone:

- Cap the dialog to the dynamic viewport height, keeping the margin around it.
- The title and the button row stay fixed and visible; only the body scrolls.
- Keyboard aware: when the on-screen keyboard is open, the dialog fits the remaining visible height so the buttons stay reachable.
- A short dialog keeps its content-sized height.

Please open the app's dialogs that can hold long content (the question answer dialog first) with a long body and with the keyboard open, and confirm the buttons stay reachable. This is a capability-parity issue: no new API, config or command is involved, and no new locale keys were added.
