# Parity issue draft — server selection behaviour (datawatch-app)

File: **datawatch-app** — one issue covering the server-picker changes below.
Not yet filed; file it with the merge that ships the PWA change.

## Changed UI surfaces

The PWA server picker (per-tab chip bar) changes in three ways:

- Re-selecting the already-active server does nothing.
- A remote server view gains an explicit "Back to <hostname>" control.
- Every page header shows a "Showing: <hostname>" line.

## Parity surface

Behaviours that `datawatch-app` must mirror, with the expected mobile
equivalent of each:

| Changed behaviour (PWA) | Expected mobile equivalent |
|---|---|
| Re-selecting the active server is a no-op (no refetch, no reset of scroll or filters) | Tapping the already-selected server in the server switcher does nothing: no reload, no navigation, no state reset |
| Explicit "Back to <hostname>" control while a remote server is active | A visible "Back to <hostname>" action on every screen while a remote server is selected, returning to the local daemon in one tap |
| "Showing: <hostname>" line in every page header | A "Showing: <hostname>" line in each screen's header or subtitle naming the server whose data is displayed (local hostname or the selected remote) |

```
PWA:                internal/server/web (server chip bar, page header)
datawatch-app:      server switcher and screen headers
datawatch-desktop:  app/renderer/src  (server picker and page header)
```

Verify against the current source of each before filing; the PWA
implementation in this branch is the reference.

## How to verify

1. Select a remote server, then select it again: no network request fires
   and the view state is unchanged.
2. With a remote server active, the "Back to <hostname>" control is visible;
   using it returns to local data.
3. Every screen shows "Showing: <hostname>" and it updates when the server
   changes.

## Context / links

- How-to: `docs/howto/multi-servers.md` (Switch between servers).
- PWA code: `internal/server/web`.
