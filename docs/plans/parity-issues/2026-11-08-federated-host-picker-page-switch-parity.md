# [parity] Choosing a federated host in the server picker should switch the page the operator is on

**Filed 2026-11-08 as https://github.com/dmz006/datawatch-app/issues/403 (release v9.112.9). Original note: not yet filed. No hosting CLI is run from this draft and nothing is created on any tracker.** Record the issue number and link in the Filing record at the end. Sibling precedent: the 2026-11-03 picker-behaviours parity draft (`docs/plans/2026-11-03-server-select-parity-issue.md`, filed as dmz006/datawatch-app#349) already covers re-select-no-op, the "Back to \<hostname\>" button and the "Showing: \<name\>" line; this draft is the follow-on behaviour that landed in the picker on 2026-11-08 — that a chip selection takes effect on the page you are on, not on the page you will next open.

Parent work: **"Make choosing a federated host in the server picker actually switch the page the operator is on"** — found 2026-11-08, reported by the operator (the report arrived truncated in the task title: "Found 2026-11-08, from t…"). Recorded under **[Unreleased] → Fixed** in `CHANGELOG.md`. The fix is present in this source tree: `selectServer` in `internal/server/web/app.js` calls `_rerenderActiveViewForServerSwitch()`, which re-renders the active picker tab in place against the chosen host, and node PWA tests exercise the re-render (`app-select-server-rerender.test.js`, `app-select-server-about-rerender.test.js`, `app-select-server-controls.test.js`). One honest caveat carried into this draft: the changelog entry notes that no live verification had been run when it was recorded, and the operator checklist in `docs/operations.md` ("Still to review (operator)", multi-server section) is still open. Until that checklist passes on a two-host setup, the PWA side of this draft is recorded as *implemented and tested*, not *live-confirmed*.

## Affected area

The per-tab server chip bar — the surface that exists on the PWA (the `selectServer` chip in `internal/server/web/app.js`, plus the `Showing: <name>` line it drives), on datawatch-app's server switcher (Android and iPhone), and on datawatch-desktop's server picker. It is a purely client-side behaviour on every one of those surfaces: choosing a host already routes all subsequent REST through the local daemon's existing proxy (`/api/proxy/<name>/...`) and the WebSocket through the proxied `/ws`; what changed is what the page on screen does when that choice is made.

## What this work closes

On the PWA, before the fix, a chip selection on the picker tabs (Sessions, Alerts, Automata, Observer, Settings and Dashboard) set the active server and reconnected — and the page you were currently viewing did not follow. It kept showing the previous server's data until you navigated away and back. (The Alerts picker behaviour had been fixed earlier, in v8.73.5, 2026-10-08, via the reconnect resync; the changelog's tab list is the operator's report of which pages didn't follow the chip, not a per-tab audit of this tree.) The end state in this tree — what the changelog and the how-to record, and what `_rerenderActiveViewForServerSwitch()` implements against all six picker routes — is:

