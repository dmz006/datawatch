# Release Notes — v8.5.0

**Date**: 2026-05-19  
**Sprint**: T43 — Operational Data Encryption (BL334)  
**Stories**: 28 E2E stories (TS-751–TS-778), code-reviewed

---

## What's new

### BL334 — Operational Data Encryption

Closes the encryption gap introduced by BL332 (Discussion Scopes) and BL331 (Channel Routing). When `--secure` is active all operational data files are now encrypted with the same Argon2id-derived XChaCha20-Poly1305 key used for the config file and data stores.

**Files now encrypted when `--secure` is active:**

| File | Format | Notes |
|---|---|---|
| `~/.datawatch/discussions/<id>/wal.jsonl` | Per-line `ENC:<b64>` | Append-only; each line independently encrypted |
| `~/.datawatch/discussions/<id>/participants.json` | `DWDAT2` (secfile) | Atomic write via secfile.WriteFile |
| `~/.datawatch/channel_routing.json` | `DWDAT2` (secfile) | Atomic write via secfile.WriteFile |

**Upgrade-compatible migration:**  
On the first startup with `--secure` after upgrading to v8.5.0, the daemon automatically detects and encrypts any plaintext WAL lines, participants files, and `channel_routing.json`. Migration is idempotent — already-encrypted lines are skipped. No data is lost.

**Per-line WAL encryption:**  
Each WAL line is encrypted as `ENC:<base64(nonce24 + ciphertext)>`. Lines without the `ENC:` prefix are read as plaintext (migration compatibility). Conflict resolution markers appended by `POST .../conflicts/resolve` are also encrypted.

**Secure wipe (manual gate):**  
After migrating to encrypted format, plaintext originals can be irreversibly wiped:

```bash
# CLI — requires --confirm flag
datawatch security wipe-plaintext --confirm

# REST
POST /api/security/wipe-plaintext
{"confirm": true}
```

Each file is overwritten 3 passes (zeros → ones → random), then unlinked. **Note:** on modern SSDs and copy-on-write filesystems this is best-effort. Use LUKS or an encrypted home directory for hardware-level guarantees.

**Encryption status:**

```bash
datawatch security encryption status
# or
GET /api/security/encryption/status
```

Reports per-file encrypted/plaintext state and a summary.

**Manual migration trigger:**

```bash
datawatch security encryption migrate
# or
POST /api/security/encryption/migrate
```

Useful if you want to migrate without restarting the daemon.

**REST surface:**

| Method | Path | Cap | Description |
|---|---|---|---|
| GET | `/api/security/encryption/status` | CommRead | Per-file encryption state |
| POST | `/api/security/encryption/migrate` | CommWrite | Encrypt all plaintext operational files |
| POST | `/api/security/wipe-plaintext` | CommWrite | 3-pass secure wipe (body: `{"confirm":true}`) |

**CLI:** `datawatch security encryption {status,migrate}`, `datawatch security wipe-plaintext --confirm`

---

## What `--secure` now covers

| Data | Encrypted | How |
|---|---|---|
| `~/.datawatch/config.yaml` | ✅ | Argon2id + XChaCha20-Poly1305 |
| `~/.datawatch/channel_routing.json` | ✅ (v8.5.0) | secfile DWDAT2 |
| `~/.datawatch/discussions/*/wal.jsonl` | ✅ (v8.5.0) | Per-line ENC: prefix |
| `~/.datawatch/discussions/*/participants.json` | ✅ (v8.5.0) | secfile DWDAT2 |
| `~/.datawatch/sessions/*/output.log` | ✅ | secfile DWLOG1 stream |
| Data stores (alerts, filters, commands, schedules, profiles) | ✅ | secfile DWDAT2 |
| `~/.datawatch/memories.db` / Postgres | Separate opt-in | `memory.encrypt_content` |
| `~/.datawatch/secrets.db` | Separate key | AES-256-GCM, own passphrase |

---

## Secrets and the vault

Config fields accept `${secret:name}` references resolved at startup by `secrets.ResolveConfig`. This means API keys, webhook tokens, and other credentials can live exclusively in the vault and be referenced from config:

```yaml
# config.yaml — key never stored in the file itself
llms:
  - name: anthropic
    api_key: "${secret:anthropic-api-key}"
```

Import into vault:
```bash
datawatch secrets import claude --from-env ANTHROPIC_API_KEY
datawatch secrets import openai --from-env OPENAI_API_KEY
```

The secrets vault (`~/.datawatch/secrets.db`) uses AES-256-GCM with its own key file (`~/.datawatch/secrets.key`) independent of `--secure`.

---

## Fixes

- Discussion WAL file mode changed from `0644` to `0600` (was world-readable on multi-user systems).
- Conflict-resolve markers now use `discussionAppendWALEntry` (encrypted when `--secure`) instead of a raw file append.
