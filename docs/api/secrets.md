# Secrets Manager API (BL242)

Centralized encrypted secrets store with scope enforcement, three storage backends, and cross-surface access (REST, MCP, CLI, comm channel, PWA). Shipped across v6.4.0–v6.4.7.

## REST Endpoints

All endpoints except `/api/agents/secrets/{name}` require the operator bearer token.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/secrets` | List all secrets (names/tags only; no values) |
| `POST` | `/api/secrets` | Create a secret |
| `GET` | `/api/secrets/{name}` | Get a secret with its value (audited) |
| `PUT` | `/api/secrets/{name}` | Update a secret |
| `DELETE` | `/api/secrets/{name}` | Delete a secret |
| `GET` | `/api/secrets/{name}/exists` | Check existence without revealing value |
| `GET` | `/api/agents/secrets/{name}` | Agent runtime fetch — bearer token is the per-agent SecretsToken (pre-auth, scope-enforced) |

### Create / Update body

```json
{
  "name": "github-token",
  "value": "ghp_...",
  "tags": ["git"],
  "description": "GitHub PAT for CI worker",
  "scopes": ["agent:ci-runner", "plugin:gh-hooks"]
}
```

`scopes` — optional list of `type:name` entries that restrict access to specific callers. Empty = universally accessible. Supported types:
- `agent:<profileName>` — only agents whose project profile name matches
- `agent:*` — all agents
- `plugin:<pluginName>` — only the named plugin
- `plugin:*` — all plugins

Operators (REST/MCP/CLI with daemon token) always bypass scope checks.

### Agent runtime access

Workers spawned by the daemon receive `DATAWATCH_SECRETS_TOKEN` and `DATAWATCH_SECRETS_URL` in their environment (set by `ApplyBootstrapEnv`). Use the worker SDK:

```go
import "github.com/dmz006/datawatch/internal/agents"

val, err := agents.FetchSecret(ctx, "github-token")
// Returns agents.ErrSecretsUnavailable if the parent has no secrets store
// Returns an error with "access denied (scope)" if the secret is out of scope
```

Or call the endpoint directly:

```
GET /api/agents/secrets/{name}
Authorization: Bearer <DATAWATCH_SECRETS_TOKEN>
```

Tokens are revoked when the agent terminates. Each child agent mints its own token.

## Config Reference (`datawatch.yaml`)

```yaml
secrets:
  backend: builtin          # builtin | keepass | onepassword

  # KeePass backend
  keepass_db: /path/to/vault.kdbx
  keepass_binary: keepassxc-cli   # default
  keepass_group: datawatch        # optional group scope
  # keepass_password: set via DATAWATCH_KEEPASS_PASSWORD env var

  # 1Password backend
  op_vault: ""              # default vault when omitted
  # op_token: set via DATAWATCH_OP_TOKEN env var
```

## Secret references in config

Fields in `datawatch.yaml` can reference secrets:

```yaml
messaging:
  discord:
    token: "${secret:discord-token}"
  slack:
    token: "${secret:slack-token}"
```

Resolved at daemon startup and on hot-reload (`/api/reload`).

## Plugin env injection

Plugin manifests can declare env vars that are resolved from the secrets store at invoke time:

```yaml
# <data_dir>/plugins/my-plugin/manifest.yaml
name: my-plugin
entry: ./plugin.sh
hooks:
  - post_session_output
env:
  GITHUB_TOKEN: "${secret:github-token}"   # scope-checked against plugin:my-plugin
  API_BASE: "https://api.example.com"       # plain value, always injected
```

## MCP Tools

| Tool | Description |
|------|-------------|
| `secret_list` | List all secrets |
| `secret_get` | Get a secret value |
| `secret_set` | Create or update a secret |
| `secret_delete` | Delete a secret |
| `secret_exists` | Check if a secret exists |

## CLI

```
datawatch secrets list
datawatch secrets get <name>
datawatch secrets set <name> <value> [--tag TAG ...] [--desc DESC] [--scope SCOPE ...]
datawatch secrets delete <name>
datawatch secrets migrate   # one-shot migration of plaintext config fields → secrets store
```

## Audit

Every secret value fetch writes an audit entry:

```json
{"actor":"operator","action":"secret_access","details":{"resource_type":"secret","resource_id":"github-token","via":"rest"}}
{"actor":"agent:ci-runner","action":"secret_access","details":{"resource_type":"secret","resource_id":"github-token","via":"agent-secrets-token"}}
```
