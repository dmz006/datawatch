# Test Plan — v8.3.0

**Version**: v8.3.0  
**Sprint**: T41/T43 — Channel-address federation + federated file service  
**Stories**: TS-683–TS-718 (36 stories)  
**Go unit tests**: 486 (385 server + 94 autonomous + 7 federation push cap)

## Scope

| BL | Feature | Stories | Go tests |
|---|---|---|---|
| BL331 | Channel-address federation via Comms | TS-683–TS-700 (18 stories) | bl331_channel_routing_test.go (4 tests) |
| BL333 | Federated file service | TS-701–TS-718 (18 stories) | bl333_file_service_test.go (3 tests) |

## New Go tests in v8.3.0

| File | Tests | What they cover |
|---|---|---|
| `internal/server/bl331_channel_routing_test.go` | 4 | GET /api/channel/routing (empty), PUT adds rule, PUT validates required fields, federation cap enforcement (comm:read/write) |
| `internal/server/bl333_file_service_test.go` | 3 | POST /api/files upload, DELETE /api/files remove, GET /api/files/meta storage overview |

**Running total:**  
- Server tests: 378 (v8.2.0 baseline) + 4 (BL331) + 3 (BL333) = **385**  
- Autonomous tests: 94 (unchanged)  
- Federation push cap tests: 7 (unchanged from BL330)  
- **Total: 486**

## Federation cap enforcement verified

BL331 channel routing endpoints use the `comm:*` capability surface:
- `GET /api/channel/routing` — requires `comm:read`
- `PUT /api/channel/routing` — requires `comm:write`

BL333 file service endpoints use the `config:*` capability surface:
- `POST /api/files` (upload) — requires `config:write`
- `DELETE /api/files` — requires `config:write`
- `GET /api/files/peers/*`, `GET /api/files/discussions/*`, `GET /api/files/meta` — require `config:read`

See [cookbook.md](cookbook.md) § Federation Access Matrix for the full endpoint→cap table.

## New structs and config fields

### BL331
- `ChannelIdentity []string` added to `multiserver.Entry` (federation peer config)
- New 14th builtin federation group: `comms-channel-agent`
- `OwnerPeer string` added to `Session` and `PRD` structs
- Channel routing rule fields: `channel_pattern`, `peer_name`, `automata_type`, `default_project_dir`
- Config stored in `~/.datawatch/channel_routing.json`

### BL333
- New config field `file_service_root` in the `session:` config block
- Priority order: `file_service_root` → `root_path` → user home dir
- 3 new MCP tools: `files_upload`, `files_delete`, `files_meta`
- New CLI subcommands: `datawatch files list|upload|delete|peer`

## PWA additions

- **BL331**: Federation peer form shows `channel_identity` text input; Settings → Comms → Channel Routing card with rules list + add form
- **BL333**: File Service card in Settings → General with storage overview (root path, peer/discussion counts, disk usage)

## Run

```sh
# Go unit tests
go test ./internal/server/... ./internal/autonomous/... -timeout 90s

# E2E against a live daemon
# See cookbook.md for full REST/CLI/MCP/PWA story scripts
```
