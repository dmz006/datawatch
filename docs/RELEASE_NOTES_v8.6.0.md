# datawatch v8.6.0 — Full Operational Data Encryption

**Released:** 2026-05-19  
**Backlog:** BL334 T43g + T43h

---

## Summary

v8.6.0 completes the `--secure` encryption coverage across all operational data
files. After this release, every file that can practically be encrypted with a
symmetric key derived from the operator's `--secure` password is encrypted.

---

## What's New

### T43g — JSON store encryption

Four additional data stores are now encrypted when `--secure` is active:

| File | Contents | Risk if plaintext |
|---|---|---|
| `~/.datawatch/servers.json` | Federation peer registry, peer tokens | Token exposure → peer impersonation |
| `~/.datawatch/skills.json` | Skills registry index, sync state | Skill metadata and auth refs |
| `~/.datawatch/compute/nodes.json` | ComputeNode registry | Infrastructure topology |
| `~/.datawatch/inference/llms.json` | LLM registry, API key refs | Backend configuration |

Each store uses `secfile.ReadFile` / `secfile.WriteFile` (XChaCha20-Poly1305,
DWDAT2 format) via the respective `NewXxxEncrypted` constructors wired from
`main.go` when `encKey != nil`.

**Upgrade compatibility:** On first `--secure` startup after upgrade,
`secfile.MigrateJSONStore` encrypts each file in place. Idempotent — already-
encrypted files are detected by their DWDAT2 header and skipped.

### T43h — Encrypted application log

When `--secure` is active the daemon redirects `log.SetOutput` to a
`secfile.EncryptedLogWriter` at `<data-dir>/daemon-app.log` immediately after
key derivation. Log lines are encrypted with XChaCha20-Poly1305 (DWLOG1 format).

**Important:** Boot messages written before the key is derived remain in the
plaintext `daemon.log` managed by the parent `daemonize` process. This is a
deliberate trade-off — the parent must write to the log before the child ever
runs, so the key is not available at that point.

**New CLI command:**

```bash
# Print all lines
datawatch security logs

# Print last 100 lines
datawatch security logs --tail 100
```

The command derives the Argon2id key from the `--secure` password and decrypts
`daemon-app.log` locally — no daemon process required.

### Updated security endpoints

`GET /api/security/encryption/status` now probes all six file categories:
- `channel_routing.json`, `servers.json`, `skills.json`,
  `compute/nodes.json`, `inference/llms.json`
- `daemon-app.log` (DWLOG1 magic header detection)
- Discussion WAL and participants files

`POST /api/security/encryption/migrate` migrates all six categories.

`POST /api/security/wipe-plaintext` wipes all six categories on confirmation.

---

## What Remains Unencryptable

| Item | Reason |
|---|---|
| `.git/` repo history | SHA-1 content-addressed storage — file-level encryption breaks all object references. Use LUKS/dm-crypt at the filesystem level. |
| Screen captures | Ephemeral — dispatched over WebSocket, never persisted to disk. |
| `daemon.log` (boot messages) | Written by the parent `daemonize` process before the child derives the key. |
| `secrets.db` | Separately encrypted with AES-256-GCM using its own keyfile — independent of `--secure`. |
| Memory embeddings (Postgres/SQLite) | Separately encrypted at the per-entry level via `memory.encrypt_content`. |

---

## Upgrade Guide

1. Stop the running daemon.
2. Install v8.6.0.
3. Start with `datawatch --secure` (as before).
4. On first startup, the daemon automatically encrypts all four new JSON stores.
5. Verify: `datawatch security encryption status` — all files should show `Encrypted: true`.
6. Optional: `datawatch security logs` to confirm daemon-app.log is being written.

No manual steps are required.
