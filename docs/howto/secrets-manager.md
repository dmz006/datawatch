---
docs:
  index: true
  topics: [secrets, vault, keepass, onepassword]
exec_params:
  - name: name
    description: Secret name (e.g. anthropic-api-key)
    required: true
  - name: value
    description: Secret value (the credential itself)
    required: true
  - name: description
    description: Optional human-readable description
    required: false
    default: ""
exec_steps:
  - tool: secret_set
    description: Create or update the secret in the active backend
    args:
      name: "{{params.name}}"
      value: "{{params.value}}"
      description: "{{params.description}}"
    read_only: false
  - tool: secret_exists
    description: Confirm the secret is now reachable
    args:
      name: "{{params.name}}"
    read_only: true
---
# How-to: Secrets Manager — native + KeePass + 1Password + Vault

One `${secret:name}` syntax across YAML configs, plugin manifests, and
per-session env injection. Three backend choices. Audit-logged on
every read.

## What it is

Centralized credential store. The native AES-256-GCM database at
`~/.datawatch/secrets.db` is the default and requires nothing to set
up. Optional KeePass, 1Password, and HashiCorp Vault / OpenBao backends for
operators who already use them.

`${secret:name}` references in any YAML get resolved at load / spawn
time. Resolved values never appear in the session log; per-secret tags
+ scopes restrict which callers can resolve them.

| Backend | Setup cost | Best for |
|---|---|---|
| `native` (default) | Zero | Single-host operators; lab work |
| `keepass` | KeePassXC + DB file | Operators on KeePass already |
| `onepassword` | `op` CLI + service account token | Org-mandated 1Password |
| `vault` | HashiCorp Vault / OpenBao KV v2; `DATAWATCH_VAULT_TOKEN` | Enterprise or OSS vault already in infra |

## Base requirements

- `datawatch start` — daemon up.
- For `keepass`: `keepassxc-cli` on PATH; existing `.kdbx` file.
- For `onepassword`: `op` CLI on PATH; service account token.

## Setup

Native backend needs nothing — just run the commands below.

For KeePass / 1Password, edit `~/.datawatch/datawatch.yaml`:

```yaml
secrets:
  backend: keepass        # or onepassword
  keepass_db: /home/<user>/.config/keepassxc/secrets.kdbx
  keepass_password: ${env:DATAWATCH_KEEPASS_PASSWORD}
  keepass_group: datawatch
```

Set the unlock secret in env (NOT in YAML — chicken-and-egg):

```sh
export DATAWATCH_KEEPASS_PASSWORD='...'
datawatch restart
```

For HashiCorp Vault / OpenBao, edit `~/.datawatch/datawatch.yaml`:

```yaml
secrets:
  backend: vault
  vault:
    address: https://vault.example.com:8200
    namespace: ""                    # Vault Enterprise namespace; leave empty for OSS
    auth_method: token               # only 'token' supported currently
    kv_mount: secret                 # KV v2 mount path
    path_prefix: datawatch/          # all secrets stored under this prefix
    path_layout: flat                # 'flat' (default) or 'tag_aware' (one subdir per tag)
    tls_ca_file: ""                  # path to custom CA bundle; empty = system CAs
    tls_skip_verify: false           # do NOT set true in production
    request_timeout: 10s
```

Set the Vault token via environment (never in YAML):

```sh
export DATAWATCH_VAULT_TOKEN='hvs.CAESXXXXXX'
datawatch restart
```

The `token` YAML field also accepts `${secret:name}` references into the built-in
store — letting you bootstrap Vault access via the native AES-256-GCM store:

```yaml
vault:
  token: ${secret:vault-token}
```

**Limitations:** Vault is the single active backend for all secrets —
per-agent Vault isolation is a future enhancement. KV v1 and Vault
AppRole/Kubernetes auth are also deferred.

## Two happy paths

### 4a. Happy path — CLI

```sh
# 1. Confirm which backend is active.
datawatch secrets backends
#  → active: native
#    available: native, keepass, onepassword

# 2. Store a secret.
datawatch secrets set GITHUB_TOKEN "ghp_..."
#  → ok (native: ~/.datawatch/secrets.db; audit-logged)

# 3. List (names + tags + scopes; never values).
datawatch secrets list
#  → GITHUB_TOKEN  (no tags) (scope: *)

# 4. Use it in a plugin / session env via the YAML reference syntax.
cat > ~/.datawatch/plugins/my-bot/manifest.yaml <<'EOF'
plugin: my-bot
env:
  GITHUB_TOKEN: ${secret:GITHUB_TOKEN}
EOF
# When the plugin spawns, the daemon resolves the reference; the
# child sees GITHUB_TOKEN in its env.

# 5. Tag + scope for finer control.
datawatch secrets set ANTHROPIC_API_KEY "sk-ant-..." \
  --tags work,ci --scopes plugin:claude-code,session:approved

# 6. Read explicitly (audit-logged).
datawatch secrets get GITHUB_TOKEN
#  → ghp_... (and an entry in the audit log)

# 7. Delete.
datawatch secrets delete OLD_TOKEN

# 8. Move to a new backend. There is no export command, so read each
#    value and set it again in the new backend. Do this yourself in a
#    terminal, not through an agent; every read is audit-logged.
datawatch secrets list                        # note each name, its tags and scopes
datawatch secrets get GITHUB_TOKEN            # read the value
# Set secrets.backend to the new backend in the config; restart.
datawatch secrets set GITHUB_TOKEN "ghp_..."  # repeat for each name, with its tags and scopes
```

