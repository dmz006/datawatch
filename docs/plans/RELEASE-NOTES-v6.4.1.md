# Release Notes — v6.4.1 (BL242 Phase 2 — KeePass backend)

Released: 2026-05-03
GH release: https://github.com/dmz006/datawatch/releases/tag/v6.4.1

## Summary

Patch release delivering **BL242 Phase 2** — KeePass backend for the centralized secrets manager.

## Added

- **`internal/secrets.KeePassStore`** — implements `Store` via `keepassxc-cli` subprocess calls. Secret value stored in KeePass Password field; description in Notes; tags in a `datawatch-tags` custom attribute (comma-separated). Database master password supplied via config or `DATAWATCH_KEEPASS_PASSWORD` env var.
- **`internal/config.SecretsConfig`** — new top-level `secrets:` YAML block with `backend` (`builtin` or `keepass`), `keepass_db`, `keepass_password`, `keepass_binary`, `keepass_group`.
- **Backend selection in `main.go`** — `secrets.backend: keepass` switches to `KeePassStore`; all other values (including empty) use the v6.4.0 `BuiltinStore`.

## See also

CHANGELOG.md `[6.4.1]` entry.
