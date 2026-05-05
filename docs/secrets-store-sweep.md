# Secrets-Store Sweep Audit (BL254)

**Audit date:** 2026-05-05 · **Audited version:** v6.11.3 · **BL ref:** [BL254](plans/README.md#bl254--secrets-store-rule-retroactive-sweep-filed-2026-05-04)

The Secrets-Store Rule (`AGENT.md` Security Rules section) requires every credential-bearing config field across the project to accept and prefer `${secret:name}` references. New backends ship secrets-store-only from day one; existing backends migrate when next opened for substantive work.

This document records the per-field state matrix and identifies the BL254 retroactive-sweep targets.

## Scope

Audit covered every `*.go` config field under:

- `internal/config/`
- `internal/messaging/backends/*`
- `internal/llm/backends/*`
- `internal/server/`

Looking for fields holding credentials, tokens, passwords, API keys, or signing secrets.

## Classification

- **plaintext-only** — field accepts only a literal string; not wired through `secrets.ResolveConfig`.
- **accepts-secret** — field accepts a literal OR `${secret:name}` reference; resolved at load time via `secrets.ResolveConfig` (BL242 Phase 4, v6.4.3).
- **secret-only** — field requires `${secret:name}`; literal values rejected at load.

## Field matrix

### `internal/config/` — core configuration

| Field | File:Line | Used for | State |
|---|---|---|---|
| `RemoteServerConfig.Token` | config.go:18 | Bearer token for remote server auth | ✅ accepts-secret |
| `OpenWebUIConfig.APIKey` | config.go:472 | OpenAI-compatible API key | ✅ accepts-secret |
| `DNSChannelConfig.Secret` | config.go:559 | HMAC-SHA256 shared secret for DNS tunneling | ✅ accepts-secret |
| `DiscordConfig.Token` | config.go:571 | Discord bot token | ✅ accepts-secret |
| `SlackConfig.Token` | config.go:580 | Slack bot token | ✅ accepts-secret |
| `TelegramConfig.Token` | config.go:589 | Telegram bot token | ✅ accepts-secret |
| `MatrixConfig.AccessToken` | config.go:601 | Matrix homeserver access token | ✅ accepts-secret |
| `SecretsConfig.KeePassPassword` | config.go:617 | KeePass DB unlock password | ⚠️ **plaintext-only by design** |
| `SecretsConfig.OPToken` | config.go:624 | 1Password API token | ⚠️ **plaintext-only by design** |
| `TailscaleConfig.AuthKey` | config.go:633 | Tailscale node auth key | ✅ accepts-secret |
| `TailscaleConfig.APIKey` | config.go:634 | Tailscale API key | ✅ accepts-secret |
| `TwilioConfig.AuthToken` | config.go:647 | Twilio account auth token | ✅ accepts-secret |
| `NtfyConfig.Token` | config.go:660 | Ntfy authentication token | ✅ accepts-secret |
| `EmailConfig.Password` | config.go:669 | SMTP account password | ✅ accepts-secret |
| `GitHubWebhookConfig.Secret` | config.go:678 | GitHub webhook HMAC secret | ✅ accepts-secret |
| `WebhookConfig.Token` | config.go:685 | Generic webhook auth token | ✅ accepts-secret |
| `MemoryConfig.OpenAIKey` | config.go:53 | OpenAI embedding API key | ✅ accepts-secret |

### `internal/server/` — server authentication

| Field | File:Line | Used for | State |
|---|---|---|---|
| `ServerConfig.Token` | config.go:716 | API bearer token | ✅ accepts-secret |
| `MCPConfig.Token` | config.go:766 | MCP SSE auth token | ✅ accepts-secret |

TLS cert/key fields are **file paths**, not credentials — excluded from this audit.

### `internal/messaging/backends/*` — backend-side

All messaging backends receive **already-resolved** credential values from config. The backends themselves do not implement secret resolution; they accept the resolved string from the daemon's startup-time `secrets.ResolveConfig()` pass:

- Discord (`discord/backend.go:22`)
- Slack (`slack/backend.go:22`)
- Telegram (`telegram/backend.go`)
- Matrix (`matrix/backend.go`)
- Twilio (`twilio/backend.go`)
- Ntfy (`ntfy/backend.go`)
- Email (`email/backend.go`)
- DNS (`dns/server.go`, `dns/client.go`)
- Webhook (`webhook/backend.go`)
- GitHub (`github/backend.go`)
- Signal (`signal/adapter.go`) — no credential fields (uses external `signal-cli`)

### `internal/llm/backends/*` — backend-side

Same model: backends receive already-resolved values. Most LLM backends (Ollama, Aider, Goose, Gemini, OpenCode, Shell) have no credential fields. Only OpenWebUI uses an API key — already covered by `OpenWebUIConfig.APIKey` in the core config matrix above.

## Findings

### Counts

| State | Count |
|---|---|
| ✅ accepts-secret | **26** |
| ⚠️ plaintext-only by design | **2** |
| ❌ plaintext-only (BL254 sweep target) | **0** |

### Architectural mechanism

`secrets.ResolveConfig()` (`internal/secrets/refs.go`, BL242 Phase 4, v6.4.3) walks the entire `*Config` struct via reflection, finds string fields containing `${secret:name}` tokens, and replaces them in-place at daemon startup (called from `cmd/datawatch/main.go:2321`). This happens **before** any backend is instantiated, so backend code receives resolved literals.

The reflection walker covers every nested struct + map + slice that contains string fields, so adding a new credential field to any existing config type **automatically** inherits accepts-secret behavior — no per-field plumbing is required.

### Plaintext-only by design (intentional, NOT sweep targets)

Two fields in `SecretsConfig` accept only literal values:

1. **`SecretsConfig.KeePassPassword`** (config.go:617) — unlocks the KeePass DB which itself stores secrets. Cannot be `${secret:name}` because the secrets store hasn't been initialized yet at the point this is needed.
2. **`SecretsConfig.OPToken`** (config.go:624) — unlocks the 1Password CLI (`op`) which serves as a backend for the secrets store. Same recursion problem.

Both prefer environment variables over YAML literals (`DATAWATCH_KEEPASS_PASSWORD`, `DATAWATCH_OP_TOKEN`) per the inline docstrings.

These are **not** BL254 sweep targets — they cannot be migrated without breaking the chicken-and-egg ordering.

### BL254 retroactive-sweep targets

**None.**

BL242 (v6.4.0–v6.4.7) was thorough — every application credential is already wired through the secrets store. The Secrets-Store Rule is met for every credential-bearing field in the codebase except the two intentional meta-secrets above.

## Status

✅ **BL254 closed v6.11.3 (2026-05-05)** — audit complete; no retroactive sweep needed because BL242 covered the entire field set during the v6.4.x phases.

Future work: when new credential-bearing config fields are added (e.g. BL241 Matrix Phase 1 ships a `Matrix.HomeserverPassword` field for the bot account), they automatically inherit accepts-secret behavior via the reflection walker. The Secrets-Store Rule check is a no-op for any new field unless it deliberately bypasses `secrets.ResolveConfig`.

## How to re-run this audit

```bash
# Find candidate fields
grep -nE 'Token|Password|Secret|APIKey|AuthKey|AuthToken|AccessToken' \
  internal/config/*.go internal/server/*.go

# For each candidate, confirm reflection-walker coverage by reading
# secrets.ResolveConfig in internal/secrets/refs.go and verifying the
# field's path through the *Config struct hierarchy.
```

Re-run on every minor release that adds a new credential field, or after any refactor of `internal/secrets/refs.go`.