(`datawatch secrets migrate` is a different job: it moves plaintext
credentials out of `config.yaml` into the secrets store and leaves
`${secret:name}` references behind. It does not move secrets between
backends.)

### 4c. External services (GH#203)

For an independent service that isn't a datawatch-spawned agent and isn't
a federation peer (for example a sibling daemon like imap-mcp, running as
its own systemd service) to resolve a specific, named secret without
being able to read every secret in the store:

```bash
# 1. Mint a persistent token for the service. Run this YOURSELF, in a
#    real terminal — never by asking an agent to run it, and not via
#    Claude Code's `!` shell-passthrough either, since both still land
#    the printed token in a conversation transcript. This command's
#    response is the ONLY place the plaintext value is ever shown.
datawatch secrets mint-service-token imap-mcp --desc "imap-mcp secret resolver"
#  → {"name":"imap-mcp","token":"<64 hex chars>"}

# 2. Scope the secret(s) that service should be able to read.
datawatch secrets set imap_mcp_token_datawatch "<value>" --scope service:imap-mcp

# 3. The external service resolves it itself:
#    GET <datawatch-url>/api/external/secrets/imap_mcp_token_datawatch
#    Authorization: Bearer <64 hex chars from step 1>
#  → {"name":"imap_mcp_token_datawatch","value":"<value>"}
#    A secret scoped to a different service, or not scoped to any
#    service, is denied (403) or simply not readable this way.

# List provisioned services (names only, never the token).
datawatch secrets list-service-tokens

# Revoke.
datawatch secrets revoke-service-token imap-mcp
```

Tokens are persistent (survive a daemon restart) and stored at
`<data_dir>/service_tokens.json`, 0600. This is distinct from
`GET /api/agents/secrets/{name}`, which is also per-secret scoped but
only ever issues tokens to spawned remote-agent-cluster workers at spawn time
(in-memory, lost on restart) — not usable by an external process.

### 4b. Happy path — PWA

1. PWA → Settings → General → **Secrets store** card.
2. The card shows the active backend (`native` / `keepass` /
   `onepassword`), the count of stored secrets, and a list (names + tags + scopes only — never values).
3. Click **+ Add Secret**. Modal asks for `name`, `value` (password
   field), `tags` (comma-separated), `scopes` (comma-separated).
4. **Save**. The card list refreshes; the new entry appears.
5. To inspect a value (audit-logged), click the secret row → **Reveal**.
   The value shows for ~10 s then auto-hides.
6. **Delete** removes the secret immediately.
7. To switch backend, scroll to **Backend** at the top of the card →
   **Change…** opens an inline editor that updates `~/.datawatch/datawatch.yaml` and prompts for `datawatch restart`.

## Other channels

### 5a. Mobile (Compose Multiplatform)

