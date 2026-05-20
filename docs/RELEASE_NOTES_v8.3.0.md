# Release Notes — v8.3.0

**Date**: 2026-05-19  
**Sprint**: T41 — Channel Routing + File Service (BL331 + BL333)  
**Stories**: 36 E2E stories (TS-683–TS-718), 32 pass, 4 skipped (cap enforcement / live session in no-auth test env)

---

## What's new

### BL331 — Channel Routing

Map inbound channel identities to specific federation peers. When the daemon receives a message from a known channel identity (Telegram group ID, Signal number, webhook URL pattern), it routes to the configured peer with optional automata type and default project directory.

**REST**: `GET /PUT /api/channel/routing` — body: `{rules: [{channel_pattern, peer_name, automata_type?, default_project_dir?}]}`

**Federation peer `channel_identity` field**: Peers can now declare which channel identities they own (`--channel-identity` on `federation peer add/update`).

**14th built-in federation group: `comms-channel-agent`** — for peers acting as channel address agents. Capabilities: sessions list/read/input/write, comm read/write, alerts list/read, autonomous list/read/write.

**Session and PRD `owner_peer` field**: when routing creates a session or PRD on behalf of a peer, the originating peer is recorded.

PWA: Settings → Comms → Channel Routing card.  
CLI: `datawatch federation peer add --channel-identity <patterns>`

How-to guide: [docs/howto/channel-routing.md](howto/channel-routing.md)

### BL333 — File Service

Federated file upload/delete/list under a configurable service root. All write paths have path-traversal guards — files cannot escape the service root.

**REST**:
- `POST /api/files` — multipart upload (50 MB limit, `path` field required, must be absolute under root)
- `DELETE /api/files` — JSON `{path}`
- `GET /api/files/peers/{name}` — list peer-specific subdirectory
- `GET /api/files/discussions/{id}` — list discussion-specific subdirectory
- `GET /api/files/meta` — storage overview (root, peer subdirs, discussion subdirs)

**Configuration**: `session.file_service_root` in config.yaml (defaults to `session.root_path` or home dir).

CLI: `datawatch files {list,upload,delete,peer}`

How-to guide: [docs/howto/file-service.md](howto/file-service.md)

---

## Fixes

- `PUT /api/channel/routing`: rules without `channel_pattern` now return HTTP 400 (was silently accepted).
- `federation peer add --channel-identity`: flag was missing; added to both `peer add` and `peer update`.
