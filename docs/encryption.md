# Encryption at Rest

datawatch can encrypt the data it keeps on disk with XChaCha20-Poly1305 and an
Argon2id-derived key when `--secure` mode is active.

> **Read this first: `--secure` does not cover everything.** It covers the
> stores listed under [What Gets Encrypted](#what-gets-encrypted). Several
> stores that hold credentials, task text or history are still plain files
> (see [Still plain today](#still-plain-today-and-could-be-covered-later)), and
> a few things can never be encrypted by datawatch (see
> [What CANNOT Be Encrypted](#what-cannot-be-encrypted--and-why)). The full
> evidence, store by store, is in the
> [encryption audit](plans/2026-11-13-encryption-audit.md) (written against
> v9.172.5 by reading the code; nothing in it was run). If you need a guarantee
> for the whole data directory, combine `--secure` with disk-level encryption
> (LUKS, dm-crypt, an encrypted home directory).

## Enabling Encryption

Encryption can be enabled **at any time** on an existing installation. The
daemon encrypts a fixed list of existing plain files on the first `--secure`
startup, and every other store becomes encrypted the next time it is saved (see
[Upgrade Compatibility](#upgrade-compatibility)).

| Method | How |
|--------|-----|
| **CLI** | `datawatch --secure start --foreground` (prompts for password) |
| **Environment** | `export DATAWATCH_SECURE_PASSWORD="passphrase"` then `datawatch --secure start` |
| **Web UI** | Settings → About tab shows encryption status |
| **REST API** | `GET /api/health` returns `"encrypted": true/false`; `GET /api/security/encryption/status` |
| **CLI status** | `datawatch security encryption status` |

```bash
# Start with --secure flag (prompts for password)
datawatch --secure start --foreground

# Or use an environment variable for non-interactive use
export DATAWATCH_SECURE_PASSWORD="your-strong-password"
datawatch --secure start
```

Starting in the background with `--secure` only prints a warning that daemon
mode needs interactive password entry; it then carries on. To run a secure
daemon unattended, supply `DATAWATCH_SECURE_PASSWORD` in the service's
environment. That puts the password wherever the environment comes from (a
service unit, a shell profile), and anyone who can read the process
environment as the same user can read it. Protect that source accordingly.

### Auto-Detection

If `config.yaml` is already encrypted (a `DWATCH1`, `DWATCH2` or `DWATCH3`
header), datawatch turns secure mode on without needing the `--secure` flag.

---

## What Gets Encrypted

"Encrypted" below means: when `--secure` is on and the key is present, the write
path goes through an encrypting call. Without `--secure`, every file here is
plain.

### Encrypted when `--secure` is active

| File | Format | Notes |
|------|--------|-------|
| `config.yaml` | `DWATCH3` | XChaCha20-Poly1305 + Argon2id. Written encrypted at startup. **Known limitation:** the code path used when a setting is changed at runtime writes plain YAML, so a running secure daemon appears to leave `config.yaml` unencrypted after its first settings change. This was found by reading the code and not reproduced. After changing settings, check with `datawatch security encryption status`. |
| `sessions.d/p.<id>.rec` | `DWDAT2` | Session records, one file per session. A record is encrypted when it is next written, so a quiet old record stays plain until then. (`sessions.json` is the legacy single-file store, only read for migration.) |
| `sessions-archive.jsonl` | `DWDAT2` per line | Archived sessions. Only the id and timestamp stay readable. Old plain lines stay plain. |
| `sessions/<id>/fields/<field>.txt` | `DWDAT2` | Long session fields kept outside the record |
| `sessions/<id>/output.log.enc` | `DWLOG1` | Streaming block encryption via FIFO. See the exceptions under [Still plain today](#still-plain-today-and-could-be-covered-later) |
| `schedule.json` | `DWDAT2` | Schedule store |
| `alerts.json` | `DWDAT2` | Alert store |
| `commands.json` | `DWDAT2` | Saved command library |
| `filters.json` | `DWDAT2` | Filter store |
| `servers.json` | `DWDAT2` | Federation peer registry (contains peer tokens) |
| `skills.json` | `DWDAT2` | Skills registry index |
| `compute/nodes.json` | `DWDAT2` | ComputeNode registry |
| `inference/llms.json` | `DWDAT2` | LLM registry |
| `channel_routing.json` | `DWDAT2` | Channel routing table |
| `profiles/projects.json`, `profiles/clusters.json` | `DWDAT2` | Project and cluster profiles |
| `lifecycle_subscriptions.json` | `DWDAT2` | Automaton lifecycle subscriptions (URLs and secrets) |
| `acme/account.json` | `DWDAT2` | ACME account email, registration and account key |
| `discussions/<id>/participants.json` | `DWDAT2` | Discussion participant lists |
| `discussions/<id>/wal.jsonl` | Per-line `ENC:<b64>` | Each JSONL line encrypted independently |
| `plan-records/<kind>/<uid>.json` | JSON envelope with a `DWDAT2` sealed payload | Plan records (registry rows and findings). Only `kind`, `project`, `state`, `id`, `uid`, `legacy_number`, `created_at`, `updated_at` and `closed_at` stay readable so records can be filtered without the key; the title, notes, aliases, links, Automaton ids, review fields, legacy source and shipped version exist only inside the sealed payload. Without `--secure` the files are plain JSON |
| `sessions/<id>/session.json` | `DWDAT2` | **Only** when `secure_tracking` is `full`, and only for a tracker created in this run. A tracker resumed after a daemon restart is written without the key. The default is not `full`, so by default this file is plain |
| `daemon-app.log` | `DWLOG1` | Application log after key derivation |
| Memory database, `content` and `summary` columns | `ENC:` per field | See below |

`secure_tracking` (`full` or `log_only`) is a `session` setting that is not yet
described in `docs/config-reference.yaml` or anywhere else under `docs/`; in `full` mode the
tracker's `conversation.md` and `timeline.md` appends are also encrypted, but
`task.md`, `README.md`, the guardrail copy and `backend_state.json` are not.

### Separately encrypted (independent of `--secure`)

| System | Mechanism | Notes |
|--------|-----------|-------|
| `secrets.db` | AES-256-GCM + own keyfile | Independent of the `--secure` password. Lives at `{data_dir}/secrets.db`. The key is 32 random bytes in `secrets.key` in the **same folder** (mode 0600), or the `DATAWATCH_SECRETS_KEY` environment variable. A copy of the data directory therefore carries both the ciphertext and its key. |
| Memory database (SQLite or Postgres) | Per-field XChaCha20-Poly1305 | Only `content` and `summary` are encrypted, and only when `--secure` is on (the key is the `--secure` data key). There is **no** `memory.encrypt_content` or `memory.encryption.*` setting: encryption follows `--secure`. The embedding vector, the content hash, the metadata columns, the knowledge graph tables and `memory-wal.jsonl` stay plain. |

### Memory behaviour to know about

- **Export.** `GET /api/memory/export` and the `memory_export` tool from a SQLite
  store return the stored `ENC:` ciphertext, which no other install can read. The
  Postgres store decrypts, so its export is plain JSON. Treat a memory export as
  sensitive either way.
- **Import.** Importing into a SQLite store inserts the rows as plain text, even in
  a secure install. Postgres import encrypts.
- **Keyword fallback.** Keyword search compares against the stored column, so on an
  encrypted SQLite store it matches ciphertext and finds nothing. Vector search is
  not affected.
- **Duplicate detection** uses an unsalted hash of the plain text; someone with the
  database and a guessed text can confirm the guess.

---

## Still plain today, and could be covered later

These are written without encryption even when `--secure` is on. They are
datawatch's own stores, so covering them is a code change. Which of them enter
the 10.0 scope is the operator's decision (see
[the audit](plans/2026-11-13-encryption-audit.md#decisions-that-stay-with-the-operator)).
Until then, treat the data directory as sensitive and use disk-level encryption.

| Group | Files | What they hold |
|-------|-------|----------------|
| Credentials and keys | `service_tokens.json`, `session_tokens.json`, `auth/tokens.json`, `auth/audit.jsonl`, `devices.json`, `plugins/widgets.json`, the file secrets provider's files (if used), TLS private keys under `tls/` and `tls/acme/`, the Matrix registration file | Tokens in clear (mode 0600), push registrations, private keys. The vault key sits beside the vault (above). |
| Sessions | `sessions/<id>/output.log` (plain file) | Created empty; in secure mode it still receives output from **subprocess sessions and the opencode ACP backend**, which do not use the encrypted pipe |
| | restart gap | After a daemon restart the encrypted pipe is not re-attached for a surviving session, so it stops recording output until it is restarted |
| | `sessions/<id>/task.md`, `README.md`, `conversation.md`, `timeline.md`, `backend_state.json`, guardrail copy, the tracker `.git/` | Task text, state history, prompts, timeline; the tracker repository commits them in plain |
| | `exit_hooks.json`, `queue.json`, discussion subscriptions | Hook actions, queued instructions |
| | `sessions.json.pre-records.bak`, `sessions.json.corrupted-*` | Raw copies of the old store taken at migration or on a parse failure; as plain as the source was |
| Automata and spend | `autonomous/` store (specs, tasks, prompts, worker output, judge text, learnings), `autonomous/scratch/`, caches and decision files, `questions.jsonl`, `decision_records.jsonl` | The largest store (hundreds of MB) |
| | `spend_ledger.json`, `usage.json`, `lanes.json`, budget and backoff files | Spend and lane history |
| Memory | Embeddings, content hash, metadata, knowledge graph (`kg_entities`, `kg_triples`), `memory-wal.jsonl` | See above |
| Observability | `audit.log`, `access.log` (and the `.cef` mirrors a SIEM reads), `trace/`, `websearch.db`, `council/`, `docs-trust.json`, `identity.yaml` and `identity-backups/` | Operator actions, tool-call traces, search queries, debate transcripts |
| Logs | `daemon.log`, `daemon-crash.log`, the restart log | Boot and crash output |
| Federation | `observer/peers.json`, peer token files (`peer.token`, `observer.federation.token_path`), `federation/groups.json` | Hashes and tokens; the peer token files are read by a separate process |

---

## What CANNOT Be Encrypted — and Why

These are structurally impossible to encrypt with the `--secure` key, or are not
stored on disk at all. Protect them with disk-level encryption or by controlling
who can read the machine.

| File / Artifact | Why it cannot be encrypted |
|-----------------|---------------------------|
| **`daemon.log`** (boot messages) | Written by the **parent** `daemonize` process before the child derives the key. The encrypted `daemon-app.log` captures log lines written _after_ key derivation. The service manager's journal and crash output are owned by the process manager. |
| **The `--secure` password itself** | It is the root of trust, never stored on disk. When supplied through `DATAWATCH_SECURE_PASSWORD` it lives in the service environment. |
| **Project directories, `.git/` and PRD worktrees** | Git and the launched tools (claude-code, opencode and others) need plain files and plain object storage; encrypting the object files would break the repository. This includes merge and trial scratch folders. Encrypt at the **filesystem level** instead. |
| **Files the launched tools read** | `.mcp.json` in each project (it carries the per-session token, or the instance token on the instance-level write), `settings.json`, the platform rules in `CLAUDE.md`/`CLAUDE.local.md`, and operator-supplied files. The task text file the daemon hands to a model is plain in the system temp folder while the session runs and is removed when it ends (a crash before cleanup leaves it). A memory-backed temp folder reduces that exposure. |
| **Temporary media** | Voice clips, images and transcripts exist as temp files for one request because ffmpeg, whisper or an LLM must read them. |
| **Other tools' own storage** | claude-code transcripts, aider history, `.opencode/`, `.goose/`, the tmux server's scrollback, Postgres, signal-cli's keystore, secrets backends such as KeePass or 1Password, Ollama and other model servers. |
| **Screen captures and tmux pane captures** | Not written to disk. Capture text goes to the daemon or the client and is discarded. |
| **tmux pipe output (live)** | In secure mode `pipe-pane` feeds a named pipe that the daemon re-encrypts into `output.log.enc`; the in-flight bytes in the kernel buffer are not at-rest data. (Exceptions: see the plain `output.log` above.) |
| **Memory embeddings** | Float32 vectors must stay plain for vector search; they can hint at the topic of the text. |
| **Certificates and hashed tokens** | Certificates are public; plugin and observer tokens are stored only as hashes. |
| **Network transit** | Not an at-rest feature. Peer, worker and observer connections rely on TLS. Certificate checking can be turned off per peer (`tls_skip_verify`), and worker observer pushes default to skipping verification. |

### Note on `daemon.log` vs `daemon-app.log`

Two log files exist when `--secure` is active:

| File | Encrypted | Contains |
|------|-----------|----------|
| `daemon.log` | No | Boot / startup messages written before the key is derived |
| `daemon-app.log` | Yes (`DWLOG1`) | All `log.*` calls after key derivation (the majority of runtime logs) |

To read `daemon-app.log`:

```bash
datawatch security logs            # all lines
datawatch security logs --tail 100  # last 100 lines
```

**Note:** `daemon-app.log` is opened for append when its header is valid, so it
**accumulates across restarts**; it is cut only when the file is new or has no
valid header. There is no retention for it today. Boot messages from `daemon.log`
are always plaintext.

---

## Upgrade Compatibility

Encryption can be enabled or the daemon upgraded at any time without data loss.

### Enabling `--secure` on an existing unencrypted installation

1. Run `datawatch --secure start --foreground`
2. On first startup the daemon encrypts, in place and once (guarded by a marker):
   `config.yaml`; every `sessions/<id>/output.log` (the plain file, as a whole-file
   `DWDAT2`, not `DWLOG1`); discussion logs and participants; `channel_routing.json`,
   `servers.json`, `skills.json`, `compute/nodes.json` and `inference/llms.json`.
3. Every **other** secure store converts the next time it saves. Until then a quiet
   file stays plain. The stores under
   [Still plain today](#still-plain-today-and-could-be-covered-later) do not convert at all.
4. Migration is idempotent (already-encrypted files are skipped).

### Upgrading from v8.5.x or earlier to v8.6.0+

v8.6.0 added encryption for four previously-plaintext stores (`servers.json`,
`skills.json`, `compute/nodes.json`, `inference/llms.json`). On first `--secure`
startup after upgrade those four are encrypted in place, with no user action.

### Manual migration trigger

If the daemon is running and you want to migrate without restarting:

```bash
datawatch security encryption migrate
```

To verify status:

```bash
datawatch security encryption status
```

`status`, `migrate` and `wipe-plaintext` (REST: `/api/security/encryption/*` and
`/api/security/wipe-plaintext`) cover only the **fixed list** of files named in
the startup migration above. They do not report on the other stores, so a clean
status does not mean the whole data directory is encrypted.

### Secure wipe of plaintext originals

Files are encrypted in place, but plain copies can remain: the
`.pre-records.bak` and `.corrupted-*` copies made during layout migrations keep the
state of their source. To 3-pass overwrite remaining plain files from the fixed
list as a belt-and-suspenders measure:

```bash
datawatch security wipe-plaintext --confirm
```

> **Warning:** 3-pass overwrite (zeros/ones/random bytes) then unlink. On modern
> NVMe/SSD and copy-on-write filesystems (Btrfs, ZFS) the overwrite may not reach
> the underlying storage due to wear-leveling. Use LUKS or an encrypted home
> directory for stronger guarantees at the block-device level.

---

## Encryption Architecture

### Config File (`DWATCH3`)

- **Algorithm:** XChaCha20-Poly1305
- **KDF:** Argon2id (time=4, memory=512 MiB, threads=4, output=32 bytes)
- **Salt:** 16 random bytes embedded in the file header
- **Nonce:** 24 bytes (fresh per write)
- **Backward compat:** `DWATCH1` (AES-256-GCM) and `DWATCH2` (XChaCha20-Poly1305 with the weaker Argon2id setting time=1, memory=64 MiB) read transparently; new writes are `DWATCH3`

### Data Key

Every store other than the config file uses a 32-byte **data key** derived from
the same password and the salt in the config header (a legacy `enc.salt` file is
the fallback), with Argon2id at the older, weaker setting: time=1, memory=64 MiB,
threads=4. The stores carry no version marker, so strengthening this setting
needs a key-rotation migration that does not exist yet. The key lives in process
memory and is zeroed on exit.

### Data Stores (`DWDAT2`)

- **Algorithm:** XChaCha20-Poly1305
- **Key:** the data key above
- **Nonce:** 24 bytes (fresh per write)
- **Format:** `DWDAT2\n` + base64(nonce24 + ciphertext)
- **Atomic write:** tmp file + sync + rename — no partial writes
- **Backward compat:** `DWDAT1` (AES-256-GCM) files read transparently. A plain file
  read with a key is accepted as-is, so a store converts to encrypted on its next write.

### Streaming Logs (`DWLOG1`)

Used for `output.log.enc` (session) and `daemon-app.log`.

- **Algorithm:** XChaCha20-Poly1305 per block
- **Block size:** up to 4096 bytes of plaintext per encrypted block
- **Nonce:** 24 bytes per block (fresh random)
- **Format:** `DWLOG1\n` + repeated `[u32le_length][nonce24 + ciphertext]`
- **Each block independently decryptable** (no cross-block dependencies)
- **Note:** a file that already has a valid header is opened for append, so logs
  continue across restarts; a file without one is started fresh.

### Discussion WAL (per-line)

Each JSONL line in `discussions/<id>/wal.jsonl` is independently encrypted:

- **Format:** `ENC:<base64(nonce24 + ciphertext)>`
- Lines without `ENC:` prefix are read as plaintext (upgrade compatibility)
- Scanner buffer: 1 MB (encrypted lines are ~33% larger than plaintext)

### Post-Quantum Safety

XChaCha20-Poly1305 with 256-bit keys: Grover's algorithm reduces effective key
strength to ~128-bit equivalent, which remains secure. No asymmetric operations
are used in the at-rest encryption path.

---

## Key Management

- **Source:** Argon2id applied to the operator's `--secure` password and the salt from the encrypted config header
- **Salt location:** Embedded in the `config.yaml` header (a legacy `enc.salt` file is only a fallback)
- **Backup:** datawatch backs up nothing. Keep a copy of `config.yaml` — it contains the salt. Without it the key cannot be derived and all encrypted data is unrecoverable. Remember that, until the runtime config write gap above is closed, a copy taken after a settings change may be plain.
- **In-memory lifetime:** the key lives in process memory during daemon runtime and is zeroed on exit
- **Rotation / password change:** **not available.** There is no command to change the password or rotate the data key. `datawatch security` offers only `encryption status`, `encryption migrate`, `wipe-plaintext` and `logs`. A password change today means exporting what you need and starting a new data directory under the new password.
- **Vault key:** separate from the `--secure` password; see [Secrets Vault](#secrets-vault-secretsdb).

---

## Export / Decrypt

```bash
# Export (decrypts for the operator; output is plain text)
datawatch export --all --folder /path/to/output/
datawatch export --config --folder /path/to/output/

# Export a specific session's log
datawatch export --log <session-id> --folder /path/

# Print the encrypted application log
datawatch security logs
datawatch security logs --tail 100
```

Reads the password from `DATAWATCH_SECURE_PASSWORD` or prompts interactively.

**An export is a plain-text copy.** The config is re-serialised as YAML **with all
tokens**, and the output folder is created 0700 and files 0600; move or delete it
with that in mind.

**Known limitation:** `export --all` reads the legacy names `sessions.json` and
`schedules.json`, so it currently exports **neither the current session records
(`sessions.d/`) nor the schedule store (`schedule.json`)**. Do not rely on it as a
complete backup. Memory has its own export (see
[Memory behaviour to know about](#memory-behaviour-to-know-about)).

---

## Secrets Vault (`secrets.db`)

The secrets vault uses a **separate** encryption key independent of `--secure`:

- **Algorithm:** AES-256-GCM (random 12-byte nonce per save)
- **Key:** 32 random bytes in `secrets.key` next to `secrets.db` (mode 0600), or the `DATAWATCH_SECRETS_KEY` environment variable. It is **not** derived from the `--secure` password and **not** from the machine id.
- **Consequence:** a copy of the data directory carries the vault and its key. Use
  disk-level encryption, or the environment-variable key kept outside the data
  directory, if the directory can be copied.
- **External backends** (KeePass, 1Password, a vault server, chosen by
  `secrets.backend`) keep their own storage outside datawatch.
- **Purpose:** Stores `${secret:name}` references resolved at startup

Config fields can reference vault entries instead of containing raw values:

```yaml
anthropic:
  api_key: "${secret:anthropic-api-key}"
```

To import an API key from the environment:

```bash
datawatch secrets import claude --from-env ANTHROPIC_API_KEY
```

This means API keys never need to appear in `config.yaml` at all — they live only
in `secrets.db`. Fields that hold a `${secret:...}` reference keep the value out
of an unencrypted `config.yaml`, which matters while the runtime config write gap
above is open.

---

## Open items for the operator

These are decisions or checks this page cannot make. They are collected in the
[audit](plans/2026-11-13-encryption-audit.md) with the options and costs.

- Which of the fixes enter the 10.0 scope (the audit's prioritised list).
- Whether the runtime `config.yaml` write gap is reproduced and fixed first, since
  the whole config, including any raw tokens, is at stake.
- The storage direction for the data directory (see "Storage direction" in the audit).
- Whether `secure_tracking` should be described in the config reference (it is undocumented today).
- Whether plain exports, and a key-change command, are wanted for 10.0.

This page describes behaviour as of v9.172.5. It is updated as each fix lands.
