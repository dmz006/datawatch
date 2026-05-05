# Release Notes — v6.5.2 (BL243 Phase 2 — headscale pre-auth key generation)

Released: 2026-05-04
GH release: https://github.com/dmz006/datawatch/releases/tag/v6.5.2
Smoke: 91/0/6

## Summary

Patch release completing **BL243 Phase 2** — headscale pre-auth key generation across all 7 surfaces.

## Added

- **`internal/tailscale/client.go`** — `GeneratePreAuthKey()` client method; POSTs to headscale `/api/v1/preauthkey` with reusable, ephemeral, tags, and expiry options.
- **`internal/server/tailscale.go`** — `POST /api/tailscale/auth/key` REST handler; accepts `{"reusable":bool,"ephemeral":bool,"tags":[],"expiry_hours":int}`.
- **`internal/mcp/tailscale.go`** — `tailscale_auth_key` MCP tool with `reusable`/`ephemeral`/`tags`/`expiry_hours` parameters.
- **`cmd/datawatch/cli_tailscale.go`** — `datawatch tailscale auth-key` CLI subcommand with `--reusable`, `--ephemeral`, `--tags`, `--expiry-hours` flags.
- **`internal/router/bl220_comm_commands.go`** — `tailscale auth-key [reusable] [ephemeral]` comm verb.
- **`internal/server/web/app.js`** — "Generate Auth Key" button in Tailscale Mesh Status panel; displays key inline with expiry timestamp.
- **Locale** — `tailscale_generate_key_btn`, `tailscale_generated_key_label`, `tailscale_key_expires` in all 5 bundles (en/de/es/fr/ja).

## See also

CHANGELOG.md `[6.5.2]` entry.