Same Settings → General → Secrets store card. Reveal-and-auto-hide
parity. Add / Delete parity. Backend-switch best done from CLI (mobile
can't restart the daemon).

### 5b. REST

```sh
# List (names + tags + scopes, never values).
curl -sk -H "Authorization: Bearer $TOKEN" $BASE/api/secrets

# Set.
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"GITHUB_TOKEN","value":"ghp_...","tags":["work"],"scopes":["plugin:gh"]}' \
  $BASE/api/secrets

# Get (audit-logged).
curl -sk -H "Authorization: Bearer $TOKEN" $BASE/api/secrets/GITHUB_TOKEN

# Delete.
curl -sk -X DELETE -H "Authorization: Bearer $TOKEN" $BASE/api/secrets/GITHUB_TOKEN
```

### Vault status

When `secrets.backend: vault`, a status row appears in Settings → Secrets Store:

```sh
# CLI
datawatch secrets vault status
# → address: https://vault.example.com:8200
#   reachable: true
#   kv_mount: secret, path_prefix: datawatch/
#   last success: 2026-05-09T14:30:01Z
#   last request_id: 01HZQ...

# REST
curl -sk -H "Authorization: Bearer $TOKEN" \
  https://localhost:8443/api/secrets/vault/status

# MCP
{ "tool": "secrets_vault_status", "args": {} }

# Comm
secrets vault status
```

The Settings nav button no longer shows a badge for an unreachable Vault. Check the status row in
Settings → Secrets Store instead: it shows **Vault** reachable or unreachable, the mount and path
layout, **Last success** (or `never`) and **Last error** (or `none`), each in your chosen language.

### Vault-unreachable alert

Instead of a nav badge, datawatch raises a real alert when the Vault stays unreachable.

- **Trigger.** Only when `secrets.backend` is `vault`. datawatch checks the Vault every 15 seconds.
  Once the Vault has been unreachable continuously for `secrets.vault.unreachable_alert_after_seconds`
  (whole seconds, default 120), one **Vault unreachable** error alert is raised. It is raised once per
  outage, not repeated.
- **What the alert contains.** The Vault address and the last error only. Secret values are scrubbed.
- **Where it shows.** It is a system alert: it appears in Alerts in the PWA and goes to your messaging
  channels like other system alerts.
- **Recovery.** When the Vault answers again, datawatch raises a separate **Vault recovered** info
  alert. The original error alert stays; mark it read as usual.
- **Threshold.** `0` turns the alert off; a negative value is rejected.

Set the threshold any of these ways (the examples use 300 seconds):

```yaml
# YAML: ~/.datawatch/datawatch.yaml
secrets:
  vault:
    unreachable_alert_after_seconds: 300
```

```sh
# REST
curl -sk -X PUT -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"secrets.vault.unreachable_alert_after_seconds": 300}' $BASE/api/config

# CLI
datawatch config set secrets.vault.unreachable_alert_after_seconds 300
```

```json
// MCP
{ "tool": "config_set", "args": { "key": "secrets.vault.unreachable_alert_after_seconds", "value": "300" } }
```

```text
# Comm channel
configure secrets.vault.unreachable_alert_after_seconds=300
```

In the PWA: Settings → General → the **Secrets Vault** section → **Vault unreachable alert after
(seconds)** (`0` = off). The field is translated in en, de, es, fr and ja.

**Checking the Settings nav button.** The sandbox story
`scripts/test-stories/pwa/vault-nav-label-clear.mjs` forces the Vault unreachable at a 390 px
viewport and confirms that nothing sits over the Settings nav label and that no Vault badge element
exists. Run it against the sandbox daemon, never the production port.

**For the operator to review.**

- The companion mobile clients may still show a Vault-unreachable badge and lack the last-error and
  last-success lines and the alert delay field. Parity issues are drafted under
  `docs/plans/parity-issues/`; confirm where the new lines and field sit on the app before they are filed.
- The default delay of 120 seconds is a supervisor choice. Change it if it does not suit your Vault.

### 5c. MCP

Tools: `secrets_list`, `secrets_get`, `secrets_set`, `secrets_delete`.

`secrets_get` is gated by the caller scope — an MCP-host AI calling
without a matching scope gets a `denied` error and a denied-attempt
audit entry. Useful: a session can call `secrets_get` for secrets
explicitly scoped to it, but can't trawl the whole vault.

### 5d. Comm channel

| Verb | Example |
|---|---|
| `secrets list` | Returns names + tags. Operator-only; gated on configured chat. |
| `secrets set <name> <value>` | Stores. Best from a private DM, not a group. |
| `secrets get <name>` | Returns the value. Audit-logged. |
| `secrets delete <name>` | Removes. |

### 5e. YAML

`~/.datawatch/datawatch.yaml` `secrets:` block configures the backend
+ unlock creds. Native: nothing required. KeePass / 1Password as shown
in **Setup** above.

`${secret:name}` references work in:

- `~/.datawatch/datawatch.yaml` (the daemon's own config)
- Plugin manifests (`~/.datawatch/plugins/<name>/manifest.yaml`)
- Project / cluster profiles (`~/.datawatch/profiles/{projects,clusters}/<name>.yaml`)
- Session spec when calling `POST /api/sessions/start`
- Automaton spec (`~/.datawatch/autonomous/prds/<id>.yaml` `env:` blocks)

References resolve at load / spawn time. The resolved value goes into
the child process env, never touches the log file.

Default-fallback syntax: `${secret:name:fallback-value}` resolves to
`fallback-value` if the secret is missing. Useful for optional creds.

## Keeping LLM API keys out of configuration

LLM registry entries for cloud inference kinds (Anthropic, Gemini, and
similar) take an `api_key_ref` field. Do not paste the key into config
files, registry JSON, or shell profiles — once a literal key lives in a
config it ends up in backups, dotfiles, and commit history. Store it in
the secrets store and reference it by name instead:

```sh
# 1. Store the key in the secrets store (placeholder below — put your
#    real key here in a private terminal, never in a config file).
datawatch secrets set ANTHROPIC_API_KEY '<your-api-key>'

# 2. Point the LLM entry at that secret.
datawatch llm add claude-api \
  --kind claude \
  --model claude-sonnet-5 \
  --api-key-ref '${secret:ANTHROPIC_API_KEY}'
```

In registry JSON the field is `"api_key_ref"`: `"${secret:ANTHROPIC_API_KEY}"`
— same reference, same store, same rules. This covers every consumer of the
entry: plain sessions, `/api/ask` and Council calls, and Automata running on
that LLM.

What the reference form gives you:

- **Rotation without a restart.** The reference is resolved on every LLM
call, not once at startup. To rotate, store a new value under the same
name — `datawatch secrets set ANTHROPIC_API_KEY '<new-api-key>'` — and
the next call uses the new value. Nothing to restart, no config file to
touch.
- **A missing secret fails loudly and safely.** If the secret does not
exist (wrong name, deleted, backend unreachable), the LLM call fails
with an error that names the secret and the LLM entry so you can fix
either. The error never prints a secret value.
- **The reference is the whole value.** `api_key_ref` must be exactly
`${secret:name}` — no surrounding text, no template interpolation, no
concatenation. A value that contains a `${secret:` token but is not exactly
one reference (e.g. text around it, or two references) is refused outright
with an error naming the LLM entry, so no broken key is ever sent.
- **Every resolution is audit-logged.** Each time the reference is
resolved you get an audit entry with the LLM entry name, the secret name,
and the outcome (`ok`, `missing`, or `refused`). The secret value
itself is never written to the audit log.

A literal key in `api_key_ref` still works and is what older setups use,
but the reference form is preferred wherever a secrets backend is
available (see the backends above).
See [howto/llm-registry](llm-registry.md) for the full LLM entry
reference and [howto/compute-routing](compute-routing.md) for a worked
cloud-LLM setup.

## Diagram

```
  ┌─────────────────────────┐
  │ Backend (native /        │
  │  keepass / 1password)    │
  └──────────┬──────────────┘
             │ resolve ${secret:name}
             ▼
  ┌─────────────────────────┐
  │ YAML / plugin manifest /│
  │ session spec            │
  └──────────┬──────────────┘
             │ at spawn
             ▼
  ┌─────────────────────────┐
  │ Child process env       │ ← secret never logged
  └─────────────────────────┘

  Audit log entry on every read (caller, scope match, denied/allowed).
```

## Common pitfalls

- **Secret in commit history.** If you've committed a value and only
  THEN moved it into the manager, rotate the secret first.
- **Native backend on a shared host.** The vault key is a random key file
  (`secrets.key`) kept next to `secrets.db`, or the `DATAWATCH_SECRETS_KEY`
  environment variable. Anyone who can read the data directory can read both,
  so a copy of the directory carries the secrets. For shared hosts use KeePass
  / 1Password with the unlock cred in env-only, or keep the key in the
  environment variable outside the data directory. See
  [encryption](../encryption.md#secrets-vault-secretsdb).
- **Unscoped secrets in a multi-plugin setup.** Default scope is `*`;
  if you have plugins you don't fully trust, scope per-plugin.
- **Backend creds with `${secret:...}`.** Won't work — the daemon
  needs to UNLOCK the secrets backend before it can resolve refs from
  it. Use `${env:...}` for KeePass / 1Password unlock creds.
- **Values copied by hand stay in your terminal.** When you move secrets
  between backends with `secrets get` and `secrets set`, the values land in
  your shell history and scrollback. Clear both afterwards, and never paste
  them into a file. There is no export file to destroy because datawatch
  writes none.

## Linked references

- API: `/api/secrets/*` (Swagger UI under `/api/docs`)
- See also: `tailscale-mesh.md` (TS_PREAUTH_KEY pattern)
- See also: `daemon-operations.md` for env-var management
- Architecture: `../architecture-overview.md` § Secrets

## Screenshots needed (operator weekend pass)

- [ ] PWA Secrets store card (list + active backend badge)
- [ ] + Add Secret modal
- [ ] Reveal Value with auto-hide countdown
- [ ] Backend change inline editor
- [ ] Audit log filtered by `secrets_get`
- [ ] CLI `datawatch secrets list --tag work` output

---

## See also

- [datawatch-definitions](../datawatch-definitions.md)
- [howto/profiles](profiles.md)
- [howto/tailscale-mesh](tailscale-mesh.md)
- [registry-and-secrets](../registry-and-secrets.md)
- [api/secrets](../api/secrets.md)
