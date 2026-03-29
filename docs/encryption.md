# Encryption at Rest

datawatch supports encrypting all sensitive data at rest using AES-256-GCM with Argon2id key derivation.

## Enabling Encryption

```bash
# Start with --secure flag (prompts for password)
datawatch --secure start

# Or use environment variable for non-interactive operation
export DATAWATCH_SECURE_PASSWORD="your-strong-password"
datawatch --secure start
```

### Auto-Detection

If the config file is already encrypted (`DWATCH1` header), datawatch auto-enables secure mode without needing `--secure` flag. The password is read from `DATAWATCH_SECURE_PASSWORD` env var or prompted interactively.

### First-Time Migration

On the first `--secure` start with a plaintext config, datawatch automatically encrypts the config file. Subsequent starts auto-detect the encrypted config.

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
| enc.salt | Plaintext | 16-byte random salt for Argon2id KDF | `{data_dir}/` |

### NOT encrypted (by design):

| File | Reason |
|------|--------|
| Session tracking .md files | Git-tracked, need readable diffs |
| session.json (in tracking folder) | Duplicates store data |
| tmux output (live) | tmux pipe-pane operates on raw terminal data |
| daemon.log | Operational logs for troubleshooting |

## Encryption Architecture

### Config File Encryption

- **Algorithm:** AES-256-GCM
- **KDF:** Argon2id (time=1, memory=64MB, threads=4, output=32 bytes)
- **Salt:** 16 random bytes, embedded in the encrypted file
- **Format:** `DWATCH1\n` + base64(salt16 + nonce12 + ciphertext)
- **Each save** generates a fresh nonce (different ciphertext every write)

### Data Store Encryption

- **Algorithm:** AES-256-GCM
- **Key:** 32-byte key derived from password + external salt via Argon2id
- **Salt:** Stored separately in `{data_dir}/enc.salt` (generated once, shared by all stores)
- **Format:** `DWDAT1\n` + base64(nonce12 + ciphertext)

### Streaming Log Encryption

- **Algorithm:** AES-256-GCM per block
- **Block size:** 4096 bytes of plaintext per encrypted block
- **Format:** `DWLOG1\n` + repeated [u32le_block_length][nonce12 + ciphertext]
- **Each block** is independently decryptable (fresh nonce per block)
- **Mechanism:** Named FIFO pipe; tmux writes to FIFO, background goroutine encrypts

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

## Security Considerations

- **Password strength:** Use a strong password (32+ characters recommended)
- **Salt file:** `enc.salt` is NOT secret but MUST be preserved. Without it, data stores cannot be decrypted even with the correct password.
- **Memory:** The derived key lives in memory during daemon runtime. It is zeroed on exit.
- **Backup:** Always backup `enc.salt` alongside encrypted data stores
- **Config secrets:** Tokens, API keys, and passwords in config.yaml are encrypted at rest when `--secure` is active
