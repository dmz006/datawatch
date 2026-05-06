# How-to: Secrets Manager — native + KeePass + 1Password

One `${secret:name}` syntax across YAML configs, plugin manifests, and
per-session env injection. Three backend choices. Audit-logged on
every read.

## What it is

Centralized credential store. The native AES-256-GCM database at
`~/.datawatch/secrets.db` is the default and requires nothing to set
up. Optional KeePass and 1Password backends for operators who already
use them.

`${secret:name}` references in any YAML get resolved at load / spawn
time. Resolved values never appear in the session log; per-secret tags
+ scopes restrict which callers can resolve them.

| Backend | Setup cost | Best for |
|---|---|---|
| `native` (default) | Zero | Single-host operators; lab work |
| `keepass` | KeePassXC + DB file | Operators on KeePass already |
| `onepassword` | `op` CLI + service account token | Org-mandated 1Password |

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
  keepass_db: /home/dmz/.config/keepassxc/secrets.kdbx
  keepass_password: ${env:DATAWATCH_KEEPASS_PASSWORD}
  keepass_group: datawatch
```

Set the unlock secret in env (NOT in YAML — chicken-and-egg):

```sh
export DATAWATCH_KEEPASS_PASSWORD='...'
datawatch restart
```

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

# 8. Migrate to a new backend.
datawatch secrets export ~/secrets-backup.json
# Edit datawatch.yaml; switch backend; restart.
datawatch secrets import ~/secrets-backup.json
shred -u ~/secrets-backup.json    # the export was plaintext
```

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
- **Native backend on a shared host.** The encryption key is derived
  from the machine ID; anyone with shell access can read the DB. For
  shared hosts use KeePass / 1Password with the unlock cred in env-only.
- **Unscoped secrets in a multi-plugin setup.** Default scope is `*`;
  if you have plugins you don't fully trust, scope per-plugin.
- **Backend creds with `${secret:...}`.** Won't work — the daemon
  needs to UNLOCK the secrets backend before it can resolve refs from
  it. Use `${env:...}` for KeePass / 1Password unlock creds.
- **`shred` on the export file.** The `secrets export` JSON is
  plaintext; `rm` leaves it recoverable on most filesystems. Use
  `shred -u` (or equivalent) to actually destroy.

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
