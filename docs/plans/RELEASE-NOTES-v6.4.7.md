# Release Notes — v6.4.7 (BL242 close — Phases 5a/5b/5c)

Released: 2026-05-03
GH release: https://github.com/dmz006/datawatch/releases/tag/v6.4.7

## Summary

Patch series closing **BL242 — Secrets Manager** with three follow-on phases (5a scoping → 5b plugin env injection → 5c agent runtime token). v6.4.5/.6/.7 collected here; CHANGELOG.md carries the v6.4.7 super-entry. Full BL242 scope is now shipped: encrypted store + KeePass + 1Password + config refs + spawn-time injection + per-secret scoping + plugin env declarations + per-agent runtime tokens.

## Phase 5a (v6.4.5) — Secret scoping

- `Scopes []string` field on `Secret` (e.g., `agent:ci-runner`, `plugin:gh-hooks`).
- `CallerCtx` carried on every Get; `CheckScope(secret, ctx)` enforces.
- 7-surface parity (REST/MCP/CLI/comm/PWA/locale + YAML).

## Phase 5b (v6.4.6) — Plugin env injection

- Plugins declare env vars in `manifest.yaml` `env:` block using `${secret:name}` refs.
- Resolved at invoke time; scope-enforced (plugin must be on the secret's scope list).

## Phase 5c (v6.4.7) — Agent runtime secret access

- **Per-agent SecretsToken** — minted at spawn time when the secrets store is wired; delivered in the bootstrap response as `secrets_token` + `secrets_url`.
- **`GET /api/agents/secrets/{name}`** — pre-auth endpoint (like bootstrap) authenticated by the agent's SecretsToken. Returns `{"name":"…","value":"…"}` after scope check. Token revoked on Terminate.
- **`FetchSecret(ctx, name)`** — worker SDK convenience function (reads `DATAWATCH_SECRETS_TOKEN` + `DATAWATCH_SECRETS_URL` set by `ApplyBootstrapEnv`). Returns `ErrSecretsUnavailable` when the parent has no secrets store or is pre-v6.4.7.
- **Recursive child workers** get their own independent token (not inherited from parent) — each `Spawn()` mints fresh credentials.
- Audit log entry written on every successful agent secret fetch (`actor: agent:<profileName>`, `via: agent-secrets-token`).

## See also

CHANGELOG.md `[6.4.7]` entry.
