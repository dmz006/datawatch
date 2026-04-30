# datawatch v5.27.9 — release notes

**Date:** 2026-04-30
**Patch.** BL213 (datawatch#31) — Signal device-linking API completion + BL212 follow-up (datawatch#29) — JS channel fallback memory tool parity.

## What's new

### JS channel fallback memory tool parity (BL212 follow-up, datawatch#29)

v5.27.7 added `memory_remember` / `memory_recall` / `memory_list` / `memory_forget` / `memory_stats` to the Go bridge (`cmd/datawatch-channel`) but left the JS fallback (`internal/channel/embed/channel.js`) at the original `reply`-only surface. Operator caught this on ring-laptop / storage testing instances where `~/.mcp.json` still points at `/usr/bin/node /home/dmz/.datawatch/channel/channel.js` — those sessions were silently missing memory.

v5.27.9 mirrors all five tools into the embedded JS bridge:

- Tool registrations in `ListToolsRequestSchema` match the Go bridge names + descriptions byte-for-byte where possible
- New `callParent(method, path, body)` helper returns the parent's response body so memory_* can plumb JSON back to the model (the legacy fire-and-forget `postToDatawatch` stays for `reply` / `ready` / `permission` paths that don't need the body)
- All five tools forward to `/api/memory/save` / `/search?q=` / `/list?n=&project_dir=` / `/delete` / `/stats` — exactly the paths the Go bridge uses
- HTTP errors surface as MCP errors (`isError: true`), not silent empty results — so the model sees "no parent" vs "no memory matched"

3 new Go tests in `internal/channel/v5279_channeljs_test.go` snapshot the embedded JS at build time:

- `TestChannelJS_ExposesMemoryTools` — every required tool name appears
- `TestChannelJS_MemoryToolsForwardToParent` — every required `/api/memory/*` path appears
- `TestChannelJS_HasCallParentHelper` — both `callParent` and `postToDatawatch` exist (drift between the two would silently break the contract)

### Signal device-linking API completion (BL213, datawatch#31)

The mobile companion's link-device flow expected three endpoints that v5.27.8
either didn't expose or only stubbed:

| Endpoint | Behaviour |
|---|---|
| `GET /api/link/qr` | SSE alias for the existing QR-pair stream — mobile companion expects this name |
| `GET /api/link/status` | Now runs `signal-cli -a <account> listDevices` and returns the parsed device list `[{id, name, created, last_seen}, ...]` |
| `DELETE /api/link/{deviceId}` | Removes a linked secondary device via `signal-cli removeDevice -d <id>` |

`DELETE /api/link/{deviceId}` validates:

- Method must be `DELETE` (others → 405)
- Device id must be numeric (non-numeric → 400)
- Device id 1 (the primary phone) cannot be removed (→ 400)
- `signal.account_number` must be configured (→ 503 if empty)

Parser (`parseListDevicesOutput`) handles signal-cli's text output: blocks
delimited by `Device N:` headers, with `Name:` / `Created:` / `Last seen:`
fields. Conservative — partial blocks are returned with whatever was extracted
rather than dropping the whole device.

## Tests

```
Go build:  Success (via `make build` + `make cross`)
Go test:   1529 passed in 58 packages (7 new in v5279_link_test.go + 3 new in v5279_channeljs_test.go)
Smoke:     run after install
JS check:  node --check internal/channel/embed/channel.js → ok
```

New tests:

- `internal/server/v5279_link_test.go`: `TestParseListDevicesOutput_TwoDevices` / `_Empty` / `_PartialBlock`; `TestHandleLinkUnlink_RejectsNonDelete` / `_RejectsMissingId` / `_RejectsNonNumericId` / `_RejectsPrimaryDevice` / `_RejectsNoAccountConfigured`
- `internal/channel/v5279_channeljs_test.go`: `TestChannelJS_ExposesMemoryTools` / `_MemoryToolsForwardToParent` / `_HasCallParentHelper`

## datawatch-app sync

Mobile companion already calls `/api/link/qr` and `DELETE /api/link/{id}` —
this release closes the parent-side gap. No mobile change needed.

## Backwards compatibility

- `/api/link/status` previously returned a placeholder; now returns real
  device list. Clients that ignored the body keep working.
- `/api/link/qr` is a new alias; the existing `/api/link/stream` SSE endpoint
  is unchanged.
- `DELETE /api/link/{id}` is new; absence of the endpoint in older daemons
  manifests as 404 to mobile companion (which already handles that path).

## Upgrade path

```bash
git pull
datawatch update && datawatch restart
# Hard-reload the PWA (cache name → datawatch-v5-27-9).
```

No data migration. No new schema. No new config keys.
