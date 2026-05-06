# Release Notes — v6.5.3 (BL243 Phase 3 — ACL generator + push, BL243 close)

Released: 2026-05-04
GH release: https://github.com/dmz006/datawatch/releases/tag/v6.5.3
Smoke: 91/0/6

## Summary

Patch release completing **BL243 Phase 3** — ACL policy generator + push with existing-node awareness across all 7 surfaces. Empty-body `POST /api/tailscale/acl/push` now auto-generates from config. Closes BL243 (all three phases shipped).

## Added

- **`internal/tailscale/acl.go`** — `GenerateACLPolicy()` and `GenerateAndPushACL()` on `Client`; generates headscale JSON ACL policy with tag-owner declarations, agent-mesh rules, allowed-peer ingress, and a catch-all preserve rule.
- **`internal/server/tailscale.go`** — `POST /api/tailscale/acl/generate` endpoint (generate without push); `POST /api/tailscale/acl/push` with empty body now auto-generates from config.
- **`internal/mcp/tailscale.go`** — `tailscale_acl_generate` MCP tool.
- **`cmd/datawatch/cli_tailscale.go`** — `datawatch tailscale acl-generate` CLI subcommand.
- **`internal/router/bl220_comm_commands.go`** — `tailscale acl-generate` and `tailscale acl-push` comm verbs (auto-generate variant).
- **`internal/server/web/app.js`** — "Generate ACL" and "Generate & Push ACL" buttons in Tailscale Mesh Status panel; inline policy preview in textarea.
- **Locale** — `tailscale_acl_generate_btn`, `tailscale_acl_push_btn`, `tailscale_acl_generated_label`, `tailscale_acl_pushed_label` in all 5 bundles.
- **Tests** — 4 new ACL generator unit tests (`internal/tailscale/acl_test.go`); 3 new server handler tests; 1709 total.

## See also

CHANGELOG.md `[6.5.3]` entry.
