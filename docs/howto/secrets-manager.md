# How-to: Secrets Manager — native + KeePass + 1Password

One `${secret:name}` syntax across YAML configs, plugin manifests,
and per-session env injection. Three backend choices. Audit-logged on
every read.

## What it is

Centralized credential store. The native AES-256-GCM database at
`~/.datawatch/secrets.db` is the default and requires nothing to set
up. Optional KeePass and 1Password backends for operators who already
use them at the org level.

`${secret:name}` references in any YAML (datawatch.yaml, plugin
manifest, session spec, profile) get resolved at load / spawn time.
Resolved values never appear in the session log; the operator can
audit who read what when via the audit log.

## Backend comparison

| Backend | Setup cost | Where credentials live | Best for |
|---|---|---|---|
| `native` (default) | Zero | `~/.datawatch/secrets.db` (AES-256-GCM, key from machine ID) | Single-host operators; lab work |
| `keepass` | KeePassXC + DB file | Your existing KeePass DB | Operators already standardized on KeePass |
| `onepassword` | `op` CLI + service account token | Your 1Password vault | Org-mandated 1Password |

You can only run ONE backend at a time. Switch by editing
`~/.datawatch/datawatch.yaml`.

## 1. Native backend (default — no setup)

Just use it:

```sh
datawatch secrets set GITHUB_TOKEN "ghp_..."
datawatch secrets list
datawatch secrets get GITHUB_TOKEN          # prints to stdout (audit-logged)
datawatch secrets delete GITHUB_TOKEN
```

The DB is encrypted with a key derived from the machine ID. If you
move datawatch to a different host, secrets won't decrypt — see
**Migrating to a new host** below.

## 2. KeePass backend

Prereqs: `keepassxc-cli` installed, your `.kdbx` file accessible to
the daemon.

`~/.datawatch/datawatch.yaml`:

```yaml
secrets:
  backend: keepass
  keepass_db: /home/dmz/.config/keepassxc/secrets.kdbx
  keepass_password: ${env:DATAWATCH_KEEPASS_PASSWORD}
  keepass_group: datawatch       # optional; group within the DB
```

Set the password in the daemon's env:

```sh
export DATAWATCH_KEEPASS_PASSWORD='...'
datawatch restart
```

Now `datawatch secrets list` reflects entries in the KeePass DB
(scoped to the configured group if set). `set` / `delete` invoke
`keepassxc-cli` to mutate the DB.

## 3. 1Password backend

Prereqs: `op` CLI installed, a service account token with read access
to the vault you want.

`~/.datawatch/datawatch.yaml`:

```yaml
secrets:
  backend: onepassword
  op_token: ${env:DATAWATCH_OP_TOKEN}
  op_vault: datawatch            # vault name
```

Set the token:

```sh
export DATAWATCH_OP_TOKEN='ops_...'
datawatch restart
```

`datawatch secrets list` reflects items in the vault; `set` / `delete`
proxy to `op item create / delete`.

## 4. Reference secrets in YAML

Plugin manifest:

```yaml
plugin: my-gh-bot
env:
  GITHUB_TOKEN: ${secret:GITHUB_TOKEN}
  ANOTHER_KEY:  ${secret:ANOTHER_KEY:default-value-if-missing}
```

Session spec (passed via `/api/sessions/start`):

```yaml
env:
  ANTHROPIC_API_KEY: ${secret:ANTHROPIC_API_KEY}
```

Datawatch config itself can reference secrets — useful for
`server.bearer_token`, signal device passwords, etc.

References resolve at config load / session spawn. The resolved value
goes into the child process env, never touches the log file.

## 5. Per-secret scopes

Restrict which callers can resolve a secret:

```sh
datawatch secrets set GITHUB_TOKEN "ghp_..." --scopes plugin:gh,session:approved
```

Scope syntax:

- `plugin:<name>` — only the named plugin can resolve.
- `session:approved` — only sessions started from an approved
  Automaton spec can resolve.
