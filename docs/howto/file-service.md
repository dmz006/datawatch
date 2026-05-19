---
docs:
  index: true
  topics: [file-service, federation, files, bl333, upload, peers, discussions]
exec_params:
  - name: file_path
    description: "Local path of the file to upload"
    required: false
exec_steps:
  - tool: get_config
    description: Check file_service_root config
    args: {}
    read_only: true
---
# How-to: Federated File Service (BL333)

The federated file service provides structured file storage on a datawatch
daemon, accessible from any surface — REST, CLI, MCP, PWA — and
controllable from federation peers that have the appropriate capabilities.
Files are organized under a configurable root directory with `peers/` and
`discussions/` subdirectories for cross-agent file sharing patterns.

## Overview

```
~/.datawatch/files/          ← file_service_root (configurable)
├── peers/
│   ├── nas-peer/            ← GET /api/files/peers/nas-peer
│   └── cloud-gpu/
└── discussions/
    └── disc-abc123/         ← GET /api/files/discussions/disc-abc123
```

Files are stored on the daemon's local filesystem. The service validates
all paths to prevent directory traversal. Write operations (`upload`,
`delete`) require `config:write`; reads require `config:read`.

## Prerequisites

- A running datawatch daemon (see [`setup-and-install.md`](setup-and-install.md)).
- Bearer token with `config:read` for listing/downloading, `config:write`
  for uploading and deleting.

---

## 1 — Configure file_service_root

By default the daemon stores files at `~/.datawatch/files/`. Override this
with `file_service_root` in your `datawatch.yaml`:

```yaml
# datawatch.yaml
session:
  file_service_root: /mnt/data/datawatch-files
```

Priority order: `file_service_root` → `root_path` → user home directory.

After changing the config, reload the daemon:

```sh
datawatch reload
# or send SIGHUP: kill -HUP $(cat ~/.datawatch/daemon.pid)
```

### Check the configured root

```sh
# REST
curl -sk \
  -H "Authorization: Bearer $TOKEN" \
  https://datawatch.local:8443/api/files/meta | jq .root
# "/home/operator/.datawatch/files"

# CLI
datawatch files list

# MCP
files_meta()
```

---

## 2 — Upload a file

### REST (multipart/form-data)

```sh
# Upload a file to the peers/nas-peer/ subdirectory.
curl -sk -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -F "file=@/tmp/build-output.txt" \
  -F "path=peers/nas-peer/build-output.txt" \
  https://datawatch.local:8443/api/files
# {"ok":true,"path":"peers/nas-peer/build-output.txt"}

# Upload to a discussion context.
curl -sk -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -F "file=@/tmp/design-doc.md" \
  -F "path=discussions/disc-abc123/design-doc.md" \
  https://datawatch.local:8443/api/files
# {"ok":true,"path":"discussions/disc-abc123/design-doc.md"}
```

The `path` form field is the destination path **relative to
`file_service_root`**. Paths that contain `..` or attempt to escape the
root are rejected with 400.

### CLI

```sh
# Upload with an explicit remote path.
datawatch files upload /tmp/build-output.txt \
  --path peers/nas-peer/build-output.txt

# Upload a file; datawatch infers the filename.
datawatch files upload /tmp/notes.md --path discussions/disc-abc123/notes.md
```

### MCP

```
files_upload(
  path="peers/nas-peer/build-output.txt",
  content="<file content as string>"
)
# → {"ok":true,"path":"peers/nas-peer/build-output.txt"}
```

### PWA

1. Open **Settings → General → File Service**.
2. The card shows the current storage root, peer count, discussion count,
   and total disk usage.
3. Use the **Upload** button to choose a local file and set a destination path.

---

## 3 — List files in a peer or discussion subdirectory

### REST

```sh
# List files under peers/<name>/.
curl -sk \
  -H "Authorization: Bearer $TOKEN" \
  https://datawatch.local:8443/api/files/peers/nas-peer | jq .
# [{"name":"build-output.txt","size":4096,"modified":"2026-05-19T12:00:00Z"}]

# List files under discussions/<id>/.
curl -sk \
  -H "Authorization: Bearer $TOKEN" \
  https://datawatch.local:8443/api/files/discussions/disc-abc123 | jq .

# Storage overview.
curl -sk \
  -H "Authorization: Bearer $TOKEN" \
  https://datawatch.local:8443/api/files/meta | jq .
# {"root":"/home/operator/.datawatch/files","peer_count":2,"discussion_count":1,"disk_bytes":8192}
```

