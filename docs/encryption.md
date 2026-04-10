# Encryption at Rest

datawatch supports encrypting all sensitive data at rest using AES-256-GCM with Argon2id key derivation.

## Enabling Encryption

Encryption can be enabled **at any time** on an existing installation — plaintext files are automatically migrated.

| Method | How |
|--------|-----|
| **CLI** | `datawatch --secure start` (prompts for password) or `datawatch --secure start --foreground` |
| **Environment** | `export DATAWATCH_SECURE_PASSWORD="passphrase"` then `datawatch start` (auto-detects encrypted config) |
| **Web UI** | Settings → About tab shows encryption status (encrypted: true/false) |
| **REST API** | `GET /api/health` returns `"encrypted": true/false` |
| **Comm channel** | N/A — encryption is controlled via CLI flags, not chat commands (security by design) |

```bash
# Start with --secure flag (prompts for password)
datawatch --secure start

# Or use environment variable for non-interactive operation
export DATAWATCH_SECURE_PASSWORD="your-strong-password"
datawatch --secure start
```

### Auto-Detection

If the config file is already encrypted (`DWATCH1` header), datawatch auto-enables secure mode without needing `--secure` flag. The password is read from `DATAWATCH_SECURE_PASSWORD` env var or prompted interactively.

### Enable Encryption at Any Time

Encryption can be enabled on an existing installation — not just at initial setup. Simply
run `datawatch --secure start` and your plaintext config and data files are automatically
encrypted on the first run. Subsequent starts auto-detect the encrypted config.

### Daemon / Background Mode

Encryption works in daemon mode when the `DATAWATCH_SECURE_PASSWORD` environment variable
is set. The daemon reads the password from the variable instead of prompting interactively:

```bash
export DATAWATCH_SECURE_PASSWORD="your-strong-password"
datawatch start          # runs as background daemon, no interactive prompt
```

## What Gets Encrypted

### When `--secure` is enabled:

| File | Encrypted | Format | Location |
|------|-----------|--------|----------|
| config.yaml | **YES** | `DWATCH1\n` + base64(salt + nonce + ciphertext) | Config path |
| sessions.json | **YES** | `DWDAT1\n` + base64(nonce + ciphertext) | `{data_dir}/` |
| alerts.json | **YES** | `DWDAT1\n` format | `{data_dir}/` |
| commands.json | **YES** | `DWDAT1\n` format | `{data_dir}/` |
| filters.json | **YES** | `DWDAT1\n` format | `{data_dir}/` |
| schedules.json | **YES** | `DWDAT1\n` format | `{data_dir}/` |
| output.log.enc | **YES** | `DWLOG1\n` + length-prefixed encrypted blocks | `{data_dir}/sessions/{id}/` |
| session.json (tracking) | **YES** | `DWDAT2\n` when `--secure` active | `{data_dir}/sessions/{id}/` |

### NOT encrypted (by design):

| File | Reason |
|------|--------|
| Session tracking .md files | Git-tracked, need readable diffs (encrypted with `secure_tracking: full`) |
| daemon.log | Operational logs for troubleshooting |
| tmux output (live) | tmux pipe-pane operates on raw terminal data |
| daemon.log | Operational logs for troubleshooting |

## Encryption Architecture

### Config File Encryption (v2)

- **Algorithm:** XChaCha20-Poly1305
- **KDF:** Argon2id (time=1, memory=64MB, threads=4, output=32 bytes)
- **Salt:** 16 random bytes, embedded in the encrypted file (no separate salt file)
- **Nonce:** 24 bytes (XChaCha20 extended nonce — reduces collision risk)
- **Format:** `DWATCH2\n` + base64(salt16 + nonce24 + ciphertext)
- **Each save** generates a fresh nonce (different ciphertext every write)
- **Backward compat:** v1 files (`DWATCH1\n`, AES-256-GCM) are read transparently

### Data Store Encryption (v2)

- **Algorithm:** XChaCha20-Poly1305
- **Key:** 32-byte key derived from password + salt extracted from config file
- **Salt:** Extracted from the encrypted config header
- **Nonce:** 24 bytes per operation
- **Format:** `DWDAT2\n` + base64(nonce24 + ciphertext)
- **Backward compat:** v1 files (`DWDAT1\n`, AES-256-GCM) are read transparently

