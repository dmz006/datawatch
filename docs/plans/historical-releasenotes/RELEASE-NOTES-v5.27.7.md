# datawatch v5.27.7 — release notes

**Date:** 2026-04-30
**Patch.** Three operator-asked items: PWA UI parity with the Android shell, config-driven quick commands, and channel.js bridge memory tools.

## What's new

### 1. PWA UI parity (BL208, datawatch#26 + #27)

Two pure-CSS changes mirroring the Android shell, no JS state changes:

- **Running badge pulse** — the state badge alpha cycles 0.55 → 1.0 every 700 ms while a session is in `running`. CSS `@keyframes dw-running-pulse`, `prefers-reduced-motion` respected. Mirrors `SessionInfoBar` from the Android v0.44.0+ baseline.
- **3-dot generating indicator** — appears below the terminal output while the session is in `running`. Each dot fades in/out over 600 ms with a 200 ms stagger between dots, producing a "breathing" wave effect. Disappears as soon as the session leaves `running`. Matches Compose `GeneratingIndicator`.
- **Scroll-mode icon** swapped `↕` → `📜` (U+1F4DC) to match `TerminalToolbarControls`.

JS hook: `refreshGeneratingIndicator(sessionId)` is called from the same updateSession/onSessionsUpdated paths as `refreshNeedsInputBanner`, so the dots appear/disappear in sync with live state without a re-render.

### 2. Config-driven quick commands (BL209, datawatch#28)

New `GET /api/quick_commands` endpoint serves the list of "system" buttons (`yes` / `no` / `continue` / `skip` / `/exit` + `Esc` / `Tab` / `Enter` / arrows / `PgUp` / `PgDn` / `Ctrl-b`) as JSON. Operator can override the whole list by editing `session.quick_commands` in YAML or `datawatch config set` — change applies on next `datawatch reload`.

Response shape:

```json
{
  "commands": [
    {"label":"yes",    "value":"yes\n",       "category":"system"},
    {"label":"Esc",    "value":"key:Escape",  "category":"keys"},
    ...
  ],
  "source": "default" | "config"
}
```

`value` prefix `key:` flags a tmux-keystroke shortcut (the client decides whether to use `send_input` or `sendkey` transport based on the prefix). The `category` field is a grouping hint for clients that section the panel.

PWA + Android migration off the hardcoded lists tracked at [datawatch-app#31](https://github.com/dmz006/datawatch-app/issues/31). v5.27.7 ships only the server side; the PWA continues hardcoding for now (separate v5.27.8+ work item).

### 3. channel.js bridge memory tools (BL212, datawatch#29)

The `datawatch-channel` Go bridge spawned per claude-code session now exposes the parent's memory subsystem as MCP tools:

| Tool | Purpose | Forwards to |
|---|---|---|
| `memory_remember` | Save text to episodic memory | `POST /api/memory/save` |
| `memory_recall` | Semantic search | `GET /api/memory/search?q=…` |
| `memory_list` | List recent memories | `GET /api/memory/list?n=…` |
| `memory_forget` | Delete by ID | `POST /api/memory/delete` |
| `memory_stats` | Backend status + counts | `GET /api/memory/stats` |

Pre-v5.27.7 the bridge only exposed `reply` — claude-code sessions had to `curl https://localhost:8443/api/memory/...` to use memory at all. Operator confirmed: 36 stored entries were inaccessible via MCP tooling that AGENT.md documents as available.

New `bridge.callParent(ctx, method, path, body)` helper supports either GET or POST + reads the response body (vs the existing fire-and-forget `postToParent`). New `urlQueryEscape` keeps the bridge stdlib-only (no `net/url` import).

The legacy `internal/channel/embed/channel.js` (Node bridge — superseded by the Go bridge per BL288 v5.4.0) is **not** updated in this release; operators on the JS bridge are expected to migrate. AGENT.md "Container maintenance" rule covers eventual removal.

## What's deferred

**BL208 #30 (PRD card style alignment + redundant "PRDs" header)** — bigger restyle of `renderPRDRow` to harmonise with Sessions card style. Deferred to **v5.27.8**. The two cosmetic items in #26/#27 are CSS-only; #30 is a structural refactor of the PRD list view.

## Tests

```
Go build:  Success (via `make build` + `make cross`)
Go test:   1519 passed in 58 packages (+11 new)
Smoke:     run after install — §7x added for the new endpoint
```

New tests:
- `internal/server/v5277_quick_commands_test.go` — 4 cases (default / override / 405 / key-prefix invariant).
- `cmd/datawatch-channel/v5277_memory_tools_test.go` — 7 cases (each tool's HTTP forwarding shape + empty-text rejection + `urlQueryEscape` matrix).

## datawatch-app sync

Mobile mirror tracked at [datawatch-app#31](https://github.com/dmz006/datawatch-app/issues/31) (drop hardcoded quick commands once parent ships) — now actionable.

## Backwards compatibility

- All changes additive. Older clients that don't fetch `/api/quick_commands` keep working with their hardcoded lists.
- Bridge gains 5 new tools — old bridges with only `reply` keep working; new tools degrade silently when the parent daemon isn't running (the existing 5-second client timeout becomes a 30-second timeout for memory ops since embedding can be slow).

## Upgrade path

```bash
git pull
datawatch update && datawatch restart
# Hard-reload the PWA (cache name → datawatch-v5-27-7).
# claude-code sessions automatically pick up the new bridge on next launch.
```

No data migration. No new schema.