### CLI

```sh
# List all files (top-level summary).
datawatch files list

# List files in a peer's subdir.
datawatch files peer nas-peer
```

### MCP

```
files_meta()
# → {"root":"...","peer_count":N,"discussion_count":N,"disk_bytes":N}
```

---

## 4 — Delete a file

### REST

```sh
curl -sk -X DELETE \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"path":"peers/nas-peer/build-output.txt"}' \
  https://datawatch.local:8443/api/files
# {"ok":true}
```

Path traversal attempts are rejected:

```sh
curl -sk -X DELETE \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"path":"../../etc/passwd"}' \
  https://datawatch.local:8443/api/files
# 400 Bad Request
```

### CLI

```sh
datawatch files delete peers/nas-peer/build-output.txt
```

### MCP

```
files_delete(path="peers/nas-peer/build-output.txt")
# → {"ok":true}
```

---

## 5 — Federation access control

The file service uses the `config:*` capability surface.

| Endpoint | Method | Required cap |
|---|---|---|
| `GET /api/files/meta` | GET | `config:read` |
| `GET /api/files/peers/{name}` | GET | `config:read` |
| `GET /api/files/discussions/{id}` | GET | `config:read` |
| `POST /api/files` (upload) | POST | `config:write` |
| `DELETE /api/files` | DELETE | `config:write` |

Federation peer groups that satisfy these requirements:

| Group | config:read | config:write |
|---|---|---|
| `read-only` | ✓ | — |
| `config-reader` | ✓ | — |
| `config-admin` | ✓ | ✓ |
| `full-control` | ✓ | ✓ |
| `federation-peer` (default) | — | — |

A standard `federation-peer` cannot access the file service at all. Upgrade
to `config-reader` for read-only access or `config-admin` for full access.

```sh
# Test: read-only peer can list (config:read via read-only group)
curl -sk \
  -H "Authorization: Bearer <read-only-peer-token>" \
  https://datawatch.local:8443/api/files/meta
# → 200 OK

# Test: read-only peer cannot upload (needs config:write → 403)
curl -sk -X POST \
  -H "Authorization: Bearer <read-only-peer-token>" \
  -F "file=@/tmp/test.txt" \
  -F "path=peers/remote/test.txt" \
  https://datawatch.local:8443/api/files
# → 403 Forbidden
```

---

## Cross-peer file sharing pattern

A common pattern: a primary daemon acts as the file hub; remote peers
upload their build artifacts / context files to the primary's file service,
then other agents list and fetch those files.

```
nas-peer  ──POST /api/files──►  primary daemon
                                 └── peers/nas-peer/artifact.tar.gz

cloud-gpu ──GET /api/files/peers/nas-peer──► list artifacts
          ──download artifact──► process on GPU
```

Both peers need `config:read` at minimum. The uploading peer needs
`config:write`. Configure their capabilities accordingly:

```sh
# nas-peer needs config:write to upload its artifacts.
curl -sk -X PUT \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"capabilities":"config-admin"}' \
  https://datawatch.local:8443/api/federation/peers/<nas-peer-id>

# cloud-gpu needs config:read to list and download.
curl -sk -X PUT \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"capabilities":"config-reader"}' \
  https://datawatch.local:8443/api/federation/peers/<cloud-gpu-id>
```

---

## Troubleshooting

**400 on upload.** Check the `path` form field — it must not contain `..`
and must be a valid relative path. Ensure the `Content-Type` of the curl
request is `multipart/form-data` (use `-F`, not `-d`).

**403 on all file operations.** Your token or peer token lacks `config:read`
or `config:write`. Upgrade the peer's capabilities to `config-reader` or
`config-admin`.

**Files not appearing after upload.** Verify `file_service_root` is set
correctly and the daemon has write access to that path. Check daemon logs:
`datawatch daemon logs --tail 50`.

**Disk usage not matching.** The `disk_bytes` in `GET /api/files/meta`
reflects files under the configured root only. Files uploaded before
`file_service_root` was changed are not counted.

---

## See also

- [`howto/channel-routing.md`](channel-routing.md) — route channel messages to federation peers
- [`howto/federated-observer.md`](federated-observer.md) — configure federation peers
- [`howto/secrets-manager.md`](secrets-manager.md) — store API token with `${secret:DATAWATCH_TOKEN}`
- [`docs/datawatch-definitions.md`](../datawatch-definitions.md) — capability group reference
