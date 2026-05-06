# Release Notes — v6.4.0 (BL242 Phase 1 — Secrets Manager)

Released: 2026-05-03
GH release: https://github.com/dmz006/datawatch/releases/tag/v6.4.0

## Summary

Minor release delivering **BL242 Phase 1** — centralized AES-256-GCM encrypted secrets manager with full 7-surface parity. First of a five-phase BL242 effort that runs through v6.4.7.

## Added — BL242 Phase 1: Secrets Manager

- **`internal/secrets` package**: `Store` interface + `BuiltinStore` (AES-256-GCM encrypted JSON, auto-generated 32-byte keyfile at `~/.datawatch/secrets.key`). `DATAWATCH_SECRETS_KEY` env-var override for headless deployments.
- **REST**: `GET/POST /api/secrets`, `GET/PUT/DELETE /api/secrets/{name}`, `GET /api/secrets/{name}/exists`. Every `GET /api/secrets/{name}` writes an `action=secret_access` audit entry.
- **MCP**: 5 tools — `secret_list`, `secret_get`, `secret_set`, `secret_delete`, `secret_exists`.
- **CLI**: `datawatch secrets list/get/set/delete` (`--tags`, `--desc` flags on `set`).
- **Comm**: `secrets [list]`, `secrets get <name>` (read-only; write via REST/MCP/CLI only).
- **PWA**: "Secrets" tab in Settings — list with delete buttons, inline create/update form with name/value/tags/description.
- **Locale**: 14 new keys (`settings_tab_secrets`, `secrets_*`) across all 5 bundles (en/de/fr/es/ja).

## Phases following in v6.4.x

- v6.4.1 — KeePass backend
- v6.4.2 — 1Password backend
- v6.4.3 — `${secret:name}` config-ref resolution + spawn-time env injection
- v6.4.4 — `datawatch secrets migrate` one-shot migrator
- v6.4.5 / .6 / .7 — secret scoping + plugin env injection + agent runtime token (BL242 close)

## See also

CHANGELOG.md `[6.4.0]` entry.
