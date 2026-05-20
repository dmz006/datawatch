# Encryption at Rest

datawatch supports encrypting all sensitive data at rest using XChaCha20-Poly1305
with Argon2id key derivation when `--secure` mode is active.

## Enabling Encryption

Encryption can be enabled **at any time** on an existing installation — plaintext
files are automatically migrated on the first `--secure` startup.

| Method | How |
|--------|-----|
| **CLI** | `datawatch --secure start` (prompts for password) |
| **Environment** | `export DATAWATCH_SECURE_PASSWORD="passphrase"` then `datawatch start` (auto-detects encrypted config) |
| **Web UI** | Settings → About tab shows encryption status |
| **REST API** | `GET /api/health` returns `"encrypted": true/false` |
| **CLI status** | `datawatch security encryption status` |

```bash
# Start with --secure flag (prompts for password)
datawatch --secure start

# Or use environment variable for non-interactive / daemon mode
export DATAWATCH_SECURE_PASSWORD="your-strong-password"
datawatch --secure start
```

### Auto-Detection

If the config file is already encrypted (`DWATCH1`/`DWATCH2` header), datawatch
auto-enables secure mode without needing the `--secure` flag.

---

## What Gets Encrypted

### Encrypted when `--secure` is active

| File | Format | Notes |
|------|--------|-------|
| `config.yaml` | `DWATCH2` | XChaCha20-Poly1305 + Argon2id |
| `sessions.json` | `DWDAT2` | Session index |
| `alerts.json` | `DWDAT2` | Alert store |
| `commands.json` | `DWDAT2` | Saved command library |
| `filters.json` | `DWDAT2` | Filter store |
| `schedules.json` | `DWDAT2` | Schedule store |
| `servers.json` | `DWDAT2` | Federation peer registry (contains peer tokens) |
| `skills.json` | `DWDAT2` | Skills registry index |
| `compute/nodes.json` | `DWDAT2` | ComputeNode registry |
| `inference/llms.json` | `DWDAT2` | LLM registry |
| `channel_routing.json` | `DWDAT2` | Channel routing table |
| `discussions/<id>/participants.json` | `DWDAT2` | Discussion participant lists |
| `discussions/<id>/wal.jsonl` | Per-line `ENC:<b64>` | Each JSONL line encrypted independently |
| `sessions/<id>/output.log` | `DWLOG1` | Streaming block encryption via FIFO |
| `sessions/<id>/session.json` | `DWDAT2` | Session tracking record |
| `daemon-app.log` | `DWLOG1` | Application log after key derivation |

### Separately encrypted (independent of `--secure`)

| System | Mechanism | Notes |
|--------|-----------|-------|
| `secrets.db` | AES-256-GCM + own keyfile | Independent of `--secure` password. Lives at `{data_dir}/secrets.db`. |
| Memory DB (SQLite/Postgres) | Per-entry XChaCha20-Poly1305 | `content` and `summary` fields encrypted; embeddings stay plaintext for vector search. Controlled by `memory.encrypt_content`. |

---

## What CANNOT Be Encrypted — and Why

These files are structurally impossible to encrypt with the `--secure` key, or
are not stored on disk at all.

| File / Artifact | Why it cannot be encrypted |
|-----------------|---------------------------|
| **`daemon.log`** (boot messages) | Written by the **parent** `daemonize` process before the child process ever derives the encryption key. By the time the key exists, the daemon is already running and the parent has handed off. Boot messages are structurally pre-key. The encrypted `daemon-app.log` captures all log lines written _after_ key derivation. |
| **`.git/` repo history** under `root_path` | Git uses SHA-1 content-addressed object storage. Encrypting the object files would break every ref, commit pointer, and pack index — the repository would be unreadable. Encrypt at the **filesystem level** instead (LUKS/dm-crypt, VeraCrypt, encrypted home directory). |
| **Screen captures** | Never written to disk. Screen capture data is dispatched ephemerally over WebSocket to the requesting client and immediately discarded. There is no file to encrypt. |
| **tmux pipe output (live)** | tmux pipe-pane writes raw terminal escape sequences to the FIFO. The session output FIFO feeds the `EncryptedLogWriter` for `output.log`, so the final stored artifact _is_ encrypted. The in-flight pipe data in the kernel buffer is not at-rest data. |
| **Memory embeddings** | Float32 vectors must remain plaintext for vector search to work. The human-readable `content` and `summary` fields are encrypted separately (see above). |

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

