# datawatch v5.27.2 — release notes

**Date:** 2026-04-29
**Patch.** Two operator-asked items + an install-guide consolidation.

## What's new

### 1. Subsystem-aware hot-reload — `POST /api/reload?subsystem=<name>`

Operator-asked: *"when saving a configuration and auto restart server is enabled it isn't restating the server. explore other ways to reload service and enable services/plugings/etc without havnig to restart the entire server."*

The existing `auto_restart_on_config` toggle only fires for the small `RESTART_FIELDS` set (port bind, TLS cert path, MCP server start) — most config changes hot-apply via `applyConfigPatch` without needing a restart at all. That's by design but isn't obvious from the toggle name. v5.27.2 does two things:

- **Adds a per-subsystem reload entry** so an operator (or a script) can ask the daemon to refresh just one subsystem without taking the whole daemon down. New `Server.RegisterReloader(name, fn)` API lets each subsystem register its own reloader at startup. v5.27.2 ships three named reloaders out of the box:
  - `config` — full config-file re-read + hot-apply (same as bare `/api/reload` and SIGHUP).
  - `filters` — invalidate the FilterEngine's compiled-regex cache so a `datawatch cmd filter add` takes effect immediately.
  - `memory` — refresh the memory adapter's derived caches.

- **Documents the toggle's actual semantics** in `docs/operations.md` so future operators don't hit the same "the toggle does nothing!" surprise — most config changes hot-apply silently, the toggle only matters for the handful of keys that genuinely require a restart.

**Configuration parity — full matrix:**

| Surface | Invocation |
|---|---|
| REST | `POST /api/reload[?subsystem=<name>]` |
| CLI | `datawatch reload [subsystem]` |
| MCP | `reload` tool with optional `subsystem` arg |
| Comm channel | `reload` or `reload <subsystem>` |
| PWA | Settings → General → Auto-restart on config save toggle (existing; behaviour unchanged) |
| YAML | n/a — this is an action, not a configurable knob |

The Server interface gained `RegisterReloader` + `ReloadSubsystem`; the HTTPServer wrapper exposes both; main.go registers the three default reloaders at startup.

### 2. `session.claude_auto_accept_disclaimer` — auto-confirm claude-code's startup prompts

Operator-asked: *"there should be a configuration option in claude llm 'Auto Accept Disclaimer' that will when starting a claude session recognize the warning messages (like we did for PRD automation) and auto accept if enabled."*

claude-code ships two startup prompts that block forward progress until a human types something:

- **"Yes, I trust this folder" / "Quick safety check"** — the numbered menu the CLI shows when entering a project directory it hasn't seen before.
- **"Loading development channels" / "I am using this for local development"** — the disclaimer the `--dangerously-load-development-channels` flag gates behind.

Datawatch already has FilterEngine hooks that detect both via `FilterActionDetectPrompt` (lines 8819-8820 of `cmd/datawatch/main.go`'s seed-filter list). v5.27.2 extends the `DetectPrompt` callback in main.go: when the new `session.claude_auto_accept_disclaimer` flag is true AND the active backend is `claude-code` AND the matched line is one of the two disclaimer patterns, the daemon waits 750 ms (lets the prompt finish painting) and sends the appropriate confirmation key:

- `1\n` — for the numbered "Yes, I trust this folder" menu.
- `\n` — for the "Loading development channels" disclaimer (just press Enter to continue).

Default is `false` so the operator opts in explicitly. When off, the existing trust-prompt review path is unaffected — the operator still sees the prompt + the existing alert filters fire normally.

**Configuration parity — full matrix:**

| Surface | Invocation |
|---|---|
| YAML | `session: { claude_auto_accept_disclaimer: true }` |
| REST | `PUT /api/config` body `{"session.claude_auto_accept_disclaimer": true}` |
| MCP | `config_set` with key `session.claude_auto_accept_disclaimer` |
| CLI | `datawatch config set session.claude_auto_accept_disclaimer true` |
| Comm channel | `configure session.claude_auto_accept_disclaimer true` |
| PWA | Settings → LLM → claude-code → "Auto-accept startup disclaimer" toggle (also surfaces under Settings → General → Sessions for operators who prefer the All-Sessions view) |

The flag is read at runtime from `cfg.Session.ClaudeAutoAcceptDisclaimer`, so a `datawatch reload` picks it up without a restart.

### 3. Install guide consolidation

Operator-asked: *"the main installation guide may need to be merged with the howto installation guide and have the original link to the howto and update howto with anything in the original that isn't in the howto (signal maybe not sure, check and update)."*

`docs/install.md` and `docs/howto/setup-and-install.md` were diverged. The howto had broader coverage (5 install paths vs 2; NFS provisioner; git-credentials patterns; first-coding-session walkthrough). The original install.md held the systemd unit template + cluster-paths reference + `mcp.allow_self_config` self-managing-config note that weren't in the howto.

v5.27.2:
- **`docs/install.md`** is now a thin redirect to the howto with a section pointer table — preserves bookmarks + cross-links.
- **`docs/howto/setup-and-install.md`** absorbs the missing content: systemd unit promotion (multi-user `%i`-template flavour), cluster-paths-datawatch-holds reference table, self-managing-config block with the `mcp.allow_self_config` gate, and a new "Messaging backend setup" section pointing at `comm-channels.md` for per-backend deep dives (Signal / Telegram / Discord / Matrix all via `datawatch setup <name>`).

## Tests

```
Go build:  Success
Go test:   1471 passed in 58 packages (+2 chat-parser tests)
Smoke:     to run after install (see Upgrade path)
```

## datawatch-app sync

- **datawatch-app#22** — mirror Auto-accept disclaimer toggle on the LLM settings screen.
- **datawatch-app#23** — surface subsystem reload (config/filters/memory) on the Settings → Operations card.

## Backwards compatibility

- `Server.RegisterReloader` is additive — backends/plugins that don't register a reloader keep working; `POST /api/reload?subsystem=unknown` returns a 500 with the registered names list, no daemon impact.
- `session.claude_auto_accept_disclaimer` defaults to `false` so behaviour is unchanged for existing deployments.
- `GET /api/memory/stats` shape unchanged.

## Upgrade path

```bash
git pull
datawatch update && datawatch restart
# Hard-reload the PWA. Service worker cache name bumps to
# datawatch-v5-27-2 so the new toggle + reload subsystem support
# show up automatically.
```

No data migration. No new schema. No new endpoints break older clients.
