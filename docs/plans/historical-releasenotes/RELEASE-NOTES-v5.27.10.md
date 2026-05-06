# datawatch v5.27.10 — release notes

**Date:** 2026-04-30
**Patch.** BL216 — MCP channel bridge introspection through every parity surface, plus a BL109 stale-`.mcp.json` fix.

## What's new

### BL216 — MCP channel bridge introspection (full configuration parity)

**Operator question that drove this**: "ring-laptop has `~/.mcp.json` pointing at `node + ~/.datawatch/channel/channel.js`, but the daemon log says it's on the Go bridge. Which one is actually being used? How do I check without grepping logs?"

**Answer surfaced through every parity surface**:

| Surface | How |
|---|---|
| REST | `GET /api/channel/info` → `{kind, path, ready, hint, node_path, node_modules, stale_mcp_json: [...]}` |
| MCP | new `channel_info` tool (forwards to `/api/channel/info` via `proxyJSON`) |
| CLI | `datawatch channel info` (human-readable) + `--json` flag for machine-readable |
| Chat | `channel info` command (multi-line summary suitable for Signal/Telegram) |
| PWA | Settings → Monitor → **MCP channel bridge** panel: kind badge (Go ✓ / JS ⚠) + path + ready state + stale-mcp-json warnings |
| Log | per-session `[channel] session <id> registered with <kind> bridge at <path>` line at register time, complementing the existing once-at-startup `[channel] using native Go bridge` line |

`stale_mcp_json` flags `.mcp.json` files (currently checked: `~/.mcp.json`) whose `datawatch` server entry points at a `channel.js` that no longer exists on disk. The daemon never deletes these automatically — operators may have hand-edited them — but the new `datawatch channel cleanup-stale-mcp-json` (with `--dry-run`) gives a one-command cleanup path for the daemon-written ones.

### BL109 fix — `WriteProjectMCPConfig` prefers the Go bridge

The per-session `.mcp.json` writer (added in BL109 for non-claude-code backends like opencode/gemini that auto-discover via project file) hardcoded `Command: <node>, Args: [<channel.js>]` even after the Go bridge migration in v5.4.0. That left stale `.mcp.json` files behind whenever a session ran with `projectDir = $HOME` (or any other dir): the file pointed at a `channel.js` that the v5.4.0+ daemon never extracted. Fixed by checking `BridgePath()` first — when the Go bridge is the active path, the writer emits `Command: <go-bridge>, Args: []` instead.

### Per-session bridge log line

Operators reading `tail -f ~/.datawatch/daemon.log` now see one line per session register:

```
[channel] session ralfthewise-eac4 registered with go bridge at /home/dmz/.local/bin/datawatch-channel
```

Instead of having to grep `[channel] using native Go bridge` + cross-reference session start times.

## Tests

```
Go build:  Success (via `make build` + `make cross`)
Go test:   1541 passed in 58 packages (+11 new for v5.27.10)
Smoke:     run after install
JS check:  node --check internal/channel/embed/channel.js → ok
```

New tests:

- `internal/channel/v52710_bridge_test.go` (5 tests): `TestBridgeKind_DefaultsToJS` / `_GoWhenHintSet`; `TestWriteProjectMCPConfig_PrefersGoBridge`; `TestIsStaleProjectMCPConfig_StaleJS` / `_LiveJS` / `_GoBridge`
- `internal/server/v52710_channel_info_test.go` (2 tests): `TestHandleChannelInfo_RejectsNonGet` / `_ShapeOK`
- `internal/router/v52710_channel_info_test.go` (3 tests): `TestParse_ChannelInfo` / `_CaseInsensitive` / `_NoMatch`

## datawatch-app sync

[datawatch-app#38](https://github.com/dmz006/datawatch-app/issues/38) tracks the Settings → System → MCP channel mirror.

## Backwards compatibility

- All additive. Older clients that don't know about `channel_info` / `/api/channel/info` keep working.
- `WriteProjectMCPConfig` change: any newly-spawned session writes a Go-bridge-shaped `.mcp.json` on Go-bridge hosts. Already-written stale files are left alone (operator's data) — use `datawatch channel cleanup-stale-mcp-json` to remove them.
- New chat `channel info` command — older comm routers that don't recognise it return the standard "unknown command" reply.

## Upgrade path

```bash
git pull
datawatch update && datawatch restart
# Hard-reload the PWA (cache name → datawatch-v5-27-10).
# Optional cleanup if `datawatch channel info` flags stale entries:
datawatch channel cleanup-stale-mcp-json
```

No data migration. No new schema. No new config keys.