- `session:*` — any session can resolve (default).
- `cli:operator` — only operator-typed CLI commands can resolve;
  excludes plugin / session callers.

If a caller doesn't match any scope, resolution returns an error and
the audit log records a denied attempt.

## 6. Tags + organization

```sh
datawatch secrets set GITHUB_TOKEN "ghp_..." --tags work,ci
datawatch secrets list --tag work
```

Tags are free-form strings; useful for grouping (work / personal,
prod / staging / dev, customer:acme, etc.).

## 7. Audit reading

Every read is logged:

```sh
datawatch audit list --action secrets_get --limit 20
```

PWA: Observer → Audit log → filter by action `secrets_get`.

## 8. Migrating between backends

```sh
# Export current backend's secrets to a portable JSON.
datawatch secrets export ~/secrets-backup.json

# Edit ~/.datawatch/datawatch.yaml; switch backend; restart.
datawatch restart

# Import into the new backend.
datawatch secrets import ~/secrets-backup.json

# Delete the backup file.
shred -u ~/secrets-backup.json
```

The export is JSON with secrets in plaintext — handle accordingly
(never commit, shred when done). For native → native migration to a
new host, this is the recommended path; the on-disk DB is keyed to
the source machine.

## CLI reference

```sh
datawatch secrets list [--tag ...]                     # names + tags + scopes; never values
datawatch secrets get <name>                           # value to stdout
datawatch secrets set <name> <value> [--tags ...] [--scopes ...]
datawatch secrets set <name> -                          # read value from stdin
datawatch secrets delete <name>
datawatch secrets export <file>                        # backend → JSON
datawatch secrets import <file>                        # JSON → backend
datawatch secrets backends                             # list available backends + active one
```

## REST / MCP

- `GET /api/secrets` → list
- `POST /api/secrets {name, value, tags?, scopes?}` → set
- `GET /api/secrets/<name>` → get (audit-logged)
- `DELETE /api/secrets/<name>` → delete
- MCP: `secrets_list`, `secrets_get`, `secrets_set`, `secrets_delete`.

## Common pitfalls

- **Secret in commit history.** If you've already committed a secret
  and only THEN moved it into the secrets manager, rotate the secret
  first — the git history isn't going away.
- **Native backend on a host you don't trust.** The encryption key
  is derived from the machine ID. Anyone with shell access can read
  the DB. For shared hosts use KeePass / 1Password and put the
  unlock secret in env-only.
- **Unscoped secrets in a multi-plugin setup.** Default scope is
  `*`; if you have plugins you don't fully trust, scope per-plugin.
- **Forgetting `${env:DATAWATCH_*}` for backend creds.** KeePass /
  1Password creds themselves can't reference `${secret:...}` (chicken-
  and-egg). Use `${env:...}` and set the env var in your daemon
  start script.

## Linked references

- Architecture: `architecture-overview.md` § Secrets
- API: `/api/secrets/*` (Swagger UI under `/api/docs`)
- Plan: BL242 Secrets Manager arc; BL267 OSS vault backend (open)
- See also: `daemon-operations.md` for env-var management

## All channels reference

| Channel | How |
|---|---|
| **PWA** | Settings → General → Secrets store card → list / set / delete. |
| **Mobile** | Same surface. |
| **REST** | `GET/POST/DELETE /api/secrets[/<name>]`. |
| **MCP** | `secrets_list`, `secrets_get`, `secrets_set`, `secrets_delete`. |
| **CLI** | `datawatch secrets {list,get,set,delete,export,import,backends}`. |
| **Comm** | `secrets list`, `secrets set <name> <value>` (audit-logged; only configured operator chat). |
| **YAML** | `${secret:name}` references in any YAML — datawatch.yaml, plugin manifest, session spec, profile. |

## Screenshots needed (operator weekend pass)

- [ ] PWA Secrets store card (list view)
- [ ] Set Secret modal
- [ ] Audit log view filtered by `secrets_get`
- [ ] CLI `datawatch secrets list --tag work` output
