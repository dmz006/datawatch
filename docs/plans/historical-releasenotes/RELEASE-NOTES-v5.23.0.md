# datawatch v5.23.0 — release notes

**Date:** 2026-04-26
**Spans:** v5.22.0 → v5.23.0
**Closed:** Asset retention rule + 4 operator-reported PWA bugs

## Why this release

Operator audit-followups stacked up during v5.22.0 release prep:

1. **Settings → Comms bind interface fields broken** — interface_select rendered nothing (treated entries as strings, but `state._interfaces` items are `{addr, label, name}` objects), AND was single-select where operators want multi-bind.
2. **Session-detail channel mode-badge is redundant** — the Channel tab already conveys mode; the inline pill duplicates the signal.
3. **Response button text is noise** — should be icon-only between commands + arrows.
4. **Asset retention** — non-major release binaries + container images add up; only majors should retain assets indefinitely.

## What's new

### PWA — Settings → Comms bind interface multi-select with auto-protect

The comms-config branch of the renderer (`loadCommsConfig`) had a bug: it was treating `state._interfaces` items as strings (`escHtml(iface)`) but the API returns objects with `addr`/`label`/`name` fields. Result: empty `<option>` rows with `[object Object]` values — operators saw an empty dropdown.

The general-config branch already had the correct multi-select with checkbox-list + connected-interface protection. The fix is to mirror that pattern in the comms branch:

- Renders one checkbox per interface with `iface.label` (e.g. `"192.168.1.50 (eth0)"`)
- Auto-protects the connected interface: if the operator unchecks "All interfaces" and removes the one they're currently connected through, `saveInterfaceField` re-checks it before persisting (prevents self-disconnect)
- Connected interface gets a `(connected — auto-protected)` badge

Affected fields: `server.host` + `mcp.sse_host` (both in `COMMS_CONFIG_FIELDS`).

### PWA — session-detail channel mode-badge dropped

Operator: *"In a session, the channel badge isn't needed, you can see the channel tab."*

The `<span class="mode-badge mode-channel">channel</span>` inline pill near the top of session detail is redundant when the session has a Channel/ACP output tab. v5.23.0 only renders the mode-badge for `tmux` mode (where there's no tab system to convey the mode).

### PWA — Response button icon-only

Operator: *"Response in tmux bar should just be icon in between saved commands and arrows, not the text word response."*

The 📄 glyph alone (with the existing title tooltip "View last response") replaces "📄 Response" in both render sites. Saves space in the saved-commands flex row, fits the v5.22.0 layout fix that put arrows on the right.

### Release rule — asset retention (AGENT.md)

Codified the operator's directive in `AGENT.md § Release-discipline rules`:

> **Asset retention.** To keep the GH releases page navigable + GHCR storage bounded, only **major releases** (X.0.0) retain binary attachments + container images indefinitely. Past **minor + patch** releases (X.Y.0 + X.Y.Z) get their assets deleted on the next subsequent release. The release notes themselves stay forever — only the binary blobs + container tags are pruned.

Plus committed the helper script as `scripts/delete-past-minor-assets.sh` so the next release can re-run it idempotently.

### Release rule — embedded docs current at build time (AGENT.md)

Operator: *"If docs are shipped with the binary they should be up to date, make sure that is part of release rules."*

Added to `AGENT.md § Release-discipline rules`:

> **Embedded docs must be current at binary build time.** The embedded PWA docs viewer reads from `internal/server/web/docs/` which is mirrored from the canonical `docs/` tree by `make sync-docs`. The Makefile's `build` and `cross` targets depend on `sync-docs` so this is automatic for `make cross` / `make build`. The release rule: never `go build ./cmd/datawatch/` directly when preparing a release binary — always go through `make cross` (cross-arch) or `make build` (host-arch).

This was already the de-facto behavior (the Makefile dependency chain enforces it), but the rule makes it explicit per the audit.

### Asset cleanup execution

Ran `scripts/delete-past-minor-assets.sh` to delete binary attachments from 105 past-minor releases (v1.1.0 → v5.21.0, sparing v5.22.0 to keep the immediate-prior upgrade path working). Major releases (v1.0.0, v2.0.0, v3.0.0, v4.0.0, v5.0.0) keep their binaries forever.

GHCR container images for past-minor releases need a separate pass with `read:packages + delete:packages` token scope; deferred to a follow-up patch.

## Operator's question — config-save behavior

Operator: *"Does saving a configuration cause the entire service to restart or just reload the configs into the running service? Make sure it's the most efficient."*

Already efficient. Three paths:

| Trigger | Behavior |
|---------|----------|
| `datawatch config set foo.bar baz` (CLI) | PUT /api/config → `applyConfigPatch` → mutates `cfg.*` in-memory + persists to YAML + hot-applies session knobs (BL41 SetScheduleSettleMs, SetDefaultEffort). NO restart. |
| PWA Settings field change | Same path. NO restart. |
| `datawatch reload` (v5.7.0 CLI) / SIGHUP / POST /api/reload | Re-reads `~/.datawatch/config.yaml` + re-wires messaging/LLM/observer backends. NO restart. |
| `datawatch restart` | Full SIGTERM + relaunch. tmux sessions preserved. |
| `auto-restart on config save` toggle (PWA Settings → General) | When ON: ONLY triggers `datawatch restart` if the changed key is in the `RESTART_FIELDS` set (port, log path, encryption mode — keys that genuinely can't hot-reload). Most config changes still skip the restart. |

So config save is the most efficient path it can be: in-memory + on-disk update only. Only the small set of keys that actually require process restart (which would be unsafe to hot-reload — TLS cert change while serving, port rebind, etc.) trigger one when the operator opts in.

## Known follow-ups (deferred to future patches)

- **Autonomous tab auto-refresh on changes** (operator-reported v5.22.0). Needs WS broadcast plumbing on every Manager.SavePRD path.
- **Diagrams page restructure** — drop `/plans` exposure, add app-docs + howtos to the viewer.
- **Design doc audit / refresh** — operator: *"Design document has not been updated in a long time"*. Sweep needed across `docs/design.md` + `docs/architecture.md` + `docs/architecture-overview.md`.
- **Every Settings card section gets a docs chip; complex settings get a howto** — backlog item per AGENT.md docs-rule expansion.
- **datawatch-app#10 catch-up issue** for v5.3.0 → v5.23.0 PWA changes.
- **Container parent-full retag** for v5.x.
- **gosec HIGH-severity review.**
- **GHCR container image cleanup** for past-minor versions (needs read:packages + delete:packages token).

## Tests

1386 passed in 58 packages (no Go changes; PWA + AGENT.md only). `make cross` built all 5 binaries.

## Upgrade path

```bash
datawatch update                            # check + install
datawatch restart                           # apply
```
