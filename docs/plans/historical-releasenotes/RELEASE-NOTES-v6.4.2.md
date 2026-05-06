# Release Notes — v6.4.2 (BL242 Phase 3 — 1Password backend)

Released: 2026-05-03
GH release: https://github.com/dmz006/datawatch/releases/tag/v6.4.2

## Summary

Patch release delivering **BL242 Phase 3** — 1Password backend for the centralized secrets manager.

## Added

- **`internal/secrets.OnePasswordStore`** — implements `Store` via the `op` CLI. Secret value stored in the item's Password field; description in Notes (`notesPlain`); tags via 1Password item tags. JSON responses from `op` parsed directly — no text scraping. Timestamps (`created_at`, `updated_at`) populated from op's RFC3339 fields.
- **`internal/config.SecretsConfig`** — three new fields: `op_binary` (default: `"op"`), `op_vault` (optional vault name/ID), `op_token` (service account token; prefer `DATAWATCH_OP_TOKEN` env var).
- **Backend selection in `main.go`** — `secrets.backend: onepassword` switches to `OnePasswordStore`; `keepass` uses `KeePassStore`; all other values use the v6.4.0 `BuiltinStore`.

## See also

CHANGELOG.md `[6.4.2]` entry.
