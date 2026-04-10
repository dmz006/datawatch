# BL68: Hybrid Memory Content Encryption

**Date:** 2026-04-09
**Priority:** medium (Tier 2)
**Effort:** 2-3 days
**Category:** memory / security

---

## Overview

Encrypt memory content at rest using the existing XChaCha20-Poly1305 infrastructure
while keeping metadata (role, project_dir, timestamps) and embeddings searchable.
This is "hybrid" because:

- **Encrypted**: `content`, `summary` fields (sensitive text)
- **Unencrypted**: `embedding` vectors (needed for cosine similarity search),
  `role`, `project_dir`, `session_id`, `created_at`, `content_hash` (needed for
  dedup and filtering)

## Architecture

```
┌──────────────────────────────────────────────────┐
│                Memory Store                       │
│                                                   │
│  content ──→ XChaCha20-Poly1305 ──→ encrypted    │
│  summary ──→ XChaCha20-Poly1305 ──→ encrypted    │
│  embedding ──→ (plaintext, for vector search)    │
│  metadata ──→ (plaintext, for filtering)         │
│                                                   │
│  Key: derived from DATAWATCH_SECURE_PASSWORD     │
│       via Argon2id (same as config encryption)   │
│       OR auto-generated and stored in keyfile    │
└──────────────────────────────────────────────────┘
```

## Key Management

### Key Sources (priority order)

1. **Existing secure mode key**: If `--secure` is active, reuse the same
   `encKey` derived from `DATAWATCH_SECURE_PASSWORD`. This is the recommended
   approach — one password protects everything.

2. **Separate memory key**: Config `memory.encryption_key_file` points to a
   keyfile. Useful when memory encryption is wanted without full secure mode.

3. **Auto-generated key**: If `memory.encryption: true` but no key source,
   auto-generate a 32-byte random key and store in `{data_dir}/memory.key`.
   User is warned that this key must be backed up for data recovery.

### Key Operations

| Operation | Command | API | Description |
|-----------|---------|-----|-------------|
| **Generate** | `datawatch memory keygen` | `POST /api/memory/keygen` | Generate new random key |
| **Rotate** | `datawatch memory rotate-key` | `POST /api/memory/rotate-key` | Re-encrypt all content with new key |
| **Export with key** | `memories export --include-key` | `GET /api/memory/export?include_key=true` | JSON export includes base64-encoded key |
| **Import with key** | `memories import --key <file>` | `POST /api/memory/import` (key in header) | Import encrypted backup with provided key |
| **Status** | `memories status` | `GET /api/memory/stats` | Shows encryption status, key fingerprint |

### Key Rotation Flow

```
1. Load current key (old_key)
2. Generate new key (new_key)
3. For each memory row:
   a. Decrypt content with old_key
   b. Encrypt content with new_key
   c. Update row in-place
4. Write new keyfile
5. WAL log: "key_rotate" with old/new key fingerprints
6. Delete old keyfile
```

## Config

```yaml
memory:
  encryption: true               # Enable content encryption (default: false)
  encryption_key_file: ""        # Path to keyfile (empty = auto from --secure or auto-gen)
```

### Access Methods

| Method | How |
|--------|-----|
| **YAML** | `memory.encryption: true` |
| **Web UI** | Settings → General → Episodic Memory → Encryption toggle |
| **CLI** | `configure memory.encryption=true` |
| **Comm channel** | `configure memory.encryption=true` |
| **API** | `PUT /api/config {"key":"memory.encryption","value":true}` |

### Recommended Secure Mode

For full data protection, use secure mode which encrypts everything:

```bash
# Set password (persists across restarts)
export DATAWATCH_SECURE_PASSWORD="your-strong-password"

# Start with secure mode
datawatch start --secure
```

This encrypts: config.yaml, sessions.json, schedules.json, alerts.json,
cmdlib.json, filters.json, output.log, tracker files, AND memory content.
One password, one key, complete protection.

For memory-only encryption without full secure mode:

```bash
# Enable memory encryption in config
memory:
  enabled: true
  encryption: true
  # Key auto-generated at {data_dir}/memory.key
  # BACK UP THIS FILE — lost key = lost memories
```

## Implementation

### Phase 1: Store-level encryption

**File:** `internal/memory/store.go`

```go
type Store struct {
    db      *sql.DB
    walPath string
    walMu   sync.Mutex
    encKey  []byte // 32-byte XChaCha20 key, nil = no encryption
}

func NewStoreEncrypted(dbPath string, encKey []byte) (*Store, error)

// encryptField encrypts a string field with XChaCha20-Poly1305
func (s *Store) encryptField(plaintext string) (string, error)

// decryptField decrypts a base64-encoded encrypted field
func (s *Store) decryptField(ciphertext string) (string, error)
```

- `Save()`: encrypt content+summary before INSERT
- `ListRecent()`, `Search()`, `ListFiltered()`: decrypt content+summary after SELECT
- `Export()`: optionally include key material
- `Import()`: accept key for re-encryption

### Phase 2: Key management

**File:** `internal/memory/keymanager.go`

```go
type KeyManager struct {
    keyPath string
    key     []byte
}

func NewKeyManager(dataDir string) *KeyManager
func (km *KeyManager) Generate() ([]byte, error)
func (km *KeyManager) Load() ([]byte, error)
func (km *KeyManager) Fingerprint() string // SHA-256 of key, first 8 hex chars
func (km *KeyManager) RotateKey(store *Store, newKey []byte) error
```

### Phase 3: Config + API + MCP

- Config: `memory.encryption`, `memory.encryption_key_file`
- API: `POST /api/memory/rotate-key`, key fingerprint in stats
- MCP: `memory_stats` shows encryption status
- Comm: `configure memory.encryption=true`
- Web: Encryption toggle in Memory settings card

## Testing

- Unit: encrypt/decrypt roundtrip, key rotation, import/export with key
- API: Create encrypted memory, recall it, export with key, import on fresh store
- Validation: encrypted content in SQLite is NOT readable without key
- Regression: all existing memory tests pass with encryption enabled AND disabled

## Migration

When encryption is first enabled on an existing store:
1. Detect unencrypted content rows (no encryption prefix)
2. Encrypt all existing content in-place
3. WAL log: "encrypt_migration" with row count

## Security Notes

- Embeddings remain unencrypted — they are floating-point vectors, not human-readable text.
  An attacker with DB access could potentially use embeddings to infer topics but cannot
  recover exact content.
- content_hash remains unencrypted — it enables dedup without decryption. The hash of
  normalized lowercase content reveals only that two memories are identical, not their content.
- WAL entries log operation types and IDs but never log content.