### Streaming Log Encryption

- **Algorithm:** XChaCha20-Poly1305 per block
- **Block size:** 4096 bytes of plaintext per encrypted block
- **Nonce:** 24 bytes per block (fresh random nonce)
- **Format:** `DWLOG1\n` + repeated [u32le_block_length][nonce24 + ciphertext]
- **Each block** is independently decryptable
- **Mechanism:** Named FIFO pipe; tmux writes to FIFO, background goroutine encrypts

### Post-Quantum Safety

XChaCha20-Poly1305 with 256-bit keys is considered post-quantum safe for symmetric encryption.
Grover's algorithm reduces effective key strength to 128-bit equivalent, which remains secure.

## Export Command

Decrypt and export data from an encrypted installation:

```bash
# Export everything (config, logs, stores)
datawatch export --all --folder /path/to/output/

# Export just the config
datawatch export --export-config --folder /path/

# Export a specific session's log
datawatch export --log <session-id> --folder /path/
```

The export command reads the password from `DATAWATCH_SECURE_PASSWORD` or prompts interactively.

## Environment Variable

Set `DATAWATCH_SECURE_PASSWORD` to enable non-interactive operation:

```bash
export DATAWATCH_SECURE_PASSWORD="your-password"
datawatch start                    # auto-detects encrypted config
datawatch export --all --folder /tmp/export  # decrypts without prompt
```

## Memory Content Encryption

The episodic memory system supports **hybrid content encryption** — sensitive text
fields (`content`, `summary`) are encrypted with XChaCha20-Poly1305 while embeddings
and metadata remain unencrypted for search.

### How it works

When `--secure` mode is active, the memory store automatically uses the same encryption
key as the rest of the system. Content is encrypted on save, decrypted on read —
transparent to all commands and search.

- **Encrypted:** `content`, `summary` fields (sensitive text)
- **Unencrypted:** embeddings (needed for vector search), metadata (role, wing, room,
  timestamps, content_hash for dedup)

### Enabling memory encryption

| Method | How |
|--------|-----|
| **Automatic** | Active when `--secure` mode is enabled — memory inherits the same key |
| **Standalone** | Place a 32-byte key at `{data_dir}/memory.key` (auto-detected on startup) |
| **Generate key** | Use `KeyManager.Generate()` to create and store a random key |

### Key management

- **Rotation:** `RotateKey()` re-encrypts all content with a new key
- **Fingerprint:** SHA-256 first 8 hex chars, shown in Monitor tab stats
- **Migration:** `MigrateToEncrypted()` encrypts existing plaintext content in-place
- **Export with key:** Export includes key material for encrypted backup transfer

### Configuration

```yaml
memory:
  enabled: true
  # Memory encryption is automatic when --secure is active.
  # No separate config needed — it uses the same encryption key.
```

The Monitor tab and `/api/memory/stats` show `encrypted: true/false` and the
key fingerprint when encryption is active.

### PostgreSQL backend

Memory encryption works identically with the PostgreSQL backend. Content and
summary fields are encrypted before INSERT, decrypted after SELECT. The
encryption key comes from `--secure` mode or `{data_dir}/memory.key`, same as
SQLite. Embeddings remain as BYTEA columns for vector search.

### What an attacker sees with DB access

| Field | Visible? | Content |
|-------|----------|---------|
| content | No | `ENC:base64(nonce+ciphertext)` |
| summary | No | `ENC:base64(nonce+ciphertext)` |
| embedding | Yes | Float32 vectors (not human-readable, but could infer topics) |
| role/wing/room | Yes | Structural metadata |
| content_hash | Yes | SHA-256 hash (reveals identical content, not content itself) |

## Security Considerations

- **Password strength:** Use a strong password (32+ characters recommended)
- **Salt:** Embedded in the encrypted config file header. No separate salt file needed.
- **Memory:** The derived key lives in memory during daemon runtime. It is zeroed on exit.
- **Backup:** Always backup the encrypted config.yaml — the salt is embedded in it
- **Config secrets:** Tokens, API keys, and passwords in config.yaml are encrypted at rest when `--secure` is active
- **Memory encryption:** Content encrypted with XChaCha20-Poly1305, embeddings remain searchable
