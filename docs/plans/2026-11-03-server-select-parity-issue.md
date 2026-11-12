# Parity issue draft — server selection behaviour (datawatch-app)

File: **datawatch-app** — one issue covering the server-picker changes below.
FILED: https://github.com/dmz006/datawatch-app/issues/349 (with the 9.75.1 merge).

## Changed UI surfaces

The PWA server picker (per-tab chip bar) changes in three ways:

- Re-selecting the already-active server does nothing.
- When a remote server is unreachable, its error panel gains an explicit "Back to <hostname>" button.
- Every page shows a "Showing: <name>" line (hostname, or "All servers" in all-servers mode).

## Parity surface

Behaviours that `datawatch-app` must mirror, with the expected mobile
equivalent of each:

| Changed behaviour (PWA) | Expected mobile equivalent |
|---|---|
| Re-selecting the active server is a no-op (no refetch, no reset of scroll or filters) | Tapping the already-selected server in the server switcher does nothing: no reload, no navigation, no state reset |
| "Back to <hostname>" button in the unreachable-remote error panel (shown only when the remote cannot be reached and no sessions are loaded) | The same button in the mobile error state for an unreachable remote server, returning to the local daemon in one tap; not a persistent control |
| "Showing: <name>" line on every page | A "Showing: <name>" line in each screen's header or subtitle naming the server whose data is displayed (hostname, or "All servers" in all-servers mode) |

### Surfaces, with include or exclude reason

| Surface | Included | Reason |
|---|---|---|
| PWA | yes | the server chip bar, the page header line and the error panel change |
| REST | no | no route or field changes; the client already reaches a peer through the existing proxy route |
| MCP | no | no tool changes |
| CLI | no | no command changes; the CLI picks a server with `--server` |
| Comm channel | no | no verb changes |
| YAML/config | no | no setting added |
| Android (datawatch-app) | yes | mirror the three behaviours in the table above |
| iPhone | yes | same issue, same behaviours |
| Desktop | yes | server picker and page header |

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
2. With a remote server unreachable, the error state shows "Back to <hostname>";
   using it returns to local data.
3. Every screen shows "Showing: <name>" and it updates when the server
   changes.

## Context / links

- How-to: `docs/howto/multi-servers.md` (Switch between servers).
- PWA code: `internal/server/web`.