- Selecting a federated host re-renders the tab you are currently looking at in place, against that host: the `Showing: <name>` line above the view names the host you picked, and the list/content is that host's data. No leaving and re-opening the view.
- Picking the local chip (labelled with the daemon's hostname) or **All** takes the same page back to that view the same way — Observer and Settings are the two tabs whose picker offers no **All** chip at all, so there that selection is simply skipped and the present behaviour stands.
- Re-selecting the already-active chip remains a no-op (no refetch, no scroll or filter reset) — the 2026-11-03 behaviour, unchanged.
- The unreachable-remote error panel and its **Back to \<hostname\>** button behave as before.

## The parity gap this leaves open

- **datawatch-app (Android and iPhone).** If the app's server switcher already changes the daemon the app talks to but leaves the screen on screen showing the previous host's data until the operator leaves and re-opens it, it has exactly the pre-fix PWA shape. The expected equivalent: choosing a host applies to the screen in front of the operator now — that screen's data and its `Showing: <name>` line become the chosen host's — and choosing the local chip or All applies the same way. (The `Showing: <name>` line itself is the row already owed to the app from issue #349; this is the behaviour it must move with.)
- **datawatch-desktop.** The renderer's server picker (`app/renderer/src`) has the same question: does a host selection re-render the current view, or only the next one?

## Expected behavior

On every surface that offers the operator a host picker: a host selection is visible on the page the operator is on, immediately — the current view's data and its `Showing: <name>` line reflect the chosen host without any navigation; the local chip (and **All** where the picker offers it) return the current view the same way; re-selecting the active host changes nothing; and an unreachable host lands in the error state (with its Back-to-local affordance) rather than silently leaving the previous host's data on screen.

## Suggested next step

Low-priority follow-up in `datawatch-app` and `datawatch-desktop`, not a blocker: no wire contract changed, the proxy routes the clients already use, and nothing the apps do today breaks. Concretely: (a) on the app side, check the server switcher — if a host selection only takes effect on the next screen the operator opens, apply it to the current screen and update that screen's `Showing: <name>` line in the same gesture; (b) on the desktop side, same check against its own source; (c) before any of it is filed as confirmed, run the operator checklist in `docs/operations.md` on a two-host setup and record the result here — the PWA side is implemented and unit-tested in this tree, including the **Automata** tab (whose re-render spans the whole tab, so the **Automation** sub-tab follows the chip too), and a pass on the checklist is what turns that into a *confirmed* row for this draft.

## Parity surface

| Surface | Included? | Reason |
|---|---|---|
| PWA | yes | the chip bar and the `Showing: <name>` line change in place; the 2026-11-08 fix is implemented and unit-tested in this tree (live verification pending per the operator checklist) |
| REST | no | no route or field changes; a named host is already reached through the existing `/api/proxy/<name>/...` proxy and proxied WebSocket |
| MCP | no | no tool changes |
| CLI | no | the CLI picks a server per invocation (`--server`); there is no "current page" for it to switch |
| Comm channel | no | no verb changes |
| YAML/config | no | no setting added; the host list and the active-host choice are both client state |
| Android (datawatch-app) | yes | mirror the in-place switch in the server switcher |
| iPhone (datawatch-app) | yes | same issue, same behaviour |
| Desktop (datawatch-desktop) | yes | mirror the in-place switch in the renderer's server picker |

```
PWA:                internal/server/web/app.js (selectServer, _serverPickerBar, renderServerHeaderLine — the per-tab chip bar and the "Showing:" line)
datawatch-app:      server switcher and screen headers
datawatch-desktop:  app/renderer/src (server picker and page header)
```

## How to verify

1. On each of Sessions, Alerts, Automata, Observer, Settings and Dashboard, select a federated host: the tab follows the chip in place — `Showing: <name>` names the picked host and the list is that host's data — with no navigation away and back.
2. Select the local chip, then **All** where it is offered (Observer and Settings have no **All** chip): the current tab returns to that view the same way.
3. Re-select the already-active chip: no network request, no scroll or filter reset.
4. With a remote unreachable, the screen shows the error state and its **Back to \<hostname\>** button — not the previous host's data.
5. Repeat 1–3 on the app's and desktop's current screens, not on a freshly opened one.

## Context / links

- How-to: `docs/howto/multi-servers.md` ("Switch between servers (per-tab picker)").
- Operator checklist: `docs/operations.md` ("Still to review (operator)", multi-server section).
- Changelog: `CHANGELOG.md` [Unreleased] → Fixed.
- PWA code: `internal/server/web/app.js` (`selectServer`, `_serverPickerBar`, `renderServerHeaderLine`).
- Story test: `internal/server/web/app-select-server-rerender.test.js` — runs the re-render, All-skip, no-op and stale-list-reset cases above with `env -u TMUX -u TMUX_TMPDIR node --test`.
- Sibling draft: `docs/plans/2026-11-03-server-select-parity-issue.md` (dmz006/datawatch-app#349).

## Filing record

Not yet filed. No hosting CLI was run and no tracker issue was created on this pass; anyone filing this later records the URL and date here — and, given the caveat in the header, only after the operator checklist has passed on a two-host setup.
