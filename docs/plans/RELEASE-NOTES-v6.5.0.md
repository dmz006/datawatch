# Release Notes — v6.5.0 (BL243 Phase 1 — Tailscale k8s sidecar)

Released: 2026-05-03
GH release: https://github.com/dmz006/datawatch/releases/tag/v6.5.0
Smoke: 91/0/6

## Summary

Minor release shipping **BL243 Phase 1** — Tailscale k8s sidecar mesh. When `tailscale.enabled=true`, every F10 agent pod spawned by the K8s driver gets a `tailscale` sidecar container injected automatically. The sidecar joins the configured headscale coordinator (or commercial Tailscale) using a pre-auth key from the secrets store (`${secret:name}` supported via BL242 Phase 4). Seven-surface parity: REST + MCP + CLI + comm + PWA + locale (all 5 bundles) + config.

## Added

- **`internal/tailscale/` package** — `Config`, `Client` with `Status()`, `Nodes()`, `PushACL()`. Headscale admin API v1 (`/api/v1/node`, `/api/v1/policy`). Commercial Tailscale stub (coordinator_url empty). `Backend()` method returns `"headscale"` or `"tailscale"`.
- **K8s pod sidecar injection** — `K8sDriver` gains `TailscaleEnabled`, `TailscaleImage`, `TailscaleAuthKey`, `TailscaleLoginServer`, `TailscaleTags` fields. Pod template conditionally renders a second container with `TS_AUTHKEY`, `TS_STATE=mem:`, `TS_LOGIN_SERVER`, `TS_TAGS`, `TS_EXTRA_ARGS`, and `NET_ADMIN`/`SYS_MODULE` capabilities. Default image: `ghcr.io/tailscale/tailscale:latest`.
- **REST**: `GET /api/tailscale/status`, `GET /api/tailscale/nodes`, `POST /api/tailscale/acl/push` (accepts raw HCL/JSON or `{"policy":"…"}` wrapper).
- **MCP**: `tailscale_status`, `tailscale_nodes`, `tailscale_acl_push`.
- **CLI**: `datawatch tailscale status/nodes/acl-push [--file <path>]`.
- **Comm channel**: `tailscale [status]`, `tailscale nodes` — routes to new `handleTailscaleCmd`.
- **PWA**: Tailscale tab in Settings with config form (enabled toggle, coordinator URL, auth/API key inputs, image override) + Mesh Status panel with per-node online/offline indicators and tag badges.
- **Locale**: `settings_tab_tailscale` + 21 tailscale keys in all 5 bundles (en/de/es/fr/ja).
- **Config**: `tailscale.enabled`, `tailscale.coordinator_url`, `tailscale.auth_key`, `tailscale.api_key`, `tailscale.image`, `tailscale.tags`, `tailscale.acl.allowed_peers`, `tailscale.acl.managed_tags`. `auth_key` and `api_key` support `${secret:name}` references.
- **`Manager.K8sDriver()`** — convenience accessor so main.go can wire tailscale config into the driver at startup without a separate setter.
- 11 new tests across `internal/server/bl243_tailscale_test.go` and `internal/agents/bl243_tailscale_sidecar_test.go`.

## Hotfix folded in

JS template literal syntax error broke PWA load entirely (`${secret:name}` inside backtick string in app.js parsed as a template-literal interpolation). Fixed by escaping the literal as `$\{secret:name}` in user-visible string contexts.

## Phases following in v6.5.x

- v6.5.2 — BL243 Phase 2 (OAuth device-flow auth-key generation)
- v6.5.3 — BL243 Phase 3 (ACL generator + push, BL243 close)

## See also

CHANGELOG.md `[6.5.0]` entry.