**Note:** `daemon-app.log` is truncated on each daemon restart. It contains only
the current run's log. Boot messages from `daemon.log` are always plaintext.

---

## Upgrade Compatibility

Encryption can be enabled or the daemon upgraded at any time without data loss.

### Enabling `--secure` on an existing unencrypted installation

1. Run `datawatch --secure start`
2. On first startup the daemon automatically encrypts all existing plaintext files
3. No manual steps required — migration is idempotent (already-encrypted files are skipped)

### Upgrading from v8.5.x or earlier to v8.6.0+

v8.6.0 added encryption for four previously-plaintext stores (`servers.json`,
`skills.json`, `compute/nodes.json`, `inference/llms.json`). On first `--secure`
startup after upgrade:

1. Config decrypts as normal → same Argon2id key is derived
2. The four new stores are encrypted in-place automatically
3. All stores open via the new encrypted constructors and read the migrated files
4. No user action required

### Manual migration trigger

If the daemon is running and you want to migrate without restarting:

```bash
datawatch security encryption migrate
```

To verify status:

```bash
datawatch security encryption status
```

### Secure wipe of plaintext originals

After migration, plaintext copies of the files no longer exist (files are encrypted
in-place). If you want to 3-pass overwrite any remaining plaintext files as a
belt-and-suspenders measure:

```bash
datawatch security wipe-plaintext --confirm
```

> **Warning:** 3-pass overwrite (zeros/ones/random bytes) then unlink. On modern
> NVMe/SSD and copy-on-write filesystems (Btrfs, ZFS) the overwrite may not reach
> the underlying storage due to wear-leveling. Use LUKS or an encrypted home
> directory for stronger guarantees at the block-device level.

---

## Encryption Architecture

### Config File (`DWATCH2`)

- **Algorithm:** XChaCha20-Poly1305
- **KDF:** Argon2id (time=1, memory=64 MB, threads=4, output=32 bytes)
- **Salt:** 16 random bytes embedded in the file header (no separate salt file)
- **Nonce:** 24 bytes (fresh per write)
- **Backward compat:** v1 `DWATCH1` (AES-256-GCM) files read transparently

### Data Stores (`DWDAT2`)

- **Algorithm:** XChaCha20-Poly1305
- **Key:** 32-byte key derived from password + salt extracted from encrypted config
- **Nonce:** 24 bytes (fresh per write)
- **Format:** `DWDAT2\n` + base64(nonce24 + ciphertext)
- **Atomic write:** tmp file + rename — no partial writes
- **Backward compat:** v1 `DWDAT1` files read transparently

### Streaming Logs (`DWLOG1`)

Used for `output.log` (session) and `daemon-app.log`.

- **Algorithm:** XChaCha20-Poly1305 per block
- **Block size:** 4096 bytes of plaintext per encrypted block
- **Nonce:** 24 bytes per block (fresh random)
- **Format:** `DWLOG1\n` + repeated `[u32le_length][nonce24 + ciphertext]`
- **Each block independently decryptable** (no cross-block dependencies)
- **Note:** Opens with `O_TRUNC` — each daemon restart begins a fresh log

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

- **Source:** Argon2id KDF applied to the operator's `--secure` password + salt from encrypted config
- **Salt location:** Embedded in the `config.yaml` header (no separate file)
- **Backup:** Back up `config.yaml` — it contains the salt. Without it the key cannot be derived and all encrypted data is unrecoverable.
- **In-memory lifetime:** Key lives in process memory during daemon runtime; zeroed on exit (`defer zeroBytes(encKey)`)
- **Rotation:** Re-encrypt `config.yaml` with a new password via `datawatch security` (key change re-derives from new password + new salt; all stores must be re-migrated)

---

## Export / Decrypt

```bash
# Export everything (config, logs, stores)
datawatch export --all --folder /path/to/output/

# Export a specific session's log
datawatch export --log <session-id> --folder /path/

# Print the encrypted application log
datawatch security logs
datawatch security logs --tail 100
```

Reads password from `DATAWATCH_SECURE_PASSWORD` env var or prompts interactively.

---

## Secrets Vault (`secrets.db`)

The secrets vault uses a **separate** encryption key independent of `--secure`:

- **Algorithm:** AES-256-GCM
- **Key:** Stored in a separate keyfile (not derived from the `--secure` password)
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

This means API keys never need to appear in `config.yaml` at all — they live only in `secrets.db`.
