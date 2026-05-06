# Release Notes — v6.4.4 (`datawatch secrets migrate`)

Released: 2026-05-03
GH release: https://github.com/dmz006/datawatch/releases/tag/v6.4.4

## Summary

Patch release adding `datawatch secrets migrate` — a one-shot operator command that moves all known plaintext credentials from `datawatch.yaml` into the secrets store and rewrites the config with `${secret:name}` references.

## Added

- **`datawatch secrets migrate`** — scans the active config for 16 known sensitive fields (messaging tokens, API keys, webhook secrets, SMTP passwords, remote-server tokens), stores each in the secrets store via the REST API, and rewrites the config file in place with `${secret:name}` references.
  - `--dry-run`: print the migration plan without making any changes
  - `--yes`: skip the confirmation prompt (for scripted use)
  - Masked preview of each credential value in the plan output (`ab**ef`)
  - Prints a `datawatch restart` reminder after config rewrite
  - CLI-only: requires local filesystem access to `datawatch.yaml`; no MCP/comm surface needed (individual secrets CRUD already available there)

## Covered fields

| Secret name | Config field |
|---|---|
| `openwebui-api-key` | `openwebui.api_key` |
| `memory-openai-key` | `memory.openai_key` |
| `dns-channel-secret` | `dns_channel.secret` |
| `discord-token` | `discord.token` |
| `slack-token` | `slack.token` |
| `telegram-token` | `telegram.token` |
| `matrix-access-token` | `matrix.access_token` |
| `twilio-auth-token` | `twilio.auth_token` |
| `twilio-account-sid` | `twilio.account_sid` |
| `ntfy-token` | `ntfy.token` |
| `email-password` | `email.password` |
| `github-webhook-secret` | `github_webhook.secret` |
| `webhook-token` | `webhook.token` |
| `mcp-sse-token` | `mcp.token` |
| `keepass-master-password` | `secrets.keepass_password` |
| `op-service-token` | `secrets.op_token` |
| `remote-token-<name>` | `servers[].token` (one per server) |

## See also

CHANGELOG.md `[6.4.4]` entry.
